package hermes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/mcp"
	"scadlab/backend/prompts"
)

// Topes. Los números son los de mini-tools salvo donde se dice.
const (
	// DefaultMaxSteps y MaxStepsLimit: el tope de pasos (llamadas a
	// herramientas) por turno que se puede pedir. El prompt de Hermes anuncia el
	// mismo número al modelo.
	DefaultMaxSteps = prompts.DefaultMaxSteps
	MaxStepsLimit   = 30

	// maxToolResultRunes acota lo que se le devuelve al modelo de UNA
	// herramienta: es la red para un resultado enorme (un render con cientos de
	// diagnósticos) que desbordaría el contexto de un modelo chico.
	maxToolResultRunes = 32000
	// maxStepTextRunes y maxStepArgRunes acotan lo que se muestra y se guarda
	// de un paso: el modelo recibe el resultado completo, la pantalla y la base
	// no necesitan 32 000 caracteres por tarjeta.
	maxStepTextRunes = 4000
	maxStepArgRunes  = 6000

	// contextBudgetChars es el presupuesto de caracteres de los mensajes de un
	// turno; pasado, los resultados viejos de herramientas se reducen a una
	// nota (ver compactToolResults).
	contextBudgetChars    = 160000
	keepRecentToolResults = 4
)

// ClampSteps lleva el tope pedido a 1–30; 0 o negativo es el default.
func ClampSteps(n int) int {
	switch {
	case n <= 0:
		return DefaultMaxSteps
	case n > MaxStepsLimit:
		return MaxStepsLimit
	}
	return n
}

// Toolset es lo que el bucle necesita de la caja de herramientas. *mcp.Toolbox
// la cumple; las pruebas usan una de mentira.
type Toolset interface {
	Tools() []mcp.ToolSpec
	Call(ctx context.Context, name string, args map[string]any) (mcp.ToolOutput, error)
}

// Config es todo lo que un turno necesita.
type Config struct {
	Client llm.Client
	Model  string
	// SystemTools es el prompt de sistema del modo con herramientas;
	// SystemText, el del modo texto (el de respaldo de S5). Los dos vienen
	// armados por quien llama: si el modelo no soporta herramientas, el turno
	// cambia de uno a otro sin que el modelo vea las instrucciones de un modo
	// que no tiene.
	SystemTools string
	SystemText  string
	// History es la conversación previa SIN el mensaje de sistema, ya recortada.
	History []llm.Message
	// User es el mensaje de ahora, con las rutas de sus imágenes.
	User llm.Message
	// Tools es la caja de herramientas; nil = modo texto.
	Tools Toolset
	// NoTools fuerza el modo texto aunque haya caja (el modelo ya mostró que no
	// las soporta, o la persona apagó «Agente»). NoToolsNotice pide avisarlo en
	// la respuesta: se usa cuando la persona quería herramientas y no se pueden.
	NoTools       bool
	NoToolsNotice bool
	// Vision dice si el modelo ve imágenes: solo con ella la imagen de `preview`
	// vuelve al modelo. ImageDir es dónde se escriben esos PNG (los mensajes
	// llevan rutas, no bytes).
	Vision   bool
	ImageDir string
	MaxSteps int
	Price    Price
	// AutoSave es la red de seguridad de S12.c, solo en modo «Agente»: cuando el
	// turno termina SIN que el modelo haya creado ningún checkpoint y su texto trae
	// un bloque OpenSCAD completo, quien llama lo guarda y lo renderiza (devuelve
	// nil si no hay nada que guardar). El bucle anuncia sus pasos como los de
	// cualquier herramienta y, si el render falló y quedan pasos, le devuelve al
	// modelo los errores para otra vuelta: el bucle de respaldo de un modelo sin
	// tool calling.
	AutoSave func(ctx context.Context, text string) *AutoSaveOutcome
}

// Hooks son los avisos del turno. Todos son opcionales.
type Hooks struct {
	// Text recibe el texto visible, en trozos.
	Text func(string)
	// Flush se llama antes de anunciar un paso: que el texto anterior ya haya
	// salido.
	Flush func()
	// Step se llama al arrancar un paso (Status running) y al terminar (ok o
	// error), con el mismo ID.
	Step func(Step)
	// Usage recibe el uso acumulado del turno tras cada vuelta.
	Usage func(Usage)
}

