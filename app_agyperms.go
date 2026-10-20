package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"scadlab/backend/clichat"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
)

// Permisos de las herramientas de ScadLab en Antigravity (S12.c, T14). El porqué,
// lo medido y por qué es `permissions.allow` y no un hook están en
// backend/clichat/agyperms.go y en claude/26-antigravity-permisos.md.
//
// En corto: ScadLab agrega al settings.json GLOBAL de Antigravity una regla
// `mcp(scadlab/<herramienta>)` por cada herramienta de su catálogo —las de lectura
// y las de escritura—, con una lista explícita y nada más (ni comodines, ni
// `command(...)`). Lo sincroniza al registrar el MCP y en cada turno de
// Antigravity, y lo quita o lo repone desde Configuración → Proveedores de IA.
// «Solo consulta» no puede viajar en una regla global: lo hace cumplir el servidor
// `--mcp` con una marca en el workspace (backend/turnmark).

// evAntigravityDenied avisa a la interfaz de que Antigravity igual bloqueó una
// herramienta de ScadLab por permisos (toast con «Reparar permisos de Antigravity»).
// Payload: AntigravityDeniedEvent.
const evAntigravityDenied = "mcp:antigravity-denied"

// AntigravityDeniedEvent es el payload de mcp:antigravity-denied.
type AntigravityDeniedEvent struct {
	Tool         string `json:"tool"`
	SettingsPath string `json:"settingsPath"`
	// Managed: ScadLab mantiene las reglas al día. false = el usuario las quitó
	// desde Configuración y hay que volver a agregarlas.
	Managed bool `json:"managed"`
}

// agySettingsPath es el settings.json de Antigravity que ScadLab gestiona. Variable
// para que las pruebas nunca toquen el del usuario: TestMain la deja vacía («no se
// sabe») y las que ejercitan los permisos ponen la suya en un directorio temporal.
var agySettingsPath = clichat.AgySettingsPath

// agyPermsOffFile es la marca (en la carpeta de datos) de que el usuario quitó los
// permisos desde Configuración: mientras exista, ScadLab NO los vuelve a agregar
// solo. «Volver a agregar» la borra.
const agyPermsOffFile = "antigravity-permissions-off"

func (a *App) agyPermsManaged() bool {
	if a.dataDir == "" {
		return true
	}
	_, err := os.Stat(filepath.Join(a.dataDir, agyPermsOffFile))
	return errors.Is(err, fs.ErrNotExist)
}

func (a *App) setAgyPermsManaged(managed bool) error {
	if a.dataDir == "" {
		return nil
	}
	path := filepath.Join(a.dataDir, agyPermsOffFile)
	if managed {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte("El usuario quitó los permisos de ScadLab de Antigravity desde Configuración.\n"), 0o644)
}

// agyBackupDir recibe una copia del settings.json de Antigravity antes de cada cambio.
func (a *App) agyBackupDir() string {
	if a.dataDir == "" {
		return ""
	}
	return filepath.Join(a.dataDir, "backups", "antigravity")
}

// agyPermOutcome es lo que pasó al sincronizar.
type agyPermOutcome struct {
	Report  clichat.AgyPermReport
	Skipped bool // no se tocó nada: sin ruta, o el usuario las quitó
	Err     error
}

// agyPermSyncMu hace que dos sincronizaciones seguidas no avisen dos veces lo mismo.
var agyPermSyncMu sync.Mutex

// syncAgyPermissions deja en el settings.json de Antigravity las reglas del catálogo
// actual: agrega las que faltan y quita las `mcp(scadlab/…)` que ya no existen. Con
// force=false respeta que el usuario las haya quitado.
func (a *App) syncAgyPermissions(force bool) agyPermOutcome {
	agyPermSyncMu.Lock()
	defer agyPermSyncMu.Unlock()
	path := agySettingsPath()
	if path == "" || (!force && !a.agyPermsManaged()) {
		return agyPermOutcome{Skipped: true}
	}
	rep, err := clichat.SyncAgyPermissions(path, a.agyBackupDir())
	if err != nil {
		log.Printf("scadlab: permisos de Antigravity en %s: %v", path, err)
		return agyPermOutcome{Report: rep, Err: err}
	}
	if rep.Changed {
		log.Printf("scadlab: permisos de Antigravity en %s: %d agregadas, %d quitadas (copia: %s)", path, len(rep.Added), len(rep.Removed), rep.Backup)
	}
	return agyPermOutcome{Report: rep}
}

