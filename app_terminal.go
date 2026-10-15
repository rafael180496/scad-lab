package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"scadlab/backend/agentsessions"
	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/prompts"
	"scadlab/backend/pty"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

// Terminal integrada de agentes de IA (S5, bloque 5.A): bindings, tope de
// terminales y auto-iteración. El PTY vive en backend/pty; las sesiones
// previas de cada CLI, en backend/agentsessions.

// maxAgentTerminals es el tope de terminales de agente abiertas a la vez: cada
// una es un CLI que consume cuota y CPU, y la interfaz tiene dos pestañas.
const maxAgentTerminals = 2

// Auto-iteración: tras maxNudgeStreak avisos seguidos sin que el checkpoint
// más nuevo llegue a renderizar bien, se deja de avisar. El agente corrige en
// un checkpoint NUEVO cada vez, así que «una vez por archivo» no frena a uno
// que se queda dando vueltas con el mismo error y gastando cuota.
const (
	maxNudgeStreak  = 5
	maxInputBytes   = 1 << 20
	nudgeSubmitWait = 300 * time.Millisecond
)

// TerminalInfo describe una terminal abierta (ListTerminals).
type TerminalInfo struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // "agent"
	AgentID     string `json:"agentId"`
	WorkspaceID int64  `json:"workspaceId"`
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
	StartedAt   int64  `json:"startedAt"` // unix ms
}

// TerminalSnapshot es la salida reciente de una terminal (base64) para
// reenganchar la UI tras recargarla. Seq es el desplazamiento justo después de
// su último byte: ver pty.DataEvent.
type TerminalSnapshot struct {
	Data string `json:"data"`
	Seq  int64  `json:"seq"`
}

// AgentChoice es un CLI agéntico que se puede lanzar desde la terminal.
type AgentChoice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Found   bool   `json:"found"`   // el ejecutable existe en este equipo
	Path    string `json:"path"`    // ruta resuelta ("" si no está)
	Enabled bool   `json:"enabled"` // habilitado en Configuración → IA
	CanList bool   `json:"canList"` // tiene listado de sesiones por ID
}

type termMeta struct {
	id          string
	workspaceID int64
	agentID     string
	started     time.Time
	lastInput   time.Time // última tecla del usuario: la «activa» de dos es la que más reciente se usó
}

// terminalService es el estado de la terminal de una App. No vive en App
// (app.go no se toca en esta fase) sino en un registro por App, ver terminals.
type terminalService struct {
	app *App
	mgr *pty.Manager

	mu       sync.Mutex
	meta     map[string]*termMeta
	nextID   int
	nudged   map[string]bool // «ws|archivo|huella»: ya se avisó de ese fallo
	streak   map[int64]int   // avisos seguidos sin un render bueno, por workspace
	nudgeCap bool            // ya se avisó en el log del tope (evita repetirlo)

	// Inyectables para probar la auto-iteración sin un PTY real.
	write        func(id, data string) error
	alive        func(id string) bool
	autoIterate  func() bool
	lang         func() string
	submitWait   time.Duration
	runAsync     func(func())
	maxTerminals int
}

var (
	termSvcMu sync.Mutex
	termSvcs  = map[*App]*terminalService{}
)

// terminals devuelve el servicio de terminal de la App, creándolo la primera
// vez que se usa: engancha el cierre de los procesos al apagado de la app
// (onShutdown) y la auto-iteración al fin de cada render (onRenderFinished).
func (a *App) terminals() *terminalService {
	termSvcMu.Lock()
	defer termSvcMu.Unlock()
	if s := termSvcs[a]; s != nil {
		return s
	}
	s := newTerminalService(a)
	termSvcs[a] = s
	a.onShutdown(s.mgr.CloseAll)
	a.onRenderFinished(s.onRender)
	return s
}

func newTerminalService(a *App) *terminalService {
	s := &terminalService{
		app:          a,
		meta:         map[string]*termMeta{},
		nudged:       map[string]bool{},
		streak:       map[int64]int{},
		submitWait:   nudgeSubmitWait,
		runAsync:     func(f func()) { go f() },
		maxTerminals: maxAgentTerminals,
		lang:         i18n.Lang,
	}
	s.mgr = pty.NewManager(func(event string, payload any) {
		if ex, ok := payload.(pty.ExitEvent); ok {
			s.forget(ex.ID)
		}
		a.emit(event, payload)
	})
	s.write = s.mgr.Write
	s.alive = s.mgr.Has
	s.autoIterate = func() bool {
		if a.store == nil {
			return false
		}
		cfg, err := a.store.GetSettings()
		return err == nil && cfg.AutoIterate
	}
	return s
}