// Result es cómo terminó el turno. Con un error (no una cancelación) trae
// igual lo que alcanzó a hacer: el texto y los pasos se guardan.
type Result struct {
	Text  string
	Steps []Step
	Usage Usage
	// Model es el que respondió de verdad, que puede diferir del pedido.
	Model string
	// Cancelled: lo cortó el usuario o el cierre de la app.
	Cancelled bool
	// StepLimit: se llegó al tope de pasos y el modelo cerró con un resumen.
	StepLimit bool
	// ToolsUnsupported: el servidor rechazó las herramientas y el turno siguió
	// en modo texto. Quien llama lo recuerda para no volver a intentarlo con ese
	// modelo.
	ToolsUnsupported bool
}

var (
	msgNoToolsNotice   = i18n.Msg{ES: "_⚠ Este modelo no admite herramientas: sigo en modo texto. Si escribo el diseño completo en un bloque, ScadLab lo guarda y lo renderiza por mí._", EN: "_⚠ This model does not support tools: continuing in text mode. If I write the complete design in a block, ScadLab saves and renders it for me._"}
	msgStepLimitNotRun = i18n.Msg{ES: "se alcanzó el tope de pasos del turno: esta llamada no se ejecutó", EN: "the step limit of the turn was reached: this call was not run"}
	msgStepLimitNudge  = i18n.Msg{ES: "Llegaste al tope de %d pasos de este turno: no llames más herramientas. Resume en pocas líneas qué hiciste, cuál es el último checkpoint que renderiza y qué queda pendiente.", EN: "You reached the limit of %d steps for this turn: do not call more tools. Summarize in a few lines what you did, which is the last checkpoint that renders and what is still pending."}
	msgStepLimitNotice = i18n.Msg{ES: "_⚠ Se alcanzó el tope de %d pasos del turno. Lo que ya hizo quedó hecho; pídele que siga si falta algo._", EN: "_⚠ The turn reached its limit of %d steps. What it already did stays done; ask it to continue if anything is missing._"}
	msgNoText          = i18n.Msg{ES: "el modelo no devolvió texto", EN: "the model returned no text"}
	msgToolUnavailable = i18n.Msg{ES: "la herramienta %q no está disponible en este chat", EN: "the tool %q is not available in this chat"}
	msgBadArgs         = i18n.Msg{ES: "los argumentos no son un JSON válido: %s", EN: "the arguments are not valid JSON: %s"}
	msgToolPanic       = i18n.Msg{ES: "falló la herramienta por dentro: %v", EN: "the tool failed internally: %v"}
	msgEmptyResult     = i18n.Msg{ES: "(resultado vacío)", EN: "(empty result)"}
	msgTrimmed         = i18n.Msg{ES: "[resultado recortado: %d caracteres de %d]", EN: "[result trimmed: %d characters of %d]"}
	msgNoVisionImage   = i18n.Msg{ES: "(La herramienta devolvió una imagen, pero este modelo no ve imágenes: solo tienes el texto.)", EN: "(The tool returned an image, but this model cannot see images: you only have the text.)"}
	msgImageNote       = i18n.Msg{ES: "Imagen devuelta por la herramienta %s (paso %d):", EN: "Image returned by the %s tool (step %d):"}
	msgOldResult       = i18n.Msg{ES: "[resultado anterior recortado para ahorrar contexto]", EN: "[earlier result trimmed to save context]"}
	msgCancelledStep   = i18n.Msg{ES: "cancelado", EN: "cancelled"}
	msgAutoFix         = i18n.Msg{
		ES: "ScadLab guardó tu bloque como %s, pero no renderiza:\n\n%s\n\nCorrígelo y devuelve el bloque ```openscad COMPLETO otra vez, con su cabecera `// @checkpoint`.",
		EN: "ScadLab saved your block as %s, but it does not render:\n\n%s\n\nFix it and return the COMPLETE ```openscad block again, with its `// @checkpoint` header.",
	}
)

type runner struct {
	cfg Config
	h   Hooks

	msgs     []llm.Message
	visible  strings.Builder
	textLen  int // unidades UTF-16 de visible
	steps    []Step
	usage    Usage
	model    string
	ran      int // pasos ejecutados
	written  []string
	res      Result
	defs     []llm.ToolDef
	toolsOn  bool
	specs    map[string]bool
	maxSteps int

	// autoDone: la red de seguridad ya guardó algo que renderiza (o no hay más
	// que hacer); autoFile, el último checkpoint que guardó.
	autoDone bool
	autoFile string
}

