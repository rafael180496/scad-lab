package mcp

import (
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
)

// fileRef es el archivo que nombró el argumento `file` de una herramienta de
// lectura o de edición: un checkpoint de la raíz del workspace o un archivo de
// lib/ (S17).
type fileRef struct {
	Key  string // "003-caja.scad" o "lib/tapa.scad" (barra normal en todos los sistemas)
	Path string // ruta absoluta
	Lib  bool
}

// libArg normaliza el argumento de un archivo de lib/: en Windows un modelo puede
// escribir `lib\tapa.scad`. Cualquier otra cosa se devuelve igual.
func libArg(file string) string {
	file = strings.TrimSpace(file)
	if strings.HasPrefix(file, `lib\`) {
		return "lib/" + strings.ReplaceAll(file[len(`lib\`):], `\`, "/")
	}
	return file
}

// targetFile resuelve `file` a un checkpoint de la raíz (vacío = el más nuevo,
// como en el resto de las herramientas) o a un archivo directo de lib/. Es la
// única puerta de las herramientas que aceptan los dos: la seguridad de los
// checkpoints es e.checkpointFile y la de lib/ es checkpoint.LibPath (sin «..»,
// rutas absolutas, unidades, subcarpetas ni enlaces que salgan).
func (e *env) targetFile(file string) (fileRef, error) {
	file = libArg(file)
	if checkpoint.IsLibFile(file) {
		path, err := checkpoint.LibPath(e.ws, file)
		if err != nil {
			return fileRef{}, err
		}
		return fileRef{Key: file, Path: path, Lib: true}, nil
	}
	if strings.ContainsAny(file, `/\`) {
		return fileRef{}, i18n.Errorf(i18n.Msg{
			ES: "%q no es un nombre válido: usa un checkpoint de la raíz (003-caja.scad, sin carpetas) o un archivo de lib/ (lib/tapa.scad)",
			EN: "%q is not a valid name: use a checkpoint from the workspace root (003-box.scad, no folders) or a lib/ file (lib/lid.scad)",
		}, clipRunes(file, 80))
	}
	name, path, err := e.checkpointFile(file)
	if err != nil {
		return fileRef{}, err
	}
	return fileRef{Key: name, Path: path}, nil
}

// explicitTarget es targetFile para las herramientas de escritura: `file` es
// obligatorio (vacío no significa «el más nuevo» cuando se va a reescribir).
func (e *env) explicitTarget(file, tool string) (fileRef, error) {
	if strings.TrimSpace(file) == "" {
		return fileRef{}, i18n.Errorf(i18n.Msg{
			ES: "falta `file`: el checkpoint (o lib/<nombre>.scad) que hay que editar con %s (ver list_checkpoints)",
			EN: "`file` is required: the checkpoint (or lib/<name>.scad) to edit with %s (see list_checkpoints)",
		}, tool)
	}
	return e.targetFile(file)
}

// readTarget lee el archivo entero.
func readTarget(ref fileRef) (string, error) {
	raw, err := os.ReadFile(ref.Path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// dependentCheckpoint devuelve el checkpoint más nuevo de la raíz que incluye (con
// include o use) el archivo de lib/. Sirve para verificar una edición de lib/:
// un módulo suelto no siempre renderiza solo, pero el modelo que lo usa sí.
func (e *env) dependentCheckpoint(libKey string) string {
	ents, err := os.ReadDir(e.ws)
	if err != nil {
		return ""
	}
	best, bestNum := "", -2
	for _, ent := range ents {
		name := ent.Name()
		if ent.IsDir() || checkpoint.ValidName(name) != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(e.ws, name))
		if err != nil || !includesLib(string(raw), libKey) {
			continue
		}
		if n := checkpointNumber(name); n > bestNum || (n == bestNum && name > best) {
			best, bestNum = name, n
		}
	}
	return best
}

// includesLib dice si src tiene un include o use de libKey.
func includesLib(src, libKey string) bool {
	for _, kw := range []string{"include", "use"} {
		for _, l := range strings.Split(src, "\n") {
			t := strings.TrimSpace(l)
			if !strings.HasPrefix(t, kw) {
				continue
			}
			rest := strings.TrimSpace(strings.TrimPrefix(t, kw))
			if strings.HasPrefix(rest, "<") {
				if end := strings.Index(rest, ">"); end > 0 {
					p := strings.TrimSpace(strings.ReplaceAll(rest[1:end], `\`, "/"))
					p = strings.TrimPrefix(p, "./")
					if p == libKey {
						return true
					}
				}
			}
		}
	}
	return false
}
