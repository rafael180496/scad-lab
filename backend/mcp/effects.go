package mcp

import "sort"

// Effect es qué le hace al proyecto una herramienta de ESCRITURA del catálogo.
// Es la única definición: la red de seguridad del chat (backend/hermes y
// app_chat_autosave.go) y el refresco del árbol (app_chat_tree.go) preguntan aquí
// en vez de repetir listas de nombres, y el frontend (CREATING_TOOLS de
// lib/chatSteps.ts) es su espejo, cruzado por una prueba. Una herramienta que
// falte en una de esas listas deja de crear su enlace, de refrescar el árbol o,
// peor, hace que la red de seguridad guarde un bloque de más.
type Effect int

const (
	// EffectNone: no cambia el proyecto (las de lectura; render, measure,
	// preview, estimate_print y suggest_orientation solo escriben caché).
	EffectNone Effect = iota
	// EffectCheckpoint: deja un `.scad` NUEVO en el workspace y su resultado trae
	// `file` con su nombre. Con eso el chat enlaza y selecciona el checkpoint, y la
	// red de seguridad sabe que el modelo ya entregó algo.
	EffectCheckpoint
	// EffectTree: cambia el árbol SIN un `.scad` nuevo (padre activo, rótulo,
	// papelera, restaurar). La app no se entera sola: tiene que reescanear y avisar.
	EffectTree
	// EffectFiles: escribe fuera del árbol (exports/, lib/): no hay nada que
	// refrescar ni que enlazar.
	EffectFiles
	// EffectConfig (S14): cambia la configuración del workspace —impresora,
	// colores, skills activas— sin tocar el árbol. La interfaz tiene que releer el
	// workspace y mostrar la impresora nueva (evento `workspace:config`).
	EffectConfig
)

// effects clasifica cada herramienta de escritura. Las de lectura no figuran.
// `TestEffectsCoverTheWriteTools` exige que toda herramienta de «Agente» tenga
// una entrada y que ninguna de «Solo consulta» la tenga.
var effects = map[string]Effect{
	"create_checkpoint":    EffectCheckpoint,
	"import_mesh":          EffectCheckpoint,
	"set_parameters":       EffectCheckpoint,
	"create_from_template": EffectCheckpoint,
	"edit_mesh":            EffectCheckpoint,

	"set_active_parent":  EffectTree,
	"rename_checkpoint":  EffectTree,
	"trash_checkpoint":   EffectTree,
	"restore_checkpoint": EffectTree,

	"export":          EffectFiles,
	"install_library": EffectFiles,
	// Escriben el SVG trazado o el mapa de alturas en refs/ (S15).
	"trace_image_outline": EffectFiles,
	"image_to_heightmap":  EffectFiles,

	// Cambia la impresora, los colores y las skills del workspace (wsconfig.Apply).
	"set_workspace_printer": EffectConfig,
}

// EffectOf dice qué le hace al proyecto la herramienta (EffectNone si no es de
// escritura o no existe).
func EffectOf(tool string) Effect { return effects[tool] }

// CreatesCheckpoint dice si la herramienta, al terminar bien, deja un checkpoint
// nuevo (su resultado trae `file`).
func CreatesCheckpoint(tool string) bool { return effects[tool] == EffectCheckpoint }

// ChangesTree dice si la herramienta cambia el árbol sin crear un `.scad`: tras
// un paso bueno hay que reescanear el workspace y avisar a la interfaz.
func ChangesTree(tool string) bool { return effects[tool] == EffectTree }

// ChangesConfig dice si la herramienta cambia la configuración del workspace
// (impresora, colores, skills): tras un paso bueno hay que releerla y avisar a la
// interfaz. Hoy es `set_workspace_printer`.
func ChangesConfig(tool string) bool { return effects[tool] == EffectConfig }

// ToolsWithEffect devuelve, ordenadas, las herramientas con ese efecto (para las
// pruebas y para el espejo del frontend).
func ToolsWithEffect(e Effect) []string {
	var out []string
	for name, eff := range effects {
		if eff == e {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
