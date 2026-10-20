// Package parts reparte un modelo en sus partes por color y las deja en una
// caché de STL, una malla por parte, para que el visor las cargue por separado
// con su color real y el panel Impresión estime peso y costo por parte (S7).
//
// El reparto es el mismo de la exportación (S9): OpenSCAD escribe un 3MF con un
// color por triángulo y threemf.Split lo separa por color. Este paquete tiene
// esa lógica común (Load3MF, RenderFailure) y backend/exporter la reutiliza.
//
// Caché: <cacheDir>/<clave>.parts/NN.stl (STL binario) + manifest.json, con la
// clave del render normal del checkpoint (el nombre de su malla en
// <datos>/cache/meshes, 64 hex). Se genera una vez por clave. El visor las pide
// por GET /parts/<clave>/NN.stl (backend/meshserve).
//
// Spec: .claude/specs/s7-medicion-impresion.md. Notas: claude/14-medicion.md.
package parts

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/threemf"
)

// DirSuffix es el sufijo de la carpeta de una clave: <clave>.parts.
const DirSuffix = ".parts"

// MaxParts es el tope de partes: el nombre de archivo lleva dos dígitos (NN).
const MaxParts = 100

// URLPrefix es la ruta bajo la que meshserve sirve las partes.
const URLPrefix = "/parts/"

// FitTolerance: medio décimo de milímetro de holgura al comparar con la placa;
// un cubo de 180,00 en una placa de 180 cabe.
const FitTolerance = 0.05

var keyRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidKey dice si s tiene la forma de una clave de render (sha256 en hex
// minúscula).
func ValidKey(s string) bool { return keyRe.MatchString(s) }

// FileName es el nombre del STL de la parte i: «00.stl», «01.stl»…
func FileName(i int) string { return fmt.Sprintf("%02d.stl", i) }

// URL es la ruta HTTP de la parte i de una clave.
func URL(key string, i int) string { return URLPrefix + key + "/" + FileName(i) }

// KeyFromMeshPath saca la clave de render.Result.MeshPath
// (<...>/meshes/<clave>.stl). Error si no tiene esa forma.
func KeyFromMeshPath(meshPath string) (string, error) {
	name := meshPath
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	key, ok := strings.CutSuffix(name, ".stl")
	if !ok || !ValidKey(key) {
		return "", i18n.Errorf(i18n.Msg{ES: "%q no es una malla de la caché de render", EN: "%q is not a mesh from the render cache"}, meshPath)
	}
	return key, nil
}

// Part es una parte del modelo (contrato con el visor).
type Part struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Color string `json:"color"` // #RRGGBB
	// Uncolored: la geometría no pasó por color(); Color es el amarillo por
	// defecto de OpenSCAD y el visor puede preferir una muestra.
	Uncolored bool      `json:"uncolored"`
	URL       string    `json:"url"` // /parts/<clave>/NN.stl
	Info      mesh.Info `json:"info"`
}

// Manifest es lo que se guarda en <clave>.parts/manifest.json: las partes y la
// malla completa. No depende de la impresora (cambia sin que cambie el modelo).
type Manifest struct {
	Version int       `json:"version"`
	Key     string    `json:"key"`
	Parts   []Part    `json:"parts"`
	Total   mesh.Info `json:"total"` // bbox y volumen del modelo entero
}

// Model es lo que devuelve el binding GetModelParts.
type Model struct {
	Key      string          `json:"key"`
	Parts    []Part          `json:"parts"`
	Total    mesh.Info       `json:"total"`
	Plate    printers.Volume `json:"plate"`    // volumen de impresión del workspace
	Fits     bool            `json:"fits"`     // el bbox global cabe en la placa
	Overflow printers.Volume `json:"overflow"` // mm que sobran por eje; 0 si cabe
}

// Model arma el Model de la impresora dada.
func (m Manifest) Model(pr printers.Printer) Model {
	fits, over := Fit(m.Total.Size, pr.BuildVolume)
	parts := m.Parts
	if parts == nil {
		parts = []Part{}
	}
	return Model{Key: m.Key, Parts: parts, Total: m.Total, Plate: pr.BuildVolume, Fits: fits, Overflow: over}
}

// Fit dice si un tamaño cabe en el volumen de impresión y cuánto se pasa en
// cada eje (0 si cabe; un eje con límite 0 se considera sin dato y no cuenta).
// Es el único cálculo de placa: lo usan el visor y la herramienta MCP measure.
func Fit(size [3]float64, v printers.Volume) (bool, printers.Volume) {
	limits := [3]float64{v.X, v.Y, v.Z}
	var over [3]float64
	fits := true
	for i := range size {
		if limits[i] > 0 && size[i] > limits[i]+FitTolerance {
			over[i] = math.Round((size[i]-limits[i])*100) / 100
			fits = false
		}
	}
	return fits, printers.Volume{X: over[0], Y: over[1], Z: over[2]}
}

