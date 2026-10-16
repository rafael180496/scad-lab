package estimate

import (
	"math"
	"strings"
	"testing"

	"scadlab/backend/i18n"
)

var mats = []Material{
	{ID: "pla", Name: "PLA", DensityGCm3: 1.24, CostPerKg: 20},
	{ID: "petg", Name: "PETG", DensityGCm3: 1.27, CostPerKg: 22},
	{ID: "abs", Name: "ABS", DensityGCm3: 1.04, CostPerKg: 22},
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, quería %v (±%v)", name, got, want, tol)
	}
}

// cubo de 20 mm: volumen 8000, área 2400.
var cube = Part{Index: 0, VolumeMm3: 8000, AreaMm2: 2400}

func opts(infill float64) Options {
	return Options{InfillPercent: infill, WallLoops: 2, LineWidthMm: 0.42, FilamentDiameterMm: 1.75, DefaultMaterialID: "pla", Currency: "USD"}
}

// Números de mano del spec: pared 0,84 → shell 2016; con 15 %: 2016 + 5984×0,15
// = 2913,6 mm³; PLA 1,24 → 3,61 g.
func TestCubeByHand(t *testing.T) {
	r, err := Estimate([]Part{cube}, mats, opts(15))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Parts[0]
	near(t, "efectivo", p.EffectiveMm3, 2913.6, 1e-9)
	near(t, "masa", p.MassG, 2913.6/1000*1.24, 1e-9)
	near(t, "masa (spec)", p.MassG, 3.61, 0.005)
	// largo = 2913,6 / (π × 0,875²) mm = 1211,4 mm
	near(t, "largo", p.LengthM, 2913.6/(math.Pi*0.875*0.875)/1000, 1e-9)
	near(t, "largo (mano)", p.LengthM, 1.2114, 0.0005)
	near(t, "costo", p.Cost, p.MassG/1000*20, 1e-12)
	if p.MaterialID != "pla" || p.VolumeMm3 != 8000 || p.Index != 0 {
		t.Errorf("parte: %+v", p)
	}
	near(t, "total masa", r.Total.MassG, p.MassG, 1e-12)
	near(t, "total largo", r.Total.LengthM, p.LengthM, 1e-12)
	near(t, "total costo", r.Total.Cost, p.Cost, 1e-12)
	if r.Currency != "USD" {
		t.Errorf("moneda %q", r.Currency)
	}
}

func TestInfillZeroAndHundred(t *testing.T) {
	r0, err := Estimate([]Part{cube}, mats, opts(0))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "relleno 0: solo cáscara", r0.Parts[0].EffectiveMm3, 2016, 1e-9)

	r100, err := Estimate([]Part{cube}, mats, opts(100))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "relleno 100: macizo", r100.Parts[0].EffectiveMm3, 8000, 1e-9)
	near(t, "masa macizo", r100.Parts[0].MassG, 8*1.24, 1e-9)
}

// Una lámina de 0,5 mm: la pared (0,84) ocupa más que el volumen. Sale maciza
// sin importar el relleno y nunca pasa del volumen.
func TestThinPartIsSolid(t *testing.T) {
	thin := Part{Index: 0, VolumeMm3: 100 * 100 * 0.5, AreaMm2: 2*100*100 + 4*100*0.5}
	for _, infill := range []float64{0, 15, 100} {
		r, err := Estimate([]Part{thin}, mats, opts(infill))
		if err != nil {
			t.Fatal(err)
		}
		near(t, "efectivo de la lámina", r.Parts[0].EffectiveMm3, thin.VolumeMm3, 1e-9)
	}
}

