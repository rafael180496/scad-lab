package mcp

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"scadlab/backend/checkpoint"
	"scadlab/backend/estimate"
	"scadlab/backend/exporter"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/scad"
	"scadlab/backend/scadwasm"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Las descripciones están en inglés a propósito: los modelos las leen mejor
// así y no se muestran al usuario. Lo que sí sale en su idioma son los mensajes
// de error y los avisos (i18n.Msg), que el agente suele repetirle.

const (
	annReadOnly = "ro"
	annWrite    = "rw"
)

func annotations(kind string) map[string]any {
	return map[string]any{"readOnlyHint": kind == annReadOnly, "destructiveHint": false, "openWorldHint": false}
}

const fileHelp = "Checkpoint file name in the workspace root (e.g. 003-add-hinge.scad). Omit to use the newest checkpoint."

// tools es el catálogo, en el orden en que se muestra al agente.
func (e *env) tools() []Tool {
	return []Tool{
		{
			Name: "list_checkpoints",
			Description: "List the workspace's checkpoints (immutable NNN-slug.scad files) as a tree: parent, description, children, modification time and the last known render status. " +
				"Also reports `activeParent`: the checkpoint the user chose to branch from (empty if none), which create_checkpoint uses as the default parent. Start here to see where the design stands.",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.listCheckpoints,
		},
		{
			Name:        "read_checkpoint",
			Description: "Return the OpenSCAD source of a checkpoint.",
			InputSchema: obj(map[string]any{"file": strProp(fileHelp)}),
			Annotations: annotations(annReadOnly),
			handler:     e.readCheckpoint,
		},
		{
			Name: "create_checkpoint",
			Description: "Create a NEW checkpoint file NNN-slug.scad from the given source. Never overwrites or edits an existing checkpoint: to change anything, create a new one. " +
				"The @checkpoint header line is written for you (an existing first-line header in source is replaced). " +
				"Every top-level part must use color(). Then call render to validate it.",
			InputSchema: obj(map[string]any{
				"parent":      strProp("Checkpoint this one derives from (e.g. 003-base-box.scad). Omit to use the workspace's active parent (see `activeParent` in list_checkpoints; it advances to each new child by itself) or, if there is none, the newest checkpoint; the very first checkpoint of a workspace has no parent."),
				"label":       strProp("Short title; becomes the slug of the file name (e.g. \"add hinge\" -> 004-add-hinge.scad)."),
				"description": strProp("One-line description stored in the header (defaults to the label)."),
				"source":      strProp("Complete OpenSCAD source code of the model."),
			}, "label", "source"),
			Annotations: annotations(annWrite),
			handler:     e.createCheckpoint,
		},
		{
			Name: "render",
			Description: "Render a checkpoint with OpenSCAD (same engine and cache as the ScadLab app; if no OpenSCAD is installed it uses the built-in WebAssembly engine, reported in `engine`). Returns the status, diagnostics with line numbers (and the offending code line), duration, " +
				"mesh size/volume/triangle count/watertightness and whether it fits the printer plate. Updates RENDER_ERRORS.md like the app does (newest checkpoint only). " +
				"On errors, fix the model in a NEW checkpoint (create_checkpoint with parent=<this file>), never by editing this one.",
			InputSchema: obj(map[string]any{"file": strProp(fileHelp)}),
			Annotations: annotations(annWrite),
			handler:     e.renderTool,
		},
		{
			Name:        "lint",
			Description: "Check OpenSCAD code without rendering it: syntax/semantic errors reported by OpenSCAD plus ScadLab rules SL001-SL009 (checkpoint header, missing color(), missing includes, high $fn, nested minkowski, reassigned variables, Customizer ranges). Give a checkpoint file or raw source.",
			InputSchema: obj(map[string]any{
				"file":   strProp("Checkpoint to check (defaults to the newest). With `source`, it is only used as the file name for the header rules."),
				"source": strProp("Code to check instead of a file on disk."),
			}),
			Annotations: annotations(annReadOnly),
			handler:     e.lintTool,
		},
		{
			Name:        "format",
			Description: "Format OpenSCAD source with ScadLab's formatter (indentation from the app settings). Returns the formatted code; it never writes files. Fails only on an unterminated string or comment.",
			InputSchema: obj(map[string]any{"source": strProp("OpenSCAD code to format.")}, "source"),
			Annotations: annotations(annReadOnly),
			handler:     e.formatTool,
		},
		{
			Name: "measure",
			Description: "Measure a checkpoint's rendered mesh: bounding box and size (mm), volume, surface area, triangles, watertightness, and whether it fits the workspace printer's build plate (overflowMm per axis if not). " +
				"Also returns the model's parts (one per color(), each with size and volume) and a print ESTIMATE per part and in total: weight, filament length and cost, computed with the infill, perimeters, line width and filament diameter from the app settings " +
				"(shell = min(volume, area x wall) + interior x infill; no supports, brim or AMS purge). The estimate's assumptions are listed in the result; the slicer gives the exact figure.",
			InputSchema: obj(map[string]any{"file": strProp(fileHelp)}),
			Annotations: annotations(annWrite),
			handler:     e.measureTool,
		},
		{
			Name: "preview",
			Description: "Render a PNG picture of a checkpoint with OpenSCAD so you can SEE the part (colors from color() are kept). " +
				"Views: iso (default), iso-back, front (seen from -Y), back, right (seen from +X), left, top, bottom. Look at it after rendering to catch shapes that are valid but wrong.",
			InputSchema: obj(map[string]any{
				"file": strProp(fileHelp),
				"view": enumProp("Camera view. Default iso.", viewNames...),
				"size": map[string]any{"type": "integer", "description": "Image side in pixels, 256-1024. Default 640.", "minimum": minPreviewSize, "maximum": maxPreviewSize},
			}),
			Annotations: annotations(annReadOnly),
			handler:     e.previewTool,
		},
		{
			Name:        "printer_info",
			Description: "Describe the workspace's target printer (Bambu Lab): build volume, nozzle, multicolor/AMS slots, key FDM design rules, the default material, and the detected OpenSCAD. Read it before designing.",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.printerInfo,
		},
		{
			Name: "export",
			Description: "Export a checkpoint into <workspace>/exports/: stl (one mesh), 3mf (multicolor 3MF for Bambu Studio, one part per color with its extruder) or stl-per-color (zip with one STL per color). " +
				"Returns the file path, the parts and warnings (e.g. more colors than AMS slots). Overwrites a previous export of the same checkpoint and format.",
			InputSchema: obj(map[string]any{
				"file":   strProp(fileHelp),
				"format": enumProp("Export format. Default 3mf.", exporter.FormatSTL, exporter.Format3MF, exporter.FormatSTLPerColor),
			}),
			Annotations: annotations(annWrite),
			handler:     e.exportTool,
		},
		{
			Name:        "list_templates",
			Description: "List the parametric OpenSCAD templates that ship with ScadLab (box with lid, electronics enclosure, bracket, gear, hinge, ...). Prefer starting from one when the request resembles it.",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.listTemplates,
		},
		{
			Name:        "get_template",
			Description: "Return the source of a template (see list_templates). Adjust its parameters and pass it to create_checkpoint: the header line is rewritten for you.",
			InputSchema: obj(map[string]any{"name": strProp("Template name from list_templates, e.g. knob or knob.scad.")}, "name"),
			Annotations: annotations(annReadOnly),
			handler:     e.getTemplate,
		},
	}
}