// AntigravityPermissions es el estado de los permisos de ScadLab en Antigravity, para
// Configuración → Proveedores de IA. State: ok | partial | none | unreadable |
// unavailable | not-installed.
type AntigravityPermissions struct {
	State    string `json:"state"`
	Path     string `json:"path"`
	Approved int    `json:"approved"` // herramientas de ScadLab con su regla
	Total    int    `json:"total"`    // herramientas del catálogo
	Stale    int    `json:"stale"`    // reglas de herramientas que ya no existen
	// Managed: ScadLab las mantiene al día. false = las quitaste y no se vuelven a
	// agregar solas.
	Managed bool   `json:"managed"`
	Detail  string `json:"detail"` // por qué no se pudo leer (state = unreadable)
}

func (a *App) agyPermStatus() AntigravityPermissions {
	st := AntigravityPermissions{Path: agySettingsPath(), Managed: a.agyPermsManaged(), Total: len(clichat.AgyRules())}
	switch {
	case agyPath() == "":
		st.State = "not-installed"
		return st
	case st.Path == "":
		st.State = "unavailable"
		return st
	}
	info, err := clichat.InspectAgyPermissions(st.Path)
	if err != nil {
		st.State, st.Detail = "unreadable", err.Error()
		return st
	}
	st.Approved, st.Stale = info.Approved, info.Stale
	switch {
	case info.Approved == info.Total:
		st.State = "ok"
	case info.Approved == 0:
		st.State = "none"
	default:
		st.State = "partial"
	}
	return st
}

// AntigravityPermissionsStatus lee el settings.json de Antigravity (sin escribir) y
// dice cuántas herramientas de ScadLab tienen su regla.
func (a *App) AntigravityPermissionsStatus() (AntigravityPermissions, error) {
	return a.agyPermStatus(), nil
}

// RepairAntigravityPermissions es «Volver a agregar» y «Reparar permisos de
// Antigravity»: vuelve a dejar las reglas del catálogo en el settings.json aunque el
// usuario las hubiera quitado (y ScadLab vuelve a mantenerlas al día). La interfaz
// pide confirmación antes de llamarlo desde Configuración.
func (a *App) RepairAntigravityPermissions() (AntigravityPermissions, error) {
	if err := a.setAgyPermsManaged(true); err != nil {
		return a.agyPermStatus(), err
	}
	out := a.syncAgyPermissions(true)
	if out.Err != nil {
		return a.agyPermStatus(), i18n.Errorf(msgAgyPermsFailed, agySettingsPath(), out.Err)
	}
	if out.Skipped {
		return a.agyPermStatus(), i18n.New(msgAgyNoSettings)
	}
	return a.agyPermStatus(), nil
}

// RemoveAntigravityPermissions quita del settings.json de Antigravity SOLO las reglas
// `mcp(scadlab/…)` y deja de agregarlas solas hasta que se pida «Volver a agregar».
// Las demás reglas del archivo no se tocan.
func (a *App) RemoveAntigravityPermissions() (AntigravityPermissions, error) {
	agyPermSyncMu.Lock()
	defer agyPermSyncMu.Unlock()
	path := agySettingsPath()
	if path == "" {
		return a.agyPermStatus(), i18n.New(msgAgyNoSettings)
	}
	rep, err := clichat.RemoveAgyPermissions(path, a.agyBackupDir())
	if err != nil {
		log.Printf("scadlab: quitar los permisos de Antigravity en %s: %v", path, err)
		return a.agyPermStatus(), i18n.Errorf(msgAgyPermsFailed, path, err)
	}
	if err := a.setAgyPermsManaged(false); err != nil {
		return a.agyPermStatus(), err
	}
	log.Printf("scadlab: permisos de Antigravity quitados de %s (%d reglas, copia: %s)", path, len(rep.Removed), rep.Backup)
	return a.agyPermStatus(), nil
}

// cleanLegacyAgyHook retira la entrada `scadlab-managed` que una versión anterior de
// ScadLab pudo dejar en <workspace>/.agents/hooks.json (ver clichat.RemoveLegacyAgyHook).
func cleanLegacyAgyHook(workspaceDir string) {
	removed, err := clichat.RemoveLegacyAgyHook(workspaceDir)
	switch {
	case err != nil:
		log.Printf("scadlab: limpiar el hook anterior de Antigravity en %s: %v", workspaceDir, err)
	case removed:
		log.Printf("scadlab: se quitó el hook anterior de Antigravity de %s", workspaceDir)
	}
}

