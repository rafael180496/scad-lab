package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"scadlab/backend/i18n"
	"scadlab/backend/refmodel"
	"scadlab/backend/render"
)

// Tamaño de la imagen del preview: un lado, en píxeles. Un PNG de 1024×1024 con
// una pieza sólida pesa decenas de KB, pero viaja en base64 dentro del contexto
// del agente: más grande no le aporta y le cuesta.
const (
	minPreviewSize     = 256
	maxPreviewSize     = 1024
	defaultPreviewSize = 640
	// defaultShotSize es el lado de la imagen de `preview` sin `size` (S17): una
	// sola vista de 384 px pesa un tercio de una de 640 y alcanza para ver la
	// forma. Las exportaciones PNG siguen en defaultPreviewSize.
	defaultShotSize = 384
	// maxPreviewViews es cuántas vistas puede pedir `views` en una llamada.
	maxPreviewViews = 8
)

// view es una cámara: rotación (rx, ry, rz) del `--camera` de OpenSCAD y si la
// proyección es ortogonal. La tabla es la de backend/refmodel (los ángulos y su
// justificación están allí): la vista previa de una referencia y la del modelo
// se miran desde los mismos lados.
type view struct {
	rot   [3]int
	ortho bool
}

var views = func() map[string]view {
	m := make(map[string]view, len(refmodel.Views))
	for name, v := range refmodel.Views {
		m[name] = view{rot: v.Rot, ortho: v.Ortho}
	}
	return m
}()

// viewNames es el orden en que se ofrecen las vistas.
var viewNames = refmodel.ViewNames

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// shot es una imagen ya dibujada y la línea de texto que la acompaña.
type shot struct {
	png  []byte
	note string
}

// viewFromArgs lee la cámara que pidió el agente: una vista con nombre (`view`) o
// una libre (`rotate` = [rx, ry, rz] en grados, la del --camera de OpenSCAD, y
// `ortho`). Devuelve el rótulo de la vista para el texto y para el nombre del
// archivo de una exportación.
func viewFromArgs(args map[string]any) (string, view, error) {
	if raw, has := args["rotate"]; has && raw != nil {
		list, ok := listArg(args, "rotate")
		if !ok {
			return "", view{}, i18n.New(i18n.Msg{ES: "`rotate` lleva tres ángulos [rx, ry, rz] en grados", EN: "`rotate` takes three angles [rx, ry, rz] in degrees"})
		}
		if len(list) != 3 {
			return "", view{}, i18n.New(i18n.Msg{ES: "`rotate` lleva tres ángulos [rx, ry, rz] en grados", EN: "`rotate` takes three angles [rx, ry, rz] in degrees"})
		}
		var v view
		for i, it := range list {
			n, ok := asNum(it)
			if !ok || math.Abs(n) > 3600 {
				return "", view{}, i18n.New(i18n.Msg{ES: "`rotate` lleva tres ángulos numéricos en grados", EN: "`rotate` takes three numeric angles in degrees"})
			}
			v.rot[i] = int(math.Round(n))
		}
		v.ortho = boolArg(args, "ortho", false)
		return fmt.Sprintf("custom-%d_%d_%d", v.rot[0], v.rot[1], v.rot[2]), v, nil
	}
	viewName := strings.ToLower(strings.TrimSpace(strArg(args, "view")))
	if viewName == "" {
		viewName = "iso"
	}
	v, ok := views[viewName]
	if !ok {
		return "", view{}, i18n.Errorf(i18n.Msg{ES: "vista desconocida: %q (usa %s, o `rotate` con tres ángulos)", EN: "unknown view: %q (use %s, or `rotate` with three angles)"}, viewName, strings.Join(viewNames, ", "))
	}
	if _, has := args["ortho"]; has {
		v.ortho = boolArg(args, "ortho", v.ortho)
	}
	return viewName, v, nil
}

// asNum lee un número que llegó como número o como texto.
func asNum(v any) (float64, bool) {
	return numArg(map[string]any{"v": v}, "v")
}

func (e *env) previewTool(ctx context.Context, args map[string]any) (*toolResult, error) {
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	size := intArg(args, "size", defaultShotSize)
	size = max(minPreviewSize, min(maxPreviewSize, size))

	// Una vista por defecto; `views` o `all` piden más.
	type req struct {
		label string
		v     view
	}
	var reqs []req
	switch {
	case boolArg(args, "all", false):
		for _, n := range viewNames {
			reqs = append(reqs, req{n, views[n]})
		}
	case args["views"] != nil:
		list, ok := listArg(args, "views")
		if !ok || len(list) == 0 || len(list) > maxPreviewViews {
			return nil, i18n.Errorf(i18n.Msg{
				ES: "`views` es una lista de 1 a %d vistas (%s)",
				EN: "`views` is a list of 1 to %d views (%s)",
			}, maxPreviewViews, strings.Join(viewNames, ", "))
		}
		seen := map[string]bool{}
		for _, it := range list {
			label, v, err := viewFromArgs(map[string]any{"view": it, "ortho": args["ortho"]})
			if err != nil {
				return nil, err
			}
			if !seen[label] {
				seen[label] = true
				reqs = append(reqs, req{label, v})
			}
		}
	default:
		label, v, err := viewFromArgs(args)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, req{label, v})
	}

	res := &toolResult{}
	var notes []string
	for _, r := range reqs {
		sh, err := e.shoot(ctx, name, r.label, r.v, size)
		if err != nil {
			return nil, err
		}
		res.Content = append(res.Content, imageContent(sh.png, "image/png"))
		notes = append(notes, sh.note)
	}
	res.Content = append(res.Content, textContent(strings.Join(notes, "\n")))
	return res, nil
}

// shoot dibuja el PNG de un checkpoint: con el OpenSCAD instalado si lo hay y, si
// no, con el motor integrado y el rasterizador de ScadLab.
func (e *env) shoot(ctx context.Context, name, viewName string, v view, size int) (shot, error) {
	s := e.settings()
	tool := e.openscad(ctx, s.OpenSCADPath)
	if !tool.Found && e.wasm.Available() {
		// Sin OpenSCAD instalado no hay OpenGL para su PNG: el motor integrado
		// renderiza la malla y ScadLab la dibuja por software.
		return e.previewRaster(ctx, name, viewName, v, size)
	}
	if !tool.Found {
		return shot{}, i18n.New(i18n.Msg{
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
			return shot{}, i18n.New(i18n.Msg{ES: "el preview se canceló", EN: "the preview was cancelled"})
		}
		_, ferr := e.previewFailure(name, out, err)
		return shot{}, ferr
	}
	return shot{png: png, note: msgf(i18n.Msg{ES: "Preview de %s, vista %s, %d×%d px.", EN: "Preview of %s, %s view, %d×%d px."}, name, viewName, size, size)}, nil
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