// --- utilidades de salida ---------------------------------------------------

func round(x float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(x*p) / p
}

type vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

func vecOf(a [3]float64) vec3 {
	return vec3{round(a[0], 3), round(a[1], 3), round(a[2], 3)}
}

type meshOut struct {
	SizeMM     vec3    `json:"sizeMm"`
	MinMM      vec3    `json:"minMm"`
	MaxMM      vec3    `json:"maxMm"`
	VolumeMM3  float64 `json:"volumeMm3"`
	VolumeCM3  float64 `json:"volumeCm3"`
	AreaMM2    float64 `json:"areaMm2"`
	Triangles  int     `json:"triangles"`
	Watertight bool    `json:"watertight"`
}

func meshOf(i mesh.Info) meshOut {
	return meshOut{
		SizeMM: vecOf(i.Size), MinMM: vecOf(i.Min), MaxMM: vecOf(i.Max),
		VolumeMM3: round(i.VolumeMM3, 2), VolumeCM3: round(i.VolumeMM3/1000, 3), AreaMM2: round(i.AreaMM2, 2),
		Triangles: i.Triangles, Watertight: i.Watertight,
	}
}

type printerOut struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BuildVolumeMM vec3   `json:"buildVolumeMm"`
}

func printerOf(p printers.Printer) printerOut {
	return printerOut{ID: p.ID, Name: p.Name, BuildVolumeMM: vec3{p.BuildVolume.X, p.BuildVolume.Y, p.BuildVolume.Z}}
}

