package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"scadlab/backend/agentsessions"
	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/mcpconf"
	"scadlab/backend/store"
)

// Servidor MCP de ScadLab (S6, bloque 6.B): configuración de cada CLI de IA y
// su interfaz. El servidor en sí (`ScadLab --mcp --workspace <dir>`) es el
// bloque 6.A (backend/mcp); aquí solo se deja apuntando a él, con el comando
// `<este ejecutable> --mcp --workspace <carpeta del workspace>`.

// evAntigravityMCP avisa a la interfaz del resultado de registrar ScadLab en la
// configuración global de Antigravity al elegirlo (S12.c): se agregó (toast) o
// falló (toast de error). Payload: AntigravityMCPEvent.
const evAntigravityMCP = "mcp:antigravity"

// AntigravityMCPEvent es el payload de mcp:antigravity. Added: se agregó el servidor
// a la configuración global. Permissions: cuántas reglas `mcp(scadlab/…)` se
// agregaron a `permissions.allow` de su settings.json (SettingsPath). Error, con el
// motivo, si falló cualquiera de las dos cosas.
type AntigravityMCPEvent struct {
	Added        bool   `json:"added"`
	Permissions  int    `json:"permissions"`
	SettingsPath string `json:"settingsPath"`
	Error        string `json:"error"`
}

// evMCPChanged avisa a la interfaz que cambió el interruptor del servidor MCP o
// se resincronizó un workspace: el indicador de la terminal se vuelve a leer.
const evMCPChanged = "mcp:changed"

// MCPChanged es el payload de mcp:changed.
type MCPChanged struct {
	Enabled bool `json:"enabled"`
}

// MCPStatus es lo que Configuración → IA y la terminal muestran del servidor
// MCP. Files trae el estado de los archivos de proyecto del workspace pedido
// (vacío si no hay workspace); Codex no tiene archivo: se le pasan CodexFlags al
// lanzarlo desde la terminal de ScadLab.
type MCPStatus struct {
	Enabled      bool                 `json:"enabled"`
	Binary       string               `json:"binary"`      // ejecutable de ScadLab ("" si no se pudo resolver)
	BinaryError  string               `json:"binaryError"` // por qué no se pudo
	WorkspaceDir string               `json:"workspaceDir"`
	Command      string               `json:"command"` // línea que usan los agentes, copiable
	Files        []mcpconf.FileReport `json:"files"`
	CodexFlags   []string             `json:"codexFlags"`
	OpenSCAD     bool                 `json:"openscad"` // OpenSCAD está disponible: sin él render/preview/export fallan
	OpenSCADPath string               `json:"openscadPath"`
}

// mcpBinary resuelve el ejecutable de ScadLab. Es una variable para que los
// tests no dependan del binario de prueba.
var mcpBinary = defaultMCPBinary

// defaultMCPBinary es os.Executable() con los enlaces simbólicos resueltos: una
// app instalada por un enlace (o `wails dev`, que corre un binario temporal)
// tiene que apuntar al archivo real.
func defaultMCPBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// GetMCPStatus describe la configuración del servidor MCP para el workspace
// dado (0 = sin workspace: solo el estado general). No escribe nada.
func (a *App) GetMCPStatus(workspaceId int64) (MCPStatus, error) {
	if err := a.ready(); err != nil {
		return MCPStatus{}, err
	}
	s, err := a.store.GetSettings()
	if err != nil {
		return MCPStatus{}, err
	}
	st := MCPStatus{Enabled: s.MCPEnabled, Files: []mcpconf.FileReport{}, CodexFlags: []string{}}
	if p := a.openscadPath(s.OpenSCADPath); p != "" {
		st.OpenSCAD, st.OpenSCADPath = true, p
	}
	bin, err := mcpBinary()
	if err != nil {
		st.BinaryError = err.Error()
	}
	st.Binary = bin

	dir := "<" + i18n.T(i18n.Msg{ES: "carpeta del workspace", EN: "workspace folder"}) + ">"
	if workspaceId != 0 {
		w, err := a.store.GetWorkspace(workspaceId)
		if err != nil {
			return st, err
		}
		dir = absDir(w.Path)
		st.WorkspaceDir = dir
		if bin != "" {
			if rep, err := mcpconf.Inspect(dir, bin, s.MCPEnabled); err == nil {
				st.Files = rep.Files
			} else {
				// La carpeta ya no existe: no hay archivos que mirar.
				log.Printf("scadlab: estado MCP de %s: %v", w.Path, err)
			}
		}
	}
	if bin != "" {
		st.Command = mcpconf.Command(bin, dir)
		st.CodexFlags = mcpconf.CodexArgs(bin, dir)
	}
	return st, nil
}

