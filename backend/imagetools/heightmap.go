package imagetools

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"

	"scadlab/backend/i18n"
)

// Límites del mapa de alturas.
const (
	MaxHeightmapSide        = 500
	DefaultHeightmapColumns = 200
	DefaultBaseMM           = 0.6
)

// HeightmapOptions son los argumentos de Heightmap.
type HeightmapOptions struct {
	// WidthMM es el ancho de la pieza; HeightMM el alto (0 = por la proporción de
	// la imagen). Si se dan los dos con otra proporción, la imagen se estira.
	WidthMM, HeightMM float64
	// MaxHeightMM es lo que sube el relieve sobre la base (valor 100).
	MaxHeightMM float64
	// BaseMM es el espesor mínimo (valor 0); 0 = DefaultBaseMM.
	BaseMM float64
	// Invert hace grueso lo oscuro (litofanía: la luz pasa por lo fino).
	Invert bool
	// Resolution son las columnas del mapa (0 = 200; máximo 500).
	Resolution int
	// Stretch estira el contraste para usar todo el rango 0–100.
	Stretch bool
}

// Heightmap es un mapa de alturas listo para `surface()`.
type Heightmap struct {
	Cols, Rows int
	// Values son Cols×Rows alturas de 0 a 100, fila 0 = arriba de la imagen.
	Values []float32

	WidthMM, HeightMM, MaxHeightMM, BaseMM float64
	Invert                                 bool
	// Min y Max son los valores reales del mapa.
	Min, Max float64
}

// BuildHeightmap convierte la luminancia de la imagen en alturas: claro = alto
// (0 negro, 100 blanco) y, con Invert, al revés. La imagen transparente cuenta
// como blanca.
func BuildHeightmap(ctx context.Context, im *Image, o HeightmapOptions) (*Heightmap, error) {
	if !(o.WidthMM > 0) || !(o.MaxHeightMM > 0) || o.HeightMM < 0 || o.BaseMM < 0 ||
		o.WidthMM > 10000 || o.HeightMM > 10000 || o.MaxHeightMM > 10000 || o.BaseMM > 10000 {
		return nil, i18n.New(i18n.Msg{
			ES: "indica `width_mm` y `max_height_mm` (números positivos, en mm)",
			EN: "give `width_mm` and `max_height_mm` (positive numbers, in mm)",
		})
	}
	if o.Resolution < 0 || o.Resolution > MaxHeightmapSide {
		return nil, i18n.Errorf(i18n.Msg{ES: "`resolution` va de 1 a %d columnas", EN: "`resolution` goes from 1 to %d columns"}, MaxHeightmapSide)
	}
	cols := o.Resolution
	if cols == 0 {
		cols = DefaultHeightmapColumns
	}
	cols = min(cols, im.W)
	rows := max(1, int(math.Round(float64(cols)*float64(im.H)/float64(im.W))))
	if rows > MaxHeightmapSide {
		cols = max(1, cols*MaxHeightmapSide/rows)
		rows = MaxHeightmapSide
	}
	if cols < 2 || rows < 2 {
		return nil, i18n.New(i18n.Msg{
			ES: "la imagen es demasiado chica para un mapa de alturas (mínimo 2×2 puntos)",
			EN: "the image is too small for a heightmap (at least 2×2 points)",
		})
	}
	small, err := im.Resize(ctx, cols, rows)
	if err != nil {
		return nil, err
	}
	g := small.gray()
	h := &Heightmap{
		Cols: cols, Rows: rows, Values: make([]float32, cols*rows),
		WidthMM: o.WidthMM, HeightMM: o.HeightMM, MaxHeightMM: o.MaxHeightMM, BaseMM: o.BaseMM, Invert: o.Invert,
	}
	if h.HeightMM == 0 {
		h.HeightMM = round3(o.WidthMM * float64(im.H) / float64(im.W))
	}
	if h.BaseMM == 0 {
		h.BaseMM = DefaultBaseMM
	}
	vals := make([]float64, len(g))
	for i, v := range g {
		vals[i] = float64(v) / 255 * 100
	}
	if o.Stretch {
		lo, hi := percentile(vals, 0.01), percentile(vals, 0.99)
		if hi-lo >= 4 {
			for i, v := range vals {
				vals[i] = math.Max(0, math.Min(100, (v-lo)/(hi-lo)*100))
			}
		}
	}
	h.Min, h.Max = math.MaxFloat64, -math.MaxFloat64
	for i, v := range vals {
		if o.Invert {
			v = 100 - v
		}
		v = math.Round(v*10) / 10
		h.Values[i] = float32(v)
		h.Min, h.Max = math.Min(h.Min, v), math.Max(h.Max, v)
	}
	return h, nil
}

// DAT escribe el mapa en el formato de `surface()`: una fila por línea, valores
// separados por espacios. surface() pone la ÚLTIMA fila del archivo en la Y más
// alta (comprobado con OpenSCAD 2026), así que las filas van de abajo hacia
// arriba para que lo de arriba de la foto quede en +Y.
func (h *Heightmap) DAT() []byte {
	var b bytes.Buffer
	b.WriteString("# ScadLab: mapa de alturas para surface(), valores 0-100\n")
	for r := h.Rows - 1; r >= 0; r-- {
		for c := 0; c < h.Cols; c++ {
			if c > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strconv.FormatFloat(float64(h.Values[r*h.Cols+c]), 'f', -1, 32))
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// Snippet es el código que usa el mapa: un relieve de MaxHeightMM sobre una base
// de BaseMM, recortado a z ≥ 0 (surface() baja un punto por debajo del mínimo).
func (h *Heightmap) Snippet(ref string) string {
	sx := h.WidthMM / float64(h.Cols-1)
	sy := h.HeightMM / float64(h.Rows-1)
	sz := h.MaxHeightMM / 100
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return fmt.Sprintf(
		"width = %s; depth = %s; base = %s; relief = %s;\n"+
			"intersection() {\n"+
			"    union() {\n"+
			"        translate([0, 0, base]) scale([%s, %s, %s]) surface(file = %q, center = true);\n"+
			"        translate([-width/2, -depth/2, 0]) cube([width, depth, base + 0.01]);\n"+
			"    }\n"+
			"    translate([-width/2, -depth/2, 0]) cube([width, depth, base + relief + 1]);\n"+
			"}",
		f(h.WidthMM), f(h.HeightMM), f(h.BaseMM), f(h.MaxHeightMM),
		f(round6(sx)), f(round6(sy)), f(round6(sz)), ref)
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
