package imagetools

import (
	"context"
	"encoding/xml"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Un círculo de radio conocido: el área en mm² sale dentro de ±2 % y la caja es
// la del círculo.
func TestTraceCircleArea(t *testing.T) {
	img := paint(400, 400, circle(200, 200, 100, black, white))
	res, err := Trace(context.Background(), load(t, img), TraceOptions{WidthMM: 50, Threshold: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 1 || len(res.Layers[0].Contours) != 1 {
		t.Fatalf("esperaba 1 capa con 1 contorno: %+v", res.Layers)
	}
	want := math.Pi * 25 * 25
	if got := res.Layers[0].AreaMM2; !near(got, want, 0.02) {
		t.Errorf("área = %.1f mm², esperaba %.1f ±2 %%", got, want)
	}
	if !near(res.WidthMM, 50, 0.01) || !near(res.HeightMM, 50, 0.01) {
		t.Errorf("caja = %.2f × %.2f mm, esperaba 50 × 50", res.WidthMM, res.HeightMM)
	}
	if res.Layers[0].Contours[0].Hole {
		t.Error("el círculo no es un hueco")
	}
	if res.ShapeIs != "dark" {
		t.Errorf("ShapeIs = %q", res.ShapeIs)
	}
	// El contorno sale simplificado: bastan unas decenas de puntos para un círculo de 50 mm a 0,1 mm.
	if n := res.Layers[0].PointCount(); n < 20 || n > 600 {
		t.Errorf("puntos del círculo: %d", n)
	}
	// La escala por mm_per_px (píxeles del archivo): 0,125 mm/px → el círculo mide 200 px → 25 mm.
	res2, err := Trace(context.Background(), load(t, img), TraceOptions{MMPerPx: 0.125, Threshold: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !near(res2.WidthMM, 25, 0.01) {
		t.Errorf("con mm_per_px el ancho es %.2f, esperaba 25", res2.WidthMM)
	}
	// Por alto.
	res3, err := Trace(context.Background(), load(t, img), TraceOptions{HeightMM: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !near(res3.HeightMM, 20, 0.01) {
		t.Errorf("con height_mm el alto es %.2f, esperaba 20", res3.HeightMM)
	}
}

// Un rectángulo con un agujero: dos contornos (uno es hueco) y el área neta.
func TestTraceRectangleWithHole(t *testing.T) {
	img := paint(400, 300, func(x, y float64) color.NRGBA {
		if x >= 50 && x < 350 && y >= 50 && y < 250 {
			if x >= 150 && x < 250 && y >= 100 && y < 160 {
				return white // agujero
			}
			return black
		}
		return white
	})
	res, err := Trace(context.Background(), load(t, img), TraceOptions{WidthMM: 150})
	if err != nil {
		t.Fatal(err)
	}
	l := res.Layers[0]
	if len(l.Contours) != 2 {
		t.Fatalf("esperaba 2 contornos, hay %d", len(l.Contours))
	}
	holes := 0
	for _, c := range l.Contours {
		if c.Hole {
			holes++
		}
	}
	if holes != 1 {
		t.Errorf("huecos: %d", holes)
	}
	// 300 px → 150 mm: 0,5 mm/px. Exterior 150×100 = 15000; agujero 50×30 = 1500.
	if !near(l.AreaMM2, 13500, 0.01) {
		t.Errorf("área neta = %.1f, esperaba 13500", l.AreaMM2)
	}
	if !near(res.HeightMM, 100, 0.01) {
		t.Errorf("alto = %.2f", res.HeightMM)
	}
	// Un rectángulo se simplifica a sus esquinas.
	if n := l.PointCount(); n > 40 {
		t.Errorf("un rectángulo con agujero no necesita %d puntos", n)
	}
}

// Fondo oscuro con la forma clara: se traza la forma, no el fondo; y `invert`
// traza lo contrario.
func TestTraceShapeIsTheOppositeOfTheBorder(t *testing.T) {
	img := paint(300, 300, circle(150, 150, 80, white, black))
	res, err := Trace(context.Background(), load(t, img), TraceOptions{WidthMM: 40})
	if err != nil {
		t.Fatal(err)
	}
	if res.ShapeIs != "light" || !near(res.Layers[0].AreaMM2, math.Pi*20*20, 0.03) {
		t.Errorf("ShapeIs=%s área=%.1f", res.ShapeIs, res.Layers[0].AreaMM2)
	}
	inv, err := Trace(context.Background(), load(t, img), TraceOptions{WidthMM: 40, Invert: true})
	if err != nil {
		t.Fatal(err)
	}
	// Invertido: el marco negro con un agujero circular: 2 contornos.
	if len(inv.Layers[0].Contours) != 2 {
		t.Errorf("invertido: %d contornos", len(inv.Layers[0].Contours))
	}
}

// Un logo de dos colores con layers=2: dos capas, una por color, con sus áreas.
func TestTraceTwoColorLayers(t *testing.T) {
	red, blue := color.NRGBA{220, 30, 30, 255}, color.NRGBA{20, 40, 200, 255}
	img := paint(500, 250, func(x, y float64) color.NRGBA {
		if x >= 40 && x < 200 && y >= 45 && y < 205 { // cuadrado rojo de 160 px
			return red
		}
		if math.Hypot(x-350, y-125) <= 80 { // círculo azul de radio 80 px
			return blue
		}
		return white
	})
	res, err := Trace(context.Background(), load(t, img), TraceOptions{MMPerPx: 0.1, Layers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 2 {
		t.Fatalf("capas: %d (%v)", len(res.Layers), res.Notes)
	}
	// 0,1 mm/px: cuadrado 16×16 = 256 mm²; círculo de radio 8 mm = 201,06 mm².
	areas := map[string]float64{}
	for _, l := range res.Layers {
		areas[l.Color] = l.AreaMM2
		if len(l.Contours) != 1 {
			t.Errorf("capa %d: %d contornos", l.Index, len(l.Contours))
		}
	}
	var sq, ci float64
	for hexc, a := range areas {
		if strings.HasPrefix(hexc, "#D") || strings.HasPrefix(hexc, "#E") {
			sq = a
		} else {
			ci = a
		}
	}
	if !near(sq, 256, 0.03) || !near(ci, math.Pi*64, 0.03) {
		t.Errorf("áreas: %v (cuadrado %.1f vs 256, círculo %.1f vs %.1f)", areas, sq, ci, math.Pi*64)
	}
	// Determinista: otra corrida da lo mismo.
	res2, err := Trace(context.Background(), load(t, img), TraceOptions{MMPerPx: 0.1, Layers: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := range res.Layers {
		if res.Layers[i].Color != res2.Layers[i].Color || res.Layers[i].AreaMM2 != res2.Layers[i].AreaMM2 {
			t.Errorf("capa %d no es determinista: %+v vs %+v", i, res.Layers[i], res2.Layers[i])
		}
	}
	// Cada capa es un SVG distinto y válido.
	if string(res.SVG(0)) == string(res.SVG(1)) {
		t.Error("las dos capas dan el mismo SVG")
	}
	// Pedir más colores de los que hay no inventa capas vacías.
	res3, err := Trace(context.Background(), load(t, img), TraceOptions{MMPerPx: 0.1, Layers: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range res3.Layers {
		if len(l.Contours) == 0 {
			t.Errorf("capa %d vacía", l.Index)
		}
	}
}

// El SVG es XML válido, con el tamaño en mm, un solo <path> par-impar y los
// números pedidos.
func TestSVGIsValidXML(t *testing.T) {
	img := paint(300, 200, func(x, y float64) color.NRGBA {
		if x >= 20 && x < 280 && y >= 20 && y < 180 && !(x >= 100 && x < 200 && y >= 70 && y < 130) {
			return black
		}
		return white
	})
	res, err := Trace(context.Background(), load(t, img), TraceOptions{WidthMM: 130})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		XMLName xml.Name `xml:"svg"`
		Width   string   `xml:"width,attr"`
		Height  string   `xml:"height,attr"`
		ViewBox string   `xml:"viewBox,attr"`
		Paths   []struct {
			Fill string `xml:"fill,attr"`
			Rule string `xml:"fill-rule,attr"`
			D    string `xml:"d,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(res.SVG(0), &doc); err != nil {
		t.Fatalf("SVG inválido: %v\n%s", err, res.SVG(0))
	}
	if doc.Width != "130mm" || doc.Height != "80mm" || doc.ViewBox != "0 0 130 80" {
		t.Errorf("tamaño: %s × %s, viewBox %q", doc.Width, doc.Height, doc.ViewBox)
	}
	if len(doc.Paths) != 1 || doc.Paths[0].Rule != "evenodd" || strings.Count(doc.Paths[0].D, "M") != 2 {
		t.Errorf("paths: %+v", doc.Paths)
	}
	if res.Polygon(0, 100) == "" || res.Polygon(0, 3) != "" {
		t.Error("polygon(): debería dar el código cuando es chico y nada cuando no")
	}
}

// El SVG que sale se importa en OpenSCAD real: el relleno par-impar hace el
// agujero, el volumen es área × altura, la caja es la esperada y la Y queda
// hacia arriba (una L con el pie abajo tiene el centroide bajo).
func TestSVGRendersInOpenSCAD(t *testing.T) {
	bin := needOpenSCAD(t)
	dir := t.TempDir()
	// Una «L»: barra vertical a la izquierda y barra horizontal abajo, con un agujero.
	img := paint(400, 300, func(x, y float64) color.NRGBA {
		switch {
		case x >= 40 && x < 120 && y >= 20 && y < 280: // vertical
			if x >= 60 && x < 100 && y >= 60 && y < 100 {
				return white
			}
			return black
		case x >= 40 && x < 360 && y >= 220 && y < 280: // pie
			return black
		}
		return white
	})
	res, err := Trace(context.Background(), load(t, img), TraceOptions{WidthMM: 64}) // 320 px → 0,2 mm/px
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shape.svg"), res.SVG(0), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "m.scad"), []byte("linear_extrude(2) import(\"shape.svg\");\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vol, cen, mn, mx := stlStats(t, renderSTL(t, bin, dir, "m.scad"))

	// Área: vertical 80×260 px (menos el agujero 40×40) + pie que sobresale 240×60 px.
	px := 0.2 * 0.2 // mm² por px²
	wantArea := (80*260 - 40*40 + 240*60) * px
	if !near(vol/2, wantArea, 0.02) {
		t.Errorf("área importada = %.1f mm², esperaba %.1f", vol/2, wantArea)
	}
	if !near(mx[0]-mn[0], 64, 0.01) || !near(mx[1]-mn[1], 52, 0.01) || math.Abs(mn[0]) > 0.2 || math.Abs(mn[1]) > 0.2 {
		t.Errorf("caja importada: x %.2f..%.2f y %.2f..%.2f", mn[0], mx[0], mn[1], mx[1])
	}
	if cen[1] > 26 {
		t.Errorf("la Y quedó al revés: centroide y = %.1f (la L tiene el pie abajo, esperaba < 26)", cen[1])
	}
	if cen[0] > 32 {
		t.Errorf("centroide x = %.1f (la barra vertical está a la izquierda)", cen[0])
	}

	// El polygon() del resultado da lo mismo que el SVG.
	poly := res.Polygon(0, 1000)
	if poly == "" {
		t.Fatal("sin polygon()")
	}
	if err := os.WriteFile(filepath.Join(dir, "p.scad"), []byte("linear_extrude(2) "+poly+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pv, pc, _, _ := stlStats(t, renderSTL(t, bin, dir, "p.scad"))
	if !near(pv, vol, 0.01) || math.Abs(pc[1]-cen[1]) > 0.3 {
		t.Errorf("polygon(): volumen %.1f vs %.1f, centroide y %.2f vs %.2f", pv, vol, pc[1], cen[1])
	}
}

// Un PNG con fondo transparente: la forma es lo opaco, aunque sea blanca.
func TestTraceTransparentLogo(t *testing.T) {
	img := paint(300, 300, circle(150, 150, 90, white, color.NRGBA{}))
	im := load(t, img)
	if !im.HasAlpha {
		t.Fatal("no detectó la transparencia")
	}
	res, err := Trace(context.Background(), im, TraceOptions{WidthMM: 36})
	if err != nil {
		t.Fatal(err)
	}
	if res.ShapeIs != "opaque" || !near(res.Layers[0].AreaMM2, math.Pi*18*18, 0.03) {
		t.Errorf("ShapeIs=%s área=%.1f", res.ShapeIs, res.Layers[0].AreaMM2)
	}
}

func TestTraceErrors(t *testing.T) {
	im := load(t, paint(100, 100, circle(50, 50, 30, black, white)))
	cases := map[string]TraceOptions{
		"sin escala":       {},
		"capas de más":     {WidthMM: 10, Layers: 9},
		"escala negativa":  {WidthMM: -3},
		"umbral inválido":  {WidthMM: 10, Threshold: 999},
		"todo muy chico":   {WidthMM: 1, MinAreaMM2: 5},
		"escala absurda":   {WidthMM: 1e9},
		"escala inválida2": {MMPerPx: math.NaN()},
	}
	for name, o := range cases {
		if _, err := Trace(context.Background(), im, o); err == nil {
			t.Errorf("%s: esperaba un error", name)
		}
	}
	// Imagen lisa: no hay forma que trazar.
	blank := load(t, paint(100, 100, func(x, y float64) color.NRGBA { return white }))
	// (con Otsu sobre un histograma de un solo valor todo cae en una clase)
	if res, err := Trace(context.Background(), blank, TraceOptions{WidthMM: 10}); err == nil && len(res.Layers[0].Contours) > 0 {
		t.Errorf("una imagen lisa no tiene contornos: %+v", res.Layers)
	}
}

// El plazo de la operación se respeta.
func TestTraceHonorsCancelledContext(t *testing.T) {
	im := load(t, paint(200, 200, circle(100, 100, 50, black, white)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Trace(ctx, im, TraceOptions{WidthMM: 10}); err == nil {
		t.Error("con el contexto cancelado debía fallar")
	}
	if _, err := im.Foreground(ctx); err == nil {
		t.Error("Foreground con el contexto cancelado debía fallar")
	}
}

// Marching squares: la orientación de los contornos (exterior positivo, hueco
// negativo) y que cierren incluso si la forma toca el borde.
func TestMarchingSquaresOrientationAndEdges(t *testing.T) {
	m := newMask(20, 20)
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			m.Bits[y*20+x] = !(x >= 7 && x < 13 && y >= 7 && y < 13) // toda la imagen menos un agujero
		}
	}
	f, w, h := blurField(m)
	loops := marchingSquares(context.Background(), f, w, h)
	if len(loops) != 2 {
		t.Fatalf("contornos: %d", len(loops))
	}
	pos, neg := 0, 0
	for _, l := range loops {
		if signedArea(l) > 0 {
			pos++
		} else {
			neg++
		}
	}
	if pos != 1 || neg != 1 {
		t.Errorf("exteriores %d, huecos %d", pos, neg)
	}
	_ = image.Pt
}

// Marching squares con máscaras al azar (incluyendo diagonales que dan casos
// ambiguos y valores del campo que valen justo 0.5): ningún contorno se pierde.
// Cada arista que cruza el nivel es un punto de exactamente un contorno, así que
// la suma de los puntos tiene que ser el número de aristas que cruzan.
func TestMarchingSquaresKeepsEveryCrossing(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 60; trial++ {
		w, h := 8+rng.Intn(40), 8+rng.Intn(40)
		m := newMask(w, h)
		density := 0.2 + 0.6*rng.Float64()
		for i := range m.Bits {
			m.Bits[i] = rng.Float64() < density
		}
		if trial%3 == 0 { // tablero de ajedrez: todo son sillas de montar
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					m.Bits[y*w+x] = (x+y)%2 == 0
				}
			}
		}
		f, fw, fh := blurField(m)
		crossings := 0
		in := func(x, y int) bool { return f[y*fw+x] >= 0.5 }
		for y := 0; y < fh; y++ {
			for x := 0; x < fw; x++ {
				if x+1 < fw && in(x, y) != in(x+1, y) {
					crossings++
				}
				if y+1 < fh && in(x, y) != in(x, y+1) {
					crossings++
				}
			}
		}
		points := 0
		for _, l := range marchingSquares(context.Background(), f, fw, fh) {
			points += len(l)
		}
		if points != crossings {
			t.Fatalf("prueba %d (%d×%d): %d aristas cruzan y los contornos suman %d puntos", trial, w, h, crossings, points)
		}
	}
}
