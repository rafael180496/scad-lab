package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"scadlab/assets"
	"scadlab/backend/i18n"
)

// Gestión de las skills propias: borrar (a la papelera), renombrar y cambiar de
// carpeta. Todo lo que toca los workspaces respeta una regla: solo se retira una
// copia que instaló ScadLab y que nadie editó a mano.

var (
	errSameName   = i18n.New(i18n.Msg{ES: "el nombre nuevo es igual al actual", EN: "the new name is the same as the current one"})
	errBuiltinDup = i18n.New(i18n.Msg{ES: "ya existe una skill incluida con ese nombre", EN: "a built-in skill with that name already exists"})
	errNotFound   = i18n.New(i18n.Msg{ES: "no se encontró la skill", EN: "the skill was not found"})
	errFolderRel  = i18n.New(i18n.Msg{ES: "la carpeta tiene que ser una ruta absoluta", EN: "the folder must be an absolute path"})
	errFolderRoot = i18n.New(i18n.Msg{ES: "elige una carpeta, no la raíz del disco", EN: "choose a folder, not the disk root"})
	errFolderSame = i18n.New(i18n.Msg{ES: "esa ya es la carpeta de skills propias", EN: "that is already the own-skills folder"})
	errFolderIn   = i18n.New(i18n.Msg{ES: "la carpeta nueva no puede estar dentro de la actual", EN: "the new folder cannot be inside the current one"})
	errFolderFile = i18n.New(i18n.Msg{ES: "esa ruta es un archivo, no una carpeta", EN: "that path is a file, not a folder"})
)

