package mcp

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/meshedit"
	"scadlab/backend/printers"
)

// stripComments deja el código sin comentarios de línea completa ni líneas
// vacías ni sangría: lo que cuenta de un checkpoint generado.
func stripComments(src string) string {
	var out []string
	for _, l := range strings.Split(src, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

type meshStats struct {
	min, max   [3]float64
	volume     float64
	watertight bool
}

func (tb *Toolbox) stats(t *testing.T, file string) meshStats {
	t.Helper()
	r := tb.mustJSON(t, "render", map[string]any{"file": file})
	if r["status"] != "ok" {
		t.Fatalf("render de %s: %v", file, r)
	}
	m := r["mesh"].(map[string]any)
	vec := func(k string) [3]float64 {
		v := m[k].(map[string]any)
		return [3]float64{v["x"].(float64), v["y"].(float64), v["z"].(float64)}
	}
	return meshStats{min: vec("minMm"), max: vec("maxMm"), volume: m["volumeCm3"].(float64) * 1000, watertight: m["watertight"].(bool)}
}

// importCube deja refs/soporte.stl (cubo de 20×10×5) importado y devuelve el
// checkpoint base.
func importCube(t *testing.T, tb *Toolbox, ws string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "soporte.stl"))
	return tb.mustJSON(t, "import_mesh", map[string]any{"file": "refs/soporte.stl"})["file"].(string)
}

// edit_mesh con un corte y un agujero genera lo mismo que el editor (el mismo
// meshedit.Generate) y renderiza igual, en el motor nativo y en el integrado.
func TestEditMeshMatchesTheEditor(t *testing.T) {
	eachEngine(t, func(t *testing.T, tb *Toolbox, ws string) {
		base := importCube(t, tb, ws)
		got := tb.mustJSON(t, "edit_mesh", map[string]any{
			"file":  base,
			"label": "agujero y corte",
			"operations": []any{
				map[string]any{"op": "center"},
				map[string]any{"op": "hole", "shape": "cylinder", "diameter": 4, "height": 20, "at_rel": []any{0.5, 0.5, 0.5}},
				map[string]any{"op": "cut", "axis": "x", "at": 0, "keep": "above"},
			},
		})
		child := got["file"].(string)
		if got["parent"] != base || got["frozen"] != false || got["ref"] != "refs/soporte.stl" {
			t.Fatalf("edit_mesh: %v", got)
		}
		if m := got["model"].(map[string]any); m["sizeMm"].(map[string]any)["x"] != 20.0 || m["minMm"].(map[string]any)["x"] != -10.0 {
			t.Errorf("caja del modelo: %v", m)
		}

		// La escena que el editor 3D armaría a mano para esa misma edición.
		scene := meshedit.Scene{
			Ref:   "refs/soporte.stl",
			Model: meshedit.Transform{Translate: [3]float64{-10, -5, 0}, Scale: [3]float64{1, 1, 1}},
			Parts: []meshedit.Primitive{{
				ID: "p1", Shape: meshedit.ShapeCylinder, Mode: meshedit.ModeHole, Size: [3]float64{4, 4, 20}, Center: true,
				Transform: meshedit.Transform{Translate: [3]float64{0, 0, 2.5}},
			}},
			Cut:    &meshedit.Cut{Point: [3]float64{0, 0, 2.5}, Normal: [3]float64{1, 0, 0}, Keep: meshedit.KeepAbove},
			Bounds: &meshedit.Bounds{Min: [3]float64{0, 0, 0}, Max: [3]float64{20, 10, 5}},
		}
		want, err := meshedit.Generate(scene, meshedit.Header{Parent: base, Desc: "x"}, "en")
		if err != nil {
			t.Fatal(err)
		}
		src := tb.must(t, "read_checkpoint", map[string]any{"file": child}).Text
		if stripComments(src) != stripComments(want) {
			t.Errorf("el código de edit_mesh no es el del editor:\n--- edit_mesh ---\n%s\n--- editor ---\n%s", src, want)
		}
		if !strings.HasPrefix(src, "// @checkpoint parent="+base+` desc="agujero y corte"`) {
			t.Errorf("cabecera: %q", strings.SplitN(src, "\n", 2)[0])
		}

		// Y renderiza igual que el checkpoint escrito con ese código.
		ref := tb.create(t, "referencia", want, map[string]any{"parent": base})
		a, b := tb.stats(t, child), tb.stats(t, ref)
		if !a.watertight || !near(a.volume, b.volume, 1e-6) || a.min != b.min || a.max != b.max {
			t.Errorf("render distinto: edit_mesh %+v, editor %+v", a, b)
		}
		// La mitad x ≥ 0 del cubo (500 mm³) menos medio agujero de Ø4 × 5 (≈31 mm³).
		if a.volume < 465 || a.volume > 475 || !near(a.min[0], 0, 1e-6) || !near(a.max[0], 10, 1e-6) {
			t.Errorf("volumen o caja inesperados: %+v", a)
		}
	})
}