// SyncWorkspaceMCP vuelve a escribir la entrada scadlab en los archivos del
// workspace (o la quita si el servidor MCP está apagado) y devuelve qué pasó con
// cada uno. Es el botón «Volver a sincronizar».
func (a *App) SyncWorkspaceMCP(workspaceId int64) (mcpconf.Report, error) {
	if err := a.ready(); err != nil {
		return mcpconf.Report{}, err
	}
	w, err := a.store.GetWorkspace(workspaceId)
	if err != nil {
		return mcpconf.Report{}, err
	}
	s, err := a.store.GetSettings()
	if err != nil {
		return mcpconf.Report{}, err
	}
	rep, err := a.syncMCP(w, s.MCPEnabled)
	if err == nil {
		a.emit(evMCPChanged, MCPChanged{Enabled: s.MCPEnabled})
	}
	return rep, err
}

// SetMCPEnabled enciende o apaga el servidor MCP y sincroniza todos los
// workspaces registrados: encendido escribe la entrada, apagado la quita.
func (a *App) SetMCPEnabled(enabled bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.SetSetting("mcpEnabled", enabled); err != nil {
		return err
	}
	a.syncAllMCP(enabled)
	a.emit(evMCPChanged, MCPChanged{Enabled: enabled})
	return nil
}

// syncMCP aplica mcpconf.Sync a un workspace con el ejecutable vigente.
func (a *App) syncMCP(w store.Workspace, enabled bool) (mcpconf.Report, error) {
	// De paso, se retira el hook de Antigravity que una versión anterior pudo dejar
	// en el proyecto (ya no se usa: los permisos van en el settings.json global).
	cleanLegacyAgyHook(absDir(w.Path))
	bin, err := mcpBinary()
	if err != nil {
		if enabled {
			return mcpconf.Report{}, i18n.Errorf(i18n.Msg{ES: "no se pudo ubicar el ejecutable de ScadLab: %v", EN: "could not locate the ScadLab executable: %v"}, err)
		}
		bin = ""
	}
	return mcpconf.Sync(absDir(w.Path), bin, enabled)
}

// syncWorkspaceMCPQuiet sincroniza al crear, abrir o importar un workspace. Un
// fallo no impide abrirlo (la configuración del MCP es un extra): se registra y
// el estado queda visible en Configuración → IA.
func (a *App) syncWorkspaceMCPQuiet(w store.Workspace) {
	s, err := a.store.GetSettings()
	if err != nil {
		return
	}
	if _, err := a.syncMCP(w, s.MCPEnabled); err != nil {
		log.Printf("scadlab: configurar el MCP en %s: %v", w.Path, err)
	}
}

// syncAllMCP recorre los workspaces registrados (los que tienen su carpeta).
func (a *App) syncAllMCP(enabled bool) {
	list, err := a.store.ListWorkspaces()
	if err != nil {
		log.Printf("scadlab: sincronizar el MCP: %v", err)
		return
	}
	for _, w := range list {
		if st, err := os.Stat(w.Path); err != nil || !st.IsDir() {
			continue
		}
		if _, err := a.syncMCP(w, enabled); err != nil {
			log.Printf("scadlab: configurar el MCP en %s: %v", w.Path, err)
		}
	}
}

