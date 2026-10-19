package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/osopen"
	"scadlab/backend/skills"
	"scadlab/backend/store"
)

// openFolder abre una carpeta en el Finder / Explorador. Es una variable para
// que las pruebas no abran nada del sistema (ninguna prueba lanza aplicaciones).
var openFolder = osopen.Open

// SkillView es una skill con su estado en el registro.
type SkillView struct {
	skills.Skill
	Enabled bool `json:"enabled"`
}

// userSkillsDir es la carpeta de las skills propias: la que eligió el usuario
// (settings.skillsDir, si es una ruta absoluta) o <datos>/skills.
func (a *App) userSkillsDir() string {
	if a.store != nil {
		if s, err := a.store.GetSettings(); err == nil {
			if d := strings.TrimSpace(s.SkillsDir); d != "" && filepath.IsAbs(d) {
				return filepath.Clean(d)
			}
		}
	}
	return skills.UserDir(a.dataDir)
}

// discoverSkills lista las skills con la carpeta propia vigente.
func (a *App) discoverSkills() ([]skills.Skill, error) { return skills.Discover(a.userSkillsDir()) }

// refreshSkillRegistry sincroniza la tabla skills con lo descubierto.
func (a *App) refreshSkillRegistry() error {
	dir := a.userSkillsDir()
	list, err := skills.Discover(dir)
	if err != nil {
		return err
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
		if err := a.store.UpsertSkill(store.SkillRow{Name: s.Name, Source: s.Source, Description: s.Description}); err != nil {
			return err
		}
	}
	// Una carpeta que no está (un disco externo desmontado) no es una carpeta
	// vacía: sus skills no se olvidan, para no perder cuáles estaban apagadas.
	if _, err := os.Stat(dir); err != nil {
		if rows, lerr := a.store.ListSkills(); lerr == nil {
			for _, r := range rows {
				if r.Source == "user" {
					names = append(names, r.Name)
				}
			}
		}
	}
	return a.store.PruneSkills(names)
}

// ListSkills devuelve las skills (incluidas y del usuario) con su estado.
func (a *App) ListSkills() ([]SkillView, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if err := a.refreshSkillRegistry(); err != nil {
		return nil, err
	}
	list, err := a.discoverSkills()
	if err != nil {
		return nil, err
	}
	enabled, err := a.enabledSkills()
	if err != nil {
		return nil, err
	}
	out := make([]SkillView, len(list))
	for i, s := range list {
		out[i] = SkillView{Skill: s, Enabled: enabled[s.Name]}
	}
	return out, nil
}

// SetSkillEnabled prende o apaga una skill. Se aplica a los workspaces la
// próxima vez que se sincronizan.
func (a *App) SetSkillEnabled(name string, enabled bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetSkillEnabled(name, enabled)
}

// CreateSkill crea una skill propia desde la plantilla, con su `name:` ya puesto
// y una descripción pendiente, y devuelve la carpeta creada. No abre nada: la
// interfaz abre el editor de la skill. No la instala en los workspaces todavía:
// eso pasa al guardar, cuando la descripción ya dice algo.
func (a *App) CreateSkill(name string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	dir, err := skills.CreateFromTemplate(a.userSkillsDir(), name)
	if err != nil {
		return "", err
	}
	if err := a.refreshSkillRegistry(); err != nil {
		return dir, err
	}
	return dir, nil
}

// SkillsFolder es la carpeta de las skills propias.
type SkillsFolder struct {
	Path    string `json:"path"`
	Default string `json:"default"`
	Custom  bool   `json:"custom"`
	Exists  bool   `json:"exists"`
	Count   int    `json:"count"` // skills propias que hay en ella
}

// GetSkillsFolder dice dónde están las skills propias.
func (a *App) GetSkillsFolder() (SkillsFolder, error) {
	if err := a.ready(); err != nil {
		return SkillsFolder{}, err
	}
	dir := a.userSkillsDir()
	def := skills.UserDir(a.dataDir)
	f := SkillsFolder{Path: dir, Default: def, Custom: filepath.Clean(dir) != filepath.Clean(def)}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		f.Exists = true
	}
	if list, err := a.discoverSkills(); err == nil {
		for _, s := range list {
			if s.Source == "user" {
				f.Count++
			}
		}
	}
	return f, nil
}

// OpenSkillsFolder abre la carpeta de las skills propias en el Finder /
// Explorador. Si todavía no existe (no se creó ninguna skill) la crea: antes se
// le pasaba la ruta inexistente al sistema, que se negaba sin que nadie lo viera.
func (a *App) OpenSkillsFolder() error {
	if err := a.ready(); err != nil {
		return err
	}
	dir := a.userSkillsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo crear la carpeta de skills %s: %v", EN: "could not create the skills folder %s: %v"}, dir, err)
	}
	return openFolder(dir)
}