// fits dice si el tamaño cabe en el volumen de impresión y, si no, cuánto se
// pasa en cada eje (solo los ejes que se pasan). El cálculo es parts.Fit, el
// mismo que usa el visor.
func fits(size [3]float64, v printers.Volume) (bool, map[string]float64) {
	ok, over := parts.Fit(size, v)
	out := map[string]float64{}
	for axis, mm := range map[string]float64{"x": over.X, "y": over.Y, "z": over.Z} {
		if mm > 0 {
			out[axis] = mm
		}
	}
	return ok, out
}

func msgf(m i18n.Msg, args ...any) string { return i18n.T(m, args...) }

// --- list_checkpoints / read_checkpoint / create_checkpoint -----------------

var numRe = regexp.MustCompile(`^(\d+)-`)

func checkpointNumber(name string) int {
	if m := numRe.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return -1
}

var renderErrorsFileRe = regexp.MustCompile("(?m)^- (?:Archivo|File): `([^`]+)`")

// failingFile es el checkpoint que nombra RENDER_ERRORS.md ("" si no hay). Es el
// único dato de «el último render falló» que sobrevive entre procesos: la app y
// este servidor no comparten memoria.
func (e *env) failingFile() string {
	raw, err := os.ReadFile(filepath.Join(e.ws, "RENDER_ERRORS.md"))
	if err != nil {
		return ""
	}
	if m := renderErrorsFileRe.FindSubmatch(raw); m != nil {
		return string(m[1])
	}
	return ""
}

type checkpointOut struct {
	File        string   `json:"file"`
	Parent      string   `json:"parent"`
	Description string   `json:"description"`
	Children    []string `json:"children"`
	Modified    string   `json:"modified"`
	Render      string   `json:"render"` // "ok" | "error" | "unknown": lo último que se sabe
}

func (e *env) listCheckpoints(_ context.Context, _ map[string]any) (*toolResult, error) {
	files, err := workspace.Scan(e.ws)
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		a, b := checkpointNumber(files[i].FileName), checkpointNumber(files[j].FileName)
		if a != b {
			return a < b
		}
		return files[i].FileName < files[j].FileName
	})
	children := map[string][]string{}
	for _, f := range files {
		if f.ParentFile != "" {
			children[f.ParentFile] = append(children[f.ParentFile], f.FileName)
		}
	}
	failing := e.failingFile()
	e.mu.Lock()
	known := make(map[string]string, len(e.results))
	for k, r := range e.results {
		known[k] = r.Status
	}
	e.mu.Unlock()

	out := make([]checkpointOut, 0, len(files))
	for _, f := range files {
		state := "unknown"
		switch known[f.FileName] {
		case render.StatusOK:
			state = "ok"
		case render.StatusError:
			state = "error"
		default:
			if failing == f.FileName {
				state = "error"
			}
		}
		kids := children[f.FileName]
		if kids == nil {
			kids = []string{}
		}
		out = append(out, checkpointOut{
			File: f.FileName, Parent: f.ParentFile, Description: f.Description, Children: kids,
			Modified: time.Unix(f.Mtime, 0).UTC().Format(time.RFC3339), Render: state,
		})
	}
	return jsonResult(map[string]any{
		"workspace":    e.ws,
		"latest":       checkpoint.Latest(e.ws),
		"activeParent": checkpoint.ActiveParent(e.ws),
		"checkpoints":  out,
	})
}

func (e *env) readCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	_, path, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return textResult(string(raw)), nil
}

// maxSourceBytes acota el código de un checkpoint: un fuente de 2 MB no es un
// modelo, es un volcado.
const maxSourceBytes = 2 << 20

