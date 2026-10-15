// Package render corre OpenSCAD sobre un checkpoint y devuelve la malla, los
// diagnósticos y el estado. Es la pieza que usan el visor (S2), el editor (S4,
// marcadores), la auto-iteración del agente (S5) y el MCP (S6).
//
// Contrato fijado en .claude/specs/s1-motor-render.md: los tipos y firmas de
// este archivo los consume app_render.go; cambiarlos es cambiar el spec.
package render

import "context"

// Severity de un diagnóstico.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Status de un render.
const (
	StatusOK        = "ok"        // malla generada
	StatusError     = "error"     // OpenSCAD falló o el modelo quedó vacío
	StatusCancelled = "cancelled" // lo reemplazó un render más nuevo o se canceló
	StatusMissing   = "missing"   // OpenSCAD no está instalado / no se encontró
)

// Diagnostic es un ERROR o WARNING de OpenSCAD con su ubicación. Line y Col
// son 1-based; 0 = sin ubicación. File es el nombre relativo al workspace
// cuando el mensaje apunta a otro archivo (un include), "" si es el propio.
type Diagnostic struct {
	Severity string `json:"severity"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	File     string `json:"file"`
	Message  string `json:"message"`
}

// Request pide renderizar un checkpoint.
type Request struct {
	WorkspaceKey string // identifica la cola: un render nuevo cancela el anterior de la misma clave
	WorkspaceDir string
	FileName     string // NNN-slug.scad, relativo a WorkspaceDir
}

// Options es la configuración vigente al momento de renderizar.
type Options struct {
	OpenSCADPath   string // ruta resuelta (detect.OpenSCAD); "" = StatusMissing
	Backend        string // "manifold" | "cgal"
	TimeoutSeconds int    // 0 = 300
}

// Result es el resultado de un render.
type Result struct {
	FileName    string       `json:"fileName"`
	Status      string       `json:"status"`
	MeshPath    string       `json:"meshPath"` // STL binario en la caché; "" si no hay malla
	CacheHit    bool         `json:"cacheHit"`
	DurationMs  int64        `json:"durationMs"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Log         string       `json:"log"`     // salida de OpenSCAD (stderr+stdout), recortada a 64 KB
	Backend     string       `json:"backend"` // el que se usó de verdad (manifold puede caer a cgal)
	Version     string       `json:"version"` // versión de OpenSCAD
}

// Engine ejecuta renders con caché en disco y una cola por workspace.
// Es seguro para uso concurrente.
type Engine struct {
	impl *engine
}

// NewEngine crea el motor. cacheDir es <datos>/cache/meshes (se crea si falta).
func NewEngine(cacheDir string) (*Engine, error) {
	impl, err := newEngine(cacheDir)
	if err != nil {
		return nil, err
	}
	return &Engine{impl: impl}, nil
}

// Render renderiza req con opts. Si ya hay un render en curso con la misma
// WorkspaceKey, ese se cancela (gana el más nuevo). Nunca devuelve error por
// un fallo de OpenSCAD: eso viaja en Result.Status/Diagnostics. error es solo
// para fallos del propio motor (no se pudo leer el archivo, disco lleno…).
func (e *Engine) Render(ctx context.Context, req Request, opts Options) (Result, error) {
	return e.impl.render(ctx, req, opts)
}

// Cancel cancela el render en curso de una WorkspaceKey, si lo hay.
func (e *Engine) Cancel(workspaceKey string) { e.impl.cancel(workspaceKey) }

// Close cancela todo lo que esté corriendo.
func (e *Engine) Close() { e.impl.close() }

// WriteRenderErrors escribe <dir>/RENDER_ERRORS.md con el resultado fallido,
// en el formato que leen los agentes (ver assets/workspace/RULES.md.tmpl).
func WriteRenderErrors(dir string, r Result) error { return writeRenderErrors(dir, r) }

// ClearRenderErrors borra <dir>/RENDER_ERRORS.md si existe.
func ClearRenderErrors(dir string) error { return clearRenderErrors(dir) }
