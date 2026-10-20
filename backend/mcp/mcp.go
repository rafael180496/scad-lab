// Package mcp es el servidor MCP de ScadLab (S6): `ScadLab --mcp --workspace
// <dir>` expone a los agentes (Claude Code, opencode, Codex, Antigravity)
// herramientas para renderizar, validar, medir y VER sus modelos en vez de
// depender de la auto-iteración o de invocar OpenSCAD a mano.
//
// Es un proceso autónomo: no hay puente a la ventana. Lee la configuración de
// scadlab.db (ruta de OpenSCAD, motor, impresora del workspace, materiales), usa
// el mismo motor de render y la misma caché de mallas, y trabaja sobre los
// archivos del workspace. Funciona con la app cerrada (un agente lanzado desde
// una terminal externa).
//
// Spec: .claude/specs/s6-servidor-mcp.md. Notas: claude/13-mcp.md.
package mcp

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
)

// ServerName es el nombre con el que se registra el servidor en la
// configuración de cada agente (`mcpServers.scadlab`). Las herramientas no
// llevan prefijo: el cliente ya las muestra como `scadlab/<herramienta>`.
const ServerName = "scadlab"

// WorkspaceEnv es la variable de entorno que da la carpeta del workspace cuando el
// servidor se lanza sin `--workspace`: el registro GLOBAL de Antigravity
// (`agy mcp add -- scadlab <ScadLab> --mcp`) no puede llevar una carpeta, y de dónde
// hereda el servidor su directorio actual depende de Antigravity. El chat de ScadLab
// la pone en el entorno de cada turno de `agy`; la prioridad es `--workspace`, luego
// esta variable y, por último, el directorio actual.
const WorkspaceEnv = "SCADLAB_WORKSPACE"

// Args son los argumentos con los que se lanza el servidor, tras el ejecutable
// de ScadLab. workspaceDir tiene que ser absoluto. Lo comparten el modo `--mcp`
// de main.go y quien escribe la configuración de los agentes (backend/mcpconf):
// una sola definición de la línea de comandos.
func Args(workspaceDir string) []string {
	return []string{"--mcp", "--workspace", workspaceDir}
}

// Options configura el servidor.
type Options struct {
	// WorkspaceDir es la carpeta del proyecto. Vacío = el directorio actual
	// (un agente lanzado dentro del workspace). Se resuelve a ruta absoluta y se
	// siguen los enlaces simbólicos: ahí se compara cada ruta que se toca.
	WorkspaceDir string
	// Version es la de ScadLab, para serverInfo. Vacío = "dev".
	Version string
	// Log recibe los mensajes de diagnóstico. Vacío = stderr. **Nunca stdout**:
	// ahí solo viaja JSON-RPC.
	Log io.Writer
}

// Serve atiende a un cliente MCP: lee peticiones JSON-RPC (una por línea) de in
// y escribe las respuestas en out hasta que in se cierra o ctx se cancela. out
// solo recibe JSON-RPC.
func Serve(ctx context.Context, in io.Reader, out io.Writer, opts Options) error {
	logw := opts.Log
	if logw == nil {
		logw = os.Stderr
	}
	lg := log.New(logw, "scadlab-mcp: ", log.LstdFlags)

	e, err := newEnv(opts, lg)
	if err != nil {
		return err
	}
	defer e.close()

	version := opts.Version
	if version == "" {
		version = "dev"
	}
	s := newServer(ServerName, version, serverInstructions, e.guardTurnMode(e.tools()), lg)
	return s.serve(ctx, in, out)
}

// serverInstructions las muestran algunos clientes (Claude Code) junto al
// servidor: el flujo recomendado en pocas líneas.
const serverInstructions = "ScadLab OpenSCAD tools for the current workspace. " +
	"Mandatory flow for any design request: printer_info / list_checkpoints -> create_checkpoint (a NEW NNN-slug.scad) or update_checkpoint (edit an existing file in place) -> render (errors with line numbers) -> if it fails, read the line and fix it with update_checkpoint on that same file " +
	"-> preview (look at the part) -> measure (size, volume, weight, fits the plate?) -> short summary naming the file. " +
	"When the user names an existing file, or refers to the open/selected one (\"this\", \"the current one\"), and asks for changes, edit it with update_checkpoint (the previous version is saved in its history first, so nothing is lost and the user can undo). " +
	"Create a new checkpoint only if they ask for something new, a variant or \"another version\". When in doubt, edit the one they named. " +
	"To read a .scad use read_checkpoint with the file name; never the CLI's own file tools or paths; never write .scad files with the CLI's file or shell tools (they leave no backup). " +
	"If the workspace has no checkpoints, create the first one right away without asking. A design turn never ends without a checkpoint that renders, and never with only code in the text. " +
	"When unsure, pick reasonable dimensions and say so. Omit parent to continue from the active parent shown by list_checkpoints. Every top-level part needs color(). Use lint/format while writing code. " +
	"Shortcuts: a request that resembles a template -> create_from_template; changing a measurement -> get_parameters + set_parameters (a new child, a variant) or update_checkpoint when the user wants it in their file; " +
	"a model in refs/ (STL/3MF/OBJ) -> inspect_reference, import_mesh, then edit_mesh (move, rotate, scale, cut, holes, recolor); too big for the plate -> suggest_orientation; " +
	"an image in refs/images/ (a photo, logo or sketch to model from) -> read_skill openscad-from-image, image_info, build it with primitives, then compare_with_image from the same view and fix; a flat logo/plaque -> trace_image_outline; a relief or lithophane -> image_to_heightmap; " +
	"threads or rounded edges -> list_libraries / install_library; export stl/3mf/png only when asked. Discarding = trash_checkpoint (restorable); you cannot open Bambu Studio."

// resolveWorkspace deja WorkspaceDir como ruta absoluta sin enlaces simbólicos.
func resolveWorkspace(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return real, nil
}
