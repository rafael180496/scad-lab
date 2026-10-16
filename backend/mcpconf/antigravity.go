package mcpconf

import (
	"regexp"
	"strings"
)

// Antigravity (`agy`) no lee ningún archivo de proyecto: su única configuración
// de servidores MCP es global (`~/.gemini/config/mcp_config.json` en agy 1.3.6),
// y se gestiona con `agy mcp add|remove|list`. No hay flags al arrancar para
// pasarle un servidor. Por eso ScadLab solo la toca cuando el usuario lo pide.
//
// La entrada global es `scadlab` → `<ScadLab> --mcp`, **sin** `--workspace`: el
// servidor usa el directorio actual como workspace (mcp.Options.WorkspaceDir
// vacío) y agy lo lanza en la carpeta del proyecto, así una sola entrada sirve a
// todos los workspaces. Si agy no lo lanzara con esa carpeta como cwd, la
// herramienta lo dice al usarla (la carpeta no sería un workspace).

// Estados de la entrada de ScadLab en la configuración global de Antigravity.
const (
	AgyNotInstalled = "not-installed" // no hay `agy` en la máquina
	AgyMissing      = "missing"       // agy no tiene la entrada scadlab
	AgyOK           = "ok"            // la tiene, activa y con este ejecutable
	AgyOther        = "other"         // la tiene, pero con otro comando (app movida u otra cosa)
	AgyDisabled     = "disabled"      // la tiene como este ejecutable, pero desactivada en agy
	AgyError        = "error"         // no se pudo preguntar a agy
)

// AgyEntry es una fila de `agy mcp list`.
type AgyEntry struct {
	Name    string
	Type    string // stdio | http
	Enabled bool
	Command string // ejecutable y argumentos, o URL, tal como los imprime agy
}

// AgyListArgs, AgyAddArgs y AgyRemoveArgs son el argv (sin el ejecutable) de cada
// operación. Se ejecutan sin shell. `--` va antes del nombre para que el
// ejecutable o sus argumentos (`--mcp`) no se lean como flags de agy; los flags
// de agy tendrían que ir antes del nombre, y aquí no se usa ninguno.
func AgyListArgs() []string { return []string{"mcp", "list"} }

// AgyAddArgs agrega (o reemplaza: `add` es un upsert) la entrada scadlab.
func AgyAddArgs(binPath string) []string {
	return append([]string{"mcp", "add", "--", ServerName, binPath}, AgyServerArgs()...)
}

// AgyRemoveArgs quita la entrada scadlab.
func AgyRemoveArgs() []string { return []string{"mcp", "remove", ServerName} }

// AgyServerArgs son los argumentos del servidor en la entrada global: sin
// --workspace (ver arriba).
func AgyServerArgs() []string { return []string{"--mcp"} }

// AgyCommand es la línea que agy imprime para la entrada vigente.
func AgyCommand(binPath string) string {
	return strings.Join(append([]string{binPath}, AgyServerArgs()...), " ")
}

// agyRow separa NAME, TYPE y STATUS (una palabra cada uno) del resto de la fila.
// El resto (COMMAND/URL) lleva espacios: la ruta de la app puede tenerlos.
var agyRow = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(enabled|disabled)\s+(.*?)\s*$`)

// ParseAgyList lee la salida de `agy mcp list`:
//
//	NAME     TYPE   STATUS   COMMAND/URL
//	scadlab  stdio  enabled  /Apps/ScadLab --mcp
//
// o «No MCP servers configured.». Lo que no tiene la forma de una fila (el
// encabezado, ese aviso, cualquier otra línea) se ignora: un formato nuevo se
// vería como «sin entrada», no como un error.
func ParseAgyList(out string) []AgyEntry {
	var rows []AgyEntry
	for _, line := range strings.Split(out, "\n") {
		m := agyRow.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		rows = append(rows, AgyEntry{Name: m[1], Type: m[2], Enabled: m[3] == "enabled", Command: m[4]})
	}
	return rows
}

// AgyStateOf dice en qué estado está la entrada scadlab entre las filas de agy
// para el ejecutable de ScadLab binPath. Devuelve también la fila encontrada
// (nil si no hay).
func AgyStateOf(rows []AgyEntry, binPath string) (string, *AgyEntry) {
	for i := range rows {
		if rows[i].Name != ServerName {
			continue
		}
		e := &rows[i]
		switch {
		case e.Type != "stdio" || e.Command != AgyCommand(binPath):
			return AgyOther, e
		case !e.Enabled:
			return AgyDisabled, e
		}
		return AgyOK, e
	}
	return AgyMissing, nil
}