func TestEditMeshFreezesAnyCheckpoint(t *testing.T) {
	eachEngine(t, func(t *testing.T, tb *Toolbox, ws string) {
		box := tb.create(t, "caja", "color(\"red\") cube([20, 10, 5]);\n", nil)
		got := tb.mustJSON(t, "edit_mesh", map[string]any{"file": box, "operations": []any{
			map[string]any{"op": "orient", "down": "+x"}, map[string]any{"op": "center"}, map[string]any{"op": "to_bed"},
		}})
		if got["frozen"] != true || got["ref"] != "refs/"+strings.TrimSuffix(box, ".scad")+".stl" {
			t.Fatalf("edit_mesh: %v", got)
		}
		if w, _ := got["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "single-color") {
			t.Errorf("debía avisar que se congeló: %v", got["warnings"])
		}
		if _, err := os.Stat(filepath.Join(ws, "refs", strings.TrimSuffix(box, ".scad")+".stl")); err != nil {
			t.Error(err)
		}
		child := got["file"].(string)
		s := tb.stats(t, child)
		// La cara +x (la de 10×5) baja a la placa: queda de 5 × 10 de planta y 20 de alto.
		if !near(s.max[2]-s.min[2], 20, 1e-3) || !near(s.min[2], 0, 1e-3) || !near(s.volume, 1000, 1e-3) {
			t.Errorf("tras orient +x: %+v", s)
		}
		// El original no cambió y la copia congelada se reutiliza.
		if again := tb.mustJSON(t, "edit_mesh", map[string]any{"file": box, "operations": []any{map[string]any{"op": "mirror", "axis": "x"}}}); again["ref"] != got["ref"] {
			t.Errorf("la malla congelada debía reutilizarse: %v / %v", again["ref"], got["ref"])
		}
		if ents, _ := os.ReadDir(filepath.Join(ws, "refs")); len(ents) != 1 {
			t.Errorf("refs/: %v", ents)
		}
		// Un hijo de un edit sigue siendo editable (y ahora es un import base).
		second := tb.mustJSON(t, "edit_mesh", map[string]any{"file": child, "operations": []any{map[string]any{"op": "move", "by": []any{5, 0, 0}}}})
		if second["frozen"] != false || second["ref"] != got["ref"] {
			t.Errorf("segunda edición: %v", second)
		}
	})
}

