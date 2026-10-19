package meshedit

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"scadlab/assets"
	"scadlab/backend/skills"
)

const stlSkill = "openscad-stl-editing"

// La skill existe, su frontmatter es válido (sin errores ni avisos), cada archivo
// que nombra existe y se sincroniza a las cuatro carpetas de los CLIs como las demás.
func TestSTLEditingSkillIsValidAndSynced(t *testing.T) {
	raw, err := assets.FS.ReadFile("skills/" + stlSkill + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if ds := skills.ValidateSkillMD(stlSkill, raw); len(ds) != 0 {
		t.Errorf("frontmatter: %+v", ds)
	}
	body := string(raw)
	if n := strings.Count(body, "\n"); n > 250 {
		t.Errorf("SKILL.md tiene %d líneas (máximo ~250: lo extenso va en references/)", n)
	}
	// Español con tú: nada de voseo en el texto de la skill.
	if m := regexp.MustCompile(`(?i)\b(elegí|creá|podés|tenés|usá|mirá|pedí|hacé|fijate)\b`).FindString(body); m != "" {
		t.Errorf("voseo en la skill: %q", m)
	}

	// Todo archivo que SKILL.md nombra existe, y toda receta de templates/ está en la tabla.
	mentioned := regexp.MustCompile("(?:templates|references)/[a-z0-9-]+\\.(?:scad|md)").FindAllString(body, -1)
	if len(mentioned) == 0 {
		t.Fatal("SKILL.md no enlaza ninguna receta")
	}
	for _, f := range mentioned {
		if _, err := assets.FS.ReadFile("skills/" + stlSkill + "/" + f); err != nil {
			t.Errorf("SKILL.md nombra %s y no existe", f)
		}
	}
	ents, err := assets.FS.ReadDir("skills/" + stlSkill + "/templates")
	if err != nil || len(ents) < 8 {
		t.Fatalf("recetas: %d %v", len(ents), err)
	}
	for _, e := range ents {
		if !strings.Contains(body, "templates/"+e.Name()) && !strings.Contains(body, e.Name()) {
			t.Errorf("la receta %s no aparece en SKILL.md", e.Name())
		}
	}
	// Las recetas empiezan con la cabecera de un checkpoint.
	for _, e := range ents {
		src, _ := assets.FS.ReadFile("skills/" + stlSkill + "/templates/" + e.Name())
		if !strings.HasPrefix(string(src), "// @checkpoint parent= desc=\"") {
			t.Errorf("%s: sin cabecera @checkpoint", e.Name())
		}
	}

	// Se descubre como skill incluida y se instala en cada carpeta de CLI.
	list, err := skills.Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var found *skills.Skill
	for i := range list {
		if list[i].Name == stlSkill {
			found = &list[i]
		}
	}
	if found == nil || found.Source != "builtin" || !strings.Contains(found.Description, "STL") {
		t.Fatalf("Discover: %+v", found)
	}
	ws := t.TempDir()
	if err := skills.Sync(ws, list, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	for _, dir := range skills.AgentDirs {
		for _, rel := range []string{"SKILL.md", "templates/through-hole.scad", "templates/split-for-bed.scad", "references/limits.md"} {
			if _, err := os.Stat(filepath.Join(ws, dir, stlSkill, filepath.FromSlash(rel))); err != nil {
				t.Errorf("%s: falta %s", dir, rel)
			}
		}
	}
}

func polyArea(r float64, n int) float64 {
	return 0.5 * float64(n) * r * r * math.Sin(2*math.Pi/float64(n))
}

type recipe struct {
	name       string
	file       string
	edits      [][2]string // texto → reemplazo, para probar las variantes de los parámetros
	min, max   [3]float64
	volume     float64
	volTol     float64
	minVol     float64 // alternativa a volume: un rango
	maxVol     float64
	watertight bool
	colors     int // partes por color esperadas (0 = no se comprueba)
}

func recipes() []recipe {
	box := func(x, y, z float64) [3]float64 { return [3]float64{x, y, z} }
	return []recipe{
		{name: "agujero pasante", file: "through-hole.scad", min: box(0, 0, 0), max: box(40, 30, 20),
			volume: 24000 - polyArea(1.7, 27)*20, volTol: 1, watertight: true},
		{name: "avellanado", file: "countersink-m3.scad", min: box(0, 0, 0), max: box(40, 30, 20),
			minVol: 24000 - 212, maxVol: 24000 - 188, watertight: true},
		{name: "caja para la cabeza", file: "countersink-m3.scad", edits: [][2]string{{`head_style = "countersunk";`, `head_style = "counterbore";`}},
			min: box(0, 0, 0), max: box(40, 30, 20), minVol: 24000 - 280, maxVol: 24000 - 250, watertight: true},
		{name: "recorte en Z", file: "plane-cut.scad", min: box(0, 0, 12), max: box(40, 30, 20), volume: 40 * 30 * 8, volTol: 1, watertight: true},
		{name: "recorte en X, lado bajo", file: "plane-cut.scad",
			edits: [][2]string{{`cut_axis = "z";`, `cut_axis = "x";`}, {`cut_at = 12;`, `cut_at = 15;`}, {`keep = "high";`, `keep = "low";`}},
			min:   box(0, 0, 0), max: box(15, 30, 20), volume: 15 * 30 * 20, volTol: 1, watertight: true},
		{name: "recorte en Y, lado alto", file: "plane-cut.scad",
			edits: [][2]string{{`cut_axis = "z";`, `cut_axis = "y";`}, {`cut_at = 12;`, `cut_at = 10;`}},
			min:   box(0, 10, 0), max: box(40, 30, 20), volume: 40 * 20 * 20, volTol: 1, watertight: true},
		{name: "plano oblicuo por el centro", file: "plane-cut-oblique.scad", min: box(10, 0, 0), max: box(40, 30, 20),
			volume: 12000, volTol: 6, watertight: true},
		{name: "plano oblicuo (1,1,1) lado de abajo", file: "plane-cut-oblique.scad",
			edits: [][2]string{{`cut_normal = [1, 0, 1];`, `cut_normal = [1, 1, 1];`}, {`keep = "above";`, `keep = "below";`}},
			min:   box(0, 0, 0), max: box(40, 30, 20), volume: 12000, volTol: 6, watertight: true},
		{name: "plano con normal hacia -Z", file: "plane-cut-oblique.scad",
			edits: [][2]string{{`cut_normal = [1, 0, 1];`, `cut_normal = [0, 0, -1];`}},
			min:   box(0, 0, 0), max: box(40, 30, 10), volume: 12000, volTol: 1, watertight: true},
		{name: "costilla con hull", file: "hull-rib.scad", min: box(0, 0, 0), max: box(40, 30, 32),
			minVol: 24000 + 400, maxVol: 24000 + 800, watertight: true},
		{name: "texto grabado", file: "text-on-face.scad", min: box(0, 0, 0), max: box(40, 30, 20),
			minVol: 24000 - 200, maxVol: 24000 - 5, watertight: true, colors: 1},
		{name: "texto en relieve", file: "text-on-face.scad", edits: [][2]string{{`mode = "engrave";`, `mode = "emboss";`}},
			min: box(0, 0, 0), max: box(40, 30, 20.6), minVol: 24000 + 5, maxVol: 24000 + 200, colors: 2},
		{name: "escalar a 60 mm de ancho", file: "scale-to-size.scad", min: box(0, 0, 0), max: box(60, 45, 30),
			volume: 24000 * 3.375, volTol: 5, watertight: true},
		{name: "estirar solo X", file: "scale-to-size.scad", edits: [][2]string{{`mode = "uniform";`, `mode = "x-only";`}},
			min: box(0, 0, 0), max: box(60, 30, 20), volume: 36000, volTol: 5, watertight: true},
		{name: "partir para la placa", file: "split-for-bed.scad", min: box(0, 0, 0), max: box(90, 30, 10),
			volume: 24000, volTol: 5, watertight: true},
		{name: "par espejado en X", file: "mirror-pair.scad", min: box(0, 0, 0), max: box(90, 30, 20),
			volume: 48000, volTol: 5, watertight: true},
		{name: "par espejado en Y", file: "mirror-pair.scad", edits: [][2]string{{`mirror_axis = "x";`, `mirror_axis = "y";`}},
			min: box(0, 0, 0), max: box(40, 70, 20), volume: 48000, volTol: 5, watertight: true},
	}
}

// Cada receta de la skill se renderiza de verdad —nativo y WebAssembly— contra un
// STL de 40 × 30 × 20 y da lo que promete: caja, volumen, malla cerrada, colores
// y el linter sin avisos.
func TestSTLEditingRecipesRender(t *testing.T) {
	for _, e := range engines(t) {
		for _, r := range recipes() {
			t.Run(e.name+"/"+r.name, func(t *testing.T) {
				src, err := assets.FS.ReadFile("skills/" + stlSkill + "/templates/" + r.file)
				if err != nil {
					t.Fatal(err)
				}
				code := string(src)
				for _, ed := range r.edits {
					if !strings.Contains(code, ed[0]) {
						t.Fatalf("la receta %s ya no tiene %q", r.file, ed[0])
					}
					code = strings.Replace(code, ed[0], ed[1], 1)
				}
				ws := t.TempDir()
				cubeSTL(t, filepath.Join(ws, "refs", "part.stl"), 0, 0, 0, 40, 30, 20)
				if err := os.WriteFile(filepath.Join(ws, "001-base.scad"), []byte("// @checkpoint parent= desc=\"base\"\ncolor(\"gray\") cube(1);\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				res := renderSource(t, ws, e, code)
				if !nearVec(res.info.Min, r.min, 2e-3) || !nearVec(res.info.Max, r.max, 2e-3) {
					t.Errorf("caja = %v … %v, esperaba %v … %v", res.info.Min, res.info.Max, r.min, r.max)
				}
				if r.volume > 0 && !near(res.info.VolumeMM3, r.volume, r.volTol) {
					t.Errorf("volumen = %.2f, esperaba %.2f ± %.2f", res.info.VolumeMM3, r.volume, r.volTol)
				}
				if r.maxVol > 0 && (res.info.VolumeMM3 < r.minVol || res.info.VolumeMM3 > r.maxVol) {
					t.Errorf("volumen = %.2f, esperaba entre %.0f y %.0f", res.info.VolumeMM3, r.minVol, r.maxVol)
				}
				if r.watertight && !res.info.Watertight {
					t.Error("la malla no es cerrada")
				}
				if r.colors > 0 {
					if got, _ := colorsOf(t, ws, e, code); len(got) != r.colors {
						t.Errorf("colores = %v, esperaba %d", got, r.colors)
					}
				}
				lintClean(t, ws, e, code)
			})
		}
	}
}

// Lo que dice la skill de las mallas abiertas es lo que hace OpenSCAD: importar
// sola funciona, una operación booleana falla y deja el modelo vacío.
func TestOpenMeshBooleansFail(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			ws := t.TempDir()
			openSTL(t, filepath.Join(ws, "refs", "abierta.stl"))
			plain := renderSource(t, ws, e, "// @checkpoint parent= desc=\"solo importa\"\ncolor(\"gray\") translate([1, 2, 3]) import(\"refs/abierta.stl\");\n")
			if plain.info.Triangles != 10 {
				t.Errorf("importar sola: %+v", plain.info)
			}
			res := tryRender(t, ws, e, "// @checkpoint parent= desc=\"con agujero\"\ncolor(\"gray\") difference() { import(\"refs/abierta.stl\"); translate([5, 10, -1]) cylinder(d = 4, h = 40, $fn = 16); }\n")
			if res.Status == "ok" {
				t.Fatalf("restar de una malla abierta no debía renderizar: %+v", res)
			}
			joined := ""
			for _, d := range res.Diagnostics {
				joined += d.Message + "\n"
			}
			if !strings.Contains(joined, "not closed") && !strings.Contains(joined, "empty") {
				t.Errorf("el motivo no es el de la skill: %q", joined)
			}
		})
	}
}
