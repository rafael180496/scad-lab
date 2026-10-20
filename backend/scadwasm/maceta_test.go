package scadwasm_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"scadlab/backend/exporter"
	"scadlab/backend/mesh"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/scadwasm"
	"scadlab/backend/threemf"
	"scadlab/backend/threemf/macetatest"
)

// Ruta del motor integrado de la maceta con relieve (S17, tarea 17.2). El wasm no escribe 3MF:
// pide un OFF con el color de cada cara y ConvertOFFTo3MF lo convierte. Con la unión por
// defecto las dos partes salen como cáscaras abiertas (la causa del hueco relleno en Bambu
// Studio); con --enable=lazy-union salen como sólidos cerrados.
func TestMacetaOFFTo3MF(t *testing.T) {
	s := newScene(t, map[string]string{
		"001-maceta.scad":  macetatest.Scad,
		"002-verde.scad":   macetatest.Only("verde"),
		"003-naranja.scad": macetatest.Only("naranja"),
	})
	convert := func(name string, args ...string) []threemf.Part {
		t.Helper()
		out, code, err := s.run(t, append([]string{"-o", "/out/" + name + ".off", "--backend=manifold"}, args...)...)
		if err != nil || code != 0 {
			t.Fatalf("%s: code=%d err=%v out=%q", name, code, err, out)
		}
		dest := filepath.Join(s.out, name+".3mf")
		if err := scadwasm.ConvertOFFTo3MF(s.outFile(name+".off"), dest, name); err != nil {
			t.Fatal(err)
		}
		m, err := threemf.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		return threemf.Split(m)
	}

	union := convert("union", "001-maceta.scad")
	if len(union) != 2 || threemf.OpenEdgeCount(union) == 0 {
		t.Fatalf("la unión por defecto tendría que dejar dos cáscaras abiertas: %d partes, %d aristas abiertas", len(union), threemf.OpenEdgeCount(union))
	}

	lazy := convert("lazy", "--enable=lazy-union", "001-maceta.scad")
	// El OFF escribe 6 cifras significativas: el volumen se desvía algo más que en el 3MF nativo.
	macetatest.AssertSolids(t, lazy, 2e-3)

	// Contra el STL que da el mismo motor para cada color por separado.
	for i, f := range []string{"002-verde.scad", "003-naranja.scad"} {
		stl := "ref" + f + ".stl"
		if out, code, err := s.run(t, "-o", "/out/"+stl, "--export-format", "binstl", "--backend=manifold", f); err != nil || code != 0 {
			t.Fatalf("%s: code=%d err=%v out=%q", f, code, err, out)
		}
		info, err := mesh.InfoFromFile(s.outFile(stl))
		if err != nil || !info.Watertight {
			t.Fatalf("%s: %+v err=%v", f, info, err)
		}
		c := threemf.Inspect(lazy[i])
		if math.Abs(c.SignedVolume-info.VolumeMM3)/info.VolumeMM3 > 2e-3 {
			t.Errorf("%s: volumen de la parte %.3f, del STL %.3f", f, c.SignedVolume, info.VolumeMM3)
		}
		if !macetatest.Near(c.Min, info.Min, 2e-3) || !macetatest.Near(c.Max, info.Max, 2e-3) {
			t.Errorf("%s: caja de la parte %v–%v, del STL %v–%v", f, c.Min, c.Max, info.Min, info.Max)
		}
	}
}

// De punta a punta con el motor integrado (como si no hubiera OpenSCAD instalado,
// SCADLAB_NO_SYSTEM_OPENSCAD=1): Load3MF entrega partes cerradas y lo exportado también.
func TestMacetaWASMEndToEnd(t *testing.T) {
	s := newScene(t, map[string]string{"001-maceta.scad": macetatest.Scad})
	eng, err := render.NewEngine(filepath.Join(t.TempDir(), "meshes"))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	opts := render.Options{WASM: shared, Backend: "manifold", TimeoutSeconds: 180}
	req := render.Request{WorkspaceKey: "wasm:maceta", WorkspaceDir: s.ws, FileName: "001-maceta.scad"}
	cache := filepath.Join(t.TempDir(), "exports")

	split, res, err := parts.Load3MF(context.Background(), eng, req, opts, cache)
	if err != nil || res.Engine != render.EngineWASM {
		t.Fatalf("Load3MF: engine=%q err=%v", res.Engine, err)
	}
	macetatest.AssertSolids(t, split, 2e-3)

	pr, _ := printers.ByID("bambu-a1-mini")
	prep, err := exporter.Prepare(context.Background(), exporter.Job{Engine: eng, Req: req, Opts: opts, CacheDir: cache, Printer: pr}, exporter.Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "maceta.3mf")
	if err := prep.Write(dest); err != nil {
		t.Fatal(err)
	}
	back, err := threemf.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	checks, shared := threemf.InspectAll(threemf.Split(back))
	if len(checks) != 2 || shared != 0 {
		t.Fatalf("3MF escrito: %d partes, %d triángulos compartidos", len(checks), shared)
	}
	for i, c := range checks {
		if !c.Closed() {
			t.Errorf("parte %d del 3MF escrito: %+v", i, c)
		}
	}
	for _, w := range prep.Warnings() {
		t.Logf("aviso: %s", w)
	}
}
