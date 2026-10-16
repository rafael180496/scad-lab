package main

import (
	"path/filepath"

	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// SaveViewer guarda el estado del visor 3D (alambre, aristas, proyección, vista
// dividida, muestra y colores del modelo). Binding liviano y aparte de
// SaveSettings, como SaveLayout: el visor lo llama a cada cambio (con espera) y
// no debe reescribir la configuración entera con lo que la ventana tenga en
// memoria. Devuelve lo que quedó guardado (la muestra se acota a las seis).
func (a *App) SaveViewer(v store.ViewerState) (store.ViewerState, error) {
	if err := a.ready(); err != nil {
		return v, err
	}
	v = v.Normalize()
	return v, a.store.SetSetting("viewer", v)
}

// SetLastCheckpoint anota qué checkpoint tiene elegido un workspace, para volver
// a él al reabrirlo. Binding liviano (la interfaz lo llama con espera al elegir
// uno en el árbol). Valida el nombre como cualquier lectura de checkpoint; que el
// archivo exista no se comprueba: la interfaz ignora uno que ya no está.
func (a *App) SetLastCheckpoint(id int64, fileName string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if fileName != "" {
		if err := validCheckpointName(fileName); err != nil {
			return err
		}
	}
	return a.store.SetLastCheckpoint(id, fileName)
}

// SetWorkspacesRoot cambia la carpeta donde se crean los workspaces por defecto
// (Configuración → General). Binding liviano: el diálogo «Nuevo workspace» lo
// usa con su casilla «Usar esta carpeta como predeterminada» sin reescribir el
// resto de la configuración. "" vuelve a ~/Documents/ScadLab.
func (a *App) SetWorkspacesRoot(dir string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if dir != "" {
		if !filepath.IsAbs(dir) {
			return workspace.ErrParentNotAbs
		}
		dir = filepath.Clean(dir)
	}
	return a.store.SetSetting("workspacesRoot", dir)
}
