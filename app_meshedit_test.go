package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/meshedit"
	"scadlab/backend/meshserve"
	"scadlab/backend/render"
	"scadlab/backend/store"
	"scadlab/backend/threemf"
)

// --- ayudas ---------------------------------------------------------------------

func asciiCube(x, y, z float64) string {
	v := [][3]float64{{0, 0, 0}, {x, 0, 0}, {x, y, 0}, {0, y, 0}, {0, 0, z}, {x, 0, z}, {x, y, z}, {0, y, z}}
	tr := [][3]int{{0, 3, 2}, {0, 2, 1}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2}, {0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5}}
	var b strings.Builder
	b.WriteString("solid cubo\n")
	for _, t := range tr {
		b.WriteString("facet normal 0 0 0\n outer loop\n")
		for _, i := range t {
			fmt.Fprintf(&b, "  vertex %g %g %g\n", v[i][0], v[i][1], v[i][2])
		}
		b.WriteString(" endloop\nendfacet\n")
	}
	b.WriteString("endsolid cubo\n")
	return b.String()
}

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func scadFiles(t *testing.T, ws string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(ws, "*.scad"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func refFiles(t *testing.T, ws string) []string {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Join(ws, "refs"))
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// --- importar (no necesita OpenSCAD) ----------------------------------------------

func TestImportModelSTL3MFOBJ(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Importar", "bambu-a1-mini") // placa de 180 mm
	if err != nil {
		t.Fatal(err)
	}

	// STL ASCII: se analiza antes y se importa centrado.
	stl := writeTemp(t, "Mi Pieza Ñ.stl", asciiCube(10, 20, 30))
	rep, err := a.AnalyzeModelFile(stl)
	if err != nil || !rep.Watertight || rep.Size != [3]float64{10, 20, 30} || rep.Triangles != 12 {
		t.Fatalf("AnalyzeModelFile: %+v %v", rep, err)
	}
	if len(refFiles(t, w.Path)) != 0 {
		t.Fatal("analizar no debe copiar nada")
	}
	res, err := a.ImportModel(w.ID, stl, ImportOptions{CenterOnBed: true, Label: "Soporte"})
	if err != nil {
		t.Fatal(err)
	}
	if res.File != "001-soporte.scad" || res.Ref != "refs/mi-pieza-n.stl" || len(res.Warnings) != 0 || res.Report.Size != rep.Size {
		t.Fatalf("resultado: %+v", res)
	}
	src, _ := os.ReadFile(filepath.Join(w.Path, res.File))
	if !strings.HasPrefix(string(src), "// @checkpoint parent= desc=\"Importa Mi Pieza Ñ.stl\"\n") || !strings.Contains(string(src), "translate([-5, -10, 0])") {
		t.Errorf("checkpoint base:\n%s", src)
	}
	if !a.checkpoints().own[markKey(w.ID, res.File)] {
		t.Error("el checkpoint importado no se marcó como propio de la app")
	}
	if got, _ := os.ReadFile(stl); string(got) != asciiCube(10, 20, 30) {
		t.Error("el archivo original cambió")
	}

	// 3MF de dos colores: se convierte a STL y se avisa que se juntan.
	boxV := func(x0, x1, z1 float64) ([][3]float64, [][3]int32) {
		v := [][3]float64{{x0, 0, 0}, {x1, 0, 0}, {x1, 10, 0}, {x0, 10, 0}, {x0, 0, z1}, {x1, 0, z1}, {x1, 10, z1}, {x0, 10, z1}}
		tr := [][3]int32{{0, 3, 2}, {0, 2, 1}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2}, {0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5}}
		return v, tr
	}
	v1, t1 := boxV(0, 10, 10)
	v2, t2 := boxV(20, 30, 5)
	three := filepath.Join(t.TempDir(), "dos colores.3mf")
	if err := threemf.WriteFile(three, []threemf.Part{
		{Name: "Color 1", Color: threemf.RGB{0xFF, 0, 0}, Extruder: 1, Vertices: v1, Triangles: t1},
		{Name: "Color 2", Color: threemf.RGB{0, 0, 0xFF}, Extruder: 2, Vertices: v2, Triangles: t2},
	}, threemf.WriteOptions{Title: "dos"}); err != nil {
		t.Fatal(err)
	}
	r3, err := a.ImportModel(w.ID, three, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r3.Ref != "refs/dos-colores.stl" || len(r3.Report.Parts) != 2 || !strings.Contains(strings.Join(r3.Warnings, "\n"), "2 colores") {
		t.Fatalf("3MF: %+v", r3)
	}
	files := strings.Join(refFiles(t, w.Path), ",")
	if !strings.Contains(files, "dos-colores.3mf") || !strings.Contains(files, "dos-colores.stl") {
		t.Errorf("refs/ debe tener el original y el convertido: %s", files)
	}

	// OBJ y una malla abierta (sin la tapa de arriba) que además no cabe en la placa.
	obj := writeTemp(t, "grande.obj", "v 0 0 0\nv 300 0 0\nv 300 20 0\nv 0 20 0\nv 0 0 30\nv 300 0 30\nv 300 20 30\nv 0 20 30\nf 1 4 3 2\nf 1 2 6 5\nf 4 8 7 3\nf 1 5 8 4\nf 2 3 7 6\n")
	ro, err := a.ImportModel(w.ID, obj, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(ro.Warnings, "\n")
	for _, want := range []string{"no es cerrada", "no cabe en la placa de Bambu Lab A1 mini"} {
		if !strings.Contains(joined, want) {
			t.Errorf("falta %q en los avisos: %s", want, joined)
		}
	}
	if ro.Ref != "refs/grande.stl" {
		t.Errorf("OBJ: %+v", ro)
	}
	if n := len(scadFiles(t, w.Path)); n != 3 {
		t.Errorf("hay %d checkpoints, esperaba 3", n)
	}

	// Un archivo que no es un modelo no deja nada en refs/ ni crea checkpoints.
	before := refFiles(t, w.Path)
	for _, bad := range []string{
		writeTemp(t, "roto.stl", "solid x\nfacet"),
		writeTemp(t, "vacio.stl", ""),
		writeTemp(t, "nota.txt", "hola"),
		filepath.Join(t.TempDir(), "no-existe.stl"),
	} {
		if r, err := a.ImportModel(w.ID, bad, ImportOptions{}); err == nil {
			t.Errorf("%s debía fallar: %+v", filepath.Base(bad), r)
		}
	}
	if after := refFiles(t, w.Path); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("refs/ cambió tras importar archivos inválidos: %v → %v", before, after)
	}
	if n := len(scadFiles(t, w.Path)); n != 3 {
		t.Errorf("se crearon checkpoints de más: %d", n)
	}
}

// --- editar de punta a punta, con OpenSCAD real ------------------------------------

type meshRig struct {
	name string
	new  func(t *testing.T) (*App, store.Workspace)
}

func meshRigs() []meshRig {
	return []meshRig{
		{"native", func(t *testing.T) (*App, store.Workspace) {
			if _, err := exec.LookPath("openscad"); err != nil {
				t.Skip("OpenSCAD no instalado")
			}
			a := newTestApp(t)
			w, err := a.CreateWorkspace("Editar", "bambu-a1-mini")
			if err != nil {
				t.Fatal(err)
			}
			return a, w
		}},
		{"wasm", func(t *testing.T) (*App, store.Workspace) {
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
			w, err := a.CreateWorkspace("Editar", "bambu-a1-mini")
			if err != nil {
				t.Fatal(err)
			}
			return a, w
		}},
	}
}

func TestMeshEditEndToEnd(t *testing.T) {
	for _, rig := range meshRigs() {
		t.Run(rig.name, func(t *testing.T) {
			a, w := rig.new(t)
			meshOf := func(file string) mesh.Info {
				t.Helper()
				res, err := a.RenderCheckpoint(w.ID, file)
				if err != nil || res.Status != render.StatusOK {
					t.Fatalf("render %s: %+v %v", file, res, err)
				}
				info, err := a.GetMeshInfo(res.MeshPath)
				if err != nil {
					t.Fatal(err)
				}
				return info
			}
			near := func(got, want [3]float64) bool {
				for i := range got {
					if d := got[i] - want[i]; d > 1e-3 || d < -1e-3 {
						return false
					}
				}
				return true
			}

			// 1. Importar un STL ASCII (el visor solo lee binario) centrado en la placa.
			stl := writeTemp(t, "cubo.stl", asciiCube(10, 20, 30))
			imp, err := a.ImportModel(w.ID, stl, ImportOptions{CenterOnBed: true, Label: "cubo"})
			if err != nil || imp.File != "001-cubo.scad" {
				t.Fatalf("import: %+v %v", imp, err)
			}
			if info := meshOf(imp.File); !near(info.Min, [3]float64{-5, -10, 0}) || !near(info.Max, [3]float64{5, 10, 30}) || !info.Watertight {
				t.Fatalf("base centrada: %+v", info)
			}

			// 2. Preparar: es un import base, así que la base es el STL original con su pose.
			base, err := a.PrepareMeshEdit(w.ID, imp.File)
			if err != nil {
				t.Fatal(err)
			}
			if !base.FromImport || base.Ref != "refs/cubo.stl" || base.Parent != imp.File ||
				base.Model.Translate != [3]float64{-5, -10, 0} || base.Model.Scale != [3]float64{1, 1, 1} {
				t.Fatalf("base del import: %+v", base)
			}
			// MeshPath: la malla CRUDA (sin la pose), en la caché del visor y binaria.
			if filepath.Dir(base.MeshPath) != filepath.Join(a.dataDir, "cache", "meshes") || !meshserve.ValidName(filepath.Base(base.MeshPath)) {
				t.Fatalf("MeshPath no sirve para /mesh/: %q", base.MeshPath)
			}
			raw, _ := os.ReadFile(base.MeshPath)
			if len(raw) != 84+50*12 {
				t.Errorf("la malla del visor no es un STL binario de 12 triángulos: %d bytes", len(raw))
			}
			if info, err := a.GetMeshInfo(base.MeshPath); err != nil || !near(info.Min, [3]float64{0, 0, 0}) || !near(info.Max, [3]float64{10, 20, 30}) {
				t.Errorf("MeshPath debe ser la malla sin transformar: %+v %v", info, err)
			}
			if again, _ := a.PrepareMeshEdit(w.ID, imp.File); again.MeshPath != base.MeshPath {
				t.Error("preparar dos veces debe dar la misma malla")
			}

			// 3. Vista previa del código: no escribe nada.
			scene := meshedit.Scene{
				Ref: base.Ref, Model: base.Model, Color: "#3366cc",
				Parts: []meshedit.Primitive{{
					Shape: meshedit.ShapeCylinder, Mode: meshedit.ModeHole, Size: [3]float64{4, 4, 40}, Center: true,
					Transform: meshedit.Transform{Translate: [3]float64{0, 0, 15}},
				}},
				Cut: &meshedit.Cut{Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: meshedit.KeepBelow},
				Ops: []string{"Agujero pasante Ø4", "Cortar a la mitad"},
			}
			before := len(scadFiles(t, w.Path))
			code, err := a.PreviewMeshEdit(w.ID, scene)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"cylinder(d = 4, h = 40, center = true, $fn = 32)", "pieza(1);", "module semiespacio", `import("refs/cubo.stl", convexity = 10)`, "//   2. Cortar a la mitad", `color("#3366cc")`} {
				if !strings.Contains(code, want) {
					t.Errorf("falta %q en la vista previa:\n%s", want, code)
				}
			}
			if len(scadFiles(t, w.Path)) != before {
				t.Error("la vista previa escribió un checkpoint")
			}

			// 4. Aplicar: checkpoint hijo, renderizable, con el agujero y el corte.
			edit, err := a.ApplyMeshEdit(w.ID, imp.File, "Agujero y corte", scene)
			if err != nil || edit != "002-agujero-y-corte.scad" {
				t.Fatalf("ApplyMeshEdit: %q %v", edit, err)
			}
			text, _ := os.ReadFile(filepath.Join(w.Path, edit))
			if !strings.HasPrefix(string(text), "// @checkpoint parent=001-cubo.scad desc=\"Agujero y corte\"\n") {
				t.Errorf("cabecera:\n%s", text)
			}
			if !a.checkpoints().own[markKey(w.ID, edit)] {
				t.Error("el hijo no se marcó como propio de la app")
			}
			// Se queda lo de abajo del plano z = 15 (de 0 a 15), centrado, con el agujero Ø4.
			info := meshOf(edit)
			if !near(info.Min, [3]float64{-5, -10, 0}) || !near(info.Max, [3]float64{5, 10, 15}) || !info.Watertight {
				t.Fatalf("resultado: %+v", info)
			}
			hole := 0.5 * 32 * 4 * 0.19509032201612825 // área del polígono de 32 lados de radio 2
			if want := 10*20*15 - hole*15; info.VolumeMM3 < want-0.2 || info.VolumeMM3 > want+0.2 {
				t.Errorf("volumen = %.2f, esperaba %.2f (con el agujero)", info.VolumeMM3, want)
			}
			// El STL de refs/ no cambió y el padre sigue ahí.
			if got, _ := os.ReadFile(stl); string(got) != asciiCube(10, 20, 30) {
				t.Error("el original cambió")
			}
			if _, err := os.Stat(filepath.Join(w.Path, imp.File)); err != nil {
				t.Error("el padre desapareció")
			}

			// 5. Editar un diseño que NO es un import: su malla se congela en refs/.
			frozen, err := a.PrepareMeshEdit(w.ID, edit)
			if err != nil {
				t.Fatal(err)
			}
			if frozen.FromImport || frozen.Ref != "refs/002-agujero-y-corte.stl" || frozen.Parent != edit ||
				frozen.Model.Translate != [3]float64{} || frozen.Model.Scale != [3]float64{1, 1, 1} || frozen.Color != "" {
				t.Fatalf("base congelada: %+v", frozen)
			}
			if !near(frozen.Report.Size, [3]float64{10, 20, 15}) || !frozen.Report.Watertight {
				t.Errorf("informe de la base congelada: %+v", frozen.Report)
			}
			res, _ := a.RenderCheckpoint(w.ID, edit)
			if frozen.MeshPath != res.MeshPath {
				t.Errorf("MeshPath = %q, la malla que ya tiene el visor es %q", frozen.MeshPath, res.MeshPath)
			}
			refsBefore := refFiles(t, w.Path)
			again, err := a.PrepareMeshEdit(w.ID, edit)
			if err != nil || again.Ref != frozen.Ref {
				t.Fatalf("segunda vez: %+v %v", again, err)
			}
			if strings.Join(refFiles(t, w.Path), ",") != strings.Join(refsBefore, ",") {
				t.Error("congelar dos veces la misma malla creó otra copia")
			}
			// La copia congelada es idéntica a la malla del render.
			c1, _ := os.ReadFile(filepath.Join(w.Path, frozen.Ref))
			c2, _ := os.ReadFile(res.MeshPath)
			if !bytes.Equal(c1, c2) {
				t.Error("la malla congelada no es idéntica a la del render")
			}

			// 6. Editar el congelado: moverlo y espejarlo.
			moved := meshedit.Scene{Ref: frozen.Ref, Model: meshedit.Transform{
				Translate: [3]float64{100, 0, 0}, Mirror: [3]bool{true, false, false}, Scale: [3]float64{1, 1, 1},
			}}
			edit2, err := a.ApplyMeshEdit(w.ID, edit, "", moved)
			if err != nil || edit2 != "003-stl-edit.scad" {
				t.Fatalf("segunda edición: %q %v", edit2, err)
			}
			// Espejo en X de [-5,5] → [-5,5]; +100 → [95,105].
			if info := meshOf(edit2); !near(info.Min, [3]float64{95, -10, 0}) || !near(info.Max, [3]float64{105, 10, 15}) {
				t.Errorf("movido y espejado: %+v", info)
			}

			// 7. Una edición que solo mueve el modelo vuelve a ser un import base.
			base2, err := a.PrepareMeshEdit(w.ID, edit2)
			if err != nil || !base2.FromImport || base2.Ref != frozen.Ref || base2.Model.Translate != [3]float64{100, 0, 0} || !base2.Model.Mirror[0] {
				t.Errorf("una edición solo de pose debe seguir siendo editable desde su STL: %+v %v", base2, err)
			}
		})
	}
}

// Un checkpoint que no renderiza no se puede editar, y el error lo dice. Las
// entradas hostiles no escriben nada.
func TestMeshEditRejections(t *testing.T) {
	i18n.SetLang("en")
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Rechazos", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	stl := writeTemp(t, "cubo.stl", asciiCube(10, 20, 30))
	imp, err := a.ImportModel(w.ID, stl, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scene := meshedit.Scene{Ref: imp.Ref}
	files := len(scadFiles(t, w.Path))

	// Nombres de checkpoint inválidos.
	for _, bad := range []string{"", "../001-cubo.scad", "refs/cubo.stl", "sub/001-cubo.scad", "no-existe.scad", "001-cubo.scad\x00"} {
		if _, err := a.PrepareMeshEdit(w.ID, bad); err == nil {
			t.Errorf("PrepareMeshEdit(%q) debía fallar", bad)
		}
		if _, err := a.ApplyMeshEdit(w.ID, bad, "x", scene); err == nil {
			t.Errorf("ApplyMeshEdit(parent=%q) debía fallar", bad)
		}
	}
	// Referencias hostiles o escenas inválidas.
	outside := writeTemp(t, "afuera.stl", asciiCube(1, 1, 1))
	for _, ref := range []string{"", "../afuera.stl", outside, "refs/../001-cubo.scad", "refs/no-existe.stl", "refs/sub/x.stl", `refs\cubo.stl`} {
		bad := meshedit.Scene{Ref: ref}
		if _, err := a.ApplyMeshEdit(w.ID, imp.File, "x", bad); err == nil {
			t.Errorf("ref=%q debía rechazarse", ref)
		}
		if _, err := a.PreviewMeshEdit(w.ID, bad); err == nil {
			t.Errorf("preview con ref=%q debía rechazarse", ref)
		}
	}
	for name, s := range map[string]meshedit.Scene{
		"color inválido":   {Ref: imp.Ref, Color: "rojo"},
		"escala negativa":  {Ref: imp.Ref, Model: meshedit.Transform{Scale: [3]float64{-1, 1, 1}}},
		"corte sin normal": {Ref: imp.Ref, Cut: &meshedit.Cut{Keep: meshedit.KeepAbove}},
		"cubo de tamaño 0": {Ref: imp.Ref, Parts: []meshedit.Primitive{{Shape: meshedit.ShapeCube, Mode: meshedit.ModeUnion}}},
	} {
		if _, err := a.ApplyMeshEdit(w.ID, imp.File, "x", s); err == nil {
			t.Errorf("%s: debía rechazarse", name)
		}
	}
	// Workspace inexistente.
	if _, err := a.ApplyMeshEdit(9999, imp.File, "x", scene); err == nil {
		t.Error("workspace inexistente")
	}
	if n := len(scadFiles(t, w.Path)); n != files {
		t.Errorf("las entradas inválidas escribieron checkpoints: %d → %d", files, n)
	}

	// Un diseño que no compila no se puede editar: el error trae el motivo.
	broken := "002-roto.scad"
	if err := os.WriteFile(filepath.Join(w.Path, broken), []byte("// @checkpoint parent=001-cubo.scad desc=\"roto\"\ncube(;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("openscad"); err == nil {
		if _, err := a.PrepareMeshEdit(w.ID, broken); err == nil || !strings.Contains(err.Error(), "cannot be rendered") {
			t.Errorf("un checkpoint roto debía explicar por qué no se edita: %v", err)
		}
		if got := refFiles(t, w.Path); len(got) != 1 {
			t.Errorf("refs/ no debe cambiar si no se pudo congelar: %v", got)
		}
	}
}
