package printers

import (
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"scadlab/assets"
	"scadlab/backend/slicers"
)

func TestCatalog(t *testing.T) {
	all, err := All()
	if err != nil || len(all) == 0 {
		t.Fatalf("catálogo vacío: %v", err)
	}
	p, ok := ByID("bambu-a1-mini")
	if !ok || p.BuildVolume.X != 180 || p.BuildVolume.Z != 180 {
		t.Errorf("A1 mini: %+v", p)
	}
	for _, p := range all {
		if p.ID == "" || p.BuildVolume.X <= 0 || p.BuildVolume.Y <= 0 || p.BuildVolume.Z <= 0 {
			t.Errorf("impresora incompleta: %+v", p)
		}
	}
	if _, ok := ByID("no-existe"); ok {
		t.Error("ByID de un id inexistente devolvió ok")
	}
}

// Los ids son únicos y cada impresora trae marca, nombre y modelo: es lo que la
// interfaz agrupa y muestra.
func TestEveryPrinterHasABrandAndUniqueID(t *testing.T) {
	all, _ := All()
	brands, err := Brands()
	if err != nil || len(brands) == 0 {
		t.Fatalf("marcas: %v, %v", brands, err)
	}
	known := map[string]Brand{}
	for _, b := range brands {
		if b.ID == "" || b.Name == "" || b.Skill == "" {
			t.Errorf("marca incompleta: %+v", b)
		}
		known[b.ID] = b
	}
	seen := map[string]bool{}
	for _, p := range all {
		if seen[p.ID] {
			t.Errorf("id repetido: %s", p.ID)
		}
		seen[p.ID] = true
		b, ok := known[p.BrandID]
		if !ok {
			t.Errorf("%s: marca %q fuera de Brands()", p.ID, p.BrandID)
			continue
		}
		if p.BrandName != b.Name || p.Skill != b.Skill {
			t.Errorf("%s: marca/skill no vienen del bloque brand: %+v vs %+v", p.ID, p, b)
		}
		if p.Name == "" || p.Model == "" {
			t.Errorf("%s: sin nombre o modelo", p.ID)
		}
		if p.MaxColors < 1 || p.NozzleCount < 1 {
			t.Errorf("%s: MaxColors %d / NozzleCount %d tienen que ser >= 1", p.ID, p.MaxColors, p.NozzleCount)
		}
		if p.DefaultColors < 1 || p.DefaultColors > p.MaxColors {
			t.Errorf("%s: default_colors %d fuera de 1..%d", p.ID, p.DefaultColors, p.MaxColors)
		}
	}
}

// Los slicers que nombra una impresora existen en el registro: un id mal escrito
// dejaría «Abrir en …» sin destino.
func TestPrinterSlicersAreInTheRegistry(t *testing.T) {
	all, _ := All()
	for _, p := range all {
		if len(p.Slicers) == 0 {
			t.Errorf("%s: sin slicers", p.ID)
		}
		for _, id := range p.Slicers {
			if _, ok := slicers.ByID(id); !ok {
				t.Errorf("%s: slicer %q no está en el registro", p.ID, id)
			}
		}
	}
}

// La skill de cada marca existe como skill incluida. Las marcas cuyo archivo
// todavía no está en el binario simplemente no figuran en Brands(): el test solo
// valida las que existen.
func TestBrandSkillsExist(t *testing.T) {
	brands, _ := Brands()
	skills := BrandSkills()
	for _, b := range brands {
		if _, err := fs.Stat(assets.FS, "skills/"+b.Skill+"/SKILL.md"); err != nil {
			t.Errorf("la skill %q de la marca %s no existe: %v", b.Skill, b.ID, err)
		}
		if !skills[b.Skill] {
			t.Errorf("BrandSkills() no incluye %q", b.Skill)
		}
	}
	if len(skills) != len(brands) {
		t.Errorf("BrandSkills() %v vs %d marcas", skills, len(brands))
	}
	if !skills["bambu-lab-printers"] {
		t.Error("la skill de Bambu Lab tiene que ser de marca")
	}
	if skills["openscad-core"] {
		t.Error("openscad-core no es una skill de marca")
	}
}

