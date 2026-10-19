package mcp

import (
	"context"
	"encoding/base64"
	"io"
	"log"
	"sort"
	"strings"
	"sync"

	"scadlab/backend/i18n"
	"scadlab/backend/parts"
	"scadlab/backend/refmodel"
	"scadlab/backend/render"
	"scadlab/backend/scadwasm"
	"scadlab/backend/store"
)

// Toolbox son las herramientas del servidor MCP llamables EN PROCESO: las usa el
// agente del chat de la app (Hermes) sin lanzar un subproceso `--mcp`. Es el
// mismo `env` y los mismos manejadores que el servidor — no hay herramientas
// duplicadas —; cambia el transporte (una llamada de función en vez de
// JSON-RPC por stdio) y que la app le presta su base, su motor de render, el
// motor WebAssembly y la caché de partes en vez de abrir los suyos.
//
// Spec: .claude/specs/s12-hermes-proveedores.md.

// ToolboxOptions configura la caja.
type ToolboxOptions struct {
	// WorkspaceDir es la carpeta del proyecto (absoluta). Vacío = el directorio
	// actual, como en Options.
	WorkspaceDir string
	// Store, Engine, WASM y Parts son los de la app. nil = abrir como lo hace el
	// servidor `--mcp` (Store solo si scadlab.db ya existe). Lo inyectado NO se
	// cierra con Close: es de la app.
	Store  *store.Store
	Engine *render.Engine
	WASM   *scadwasm.Runner
	Parts  *parts.Store
	// Log recibe los diagnósticos. nil = descartarlos.
	Log io.Writer
	// InApp suma las herramientas que solo tienen sentido dentro de la app:
	// list_skills, read_skill e inspect_reference. El servidor `--mcp` no las
	// expone (el agente externo ya lee las skills del disco).
	InApp bool
}

// ToolSpec describe una herramienta para el modelo: su nombre, su descripción y
// el JSON Schema de sus argumentos.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolImage es una imagen que devuelve una herramienta (PNG crudo).
type ToolImage struct {
	MIME string
	Data []byte
}

// ToolOutput es el resultado de una llamada. Un error de la herramienta (el
// checkpoint no existe, el modelo no renderiza) llega con IsError y el texto que
// el modelo tiene que leer para corregirse; no es un error de Go.
type ToolOutput struct {
	Text    string
	Images  []ToolImage
	IsError bool
}

// Toolbox es la caja de herramientas de un workspace. Es segura para llamadas
// concurrentes (las herramientas comparten el env, que ya lo es).
type Toolbox struct {
	e *env

	// mu: las llamadas toman el lado de lectura (pueden ser concurrentes) y
	// Close el de escritura, así que no se cierra el motor bajo una llamada en
	// vuelo.
	mu     sync.RWMutex
	closed bool
	tools  []Tool
}

// NewToolbox abre la caja sobre el workspace.
func NewToolbox(opts ToolboxOptions) (*Toolbox, error) {
	logw := opts.Log
	if logw == nil {
		logw = io.Discard
	}
	lg := log.New(logw, "scadlab-toolbox: ", log.LstdFlags)
	e, err := newEnvWith(Options{WorkspaceDir: opts.WorkspaceDir, Log: logw}, lg, injected{
		store: opts.Store, engine: opts.Engine, wasm: opts.WASM, parts: opts.Parts,
	})
	if err != nil {
		return nil, err
	}
	tools := e.tools()
	if opts.InApp {
		tools = append(tools, e.inAppTools()...)
	}
	return &Toolbox{e: e, tools: tools}, nil
}

// inAppTools son las herramientas que solo ofrece la caja de la app.
func (e *env) inAppTools() []Tool {
	return []Tool{
		{
			Name: "list_skills",
			Description: "List the skills installed in this workspace (.claude/skills/*/SKILL.md) with the name and description of each. " +
				"Skills hold ScadLab's modeling rules and templates (OpenSCAD conventions, Bambu Lab printers and FDM rules, multicolor 3MF, templates, libraries). Check the list before designing something a skill covers, then load it with read_skill.",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.listSkills,
		},
		{
			Name: "read_skill",
			Description: "Return a skill's SKILL.md, or another file inside that skill (e.g. references/x.md, templates/y.scad). " +
				"Reading SKILL.md also lists the skill's other files. Only text files up to 256 KB inside the skill's own folder.",
			InputSchema: obj(map[string]any{
				"name": strProp("Skill name from list_skills (the name or its folder)."),
				"file": strProp("File inside the skill, relative (e.g. references/boxes.md). Omit for SKILL.md."),
			}, "name"),
			Annotations: annotations(annReadOnly),
			handler:     e.readSkill,
		},
		{
			Name: "inspect_reference",
			Description: "Analyze a 3D reference the user attached (STL, 3MF or OBJ in the workspace's refs/ folder): size in mm, volume, surface area, triangles, watertightness and, for a 3MF, its parts by color; " +
				"plus a PNG picture of it. Use it before copying a part. Note that import() cannot be trusted with a 3MF: rebuild it with primitives from the measurements.",
			InputSchema: obj(map[string]any{
				"file": strProp("Reference file in refs/ (e.g. refs/bracket.stl or bracket.stl)."),
				"view": enumProp("Camera view. Default iso.", viewNames...),
			}, "file"),
			Annotations: annotations(annReadOnly),
			handler:     e.inspectReference,
		},
	}
}

