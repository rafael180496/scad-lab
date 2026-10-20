package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/slicers"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
	"scadlab/backend/wsconfig"
)

// Configuración propia de cada workspace (S14): impresora y colores, skills
// activas y ajustes de impresión. La lógica (qué vale, cómo se hereda, qué se
// valida) vive en backend/wsconfig, la misma que lee el servidor MCP; aquí solo
// se arman los contratos para la interfaz.

// evWorkspaceConfig avisa a la interfaz que cambió la configuración de un
// workspace: tras SaveWorkspaceConfig y tras un paso bueno de una herramienta de
// la IA que la cambia (mcp.EffectConfig). El payload es un WorkspaceConfigView.
const evWorkspaceConfig = "workspace:config"

// EffectivePrint son los ajustes de impresión que de verdad rigen en un
// workspace: lo que fijó él, o lo heredado de Configuración → Impresión y de la
// impresora (el diámetro del filamento).
type EffectivePrint struct {
	Material           string  `json:"material"`
	InfillPercent      float64 `json:"infillPercent"`
	WallLoops          int     `json:"wallLoops"`
	LineWidthMm        float64 `json:"lineWidthMm"`
	FilamentDiameterMm float64 `json:"filamentDiameterMm"`
}

// WorkspaceConfigView es la configuración de un workspace, resuelta.
type WorkspaceConfigView struct {
	Workspace store.Workspace `json:"workspace"`
	// Printer es la impresora EFECTIVA: la del catálogo con multicolor_slots = los
	// colores del workspace (max_colors es el tope de la máquina).
	Printer printers.Printer `json:"printer"`
	Colors  int              `json:"colors"`
	// Skills son las que tiene activas (su lista, o las de por defecto si todavía
	// no tiene una); DefaultSkills las que tendría con la impresora actual si se
	// partiera de cero.
	Skills        []string             `json:"skills"`
	DefaultSkills []string             `json:"defaultSkills"`
	Print         store.WorkspacePrint `json:"print"` // solo lo que el workspace fijó
	Effective     EffectivePrint       `json:"effective"`
}

// WorkspaceConfigInput es lo que guarda «Configuración del workspace».
type WorkspaceConfigInput struct {
	PrinterID string `json:"printerId"`
	// Skills nil pide las de por defecto para la impresora; vacío, ninguna.
	Skills []string             `json:"skills"`
	Print  store.WorkspacePrint `json:"print"`
}

// WorkspaceConfigResult es lo que pasó al guardar: la configuración ya resuelta y
// qué skills se retiraron o se dejaron (porque alguien editó su copia).
type WorkspaceConfigResult struct {
	Config    WorkspaceConfigView `json:"config"`
	Removed   []string            `json:"removed"`
	Kept      []string            `json:"kept"`
	SyncError string              `json:"syncError"` // reglas o skills que no se pudieron escribir; la configuración sí quedó guardada
}

// NewWorkspaceInput es lo que pide «Nuevo workspace».
type NewWorkspaceInput struct {
	Name      string `json:"name"`
	ParentDir string `json:"parentDir"` // vacío = la carpeta predeterminada
	PrinterID string `json:"printerId"` // vacío = la impresora por defecto de Configuración
	// Skills nil = las de por defecto para esa impresora; se guardan explícitas.
	Skills []string             `json:"skills"`
	Print  store.WorkspacePrint `json:"print"`
}

// workspaceConfigView resuelve la configuración de un workspace.
func (a *App) workspaceConfigView(w store.Workspace) (WorkspaceConfigView, error) {
	s, err := a.store.GetSettings()
	if err != nil {
		return WorkspaceConfigView{}, err
	}
	all, err := a.discoverSkills()
	if err != nil {
		return WorkspaceConfigView{}, err
	}
	enabled, err := wsconfig.GlobalEnabled(a.store)
	if err != nil {
		return WorkspaceConfigView{}, err
	}
	pr := wsconfig.EffectivePrinter(w)
	eff := wsconfig.EffectiveSettings(s, w, pr)
	return WorkspaceConfigView{
		Workspace:     w,
		Printer:       pr,
		Colors:        pr.MulticolorSlots,
		Skills:        wsconfig.EffectiveSkills(w, all, enabled, pr),
		DefaultSkills: wsconfig.DefaultSkills(all, enabled, pr),
		Print:         w.Print,
		Effective: EffectivePrint{
			Material: eff.DefaultMaterial, InfillPercent: eff.InfillPercent, WallLoops: eff.WallLoops,
			LineWidthMm: eff.LineWidthMm, FilamentDiameterMm: eff.FilamentDiameterMm,
		},
	}, nil
}