// Las marcas salen en el orden fijo de la app y las impresoras agrupadas por
// marca, dentro de cada una en el orden de su archivo.
func TestBrandOrderIsFixed(t *testing.T) {
	brands, _ := Brands()
	idx := func(id string) int { return slices.Index(brandOrder, id) }
	last := -1
	for _, b := range brands {
		i := idx(b.ID)
		if i < 0 {
			continue // marca fuera de la lista: va después de las conocidas
		}
		if i < last {
			t.Errorf("la marca %s está fuera de orden: %v", b.ID, brands)
		}
		last = i
	}
	if len(brands) == 0 || brands[0].ID != "bambu" {
		t.Errorf("Bambu Lab va primero: %v", brands)
	}
	all, _ := All()
	var seen []string
	for _, p := range all {
		if len(seen) == 0 || seen[len(seen)-1] != p.BrandID {
			if slices.Contains(seen, p.BrandID) {
				t.Errorf("las impresoras de %s no están juntas", p.BrandID)
			}
			seen = append(seen, p.BrandID)
		}
	}
}

// Los ids de Bambu que ya existían se mantienen: hay workspaces guardados con
// ellos. Los modelos nuevos (A2L, X2D, H2C) se suman.
func TestBambuKeepsItsIDs(t *testing.T) {
	for _, id := range []string{
		"bambu-a1-mini", "bambu-a1", "bambu-p1p", "bambu-p1s", "bambu-p2s",
		"bambu-x1c", "bambu-x1e", "bambu-h2d", "bambu-h2s",
		"bambu-a2l", "bambu-x2d", "bambu-h2c",
	} {
		if p, ok := ByID(id); !ok || p.ID != id || p.BrandID != "bambu" {
			t.Errorf("%s no está en el catálogo de Bambu: %+v", id, p)
		}
	}
}

// Medidas de Bambu con la investigación de S14 (store.bambulab.com y la wiki del
// AMS): el máximo de colores crece con las unidades AMS que admite cada modelo.
func TestBambuMeasures(t *testing.T) {
	cases := []struct {
		id                  string
		x, y, z, noz        float64
		slots, nozzles, def int
		kind                string
	}{
		{"bambu-a1-mini", 180, 180, 180, 0.4, 16, 1, 4, "ams"},
		{"bambu-a1", 256, 256, 256, 0.4, 16, 1, 4, "ams"},
		{"bambu-a2l", 330, 320, 325, 0.4, 19, 1, 4, "ams"},
		{"bambu-p1s", 256, 256, 256, 0.4, 16, 1, 4, "ams"},
		{"bambu-p2s", 256, 256, 256, 0.4, 20, 1, 4, "ams"},
		{"bambu-x2d", 256, 256, 260, 0.4, 25, 2, 4, "ams"},
		{"bambu-h2d", 325, 320, 325, 0.4, 25, 2, 4, "ams"},
		{"bambu-h2s", 340, 320, 340, 0.4, 24, 1, 4, "ams"},
	}
	for _, c := range cases {
		p, ok := ByID(c.id)
		if !ok {
			t.Errorf("%s no está", c.id)
			continue
		}
		if p.BuildVolume != (Volume{c.x, c.y, c.z}) || p.NozzleMM != c.noz || p.MulticolorSlots != c.slots ||
			p.NozzleCount != c.nozzles || p.DefaultColors != c.def || p.MulticolorKind != c.kind {
			t.Errorf("%s cambió: %+v", c.id, p)
		}
		if p.BrandID != "bambu" || p.Skill != "bambu-lab-printers" || p.FilamentDiameterMM != 1.75 || p.MaxColors != c.slots {
			t.Errorf("%s: campos v2: %+v", c.id, p)
		}
		if !slices.Equal(p.Slicers, []string{"bambu-studio", "orcaslicer"}) {
			t.Errorf("%s: slicers %v", c.id, p.Slicers)
		}
	}
}

