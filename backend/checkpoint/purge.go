package checkpoint

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"scadlab/backend/i18n"
)

// Eliminación permanente de la papelera.
//
// Es la única forma en que ScadLab borra de verdad un archivo del usuario, y es
// una excepción pedida por él (regla 3 de technical.md): solo la dispara una
// acción explícita en la papelera de la app, con confirmación. Por eso no hay
// herramienta MCP equivalente —un agente puede mandar a la papelera y restaurar,
// nunca vaciarla— ni se llama desde ningún otro lado.
//
// Las dos funciones solo tocan dentro de <ws>/.scadlab/trash: el nombre se
// valida como nombre suelto, la carpeta no puede ser un enlace (metaDir) y los
// enlaces que haya adentro se quitan a ellos, nunca a lo que apuntan.

// starterDeclinedName marca que el usuario vació la papelera a propósito.
const starterDeclinedName = "no-starter"

// StarterDeclined dice si el usuario vació la papelera de este workspace. La
// regla de S12.c escribe el modelo de inicio al abrir un workspace sin .scad
// **y con la papelera vacía**; vaciarla a mano deja justo ese estado, y sin esta
// marca el modelo reaparecería en un workspace que el usuario dejó vacío adrede.
// Con la marca, la interfaz sigue ofreciendo «Crear un modelo de inicio».
func StarterDeclined(wsDir string) bool {
	dir, err := metaDir(wsDir, false)
	if err != nil {
		return false
	}
	st, err := os.Lstat(filepath.Join(dir, starterDeclinedName))
	return err == nil && st.Mode().IsRegular()
}

func markStarterDeclined(wsDir string) {
	dir, err := metaDir(wsDir, true)
	if err != nil {
		return
	}
	// Mejor esfuerzo: si no se puede escribir, lo peor es que el modelo de inicio
	// reaparezca, y el usuario puede volver a borrarlo.
	_ = writeFileAtomic(dir, starterDeclinedName, []byte("1\n"))
}

// PurgeTrashed elimina para siempre un elemento de la papelera (por el nombre
// que lista ListTrash) y su registro de re-colgado. No se puede deshacer.
func PurgeTrashed(wsDir, trashedName string) error {
	if err := ValidName(trashedName); err != nil {
		return err
	}
	if m := trashedRe.FindStringSubmatch(trashedName); m == nil || ValidName(m[2]) != nil {
		return i18n.Errorf(i18n.Msg{
			ES: "%s no tiene el formato de un archivo de la papelera",
			EN: "%s is not in the trash file format",
		}, trashedName)
	}
	dir, err := metaDir(wsDir, false, trashDirName)
	if errors.Is(err, fs.ErrNotExist) {
		return notInTrash(trashedName)
	}
	if err != nil {
		return err
	}
	path := filepath.Join(dir, trashedName)
	st, err := os.Lstat(path)
	if err != nil || st.IsDir() {
		return notInTrash(trashedName)
	}
	// Remove sobre un enlace simbólico quita el enlace, no su destino.
	if err := os.Remove(path); err != nil {
		return err
	}
	removeRelink(dir, trashedName)
	if TrashEmpty(wsDir) {
		markStarterDeclined(wsDir)
	}
	return nil
}

func notInTrash(name string) error {
	return i18n.Errorf(i18n.Msg{ES: "no se encontró %s en la papelera", EN: "%s was not found in the trash"}, name)
}

// EmptyTrash elimina para siempre TODO lo que hay en <ws>/.scadlab/trash:
// checkpoints, sus registros de re-colgado y cualquier otra cosa que alguien
// haya dejado ahí. La carpeta queda (vacía). Devuelve cuántos checkpoints se
// eliminaron (lo que lista ListTrash, no los registros ni lo ajeno). Si algo
// falla sigue con el resto y devuelve el primer error junto con la cuenta.
func EmptyTrash(wsDir string) (int, error) {
	dir, err := metaDir(wsDir, false, trashDirName)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	items, _ := ListTrash(wsDir)
	listed := make(map[string]bool, len(items))
	for _, it := range items {
		listed[it.Name] = true
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	removed := 0
	var firstErr error
	for _, e := range ents {
		path := filepath.Join(dir, e.Name())
		var rerr error
		if e.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 || !e.IsDir() {
			rerr = os.Remove(path) // archivo o enlace: el enlace, no su destino
		} else {
			rerr = os.RemoveAll(path)
		}
		if rerr != nil {
			if firstErr == nil {
				firstErr = rerr
			}
			continue
		}
		if listed[e.Name()] {
			removed++
		}
	}
	if TrashEmpty(wsDir) {
		markStarterDeclined(wsDir)
	}
	return removed, firstErr
}