func TestSeveralPartsDifferentMaterials(t *testing.T) {
	small := Part{Index: 1, VolumeMm3: 1000, AreaMm2: 600} // cubo de 10 mm
	o := opts(20)
	o.PartMaterials = map[int]string{1: "abs"}
	r, err := Estimate([]Part{cube, small}, mats, o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Parts[0].MaterialID != "pla" || r.Parts[1].MaterialID != "abs" {
		t.Fatalf("materiales: %+v", r.Parts)
	}
	// parte 1: shell = min(1000, 600×0,84 = 504) → 504 + 496×0,2 = 603,2 mm³ de ABS (1,04)
	near(t, "efectivo parte 1", r.Parts[1].EffectiveMm3, 603.2, 1e-9)
	near(t, "masa parte 1", r.Parts[1].MassG, 0.6032*1.04, 1e-9)
	near(t, "costo parte 1", r.Parts[1].Cost, 0.6032*1.04/1000*22, 1e-12)
	near(t, "total masa", r.Total.MassG, r.Parts[0].MassG+r.Parts[1].MassG, 1e-12)
	near(t, "total costo", r.Total.Cost, r.Parts[0].Cost+r.Parts[1].Cost, 1e-12)
	near(t, "total largo", r.Total.LengthM, r.Parts[0].LengthM+r.Parts[1].LengthM, 1e-12)
}

func TestUnknownMaterialIsAClearError(t *testing.T) {
	o := opts(15)
	o.DefaultMaterialID = "unobtanium"
	_, err := Estimate([]Part{cube}, mats, o)
	if err == nil || !strings.Contains(err.Error(), "unobtanium") {
		t.Fatalf("quería un error que nombre el material, vino %v", err)
	}
	// También un material propio de una parte.
	o = opts(15)
	o.PartMaterials = map[int]string{0: "nope"}
	if _, err := Estimate([]Part{cube}, mats, o); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("material de parte desconocido: %v", err)
	}
	// Densidad cero = no sirve para estimar.
	if _, err := Estimate([]Part{cube}, []Material{{ID: "pla", DensityGCm3: 0}}, opts(15)); err == nil {
		t.Error("densidad 0: quería error")
	}
	// Sin material elegido.
	o = opts(15)
	o.DefaultMaterialID = ""
	if _, err := Estimate([]Part{cube}, mats, o); err == nil {
		t.Error("sin material: quería error")
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, infill := range []float64{-1, 100.5, math.NaN()} {
		if _, err := Estimate([]Part{cube}, mats, opts(infill)); err == nil {
			t.Errorf("relleno %v: quería error", infill)
		}
	}
}

func TestDefaultsForZeroFields(t *testing.T) {
	// Perímetros, ancho y diámetro en cero se leen como los defaults.
	r, err := Estimate([]Part{cube}, mats, Options{InfillPercent: 15, DefaultMaterialID: "pla"})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "efectivo con defaults", r.Parts[0].EffectiveMm3, 2913.6, 1e-9)
	if r.Currency != "USD" {
		t.Errorf("moneda por defecto %q", r.Currency)
	}
}

func TestFilamentDiameterChangesLengthOnly(t *testing.T) {
	a, _ := Estimate([]Part{cube}, mats, opts(15))
	o := opts(15)
	o.FilamentDiameterMm = 2.85
	b, _ := Estimate([]Part{cube}, mats, o)
	near(t, "misma masa", b.Parts[0].MassG, a.Parts[0].MassG, 1e-12)
	near(t, "largo ∝ 1/d²", b.Parts[0].LengthM, a.Parts[0].LengthM*(1.75*1.75)/(2.85*2.85), 1e-9)
}

func TestAssumptionsInBothLanguages(t *testing.T) {
	defer i18n.SetLang("en")
	for _, lang := range []string{"es", "en"} {
		i18n.SetLang(lang)
		r, err := Estimate([]Part{{Index: 0, VolumeMm3: 8000, AreaMm2: 2400, OpenMesh: true}}, mats, opts(12.5))
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Assumptions) != 5 {
			t.Fatalf("%s: %d supuestos, quería 5 (la malla abierta suma uno)", lang, len(r.Assumptions))
		}
		all := strings.Join(r.Assumptions, "\n")
		for _, want := range []string{"12.5", "0.84", "1.75"} {
			if !strings.Contains(all, want) {
				t.Errorf("%s: los supuestos no mencionan %s:\n%s", lang, want, all)
			}
		}
		if lang == "es" {
			for _, voseo := range []string{"Elegí", "Revisá", "podés"} {
				if strings.Contains(all, voseo) {
					t.Errorf("voseo: %s", voseo)
				}
			}
			if !strings.Contains(all, "estimación") {
				t.Error("el supuesto de estimación no está en español")
			}
		} else if !strings.Contains(all, "estimate") {
			t.Error("el supuesto de estimación no está en inglés")
		}
	}
	r, _ := Estimate([]Part{cube}, mats, opts(15))
	if len(r.Assumptions) != 4 {
		t.Errorf("sin malla abierta: %d supuestos, quería 4", len(r.Assumptions))
	}
}