// Un id que no existe cae en la A1 mini, y Colors recorta a 1..máximo.
func TestFallbackAndColors(t *testing.T) {
	p, ok := ByID("no-existe")
	if ok || p.ID != "bambu-a1-mini" {
		t.Errorf("fallback: %v %+v", ok, p.ID)
	}
	h2d, _ := ByID("bambu-h2d")
	for _, c := range []struct{ in, want int }{{0, 4}, {-3, 4}, {1, 1}, {7, 7}, {25, 25}, {99, 25}} {
		if got := h2d.Colors(c.in); got != c.want {
			t.Errorf("H2D Colors(%d) = %d, esperaba %d", c.in, got, c.want)
		}
	}
	// Sin default_colors vale el máximo; sin máximo, 1.
	if got := (Printer{MaxColors: 5}).Colors(0); got != 5 {
		t.Errorf("sin default: %d", got)
	}
	if got := (Printer{}).Colors(0); got != 1 {
		t.Errorf("sin nada: %d", got)
	}
	if got := (Printer{MaxColors: 1, DefaultColors: 1}).Colors(4); got != 1 {
		t.Errorf("una sola boquilla sin multicolor: %d", got)
	}
}

// --- Catálogo de S14: datos de la investigación (.claude/specs/s14-impresoras-multimarca.md) ---

// Medidas clave de la ficha oficial, en el orden X×Y×Z. La Kobra 2 da su volumen
// como alto × ancho × fondo: acá va al revés (el eje largo es Z).
func TestCatalogKeyMeasures(t *testing.T) {
	cases := []struct {
		id      string
		x, y, z float64
	}{
		{"prusa-mk4s", 250, 210, 220},
		{"prusa-core-one", 250, 220, 270},
		{"prusa-core-one-l", 300, 300, 330},
		{"prusa-mini", 180, 180, 180},
		{"prusa-xl", 360, 360, 360},
		{"creality-ender-3-v3", 220, 220, 250},
		{"creality-ender-3-v3-ke", 220, 220, 240},
		{"creality-k1", 220, 220, 250},
		{"creality-k1-max", 300, 300, 300},
		{"creality-k2-plus", 350, 350, 350},
		{"creality-k2-pro", 300, 300, 300},
		{"creality-k2", 260, 260, 260},
		{"creality-hi", 260, 260, 300},
		{"elegoo-neptune-4", 225, 225, 265},
		{"elegoo-neptune-4-max", 420, 420, 480},
		{"elegoo-centauri-carbon", 256, 256, 256},
		{"anycubic-kobra-3", 250, 250, 260},
		{"anycubic-kobra-s1-max", 350, 350, 350},
		{"anycubic-kobra-2-max", 420, 420, 500},
		{"anycubic-kobra-2-plus", 320, 320, 400},
		{"anycubic-kobra-2-pro", 220, 220, 250},
		{"snapmaker-u1", 270, 270, 270},
		{"snapmaker-j1s", 300, 200, 200},
		{"snapmaker-artisan", 350, 400, 400},
		{"ultimaker-s3", 230, 190, 200},
		{"ultimaker-s5", 330, 240, 300},
		{"ultimaker-s7", 330, 240, 300},
	}
	for _, c := range cases {
		p, ok := ByID(c.id)
		if !ok || p.ID != c.id {
			t.Errorf("%s no está en el catálogo", c.id)
			continue
		}
		if p.BuildVolume != (Volume{c.x, c.y, c.z}) {
			t.Errorf("%s: volumen %+v, la ficha dice %v × %v × %v", c.id, p.BuildVolume, c.x, c.y, c.z)
		}
	}
	// La MK4S, en particular, con su nombre completo en el prompt y la interfaz.
	mk4s, _ := ByID("prusa-mk4s")
	if !strings.Contains(mk4s.Name, "Prusa MK4S") || mk4s.MulticolorKind != "mmu" || mk4s.MulticolorSlots != 5 || mk4s.DefaultColors != 1 ||
		!slices.Equal(mk4s.Slicers, []string{"prusaslicer", "orcaslicer"}) || mk4s.MaxNozzleTempC != 290 || mk4s.MaxBedTempC != 120 {
		t.Errorf("MK4S: %+v", mk4s)
	}
}

