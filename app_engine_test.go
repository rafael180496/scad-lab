package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"scadlab/backend/detect"
	"scadlab/backend/estimate"
	"scadlab/backend/exporter"
	"scadlab/backend/render"
	"scadlab/backend/scadwasm"
)

// Estas pruebas corren la App SIN OpenSCAD (SCADLAB_NO_SYSTEM_OPENSCAD=1 y un
// directorio de datos vacío): visor, partes, estimación, exportación y linter
// salen del motor integrado (OpenSCAD en WebAssembly).

func wasmRig(t *testing.T) (*App, int64, string) {
	t.Helper()
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	t.Setenv("SCADLAB_DATA_DIR", t.TempDir())
	a := newTestApp(t)
	t.Cleanup(func() {
		wasmRunners.Lock()
		r := wasmRunners.m[a.dataDir]
		delete(wasmRunners.m, a.dataDir)
		wasmRunners.Unlock()
		_ = r.Close()
	})
	w, err := a.CreateWorkspace("Mi Placa", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	name := "001-dos.scad"
	body := "// @checkpoint parent= desc=\"dos colores\"\ncolor(\"red\") cube(10);\ntranslate([20,0,0]) color(\"blue\") cube(10);\n"
	if err := os.WriteFile(filepath.Join(w.Path, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, w.ID, name
}

func TestAppRendersWithBuiltInEngine(t *testing.T) {
	a, id, name := wasmRig(t)
	if got := a.openscadPath(""); got != "" {
		t.Fatalf("la prueba necesita no tener OpenSCAD: %q", got)
	}
	res, err := a.RenderCheckpoint(id, name)
	if err != nil || res.Status != render.StatusOK || res.Engine != render.EngineWASM || res.Version != scadwasm.Version {
		t.Fatalf("render: %+v err=%v", res, err)
	}

	// Partes por color y estimación de peso y costo (3MF del integrado).
	m, err := a.GetModelParts(id, name)
	if err != nil || len(m.Parts) != 2 || m.Parts[0].Color != "#FF0000" || m.Parts[1].Color != "#0000FF" {
		t.Fatalf("partes: %+v err=%v", m.Parts, err)
	}
	est, err := a.EstimatePrint(id, name, estimate.Options{})
	if err != nil || est.Total.MassG <= 0 || est.Total.Cost <= 0 || len(est.Parts) != 2 {
		t.Fatalf("estimación: %+v err=%v", est, err)
	}

	// Exportación: se escribe el 3MF y el STL por color con una parte por color.
	out := t.TempDir()
	for _, c := range []struct{ format, file string }{
		{exporter.FormatSTL, "salida.stl"},
		{exporter.Format3MF, "salida.3mf"},
		{exporter.FormatSTLPerColor, "salida.zip"},
	} {
		r, err := a.exportCheckpoint(id, name, c.format, func(dir, suggested, format string) (string, error) {
			return filepath.Join(out, c.file), nil
		})
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		if c.format != exporter.FormatSTL && len(r.Parts) != 2 {
			t.Errorf("%s: partes %+v", c.format, r.Parts)
		}
		if st, err := os.Stat(filepath.Join(out, c.file)); err != nil || st.Size() == 0 {
			t.Errorf("%s: no se escribió: %v", c.format, err)
		}
	}
	if zr, err := zip.OpenReader(filepath.Join(out, "salida.3mf")); err != nil {
		t.Errorf("3MF: %v", err)
	} else {
		zr.Close()
	}

	// Linter: error de sintaxis con línea, por el integrado.
	ds, err := a.LintCheckpointSource(id, name, "color(\"red\")\ncube(1\n")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range ds {
		if d.Severity == "error" && d.Source == "openscad" && d.Line >= 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("lint sin error de OpenSCAD: %+v", ds)
	}

	// Un error de sintaxis llega con su línea al resultado del render.
	bad := "002-rota.scad"
	w, _ := a.store.GetWorkspace(id)
	if err := os.WriteFile(filepath.Join(w.Path, bad), []byte("// @checkpoint parent=001-dos.scad desc=\"rota\"\ncube(1);\ncube(;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := a.RenderCheckpoint(id, bad)
	if err != nil || r.Status != render.StatusError || len(r.Diagnostics) == 0 || r.Diagnostics[0].Line != 3 {
		t.Fatalf("render roto: %+v err=%v", r, err)
	}
	if _, err := os.Stat(filepath.Join(w.Path, "RENDER_ERRORS.md")); err != nil {
		t.Errorf("falta RENDER_ERRORS.md: %v", err)
	}
}