func (s *terminalService) forget(id string) {
	s.mu.Lock()
	delete(s.meta, id)
	s.mu.Unlock()
}

// openCount cuenta las terminales de agente vivas y descarta de meta las que
// ya murieron. Con s.mu tomado.
func (s *terminalService) openCount() int {
	n := 0
	for id := range s.meta {
		if s.alive(id) {
			n++
		} else {
			delete(s.meta, id)
		}
	}
	return n
}

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// ListAgentChoices lista los CLIs agénticos con su estado, para el selector de
// la terminal. No corre `--version` (DetectTools sí, y tarda): solo busca el
// ejecutable.
func (a *App) ListAgentChoices() ([]AgentChoice, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	provs, err := a.store.ListProviders()
	if err != nil {
		return nil, err
	}
	out := []AgentChoice{}
	for _, p := range provs {
		if p.Kind != "cli" {
			continue
		}
		path := detect.CLIPath(p.ID, p.Command)
		out = append(out, AgentChoice{ID: p.ID, Name: p.Name, Found: path != "", Path: path, Enabled: p.Enabled, CanList: agentsessions.CanList(p.ID)})
	}
	return out, nil
}

// StartAgentTerminal abre el CLI de agentId en la carpeta del workspace y
// devuelve el id de la terminal. mode: "new" | "resume" (con sessionId) |
// "continue" (la última sesión). Máximo 2 terminales de agente a la vez.
func (a *App) StartAgentTerminal(workspaceId int64, agentId, mode, sessionId string, cols, rows int) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	if mode == "" {
		mode = agentsessions.ModeNew
	}
	// Se valida primero lo barato y lo que depende solo de la entrada: el id de
	// sesión llega del frontend y acaba como argumento del CLI.
	args, err := agentsessions.Args(agentId, mode, sessionId)
	if err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(workspaceId)
	if err != nil {
		return "", err
	}
	p, err := a.agentProvider(agentId)
	if err != nil {
		return "", err
	}
	exe := detect.CLIPath(p.ID, p.Command)
	if exe == "" {
		return "", i18n.Errorf(i18n.Msg{ES: "no se encontró %s en este equipo; indica su comando en Configuración → IA", EN: "%s was not found on this computer; set its command in Settings → AI"}, p.Name)
	}
	return a.terminals().startAgent(w, p, exe, mode, args, cols, rows)
}

// agentProvider devuelve el proveedor si es un CLI habilitado.
func (a *App) agentProvider(agentID string) (store.AIProvider, error) {
	p, err := a.store.GetProvider(agentID)
	if err != nil {
		return p, i18n.Errorf(i18n.Msg{ES: "agente desconocido: %q", EN: "unknown agent: %q"}, agentID)
	}
	if p.Kind != "cli" {
		return p, i18n.Errorf(i18n.Msg{ES: "%s no es un CLI que se pueda abrir en la terminal", EN: "%s is not a CLI that can be opened in the terminal"}, p.Name)
	}
	if !p.Enabled {
		return p, i18n.Errorf(i18n.Msg{ES: "%s está deshabilitado en Configuración → IA", EN: "%s is disabled in Settings → AI"}, p.Name)
	}
	return p, nil
}

func (s *terminalService) startAgent(w store.Workspace, p store.AIProvider, exe, mode string, args []string, cols, rows int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openCount() >= s.maxTerminals {
		return "", i18n.Errorf(i18n.Msg{ES: "ya hay %d terminales de agente abiertas; cierra una antes de abrir otra", EN: "there are already %d agent terminals open; close one before opening another"}, s.maxTerminals)
	}
	s.nextID++
	id := fmt.Sprintf("term-%d", s.nextID)
	argv := launchArgv(exe, args)
	// El directorio del propio ejecutable va primero: un CLI de node instalado
	// con nvm lo encuentra junto a su `node`.
	env := pty.Environ(os.Environ(), append([]string{filepath.Dir(exe)}, detect.ExtraDirs()...))
	now := time.Now()
	// Se registra antes de abrir: un CLI que muere al instante emite
	// terminal:exit, que borra la entrada; registrar después la dejaría huérfana.
	s.meta[id] = &termMeta{id: id, workspaceID: w.ID, agentID: p.ID, started: now, lastInput: now}
	if err := s.mgr.Open(id, w.Path, argv, env, cols, rows); err != nil {
		delete(s.meta, id)
		return "", err
	}
	log.Printf("scadlab: terminal %s: %s (%s) en %s", id, p.ID, mode, w.Name)
	return id, nil
}