var (
	msgAgyPermsFailed = i18n.Msg{
		ES: "No se pudieron aprobar las herramientas de ScadLab en Antigravity (%s: %v): sin esa aprobación las bloquea en modo no interactivo y no puede crear checkpoints desde el chat. Arréglalo y usa «Reparar permisos de Antigravity» (Configuración → Proveedores de IA).",
		EN: "ScadLab's tools could not be approved in Antigravity (%s: %v): without that approval it blocks them in non-interactive mode and cannot create checkpoints from the chat. Fix it and use “Repair Antigravity permissions” (Settings → AI providers).",
	}
	msgAgyNoSettings = i18n.Msg{
		ES: "No se pudo ubicar el settings.json de Antigravity en este equipo.",
		EN: "Antigravity's settings.json could not be located on this computer.",
	}
	msgCLIAgyDenied = i18n.Msg{
		ES: "Antigravity bloqueó la herramienta «%s» de ScadLab: en modo no interactivo no puede pedirte permiso y termina el turno. ScadLab la aprueba con una regla `mcp(scadlab/…)` en `permissions.allow` de %s. Usa «Reparar permisos de Antigravity» (en el aviso, o en Configuración → Proveedores de IA) y vuelve a pedirlo.%s Detalle: %s",
		EN: "Antigravity blocked ScadLab's “%s” tool: in non-interactive mode it cannot ask you for permission and ends the turn. ScadLab approves it with an `mcp(scadlab/…)` rule in `permissions.allow` of %s. Use “Repair Antigravity permissions” (in the notice, or in Settings → AI providers) and ask again.%s Detail: %s",
	}
	msgCLIAgyDeniedOff = i18n.Msg{
		ES: " Quitaste esos permisos desde Configuración y ScadLab no los vuelve a agregar solo.",
		EN: " You removed those permissions from Settings and ScadLab does not add them back by itself.",
	}
	msgCLIAgyDeniedUntrusted = i18n.Msg{
		ES: " Además, Antigravity todavía no confía en la carpeta de este proyecto: ábrela una vez con Antigravity (su terminal en ScadLab, o `agy` dentro de la carpeta) y acepta «Yes, I trust this folder».",
		EN: " Also, Antigravity does not trust this project's folder yet: open it once with Antigravity (its terminal in ScadLab, or `agy` inside the folder) and accept “Yes, I trust this folder”.",
	}
	msgCLIAgyMarkFailed = i18n.Msg{
		ES: "No se pudo marcar el turno como «Solo consulta» (%v). Antigravity tiene aprobadas las herramientas de ScadLab que modifican el proyecto, así que el turno no se ejecutó para no arriesgar cambios. Revisa los permisos de la carpeta del proyecto.",
		EN: "The turn could not be marked as “Query only” (%v). Antigravity has ScadLab's project-changing tools approved, so the turn was not run so as not to risk changes. Check the permissions of the project folder.",
	}
)

// agyDeniedRe lee el rechazo de Antigravity a una herramienta MCP:
//
//	permission check failed for mcp "scadlab/printer_info": user denied permission for mcp(scadlab/printer_info)
var agyDeniedRe = regexp.MustCompile(`permission check failed for mcp "([^"/]+)/([^"]+)"`)

// agyDenial devuelve la primera herramienta de ScadLab que Antigravity bloqueó por
// permisos en el turno (ya con su nombre real, no `call_mcp_tool`) y el texto del
// rechazo. Vacío si ninguna.
func agyDenial(steps []hermes.Step) (tool, detail string) {
	for _, st := range steps {
		if st.Status != hermes.StepError {
			continue
		}
		if m := agyDeniedRe.FindStringSubmatch(st.Text); m != nil {
			if m[1] != "scadlab" {
				continue
			}
			return m[2], clipText(strings.TrimSpace(st.Text), 300)
		}
		if strings.Contains(strings.ToLower(st.Text), "user denied permission for mcp(scadlab/") {
			return st.Tool, clipText(strings.TrimSpace(st.Text), 300)
		}
	}
	return "", ""
}

// emitAgyDenied avisa a la interfaz si el turno de Antigravity terminó con una
// herramienta de ScadLab bloqueada por permisos: el aviso trae el botón «Reparar
// permisos de Antigravity».
func (a *App) emitAgyDenied(steps []hermes.Step) {
	tool, _ := agyDenial(steps)
	if tool == "" {
		return
	}
	a.emit(evAntigravityDenied, AntigravityDeniedEvent{Tool: tool, SettingsPath: agySettingsPath(), Managed: a.agyPermsManaged()})
}
