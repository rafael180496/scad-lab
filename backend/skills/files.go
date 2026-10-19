package skills

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"scadlab/assets"
	"scadlab/backend/i18n"
)

// Edición de los archivos de una skill. Toda ruta que llega de la interfaz es
// relativa a la carpeta de la skill y se confina acá: nada de `..`, rutas
// absolutas ni enlaces simbólicos que se salgan de ella.

// MaxFileBytes es el tope de un archivo editable (un SKILL.md ronda los 20 KB).
const MaxFileBytes = 1 << 20

// MaxFiles es el tope de archivos de una skill que se listan y se crean.
const MaxFiles = 300

// MainFile es el archivo que hace de skill: no se borra ni se renombra.
const MainFile = "SKILL.md"

// File es un archivo de una skill.
type File struct {
	Path string `json:"path"` // relativa a la carpeta de la skill, con «/»
	Size int64  `json:"size"`
	Text bool   `json:"text"` // se puede abrir en el editor (UTF-8, de tamaño razonable)
}

var (
	errBadPath = i18n.New(i18n.Msg{
		ES: "ruta de archivo inválida: usa una ruta relativa dentro de la skill, con «/», sin «..»",
		EN: "invalid file path: use a relative path inside the skill, with «/», without «..»",
	})
	errOutside   = i18n.New(i18n.Msg{ES: "esa ruta sale de la carpeta de la skill", EN: "that path leaves the skill folder"})
	errReadOnly  = i18n.New(i18n.Msg{ES: "las skills incluidas son de solo lectura", EN: "built-in skills are read-only"})
	errNotText   = i18n.New(i18n.Msg{ES: "el archivo no es de texto o es demasiado grande para el editor", EN: "the file is not text or is too large for the editor"})
	errMainFile  = i18n.New(i18n.Msg{ES: "SKILL.md es la skill: no se puede borrar ni renombrar", EN: "SKILL.md is the skill itself: it cannot be deleted or renamed"})
	errFileExist = i18n.New(i18n.Msg{ES: "ya existe un archivo con ese nombre", EN: "a file with that name already exists"})
	errTooMany   = i18n.New(i18n.Msg{ES: "la skill ya tiene demasiados archivos", EN: "the skill already has too many files"})
)

// ReadOnlyError es el error de intentar escribir en una skill incluida.
func ReadOnlyError() error { return errReadOnly }

const badPathChars = `<>:"|?*`

// CleanRel valida una ruta relativa de la interfaz y la devuelve limpia y con
// «/». Rechaza vacíos, rutas absolutas o con unidad (C:), «\», «..» y nombres
// que Windows no admite.
func CleanRel(rel string) (string, error) {
	if rel == "" || len(rel) > 240 || strings.ContainsAny(rel, "\x00\\") || strings.HasPrefix(rel, "/") {
		return "", errBadPath
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", errBadPath
	}
	segs := strings.Split(rel, "/")
	if len(segs) > 8 {
		return "", errBadPath
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." || len(s) > 120 || strings.ContainsAny(s, badPathChars) || s == managedMarker {
			return "", errBadPath
		}
		if strings.HasSuffix(s, " ") || strings.HasSuffix(s, ".") {
			return "", errBadPath
		}
		for _, r := range s {
			if r < 0x20 {
				return "", errBadPath
			}
		}
	}
	return strings.Join(segs, "/"), nil
}

// within dice si p está dentro de root (o es root).
func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return r == "." || (r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r))
}

