package main

import (
	"scadlab/backend/skills"
)

// Editor de las skills (Configuración → Skills → Editar / Ver). Cada binding
// recibe el nombre de la skill y una ruta relativa a su carpeta; backend/skills
// confina esa ruta (nada de «..», rutas absolutas ni enlaces que salgan).
// Las skills incluidas se pueden leer, no escribir.

// ListSkillFiles lista los archivos de una skill, SKILL.md primero.
func (a *App) ListSkillFiles(name string) ([]skills.File, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s, err := a.anySkill(name)
	if err != nil {
		return nil, err
	}
	return skills.ListFiles(s)
}

// ReadSkillFile lee un archivo de texto de una skill.
func (a *App) ReadSkillFile(name, rel string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	s, err := a.anySkill(name)
	if err != nil {
		return "", err
	}
	return skills.ReadFile(s, rel)
}

// ValidateSkillMarkdown revisa el frontmatter de un SKILL.md (name y
// description obligatorios, name igual a la carpeta). El editor lo llama mientras
// se escribe para marcar los problemas; los mensajes salen en el idioma activo.
func (a *App) ValidateSkillMarkdown(name, text string) []skills.Diagnostic {
	return skills.ValidateSkillMD(name, []byte(text))
}

// SaveSkillFileResult es el resultado de guardar un archivo de una skill.
type SaveSkillFileResult struct {
	// Saved es false cuando el SKILL.md tiene errores de frontmatter: no se
	// escribe nada, porque un SKILL.md roto se instalaría en los workspaces y
	// los agentes lo ignorarían sin avisar.
	Saved       bool                `json:"saved"`
	Diagnostics []skills.Diagnostic `json:"diagnostics"`
	SyncError   string              `json:"syncError"`
}

// SaveSkillFile guarda un archivo de una skill propia y la reinstala en los
// proyectos abiertos (el mismo Sync de siempre). Si es el SKILL.md, además
// actualiza el registro: la descripción nueva se ve en la lista.
func (a *App) SaveSkillFile(name, rel, content string) (SaveSkillFileResult, error) {
	if err := a.ready(); err != nil {
		return SaveSkillFileResult{}, err
	}
	s, err := a.userSkill(name)
	if err != nil {
		return SaveSkillFileResult{}, err
	}
	clean, err := skills.CleanRel(rel)
	if err != nil {
		return SaveSkillFileResult{}, err
	}
	res := SaveSkillFileResult{Diagnostics: []skills.Diagnostic{}}
	if clean == skills.MainFile {
		res.Diagnostics = skills.ValidateSkillMD(s.Name, []byte(content))
		if skills.HasErrors(res.Diagnostics) {
			return res, nil
		}
	}
	if err := skills.WriteFile(s.Dir, clean, content); err != nil {
		return res, err
	}
	res.Saved = true
	if clean == skills.MainFile {
		if err := a.refreshSkillRegistry(); err != nil {
			return res, err
		}
	}
	res.SyncError = a.syncOpenWorkspaces(name)
	return res, nil
}

// CreateSkillFile crea un archivo vacío (con sus carpetas) en una skill propia.
func (a *App) CreateSkillFile(name, rel string) error {
	if err := a.ready(); err != nil {
		return err
	}
	s, err := a.userSkill(name)
	if err != nil {
		return err
	}
	return skills.CreateFile(s.Dir, rel)
}

// RenameSkillFile renombra o mueve un archivo dentro de una skill propia.
func (a *App) RenameSkillFile(name, from, to string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	s, err := a.userSkill(name)
	if err != nil {
		return "", err
	}
	if err := skills.RenameFile(s.Dir, from, to); err != nil {
		return "", err
	}
	return a.syncOpenWorkspaces(name), nil
}

// DeleteSkillFile manda un archivo de una skill propia a la papelera de la app
// (<datos>/trash/skill-files) y devuelve dónde quedó. SKILL.md no se borra.
func (a *App) DeleteSkillFile(name, rel string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	s, err := a.userSkill(name)
	if err != nil {
		return "", err
	}
	trash, err := skills.DeleteFile(s.Dir, a.dataDir, name, rel)
	if err != nil {
		return "", err
	}
	_ = a.syncOpenWorkspaces(name)
	return trash, nil
}