func TestEditMeshRejectsBadInputWithoutLeavingAnything(t *testing.T) {
	tb, ws := newToolbox(t, false)
	needOpenSCAD(t)
	box := tb.create(t, "caja", "color(\"red\") cube([20, 10, 5]);\n", nil)
	before := scadCount(t, ws)
	bad := []map[string]any{
		{"file": box},
		{"file": box, "operations": []any{}},
		{"file": box, "operations": "no es json"},
		{"file": box, "operations": []any{map[string]any{"op": "explode"}}},
		{"file": box, "operations": []any{map[string]any{"op": "move"}}},
		{"file": box, "operations": []any{map[string]any{"op": "scale", "factor": -2}}},
		{"file": box, "operations": []any{map[string]any{"op": "hole", "shape": "cube"}}},
		{"file": box, "operations": []any{map[string]any{"op": "cut", "axis": "z", "at": 1}}},
		{"file": box, "operations": []any{map[string]any{"op": "move", "by": []any{1, 2}}}},
		{"file": box, "operations": []any{map[string]any{"op": "color", "color": "rojo"}}},
		{"file": "no-existe.scad", "operations": []any{map[string]any{"op": "to_bed"}}},
	}
	for _, f := range []string{"../" + box, "/etc/passwd", "sub/" + box, box + "\x00", "x.txt"} {
		bad = append(bad, map[string]any{"file": f, "operations": []any{map[string]any{"op": "to_bed"}}})
	}
	for _, args := range bad {
		tb.fails(t, "edit_mesh", args)
	}
	if scadCount(t, ws) != before {
		t.Error("una edición inválida creó un checkpoint")
	}
	// La malla congelada para una edición que falló no se queda en refs/.
	if ents, _ := os.ReadDir(filepath.Join(ws, "refs")); len(ents) != 0 {
		t.Errorf("quedaron mallas congeladas de ediciones fallidas: %v", ents)
	}
}