// mcpLaunchArgs son los argumentos que se anteponen al lanzar un CLI para que
// vea el servidor sin depender del archivo de proyecto: Codex (no lee
// configuración de proyecto) y Claude Code (el .mcp.json del proyecto pide una
// aprobación la primera vez; con --mcp-config no hace falta). Van antes del subcomando (`codex -c … resume --last`). Vacío si
// el servidor MCP está apagado o no se conoce el ejecutable.
func (a *App) mcpLaunchArgs(agentID, workspaceDir string) []string {
	if agentID != agentsessions.Codex && agentID != agentsessions.Claude {
		return nil
	}
	if s, err := a.store.GetSettings(); err != nil || !s.MCPEnabled {
		return nil
	}
	bin, err := mcpBinary()
	if err != nil || bin == "" {
		return nil
	}
	if agentID == agentsessions.Claude {
		return mcpconf.ClaudeArgs(bin, absDir(workspaceDir))
	}
	return mcpconf.CodexArgs(bin, absDir(workspaceDir))
}

// absDir devuelve la ruta absoluta y limpia de una carpeta: el comando del MCP
// se ejecuta desde donde lo lance el agente, no desde la de ScadLab.
func absDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// ---- Antigravity (configuración global) ----

// Antigravity (`agy`) no lee archivos de proyecto: su configuración de MCP es
// global (~/.gemini). ScadLab solo la modifica cuando el usuario pulsa «Agregar
// a Antigravity» / «Quitar de Antigravity» (con confirmación), siempre por
// `agy mcp add|remove|list` —nunca editando su archivo— y sin shell.

// agyTimeout es el plazo de cada comando de agy.
const agyTimeout = 15 * time.Second

// agyExec ejecuta `agy` con argv (sin shell) y devuelve su salida (stdout y
// stderr juntos). Es una variable para que los tests no lancen el agy real ni
// toquen la configuración del usuario.
var agyExec = defaultAgyExec

func defaultAgyExec(ctx context.Context, agyPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, agyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, agyPath, args...)
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), i18n.Errorf(i18n.Msg{ES: "agy no respondió en %s", EN: "agy did not respond within %s"}, agyTimeout)
	}
	return out.String(), err
}

// agyPath localiza `agy` (solo ese nombre: el `gemini` anterior no es
// Antigravity). Es variable para los tests.
var agyPath = func() string { return detect.Find("agy") }

// agyMu serializa add/remove: dos clics seguidos no deben pisarse en el archivo.
var agyMu sync.Mutex

// AntigravityMCP es el estado de la entrada de ScadLab en la configuración
// global de Antigravity. State: not-installed | missing | ok | other | disabled |
// error (constantes Agy* de mcpconf).
type AntigravityMCP struct {
	State   string `json:"state"`
	AgyPath string `json:"agyPath"`
	Command string `json:"command"` // lo que ScadLab agregaría: «<ScadLab> --mcp»
	Current string `json:"current"` // lo que hoy dice Antigravity de la entrada scadlab, si la hay
	Detail  string `json:"detail"`  // por qué falló (state = error)
}

// AntigravityMCPStatus pregunta a `agy mcp list` cómo está la entrada. No
// escribe nada.
func (a *App) AntigravityMCPStatus() (AntigravityMCP, error) {
	return a.agyStatus(context.Background()), nil
}

func (a *App) agyStatus(ctx context.Context) AntigravityMCP {
	st := AntigravityMCP{}
	bin, err := mcpBinary()
	if err != nil || bin == "" {
		st.State = mcpconf.AgyError
		if err != nil {
			st.Detail = err.Error()
		}
		return st
	}
	st.Command = mcpconf.AgyCommand(bin)
	st.AgyPath = agyPath()
	if st.AgyPath == "" {
		st.State = mcpconf.AgyNotInstalled
		return st
	}
	out, err := agyExec(ctx, st.AgyPath, mcpconf.AgyListArgs()...)
	if err != nil {
		st.State, st.Detail = mcpconf.AgyError, agyFailure(out, err)
		return st
	}
	state, row := mcpconf.AgyStateOf(mcpconf.ParseAgyList(out), bin)
	st.State = state
	if row != nil {
		st.Current = row.Command
	}
	return st
}

