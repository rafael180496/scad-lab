package checkpoint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"scadlab/backend/i18n"
	"scadlab/backend/workspace"
)

// trashDirName es la papelera, dentro de .scadlab/. Los checkpoints son
// inmutables y nunca se borran (regla 3 de technical.md): «borrar» es mover
// aquí, con la fecha delante para que dos borrados del mismo nombre convivan y
// para saber cuándo pasó.
const trashDirName = "trash"

// trashStamp es el prefijo de fecha de un archivo en la papelera
// (AAAAMMDD-HHMMSS, hora local: es lo que el usuario ve).
const trashStamp = "20060102-150405"

// trashedRe separa `<AAAAMMDD-HHMMSS>-<archivo original>`.
var trashedRe = regexp.MustCompile(`^(\d{8}-\d{6})-(.+)$`)

// now se reemplaza en los tests para fijar la hora del borrado.
var now = time.Now

// maxTrashBumps es cuántos segundos se corre la marca si ya hay un archivo con
// ese nombre en la papelera (borrar, restaurar y volver a borrar en el mismo
// segundo).
const maxTrashBumps = 100

// TrashedItem es un checkpoint de la papelera. Parent y Description salen de su
// encabezado `@checkpoint` (que Restore deja intacto).
type TrashedItem struct {
	Name        string `json:"name"`     // nombre dentro de la papelera: <AAAAMMDD-HHMMSS>-<archivo>
	Original    string `json:"original"` // nombre que tenía en el workspace
	Parent      string `json:"parent"`
	Description string `json:"description"`
	TrashedAt   int64  `json:"trashedAt"` // segundos desde la época
}

// moveNoReplace mueve src a dst y falla con fs.ErrExist —sin tocar nada— si dst
// ya existe. os.Rename pisa el destino en macOS y Linux, y aquí un pisotón sería
// perder un checkpoint (de la papelera o del workspace, según el sentido).
//
// Para un archivo normal usa un enlace duro y luego quita el nombre viejo:
// os.Link es atómico y nunca reemplaza, así que ni un agente que crea el mismo
// nombre en el medio puede perder su archivo. Quitar el nombre de origen no
// borra contenido —sigue vivo bajo el nombre nuevo—. Donde no hay enlaces duros
// (FAT, algunos recursos de red) o el origen es un enlace simbólico (se mueve el
// enlace, no su destino), cae a «comprobar y renombrar».
func moveNoReplace(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if st.Mode().IsRegular() {
		lerr := os.Link(src, dst)
		if lerr == nil {
			if rerr := os.Remove(src); rerr != nil {
				_ = os.Remove(dst) // el contenido sigue en src; se deshace el segundo nombre
				return rerr
			}
			return nil
		}
		if errors.Is(lerr, fs.ErrExist) {
			return &fs.PathError{Op: "move", Path: dst, Err: fs.ErrExist}
		}
	}
	if _, err := os.Lstat(dst); err == nil {
		return &fs.PathError{Op: "move", Path: dst, Err: fs.ErrExist}
	}
	return os.Rename(src, dst)
}

// lookupParents devuelve archivo → padre de todos los checkpoints del workspace.
func lookupParents(wsDir string) (map[string]string, error) {
	files, err := workspace.Scan(wsDir)
	if err != nil {
		return nil, err
	}
	parents := make(map[string]string, len(files))
	for _, f := range files {
		parents[f.FileName] = f.ParentFile
	}
	return parents, nil
}

// descendantsBFS recorre el árbol por niveles: hijos, nietos… (sin repetir y sin
// girar en círculos si dos encabezados se apuntan entre sí). El orden es el de
// profundidad creciente, que TrashBranch invierte para mover las hojas primero.
func descendantsBFS(parents map[string]string, file string) []string {
	children := map[string][]string{}
	for name, p := range parents {
		if p != "" {
			children[p] = append(children[p], name)
		}
	}
	for _, kids := range children {
		sortNames(kids)
	}
	seen := map[string]bool{file: true}
	var out []string
	queue := []string{file}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, k := range children[cur] {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
				queue = append(queue, k)
			}
		}
	}
	return out
}

// sortNames ordena por número de checkpoint y, a igualdad, por nombre.
func sortNames(names []string) {
	num := func(s string) int {
		if m := numRe.FindStringSubmatch(s); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return -1
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := num(names[i]), num(names[j])
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})
}