// treeHash es un hash del contenido de una carpeta: rutas y bytes de sus
// archivos, sin la marca de ScadLab ni basura del sistema.
func treeHash(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || ignoredFiles[d.Name()] {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// InstalledPlan es qué hacer con las copias de una skill en los workspaces.
type InstalledPlan struct {
	Remove []string // copias de ScadLab sin tocar: se pueden retirar
	Keep   []string // copias de ScadLab que alguien editó a mano: se dejan y se avisa
}

// PlanInstalled revisa, en cada workspace y cada carpeta de agente, la copia de
// la skill `name`. Solo cuenta si tiene la marca de ScadLab con origen «user»
// (lo que no instaló la app no es suyo). Una copia está editada si su contenido
// ya no es el que se instaló: se compara con el hash que guardó la marca y, en
// copias de una versión anterior (sin hash), con la skill original.
func PlanInstalled(workspaces []string, name, original string) InstalledPlan {
	var plan InstalledPlan
	var origHash string
	for _, ws := range workspaces {
		for _, rel := range AgentDirs {
			target := filepath.Join(ws, rel, name)
			if st, err := os.Lstat(target); err != nil || !st.IsDir() {
				continue
			}
			src, hash := readMarker(target)
			if src != "user" {
				continue
			}
			cur, err := treeHash(target)
			if err != nil {
				plan.Keep = append(plan.Keep, target)
				continue
			}
			if hash == "" {
				if origHash == "" {
					origHash, _ = treeHash(original)
				}
				hash = origHash
			}
			if hash != "" && cur == hash {
				plan.Remove = append(plan.Remove, target)
			} else {
				plan.Keep = append(plan.Keep, target)
			}
		}
	}
	return plan
}

// Apply retira las copias del plan. Devuelve cuántas quitó.
func (p InstalledPlan) Apply() (int, error) {
	var errs []error
	n := 0
	for _, dir := range p.Remove {
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// DeleteResult es lo que pasó al borrar una skill propia.
type DeleteResult struct {
	Trash   string   `json:"trash"`   // dónde quedó la carpeta de la skill
	Removed int      `json:"removed"` // copias retiradas de los workspaces
	Kept    []string `json:"kept"`    // copias editadas a mano que se dejaron
}

// userSkillDir devuelve la carpeta de una skill propia que existe.
func userSkillDir(userDir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return "", errNotFound
	}
	dir := filepath.Join(userDir, name)
	if st, err := os.Lstat(dir); err != nil || !(st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return "", errNotFound
	}
	return dir, nil
}

// DeleteUser manda una skill propia a la papelera de la app (<datos>/trash/skills)
// y retira sus copias sin editar de los workspaces. La carpeta se mueve primero:
// si eso falla no se toca ningún workspace.
func DeleteUser(userDir, dataDir, name string, workspaces []string) (DeleteResult, error) {
	dir, err := userSkillDir(userDir, name)
	if err != nil {
		return DeleteResult{}, err
	}
	plan := PlanInstalled(workspaces, name, dir)
	trash, err := MoveToTrash(dataDir, "skills", name, dir)
	if err != nil {
		return DeleteResult{}, err
	}
	n, aerr := plan.Apply()
	return DeleteResult{Trash: trash, Removed: n, Kept: plan.Keep}, aerr
}

// BuiltinExists dice si hay una skill incluida (o la plantilla) con ese nombre.
func BuiltinExists(name string) bool {
	if name == TemplateName {
		return true
	}
	_, err := fs.Stat(assets.FS, "skills/"+name)
	return err == nil
}

// RenameResult es lo que pasó al renombrar una skill propia.
type RenameResult struct {
	Removed int      `json:"removed"` // copias con el nombre viejo retiradas de los workspaces
	Kept    []string `json:"kept"`    // copias editadas a mano que se dejaron
}

// RenameUser renombra una skill propia: la carpeta, el `name:` de su SKILL.md y
// las copias instaladas con el nombre viejo. El nombre nuevo no puede chocar con
// otra skill propia ni con una incluida (esa se reemplazaría sin querer).
func RenameUser(userDir, oldName, newName string, workspaces []string) (RenameResult, error) {
	if err := CheckName(newName); err != nil {
		return RenameResult{}, err
	}
	if oldName == newName {
		return RenameResult{}, errSameName
	}
	oldDir, err := userSkillDir(userDir, oldName)
	if err != nil {
		return RenameResult{}, err
	}
	if BuiltinExists(newName) {
		return RenameResult{}, errBuiltinDup
	}
	newDir := filepath.Join(userDir, newName)
	if _, err := os.Lstat(newDir); err == nil {
		return RenameResult{}, errExists
	}
	plan := PlanInstalled(workspaces, oldName, oldDir)
	if err := os.Rename(oldDir, newDir); err != nil {
		return RenameResult{}, err
	}
	main := filepath.Join(newDir, MainFile)
	if raw, err := os.ReadFile(main); err == nil {
		if err := WriteFile(newDir, MainFile, string(setFrontmatterName(raw, newName))); err != nil {
			_ = os.Rename(newDir, oldDir) // que no quede a medias
			return RenameResult{}, err
		}
	}
	n, aerr := plan.Apply()
	return RenameResult{Removed: n, Kept: plan.Keep}, aerr
}

// CheckFolder valida la carpeta nueva de skills propias y la devuelve limpia:
// absoluta, no la raíz del disco, distinta de la actual, no dentro de ella (al
// mover las skills se meterían dentro de sí mismas) y no un archivo.
func CheckFolder(oldDir, newDir string) (string, error) {
	newDir = strings.TrimSpace(newDir)
	if !filepath.IsAbs(newDir) {
		return "", errFolderRel
	}
	newDir = filepath.Clean(newDir)
	if filepath.Dir(newDir) == newDir {
		return "", errFolderRoot
	}
	oldDir = filepath.Clean(oldDir)
	if newDir == oldDir {
		return "", errFolderSame
	}
	if within(oldDir, newDir) {
		return "", errFolderIn
	}
	if st, err := os.Stat(newDir); err == nil && !st.IsDir() {
		return "", errFolderFile
	}
	return newDir, nil
}

// MoveResult es lo que pasó al mover las skills propias a otra carpeta.
type MoveResult struct {
	Moved   []string `json:"moved"`
	Skipped []string `json:"skipped"` // ya había una con ese nombre en la carpeta nueva
}

// MoveAll mueve las skills propias de oldDir a newDir (se crea si falta). No
// pisa nada: una skill que ya existe en el destino se deja donde estaba y se
// informa. Solo se mueven las carpetas que son una skill (tienen SKILL.md).
func MoveAll(oldDir, newDir string) (MoveResult, error) {
	res := MoveResult{Moved: []string{}, Skipped: []string{}}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return res, err
	}
	ents, err := os.ReadDir(oldDir)
	if errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	var errs []error
	for _, e := range ents {
		if !e.IsDir() || e.Name() == TemplateName {
			continue
		}
		src := filepath.Join(oldDir, e.Name())
		if _, err := os.Stat(filepath.Join(src, MainFile)); err != nil {
			continue
		}
		dst := filepath.Join(newDir, e.Name())
		if _, err := os.Lstat(dst); err == nil {
			res.Skipped = append(res.Skipped, e.Name())
			continue
		}
		if err := MovePath(src, dst); err != nil {
			errs = append(errs, i18n.Errorf(i18n.Msg{ES: "no se pudo mover «%s»: %w", EN: "could not move «%s»: %w"}, e.Name(), err))
			continue
		}
		res.Moved = append(res.Moved, e.Name())
	}
	return res, errors.Join(errs...)
}