func (e *env) createCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	label := strings.TrimSpace(strArg(args, "label"))
	source := strArg(args, "source")
	if label == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `label`: un título corto para el nombre del archivo", EN: "`label` is required: a short title for the file name"})
	}
	if strings.TrimSpace(source) == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `source`: el código OpenSCAD del modelo", EN: "`source` is required: the OpenSCAD code of the model"})
	}
	if len(source) > maxSourceBytes {
		return nil, i18n.New(i18n.Msg{ES: "el código es demasiado grande (máximo 2 MB)", EN: "the source is too large (2 MB maximum)"})
	}
	parent := strArg(args, "parent")
	var name string
	var err error
	if parent == "" {
		// Sin padre se continúa desde el padre activo —el que el usuario eligió
		// con «Ramificar desde aquí», que avanza con cada hijo— y, si no hay,
		// desde el checkpoint más nuevo; el primero de un workspace nace sin
		// padre. checkpoint.Create avanza el activo cuando el padre lo era.
		parent = checkpoint.ActiveParent(e.ws)
		if parent == "" {
			parent = checkpoint.Latest(e.ws)
		}
	}
	if parent == "" {
		name, err = checkpoint.CreateRoot(e.ws, label, strArg(args, "description"), source)
	} else {
		name, err = checkpoint.Create(e.ws, parent, label, strArg(args, "description"), source)
	}
	if err != nil {
		return nil, err
	}
	desc := strings.TrimSpace(strArg(args, "description"))
	if desc == "" {
		desc = label
	}
	return jsonResult(map[string]any{
		"file":         name,
		"parent":       parent,
		"activeParent": checkpoint.ActiveParent(e.ws),
		"description":  strings.Join(strings.Fields(desc), " "),
		"next":         msgf(i18n.Msg{ES: "Valídalo con render (file=%s).", EN: "Validate it with render (file=%s)."}, name),
	})
}

// --- render -----------------------------------------------------------------

type diagOut struct {
	Severity string `json:"severity"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
	File     string `json:"file,omitempty"` // include, si el mensaje apunta a otro archivo
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"` // la línea de código afectada
}

// maxDiagnostics acota la lista: un modelo con cientos de warnings iguales no
// le aporta nada más al agente y gasta su contexto.
const maxDiagnostics = 40

// diagnosticsOut convierte los diagnósticos del motor, agregando la línea de
// código que marca cada uno (el agente corrige sin tener que releer el archivo).
func (e *env) diagnosticsOut(name string, ds []render.Diagnostic) []diagOut {
	cache := map[string][]string{}
	lines := func(file string) []string {
		if file == "" {
			file = name
		}
		if l, ok := cache[file]; ok {
			return l
		}
		var l []string
		if p := filepath.Join(e.ws, filepath.FromSlash(file)); within(e.ws, p) {
			if raw, err := os.ReadFile(p); err == nil {
				l = strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
			}
		}
		cache[file] = l
		return l
	}
	out := make([]diagOut, 0, len(ds))
	for i, d := range ds {
		if i >= maxDiagnostics {
			break
		}
		o := diagOut{Severity: d.Severity, Line: d.Line, Col: d.Col, File: d.File, Message: d.Message}
		if d.Line > 0 {
			if l := lines(d.File); d.Line <= len(l) {
				o.Code = strings.TrimSpace(l[d.Line-1])
			}
		}
		out = append(out, o)
	}
	return out
}

