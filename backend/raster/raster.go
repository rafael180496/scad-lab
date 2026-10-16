// Package raster dibuja una malla triangular en una imagen PNG con un
// rasterizador por software (z-buffer, sombreado plano, sin OpenGL ni GPU).
//
// Existe porque el PNG del OpenSCAD nativo (`openscad -o x.png`) necesita
// OpenGL fuera de pantalla, y el motor integrado (OpenSCAD en WebAssembly) no
// lo tiene: la herramienta `preview` del servidor MCP usa este paquete cuando
// no hay un OpenSCAD instalado. Acepta las mismas vistas que el preview nativo
// (la rotación de `--camera`) y pinta cada parte con su color.
package raster

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"strconv"

	"scadlab/backend/i18n"
)

// Tri es un triángulo: tres vértices (x, y, z) consecutivos.
type Tri [9]float32

// Part es una malla con su color.
type Part struct {
	Tris  []Tri
	Color [3]uint8
}

// View es una cámara como la de `--camera=0,0,0,rx,ry,rz,dist` de OpenSCAD:
// rotación en grados y proyección. La pieza se centra y se encuadra entera
// (--viewall --autocenter).
type View struct {
	RotX, RotY, RotZ float64
	Ortho            bool
}

// Fondo de la imagen: el del esquema «Tomorrow» de OpenSCAD.
var background = color.RGBA{255, 255, 255, 255}

// ssaa es el factor de supermuestreo: se dibuja a ssaa× y se promedia, que da
// bordes sin dientes de sierra sin una pasada de antialiasing aparte.
const ssaa = 2

// ReadSTL lee un STL binario (el que escribe OpenSCAD) o ASCII.
func ReadSTL(path string) ([]Tri, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var hdr [84]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, errBadSTL
	}
	n := binary.LittleEndian.Uint32(hdr[80:84])
	if st.Size() == 84+50*int64(n) {
		tris := make([]Tri, 0, n)
		br := bufio.NewReaderSize(f, 1<<20)
		var rec [50]byte
		for i := uint32(0); i < n; i++ {
			if _, err := io.ReadFull(br, rec[:]); err != nil {
				return nil, errBadSTL
			}
			var t Tri
			for k := 0; k < 9; k++ {
				t[k] = math.Float32frombits(binary.LittleEndian.Uint32(rec[12+4*k:]))
			}
			tris = append(tris, t)
		}
		return tris, nil
	}
	// ASCII: lo que sigue a «vertex» son tres números.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return parseASCII(data)
}

var errBadSTL = i18n.New(i18n.Msg{ES: "el STL está truncado o no es un STL", EN: "the STL is truncated or is not an STL"})

func parseASCII(data []byte) ([]Tri, error) {
	var tris []Tri
	var cur Tri
	k := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		fs := bytes.Fields(line)
		if len(fs) != 4 || string(fs[0]) != "vertex" {
			continue
		}
		for j := 0; j < 3; j++ {
			x, err := strconv.ParseFloat(string(fs[1+j]), 32)
			if err != nil {
				return nil, errBadSTL
			}
			cur[k*3+j] = float32(x)
		}
		k++
		if k == 3 {
			tris = append(tris, cur)
			k = 0
		}
	}
	if len(tris) == 0 || k != 0 {
		return nil, errBadSTL
	}
	return tris, nil
}

type vec3 [3]float64