func (e *env) inspectReference(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	file := strings.TrimSpace(strArg(args, "file"))
	if file == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `file`: la referencia de refs/ (p. ej. refs/pieza.stl)", EN: "`file` is required: a reference in refs/ (e.g. refs/part.stl)"})
	}
	viewName, _, err := refmodel.ParseView(strArg(args, "view"))
	if err != nil {
		return nil, err
	}
	abs, rel, err := refmodel.ResolveRef(e.ws, file)
	if err != nil {
		return nil, err
	}
	report, err := refmodel.Analyze(abs)
	if err != nil {
		return nil, err
	}
	png, err := refmodel.Preview(abs, viewName, defaultPreviewSize)
	if err != nil {
		return nil, err
	}
	return &toolResult{Content: []map[string]any{
		imageContent(png, "image/png"),
		textContent(report.PromptText(rel, i18n.Lang()) + "\n" + msgf(i18n.Msg{
			ES: "Vista previa de %s, vista %s, %d×%d px, dibujada por ScadLab.",
			EN: "Preview of %s, %s view, %d×%d px, drawn by ScadLab.",
		}, rel, viewName, defaultPreviewSize, defaultPreviewSize)),
	}}, nil
}

// Tools devuelve las herramientas para ofrecérselas al modelo, en el orden en
// que el servidor las lista.
func (t *Toolbox) Tools() []ToolSpec {
	out := make([]ToolSpec, len(t.tools))
	for i, tool := range t.tools {
		out[i] = ToolSpec{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema}
	}
	return out
}

// ErrToolboxClosed: se llamó una herramienta después de Close.
var ErrToolboxClosed = i18n.New(i18n.Msg{ES: "la caja de herramientas está cerrada", EN: "the toolbox is closed"})

// Call ejecuta una herramienta con los argumentos que mandó el modelo.
//
//   - Lo que falla DENTRO de la herramienta (checkpoint inexistente, modelo que
//     no renderiza, ruta hostil) vuelve como ToolOutput{IsError: true} con el
//     texto que el modelo debe leer; el error de Go es nil. Igual un nombre de
//     herramienta desconocido: es un tropiezo del modelo y puede corregirse.
//   - Un pánico dentro de la herramienta se atrapa y vuelve igual, como IsError.
//   - Si ctx se cancela, Call devuelve ctx.Err() junto con lo que alcanzó a
//     producir: quien cancela no espera un resultado, pero conserva el texto.
//   - Solo ErrToolboxClosed es un error "de verdad" de la caja.
func (t *Toolbox) Call(ctx context.Context, name string, args map[string]any) (out ToolOutput, err error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.closed {
		return ToolOutput{}, ErrToolboxClosed
	}
	var tool *Tool
	for i := range t.tools {
		if t.tools[i].Name == name {
			tool = &t.tools[i]
			break
		}
	}
	if tool == nil {
		names := make([]string, len(t.tools))
		for i, tl := range t.tools {
			names[i] = tl.Name
		}
		sort.Strings(names)
		return ToolOutput{IsError: true, Text: i18n.T(i18n.Msg{
			ES: "herramienta desconocida: %s (disponibles: %s)",
			EN: "unknown tool: %s (available: %s)",
		}, name, strings.Join(names, ", "))}, nil
	}
	if args == nil {
		args = map[string]any{}
	}
	res, runErr := t.run(ctx, tool, args)
	out = toOutput(res, runErr, t.e.log)
	if cerr := ctx.Err(); cerr != nil {
		return out, cerr
	}
	return out, nil
}

// run protege a la app de un pánico dentro de una herramienta.
func (t *Toolbox) run(ctx context.Context, tool *Tool, args map[string]any) (res *toolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			t.e.log.Printf("panic en la herramienta %s: %v", tool.Name, r)
			res, err = nil, i18n.Errorf(i18n.Msg{ES: "error interno en la herramienta %s", EN: "internal error in tool %s"}, tool.Name)
		}
	}()
	return tool.handler(ctx, args)
}

// toOutput convierte el resultado en formato MCP (contenido con texto e
// imágenes en base64) al de la caja: texto unido e imágenes en bytes crudos.
func toOutput(res *toolResult, err error, lg *log.Logger) ToolOutput {
	if err != nil {
		return ToolOutput{Text: err.Error(), IsError: true}
	}
	if res == nil {
		return ToolOutput{}
	}
	out := ToolOutput{IsError: res.IsError}
	var texts []string
	for _, c := range res.Content {
		switch c["type"] {
		case "text":
			s, _ := c["text"].(string)
			texts = append(texts, s)
		case "image":
			data, _ := c["data"].(string)
			raw, derr := base64.StdEncoding.DecodeString(data)
			if derr != nil {
				lg.Printf("imagen ilegible en el resultado: %v", derr)
				continue
			}
			mime, _ := c["mimeType"].(string)
			if mime == "" {
				mime = "image/png"
			}
			out.Images = append(out.Images, ToolImage{MIME: mime, Data: raw})
		}
	}
	out.Text = strings.Join(texts, "\n")
	return out
}

// Close libera lo que la caja abrió por su cuenta. NO cierra lo inyectado (la
// base, el motor de render, el WebAssembly y la caché de partes son de la
// app). Es idempotente.
func (t *Toolbox) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	t.e.close()
	return nil
}