// tailLines devuelve las últimas n líneas no vacías de s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// failedRender arma el resultado de un render que no produjo malla: mismo JSON
// que uno bueno, con isError para que el agente lo trate como algo a corregir.
func (e *env) failedRender(name string, res render.Result) (*toolResult, error) {
	out := map[string]any{
		"file":        name,
		"status":      res.Status,
		"durationMs":  res.DurationMs,
		"diagnostics": e.diagnosticsOut(name, res.Diagnostics),
	}
	switch res.Status {
	case render.StatusMissing:
		out["message"] = msgf(i18n.Msg{
			ES: "OpenSCAD no está instalado o no se encontró. Pídele al usuario que lo instale o indique su ruta en ScadLab → Configuración → OpenSCAD.",
			EN: "OpenSCAD is not installed or was not found. Ask the user to install it or set its path in ScadLab → Settings → OpenSCAD.",
		})
	case render.StatusCancelled:
		out["message"] = msgf(i18n.Msg{
			ES: "El render se canceló (otro render del mismo archivo lo reemplazó, o se canceló la petición).",
			EN: "The render was cancelled (a newer render of the same file replaced it, or the request was cancelled).",
		})
	default:
		located := false
		for _, d := range res.Diagnostics {
			if d.Severity == render.SeverityError && d.Line > 0 {
				located = true
			}
		}
		if !located && res.Log != "" {
			out["logTail"] = tailLines(res.Log, 15)
		}
		out["message"] = msgf(i18n.Msg{
			ES: "El modelo no renderiza. Corrígelo en un checkpoint NUEVO (create_checkpoint con parent=%s), nunca editando este.",
			EN: "The model does not render. Fix it in a NEW checkpoint (create_checkpoint with parent=%s), never by editing this one.",
		}, name)
	}
	r, err := jsonResult(out)
	if err != nil {
		return nil, err
	}
	r.IsError = true
	return r, nil
}

func (e *env) renderTool(ctx context.Context, args map[string]any) (*toolResult, error) {
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	res, s, err := e.render(ctx, name)
	if err != nil {
		return nil, err
	}
	if res.Status != render.StatusOK {
		return e.failedRender(name, res)
	}
	info, err := mesh.InfoFromFile(res.MeshPath)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la malla renderizada: %w", EN: "could not read the rendered mesh: %w"}, err)
	}
	pr := e.printer(s)
	ok, over := fits(info.Size, pr.BuildVolume)
	out := map[string]any{
		"file":            name,
		"status":          res.Status,
		"durationMs":      res.DurationMs,
		"cacheHit":        res.CacheHit,
		"backend":         res.Backend,
		"openscadVersion": res.Version,
		"engine":          res.Engine,
		"diagnostics":     e.diagnosticsOut(name, res.Diagnostics),
		"mesh":            meshOf(info),
		"printer":         printerOf(pr),
		"fitsBuildVolume": ok,
	}
	if res.Engine == render.EngineWASM {
		out["engineNote"] = engineNote()
	}
	if !ok {
		out["overflowMm"] = over
	}
	if warns := meshWarnings(info); len(warns) > 0 {
		out["warnings"] = warns
	}
	return jsonResult(out)
}

// meshWarnings son los avisos de forma que el agente querrá saber.
func meshWarnings(info mesh.Info) []string {
	var w []string
	if !info.Watertight {
		w = append(w, msgf(i18n.Msg{
			ES: "La malla no es cerrada (watertight): el volumen y el peso no son fiables y el slicer puede quejarse. Revisa caras coincidentes o piezas que solo se tocan en una arista.",
			EN: "The mesh is not watertight: volume and weight are unreliable and the slicer may complain. Check coincident faces or parts that only touch along an edge.",
		}))
	}
	if math.Abs(info.Min[2]) > 0.05 {
		w = append(w, msgf(i18n.Msg{
			ES: "El modelo no apoya en z=0 (z mínima = %.2f mm): en la placa quedaría flotando o enterrado.",
			EN: "The model does not rest on z=0 (minimum z = %.2f mm): on the plate it would float or be buried.",
		}, info.Min[2]))
	}
	return w
}

// --- lint / format ----------------------------------------------------------

func (e *env) lintTool(ctx context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	source := strArg(args, "source")
	file := strArg(args, "file")
	fileName := ""
	if source == "" {
		name, path, err := e.checkpointFile(file)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		source, fileName = string(raw), name
	} else if file != "" {
		if err := checkpoint.ValidName(file); err != nil {
			return nil, err
		}
		fileName = file
	}
	files, err := workspace.Scan(e.ws)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.FileName
	}
	tool := e.openscad(ctx, s.OpenSCADPath)
	ds := scad.Lint(ctx, source, scad.LintOptions{
		WorkspaceDir:  e.ws,
		FileName:      fileName,
		OpenSCADPath:  tool.Path,
		WASM:          e.wasm,
		ExistingFiles: names,
	})
	if ds == nil {
		ds = []scad.Diagnostic{}
	}
	counts := map[string]int{scad.SeverityError: 0, scad.SeverityWarning: 0, scad.SeverityInfo: 0}
	for _, d := range ds {
		counts[d.Severity]++
	}
	out := map[string]any{
		"file":        fileName,
		"errors":      counts[scad.SeverityError],
		"warnings":    counts[scad.SeverityWarning],
		"infos":       counts[scad.SeverityInfo],
		"diagnostics": ds,
	}
	switch {
	case e.usesWASM(tool):
		out["note"] = engineNote()
	case !tool.Found:
		out["note"] = msgf(i18n.Msg{
			ES: "OpenSCAD no está disponible: solo se aplicaron las reglas propias de ScadLab (sin chequeo de sintaxis).",
			EN: "OpenSCAD is not available: only ScadLab's own rules were applied (no syntax check).",
		})
	}
	return jsonResult(out)
}

