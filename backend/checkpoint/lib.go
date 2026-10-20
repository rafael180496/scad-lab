package checkpoint

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"scadlab/backend/i18n"
)

// Archivos de lib/ (S17). Un modelo grande se parte en módulos: `extract_module`
// mueve módulos y funciones a lib/<nombre>.scad y `edit_checkpoint` los edita.
// Solo cuentan los hijos DIRECTOS de lib/: las subcarpetas (lib/BOSL2/…) son
// librerías instaladas y son de solo lectura para la IA.
//
// Un archivo de lib/ no es un checkpoint (no tiene cabecera ni entra al árbol),
// pero se protege igual: se reescribe solo con respaldo en
// `.scadlab/history/lib/<nombre>.scad/`, con la misma garantía respaldo →
// escritura atómica de Update. La clave de un archivo de lib/ es siempre
// "lib/<nombre>.scad", con barra normal en todos los sistemas.

// LibDir es la carpeta de las librerías del workspace.
const LibDir = "lib"

// IsLibFile dice si la clave nombra un archivo de lib/ ("lib/x.scad"). No lo
// valida: eso es ValidLibName.
func IsLibFile(file string) bool { return strings.HasPrefix(file, LibDir+"/") }

// ValidLibName valida la clave "lib/<nombre>.scad": solo un hijo directo de lib/,
// un nombre suelto que termina en .scad. Rechaza «..», rutas absolutas, unidades
// de Windows, subcarpetas (lib/BOSL2/std.scad) y caracteres de control.
func ValidLibName(file string) error {
	if !IsLibFile(file) {
		return errBadLib()
	}
	name := strings.TrimPrefix(file, LibDir+"/")
	// «\» y «:» no separan nada en macOS pero sí en Windows (subcarpetas, unidades,
	// flujos alternativos): un nombre que los trae es hostil en un sistema.
	if strings.ContainsAny(name, `\:`) || ValidName(name) != nil {
		return errBadLib()
	}
	return nil
}

func errBadLib() error {
	return i18n.New(i18n.Msg{
		ES: "nombre de librería inválido: usa lib/<nombre>.scad (solo archivos directos de lib/; las subcarpetas son librerías instaladas y son de solo lectura)",
		EN: "invalid library name: use lib/<name>.scad (direct files of lib/ only; subfolders are installed libraries and are read-only)",
	})
}

// validTarget valida la clave de un archivo que tiene historial: un checkpoint
// de la raíz o un archivo de lib/.
func validTarget(file string) error {
	if IsLibFile(file) {
		return ValidLibName(file)
	}
	return ValidName(file)
}

// libDirPath devuelve <ws>/lib comprobando que sea una carpeta normal (o un enlace
// que se queda dentro del workspace). Si no existe devuelve fs.ErrNotExist.
func libDirPath(ws string) (string, error) {
	dir := filepath.Join(ws, LibDir)
	st, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		real, rerr := filepath.EvalSymlinks(dir)
		wsReal, werr := filepath.EvalSymlinks(ws)
		if rerr != nil || werr != nil || !inside(wsReal, real) {
			return "", i18n.New(i18n.Msg{
				ES: "lib apunta fuera del workspace: no la uso por seguridad",
				EN: "lib points outside the workspace: not using it for safety",
			})
		}
		return dir, nil
	}
	if !st.IsDir() {
		return "", i18n.New(i18n.Msg{ES: "lib no es una carpeta", EN: "lib is not a folder"})
	}
	return dir, nil
}

