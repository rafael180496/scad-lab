package main

import (
	"scadlab/backend/checkpoint"
)

// Historial de un checkpoint (S16). Los checkpoints se reescriben en el lugar con
// respaldo previo —la IA con update_checkpoint, el usuario con «Restaurar versión» o
// «Deshacer cambio de la IA»— y cada respaldo queda en .scadlab/history/<archivo>/.
// Las operaciones de archivo viven en backend/checkpoint (que comparte con el
// servidor MCP); estos bindings resuelven el workspace, validan el nombre y el id de
// versión (checkpoint.ValidName / ValidVersionID: nada con rutas) y avisan a la
// interfaz. Ver claude/34-historial-checkpoints.md.

// CheckpointHistory es el historial de un archivo tal como lo ve la interfaz.
type CheckpointHistory struct {
	File string `json:"file"`
	// Versions son los respaldos, el más nuevo primero. Nunca null.
	Versions []checkpoint.Version `json:"versions"`
	// EditedByAI: la versión más nueva la guardó la IA, o sea que el último cambio
	// del archivo es suyo y «Deshacer cambio de la IA» lo revierte.
	EditedByAI bool `json:"editedByAI"`
	// Max es cuántas versiones se conservan por archivo (Configuración).
	Max int `json:"max"`
}

// historyMax es el tope de versiones por archivo vigente: lo que dice la
// configuración, o el predeterminado si no se pudo leer.
func (a *App) historyMax() int {
	if s, err := a.store.GetSettings(); err == nil && s.HistoryMaxVersions > 0 {
		return s.HistoryMaxVersions
	}
	return checkpoint.DefaultHistoryMax
}

// ListCheckpointHistory lista las versiones guardadas de un checkpoint, la más nueva
// primero. Un archivo sin historial devuelve la lista vacía (no es un error).
func (a *App) ListCheckpointHistory(workspaceID int64, file string) (CheckpointHistory, error) {
	w, err := a.checkpointWorkspace(workspaceID, file)
	if err != nil {
		return CheckpointHistory{}, err
	}
	vs, err := checkpoint.History(w.Path, file)
	if err != nil {
		return CheckpointHistory{}, err
	}
	if vs == nil {
		vs = []checkpoint.Version{}
	}
	return CheckpointHistory{
		File:       file,
		Versions:   vs,
		EditedByAI: len(vs) > 0 && vs[0].Reason == checkpoint.ReasonAI,
		Max:        a.historyMax(),
	}, nil
}

// ReadCheckpointVersion devuelve el código de una versión guardada (para compararla).
func (a *App) ReadCheckpointVersion(workspaceID int64, file, versionID string) (string, error) {
	w, err := a.checkpointWorkspace(workspaceID, file)
	if err != nil {
		return "", err
	}
	return checkpoint.ReadVersion(w.Path, file, versionID)
}

// RestoreCheckpointVersion vuelve el archivo a una versión del historial. Antes
// guarda lo que había (motivo «restore»), así que restaurar también se puede
// deshacer y el indicador «editado por la IA» se apaga solo. Devuelve la versión que
// guardó. Emite checkpoint:updated (la interfaz relee y vuelve a renderizar) y
// checkpoints:changed (el árbol: descripción e indicador).
func (a *App) RestoreCheckpointVersion(workspaceID int64, file, versionID string) (checkpoint.Version, error) {
	w, err := a.checkpointWorkspace(workspaceID, file)
	if err != nil {
		return checkpoint.Version{}, err
	}
	saved, err := checkpoint.RestoreVersion(w.Path, file, versionID, a.historyMax())
	if err != nil {
		return checkpoint.Version{}, err
	}
	a.emitView(w)
	a.emitCheckpointUpdated(w.ID, file)
	return saved, nil
}