// SkillsFolderChange es lo que pasó al cambiar la carpeta de skills propias.
type SkillsFolderChange struct {
	Path      string   `json:"path"`
	Moved     []string `json:"moved"`
	Skipped   []string `json:"skipped"` // no se movieron: ya había una con ese nombre en la carpeta nueva
	SyncError string   `json:"syncError"`
}

// ChangeSkillsFolder cambia la carpeta de las skills propias. dir vacío vuelve a
// la predeterminada (<datos>/skills). Con move, las skills de la carpeta actual
// se mueven a la nueva; sin move solo se apunta a ella y las de la anterior
// quedan donde estaban (dejan de aparecer). Si mover falla a medias, lo movido
// vuelve a su lugar y la carpeta no cambia.
func (a *App) ChangeSkillsFolder(dir string, move bool) (SkillsFolderChange, error) {
	if err := a.ready(); err != nil {
		return SkillsFolderChange{}, err
	}
	oldDir := a.userSkillsDir()
	def := skills.UserDir(a.dataDir)
	target := strings.TrimSpace(dir)
	if target == "" {
		target = def
	}
	clean, err := skills.CheckFolder(oldDir, target)
	if err != nil {
		return SkillsFolderChange{}, err
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return SkillsFolderChange{}, i18n.Errorf(i18n.Msg{ES: "no se pudo crear la carpeta %s: %v", EN: "could not create the folder %s: %v"}, clean, err)
	}
	res := skills.MoveResult{Moved: []string{}, Skipped: []string{}}
	if move {
		res, err = skills.MoveAll(oldDir, clean)
		if err != nil {
			for _, n := range res.Moved { // que no quede a medias
				_ = skills.MovePath(filepath.Join(clean, n), filepath.Join(oldDir, n))
			}
			return SkillsFolderChange{}, err
		}
	}
	stored := clean
	if filepath.Clean(clean) == filepath.Clean(def) {
		stored = ""
	}
	if err := a.store.SetSetting("skillsDir", stored); err != nil {
		for _, n := range res.Moved {
			_ = skills.MovePath(filepath.Join(clean, n), filepath.Join(oldDir, n))
		}
		return SkillsFolderChange{}, err
	}
	if err := a.refreshSkillRegistry(); err != nil {
		return SkillsFolderChange{Path: clean, Moved: res.Moved, Skipped: res.Skipped}, err
	}
	// Las skills de la carpeta nueva se instalan en los proyectos abiertos. Las
	// copias que ya había en los workspaces no se tocan.
	out := SkillsFolderChange{Path: clean, Moved: res.Moved, Skipped: res.Skipped}
	if list, derr := a.discoverSkills(); derr == nil {
		var names []string
		for _, s := range list {
			if s.Source == "user" {
				names = append(names, s.Name)
			}
		}
		out.SyncError = a.syncOpenWorkspaces(names...)
	}
	return out, nil
}

// userSkill busca una skill propia por nombre. Las incluidas se ven pero no se
// editan.
func (a *App) userSkill(name string) (skills.Skill, error) {
	list, err := a.discoverSkills()
	if err != nil {
		return skills.Skill{}, err
	}
	for _, s := range list {
		if s.Name == name {
			if s.Source != "user" {
				return skills.Skill{}, skills.ReadOnlyError()
			}
			return s, nil
		}
	}
	return skills.Skill{}, i18n.New(i18n.Msg{ES: "no se encontró la skill", EN: "the skill was not found"})
}

// anySkill busca una skill (incluida o propia) por nombre.
func (a *App) anySkill(name string) (skills.Skill, error) {
	list, err := a.discoverSkills()
	if err != nil {
		return skills.Skill{}, err
	}
	for _, s := range list {
		if s.Name == name {
			return s, nil
		}
	}
	return skills.Skill{}, i18n.New(i18n.Msg{ES: "no se encontró la skill", EN: "the skill was not found"})
}

// workspacePaths son las carpetas de todos los workspaces registrados.
func (a *App) workspacePaths() []string {
	ws, err := a.store.ListWorkspaces()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Path)
	}
	return out
}

// DeleteSkillResult es lo que pasó al borrar una skill propia.
type DeleteSkillResult struct {
	Trash     string   `json:"trash"`     // dónde quedó la carpeta
	Removed   int      `json:"removed"`   // copias retiradas de los workspaces
	Kept      []string `json:"kept"`      // copias editadas a mano que se dejaron
	SyncError string   `json:"syncError"` // al reinstalar la incluida que la skill borrada tapaba
}