func (e *env) formatTool(_ context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	source := strArg(args, "source")
	if strings.TrimSpace(source) == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `source`: el código a formatear", EN: "`source` is required: the code to format"})
	}
	out, err := scad.Format(source, scad.FormatOptions{IndentSize: s.EditorIndent})
	if err != nil {
		return nil, err
	}
	return textResult(out), nil
}

// --- measure ----------------------------------------------------------------

func (e *env) measureTool(ctx context.Context, args map[string]any) (*toolResult, error) {
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	res, s, err := e.render(ctx, name)
	if err != nil {
		return nil, err
	}
	if res.Status != render.StatusOK {
		return e.failedRender(name, res)
	}
	info, err := mesh.InfoFromFile(res.MeshPath)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la malla renderizada: %w", EN: "could not read the rendered mesh: %w"}, err)
	}
	pr := e.printer(s)
	ok, over := fits(info.Size, pr.BuildVolume)
	mat := e.material(s)

	// Partes por color (las mismas que ve el visor) y estimación de impresión.
	// Si no se pueden repartir, se estima el modelo entero como una sola parte
	// y se avisa: medir no debe fallar porque el 3MF no salió.
	var warns []string
	partList, perr := e.modelParts(ctx, s, name, res.MeshPath, info)
	if perr != nil {
		warns = append(warns, msgf(i18n.Msg{
			ES: "No se pudieron separar las partes por color (%v): la estimación trata el modelo como una sola parte.",
			EN: "The parts by color could not be separated (%v): the estimate treats the model as a single part.",
		}, perr))
		partList = []parts.Part{{Index: 0, Name: "Default", Color: "#F9D72C", Uncolored: true, Info: info}}
	}
	opts := estimate.OptionsFromSettings(s)
	mats := e.estimateMaterials()
	if !hasMaterial(mats, opts.DefaultMaterialID) {
		// El material por defecto ya no existe (se borró de la base): se estima
		// con el mismo respaldo de siempre (material()) y se avisa.
		mats = append(mats, estimate.Material{ID: mat.ID, Name: mat.Name, DensityGCm3: mat.DensityGCm3})
		warns = append(warns, msgf(i18n.Msg{
			ES: "El material por defecto %q no existe: la estimación usa %s.",
			EN: "The default material %q does not exist: the estimate uses %s.",
		}, opts.DefaultMaterialID, mat.Name))
		opts.DefaultMaterialID = mat.ID
	}
	est, err := estimate.Estimate(estimate.ClosedAsWhole(estimateParts(partList), info.Watertight), mats, opts)
	if err != nil {
		return nil, err
	}

	out := map[string]any{
		"file":             name,
		"mesh":             meshOf(info),
		"printer":          printerOf(pr),
		"fitsBuildVolume":  ok,
		"estimatedWeightG": round(est.Total.MassG, 2),
		"material":         map[string]any{"id": mat.ID, "name": mat.Name, "densityGCm3": mat.DensityGCm3},
		"parts":            partsOut(partList),
		"estimate":         estimateOut(est, opts.Normalized()),
	}
	if !ok {
		out["overflowMm"] = over
	}
	warns = append(warns, meshWarnings(info)...)
	if len(warns) > 0 {
		out["warnings"] = warns
	}
	return jsonResult(out)
}