// AddAntigravityMCP agrega (o reemplaza) la entrada scadlab en la configuración
// global de Antigravity. La interfaz pide confirmación antes de llamarlo.
func (a *App) AddAntigravityMCP() (AntigravityMCP, error) {
	agyMu.Lock()
	defer agyMu.Unlock()
	bin, err := mcpBinary()
	if err != nil || bin == "" {
		return AntigravityMCP{}, i18n.Errorf(i18n.Msg{ES: "no se pudo ubicar el ejecutable de ScadLab: %v", EN: "could not locate the ScadLab executable: %v"}, err)
	}
	path := agyPath()
	if path == "" {
		return AntigravityMCP{}, i18n.New(i18n.Msg{ES: "no se encontró el comando agy de Antigravity", EN: "the Antigravity agy command was not found"})
	}
	ctx := context.Background()
	log.Printf("scadlab: agregando el MCP a la configuración global de Antigravity (%s)", path)
	if out, err := agyExec(ctx, path, mcpconf.AgyAddArgs(bin)...); err != nil {
		return a.agyStatus(ctx), i18n.Errorf(i18n.Msg{ES: "Antigravity no aceptó la entrada: %s", EN: "Antigravity did not accept the entry: %s"}, agyFailure(out, err))
	}
	return a.agyStatus(ctx), nil
}

// RemoveAntigravityMCP quita la entrada scadlab de la configuración global de
// Antigravity. Si no estaba, no hace nada.
func (a *App) RemoveAntigravityMCP() (AntigravityMCP, error) {
	agyMu.Lock()
	defer agyMu.Unlock()
	ctx := context.Background()
	st := a.agyStatus(ctx)
	switch st.State {
	case mcpconf.AgyNotInstalled:
		return st, i18n.New(i18n.Msg{ES: "no se encontró el comando agy de Antigravity", EN: "the Antigravity agy command was not found"})
	case mcpconf.AgyError:
		return st, i18n.Errorf(i18n.Msg{ES: "no se pudo consultar a Antigravity: %s", EN: "could not query Antigravity: %s"}, st.Detail)
	case mcpconf.AgyMissing:
		return st, nil // nada que quitar
	}
	log.Printf("scadlab: quitando el MCP de la configuración global de Antigravity (%s)", st.AgyPath)
	if out, err := agyExec(ctx, st.AgyPath, mcpconf.AgyRemoveArgs()...); err != nil {
		return a.agyStatus(ctx), i18n.Errorf(i18n.Msg{ES: "Antigravity no pudo quitar la entrada: %s", EN: "Antigravity could not remove the entry: %s"}, agyFailure(out, err))
	}
	return a.agyStatus(ctx), nil
}

// agyFailure arma el motivo de un fallo de agy: su propia salida si la tiene
// (suele ser el mensaje útil) y, si no, el error del proceso.
func agyFailure(out string, err error) string {
	if msg := strings.TrimSpace(out); msg != "" {
		return msg
	}
	return err.Error()
}

// ---- Antigravity: registro automático (S12.c) ----

// AntigravityEnsure es el resultado de EnsureAntigravityMCP.
type AntigravityEnsure struct {
	// Added: la entrada se agregó AHORA a la configuración global de Antigravity.
	Added bool `json:"added"`
	// State: cómo quedó la entrada (AgyOK, AgyMissing, AgyOther, AgyDisabled,
	// AgyNotInstalled) o "off" si el servidor MCP está apagado en Configuración.
	State string `json:"state"`
	// PermissionsAdded: reglas `mcp(scadlab/…)` que se agregaron AHORA al settings.json.
	PermissionsAdded int `json:"permissionsAdded"`
	// PermissionsError: por qué no se pudieron aprobar las herramientas (vacío = bien).
	PermissionsError string `json:"permissionsError"`
}

// agyEnsureMu serializa el registro automático: elegir Antigravity dos veces
// seguidas no debe agregar la entrada (ni avisar) dos veces.
var agyEnsureMu sync.Mutex