// inside dice si p está dentro de root (o es root).
func inside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// LibPath devuelve la ruta absoluta de un archivo de lib/ que existe y es
// legible: un archivo normal, o un enlace cuyo destino sigue dentro del
// workspace.
func LibPath(ws, file string) (string, error) {
	if err := ValidLibName(file); err != nil {
		return "", err
	}
	dir, err := libDirPath(ws)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", notFoundLib(ws, file)
		}
		return "", err
	}
	path := filepath.Join(dir, strings.TrimPrefix(file, LibDir+"/"))
	st, err := os.Lstat(path)
	if err != nil || st.IsDir() {
		return "", notFoundLib(ws, file)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		real, rerr := filepath.EvalSymlinks(path)
		wsReal, werr := filepath.EvalSymlinks(ws)
		if rerr != nil || werr != nil || !inside(wsReal, real) {
			return "", i18n.Errorf(i18n.Msg{ES: "%s apunta fuera del workspace", EN: "%s points outside the workspace"}, file)
		}
	}
	return path, nil
}

// notFoundLib arma el error de «no existe» con los archivos de lib/ que sí hay.
func notFoundLib(ws, file string) error {
	have, _ := ListLibFiles(ws)
	if len(have) == 0 {
		return i18n.Errorf(i18n.Msg{ES: "no se encontró %s (lib/ no tiene archivos .scad propios)", EN: "%s was not found (lib/ has no .scad files of its own)"}, file)
	}
	return i18n.Errorf(i18n.Msg{ES: "no se encontró %s; en lib/ hay: %s", EN: "%s was not found; lib/ has: %s"}, file, strings.Join(have, ", "))
}

// ListLibFiles lista los .scad directos de lib/ ("lib/x.scad"), ordenados. Sin
// carpeta lib/ devuelve nil, nil.
func ListLibFiles(ws string) ([]string, error) {
	dir, err := libDirPath(ws)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".scad") || ValidName(e.Name()) != nil {
			continue
		}
		out = append(out, LibDir+"/"+e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// CreateLib crea lib/<nombre>.scad con content. Nunca pisa: si ya existe falla
// con un error que envuelve fs.ErrExist. La escritura es atómica (temporal y
// enlace duro que no reemplaza): un lector ve el archivo entero o ninguno.
func CreateLib(ws, file, content string) error {
	if err := ValidLibName(file); err != nil {
		return err
	}
	dir, err := libDirPath(ws)
	if errors.Is(err, fs.ErrNotExist) {
		if merr := os.Mkdir(filepath.Join(ws, LibDir), 0o755); merr != nil && !errors.Is(merr, fs.ErrExist) {
			return merr
		}
		dir, err = libDirPath(ws)
	}
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, strings.TrimPrefix(file, LibDir+"/"))
	if fileExists(dst) {
		return &fs.PathError{Op: "create", Path: dst, Err: fs.ErrExist}
	}
	tmp, err := os.CreateTemp(dir, "."+strings.TrimPrefix(file, LibDir+"/")+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // el temporal es nuestro; el destino ya tiene su propio nombre
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return moveNoReplace(tmpName, dst)
}

// RemoveCreatedLib deshace un CreateLib de esta misma operación: borra el archivo
// SOLO si su contenido es exactamente el que se escribió. Existe para el
// «deshacer» de extract_module cuando el resultado no es equivalente; nunca
// borra algo que alguien cambió después ni un archivo que ya existía antes
// (CreateLib no lo habría creado).
func RemoveCreatedLib(ws, file, content string) error {
	path, err := LibPath(ws, file)
	if err != nil {
		return err
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(cur, []byte(content)) {
		return i18n.Errorf(i18n.Msg{
			ES: "%s cambió desde que se creó: no lo borro",
			EN: "%s changed since it was created: not removing it",
		}, file)
	}
	return os.Remove(path)
}

// LibExists dice si lib/<nombre>.scad existe (y se puede usar). Un nombre inválido
// o un lib/ que sale del workspace son errores; un archivo que simplemente no
// está es false, nil.
func LibExists(ws, file string) (bool, error) {
	if err := ValidLibName(file); err != nil {
		return false, err
	}
	dir, err := libDirPath(ws)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	st, err := os.Lstat(filepath.Join(dir, strings.TrimPrefix(file, LibDir+"/")))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if st.IsDir() {
		return false, i18n.Errorf(i18n.Msg{ES: "%s es una carpeta", EN: "%s is a folder"}, file)
	}
	return true, nil
}
