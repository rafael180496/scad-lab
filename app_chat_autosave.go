package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"scadlab/backend/checkpoint"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/mcp"
	"scadlab/backend/prompts"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Red de seguridad del chat (S12.c). Un turno en modo «Agente» que termina SIN
// haber creado ningún checkpoint pero que trae el diseño completo en su texto
// —un bloque ```openscad— no puede dejar al usuario con un bloque de código y
// nada en el visor: ScadLab lo guarda como checkpoint, lo renderiza y lo muestra
// como un paso más, «Guardado automáticamente».
//
// Pasa con cualquier proveedor: un modelo local sin tool calling (Hermes en modo
// texto), uno que sí las tiene pero contesta con el código en vez de llamarlas, o
// un CLI cuyo MCP no quedó conectado. Es el MISMO camino para Hermes (que además
// le devuelve al modelo los errores del render para que corrija) y para los CLIs
// (un solo intento: el CLI tiene su propia sesión, ver runCLITurn).

// autoStepTool es el nombre del paso «Guardado automáticamente». No es una
// herramienta del servidor MCP: la tarjeta lo conoce por su etiqueta
// (`steps.tools.autosave`) y trata su resultado como el de create_checkpoint.
const autoStepTool = "autosave"

// autoSaver es la red de seguridad de UN turno. Recuerda el último checkpoint que
// guardó (el padre de un reintento: la corrección de un bloque que no renderizó
// cuelga de él) y los bloques que ya intentó, para no guardar dos veces lo mismo
// si el modelo no cambia nada.
type autoSaver struct {
	a           *App
	w           store.Workspace
	chatID      int64
	currentFile string

	prev  string
	tried map[string]bool
}

func (a *App) newAutoSaver(w store.Workspace, chatID int64, currentFile string) *autoSaver {
	return &autoSaver{a: a, w: w, chatID: chatID, currentFile: currentFile, tried: map[string]bool{}}
}

// designBlock elige el bloque del texto que es el diseño: el último de los
// bloques OpenSCAD completos que no sea un fragmento al lado de uno mucho más
// largo (una respuesta que muestra la pieza y después un `wall = 2;` para
// ajustarla). Nunca un bloque sin cerrar. ok=false si no hay ninguno.
func designBlock(text string) (code string, ok bool) {
	var blocks []prompts.Block
	longest := 0
	for _, b := range prompts.ScadBlocks(text) {
		if b.Partial || strings.TrimSpace(b.Code) == "" {
			continue
		}
		blocks = append(blocks, b)
		if n := strings.Count(b.Code, "\n") + 1; n > longest {
			longest = n
		}
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		if n := strings.Count(blocks[i].Code, "\n") + 1; n*2 >= longest {
			return blocks[i].Code, true
		}
	}
	return "", false
}

var headerDescRe = regexp.MustCompile(`^\s*//\s*@checkpoint\b.*?\bdesc="((?:[^"\\]|\\.)*)"`)

// blockDescription es el `desc` de la cabecera del bloque, o "".
func blockDescription(code string) string {
	first, _, _ := strings.Cut(code, "\n")
	m := headerDescRe.FindStringSubmatch(strings.TrimRight(first, "\r"))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[1]))
}

// parent es el padre del checkpoint a guardar: el de un intento anterior de este
// turno; si no, el archivo abierto en el editor (el prompt le dijo al modelo que
// partiera de él), el padre activo del workspace y por último el más nuevo. ""
// en un workspace sin checkpoints: el bloque será la raíz.
func (s *autoSaver) parent() string {
	if s.prev != "" {
		return s.prev
	}
	if f := s.currentFile; f != "" && validCheckpointName(f) == nil {
		if st, err := os.Stat(filepath.Join(s.w.Path, f)); err == nil && !st.IsDir() {
			return f
		}
	}
	if p := checkpoint.ActiveParent(s.w.Path); p != "" {
		return p
	}
	return latestCheckpoint(s.w.Path)
}

// run guarda y renderiza el diseño de text. nil si no hay nada que guardar (sin
// bloque completo, o el mismo bloque que ya se intentó).
func (s *autoSaver) run(ctx context.Context, text string) *hermes.AutoSaveOutcome {
	code, ok := designBlock(text)
	if !ok {
		return nil
	}
	key := strings.TrimSpace(code)
	if s.tried[key] {
		return nil
	}
	s.tried[key] = true

	desc := blockDescription(code)
	label := desc
	if label == "" {
		label = i18n.T(msgAutoLabel)
	}
	if r := []rune(label); len(r) > 80 {
		label = string(r[:80])
	}
	parent := s.parent()

	start := time.Now()
	name, err := s.a.SaveChatCheckpoint(s.w.ID, s.chatID, parent, label, desc, code)
	if err != nil {
		log.Printf("scadlab: chat %d: guardar el bloque automáticamente: %v", s.chatID, err)
		return &hermes.AutoSaveOutcome{Steps: []hermes.AutoStep{{
			Tool: autoStepTool, Args: map[string]any{"label": label, "parent": parent},
			Text: "error: " + err.Error(), Failed: true, DurationMs: time.Since(start).Milliseconds(),
		}}}
	}
	s.prev = name
	saved, _ := json.Marshal(map[string]any{"file": name, "parent": parent, "message": i18n.T(msgAutoSaved, name)})
	out := &hermes.AutoSaveOutcome{File: name, Steps: []hermes.AutoStep{{
		Tool: autoStepTool, Args: map[string]any{"label": label, "parent": parent, "file": name},
		Text: string(saved), DurationMs: time.Since(start).Milliseconds(),
	}}}

	rstep, feedback := s.render(ctx, name)
	out.Steps = append(out.Steps, rstep)
	out.Feedback = feedback
	return out
}