// modelParts devuelve las partes por color del render (la caché compartida con
// la app: lo que generó el visor se reutiliza y al revés).
func (e *env) modelParts(ctx context.Context, s store.Settings, name, meshPath string, total mesh.Info) ([]parts.Part, error) {
	if e.parts == nil || e.engine == nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "el motor de render no está disponible: %v", EN: "the render engine is not available: %v"}, e.engineErr)
	}
	opts, _ := e.renderOptions(ctx, s)
	m, err := e.parts.Ensure(ctx, parts.Source{
		Engine: e.engine,
		// Cola propia: repartir en partes no cancela el render del checkpoint.
		Req:         render.Request{WorkspaceKey: "mcp:" + e.ws + ":" + name + ":parts", WorkspaceDir: e.ws, FileName: name},
		Opts:        opts,
		ArtifactDir: filepath.Join(e.dataDir, "cache", "exports"),
		MeshPath:    meshPath,
	})
	if err != nil {
		return nil, err
	}
	return m.Parts, nil
}

func hasMaterial(mats []estimate.Material, id string) bool {
	for _, m := range mats {
		if m.ID == id && m.DensityGCm3 > 0 {
			return true
		}
	}
	return false
}

func estimateParts(ps []parts.Part) []estimate.Part {
	out := make([]estimate.Part, len(ps))
	for i, p := range ps {
		out[i] = estimate.PartOf(p.Index, p.Info)
	}
	return out
}

type partOut struct {
	Index      int     `json:"index"`
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	Uncolored  bool    `json:"uncolored"`
	SizeMM     vec3    `json:"sizeMm"`
	VolumeMM3  float64 `json:"volumeMm3"`
	AreaMM2    float64 `json:"areaMm2"`
	Triangles  int     `json:"triangles"`
	Watertight bool    `json:"watertight"`
}

func partsOut(ps []parts.Part) []partOut {
	out := make([]partOut, len(ps))
	for i, p := range ps {
		out[i] = partOut{
			Index: p.Index, Name: p.Name, Color: p.Color, Uncolored: p.Uncolored,
			SizeMM: vecOf(p.Info.Size), VolumeMM3: round(p.Info.VolumeMM3, 2), AreaMM2: round(p.Info.AreaMM2, 2),
			Triangles: p.Info.Triangles, Watertight: p.Info.Watertight,
		}
	}
	return out
}

// estimateOut es la estimación para el agente: los parámetros usados, cada parte,
// el total y los supuestos (en el idioma de la app).
func estimateOut(r estimate.Result, o estimate.Options) map[string]any {
	ps := make([]map[string]any, len(r.Parts))
	for i, p := range r.Parts {
		ps[i] = map[string]any{
			"index": p.Index, "materialId": p.MaterialID,
			"volumeMm3": round(p.VolumeMm3, 2), "effectiveMm3": round(p.EffectiveMm3, 2),
			"massG": round(p.MassG, 2), "lengthM": round(p.LengthM, 2), "cost": round(p.Cost, 3),
		}
	}
	return map[string]any{
		"infillPercent": o.InfillPercent, "wallLoops": o.WallLoops,
		"lineWidthMm": o.LineWidthMm, "filamentDiameterMm": o.FilamentDiameterMm,
		"parts": ps,
		"total": map[string]any{
			"massG": round(r.Total.MassG, 2), "lengthM": round(r.Total.LengthM, 2), "cost": round(r.Total.Cost, 3),
		},
		"currency":    r.Currency,
		"assumptions": r.Assumptions,
	}
}

// --- printer_info -----------------------------------------------------------

// fdmRules son las reglas de diseño que el workspace le da al agente (las
// mismas de RULES.md.tmpl y de la skill bambu-lab-printers). Van en inglés: es
// dato para el modelo.
var fdmRules = []string{
	"Overhangs up to 45 degrees; steeper needs supports or a redesign.",
	"Minimum wall 0.8 mm (2 perimeters of a 0.4 mm nozzle).",
	"Clearance 0.2 mm for a press fit, 0.3-0.4 mm for a sliding fit.",
	"0.5 mm chamfer on the bottom edge against elephant foot.",
	"The whole model must fit the build volume; if it does not, split it into parts and say so.",
	"Every top-level part needs color(); the multicolor 3MF is one part per color (max colors = multicolorSlots).",
	"Model rests on z=0.",
}

