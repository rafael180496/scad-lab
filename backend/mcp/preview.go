package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"scadlab/backend/i18n"
	"scadlab/backend/render"
)

// Tamaño de la imagen del preview: un lado, en píxeles. Un PNG de 1024×1024 con
// una pieza sólida pesa decenas de KB, pero viaja en base64 dentro del contexto
// del agente: más grande no le aporta y le cuesta.
const (
	minPreviewSize     = 256
	maxPreviewSize     = 1024
	defaultPreviewSize = 640
)

// view es una cámara: rotación (rx, ry, rz) del `--camera` de OpenSCAD y si la
// proyección es ortogonal. Las vistas por eje son ortogonales (se pueden medir a
// ojo); iso es en perspectiva, como la vista por defecto de OpenSCAD.
//
// Los ángulos se comprobaron renderizando una pieza asimétrica. Ojo con
// right/left: en la GUI de OpenSCAD «Right» es 90,0,270, que mira desde −X (la
// convención contraria a la de los planos). Aquí `right` se ve desde +X.
type view struct {
	rot   [3]int
	ortho bool
}

var views = map[string]view{
	"iso":      {rot: [3]int{55, 0, 25}},
	"iso-back": {rot: [3]int{55, 0, 205}},
	"front":    {rot: [3]int{90, 0, 0}, ortho: true},   // desde −Y
	"back":     {rot: [3]int{90, 0, 180}, ortho: true}, // desde +Y
	"right":    {rot: [3]int{90, 0, 90}, ortho: true},  // desde +X
	"left":     {rot: [3]int{90, 0, 270}, ortho: true}, // desde −X
	"top":      {rot: [3]int{0, 0, 0}, ortho: true},    // desde +Z
	"bottom":   {rot: [3]int{180, 0, 0}, ortho: true},  // desde −Z
}

// viewNames es el orden en que se ofrecen las vistas.
var viewNames = []string{"iso", "iso-back", "front", "back", "right", "left", "top", "bottom"}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