// Las UltiMaker usan filamento de 2,85 mm; el resto del catálogo, de 1,75 (o lo
// deja en verify cuando la ficha no lo dice).
func TestFilamentDiameterByBrand(t *testing.T) {
	all, _ := All()
	for _, p := range all {
		want := 1.75
		if p.BrandID == "ultimaker" {
			want = 2.85
		}
		if p.FilamentDiameterMM != want {
			t.Errorf("%s: filamento %v mm, esperaba %v", p.ID, p.FilamentDiameterMM, want)
		}
	}
	if u, _ := ByID("ultimaker-s5"); u.FilamentDiameterMM != 2.85 || u.MulticolorKind != "dual" || u.MaxColors != 2 {
		t.Errorf("S5: %+v", u)
	}
}

// Las siete marcas, en el orden de la app, con sus skills.
func TestSevenBrands(t *testing.T) {
	brands, err := Brands()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bambu", "prusa", "creality", "elegoo", "anycubic", "snapmaker", "ultimaker"}
	var got []string
	for _, b := range brands {
		got = append(got, b.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("marcas %v, esperaba %v", got, want)
	}
	skillOf := map[string]string{
		"bambu": "bambu-lab-printers", "prusa": "prusa-printers", "creality": "creality-printers",
		"elegoo": "elegoo-printers", "anycubic": "anycubic-printers", "snapmaker": "snapmaker-printers", "ultimaker": "ultimaker-printers",
	}
	for _, b := range brands {
		if b.Skill != skillOf[b.ID] {
			t.Errorf("marca %s: skill %q, esperaba %q", b.ID, b.Skill, skillOf[b.ID])
		}
	}
	all, _ := All()
	if len(all) < 47 {
		t.Errorf("el catálogo trae %d impresoras, la investigación dejó 47", len(all))
	}
}

// Toda impresora cita su fuente: sin una URL de la ficha oficial no hay forma de
// confirmar la cifra. Las URLs son https y los ids llevan el prefijo de su marca.
func TestEveryPrinterCitesItsSources(t *testing.T) {
	all, _ := All()
	for _, p := range all {
		if len(p.Sources) == 0 {
			t.Errorf("%s: sin sources", p.ID)
		}
		for _, u := range p.Sources {
			if !strings.HasPrefix(u, "https://") {
				t.Errorf("%s: fuente que no es una URL https: %q", p.ID, u)
			}
		}
		if !strings.HasPrefix(p.ID, p.BrandID+"-") {
			t.Errorf("%s: el id no empieza con la marca %s", p.ID, p.BrandID)
		}
		if p.MaxBedTempC < 0 || p.MaxNozzleTempC < 0 || p.NozzleMM <= 0 {
			t.Errorf("%s: temperaturas o boquilla inválidas: %+v", p.ID, p)
		}
	}
}

// Los sistemas multicolor son los del contrato; una máquina sin sistema es de un
// solo color y una con sistema puede elegir entre 1 y su máximo desde default_colors.
func TestMulticolorKindsAndColorRanges(t *testing.T) {
	known := []string{"ams", "mmu", "toolchanger", "cfs", "ace", "idex", "dual", "none"}
	all, _ := All()
	for _, p := range all {
		if !slices.Contains(known, p.MulticolorKind) {
			t.Errorf("%s: multicolor_kind %q desconocido", p.ID, p.MulticolorKind)
		}
		if p.MulticolorKind == "none" && p.MaxColors != 1 {
			t.Errorf("%s: sin multicolor pero MaxColors = %d", p.ID, p.MaxColors)
		}
		if p.DefaultColors < 1 || p.DefaultColors > p.MaxColors {
			t.Errorf("%s: default_colors %d fuera de 1..%d", p.ID, p.DefaultColors, p.MaxColors)
		}
		if (p.MulticolorKind == "dual" || p.MulticolorKind == "idex") && p.MaxColors != 2 {
			t.Errorf("%s: %s es de dos extrusores, MaxColors = %d", p.ID, p.MulticolorKind, p.MaxColors)
		}
	}
}

// Lo que va en verify son campos reales del esquema: un nombre mal escrito
// escondería la duda en vez de marcarla.
func TestVerifyNamesAreSchemaFields(t *testing.T) {
	fields := []string{
		"build_volume", "nozzle_mm", "nozzle_count", "filament_diameter_mm", "multicolor_kind", "multicolor_system",
		"multicolor_slots", "default_colors", "enclosed", "heated_chamber", "bed_surface", "max_nozzle_temp_c",
		"max_bed_temp_c", "slicers", "build_volume_dual_nozzle", "build_volume_total",
	}
	all, _ := All()
	for _, p := range all {
		for _, v := range p.Verify {
			if !slices.Contains(fields, v) {
				t.Errorf("%s: verify nombra %q, que no es un campo del esquema", p.ID, v)
			}
		}
	}
}

func brandSkillFile(t *testing.T, skill, name string) string {
	t.Helper()
	b, err := fs.ReadFile(assets.FS, "skills/"+skill+"/"+name)
	if err != nil {
		t.Fatalf("skills/%s/%s: %v", skill, name, err)
	}
	return string(b)
}

func volText(v Volume) string {
	f := func(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }
	return f(v.X) + "×" + f(v.Y) + "×" + f(v.Z)
}

// La skill de cada marca documenta TODAS las impresoras de su catálogo: el id y el
// volumen X×Y×Z de su tabla coinciden con el JSON. Es el espejo que impide que la
// tabla de la skill y el catálogo se desincronicen.
func TestBrandSkillsDocumentTheirPrinters(t *testing.T) {
	all, _ := All()
	nameRe := regexp.MustCompile(`(?m)^name:\s*(\S+)\s*$`)
	for _, b := range func() []Brand { bs, _ := Brands(); return bs }() {
		md := brandSkillFile(t, b.Skill, "SKILL.md")
		if m := nameRe.FindStringSubmatch(md); m == nil || m[1] != b.Skill {
			t.Errorf("%s: el frontmatter name no es la carpeta", b.Skill)
		}
		if !strings.Contains(md, "description:") || strings.Count(md, "\n") > 250 {
			t.Errorf("%s: sin description o más de 250 líneas", b.Skill)
		}
		for _, p := range all {
			if p.BrandID != b.ID {
				continue
			}
			row := ""
			for _, line := range strings.Split(md, "\n") {
				if strings.HasPrefix(line, "| `"+p.ID+"` |") {
					row = line
				}
			}
			if row == "" {
				t.Errorf("%s: %s no figura en la tabla de perfiles", b.Skill, p.ID)
				continue
			}
			if !strings.Contains(row, volText(p.BuildVolume)) {
				t.Errorf("%s: la fila de %s no dice %s: %s", b.Skill, p.ID, volText(p.BuildVolume), row)
			}
		}
	}
}

// plate_check.scad de cada marca es espejo del JSON: mismos ids, mismos volúmenes.
func TestPlateCheckMirrorsTheCatalog(t *testing.T) {
	row := regexp.MustCompile(`\["([a-z0-9-]+)",\s*\[([0-9.]+),\s*([0-9.]+),\s*([0-9.]+)\]\]`)
	all, _ := All()
	brands, _ := Brands()
	for _, b := range brands {
		scad := brandSkillFile(t, b.Skill, "references/plate_check.scad")
		got := map[string]Volume{}
		for _, m := range row.FindAllStringSubmatch(scad, -1) {
			x, _ := strconv.ParseFloat(m[2], 64)
			y, _ := strconv.ParseFloat(m[3], 64)
			z, _ := strconv.ParseFloat(m[4], 64)
			got[m[1]] = Volume{x, y, z}
		}
		want := 0
		for _, p := range all {
			if p.BrandID != b.ID {
				continue
			}
			want++
			if v, ok := got[p.ID]; !ok || v != p.BuildVolume {
				t.Errorf("%s: plate_check dice %v para %s, el catálogo %v", b.Skill, v, p.ID, p.BuildVolume)
			}
		}
		if len(got) != want {
			t.Errorf("%s: plate_check trae %d impresoras, el catálogo %d", b.Skill, len(got), want)
		}
		if !strings.Contains(scad, "assert_fits_plate") {
			t.Errorf("%s: plate_check sin assert_fits_plate", b.Skill)
		}
	}
}