// confine devuelve la ruta absoluta de rel dentro de dir, comprobando además
// que ningún enlace simbólico del camino saque el archivo de la carpeta.
func confine(dir, rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(dir, filepath.FromSlash(clean))
	if !within(dir, abs) {
		return "", errOutside
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	// El ancestro más profundo que ya existe: si el archivo todavía no existe,
	// lo que importa es dónde caería.
	p := abs
	for {
		if _, err := os.Lstat(p); err == nil {
			break
		}
		next := filepath.Dir(p)
		if next == p {
			return "", errOutside
		}
		p = next
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if !within(realDir, real) {
		return "", errOutside
	}
	return abs, nil
}

// isText dice si los primeros bytes parecen texto UTF-8.
func isText(head []byte, size int64) bool {
	if size > MaxFileBytes {
		return false
	}
	for _, b := range head {
		if b == 0 {
			return false
		}
	}
	// Un carácter multibyte cortado al final del bloque no cuenta como error.
	for i := 0; i < utf8.UTFMax && len(head) > 0 && !utf8.Valid(head); i++ {
		head = head[:len(head)-1]
	}
	return utf8.Valid(head)
}

var ignoredFiles = map[string]bool{".DS_Store": true, "Thumbs.db": true, "desktop.ini": true, managedMarker: true}

// ListFiles lista los archivos de una skill (incluida o propia): SKILL.md
// primero, el resto por ruta.
func ListFiles(s Skill) ([]File, error) {
	var out []File
	add := func(rel string, size int64, head []byte) error {
		if len(out) >= MaxFiles {
			return errTooMany
		}
		out = append(out, File{Path: rel, Size: size, Text: isText(head, size)})
		return nil
	}
	if s.Source == "builtin" {
		err := fs.WalkDir(assets.FS, s.Dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || ignoredFiles[d.Name()] {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			raw, err := fs.ReadFile(assets.FS, p)
			if err != nil {
				return err
			}
			return add(strings.TrimPrefix(strings.TrimPrefix(p, s.Dir), "/"), info.Size(), raw[:min(len(raw), 4096)])
		})
		if err != nil {
			return nil, err
		}
	} else {
		err := filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || ignoredFiles[d.Name()] || !d.Type().IsRegular() {
				return nil // los enlaces simbólicos no se listan: no se siguen
			}
			rel, err := filepath.Rel(s.Dir, p)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			head := make([]byte, 4096)
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			n, _ := f.Read(head)
			_ = f.Close()
			return add(filepath.ToSlash(rel), info.Size(), head[:n])
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Path, out[j].Path
		if (a == MainFile) != (b == MainFile) {
			return a == MainFile
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	if out == nil {
		out = []File{}
	}
	return out, nil
}

// ReadFile lee un archivo de texto de una skill (incluida o propia).
func ReadFile(s Skill, rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	var raw []byte
	if s.Source == "builtin" {
		raw, err = fs.ReadFile(assets.FS, path.Join(s.Dir, clean))
	} else {
		var abs string
		if abs, err = confine(s.Dir, clean); err != nil {
			return "", err
		}
		var st os.FileInfo
		if st, err = os.Stat(abs); err != nil {
			return "", err
		}
		if !st.Mode().IsRegular() || st.Size() > MaxFileBytes {
			return "", errNotText
		}
		raw, err = os.ReadFile(abs)
	}
	if err != nil {
		return "", err
	}
	if !isText(raw[:min(len(raw), 4096)], int64(len(raw))) || !utf8.Valid(raw) {
		return "", errNotText
	}
	return string(raw), nil
}

// WriteFile guarda el contenido de un archivo que ya existe, de forma atómica
// (archivo temporal en la misma carpeta y renombrado): un corte a mitad no deja
// el SKILL.md truncado.
func WriteFile(dir, rel, content string) error {
	abs, err := confine(dir, rel)
	if err != nil {
		return err
	}
	if len(content) > MaxFileBytes || !utf8.ValidString(content) {
		return errNotText
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errBadPath
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".scadlab-save-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, abs); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// CreateFile crea un archivo vacío (con sus carpetas) dentro de la skill.
func CreateFile(dir, rel string) error {
	abs, err := confine(dir, rel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(abs); err == nil {
		return errFileExist
	}
	if countFiles(dir) >= MaxFiles {
		return errTooMany
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return errFileExist
		}
		return err
	}
	return f.Close()
}

func isMain(rel string) bool { return strings.EqualFold(rel, MainFile) }

// RenameFile renombra o mueve un archivo dentro de la skill. SKILL.md ni se
// renombra ni se pisa.
func RenameFile(dir, from, to string) error {
	src, err := confine(dir, from)
	if err != nil {
		return err
	}
	dst, err := confine(dir, to)
	if err != nil {
		return err
	}
	cf, _ := CleanRel(from)
	ct, _ := CleanRel(to)
	if isMain(cf) || isMain(ct) {
		return errMainFile
	}
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errBadPath
	}
	if dstSt, derr := os.Lstat(dst); derr == nil && !os.SameFile(st, dstSt) {
		return errFileExist
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// DeleteFile manda un archivo de la skill a la papelera de la app (nunca lo
// borra) y devuelve dónde quedó. SKILL.md no se borra.
func DeleteFile(dir, dataDir, skill, rel string) (string, error) {
	abs, err := confine(dir, rel)
	if err != nil {
		return "", err
	}
	clean, _ := CleanRel(rel)
	if isMain(clean) {
		return "", errMainFile
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errBadPath
	}
	return MoveToTrash(dataDir, "skill-files", skill+"-"+strings.ReplaceAll(clean, "/", "__"), abs)
}
