package clichat

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Limpieza del mecanismo anterior (hooks). Una versión previa de ScadLab aprobaba las
// herramientas de Antigravity con un hook en `<workspace>/.agents/hooks.json` (la
// entrada `scadlab-managed`, un `PreToolUse` que ejecutaba `ScadLab --agy-hook`). Se
// midió que no sirve —la confirmación de permisos de un MCP ocurre antes de los
// hooks— y se retiró; si un turno interrumpido dejó la entrada, aquí se quita.

// legacyAgyHookName es la clave de la entrada que escribía ScadLab.
const legacyAgyHookName = "scadlab-managed"

// RemoveLegacyAgyHook quita la entrada `scadlab-managed` del hooks.json del workspace
// sin tocar el resto de lo que hubiera ahí (las entradas del usuario, su orden). Si el
// archivo queda sin entradas lo borra, y también `.agents/` si quedó vacía. Un archivo
// que no es un objeto JSON no es de ScadLab: no se toca. Devuelve si quitó algo.
func RemoveLegacyAgyHook(workspaceDir string) (bool, error) {
	path := filepath.Join(workspaceDir, ".agents", "hooks.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	bom := bytes.HasPrefix(raw, []byte("\xef\xbb\xbf"))
	obj, err := parseObject(trimBOM(raw))
	if err != nil {
		return false, nil
	}
	found := false
	for i := obj.index(legacyAgyHookName); i >= 0; i = obj.index(legacyAgyHookName) {
		obj, found = obj.without(i), true
	}
	if !found {
		return false, nil
	}
	if len(obj) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		_ = os.Remove(filepath.Dir(path)) // solo si quedó vacía
		return true, nil
	}
	out, err := obj.marshal()
	if err != nil {
		return false, err
	}
	if bom {
		out = append([]byte("\xef\xbb\xbf"), out...)
	}
	return true, writeKeepingMode(path, out)
}
