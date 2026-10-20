package main

import (
	"encoding/json"
	"log"

	"scadlab/backend/hermes"
	"scadlab/backend/mcp"
)

// Cambios del árbol hechos por la IA sin un .scad nuevo (S12.c, T13).
//
// set_active_parent, rename_checkpoint, trash_checkpoint y restore_checkpoint no
// crean un archivo: el padre activo y el rótulo no son un .scad, y el watcher solo
// ve aparecer o desaparecer archivos (tarde, tras su debounce, y nunca el rótulo).
// Con Hermes la herramienta corre en proceso y la app no se entera; con un CLI
// corre en el `ScadLab --mcp` de otro proceso. En los dos casos el paso llega aquí
// ya terminado y se hace lo mismo que hacen los bindings de la interfaz
// (emitView): reescanear el workspace y emitir checkpoints:changed, para que el
// árbol, la papelera, los rótulos y el padre activo se actualicen al instante.
//
// Qué herramientas son de este tipo lo dice mcp.ChangesTree, la misma definición
// que usan la red de seguridad y el espejo del frontend.

// refreshTreeAfterStep refresca la interfaz tras un paso que cambió el árbol o la
// configuración del workspace. No hace nada con un paso en curso, uno fallido (no
// cambió nada) ni otra herramienta.
//
// Una herramienta de configuración (mcp.EffectConfig, S14: set_workspace_printer)
// no toca el árbol: cambia la impresora, los colores o las skills del workspace en
// la base. El paso llega aquí ya terminado y se hace lo mismo que al guardar desde
// la interfaz: releer el workspace y emitir workspace:config, para que la barra
// de estado y el panel «Configuración del workspace» muestren la impresora nueva.
func (a *App) refreshTreeAfterStep(workspaceID int64, st hermes.Step) {
	if st.Status != hermes.StepOK {
		return
	}
	if mcp.ChangesConfig(st.Tool) {
		a.emitWorkspaceConfig(workspaceID)
		return
	}
	if mcp.UpdatesInPlace(st.Tool) {
		a.refreshAfterUpdate(workspaceID, st)
		return
	}
	if !mcp.ChangesTree(st.Tool) {
		return
	}
	w, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		log.Printf("scadlab: refrescar el árbol tras %s: %v", st.Tool, err)
		return
	}
	if st.Tool == "restore_checkpoint" {
		// Volver de la papelera no es trabajo nuevo: ni avanza el padre activo ni se
		// renderiza solo (lo mismo que RestoreCheckpoint). La marca se pone antes de
		// que el watcher (300 ms) avise de que el archivo reapareció.
		var res struct {
			File string `json:"file"`
		}
		if json.Unmarshal([]byte(st.Text), &res) == nil && res.File != "" {
			a.checkpoints().markRestored(w.ID, res.File)
		}
	}
	a.emitView(w)
}

// refreshAfterUpdate es el refresco tras update_checkpoint (S16): el archivo ya
// existía y cambió de contenido, así que no hay un .scad nuevo que el watcher
// vea aparecer. Se reescanea el workspace (la descripción de la cabecera pudo
// cambiar y «editado por la IA» se enciende) y se emite checkpoint:updated, para
// que la interfaz relea el código y vuelva a renderizar. Con un CLI la escritura
// la hizo el subproceso `--mcp`; el watcher la ve unos 300 ms después y avisa
// otra vez (la interfaz lo tolera: releer es idempotente), pero este aviso sale
// apenas termina el paso.
func (a *App) refreshAfterUpdate(workspaceID int64, st hermes.Step) {
	w, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		log.Printf("scadlab: refrescar el árbol tras %s: %v", st.Tool, err)
		return
	}
	file := updatedFileOf(st)
	a.emitView(w)
	if file != "" {
		a.emitCheckpointUpdated(workspaceID, file)
	}
}

// updatedFileOf es el archivo que tocó un paso update_checkpoint: lo que el
// modelo pidió (`file`) y, si falta, el `file` del resultado. Solo se devuelve un
// nombre válido: es lo que la interfaz usa para releer el archivo.
func updatedFileOf(st hermes.Step) string {
	file, _ := st.Args["file"].(string)
	if file == "" {
		var res struct {
			File string `json:"file"`
		}
		_ = json.Unmarshal([]byte(st.Text), &res)
		file = res.File
	}
	if validCheckpointName(file) != nil {
		return ""
	}
	return file
}
