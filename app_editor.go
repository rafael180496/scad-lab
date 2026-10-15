package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
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

// saveMaxAttempts es cuántos números se prueban si otro proceso (el agente)
// toma el nombre entre que se calcula y se crea.
const saveMaxAttempts = 50

// beforeCreateCheckpoint es un gancho para los tests: simula que otro proceso
// crea el archivo justo antes que nosotros.
var beforeCreateCheckpoint func(path string)

var checkpointNumRe = regexp.MustCompile(`^(\d{3,})-(.*)$`)

// SaveAsCheckpoint guarda el código del editor como un checkpoint NUEVO hijo
// de parentFile (los checkpoints son inmutables: guardar nunca sobrescribe).
// Escribe el encabezado `// @checkpoint parent=… desc="…"` —reemplaza la
// primera línea si el código ya trae uno— y devuelve el nombre creado. No
// renderiza: lo hace el watcher al ver el archivo nuevo.
func (a *App) SaveAsCheckpoint(id int64, parentFile, label, description, source string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return "", err
	}
	return saveCheckpoint(w.Path, parentFile, label, description, source)
}

func saveCheckpoint(dir, parentFile, label, description, source string) (string, error) {
	if err := validCheckpointName(parentFile); err != nil {
		return "", err
	}
	if st, err := os.Stat(filepath.Join(dir, parentFile)); err != nil || st.IsDir() {
		return "", i18n.Errorf(i18n.Msg{ES: "el checkpoint padre %s no existe", EN: "the parent checkpoint %s does not exist"}, parentFile)
	}
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = strings.TrimSpace(label)
	}
	content := withCheckpointHeader(source, parentFile, desc)

	name, err := workspace.NextFileName(dir, label)
	if err != nil {
		return "", err
	}
	m := checkpointNumRe.FindStringSubmatch(name)
	if m == nil { // NextFileName siempre da NNN-slug.scad; por las dudas
		return "", i18n.Errorf(i18n.Msg{ES: "nombre de checkpoint inesperado: %s", EN: "unexpected checkpoint name: %s"}, name)
	}
	n, _ := strconv.Atoi(m[1])
	rest := m[2]
	for attempt := 0; attempt < saveMaxAttempts; attempt++ {
		name = fmt.Sprintf("%03d-%s", n+attempt, rest)
		path := filepath.Join(dir, name)
		if beforeCreateCheckpoint != nil {
			beforeCreateCheckpoint(path)
		}
		// O_EXCL: si el agente creó ese mismo nombre en el medio, falla en vez
		// de pisarlo, y se prueba el número siguiente.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(content)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			// El archivo es nuestro y quedó a medias: un checkpoint truncado
			// sería peor que ninguno.
			_ = os.Remove(path)
			if werr != nil {
				return "", werr
			}
			return "", cerr
		}
		return name, nil
	}
	return "", i18n.New(i18n.Msg{ES: "no se encontró un nombre libre para el checkpoint", EN: "could not find a free name for the checkpoint"})
}

var headerLineRe = regexp.MustCompile(`^\s*//\s*@checkpoint\b`)

// withCheckpointHeader pone el encabezado en la primera línea: reemplaza el
// que haya o lo antepone. Respeta los finales de línea \r\n del código.
func withCheckpointHeader(source, parent, desc string) string {
	source = strings.TrimPrefix(source, "\uFEFF")
	nl := "\n"
	if strings.Contains(source, "\r\n") {
		nl = "\r\n"
	}
	header := checkpointHeader(parent, desc)
	first, rest, found := strings.Cut(source, "\n")
	if headerLineRe.MatchString(strings.TrimRight(first, "\r")) {
		if !found {
			return header + nl
		}
		return header + nl + rest
	}
	return header + nl + source
}

// checkpointHeader arma `// @checkpoint parent=X desc="…"`. La descripción va
// en una sola línea y con \ y " escapados: un " suelto cortaría desc antes de
// tiempo para workspace.Scan.
func checkpointHeader(parent, desc string) string {
	desc = strings.Join(strings.Fields(desc), " ")
	desc = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(desc)
	return fmt.Sprintf(`// @checkpoint parent=%s desc="%s"`, parent, desc)
}