// emitWorkspaceConfig relee el workspace y avisa a la interfaz de su
// configuración vigente. Lo usan SaveWorkspaceConfig y refreshTreeAfterStep.
func (a *App) emitWorkspaceConfig(id int64) {
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		log.Printf("scadlab: configuración del workspace %d: %v", id, err)
		return
	}
	v, err := a.workspaceConfigView(w)
	if err != nil {
		log.Printf("scadlab: configuración de %s: %v", w.Path, err)
		return
	}
	a.emit(evWorkspaceConfig, v)
}

// GetWorkspaceConfig devuelve la configuración resuelta de un workspace: su
// impresora efectiva, sus colores, las skills que tiene activas y los ajustes de
// impresión (lo que fijó y lo que rige).
func (a *App) GetWorkspaceConfig(id int64) (WorkspaceConfigView, error) {
	if err := a.ready(); err != nil {
		return WorkspaceConfigView{}, err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return WorkspaceConfigView{}, err
	}
	return a.workspaceConfigView(w)
}

// SaveWorkspaceConfig valida y guarda la configuración de un workspace, reescribe
// sus reglas (CLAUDE.md, AGENTS.md, printer.json) con la impresora efectiva y
// sincroniza SUS skills: instala las que quedaron y retira las que salieron, salvo
// que alguien haya editado la copia (van en Kept). Un valor fuera de rango es un
// error y no se guarda nada. Emite workspace:config.
func (a *App) SaveWorkspaceConfig(id int64, in WorkspaceConfigInput) (WorkspaceConfigResult, error) {
	if err := a.ready(); err != nil {
		return WorkspaceConfigResult{}, err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return WorkspaceConfigResult{}, err
	}
	printerID := strings.TrimSpace(in.PrinterID)
	if printerID == "" {
		printerID = w.PrinterID
	}
	saved, res, err := wsconfig.Apply(a.store, a.userSkillsDir(), w, printerID, in.Skills, in.Print)
	if err != nil {
		return WorkspaceConfigResult{}, err
	}
	v, err := a.workspaceConfigView(saved)
	if err != nil {
		return WorkspaceConfigResult{}, err
	}
	a.emit(evWorkspaceConfig, v)
	return WorkspaceConfigResult{Config: v, Removed: res.Removed, Kept: res.Kept, SyncError: res.SyncError}, nil
}

// DefaultWorkspaceSkills son las skills con que nacería un workspace de esa
// impresora: las encendidas en Configuración → Skills que no son de marca, más la
// skill de la marca de la impresora. «Nuevo workspace» las muestra marcadas.
// printerID vacío usa la impresora por defecto de Configuración.
func (a *App) DefaultWorkspaceSkills(printerID string) ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	pr, err := a.resolvePrinter(printerID)
	if err != nil {
		return nil, err
	}
	// Al día con lo que hay en disco: una skill propia recién creada tiene que
	// figurar aunque nadie haya listado las skills todavía.
	if err := a.refreshSkillRegistry(); err != nil {
		return nil, err
	}
	all, err := a.discoverSkills()
	if err != nil {
		return nil, err
	}
	enabled, err := wsconfig.GlobalEnabled(a.store)
	if err != nil {
		return nil, err
	}
	return wsconfig.DefaultSkills(all, enabled, pr), nil
}

// ListPrinterBrands devuelve las marcas del catálogo en el orden de la app.
func (a *App) ListPrinterBrands() ([]printers.Brand, error) { return printers.Brands() }

// resolvePrinter busca la impresora pedida: vacía = la de Configuración (que cae
// en la A1 mini si ya no existe); un id que no está en el catálogo es un error.
func (a *App) resolvePrinter(id string) (printers.Printer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = store.DefaultSettings().DefaultPrinter
		if s, err := a.store.GetSettings(); err == nil && s.DefaultPrinter != "" {
			id = s.DefaultPrinter
		}
		p, _ := printers.ByID(id)
		return p, nil
	}
	p, ok := printers.ByID(id)
	if !ok {
		return p, i18n.Errorf(i18n.Msg{ES: "no existe la impresora %q", EN: "there is no printer %q"}, id)
	}
	return p, nil
}