// DeleteSkill borra una skill propia: su carpeta va a la papelera de la app
// (<datos>/trash/skills, recuperable a mano) y se retiran sus copias de los
// workspaces donde se instaló — solo las que instaló ScadLab y nadie editó; las
// editadas se dejan y se informan en Kept. Las incluidas no se borran: se apagan.
func (a *App) DeleteSkill(name string) (DeleteSkillResult, error) {
	if err := a.ready(); err != nil {
		return DeleteSkillResult{}, err
	}
	if _, err := a.userSkill(name); err != nil {
		return DeleteSkillResult{}, err
	}
	res, err := skills.DeleteUser(a.userSkillsDir(), a.dataDir, name, a.workspacePaths())
	out := DeleteSkillResult{Trash: res.Trash, Removed: res.Removed, Kept: res.Kept}
	if out.Kept == nil {
		out.Kept = []string{}
	}
	if res.Trash == "" {
		return out, err
	}
	if rerr := a.refreshSkillRegistry(); rerr != nil {
		err = errors.Join(err, rerr)
	}
	// Si la skill tapaba a una incluida del mismo nombre, esa vuelve.
	if skills.BuiltinExists(name) {
		out.SyncError = a.syncOpenWorkspaces(name)
	}
	return out, err
}

// RenameSkillResult es lo que pasó al renombrar una skill propia.
type RenameSkillResult struct {
	Name      string   `json:"name"`
	Removed   int      `json:"removed"`
	Kept      []string `json:"kept"`
	SyncError string   `json:"syncError"`
}

// RenameSkill renombra una skill propia: carpeta, `name:` de su SKILL.md y
// copias instaladas. Conserva si estaba encendida o apagada. El nombre tiene que
// ser kebab-case y no chocar con otra skill, propia o incluida.
func (a *App) RenameSkill(oldName, newName string) (RenameSkillResult, error) {
	if err := a.ready(); err != nil {
		return RenameSkillResult{}, err
	}
	newName = strings.TrimSpace(newName)
	if _, err := a.userSkill(oldName); err != nil {
		return RenameSkillResult{}, err
	}
	enabled, _ := a.enabledSkills()
	res, err := skills.RenameUser(a.userSkillsDir(), oldName, newName, a.workspacePaths())
	if _, found := a.userSkill(newName); found != nil {
		return RenameSkillResult{}, err // no llegó a renombrarse
	}
	out := RenameSkillResult{Name: newName, Removed: res.Removed, Kept: res.Kept}
	if out.Kept == nil {
		out.Kept = []string{}
	}
	if rerr := a.refreshSkillRegistry(); rerr != nil {
		return out, errors.Join(err, rerr)
	}
	if on, ok := enabled[oldName]; ok && !on {
		_ = a.store.SetSkillEnabled(newName, false)
	}
	out.SyncError = a.syncOpenWorkspaces(newName)
	return out, err
}

// SyncWorkspaceSkills reinstala las skills habilitadas en un workspace.
func (a *App) SyncWorkspaceSkills(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return err
	}
	if err := a.syncSkills(w.Path); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudieron instalar las skills: %v", EN: "could not install the skills: %v"}, err)
	}
	return nil
}

// syncSkills instala en un workspace todas las skills habilitadas.
func (a *App) syncSkills(dir string) error { return a.syncSkillNames(dir) }

// syncSkillNames instala en un workspace las skills nombradas (todas si no se
// nombra ninguna). Sync solo reescribe las que recibe: sincronizar una skill no
// pisa las copias de las demás.
func (a *App) syncSkillNames(dir string, names ...string) error {
	list, err := a.discoverSkills()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		want := map[string]bool{}
		for _, n := range names {
			want[n] = true
		}
		filtered := list[:0:0]
		for _, s := range list {
			if want[s.Name] {
				filtered = append(filtered, s)
			}
		}
		list = filtered
	}
	enabled, err := a.enabledSkills()
	if err != nil {
		return err
	}
	return skills.Sync(dir, list, func(n string) bool { return enabled[n] })
}

// syncOpenWorkspaces reinstala las skills nombradas en los proyectos abiertos
// (los demás se sincronizan al abrirse). Devuelve el texto del error, "" si todo
// salió bien: un fallo acá no deshace el guardado que lo provocó.
func (a *App) syncOpenWorkspaces(names ...string) string {
	s, err := a.store.GetSettings()
	if err != nil {
		return err.Error()
	}
	var errs []error
	for _, id := range s.OpenProjects {
		w, err := a.store.GetWorkspace(id)
		if err != nil {
			continue
		}
		if err := a.syncSkillNames(w.Path, names...); err != nil {
			log.Printf("scadlab: skills de %s: %v", w.Path, err)
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err.Error()
	}
	return ""
}

func (a *App) enabledSkills() (map[string]bool, error) {
	rows, err := a.store.ListSkills()
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.Name] = r.Enabled
	}
	return m, nil
}