// Descendants devuelve todos los checkpoints que cuelgan de file (hijos, nietos…),
// sin incluirlo, ordenados por número. Mira los encabezados `parent=` del
// workspace. file no tiene que existir: los hijos de un checkpoint que ya está en
// la papelera siguen apuntándole y se pueden listar igual.
func Descendants(wsDir, file string) ([]string, error) {
	if err := ValidName(file); err != nil {
		return nil, err
	}
	parents, err := lookupParents(wsDir)
	if err != nil {
		return nil, err
	}
	out := descendantsBFS(parents, file)
	if out == nil {
		out = []string{}
	}
	sortNames(out)
	return out, nil
}

// trashDir devuelve (creándola) <ws>/.scadlab/trash.
func trashDir(wsDir string) (string, error) { return metaDir(wsDir, true, trashDirName) }

// trashOne mueve un checkpoint a la papelera con la marca t y devuelve el nombre
// que le quedó allí. Si ese nombre ya existe corre la marca de a un segundo.
func trashOne(wsDir, dir, file string, t time.Time) (string, error) {
	src := filepath.Join(wsDir, file)
	for i := 0; i < maxTrashBumps; i++ {
		name := t.Add(time.Duration(i)*time.Second).Format(trashStamp) + "-" + file
		err := moveNoReplace(src, filepath.Join(dir, name))
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return name, nil
	}
	return "", i18n.New(i18n.Msg{ES: "no se encontró un nombre libre en la papelera", EN: "could not find a free name in the trash"})
}

// checkTrashable valida el nombre y que sea un archivo del workspace.
func checkTrashable(wsDir, file string) error {
	if err := ValidName(file); err != nil {
		return err
	}
	if !exists(wsDir, file) {
		return i18n.Errorf(i18n.Msg{ES: "no se encontró el checkpoint %s", EN: "checkpoint %s was not found"}, file)
	}
	return nil
}

// Trash manda un checkpoint a la papelera (.scadlab/trash/<AAAAMMDD-HHMMSS>-<archivo>)
// y devuelve su nombre allí. Nunca borra: se puede traer de vuelta con Restore.
//
// Los hijos NO se mueven: siguen con `parent=` apuntando al archivo, que queda
// huérfano en el árbol hasta que se restaure (Restore lo devuelve a su nombre
// si está libre, y el vínculo se recompone solo). Para llevarse la rama entera
// está TrashBranch.
//
// Si era el padre activo, el activo pasa a SU padre (si existe): quien estaba
// trabajando en esa rama sigue en ella, un paso atrás, en vez de caer a «el más
// nuevo», que puede ser de otra rama. Sin padre, se limpia.
func Trash(wsDir, file string) (string, error) {
	names, err := trashFiles(wsDir, file, false)
	if len(names) == 0 {
		return "", err
	}
	return names[0], err
}

// TrashBranch manda a la papelera el checkpoint y todos sus descendientes con la
// misma marca de fecha (así se ven juntos y se restauran juntos). Devuelve los
// nombres en la papelera, el de file primero. Mueve primero las hojas y al final
// file: si algo falla a medio camino, lo que queda sigue siendo un árbol
// conectado y no hay huérfanos nuevos. En ese caso devuelve lo que sí se movió
// junto con el error.
//
// El padre activo se trata como en Trash: si estaba dentro de la rama, pasa al
// padre de file.
func TrashBranch(wsDir, file string) ([]string, error) {
	return trashFiles(wsDir, file, true)
}

func trashFiles(wsDir, file string, branch bool) ([]string, error) {
	if err := checkTrashable(wsDir, file); err != nil {
		return nil, err
	}
	parents, err := lookupParents(wsDir)
	if err != nil {
		return nil, err
	}
	// order: hojas primero. set: file y luego sus descendientes por número, que
	// es como se devuelven.
	order := []string{file}
	set := []string{file}
	if branch {
		desc := descendantsBFS(parents, file)
		order = append(order, desc...)
		sortNames(desc)
		set = append(set, desc...)
	}
	dir, err := trashDir(wsDir)
	if err != nil {
		return nil, err
	}

	activeMu.Lock()
	defer activeMu.Unlock()
	active := ActiveParent(wsDir)

	t := now()
	moved := map[string]string{} // original → nombre en la papelera
	var firstErr error
	for i := len(order) - 1; i >= 0; i-- { // la más profunda primero, file al final
		name, err := trashOne(wsDir, dir, order[i], t)
		if err != nil {
			firstErr = err
			break
		}
		moved[order[i]] = name
	}

	// El activo se mueve solo si de verdad se llevó la papelera su archivo. Si
	// este paso falla no se informa: un activo colgado ya se lee como «ninguno»
	// (ActiveParent comprueba que exista), así que no hay nada que reparar.
	if _, gone := moved[active]; active != "" && gone {
		next := parents[file]
		if _, alsoGone := moved[next]; next == "" || alsoGone || ValidName(next) != nil || !exists(wsDir, next) {
			next = ""
		}
		_ = setActive(wsDir, next)
	}

	out := make([]string, 0, len(moved))
	for _, f := range set { // file primero, luego por número
		if name, ok := moved[f]; ok {
			out = append(out, name)
		}
	}
	return out, firstErr
}