// Run corre un turno completo. Devuelve un error solo por un fallo real del
// proveedor (con lo hecho hasta ahí dentro del Result); una cancelación no es
// un error.
func Run(ctx context.Context, cfg Config, h Hooks) (Result, error) {
	r := &runner{cfg: cfg, h: h, maxSteps: ClampSteps(cfg.MaxSteps), specs: map[string]bool{}}
	defer r.cleanup()
	return r.run(ctx)
}

func (r *runner) run(ctx context.Context) (Result, error) {
	cfg := r.cfg
	if cfg.Tools != nil && !cfg.NoTools {
		for _, s := range cfg.Tools.Tools() {
			r.defs = append(r.defs, llm.ToolDef{Name: s.Name, Description: s.Description, Parameters: s.InputSchema})
			r.specs[s.Name] = true
		}
	}
	r.toolsOn = len(r.defs) > 0
	system := cfg.SystemText
	if r.toolsOn {
		system = cfg.SystemTools
	}
	r.msgs = make([]llm.Message, 0, len(cfg.History)+4)
	r.msgs = append(r.msgs, llm.Message{Role: llm.RoleSystem, Content: system})
	r.msgs = append(r.msgs, cfg.History...)
	// Un turno anterior que falló sin respuesta deja un mensaje de usuario al
	// final del historial: se funde con el de ahora, porque varias plantillas de
	// chat locales exigen que los roles alternen.
	if n := len(r.msgs); n > 1 && r.msgs[n-1].Role == llm.RoleUser && cfg.User.Role == llm.RoleUser {
		r.msgs[n-1].Content = withText(r.msgs[n-1].Content, cfg.User.Content)
		r.msgs[n-1].Images = append(append([]string(nil), r.msgs[n-1].Images...), cfg.User.Images...)
	} else {
		r.msgs = append(r.msgs, cfg.User)
	}
	if cfg.NoToolsNotice && !r.toolsOn {
		r.notice(msgNoToolsNotice)
	}

	for round := 1; ; round++ {
		compactToolResults(r.msgs)
		req := llm.ChatRequest{Model: cfg.Model, Messages: r.msgs}
		if r.toolsOn {
			req.Tools = r.defs
		}
		res, err := chatRetry(ctx, cfg.Client, req, r.onDelta(round))
		if err != nil && r.toolsOn && ctx.Err() == nil && llm.IsToolsUnsupported(err) {
			// El modelo (o el servidor) no soporta herramientas: se sigue sin
			// ellas en lugar de fallar, con el prompt del modo texto y un aviso.
			r.toolsOn = false
			r.res.ToolsUnsupported = true
			r.msgs[0].Content = cfg.SystemText
			r.notice(msgNoToolsNotice)
			res, err = chatRetry(ctx, cfg.Client, llm.ChatRequest{Model: cfg.Model, Messages: r.msgs}, r.onDelta(round))
		}
		if err != nil {
			return r.finish(ctx, err)
		}
		r.addUsage(res)
		r.msgs = append(r.msgs, res.Message)

		calls := res.Message.ToolCalls
		if len(calls) == 0 || !r.toolsOn {
			if r.autoSave(ctx) {
				continue // el render falló y el modelo corrige en otra vuelta
			}
			if strings.TrimSpace(r.visible.String()) == "" && len(r.steps) == 0 {
				return r.finish(ctx, i18n.New(msgNoText))
			}
			return r.finish(ctx, nil)
		}

		capped, err := r.runCalls(ctx, calls)
		if err != nil {
			return r.finish(ctx, err)
		}
		if capped {
			return r.summarize(ctx)
		}
	}
}

// createdCheckpoint dice si el modelo mismo creó un checkpoint en este turno (una
// herramienta que lo hace, terminada bien): con eso la red de seguridad no tiene
// nada que hacer. Qué herramientas lo hacen lo dice mcp.CreatesCheckpoint, la única
// definición (no una lista local que se desactualice con el catálogo).
func (r *runner) createdCheckpoint() bool {
	for _, st := range r.steps {
		if st.Status == StepOK && mcp.CreatesCheckpoint(st.Tool) {
			return true
		}
	}
	return false
}

