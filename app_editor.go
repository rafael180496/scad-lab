package main

import (
	"encoding/json"

	"scadlab/backend/checkpoint"
	"scadlab/backend/scad"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Editor de código OpenSCAD (S4): linter, formateador, catálogo y guardar
// como checkpoint nuevo. La lógica vive en backend/scad; aquí solo se resuelve
// el workspace, la configuración y la ruta de OpenSCAD.

// LintCheckpointSource revisa el código del editor (sin guardar) con
// OpenSCAD y las reglas propias. fileName es el checkpoint abierto (para las
// reglas de encabezado); "" si todavía no tiene nombre.
func (a *App) LintCheckpointSource(id int64, fileName, source string) ([]scad.Diagnostic, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return nil, err
	}
	if fileName != "" {
		if err := validCheckpointName(fileName); err != nil {
			return nil, err
		}
	}
	s, err := a.store.GetSettings()
	if err != nil {
		return nil, err
	}
	files, err := workspace.Scan(w.Path)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.FileName
	}
	return scad.Lint(a.ctx, source, scad.LintOptions{
		WorkspaceDir:  w.Path,
		FileName:      fileName,
		OpenSCADPath:  a.openscadPath(s.OpenSCADPath),
		ExistingFiles: names,
	}), nil
}

// FormatScad formatea código OpenSCAD con la indentación de Configuración.
// Falla solo si el código tiene un string o un comentario sin cerrar.
func (a *App) FormatScad(source string) (string, error) {
	indent := 4
	if a.store != nil {
		if s, err := a.store.GetSettings(); err == nil {
			indent = editorIndent(s)
		}
	}
	return scad.Format(source, scad.FormatOptions{IndentSize: indent})
}

// editorIndent lee settings.editorIndent (2 o 4; si no, 4). Se lee por su
// nombre JSON y no por el campo para no depender de cuándo se agrega a
// store.Settings (lo agrega la tanda 4.B).
func editorIndent(s store.Settings) int {
	raw, err := json.Marshal(s)
	if err != nil {
		return 4
	}
	var m struct {
		EditorIndent int `json:"editorIndent"`
	}
	if json.Unmarshal(raw, &m) != nil || (m.EditorIndent != 2 && m.EditorIndent != 4) {
		return 4
	}
	return m.EditorIndent
}

// GetScadCatalog devuelve el catálogo del lenguaje para autocompletado y hover.
func (a *App) GetScadCatalog() (scad.Catalog, error) { return scad.LoadCatalog() }

// SaveAsCheckpoint guarda el código del editor como un checkpoint NUEVO hijo
// de parentFile (los checkpoints son inmutables: guardar nunca sobrescribe).
// Escribe el encabezado `// @checkpoint parent=… desc="…"` —reemplaza la
// primera línea si el código ya trae uno— y devuelve el nombre creado. No
// renderiza: lo hace el watcher al ver el archivo nuevo. La lógica vive en
// backend/checkpoint, que comparte con el servidor MCP.
func (a *App) SaveAsCheckpoint(id int64, parentFile, label, description, source string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return "", err
	}
	name, err := checkpoint.Create(w.Path, parentFile, label, description, source)
	if err != nil {
		return "", err
	}
	// Lo guardó el usuario, no el agente que tenga abierto: el vínculo con la
	// sesión (S3) no se le atribuye. Create ya avanzó el padre activo.
	a.checkpoints().markOwn(w.ID, name)
	return name, nil
}