func (e *env) previewTool(ctx context.Context, args map[string]any) (*toolResult, error) {
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	viewName := strings.ToLower(strings.TrimSpace(strArg(args, "view")))
	if viewName == "" {
		viewName = "iso"
	}
	v, ok := views[viewName]
	if !ok {
		return nil, i18n.Errorf(i18n.Msg{ES: "vista desconocida: %q (usa %s)", EN: "unknown view: %q (use %s)"}, viewName, strings.Join(viewNames, ", "))
	}
	size := intArg(args, "size", defaultPreviewSize)
	size = max(minPreviewSize, min(maxPreviewSize, size))

	s := e.settings()
	tool := e.openscad(ctx, s.OpenSCADPath)
	if !tool.Found && e.wasm.Available() {
		// Sin OpenSCAD instalado no hay OpenGL para su PNG: el motor integrado
		// renderiza la malla y ScadLab la dibuja por software.
		return e.previewRaster(ctx, name, viewName, v, size)
	}
	if !tool.Found {
		return nil, i18n.New(i18n.Msg{
			ES: "OpenSCAD no está instalado o no se encontró. Pídele al usuario que lo instale o indique su ruta en ScadLab → Configuración → OpenSCAD.",
			EN: "OpenSCAD is not installed or was not found. Ask the user to install it or set its path in ScadLab → Settings → OpenSCAD.",
		})
	}
	timeout := time.Duration(s.RenderTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	png, out, err := e.runPreview(ctx, tool.Path, name, v, size, s.RenderBackend != "cgal", timeout)
	if err != nil {
		if ctx.Err() != nil {
			return nil, i18n.New(i18n.Msg{ES: "el preview se canceló", EN: "the preview was cancelled"})
		}
		return e.previewFailure(name, out, err)
	}
	res := &toolResult{Content: []map[string]any{
		imageContent(png, "image/png"),
		textContent(msgf(i18n.Msg{ES: "Preview de %s, vista %s, %d×%d px.", EN: "Preview of %s, %s view, %d×%d px."}, name, viewName, size, size)),
	}}
	return res, nil
}

// previewFailure traduce la salida de OpenSCAD a un error con las líneas del
// modelo, igual que un render.
func (e *env) previewFailure(name, output string, runErr error) (*toolResult, error) {
	ds := render.ParseOutput(output, e.ws, name)
	var errs []render.Diagnostic
	for _, d := range ds {
		if d.Severity == render.SeverityError {
			errs = append(errs, d)
		}
	}
	if len(errs) == 0 {
		msg := tailLines(output, 10)
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		return nil, i18n.Errorf(i18n.Msg{
			ES: "OpenSCAD no pudo generar el preview de %s:\n%s",
			EN: "OpenSCAD could not generate the preview of %s:\n%s",
		}, name, msg)
	}
	var b strings.Builder
	b.WriteString(msgf(i18n.Msg{ES: "El modelo %s no renderiza, no hay preview:", EN: "Model %s does not render, no preview:"}, name))
	for _, d := range e.diagnosticsOut(name, errs) {
		b.WriteString("\n- ")
		if d.Line > 0 {
			fmt.Fprintf(&b, "%s %d: ", msgf(i18n.Msg{ES: "línea", EN: "line"}), d.Line)
		}
		b.WriteString(d.Message)
		if d.Code != "" {
			b.WriteString("  | " + d.Code)
		}
	}
	return nil, errors.New(b.String())
}

// runPreview corre `openscad -o x.png --render …` y devuelve el PNG.
//
// El PNG va a un temporal DENTRO del workspace (<ws>/.scadlab/preview/, que ya
// está en el .gitignore y que el watcher no ve porque no es recursivo): nada se
// escribe fuera de la carpeta del proyecto. Se borra siempre.
//
// --render hace un render completo (no la vista previa rápida de OpenGL) para
// que se vea el resultado real de las operaciones booleanas, con los color() del
// modelo. --viewall --autocenter encuadra la pieza sin importar su tamaño ni
// dónde esté en el espacio.
func (e *env) runPreview(ctx context.Context, bin, name string, v view, size int, manifold bool, timeout time.Duration) ([]byte, string, error) {
	dir := filepath.Join(e.ws, ".scadlab", "preview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	f, err := os.CreateTemp(dir, "preview-*.png")
	if err != nil {
		return nil, "", err
	}
	out := f.Name()
	_ = f.Close()
	defer os.Remove(out)

	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var output string
	var runErr error
	// Como mucho dos intentos: con --backend y sin él (OpenSCAD 2021.01 no lo
	// conoce).
	for range 2 {
		output, runErr = runOpenSCADPNG(rctx, bin, e.ws, name, out, v, size, manifold)
		if runErr == nil || rctx.Err() != nil {
			break
		}
		if manifold && rejectsBackend(output) {
			manifold = false
			continue
		}
		break
	}
	if rctx.Err() != nil && ctx.Err() == nil {
		return nil, output, i18n.Errorf(i18n.Msg{
			ES: "OpenSCAD superó el tiempo máximo (%d s) generando el preview",
			EN: "OpenSCAD exceeded the maximum time (%d s) generating the preview",
		}, int(timeout.Seconds()))
	}
	if runErr != nil {
		return nil, output, runErr
	}
	png, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(png, pngMagic) {
		// OpenSCAD salió con 0 sin escribir una imagen (modelo vacío o 2D).
		return nil, output, i18n.New(i18n.Msg{ES: "OpenSCAD no escribió la imagen", EN: "OpenSCAD did not write the image"})
	}
	return png, output, nil
}

func runOpenSCADPNG(ctx context.Context, bin, dir, file, out string, v view, size int, manifold bool) (string, error) {
	camera := fmt.Sprintf("--camera=0,0,0,%d,%d,%d,500", v.rot[0], v.rot[1], v.rot[2])
	proj := "--projection=perspective"
	if v.ortho {
		proj = "--projection=ortho"
	}
	args := []string{
		"-o", out,
		"--render",
		"--colorscheme=Tomorrow",
		fmt.Sprintf("--imgsize=%d,%d", size, size),
		"--viewall", "--autocenter",
		camera, proj,
	}
	if manifold {
		args = append(args, "--backend=manifold")
	}
	args = append(args, file)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 3 * time.Second
	hideWindow(cmd)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

func rejectsBackend(output string) bool {
	for _, l := range strings.Split(strings.ToLower(output), "\n") {
		if strings.Contains(l, "backend") && (strings.Contains(l, "unknown option") ||
			strings.Contains(l, "unrecognised option") || strings.Contains(l, "unrecognized option")) {
			return true
		}
	}
	return false
}
