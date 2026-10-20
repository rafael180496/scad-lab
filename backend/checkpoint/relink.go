package checkpoint

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"scadlab/backend/i18n"
	"scadlab/backend/workspace"
)

// relinkSuffix es el registro de re-colgado de un elemento de la papelera:
// <papelera>/<AAAAMMDD-HHMMSS>-<archivo>.scad.relink.json. No termina en .scad,
// así que ListTrash y workspace.Scan no lo ven; vive junto al archivo para que
// se vaya con él (Restore lo consume, la eliminación permanente lo borra).
const relinkSuffix = ".relink.json"

// maxRelinkBytes acota la lectura del registro: es una lista de nombres.
const maxRelinkBytes = 1 << 20

// relinkRecord dice qué hijos de un checkpoint borrado se re-colgaron, y de
// dónde: lo que Restore necesita para deshacerlo sin adivinar.
type relinkRecord struct {
	// Parent es de quien pasaron a colgar los hijos ("" = quedaron como raíz).
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
}

// validParentName dice si name sirve como valor de `parent=`: un nombre de
// checkpoint sin espacios (el valor termina en el primero).
func validParentName(name string) bool {
	return ValidName(name) == nil && !strings.ContainsFunc(name, unicode.IsSpace)
}

// relinkChildren cuelga los hijos directos de file —que acaba de ir a la
// papelera con el nombre trashedName— de su padre, o los deja como raíces si no
// tenía padre (o este ya no está en el workspace: colgarlos de un ausente sería
// volver al problema). parents es el mapa archivo → padre de ANTES de mover.
// Devuelve los que sí se re-colgaron.
func relinkChildren(wsDir, trashDir, file, trashedName string, parents map[string]string) ([]string, error) {
	var kids []string
	for name, p := range parents {
		if p == file && name != file {
			kids = append(kids, name)
		}
	}
	if len(kids) == 0 {
		return nil, nil
	}
	sortNames(kids)
	newParent := parents[file]
	if newParent == file || !validParentName(newParent) || !exists(wsDir, newParent) {
		newParent = ""
	}

	var done []string
	var failed []string
	var firstErr error
	for _, k := range kids {
		if err := SetParent(wsDir, k, newParent); err != nil {
			failed = append(failed, k)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done = append(done, k)
	}
	if len(done) > 0 {
		if err := writeRelink(trashDir, trashedName, relinkRecord{Parent: newParent, Children: done}); err != nil {
			// Sin registro, Restore no podrá devolverlos: el árbol queda bien, solo
			// se pierde ese «deshacer». Se avisa en vez de callarlo.
			failed = append(failed, trashedName)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if len(failed) > 0 {
		return done, i18n.Errorf(i18n.Msg{
			ES: "%s está en la papelera, pero no se pudo reenganchar %s: %w",
			EN: "%s is in the trash, but %s could not be re-hung: %w",
		}, file, strings.Join(failed, ", "), firstErr)
	}
	return done, nil
}

func writeRelink(trashDir, trashedName string, rec relinkRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return writeFileAtomic(trashDir, trashedName+relinkSuffix, data)
}

// readRelink lee el registro de trashedName (zero, false si no hay o no se
// entiende: un registro ilegible equivale a no tener registro).
func readRelink(trashDir, trashedName string) (relinkRecord, bool) {
	path := filepath.Join(trashDir, trashedName+relinkSuffix)
	if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
		return relinkRecord{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return relinkRecord{}, false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxRelinkBytes))
	if err != nil {
		return relinkRecord{}, false
	}
	var rec relinkRecord
	if json.Unmarshal(raw, &rec) != nil {
		return relinkRecord{}, false
	}
	if rec.Parent != "" && !validParentName(rec.Parent) {
		return relinkRecord{}, false
	}
	return rec, true
}

// relinkOnRestore deshace el re-colgado de trashedName ya restaurado como
// restored: cada hijo que siga existiendo y siga colgando de donde se lo dejó
// vuelve a colgar de restored. Consume el registro (con éxito o sin él: un
// registro viejo no debe sobrevivir a la restauración).
func relinkOnRestore(wsDir, trashedName, restored string) ([]string, error) {
	dir, err := metaDir(wsDir, false, trashDirName)
	if err != nil {
		return nil, nil // sin papelera no hay registro
	}
	rec, ok := readRelink(dir, trashedName)
	defer func() { _ = os.Remove(filepath.Join(dir, trashedName+relinkSuffix)) }()
	if !ok || len(rec.Children) == 0 {
		return nil, nil
	}
	files, err := workspace.Scan(wsDir)
	if err != nil {
		return nil, err
	}
	parents := make(map[string]string, len(files))
	for _, f := range files {
		parents[f.FileName] = f.ParentFile
	}
	var done []string
	var firstErr error
	for _, k := range rec.Children {
		cur, there := parents[k]
		if !there || cur != rec.Parent || ValidName(k) != nil || k == restored {
			continue
		}
		if hangsFrom(parents, restored, k) { // colgarlo crearía un ciclo
			continue
		}
		if err := SetParent(wsDir, k, restored); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		parents[k] = restored
		done = append(done, k)
	}
	sortNames(done)
	if firstErr != nil {
		return done, i18n.Errorf(i18n.Msg{
			ES: "%s volvió, pero no se pudieron re-colgar sus hijos: %w",
			EN: "%s is back, but its children could not be re-hung: %w",
		}, restored, firstErr)
	}
	return done, nil
}

// hangsFrom dice si target está en la cadena de ancestros de start (o es start).
func hangsFrom(parents map[string]string, start, target string) bool {
	seen := map[string]bool{}
	for cur := start; cur != "" && !seen[cur]; cur = parents[cur] {
		if cur == target {
			return true
		}
		seen[cur] = true
	}
	return false
}

// removeRelink borra el registro de un elemento (si lo hay). Un fallo se ignora:
// un registro que quedó suelto lo barre «Vaciar papelera».
func removeRelink(trashDir, trashedName string) {
	_ = os.Remove(filepath.Join(trashDir, trashedName+relinkSuffix))
}
