package imagetools

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"scadlab/backend/detect"
)

var (
	white = color.NRGBA{255, 255, 255, 255}
	black = color.NRGBA{0, 0, 0, 255}
)

// paint dibuja una imagen sintética: por cada píxel pide el color a f, con
// supermuestreo 3×3 para que los bordes salgan suavizados como en una foto.
func paint(w, h int, f func(x, y float64) color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	const n = 3
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b, a float64
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					c := f(float64(x)+(float64(sx)+0.5)/n, float64(y)+(float64(sy)+0.5)/n)
					pa := float64(c.A)
					r += float64(c.R) * pa
					g += float64(c.G) * pa
					b += float64(c.B) * pa
					a += pa
				}
			}
			if a > 0 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / (n * n))})
			}
		}
	}
	return img
}

func circle(cx, cy, r float64, in, out color.NRGBA) func(x, y float64) color.NRGBA {
	return func(x, y float64) color.NRGBA {
		if math.Hypot(x-cx, y-cy) <= r {
			return in
		}
		return out
	}
}

func writePNG(t *testing.T, dir, name string, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func load(t *testing.T, img image.Image) *Image {
	t.Helper()
	im, err := FromImage(context.Background(), img)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func needOpenSCAD(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tool := detect.OpenSCAD(ctx, "")
	if !tool.Found {
		t.Skip("OpenSCAD no está instalado")
	}
	return tool.Path
}

// stlStats lee un STL ASCII (el que escribe OpenSCAD) y devuelve el volumen, el
// centroide (ponderado por volumen) y la caja.
func stlStats(t *testing.T, path string) (vol float64, cen, min, max [3]float64) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vs [][3]float64
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) == 4 && f[0] == "vertex" {
			var v [3]float64
			for i := 0; i < 3; i++ {
				v[i], _ = strconv.ParseFloat(f[1+i], 64)
			}
			vs = append(vs, v)
		}
	}
	if len(vs) == 0 || len(vs)%3 != 0 {
		t.Fatalf("STL ilegible: %d vértices", len(vs))
	}
	min = [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	max = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, v := range vs {
		for i := 0; i < 3; i++ {
			min[i], max[i] = math.Min(min[i], v[i]), math.Max(max[i], v[i])
		}
	}
	for i := 0; i < len(vs); i += 3 {
		a, b, c := vs[i], vs[i+1], vs[i+2]
		v := (a[0]*(b[1]*c[2]-b[2]*c[1]) - a[1]*(b[0]*c[2]-b[2]*c[0]) + a[2]*(b[0]*c[1]-b[1]*c[0])) / 6
		vol += v
		for k := 0; k < 3; k++ {
			cen[k] += v * (a[k] + b[k] + c[k]) / 4
		}
	}
	for k := 0; k < 3; k++ {
		cen[k] /= vol
	}
	return
}

// renderSTL corre OpenSCAD sobre un .scad y devuelve el STL.
func renderSTL(t *testing.T, bin, dir, scad string) string {
	t.Helper()
	out := filepath.Join(dir, "out.stl")
	cmd := exec.Command(bin, "-o", out, scad)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("openscad: %v\n%s", err, b)
	}
	return out
}

func near(got, want, tolFrac float64) bool {
	return math.Abs(got-want) <= tolFrac*math.Abs(want)
}
