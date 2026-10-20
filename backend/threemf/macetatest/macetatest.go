// Package macetatest es el modelo de prueba del diagnóstico del 3MF que Bambu Studio
// rellenaba (S17, tarea 17.2) y las comprobaciones que se le hacen por las tres rutas de
// exportación: OpenSCAD nativo, ScadLab (backend/exporter → parts → threemf) y el motor
// integrado (backend/scadwasm). SOLO para pruebas: la app no lo importa.
//
// El modelo (maceta.scad) es una maceta cónica hueca, con cavidad cónica y drenajes en el
// fondo (color verde), y unas letras y un marco en relieve curvados con la intersección de
// un cascarón cónico (color naranja). Es la estructura de la maceta real del usuario, sin
// fuentes ni datos suyos. Los dos color() cuelgan de un `if` del nivel superior.
//
// Fixtures, generadas con OpenSCAD 2026.10.11:
//
//	maceta-union.3mf   openscad -o maceta-union.3mf maceta.scad
//	maceta-lazy.3mf    openscad --enable=lazy-union -o maceta-lazy.3mf maceta.scad
package macetatest

import (
	_ "embed"
	"math"
	"strings"
	"testing"

	"scadlab/backend/threemf"
)

// Scad es el código del modelo; la variable `solo` ("verde" / "naranja") deja un solo color.
//
//go:embed maceta.scad
var Scad string

// Union y Lazy son los 3MF que escribe OpenSCAD por defecto y con --enable=lazy-union.
var (
	//go:embed maceta-union.3mf
	Union []byte
	//go:embed maceta-lazy.3mf
	Lazy []byte
)

// Only devuelve el modelo con un solo color: "verde" o "naranja" ("" = los dos). Sirve para
// obtener, con el mismo motor, el STL de cada color y compararlo con la parte del 3MF.
func Only(color string) string {
	return strings.Replace(Scad, `solo = "";`, `solo = "`+color+`";`, 1)
}

// Ref son las medidas de referencia: el STL de cada color por separado y el del modelo
// entero (sólido fundido), con OpenSCAD 2026.10.11.
var Ref = struct {
	GreenMin, GreenMax   [3]float64
	GreenVol             float64
	OrangeMin, OrangeMax [3]float64
	OrangeVol            float64
	UnionVol             float64
}{
	GreenMin: [3]float64{-30, -30, 0}, GreenMax: [3]float64{30, 30, 50}, GreenVol: 23132.022,
	OrangeMin: [3]float64{-14.170262, -28.357927, 13.013348}, OrangeMax: [3]float64{14.170262, -21.139783, 35.335818},
	OrangeVol: 163.687,
	UnionVol:  23264.751,
}

// Colors son los colores de las dos partes, en el orden de declaración.
var Colors = [2]string{"#3A7D44", "#E76F51"}

// AssertSolids comprueba que parts son las dos partes de la maceta como las espera un
// slicer: cada una un sólido cerrado (cada arista dirigida una vez y su opuesta una vez),
// con volumen con signo > 0 (normales hacia afuera), sin triángulos degenerados ni
// repetidos, sin triángulos compartidos entre las partes, con el volumen y la caja del STL
// de ese color, y que juntas sumen algo más que el sólido fundido (el relieve entra 0,2 mm
// en la pared, así que se solapan). tolVol es la tolerancia relativa del volumen.
func AssertSolids(t testing.TB, parts []threemf.Part, tolVol float64) {
	t.Helper()
	if len(parts) != 2 {
		t.Fatalf("partes: %d, quería 2", len(parts))
	}
	for i, p := range parts {
		if p.Color.Hex() != Colors[i] {
			t.Fatalf("parte %d: color %s, quería %s", i, p.Color.Hex(), Colors[i])
		}
	}
	checks, shared := threemf.InspectAll(parts)
	if shared != 0 {
		t.Errorf("%d triángulos idénticos en las dos partes", shared)
	}
	want := []struct {
		min, max [3]float64
		vol      float64
	}{
		{Ref.GreenMin, Ref.GreenMax, Ref.GreenVol},
		{Ref.OrangeMin, Ref.OrangeMax, Ref.OrangeVol},
	}
	var sum float64
	for i, c := range checks {
		if !c.Closed() {
			t.Errorf("%s: no es un sólido cerrado: %+v", c.Name, c)
		}
		if c.OpenEdges != 0 || c.NonManifoldEdges != 0 || c.Degenerate != 0 || c.Duplicates != 0 {
			t.Errorf("%s: aristas abiertas o repetidas, degenerados o duplicados: %+v", c.Name, c)
		}
		if c.SignedVolume <= 0 {
			t.Errorf("%s: volumen con signo %.3f, tiene que ser > 0 (normales hacia afuera)", c.Name, c.SignedVolume)
		}
		if math.Abs(c.SignedVolume-want[i].vol)/want[i].vol > tolVol {
			t.Errorf("%s: volumen %.3f, el del STL de ese color es %.3f", c.Name, c.SignedVolume, want[i].vol)
		}
		if !Near(c.Min, want[i].min, 2e-3) || !Near(c.Max, want[i].max, 2e-3) {
			t.Errorf("%s: caja %v–%v, la del STL de ese color es %v–%v", c.Name, c.Min, c.Max, want[i].min, want[i].max)
		}
		sum += c.SignedVolume
	}
	// El naranja es solo el relieve: ningún punto suyo entra en la cavidad (más adentro que la
	// pared exterior menos los 0,2 mm que se solapan con el verde y 0,1 mm de la aproximación
	// poligonal de 64 lados del cono; la cavidad empieza 2,4 mm adentro). Es la hipótesis «una parte
	// ocupa el hueco de la otra».
	for _, v := range parts[1].Vertices {
		outer := 22 + 8*v[2]/50 // radio exterior de la pared a esa altura (d_inf 44, d_sup 60, alto 50)
		if r := math.Hypot(v[0], v[1]); r < outer-0.3 {
			t.Errorf("el vértice %v del relieve está dentro de la pared/cavidad (radio %.3f < %.3f)", v, r, outer-0.3)
			break
		}
	}
	if sum < Ref.UnionVol || (sum-Ref.UnionVol)/Ref.UnionVol > 0.01 {
		t.Errorf("suma de las partes %.3f, sólido fundido %.3f: tiene que ser algo mayor (solape de 0,2 mm)", sum, Ref.UnionVol)
	}
}

// Near dice si a y b coinciden eje por eje dentro de tol.
func Near(a, b [3]float64, tol float64) bool {
	for k := 0; k < 3; k++ {
		if math.Abs(a[k]-b[k]) > tol {
			return false
		}
	}
	return true
}
