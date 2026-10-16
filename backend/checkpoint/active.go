package checkpoint

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"scadlab/backend/i18n"
)

// metaDirName es la carpeta de datos propios de ScadLab dentro del workspace
// (padre activo, papelera, temporales del linter). Va en el .gitignore.
const metaDirName = ".scadlab"

// activeFileName guarda, en una línea, el nombre del checkpoint «activo».
const activeFileName = "active-parent"

// maxActiveBytes acota la lectura de active-parent: es un nombre de archivo, un
// archivo de megas no es nuestro.
const maxActiveBytes = 1024

// activeMu serializa los cambios del padre activo dentro del proceso (la app
// los hace desde el watcher, los bindings y el chat a la vez). Entre procesos
// —la app y el servidor MCP— no hay candado: no hace falta, porque Advance es
// idempotente (con el mismo padre y el mismo hijo da el mismo resultado) y el
// archivo se reemplaza entero con rename.
var activeMu sync.Mutex

// metaDir devuelve <ws>/.scadlab. Con create=true lo crea si falta. Si existe y
// es un enlace (simbólico o punto de unión de Windows) o no es una carpeta,
// falla: seguirlo podría llevar la papelera o el padre activo fuera del
// workspace, y borrar —aunque sea «mover»— fuera de él no es nuestro derecho.
func metaDir(wsDir string, create bool, sub ...string) (string, error) {
	dir := wsDir
	for _, part := range append([]string{metaDirName}, sub...) {
		dir = filepath.Join(dir, part)
		st, err := os.Lstat(dir)
		switch {
		case err == nil:
			if !st.IsDir() || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return "", i18n.Errorf(i18n.Msg{
					ES: "%s no es una carpeta normal (¿es un enlace?): no la uso por seguridad",
					EN: "%s is not a plain folder (is it a link?): not using it for safety",
				}, dir)
			}
		case errors.Is(err, fs.ErrNotExist):
			if !create {
				return "", err
			}
			if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return "", err
			}
			// Otro proceso pudo crear un enlace con ese nombre entre el Lstat y
			// el Mkdir: se vuelve a mirar.
			if st, err := os.Lstat(dir); err != nil || !st.IsDir() || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return "", i18n.Errorf(i18n.Msg{
					ES: "%s no es una carpeta normal (¿es un enlace?): no la uso por seguridad",
					EN: "%s is not a plain folder (is it a link?): not using it for safety",
				}, dir)
			}
		default:
			return "", err
		}
	}
	return dir, nil
}

// writeFileAtomic escribe data en dir/name con un temporal en la misma carpeta
// y un rename: un lector ve el contenido anterior o el nuevo, nunca uno a medias.
func writeFileAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "."+name+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName) // el temporal es nuestro, no un checkpoint
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// exists dice si dir/file es un archivo (o un enlace a archivo) de la raíz del
// workspace. Las carpetas no cuentan: un checkpoint es un archivo.
func exists(wsDir, file string) bool {
	st, err := os.Lstat(filepath.Join(wsDir, file))
	return err == nil && !st.IsDir()
}

// ActiveParent devuelve el checkpoint «activo» del workspace: el padre que
// recibirá el próximo checkpoint. Es "" cuando no hay (y entonces rige el
// comportamiento de siempre: continuar la rama del más nuevo), cuando el archivo
// está mal formado o cuando apunta a un checkpoint que ya no existe —por
// ejemplo, uno que el usuario movió a mano—. Un puntero colgado se trata como
// ausente en vez de fallar: es una preferencia, nunca debe impedir crear.
func ActiveParent(wsDir string) string {
	dir, err := metaDir(wsDir, false)
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, activeFileName)
	// Lstat: si active-parent es un enlace, no lo seguimos (podría leer
	// cualquier archivo del disco como «nombre de checkpoint»).
	if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxActiveBytes))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(strings.TrimPrefix(string(raw), "\uFEFF"))
	if ValidName(name) != nil || !exists(wsDir, name) {
		return ""
	}
	return name
}

// SetActiveParent fija el padre activo. "" lo limpia (vuelve a regir «la rama
// del más nuevo»). El checkpoint tiene que existir en la raíz del workspace.
func SetActiveParent(wsDir, file string) error {
	activeMu.Lock()
	defer activeMu.Unlock()
	return setActive(wsDir, file)
}

// setActive es SetActiveParent sin el candado; lo usan quienes ya lo tienen.
func setActive(wsDir, file string) error {
	if file == "" {
		dir, err := metaDir(wsDir, false)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // no hay nada que limpiar
		}
		if err != nil {
			return err
		}
		// active-parent es metadato nuestro, no un .scad: limpiarlo es borrarlo.
		if err := os.Remove(filepath.Join(dir, activeFileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := ValidName(file); err != nil {
		return err
	}
	if !exists(wsDir, file) {
		return i18n.Errorf(i18n.Msg{ES: "el checkpoint %s no existe", EN: "the checkpoint %s does not exist"}, file)
	}
	dir, err := metaDir(wsDir, true)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, activeFileName, []byte(file+"\n"))
}

// Advance hace que el padre activo «avance solo»: si el activo es parent, el
// checkpoint nuevo newFile (hijo de parent) pasa a ser el activo. Si el activo
// es otro, o no hay, no hace nada: crear una variante lateral no debe mover la
// rama en la que el usuario está trabajando.
//
// Lo llama Create, pero también el watcher de la app cuando un agente escribe el
// archivo directo. Por eso es idempotente: llamarlo dos veces con los mismos
// argumentos da lo mismo que una.
func Advance(wsDir, parent, newFile string) error {
	if parent == "" || parent == newFile {
		return nil
	}
	if err := ValidName(newFile); err != nil {
		return err
	}
	activeMu.Lock()
	defer activeMu.Unlock()
	if ActiveParent(wsDir) != parent {
		return nil
	}
	return setActive(wsDir, newFile)
}