// ListTrash lista la papelera, lo más reciente primero. Sin papelera, lista vacía.
// Ignora lo que no tiene el formato `<AAAAMMDD-HHMMSS>-<archivo>.scad` (algo
// que alguien dejó a mano): Restore no sabría a qué nombre devolverlo.
func ListTrash(wsDir string) ([]TrashedItem, error) {
	dir, err := metaDir(wsDir, false, trashDirName)
	if errors.Is(err, fs.ErrNotExist) {
		return []TrashedItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	files, err := workspace.Scan(dir)
	if err != nil {
		return nil, err
	}
	out := make([]TrashedItem, 0, len(files))
	for _, f := range files {
		m := trashedRe.FindStringSubmatch(f.FileName)
		if m == nil || ValidName(m[2]) != nil {
			continue
		}
		at, err := time.ParseInLocation(trashStamp, m[1], time.Local)
		if err != nil {
			continue
		}
		out = append(out, TrashedItem{
			Name: f.FileName, Original: m[2], Parent: f.ParentFile,
			Description: f.Description, TrashedAt: at.Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TrashedAt != out[j].TrashedAt {
			return out[i].TrashedAt > out[j].TrashedAt
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Restore saca un archivo de la papelera y lo devuelve al workspace. Vuelve a su
// nombre original si está libre; si no, toma el siguiente NNN con el mismo slug
// (003-base.scad ocupado → 007-base.scad). El archivo se mueve entero, sin
// reescribir: el encabezado `@checkpoint` queda como estaba. Por eso, si cambió
// de número, los hijos que decían `parent=003-base.scad` no se reenganchan (son
// inmutables, igual que él): la rama restaurada queda como una raíz más.
// Nunca pisa un checkpoint existente.
func Restore(wsDir, trashedName string) (string, error) {
	if err := ValidName(trashedName); err != nil {
		return "", err
	}
	m := trashedRe.FindStringSubmatch(trashedName)
	if m == nil || ValidName(m[2]) != nil {
		return "", i18n.Errorf(i18n.Msg{
			ES: "%s no tiene el formato de un archivo de la papelera",
			EN: "%s is not in the trash file format",
		}, trashedName)
	}
	original := m[2]
	dir, err := metaDir(wsDir, false, trashDirName)
	if errors.Is(err, fs.ErrNotExist) {
		return "", i18n.Errorf(i18n.Msg{ES: "no se encontró %s en la papelera", EN: "%s was not found in the trash"}, trashedName)
	}
	if err != nil {
		return "", err
	}
	src := filepath.Join(dir, trashedName)
	if st, err := os.Lstat(src); err != nil || st.IsDir() {
		return "", i18n.Errorf(i18n.Msg{ES: "no se encontró %s en la papelera", EN: "%s was not found in the trash"}, trashedName)
	}

	// 1) Su nombre de siempre.
	err = moveNoReplace(src, filepath.Join(wsDir, original))
	if err == nil {
		return original, nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return "", err
	}

	// 2) Ocupado: el siguiente número libre con el mismo slug. Si otro proceso
	// toma el número en el medio, moveNoReplace falla con ErrExist y se prueba el
	// siguiente (igual que create).
	rest := original
	if nm := numRe.FindStringSubmatch(original); nm != nil {
		rest = nm[2]
	}
	next := 1
	if files, err := workspace.Scan(wsDir); err == nil {
		for _, f := range files {
			if nm := numRe.FindStringSubmatch(f.FileName); nm != nil {
				if n, _ := strconv.Atoi(nm[1]); n >= next {
					next = n + 1
				}
			}
		}
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		name := fmt.Sprintf("%03d-%s", next+attempt, rest)
		err := moveNoReplace(src, filepath.Join(wsDir, name))
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return name, nil
	}
	return "", i18n.New(i18n.Msg{ES: "no se encontró un nombre libre para el checkpoint", EN: "could not find a free name for the checkpoint"})
}