// Renderer es la parte del motor de render que usa este paquete (la cumple
// *render.Engine; la interfaz permite probar sin OpenSCAD).
type Renderer interface {
	Render(ctx context.Context, req render.Request, opts render.Options) (render.Result, error)
	RenderArtifact(ctx context.Context, req render.Request, opts render.Options, ext, dir string) (render.Result, error)
}

// Load3MF pide a OpenSCAD un 3MF con color, lo lee y lo reparte en una parte
// por color. dir es la caché de los 3MF (<datos>/cache/exports). Devuelve
// siempre el render.Result; si el render no salió bien, el error es el de
// RenderFailure.
//
// Cada parte tiene que ser un sólido cerrado: Bambu Studio lamina cada una por
// separado. OpenSCAD, en cambio, funde todo lo del nivel superior en un solo
// sólido antes de exportar, y la cara por la que dos colores se tocaban (un
// relieve sobre una pared, un texto sobre una placa) desaparece: al repartir esa
// malla por color, cada parte queda como una cáscara con un hueco (claude/11).
// Si eso pasa con más de un color, se vuelve a renderizar con
// `--enable=lazy-union`, que entrega cada color() del nivel superior como su
// propio sólido, y se usa ese reparto si deja menos bordes abiertos. Un modelo
// de un solo color, o cuyos colores no se tocan, no paga el segundo render ni
// cambia de forma.
func Load3MF(ctx context.Context, eng Renderer, req render.Request, opts render.Options, dir string) ([]threemf.Part, render.Result, error) {
	split, res, err := load3MF(ctx, eng, req, opts, dir)
	if err != nil || len(split) < 2 || opts.LazyUnion {
		return split, res, err
	}
	open := threemf.OpenEdgeCount(split)
	if open == 0 {
		return split, res, nil
	}
	lazy := opts
	lazy.LazyUnion = true
	lsplit, lres, lerr := load3MF(ctx, eng, req, lazy, dir)
	if lerr != nil {
		if ctx.Err() != nil {
			return nil, lres, lerr
		}
		return split, res, nil // el modelo se exporta como salió, con el aviso de partes abiertas
	}
	if len(lsplit) >= len(split) && threemf.OpenEdgeCount(lsplit) < open {
		return lsplit, lres, nil
	}
	return split, res, nil
}

func load3MF(ctx context.Context, eng Renderer, req render.Request, opts render.Options, dir string) ([]threemf.Part, render.Result, error) {
	res, err := eng.RenderArtifact(ctx, req, opts, render.ArtifactExt3MF, dir)
	if err != nil {
		return nil, res, err
	}
	if err := RenderFailure(res); err != nil {
		return nil, res, err
	}
	m, err := threemf.ReadFile(res.MeshPath)
	if err != nil {
		return nil, res, err
	}
	return threemf.Split(m), res, nil
}

// RenderFailure convierte un render que no salió bien en un error legible, con
// el primer ERROR de OpenSCAD y su línea si la tiene. nil si salió bien.
func RenderFailure(res render.Result) error {
	switch res.Status {
	case render.StatusOK:
		return nil
	case render.StatusMissing:
		return i18n.New(i18n.Msg{
			ES: "OpenSCAD no está instalado o no se encontró: instálalo o indica su ruta en Configuración → OpenSCAD",
			EN: "OpenSCAD is not installed or was not found: install it or set its path in Settings → OpenSCAD",
		})
	case render.StatusCancelled:
		return i18n.New(i18n.Msg{
			ES: "el render del modelo se canceló porque empezó otro",
			EN: "the model render was cancelled because another one started",
		})
	}
	for _, d := range res.Diagnostics {
		if d.Severity != render.SeverityError {
			continue
		}
		if d.Line > 0 {
			return i18n.Errorf(i18n.Msg{
				ES: "el checkpoint no se puede renderizar (línea %d): %s",
				EN: "the checkpoint cannot be rendered (line %d): %s",
			}, d.Line, d.Message)
		}
		return i18n.Errorf(i18n.Msg{
			ES: "el checkpoint no se puede renderizar: %s",
			EN: "the checkpoint cannot be rendered: %s",
		}, d.Message)
	}
	return i18n.New(i18n.Msg{ES: "el checkpoint no se puede renderizar", EN: "the checkpoint cannot be rendered"})
}
