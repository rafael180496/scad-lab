package main

import (
	"scadlab/backend/i18n"
	"scadlab/backend/osopen"
	"scadlab/backend/skills"
	"scadlab/backend/store"
)

// SkillView es una skill con su estado en el registro.
type SkillView struct {
	skills.Skill
	Enabled bool `json:"enabled"`
}

// refreshSkillRegistry sincroniza la tabla skills con lo descubierto.
func (a *App) refreshSkillRegistry() error {
	list, err := skills.Discover(a.dataDir)
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
	list, err := skills.Discover(a.dataDir)
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

// CreateSkill crea una skill de usuario desde la plantilla y abre su carpeta.
func (a *App) CreateSkill(name string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	dir, err := skills.CreateFromTemplate(a.dataDir, name)
	if err != nil {
		return "", err
	}
	if err := a.refreshSkillRegistry(); err != nil {
		return dir, err
	}
	_ = osopen.Open(dir)
	return dir, nil
}

// OpenSkillsFolder abre <datos>/skills (donde viven las skills del usuario).
func (a *App) OpenSkillsFolder() error {
	if err := a.ready(); err != nil {
		return err
	}
	return osopen.Open(skills.UserDir(a.dataDir))
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

func (a *App) syncSkills(dir string) error {
	list, err := skills.Discover(a.dataDir)
	if err != nil {
		return err
	}
	enabled, err := a.enabledSkills()
	if err != nil {
		return err
	}
	return skills.Sync(dir, list, func(n string) bool { return enabled[n] })
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