// autoSave es la red de seguridad (ver Config.AutoSave). Devuelve true si el
// render falló, quedan pasos y se le pidió al modelo que corrija: el bucle sigue
// con otra vuelta.
func (r *runner) autoSave(ctx context.Context) (retry bool) {
	if r.cfg.AutoSave == nil || r.autoDone || ctx.Err() != nil || r.createdCheckpoint() || r.ran >= r.maxSteps {
		return false
	}
	out := r.cfg.AutoSave(ctx, r.visible.String())
	if out == nil || len(out.Steps) == 0 {
		return false
	}
	if r.h.Flush != nil {
		r.h.Flush()
	}
	for _, as := range out.Steps {
		r.ran++
		step := Step{
			ID: newStepID(), Index: r.ran, Tool: as.Tool, Args: capArgs(as.Args), Status: StepOK,
			Text: clipRunes(as.Text, maxStepTextRunes), Images: append([]string{}, as.Images...), DurationMs: as.DurationMs, TextAt: r.textLen,
		}
		if as.Failed {
			step.Status = StepError
		}
		r.steps = append(r.steps, step)
		r.emitStep(step)
	}
	if out.File != "" {
		r.autoFile = out.File
	}
	// Con el render bien (o sin nada que el modelo pueda corregir) se terminó.
	// Con un error y pasos disponibles, otra vuelta: cada intento cuenta sus pasos.
	if out.Feedback == "" || ctx.Err() != nil || r.ran+len(out.Steps) > r.maxSteps {
		r.autoDone = out.Feedback == ""
		return false
	}
	r.msgs = append(r.msgs, llm.Message{Role: llm.RoleUser, Content: i18n.T(msgAutoFix, out.File, clipRunes(out.Feedback, maxToolResultRunes))})
	return true
}

// onDelta arma el receptor del texto de una vuelta. Entre dos vueltas el texto
// del modelo se separa con un párrafo: sin eso «Voy a renderizar.» y la
// respuesta final salen pegados. El pensamiento del modelo no es parte de la
// respuesta visible y se descarta.
func (r *runner) onDelta(round int) func(llm.Delta) {
	opened := false
	return func(d llm.Delta) {
		if d.Text == "" {
			return
		}
		if !opened {
			opened = true
			if round > 1 && strings.TrimSpace(r.visible.String()) != "" {
				r.text("\n\n")
			}
		}
		r.text(d.Text)
	}
}

func (r *runner) text(s string) {
	r.visible.WriteString(s)
	r.textLen += utf16Len(s)
	if r.h.Text != nil {
		r.h.Text(s)
	}
}

// notice agrega un aviso del sistema al texto visible, como párrafo aparte.
func (r *runner) notice(m i18n.Msg, args ...any) {
	sep := ""
	if r.visible.Len() > 0 {
		sep = "\n\n"
	}
	r.text(sep + i18n.T(m, args...) + "\n\n")
}

func (r *runner) addUsage(res llm.ChatResult) {
	u := res.Usage
	r.usage.Input += u.Input
	r.usage.Output += u.Output
	r.usage.CacheRead += u.CacheRead
	r.usage.CacheWrite += u.CacheWrite
	r.usage.Thinking += u.Thinking
	r.usage.CostUSD = r.cfg.Price.Cost(r.usage)
	if res.Model != "" {
		r.model = res.Model
	}
	if r.h.Usage != nil {
		r.h.Usage(r.usage)
	}
}

// finish arma el Result. Una cancelación (del usuario o del cierre de la app)
// no es un error: conserva lo hecho.
func (r *runner) finish(ctx context.Context, err error) (Result, error) {
	res := r.res
	res.Text = r.visible.String()
	res.Steps = r.steps
	res.Usage = r.usage
	res.Model = r.model
	if err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled)) {
		res.Cancelled = true
		err = nil
	}
	return res, err
}