// launchArgv arma el argv de un CLI. Los .ps1 que deja npm en Windows no son
// ejecutables por sí solos: se pasan a PowerShell como archivo (sigue siendo
// argv, sin interpolar texto en una línea de comandos).
func launchArgv(exe string, args []string) []string {
	if strings.EqualFold(filepath.Ext(exe), ".ps1") {
		ps := detect.Find("powershell")
		if ps == "" {
			ps = "powershell.exe"
		}
		return append([]string{ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", exe}, args...)
	}
	return append([]string{exe}, args...)
}

// WriteTerminal envía teclas o texto pegado a la terminal.
func (a *App) WriteTerminal(termId, data string) error {
	if len(data) > maxInputBytes {
		return i18n.New(i18n.Msg{ES: "el texto a enviar a la terminal es demasiado grande", EN: "the text to send to the terminal is too large"})
	}
	s := a.terminals()
	if err := s.mgr.Write(termId, data); err != nil {
		return err
	}
	s.mu.Lock()
	if m := s.meta[termId]; m != nil {
		m.lastInput = time.Now()
	}
	s.mu.Unlock()
	return nil
}

// ResizeTerminal reflowa el PTY tras el fit del frontend.
func (a *App) ResizeTerminal(termId string, cols, rows int) error {
	return a.terminals().mgr.Resize(termId, cols, rows)
}

// CloseTerminal cierra la terminal y mata el CLI. Idempotente.
func (a *App) CloseTerminal(termId string) error {
	return a.terminals().mgr.Close(termId)
}

// ListTerminals devuelve las terminales abiertas. El frontend lo llama al
// arrancar y al recargar la UI para reenganchar las que siguen vivas.
func (a *App) ListTerminals() ([]TerminalInfo, error) {
	s := a.terminals()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openCount()
	out := []TerminalInfo{}
	for _, in := range s.mgr.List() {
		m := s.meta[in.ID]
		if m == nil {
			continue
		}
		out = append(out, TerminalInfo{ID: in.ID, Kind: "agent", AgentID: m.agentID, WorkspaceID: m.workspaceID, Cols: in.Cols, Rows: in.Rows, StartedAt: in.StartedAt})
	}
	sortTerminals(out)
	return out, nil
}

func sortTerminals(t []TerminalInfo) {
	sort.Slice(t, func(i, j int) bool {
		if t[i].StartedAt != t[j].StartedAt {
			return t[i].StartedAt < t[j].StartedAt
		}
		return t[i].ID < t[j].ID
	})
}

// ReadTerminalBuffer devuelve la salida reciente de la terminal: con ella la UI
// recargada vuelve a dibujar lo que había en pantalla.
func (a *App) ReadTerminalBuffer(termId string) (TerminalSnapshot, error) {
	snap, err := a.terminals().mgr.Snapshot(termId)
	if err != nil {
		return TerminalSnapshot{}, err
	}
	return TerminalSnapshot{Data: base64.StdEncoding.EncodeToString(snap.Data), Seq: snap.Seq}, nil
}

// ListAgentSessions lista las sesiones previas de agentId en la carpeta del
// workspace, la más reciente primero. Un CLI sin listado verificable
// (Antigravity) devuelve una lista vacía: no es un error.
func (a *App) ListAgentSessions(workspaceId int64, agentId string) ([]agentsessions.Session, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if !agentsessions.Known(agentId) {
		return nil, i18n.Errorf(i18n.Msg{ES: "agente desconocido: %q", EN: "unknown agent: %q"}, agentId)
	}
	if !agentsessions.CanList(agentId) {
		return []agentsessions.Session{}, nil
	}
	w, err := a.store.GetWorkspace(workspaceId)
	if err != nil {
		return nil, err
	}
	opts := agentsessions.Options{}
	if agentId == agentsessions.OpenCode {
		p, err := a.store.GetProvider(agentId)
		if err != nil {
			return nil, err
		}
		opts.Exe = detect.CLIPath(p.ID, p.Command)
	}
	return agentsessions.List(a.context(), agentId, w.Path, opts)
}

// SetAutoIterate enciende o apaga la auto-iteración sin reescribir el resto de
// la configuración (el interruptor vive en la barra de la terminal).
func (a *App) SetAutoIterate(enabled bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetSetting("autoIterate", enabled)
}

// ── Auto-iteración ──────────────────────────────────────────────────────────

// onRender reacciona al fin de un render (a.onRenderFinished). Si falló el
// checkpoint MÁS NUEVO del workspace y hay un agente abierto ahí, le escribe el
// aviso de prompts.Nudge — el mismo criterio que RENDER_ERRORS.md. Corre en Go
// y no en el frontend para que funcione con el panel plegado.
func (s *terminalService) onRender(workspaceID int64, res render.Result, latest bool) {
	if !latest {
		return
	}
	switch res.Status {
	case render.StatusOK:
		s.mu.Lock()
		delete(s.streak, workspaceID)
		s.nudgeCap = false
		s.mu.Unlock()
		return
	case render.StatusError:
	default:
		return // cancelado o sin OpenSCAD: no dice nada del modelo
	}
	if !s.autoIterate() {
		return
	}
	errs := 0
	for _, d := range res.Diagnostics {
		if d.Severity == render.SeverityError {
			errs++
		}
	}
	key := nudgeKey(workspaceID, res)

	s.mu.Lock()
	target := s.activeAgent(workspaceID)
	switch {
	case target == "":
		// Sin agente no se «gasta» el aviso: si abren uno después, el error
		// de un checkpoint viejo no le sirve, pero el siguiente fallo sí.
	case s.nudged[key]:
		target = ""
	case s.streak[workspaceID] >= maxNudgeStreak:
		if !s.nudgeCap {
			s.nudgeCap = true
			log.Printf("scadlab: auto-iteración detenida tras %d avisos sin un render correcto (workspace %d)", maxNudgeStreak, workspaceID)
		}
		target = ""
	default:
		if len(s.nudged) > 500 {
			s.nudged = map[string]bool{}
		}
		s.nudged[key] = true
		s.streak[workspaceID]++
	}
	s.mu.Unlock()
	if target == "" {
		return
	}

	text := prompts.Nudge(res.FileName, errs, s.lang())
	s.runAsync(func() {
		if err := s.write(target, text); err != nil {
			log.Printf("scadlab: auto-iteración: %v", err)
			return
		}
		// El Enter va aparte y tras una pausa: los CLIs de terminal (Ink,
		// Bubble Tea) tratan una ráfaga de texto como un pegado y un \r dentro
		// del mismo bloque queda como salto de línea, sin enviar el mensaje.
		time.Sleep(s.submitWait)
		if err := s.write(target, "\r"); err != nil {
			log.Printf("scadlab: auto-iteración: %v", err)
		}
	})
}

// activeAgent elige la terminal de agente del workspace a la que avisar: la
// que el usuario tocó más recientemente (a igualdad, la más nueva). "" si no
// hay ninguna viva. Con s.mu tomado.
func (s *terminalService) activeAgent(workspaceID int64) string {
	var best *termMeta
	for _, m := range s.meta {
		if m.workspaceID != workspaceID || !s.alive(m.id) {
			continue
		}
		if best == nil || m.lastInput.After(best.lastInput) || (m.lastInput.Equal(best.lastInput) && m.started.After(best.started)) {
			best = m
		}
	}
	if best == nil {
		return ""
	}
	return best.id
}

// nudgeKey identifica un fallo concreto: workspace, archivo y una huella de sus
// diagnósticos. Un checkpoint es inmutable, así que el mismo archivo con los
// mismos errores es el mismo fallo; si se reescribiera con otros errores,
// cuenta como uno nuevo.
func nudgeKey(workspaceID int64, res render.Result) string {
	h := sha256.New()
	for _, d := range res.Diagnostics {
		fmt.Fprintf(h, "%s|%d|%s\n", d.Severity, d.Line, d.Message)
	}
	return fmt.Sprintf("%d|%s|%s", workspaceID, res.FileName, hex.EncodeToString(h.Sum(nil))[:16])
}
