package mcp

import (
	"image/color"
	"math"
	"regexp"
	"strings"
	"testing"

	"scadlab/assets"
	"scadlab/backend/skills"
)

const imageSkill = "openscad-from-image"

// La skill existe, su frontmatter es válido, cada archivo que nombra existe, las
// recetas empiezan con la cabecera de un checkpoint y se descubre como incluida.
func TestFromImageSkillIsValidAndSynced(t *testing.T) {
	raw, err := assets.FS.ReadFile("skills/" + imageSkill + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if ds := skills.ValidateSkillMD(imageSkill, raw); len(ds) != 0 {
		t.Errorf("frontmatter: %+v", ds)
	}
	body := string(raw)
	if n := strings.Count(body, "\n"); n > 250 {
		t.Errorf("SKILL.md tiene %d líneas (máximo ~250: lo extenso va en references/)", n)
	}
	if m := regexp.MustCompile(`(?i)\b(elegí|creá|podés|tenés|usá|mirá|pedí|hacé|fijate|sabés|querés)\b`).FindString(body); m != "" {
		t.Errorf("voseo en la skill: %q", m)
	}
	// Nombra las cuatro herramientas y los límites honestos.
	for _, want := range []string{"image_info", "compare_with_image", "trace_image_outline", "image_to_heightmap", "silhouetteIoU", "supuesto"} {
		if !strings.Contains(body, want) {
			t.Errorf("SKILL.md no menciona %q", want)
		}
	}
	mentioned := regexp.MustCompile(`(?:templates|references)/[a-z0-9-]+\.(?:scad|md)`).FindAllString(body, -1)
	if len(mentioned) < 5 {
		t.Fatalf("SKILL.md enlaza pocos archivos: %v", mentioned)
	}
	for _, f := range mentioned {
		if _, err := assets.FS.ReadFile("skills/" + imageSkill + "/" + f); err != nil {
			t.Errorf("SKILL.md nombra %s y no existe", f)
		}
	}
	ents, err := assets.FS.ReadDir("skills/" + imageSkill + "/templates")
	if err != nil || len(ents) != 3 {
		t.Fatalf("plantillas: %d %v", len(ents), err)
	}
	for _, e := range ents {
		if !strings.Contains(body, "templates/"+e.Name()) {
			t.Errorf("la plantilla %s no aparece en SKILL.md", e.Name())
		}
		src, _ := assets.FS.ReadFile("skills/" + imageSkill + "/templates/" + e.Name())
		if !strings.HasPrefix(string(src), "// @checkpoint parent= desc=\"") {
			t.Errorf("%s: sin cabecera @checkpoint", e.Name())
		}
	}
	// La tabla de medidas dice de dónde sale cada cifra o que está por confirmar.
	sizes, err := assets.FS.ReadFile("skills/" + imageSkill + "/references/standard-sizes.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"USD", "EUR", "MXN", "GTQ", "85,60", "por confirmar", "ECB", "BCE"} {
		if want == "ECB" {
			continue
		}
		if !strings.Contains(string(sizes), want) {
			t.Errorf("standard-sizes.md no menciona %q", want)
		}
	}
	for _, line := range strings.Split(string(sizes), "\n") {
		if strings.HasPrefix(line, "|") && !strings.Contains(line, "---") && strings.Count(line, "|") >= 5 &&
			!strings.Contains(line, "Fuente") && !strings.Contains(line, "Rosca") && !strings.Contains(line, "Pila") &&
			!strings.Contains(line, "Objeto") && !strings.Contains(line, "Conector") {
			if !strings.Contains(line, "✓") && !strings.Contains(line, "por confirmar") && !strings.Contains(line, "| M") {
				t.Errorf("la fila no dice si está confirmada: %s", line)
			}
		}
	}

	list, err := skills.Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var found *skills.Skill
	for i := range list {
		if list[i].Name == imageSkill {
			found = &list[i]
		}
	}
	if found == nil || found.Source != "builtin" || !strings.Contains(found.Description, "refs/images") {
		t.Fatalf("Discover: %+v", found)
	}
	ws := t.TempDir()
	if _, err := skills.Sync(ws, list, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
}

// Las tres recetas se renderizan de verdad (con OpenSCAD o el motor integrado) con
// los archivos que dejan las herramientas, dan las medidas que prometen y pasan el linter.
func TestFromImageSkillTemplatesRender(t *testing.T) {
	tb, ws := newToolbox(t, false)
	needRenderEngine(t, tb)

	// Un logo de 3:2 (la forma ocupa 540×360 de una imagen de 600×400): un disco con un agujero y una barra.
	logo := putImage(t, ws, "logo.png", paintPNG(600, 400, func(x, y int) color.NRGBA {
		switch {
		case math.Hypot(float64(x)-200, float64(y)-200) <= 180 && math.Hypot(float64(x)-200, float64(y)-200) >= 80:
			return cBlack
		case x >= 420 && x < 560 && y >= 40 && y < 360:
			return cBlack
		}
		return cWhite
	}))
	svg := tb.mustJSON(t, "trace_image_outline", map[string]any{"image": logo, "width_mm": 60, "name": "logo-outline"})
	if f := svg["files"].([]any)[0]; f != "refs/logo-outline.svg" {
		t.Fatalf("archivo del logo: %v", f)
	}
	if h := num(t, svg, "sizeMm", "height"); math.Abs(h-40) > 0.6 {
		t.Fatalf("alto del logo %.2f, esperaba ≈ 40 (la plantilla usa logo_h = 40)", h)
	}
	// Un degradado 4:3 para el mapa de alturas (200 × 150 puntos).
	photo := putImage(t, ws, "foto.png", paintPNG(400, 300, func(x, y int) color.NRGBA {
		v := uint8(x * 255 / 399)
		return color.NRGBA{v, v, v, 255}
	}))
	hm := tb.mustJSON(t, "image_to_heightmap", map[string]any{"image": photo, "width_mm": 80, "max_height_mm": 2.4, "invert": true, "name": "photo-heightmap"})
	if hm["file"] != "refs/photo-heightmap.dat" || num(t, hm, "columns") != 200 || num(t, hm, "rows") != 150 || num(t, hm, "sizeMm", "height") != 60 {
		t.Fatalf("mapa: %v", hm)
	}

	cases := []struct {
		name  string
		file  string
		edits [][2]string
		size  [3]float64
		tol   float64
	}{
		{"soporte en L", "bracket-from-photo.scad", nil, [3]float64{480 * 24.26 / 150, 25, 360 * 24.26 / 150}, 0.3},
		{"placa con logo", "plate-from-svg.scad", nil, [3]float64{68, 48, 3.2}, 0.3},
		{"placa con dos capas", "plate-from-svg.scad", [][2]string{
			{`logo_files = ["refs/logo-outline.svg"];`, `logo_files = ["refs/logo-outline.svg", "refs/logo-outline.svg"];`},
			{`logo_colors = ["#1f2937"];`, `logo_colors = ["#1f2937", "#dc2626"];`},
		}, [3]float64{68, 48, 3.2}, 0.3},
		{"litofanía de pie", "lithophane-surface.scad", nil, [3]float64{86, 3, 66}, 0.3},
		{"relieve acostado", "lithophane-surface.scad", [][2]string{{"stand_up = true;", "stand_up = false;"}, {"frame = true;", "frame = false;"}}, [3]float64{80, 60, 3.0}, 0.3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, err := assets.FS.ReadFile("skills/" + imageSkill + "/templates/" + c.file)
			if err != nil {
				t.Fatal(err)
			}
			code := string(src)
			for _, ed := range c.edits {
				if !strings.Contains(code, ed[0]) {
					t.Fatalf("la receta %s ya no tiene %q", c.file, ed[0])
				}
				code = strings.Replace(code, ed[0], ed[1], 1)
			}
			file := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": c.name, "source": code, "root": true})["file"].(string)
			r := tb.mustJSON(t, "render", map[string]any{"file": file})
			if r["status"] != "ok" {
				t.Fatalf("render: %v", r)
			}
			size := r["mesh"].(map[string]any)["sizeMm"].(map[string]any)
			for i, axis := range []string{"x", "y", "z"} {
				if got := size[axis].(float64); math.Abs(got-c.size[i]) > c.tol {
					t.Errorf("tamaño %s = %.2f, esperaba %.2f ± %.1f (%v)", axis, got, c.size[i], c.tol, size)
				}
			}
			if r["mesh"].(map[string]any)["volumeCm3"].(float64) <= 0 {
				t.Errorf("volumen: %v", r["mesh"])
			}
			l := tb.mustJSON(t, "lint", map[string]any{"file": file})
			if l["errors"].(float64) != 0 || l["warnings"].(float64) != 0 {
				t.Errorf("lint: %v", l["diagnostics"])
			}
		})
	}
}

// La plantilla de reglas de cada workspace (para los CLIs) nombra todas las
// herramientas del catálogo, las de imagen incluidas, y explica refs/images/ y la
// skill: un CLI solo se entera de una herramienta por lo que dice ese archivo.
func TestRulesTemplateDescribesTheImageTools(t *testing.T) {
	raw, err := assets.FS.ReadFile("workspace/RULES.md.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	rules := string(raw)
	tb, _ := newToolbox(t, false)
	for _, tool := range tb.Tools() {
		if !strings.Contains(rules, "`"+tool.Name+"`") {
			t.Errorf("RULES.md.tmpl no nombra la herramienta %s", tool.Name)
		}
	}
	for _, want := range []string{"refs/images/", imageSkill, "silhouetteIoU", "20 MB", "no trae medidas"} {
		if !strings.Contains(rules, want) {
			t.Errorf("RULES.md.tmpl no menciona %q", want)
		}
	}
}

// Con la skill sincronizada al workspace, la IA la lee con read_skill: el SKILL.md
// (que lista sus archivos), la tabla de medidas y una plantilla.
func TestFromImageSkillIsReadableWithReadSkill(t *testing.T) {
	tb, ws := newToolbox(t, false)
	list, err := skills.Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skills.Sync(ws, list, func(name string) bool { return name == imageSkill }); err != nil {
		t.Fatal(err)
	}
	if names := tb.mustJSON(t, "list_skills", nil)["skills"].([]any); len(names) != 1 || names[0].(map[string]any)["name"] != imageSkill {
		t.Fatalf("list_skills: %v", names)
	}
	out := tb.must(t, "read_skill", map[string]any{"name": imageSkill})
	for _, want := range []string{"compare_with_image", "references/standard-sizes.md", "templates/plate-from-svg.scad"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("read_skill SKILL.md no trae %q", want)
		}
	}
	sizes := tb.must(t, "read_skill", map[string]any{"name": imageSkill, "file": "references/standard-sizes.md"})
	if !strings.Contains(sizes.Text, "85,60") || !strings.Contains(sizes.Text, "por confirmar") {
		t.Errorf("standard-sizes.md: %.200s", sizes.Text)
	}
	tpl := tb.must(t, "read_skill", map[string]any{"name": imageSkill, "file": "templates/lithophane-surface.scad"})
	if !strings.Contains(tpl.Text, "surface(file = map_file") {
		t.Errorf("plantilla: %.200s", tpl.Text)
	}
}