// runCalls ejecuta las llamadas de una vuelta y agrega sus respuestas a la
// conversación (una por llamada: el protocolo lo exige). capped dice que se
// llegó al tope de pasos: las llamadas que ya no caben se contestan con un
// error sin ejecutarse.
func (r *runner) runCalls(ctx context.Context, calls []llm.ToolCall) (capped bool, err error) {
	var images []string
	var notes []string
	for _, c := range calls {
		if cerr := ctx.Err(); cerr != nil {
			return false, cerr
		}
		if r.ran >= r.maxSteps {
			capped = true
			r.msgs = append(r.msgs, toolMessage(c, "error: "+i18n.T(msgStepLimitNotRun)))
			continue
		}
		r.ran++
		step := Step{
			ID: newStepID(), Index: r.ran, Tool: c.Name, Args: capArgs(c.Arguments),
			Status: StepRunning, Images: []string{}, TextAt: r.textLen,
		}
		if r.h.Flush != nil {
			r.h.Flush()
		}
		r.emitStep(step)
		start := time.Now()
		out := r.execTool(ctx, c)
		step.DurationMs = time.Since(start).Milliseconds()

		step.Text = clipRunes(out.uiText, maxStepTextRunes)
		for _, img := range out.images {
			step.Images = append(step.Images, dataURL(img.MIME, img.Data))
		}
		switch {
		case out.cancelled:
			step.Status = StepError
			if strings.TrimSpace(step.Text) == "" {
				step.Text = i18n.T(msgCancelledStep)
			}
		case out.isErr:
			step.Status = StepError
		default:
			step.Status = StepOK
		}
		r.steps = append(r.steps, step)
		r.emitStep(step)
		if out.cancelled {
			return false, context.Canceled
		}

		modelText := out.modelText
		if len(out.images) > 0 {
			if r.cfg.Vision && r.cfg.ImageDir != "" {
				for i, img := range out.images {
					path, werr := r.writeImage(step.ID, i, img)
					if werr != nil {
						continue
					}
					images = append(images, path)
				}
				if len(images) > 0 {
					notes = append(notes, i18n.T(msgImageNote, c.Name, step.Index))
				}
			} else {
				modelText += "\n" + i18n.T(msgNoVisionImage)
			}
		}
		r.msgs = append(r.msgs, toolMessage(c, modelText))
	}
	if len(images) > 0 {
		// La imagen vuelve como un mensaje de usuario justo después de los
		// resultados: ni OpenAI ni Ollama aceptan imágenes en un mensaje de
		// herramienta, y Anthropic funde ambos en un solo mensaje.
		r.msgs = append(r.msgs, llm.Message{Role: llm.RoleUser, Content: strings.Join(notes, "\n"), Images: images})
	}
	return capped, ctx.Err()
}

func (r *runner) emitStep(s Step) {
	if r.h.Step != nil {
		r.h.Step(s)
	}
}

// summarize cierra un turno que llegó al tope de pasos: una última vuelta para
// que el modelo resuma (sus llamadas, si las hace, se ignoran) y el aviso del
// tope. Si esa vuelta falla no se pierde el turno: queda el aviso.
func (r *runner) summarize(ctx context.Context) (Result, error) {
	r.res.StepLimit = true
	r.msgs = append(r.msgs, llm.Message{Role: llm.RoleUser, Content: i18n.T(msgStepLimitNudge, r.maxSteps)})
	res, err := chatRetry(ctx, r.cfg.Client, llm.ChatRequest{Model: r.cfg.Model, Messages: r.msgs, Tools: r.defs}, r.onDelta(2))
	if err == nil {
		r.addUsage(res)
	} else if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return r.finish(ctx, err)
	}
	r.notice(msgStepLimitNotice, r.maxSteps)
	return r.finish(ctx, nil)
}

type toolRun struct {
	modelText string
	uiText    string
	images    []mcp.ToolImage
	isErr     bool
	cancelled bool
}

// execTool ejecuta una llamada y devuelve lo que se le da al modelo y lo que se
// muestra. Nunca falla el turno: un error de la herramienta —o un pánico—
// vuelve como «error: …» para que el modelo lo lea y corrija.
func (r *runner) execTool(ctx context.Context, c llm.ToolCall) (out toolRun) {
	fail := func(msg string) toolRun {
		t := "error: " + msg
		return toolRun{modelText: t, uiText: t, isErr: true}
	}
	defer func() {
		if p := recover(); p != nil {
			out = fail(i18n.T(msgToolPanic, p))
		}
	}()
	if !r.specs[c.Name] {
		return fail(i18n.T(msgToolUnavailable, c.Name))
	}
	if raw, ok := c.Arguments["_raw"].(string); ok && len(c.Arguments) == 1 {
		return fail(i18n.T(msgBadArgs, clipRunes(raw, 300)))
	}
	res, err := r.cfg.Tools.Call(ctx, c.Name, c.Arguments)
	if err != nil && ctx.Err() != nil {
		// Cancelado a mitad de la herramienta: conserva lo que alcanzó a decir.
		return toolRun{uiText: res.Text, cancelled: true, isErr: true}
	}
	if err != nil {
		return fail(err.Error())
	}
	text := strings.TrimSpace(res.Text)
	if res.IsError && !strings.HasPrefix(text, "error:") {
		text = "error: " + text
	}
	if text == "" {
		text = i18n.T(msgEmptyResult)
	}
	ui := text
	if rs := []rune(text); len(rs) > maxToolResultRunes {
		text = string(rs[:maxToolResultRunes]) + "\n" + i18n.T(msgTrimmed, maxToolResultRunes, len(rs))
	}
	return toolRun{modelText: text, uiText: ui, images: res.Images, isErr: res.IsError}
}