// openBoxSTL escribe un cubo sin la tapa de arriba (una malla abierta).
func openBoxSTL(t *testing.T, path string) {
	t.Helper()
	v := [][3]float64{{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0}, {0, 0, 10}, {10, 0, 10}, {10, 10, 10}, {0, 10, 10}}
	tr := [][3]int{{0, 3, 2}, {0, 2, 1}, {0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2}, {0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5}}
	var b bytes.Buffer
	b.WriteString("solid o\n")
	for _, t3 := range tr {
		b.WriteString("facet normal 0 0 0\n outer loop\n")
		for _, i := range t3 {
			b.WriteString("  vertex " + ftoa(v[i][0]) + " " + ftoa(v[i][1]) + " " + ftoa(v[i][2]) + "\n")
		}
		b.WriteString(" endloop\nendfacet\n")
	}
	b.WriteString("endsolid o\n")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEditMeshOnAnOpenMesh(t *testing.T) {
	tb, ws := newToolbox(t, false)
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	openBoxSTL(t, filepath.Join(ws, "refs", "abierta.stl"))
	imp := tb.mustJSON(t, "import_mesh", map[string]any{"file": "abierta.stl"})
	if w, _ := imp["warnings"].([]any); len(w) == 0 {
		t.Error("import_mesh debía avisar de la malla abierta")
	}
	base := imp["file"].(string)
	// Mover, escalar y espejar sí se puede.
	tb.mustJSON(t, "edit_mesh", map[string]any{"file": base, "operations": []any{map[string]any{"op": "move", "by": []any{1, 2, 3}}, map[string]any{"op": "scale", "factor": 2}}})
	// Perforar o cortar, no: sería un «mesh is not closed».
	before := scadCount(t, ws)
	out := tb.fails(t, "edit_mesh", map[string]any{"file": base, "operations": []any{map[string]any{"op": "hole", "shape": "sphere", "diameter": 2}}})
	if !strings.Contains(out.Text, "watertight") {
		t.Errorf("mensaje: %q", out.Text)
	}
	tb.fails(t, "edit_mesh", map[string]any{"file": base, "operations": []any{map[string]any{"op": "cut", "axis": "z", "at_rel": 0.5, "keep": "above"}}})
	if scadCount(t, ws) != before {
		t.Error("se creó un checkpoint que no se puede renderizar")
	}
}

// --- suggest_orientation -------------------------------------------------------------

func TestRankOrientationsPrefersWhatFitsStanding(t *testing.T) {
	// Una placa alta y angosta: 100 × 100 × 300. Una pieza de 20 × 150 × 20 no cabe
	// acostada (150 > 100) pero sí parada.
	plate := printers.Volume{X: 100, Y: 100, Z: 300}
	size := [3]float64{20, 150, 20}
	cands := rankOrientations(size, plate, nil, nil)
	if len(cands) != 6 {
		t.Fatalf("seis caras: %d", len(cands))
	}
	if !cands[0].Fits || cands[0].Down[1] != 'y' || cands[0].SizeMM.Z != 150 {
		t.Errorf("la primera debía ser parada sobre una cara en Y: %+v", cands[0])
	}
	for _, c := range cands {
		wantFit := c.Down[1] == 'y'
		if c.Fits != wantFit {
			t.Errorf("%s: fits=%v, quería %v (%+v)", c.Down, c.Fits, wantFit, c.SizeMM)
		}
	}
	// El orden: las que caben primero.
	seenNoFit := false
	for _, c := range cands {
		if !c.Fits {
			seenNoFit = true
		} else if seenNoFit {
			t.Errorf("una que cabe va después de una que no: %+v", cands)
		}
	}
	// Una placa que no es cuadrada: girar 90° en Z hace caber una que de otro modo no.
	wide := printers.Volume{X: 300, Y: 100, Z: 100}
	for _, c := range rankOrientations([3]float64{50, 250, 20}, wide, nil, nil) {
		if c.Down == "-z" && (!c.Fits || !c.NeedsZSpin || c.SizeMM.X != 250) {
			t.Errorf("-z con giro de 90°: %+v", c)
		}
	}
	// A igual ajuste manda el voladizo, y a igual voladizo la altura.
	over := map[string]float64{"-z": 300, "+z": 0, "-x": 0, "+x": 0, "-y": 0, "+y": 0}
	cands = rankOrientations([3]float64{40, 10, 40}, printers.Volume{X: 180, Y: 180, Z: 180}, over, nil)
	if cands[0].Down == "-z" || cands[len(cands)-1].Down != "-z" {
		t.Errorf("el voladizo manda: %+v", cands)
	}
}

func TestFaceAreasOfAnLShape(t *testing.T) {
	// Una L: la barra de 40×10×10 con un poste de 10×10×40 sobre su extremo.
	eachEngine(t, func(t *testing.T, tb *Toolbox, ws string) {
		l := tb.create(t, "ele", "color(\"gray\") union() { cube([40, 10, 10]); cube([10, 10, 40]); }\n", nil)
		got := tb.mustJSON(t, "suggest_orientation", map[string]any{"file": l})
		by := map[string]map[string]any{}
		for _, o := range got["orientations"].([]any) {
			m := o.(map[string]any)
			by[m["down"].(string)] = m
		}
		// Apoyada en la barra (-z): nada en voladizo y 400 mm² de contacto.
		if oh := by["-z"]["overhangAreaMm2"].(float64); oh > 1 {
			t.Errorf("-z no tiene voladizo: %v", oh)
		}
		if c := by["-z"]["contactAreaMm2"].(float64); !near(c, 400, 1) {
			t.Errorf("contacto de -z: %v", c)
		}
		// Boca abajo (+z): el lado de la barra que mira abajo (30×10) queda en el aire.
		if oh := by["+z"]["overhangAreaMm2"].(float64); !near(oh, 300, 1) {
			t.Errorf("voladizo de +z: %v", oh)
		}
		if c := by["+z"]["contactAreaMm2"].(float64); !near(c, 100, 1) {
			t.Errorf("contacto de +z: %v", c)
		}
		if got["recommended"] == "+z" {
			t.Error("no debía recomendar la que tiene voladizo")
		}
		first := got["orientations"].([]any)[0].(map[string]any)
		if first["fits"] != true || got["fitsInSomeOrientation"] != true {
			t.Errorf("primera: %v", first)
		}
		ops := first["operations"].([]any)
		if ops[0].(map[string]any)["op"] != "orient" || ops[len(ops)-1].(map[string]any)["op"] != "to_bed" {
			t.Errorf("operaciones listas para edit_mesh: %v", ops)
		}
	})
}

func TestSuggestOrientationOnATallPiece(t *testing.T) {
	// En la H2D (325 × 320 × 325) una pieza de 10 × 322 × 30 no cabe como está (322
	// > 320) pero sí en otra orientación; y la sugerencia se aplica con edit_mesh.
	eachEngine(t, func(t *testing.T, tb *Toolbox, ws string) {
		if err := os.WriteFile(filepath.Join(ws, "printer.json"), []byte(`{"printer":"bambu-h2d"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		piece := tb.create(t, "larga", "color(\"red\") cube([10, 322, 30]);\n", nil)
		if m := tb.mustJSON(t, "measure", map[string]any{"file": piece}); m["fitsBuildVolume"] != false {
			t.Fatalf("la pieza no debería caber como está: %v", m["fitsBuildVolume"])
		}
		got := tb.mustJSON(t, "suggest_orientation", map[string]any{"file": piece})
		if got["currentFits"] != false || got["fitsInSomeOrientation"] != true || got["recommended"] == nil {
			t.Fatalf("suggest_orientation: %v", got)
		}
		best := got["orientations"].([]any)[0].(map[string]any)
		// Se aplica con edit_mesh y la pieza queda dentro de la placa.
		ed := tb.mustJSON(t, "edit_mesh", map[string]any{"file": piece, "operations": best["operations"]})
		if ed["fitsBuildVolume"] != true {
			t.Errorf("la orientación sugerida no cabe: %v (%v)", ed["model"], best)
		}
		child := ed["file"].(string)
		if m := tb.mustJSON(t, "measure", map[string]any{"file": child}); m["fitsBuildVolume"] != true {
			t.Errorf("measure del hijo: %v", m)
		}
	})
}

func TestSuggestOrientationWhenNothingFits(t *testing.T) {
	needOpenSCAD(t)
	tb, _ := newToolbox(t, false) // A1 mini: 180 × 180 × 180
	big := tb.create(t, "grande", "color(\"red\") cube([200, 50, 50]);\n", nil)
	got := tb.mustJSON(t, "suggest_orientation", map[string]any{"file": big})
	if got["fitsInSomeOrientation"] != false || got["recommended"] != nil {
		t.Fatalf("no debía caber: %v", got)
	}
	sp := got["split"].(map[string]any)
	if sp["overflowAxis"] != "x" && sp["overflowAxis"] != "y" && sp["overflowAxis"] != "z" {
		t.Errorf("eje: %v", sp)
	}
	if sp["piecesAlongAxis"] != 2.0 || !near(sp["maxUniformScale"].(float64), 0.9, 1e-3) {
		t.Errorf("partir en 2 o escalar a 0.9: %v", sp)
	}
	cut := sp["firstCutOperation"].(map[string]any)
	if cut["op"] != "cut" || cut["keep"] != "both" || cut["at_rel"] != 0.5 {
		t.Errorf("corte: %v", cut)
	}
	// El corte sugerido se aplica con edit_mesh, y cada mitad cabe.
	ops := append(sp["orientOperations"].([]any), cut)
	ed := tb.mustJSON(t, "edit_mesh", map[string]any{"file": big, "operations": ops})
	child := ed["file"].(string)
	r := tb.mustJSON(t, "render", map[string]any{"file": child})
	if r["status"] != "ok" || r["mesh"].(map[string]any)["watertight"] != true {
		t.Errorf("render de las dos mitades: %v", r)
	}
}

// --- impresión: estimate_print y list_materials -------------------------------------

func TestEstimatePrintAndListMaterials(t *testing.T) {
	needOpenSCAD(t)
	tb, _, st, _ := registeredToolbox(t)
	_ = st
	mats := tb.mustJSON(t, "list_materials", nil)
	list := mats["materials"].([]any)
	if len(list) < 3 || mats["defaultMaterial"] == "" {
		t.Fatalf("list_materials: %v", mats)
	}
	ids := map[string]float64{}
	var def string
	for _, m := range list {
		mm := m.(map[string]any)
		ids[mm["id"].(string)] = mm["densityGCm3"].(float64)
		if mm["default"] == true {
			def = mm["id"].(string)
		}
	}
	if def != mats["defaultMaterial"] || ids["pla"] == 0 || ids["petg"] == 0 {
		t.Errorf("materiales: %v", ids)
	}

	// Una pieza maciza grande: con 100 % de relleno pesa volumen × densidad.
	box := tb.create(t, "bloque", "color(\"red\") cube([40, 40, 40]);\n", nil)
	full := tb.mustJSON(t, "estimate_print", map[string]any{"file": box, "material": "pla", "infill_percent": 100})
	if w := full["estimatedWeightG"].(float64); !near(w, 64*ids["pla"], 0.05) {
		t.Errorf("40 cm³ de PLA al 100 %%: %v g, esperaba %v", w, 64*ids["pla"])
	}
	petg := tb.mustJSON(t, "estimate_print", map[string]any{"file": box, "material": "PETG", "infill_percent": 100})
	if w := petg["estimatedWeightG"].(float64); !near(w, 64*ids["petg"], 0.05) || petg["material"].(map[string]any)["id"] != "petg" {
		t.Errorf("PETG: %v", petg)
	}
	hollow := tb.mustJSON(t, "estimate_print", map[string]any{"file": box, "infill_percent": "0", "wall_loops": 2})
	if hollow["estimatedWeightG"].(float64) >= full["estimatedWeightG"].(float64) {
		t.Errorf("sin relleno debía pesar menos: %v vs %v", hollow["estimatedWeightG"], full["estimatedWeightG"])
	}
	est := hollow["estimate"].(map[string]any)
	if est["infillPercent"] != 0.0 || est["wallLoops"] != 2.0 || hollow["filamentM"] == nil || hollow["cost"] == nil {
		t.Errorf("parámetros usados: %v", est)
	}
	// measure sigue usando la configuración de la app (15 % por defecto).
	if m := tb.mustJSON(t, "measure", map[string]any{"file": box, "verbose": true}); m["estimate"].(map[string]any)["infillPercent"] != 15.0 {
		t.Errorf("measure: %v", m["estimate"])
	}
	for _, args := range []map[string]any{
		{"file": box, "material": "unobtanio"}, {"file": box, "infill_percent": 101}, {"file": box, "infill_percent": -1},
		{"file": box, "infill_percent": "mucho"}, {"file": box, "wall_loops": 0}, {"file": box, "wall_loops": 99}, {"file": "../x.scad"},
	} {
		tb.fails(t, "estimate_print", args)
	}
}

// --- preview con ángulo libre y export png ------------------------------------------

func TestPreviewFreeCameraAndExportPNG(t *testing.T) {
	eachEngine(t, func(t *testing.T, tb *Toolbox, ws string) {
		name := tb.create(t, "dos colores", twoColors, nil)
		pv := tb.must(t, "preview", map[string]any{"file": name, "rotate": []any{60, 0, 30}, "size": 300})
		if len(pv.Images) != 1 || !isPNG(pv.Images[0].Data) || !strings.Contains(pv.Text, "custom-60_0_30") {
			t.Fatalf("preview con rotate: %q %d", pv.Text, len(pv.Images))
		}
		// Un rotate como texto JSON y el ortogonal también.
		tb.must(t, "preview", map[string]any{"file": name, "rotate": "[10, 20, 30]", "ortho": true})
		for _, bad := range []any{[]any{1, 2}, []any{"a", 0, 0}, []any{0, 0, 99999}, "no"} {
			tb.fails(t, "preview", map[string]any{"file": name, "rotate": bad})
		}
		tb.fails(t, "preview", map[string]any{"file": name, "view": "arriba"})

		// export png: a exports/, con la vista en el nombre.
		ex := tb.mustJSON(t, "export", map[string]any{"file": name, "format": "png", "view": "front", "size": 400})
		path := ex["path"].(string)
		if ex["format"] != "png" || !strings.HasSuffix(path, "-front.png") || filepath.Dir(path) != mustReal(t, filepath.Join(ws, "exports")) {
			t.Fatalf("export png: %v", ex)
		}
		raw, err := os.ReadFile(path)
		if err != nil || !isPNG(raw) || int(ex["bytes"].(float64)) != len(raw) {
			t.Errorf("el PNG exportado: %v", err)
		}
		other := tb.mustJSON(t, "export", map[string]any{"file": name, "format": "png", "rotate": []any{45, 0, 45}})
		if other["path"] == path || !strings.HasSuffix(other["path"].(string), "-custom-45_0_45.png") {
			t.Errorf("otra vista no debe pisar la anterior: %v", other["path"])
		}
		if ents, _ := os.ReadDir(filepath.Join(ws, "exports")); len(ents) != 2 {
			t.Errorf("exports/: %v", ents)
		}
		// Sin carpeta exports afuera ni temporales de más.
		for _, e := range mustReadDir(t, filepath.Join(ws, "exports")) {
			if strings.HasPrefix(e, ".") {
				t.Errorf("temporal sin borrar: %s", e)
			}
		}
		tb.fails(t, "export", map[string]any{"file": name, "format": "png", "view": "secreta"})
		tb.fails(t, "export", map[string]any{"file": "../x.scad", "format": "png"})
	})
}

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// --- por --mcp ---------------------------------------------------------------------

// Las herramientas nuevas funcionan por el servidor `--mcp` (JSON-RPC), no solo en
// proceso: la ven los CLIs.
func TestNewToolsOverMCP(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	a := c.create("caja", paramsModel, nil)

	if g := c.mustOK("get_parameters", map[string]any{"file": a}); g["count"].(float64) != 5 {
		t.Errorf("get_parameters: %v", g)
	}
	set := c.mustOK("set_parameters", map[string]any{"file": a, "values": map[string]any{"width": 33}})
	child := set["file"].(string)
	if r := c.mustOK("render", map[string]any{"file": child}); r["mesh"].(map[string]any)["sizeMm"].(map[string]any)["x"] != 33.0 {
		t.Errorf("render del hijo: %v", r["mesh"])
	}
	tpl := c.mustOK("create_from_template", map[string]any{"name": "cable-clip"})
	if tpl["template"] != "cable-clip" {
		t.Errorf("create_from_template: %v", tpl)
	}
	if mats := c.mustOK("list_materials", nil); len(mats["materials"].([]any)) == 0 {
		t.Errorf("list_materials: %v", mats)
	}
	if libs := c.mustOK("list_libraries", nil); len(libs["libraries"].([]any)) != 3 {
		t.Errorf("list_libraries: %v", libs)
	}
	if refs := c.mustOK("list_refs", nil); refs["refs"] == nil {
		t.Errorf("list_refs: %v", refs)
	}
	if o := c.mustOK("suggest_orientation", map[string]any{"file": a}); o["fitsInSomeOrientation"] != true {
		t.Errorf("suggest_orientation: %v", o)
	}
	em := c.mustOK("edit_mesh", map[string]any{"file": a, "operations": []any{map[string]any{"op": "rotate", "degrees": []any{0, 0, 90}}, map[string]any{"op": "to_bed"}}})
	if em["frozen"] != true {
		t.Errorf("edit_mesh: %v", em)
	}
	// set_active_parent / trash / restore por JSON-RPC.
	if r := c.mustOK("set_active_parent", map[string]any{"file": a}); r["activeParent"] != a {
		t.Errorf("set_active_parent: %v", r)
	}
	tr := c.mustOK("trash_checkpoint", map[string]any{"file": child})
	trashed := tr["trashed"].([]any)[0].(string)
	if c.exists(child) {
		t.Error("trash_checkpoint no movió el archivo")
	}
	if r := c.mustOK("restore_checkpoint", map[string]any{"trashed_name": trashed}); r["file"] != child {
		t.Errorf("restore: %v", r)
	}
	// Las skills del workspace, por --mcp.
	writeSkill(t, c.Ws, "demo", "---\nname: demo\ndescription: una skill de prueba\n---\n# Demo\n", nil)
	if s := c.mustOK("list_skills", nil); len(s["skills"].([]any)) != 1 {
		t.Errorf("list_skills: %v", s)
	}
	if r := c.tool("read_skill", map[string]any{"name": "demo"}); r.IsError || !strings.Contains(r.Text, "# Demo") {
		t.Errorf("read_skill: %+v", r)
	}
	// inspect_reference devuelve imagen + texto.
	if err := os.MkdirAll(filepath.Join(c.Ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(c.Ws, "refs", "p.stl"))
	if r := c.tool("inspect_reference", map[string]any{"file": "p.stl"}); r.IsError || len(r.Content) != 2 || r.Content[0]["type"] != "image" {
		t.Errorf("inspect_reference: %+v", r)
	}
}