func (e *env) printerInfo(ctx context.Context, _ map[string]any) (*toolResult, error) {
	s := e.settings()
	pr := e.printer(s)
	mat := e.material(s)
	tool := e.openscad(ctx, s.OpenSCADPath)
	scadInfo := map[string]any{"found": tool.Found, "backend": s.RenderBackend}
	switch {
	case tool.Found:
		scadInfo["engine"] = render.EngineNative
		scadInfo["path"] = tool.Path
		scadInfo["version"] = tool.Version
	case e.wasm.Available():
		// Sin OpenSCAD instalado, ScadLab renderiza con el motor integrado.
		scadInfo["engine"] = render.EngineWASM
		scadInfo["version"] = scadwasm.Version
		scadInfo["note"] = engineNote()
	}
	verify := pr.Verify
	if verify == nil {
		verify = []string{}
	}
	return jsonResult(map[string]any{
		"workspace": e.wsName(),
		"printer": map[string]any{
			"id":                pr.ID,
			"name":              pr.Name,
			"buildVolumeMm":     vec3{pr.BuildVolume.X, pr.BuildVolume.Y, pr.BuildVolume.Z},
			"nozzleMm":          pr.NozzleMM,
			"multicolorSlots":   pr.MulticolorSlots,
			"multicolorSystem":  pr.MulticolorSys,
			"enclosed":          pr.Enclosed,
			"heatedChamber":     pr.HeatedChamber,
			"notes":             pr.Notes,
			"unconfirmedFields": verify,
		},
		"defaultMaterial": map[string]any{"id": mat.ID, "name": mat.Name, "densityGCm3": mat.DensityGCm3},
		"fdmRules":        fdmRules,
		"openscad":        scadInfo,
	})
}

// --- export -----------------------------------------------------------------

func (e *env) exportTool(ctx context.Context, args map[string]any) (*toolResult, error) {
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	format := strArg(args, "format")
	if format == "" {
		format = exporter.Format3MF
	}
	switch format {
	case exporter.FormatSTL, exporter.Format3MF, exporter.FormatSTLPerColor:
	default:
		return nil, i18n.Errorf(i18n.Msg{ES: "formato de exportación desconocido: %q (usa stl, 3mf o stl-per-color)", EN: "unknown export format: %q (use stl, 3mf or stl-per-color)"}, format)
	}
	s := e.settings()
	if e.engine == nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "el motor de render no está disponible: %v", EN: "the render engine is not available: %v"}, e.engineErr)
	}
	opts, _ := e.renderOptions(ctx, s)
	job := exporter.Job{
		Engine: e.engine,
		// Cola propia: exportar no cancela el render del mismo checkpoint.
		Req:      render.Request{WorkspaceKey: "mcp:" + e.ws + ":" + name + ":export", WorkspaceDir: e.ws, FileName: name},
		Opts:     opts,
		CacheDir: filepath.Join(e.dataDir, "cache", "exports"),
		Printer:  e.printer(s),
	}
	p, err := exporter.Prepare(ctx, job, format)
	if err != nil {
		return nil, err
	}
	dir, err := e.exportsDir()
	if err != nil {
		return nil, err
	}
	slug := workspace.Slug(e.wsName())
	if slug == "" {
		slug = "workspace"
	}
	dest := filepath.Join(dir, exporter.SuggestedName(slug, name, format))
	if err := p.Write(dest); err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo guardar la exportación: %w", EN: "could not save the export: %w"}, err)
	}
	rel, _ := filepath.Rel(e.ws, dest)
	return jsonResult(map[string]any{
		"file":         name,
		"format":       format,
		"path":         dest,
		"relativePath": filepath.ToSlash(rel),
		"parts":        p.Infos(),
		"warnings":     p.Warnings(),
	})
}

// exportsDir devuelve <ws>/exports (la crea) comprobando que no sea un enlace
// que saque las exportaciones del workspace.
func (e *env) exportsDir() (string, error) {
	dir := filepath.Join(e.ws, "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || !within(e.ws, real) {
		return "", i18n.New(i18n.Msg{ES: "la carpeta exports apunta fuera del workspace", EN: "the exports folder points outside the workspace"})
	}
	return real, nil
}