func sub(a, b vec3) vec3 { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func cross(a, b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func dot(a, b vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func norm(a vec3) vec3 {
	l := math.Sqrt(dot(a, a))
	if l == 0 {
		return vec3{}
	}
	return vec3{a[0] / l, a[1] / l, a[2] / l}
}

// rotation devuelve la matriz que lleva un punto del mundo a coordenadas de
// cámara: Rx(−rx)·Ry(−ry)·Rz(−rz) (la cámara de OpenSCAD gira el espacio al
// revés que el objeto). En la cámara, +x es la derecha de la imagen, +y arriba
// y +z hacia quien mira. Las ocho vistas del preview nativo se comprobaron
// con una pieza asimétrica (ver backend/mcp/preview.go) y salen igual.
func rotation(v View) [3][3]float64 {
	rad := func(d float64) float64 { return -d * math.Pi / 180 }
	cx, sx := math.Cos(rad(v.RotX)), math.Sin(rad(v.RotX))
	cy, sy := math.Cos(rad(v.RotY)), math.Sin(rad(v.RotY))
	cz, sz := math.Cos(rad(v.RotZ)), math.Sin(rad(v.RotZ))
	rx := [3][3]float64{{1, 0, 0}, {0, cx, -sx}, {0, sx, cx}}
	ry := [3][3]float64{{cy, 0, sy}, {0, 1, 0}, {-sy, 0, cy}}
	rz := [3][3]float64{{cz, -sz, 0}, {sz, cz, 0}, {0, 0, 1}}
	return mul3(rx, mul3(ry, rz))
}

func mul3(a, b [3][3]float64) [3][3]float64 {
	var r [3][3]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			r[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return r
}

func apply(m [3][3]float64, p vec3) vec3 {
	return vec3{
		m[0][0]*p[0] + m[0][1]*p[1] + m[0][2]*p[2],
		m[1][0]*p[0] + m[1][1]*p[1] + m[1][2]*p[2],
		m[2][0]*p[0] + m[2][1]*p[1] + m[2][2]*p[2],
	}
}

// Render dibuja las partes en una imagen cuadrada de size×size píxeles.
func Render(parts []Part, v View, size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for i := range img.Pix {
		img.Pix[i] = 0
	}
	fillBackground(img)

	// Caja de la pieza entera, en el espacio de la cámara.
	rot := rotation(v)
	wlo := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	whi := vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	n := 0
	for _, p := range parts {
		for _, t := range p.Tris {
			for k := 0; k < 3; k++ {
				w := vec3{float64(t[3*k]), float64(t[3*k+1]), float64(t[3*k+2])}
				for a := 0; a < 3; a++ {
					wlo[a], whi[a] = math.Min(wlo[a], w[a]), math.Max(whi[a], w[a])
				}
			}
			n++
		}
	}
	if n == 0 {
		return img
	}
	center := vec3{(wlo[0] + whi[0]) / 2, (wlo[1] + whi[1]) / 2, (wlo[2] + whi[2]) / 2}
	radius := 0.0
	for a := 0; a < 3; a++ {
		d := (whi[a] - wlo[a]) / 2
		radius += d * d
	}
	radius = math.Max(math.Sqrt(radius), 1e-6)

	W := size * ssaa
	zbuf := make([]float32, W*W)
	for i := range zbuf {
		zbuf[i] = float32(math.Inf(-1))
	}
	pix := make([]uint8, W*W*3)
	have := make([]bool, W*W)

	// Encuadre: la esfera que envuelve la pieza ocupa ~92 % del lado.
	const fov = 22.5 * math.Pi / 180
	camDist := radius / math.Sin(fov/2)
	scale := float64(W) / 2 * 0.92 / radius // píxeles por mm (ortogonal)
	half := float64(W) / 2

	// Perspectiva: la cámara está a camDist del centro sobre +z de la cámara, y
	// la esfera que envuelve la pieza ocupa el 92 % del lado de la imagen.
	persp := half * 0.92 / (camDist * math.Tan(fov/2))
	project := func(p vec3) (x, y, z float64) {
		c := apply(rot, sub(p, center))
		if v.Ortho {
			return half + c[0]*scale, half - c[1]*scale, c[2]
		}
		d := math.Max(camDist-c[2], 1e-6)
		f := camDist / d
		return half + c[0]*f*persp, half - c[1]*f*persp, c[2]
	}

	// Luz: desde la cámara, algo desplazada hacia arriba y la izquierda, para
	// que las caras paralelas a la pantalla no queden todas del mismo tono.
	light := norm(vec3{-0.35, 0.55, 0.76})
	for _, p := range parts {
		base := [3]float64{float64(p.Color[0]), float64(p.Color[1]), float64(p.Color[2])}
		for _, t := range p.Tris {
			a := vec3{float64(t[0]), float64(t[1]), float64(t[2])}
			b := vec3{float64(t[3]), float64(t[4]), float64(t[5])}
			c := vec3{float64(t[6]), float64(t[7]), float64(t[8])}
			// Normal en el espacio de la cámara.
			nrm := norm(cross(sub(apply(rot, b), apply(rot, a)), sub(apply(rot, c), apply(rot, a))))
			if nrm == (vec3{}) {
				continue
			}
			// Sombreado plano de doble cara: se usa el valor absoluto, así un
			// triángulo con la normal invertida no se ve negro.
			diff := math.Abs(dot(nrm, light))
			shade := 0.30 + 0.70*diff
			col := [3]uint8{
				uint8(math.Min(255, base[0]*shade)),
				uint8(math.Min(255, base[1]*shade)),
				uint8(math.Min(255, base[2]*shade)),
			}
			x0, y0, z0 := project(a)
			x1, y1, z1 := project(b)
			x2, y2, z2 := project(c)
			fillTriangle(pix, zbuf, have, W, x0, y0, z0, x1, y1, z1, x2, y2, z2, col)
		}
	}

	// Promedio ssaa×ssaa; el fondo cuenta como color.
	bg := [3]float64{float64(background.R), float64(background.G), float64(background.B)}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var acc [3]float64
			for dy := 0; dy < ssaa; dy++ {
				for dx := 0; dx < ssaa; dx++ {
					i := (y*ssaa+dy)*W + x*ssaa + dx
					if have[i] {
						acc[0] += float64(pix[3*i])
						acc[1] += float64(pix[3*i+1])
						acc[2] += float64(pix[3*i+2])
					} else {
						acc[0] += bg[0]
						acc[1] += bg[1]
						acc[2] += bg[2]
					}
				}
			}
			k := float64(ssaa * ssaa)
			o := img.PixOffset(x, y)
			img.Pix[o] = uint8(acc[0]/k + 0.5)
			img.Pix[o+1] = uint8(acc[1]/k + 0.5)
			img.Pix[o+2] = uint8(acc[2]/k + 0.5)
			img.Pix[o+3] = 255
		}
	}
	return img
}

func fillBackground(img *image.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = background.R, background.G, background.B, 255
	}
}

// fillTriangle rellena un triángulo en coordenadas de pantalla con z-buffer
// (z mayor = más cerca de la cámara).
func fillTriangle(pix []uint8, zbuf []float32, have []bool, W int, x0, y0, z0, x1, y1, z1, x2, y2, z2 float64, col [3]uint8) {
	minX := int(math.Floor(math.Min(x0, math.Min(x1, x2))))
	maxX := int(math.Ceil(math.Max(x0, math.Max(x1, x2))))
	minY := int(math.Floor(math.Min(y0, math.Min(y1, y2))))
	maxY := int(math.Ceil(math.Max(y0, math.Max(y1, y2))))
	minX, minY = max(minX, 0), max(minY, 0)
	maxX, maxY = min(maxX, W-1), min(maxY, W-1)
	area := (x1-x0)*(y2-y0) - (x2-x0)*(y1-y0)
	if area == 0 || math.IsNaN(area) {
		return
	}
	inv := 1 / area
	for y := minY; y <= maxY; y++ {
		py := float64(y) + 0.5
		for x := minX; x <= maxX; x++ {
			px := float64(x) + 0.5
			w0 := ((x1-px)*(y2-py) - (x2-px)*(y1-py)) * inv
			w1 := ((x2-px)*(y0-py) - (x0-px)*(y2-py)) * inv
			w2 := 1 - w0 - w1
			if w0 < 0 || w1 < 0 || w2 < 0 {
				continue
			}
			z := float32(w0*z0 + w1*z1 + w2*z2)
			i := y*W + x
			if z > zbuf[i] {
				zbuf[i] = z
				pix[3*i], pix[3*i+1], pix[3*i+2] = col[0], col[1], col[2]
				have[i] = true
			}
		}
	}
}

// EncodePNG codifica la imagen como PNG.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