// EnsureAntigravityMCP deja ScadLab registrado en Antigravity —y sus herramientas
// aprobadas— cuando se lo elige (en el chat o al abrir su terminal). Antigravity solo
// lee MCP de su configuración GLOBAL —no hay por proyecto ni por flag—, y sin él no
// tiene ninguna herramienta de ScadLab: intenta la shell, que está denegada, y el
// turno no deja nada. Antes era un botón opcional (S6); el chat lo vuelve necesario.
//
// Solo AGREGA lo que falta: una entrada que ya existe —aunque apunte a otra
// ScadLab o esté desactivada— es decisión del usuario y no se toca (el botón de
// Configuración la reemplaza si él quiere). Con el servidor MCP apagado en
// Configuración → IA no hace nada. Va por `agy mcp list` y `agy mcp add -- scadlab
// <ScadLab> --mcp`, sin shell y sin `--workspace` (el servidor toma la carpeta
// actual, que es la del workspace).
//
// Después sincroniza los permisos (app_agyperms.go): en cada llamada agrega las
// reglas `mcp(scadlab/<herramienta>)` que falten en `permissions.allow` del
// settings.json de Antigravity y quita las de herramientas que ya no existen. Si el
// usuario las quitó desde Configuración, no las repone.
//
// Emite mcp:antigravity cuando agregó el servidor o reglas (toast «se agregó a su
// configuración global») o falló (toast con el motivo). No tener `agy` no es un fallo.
func (a *App) EnsureAntigravityMCP() (AntigravityEnsure, error) {
	if err := a.ready(); err != nil {
		return AntigravityEnsure{}, err
	}
	agyEnsureMu.Lock()
	defer agyEnsureMu.Unlock()
	s, err := a.store.GetSettings()
	if err != nil {
		return AntigravityEnsure{}, err
	}
	if !s.MCPEnabled {
		return AntigravityEnsure{State: "off"}, nil
	}
	ctx := a.appCtx()
	st := a.agyStatus(ctx)
	res := AntigravityEnsure{State: st.State}
	switch st.State {
	case mcpconf.AgyError:
		err := i18n.Errorf(msgAgyEnsureFailed, st.Detail)
		a.emit(evAntigravityMCP, AntigravityMCPEvent{Error: err.Error()})
		return res, err
	case mcpconf.AgyNotInstalled:
		return res, nil
	case mcpconf.AgyMissing:
		// AddAntigravityMCP toma agyMu por su cuenta y vuelve a leer el estado.
		after, err := a.AddAntigravityMCP()
		res.State = after.State
		if err != nil {
			err = i18n.Errorf(msgAgyEnsureFailed, err.Error())
			a.emit(evAntigravityMCP, AntigravityMCPEvent{Error: err.Error()})
			return res, err
		}
		res.Added = true
		log.Printf("scadlab: ScadLab registrado en la configuración global de Antigravity (%s)", after.Command)
	}

	// Un fallo de permisos no deshace el registro: se avisa y el turno explica por qué.
	perm := a.syncAgyPermissions(false)
	ev := AntigravityMCPEvent{Added: res.Added, Permissions: len(perm.Report.Added), SettingsPath: perm.Report.Path}
	if perm.Err != nil {
		res.PermissionsError = i18n.T(msgAgyPermsFailed, agySettingsPath(), perm.Err)
		ev.Error = res.PermissionsError
	}
	res.PermissionsAdded = ev.Permissions
	if ev.Added || ev.Permissions > 0 || ev.Error != "" {
		a.emit(evAntigravityMCP, ev)
	}
	return res, nil
}

var msgAgyEnsureFailed = i18n.Msg{
	ES: "No se pudo agregar ScadLab a la configuración global de Antigravity: %s. Sin ese servidor, Antigravity no puede crear checkpoints desde el chat; agrégalo a mano en Configuración → IA → «Agregar a Antigravity».",
	EN: "Could not add ScadLab to Antigravity's global configuration: %s. Without that server, Antigravity cannot create checkpoints from the chat; add it by hand in Settings → AI → “Add to Antigravity”.",
}