func toolMessage(c llm.ToolCall, content string) llm.Message {
	return llm.Message{Role: llm.RoleTool, ToolCallID: c.ID, Name: c.Name, Content: content}
}

// writeImage escribe una imagen de herramienta a un archivo para mandarla al
// modelo (llm.Message.Images son rutas). Se borran al terminar el turno.
func (r *runner) writeImage(stepID string, i int, img mcp.ToolImage) (string, error) {
	if err := os.MkdirAll(r.cfg.ImageDir, 0o700); err != nil {
		return "", err
	}
	ext := ".png"
	if strings.Contains(img.MIME, "jpeg") {
		ext = ".jpg"
	}
	path := filepath.Join(r.cfg.ImageDir, fmt.Sprintf("step-%s-%d%s", stepID, i, ext))
	if err := os.WriteFile(path, img.Data, 0o600); err != nil {
		return "", err
	}
	r.written = append(r.written, path)
	return path, nil
}

func (r *runner) cleanup() {
	for _, p := range r.written {
		_ = os.Remove(p)
	}
}

// llmRetryDelay es la espera antes del segundo intento (variable para que las
// pruebas no esperen un segundo de verdad).
var llmRetryDelay = time.Second

// chatRetry es Chat con un reintento para el fallo de conexión transitorio: el
// servidor remoto se reinició, la conexión se cayó, un hipo de red. Se
// reintenta UNA vez, tras ~1 s, y solo si:
//
//   - el fallo es de conexión (rechazada, reset, EOF, timeout… ver
//     llm.IsRetryable): un 4xx/5xx o un error de certificado no se curan solos;
//   - todavía no llegó ningún trozo de la respuesta: con el stream ya abierto,
//     reintentar repetiría en pantalla texto que la persona ya leyó;
//   - nadie canceló: un ctx muerto corta de inmediato.
func chatRetry(ctx context.Context, c llm.Client, req llm.ChatRequest, onDelta func(llm.Delta)) (llm.ChatResult, error) {
	started := false
	res, err := c.Chat(ctx, req, func(d llm.Delta) {
		started = true
		if onDelta != nil {
			onDelta(d)
		}
	})
	if err == nil || started || ctx.Err() != nil || !llm.IsRetryable(err) {
		return res, err
	}
	timer := time.NewTimer(llmRetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
	return c.Chat(ctx, req, onDelta)
}

// compactToolResults reduce a una nota los resultados de herramientas más viejos
// cuando la conversación del turno pasa del presupuesto. Los últimos
// keepRecentToolResults siempre quedan completos: son los que el modelo está
// usando. Edita los mensajes en su lugar.
func compactToolResults(msgs []llm.Message) {
	total := 0
	for _, m := range msgs {
		total += len(m.Content)
	}
	if total <= contextBudgetChars {
		return
	}
	var tools []int
	for i, m := range msgs {
		if m.Role == llm.RoleTool {
			tools = append(tools, i)
		}
	}
	if len(tools) <= keepRecentToolResults {
		return
	}
	note := i18n.T(msgOldResult)
	for _, i := range tools[:len(tools)-keepRecentToolResults] {
		if total <= contextBudgetChars {
			return
		}
		total -= len(msgs[i].Content) - len(note)
		msgs[i].Content = note
	}
}

// capArgs devuelve los argumentos con los textos muy largos recortados. Se
// muestran y se guardan: el código de un `create_checkpoint` ya está en el
// archivo del checkpoint.
func capArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			v = clipRunes(s, maxStepArgRunes)
		}
		out[k] = v
	}
	return out
}

func clipRunes(s string, max int) string {
	if rs := []rune(s); len(rs) > max {
		return string(rs[:max]) + "…"
	}
	return s
}

func dataURL(mime string, data []byte) string {
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func newStepID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "st-" + hex.EncodeToString(b[:])
}