type fakeSettings struct {
	InfillPercent   float64 `json:"infillPercent"`
	DefaultMaterial string  `json:"defaultMaterial"`
}
type fullSettings struct {
	fakeSettings
	WallLoops          int     `json:"wallLoops"`
	LineWidthMm        float64 `json:"lineWidthMm"`
	FilamentDiameterMm float64 `json:"filamentDiameterMm"`
	Currency           string  `json:"currency"`
}

func TestOptionsFromSettings(t *testing.T) {
	// Sin los settings nuevos: defaults seguros.
	o := OptionsFromSettings(fakeSettings{InfillPercent: 20, DefaultMaterial: "petg"})
	if o.InfillPercent != 20 || o.DefaultMaterialID != "petg" || o.WallLoops != 2 || o.LineWidthMm != 0.42 || o.FilamentDiameterMm != 1.75 || o.Currency != "USD" {
		t.Errorf("defaults: %+v", o)
	}
	// Relleno 0 en settings es 0, no el default.
	if o := OptionsFromSettings(fakeSettings{InfillPercent: 0, DefaultMaterial: "pla"}); o.InfillPercent != 0 {
		t.Errorf("relleno 0: %v", o.InfillPercent)
	}
	// Con ellos.
	o = OptionsFromSettings(fullSettings{fakeSettings{15, "abs"}, 3, 0.4, 2.85, "EUR"})
	if o.WallLoops != 3 || o.LineWidthMm != 0.4 || o.FilamentDiameterMm != 2.85 || o.Currency != "EUR" || o.DefaultMaterialID != "abs" {
		t.Errorf("completos: %+v", o)
	}
	// Algo que no es un objeto JSON: defaults.
	if o := OptionsFromSettings(42); o.WallLoops != 2 || o.InfillPercent != 15 {
		t.Errorf("basura: %+v", o)
	}
}

func TestMerge(t *testing.T) {
	base := OptionsFromSettings(fakeSettings{InfillPercent: 15, DefaultMaterial: "pla"})
	// Vacío del todo → la configuración.
	if got := Merge(Options{}, base); got.InfillPercent != 15 || got.WallLoops != 2 || got.DefaultMaterialID != "pla" || got.Currency != "USD" {
		t.Errorf("vacío: %+v", got)
	}
	// Solo un material por parte: el relleno 0 se lee como «sin dato».
	if got := Merge(Options{PartMaterials: map[int]string{1: "abs"}}, base); got.InfillPercent != 15 || got.PartMaterials[1] != "abs" {
		t.Errorf("parcial: %+v", got)
	}
	// Conjunto completo con 0 %: se respeta.
	got := Merge(Options{InfillPercent: 0, WallLoops: 3, LineWidthMm: 0.4, FilamentDiameterMm: 1.75}, base)
	if got.InfillPercent != 0 || got.WallLoops != 3 {
		t.Errorf("completo con 0 %%: %+v", got)
	}
	// El relleno propio gana siempre.
	if got := Merge(Options{InfillPercent: 40}, base); got.InfillPercent != 40 {
		t.Errorf("relleno propio: %+v", got)
	}
}

func TestClosedAsWhole(t *testing.T) {
	ps := []Part{{Index: 0, OpenMesh: true}, {Index: 1, OpenMesh: true}}
	if got := ClosedAsWhole(ps, true); got[0].OpenMesh || got[1].OpenMesh {
		t.Errorf("modelo estanco: las fronteras entre colores no son aberturas: %+v", got)
	}
	if !ps[0].OpenMesh {
		t.Error("ClosedAsWhole no debe modificar la entrada")
	}
	if got := ClosedAsWhole(ps, false); !got[0].OpenMesh {
		t.Error("modelo abierto: el aviso tiene que seguir")
	}
}