// CreateWorkspaceWith crea un workspace con la impresora, los colores, las skills
// y los ajustes de impresión elegidos. Escribe las reglas con la impresora
// efectiva, un primer checkpoint por defecto e instala SOLO las skills elegidas
// (nil = las de por defecto de esa impresora): la lista queda guardada explícita,
// así cambiar Configuración → Skills después no altera este workspace.
func (a *App) CreateWorkspaceWith(in NewWorkspaceInput) (store.Workspace, error) {
	if err := a.ready(); err != nil {
		return store.Workspace{}, err
	}
	name := strings.TrimSpace(in.Name)
	pr, err := a.resolvePrinter(in.PrinterID)
	if err != nil {
		return store.Workspace{}, err
	}
	print := in.Print // los colores viajan en Print.Colors (0 = los de fábrica de la impresora)
	if err := wsconfig.Validate(a.store, pr, print); err != nil {
		return store.Workspace{}, err
	}
	names, err := a.resolveSkillNames(pr, in.Skills)
	if err != nil {
		return store.Workspace{}, err
	}
	w := store.Workspace{Name: name, PrinterID: pr.ID, Skills: names, Print: print}
	dir, err := workspace.Create(a.workspaceParent(in.ParentDir), name, wsconfig.EffectivePrinter(w))
	if err != nil {
		return store.Workspace{}, err
	}
	w.Path = dir
	if w.ID, err = a.store.InsertWorkspace(w); err != nil {
		return store.Workspace{}, err
	}
	a.writeStarter(dir, name)
	a.syncWorkspaceMCPQuiet(w)
	if err := a.syncSkills(w); err != nil {
		return w, i18n.Errorf(i18n.Msg{ES: "workspace creado, pero falló la instalación de skills: %v", EN: "workspace created, but installing skills failed: %v"}, err)
	}
	return a.store.GetWorkspace(w.ID)
}

// resolveSkillNames valida la lista de skills pedida (nil = las de por defecto de
// la impresora) contra las que existen, con el registro al día.
func (a *App) resolveSkillNames(pr printers.Printer, names []string) ([]string, error) {
	if err := a.refreshSkillRegistry(); err != nil {
		return nil, err
	}
	all, err := a.discoverSkills()
	if err != nil {
		return nil, err
	}
	enabled, err := wsconfig.GlobalEnabled(a.store)
	if err != nil {
		return nil, err
	}
	return wsconfig.ResolveSkills(all, enabled, pr, names)
}

// importWorkspace registra una carpeta existente con la impresora pedida y las
// skills de por defecto de esa impresora. Escribe las reglas solo en su bloque
// marcado (un CLAUDE.md propio de la carpeta se conserva debajo).
func (a *App) importWorkspace(dir, printerID string) (store.Workspace, error) {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return store.Workspace{}, i18n.New(i18n.Msg{ES: "la carpeta no existe", EN: "the folder does not exist"})
	}
	pr, err := a.resolvePrinter(printerID)
	if err != nil {
		return store.Workspace{}, err
	}
	names, err := a.resolveSkillNames(pr, nil)
	if err != nil {
		return store.Workspace{}, err
	}
	name := filepath.Base(dir)
	w := store.Workspace{Name: name, Path: dir, PrinterID: pr.ID, Skills: names}
	if err := workspace.WriteRules(dir, name, wsconfig.EffectivePrinter(w)); err != nil {
		return store.Workspace{}, err
	}
	if w.ID, err = a.store.InsertWorkspace(w); err != nil {
		return store.Workspace{}, err
	}
	a.writeStarter(dir, name)
	_ = a.syncSkills(w)
	a.syncWorkspaceMCPQuiet(w)
	return a.store.GetWorkspace(w.ID)
}

// recommendedSlicerName es el nombre del slicer recomendado de una impresora
// ("" si el catálogo no nombra ninguno que el registro conozca).
func recommendedSlicerName(p printers.Printer) string {
	if d, ok := slicers.Recommended(p.Slicers); ok {
		return d.Name
	}
	return ""
}