// render renderiza el checkpoint con la caja de herramientas de la app (el mismo
// `render` que usa el modelo: devuelve la malla y los diagnósticos con su línea).
// feedback es el texto para el modelo si el render falló por culpa del código.
func (s *autoSaver) render(ctx context.Context, name string) (step hermes.AutoStep, feedback string) {
	step = hermes.AutoStep{Tool: "render", Args: map[string]any{"file": name}}
	start := time.Now()
	defer func() { step.DurationMs = time.Since(start).Milliseconds() }()

	tools, err := newChatToolset(s.a, s.w)
	if err != nil {
		step.Text, step.Failed = "error: "+err.Error(), true
		return step, ""
	}
	if cl, ok := tools.(io.Closer); ok {
		defer cl.Close()
	}
	res, err := tools.Call(ctx, "render", map[string]any{"file": name})
	switch {
	case err != nil && ctx.Err() != nil:
		step.Text, step.Failed = i18n.T(msgAutoCancelled), true
		return step, ""
	case err != nil:
		// No es culpa del código: el motor no está, la base no responde…
		step.Text, step.Failed = "error: "+err.Error(), true
		return step, ""
	}
	step.Text = strings.TrimSpace(res.Text)
	for _, img := range res.Images {
		step.Images = append(step.Images, "data:"+img.MIME+";base64,"+base64.StdEncoding.EncodeToString(img.Data))
	}
	if res.IsError {
		step.Failed = true
		if !strings.HasPrefix(step.Text, "error:") {
			step.Text = "error: " + step.Text
		}
		return step, step.Text
	}
	return step, ""
}

var (
	msgAutoLabel     = i18n.Msg{ES: "Diseño del chat", EN: "Chat design"}
	msgAutoSaved     = i18n.Msg{ES: "ScadLab guardó el bloque como %s", EN: "ScadLab saved the block as %s"}
	msgAutoCancelled = i18n.Msg{ES: "cancelado", EN: "cancelled"}
)

// cliSafetyNet es la red de seguridad de un turno con un CLI: si en modo «Agente»
// terminó sin dejar ningún checkpoint y su texto trae el diseño completo, lo
// guarda y lo renderiza, y suma los pasos al resultado (y a la pantalla). Un solo
// intento: el CLI tiene su propia sesión y, si el render falla, la tarjeta del
// paso muestra el error con su línea para pedirle la corrección en el mensaje
// siguiente.
//
// Que el CLI haya dejado un checkpoint se ve de dos maneras: un paso
// que crea checkpoints y terminó bien (mcp.CreatesCheckpoint), o un .scad que no estaba al
// empezar el turno (Claude con `Write`, Codex editando el workspace). Con
// cualquiera de las dos no se hace nada: duplicarlo sería peor que no ayudar.
func (a *App) cliSafetyNet(ctx context.Context, j chatJob, before map[string]bool, out *hermes.Result) (saved bool) {
	if !j.agent || out.Cancelled || ctx.Err() != nil {
		return false
	}
	for _, st := range out.Steps {
		if st.Status == hermes.StepOK && (mcp.CreatesCheckpoint(st.Tool) || st.Tool == autoStepTool) {
			return true
		}
	}
	if files, err := workspace.Scan(j.workspace.Path); err == nil {
		for _, f := range files {
			if !before[f.FileName] {
				return true // el CLI escribió un .scad por su cuenta
			}
		}
	}
	oc := a.newAutoSaver(j.workspace, j.chat.ID, j.currentFile).run(ctx, out.Text)
	if oc == nil {
		return false
	}
	for _, as := range oc.Steps {
		st := hermes.Step{
			ID: "st-" + randHex(6), Index: len(out.Steps) + 1, Tool: as.Tool, Args: capStepArgs(as.Args), Status: hermes.StepOK,
			Text: clipText(as.Text, cliStepTextRunes), Images: append([]string{}, as.Images...), DurationMs: as.DurationMs, TextAt: utf16Len(out.Text),
		}
		if as.Failed {
			st.Status = hermes.StepError
		}
		out.Steps = append(out.Steps, st)
		a.chatSvc().upsertStep(j.chat.ID, st)
		a.emit(evChatStep, ChatStepEvent{ChatID: j.chat.ID, WorkspaceID: j.chat.WorkspaceID, Step: st})
	}
	return oc.File != ""
}

// workspaceScadSet son los .scad de la raíz de dir (para saber, al terminar un
// turno, si apareció alguno).
func workspaceScadSet(dir string) map[string]bool {
	set := map[string]bool{}
	files, err := workspace.Scan(dir)
	if err != nil {
		return set
	}
	for _, f := range files {
		set[f.FileName] = true
	}
	return set
}
