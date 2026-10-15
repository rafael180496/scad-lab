package main

import (
	"scadlab/backend/i18n"
	"scadlab/backend/store"
)

// SaveDockLayout guarda la disposición actual de los paneles acoplables (el
// JSON que arma el frontend). Es un binding liviano y aparte de SaveSettings,
// por la misma razón que SaveLayout y SaveAppearance: se llama al mover,
// dividir o ocultar un panel (con espera de unos cientos de ms) y no debe
// reescribir la configuración entera con lo que la ventana tenga en memoria.
// Un JSON vacío borra el diseño guardado (vuelve al predeterminado); uno que
// no es un objeto JSON, o pasa del tope de tamaño, se rechaza.
func (a *App) SaveDockLayout(layout string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if layout != "" && !store.ValidDockLayoutJSON(layout) {
		return i18n.Errorf(i18n.Msg{
			ES: "el diseño de paneles no es válido (debe ser un objeto JSON de hasta 256 KB)",
			EN: "the panel layout is not valid (it must be a JSON object of up to 256 KB)",
		})
	}
	return a.store.SetSetting("dockLayout", layout)
}

// SaveDockLayouts guarda la lista de diseños propios con nombre («Guardar
// diseño como…», aplicar y borrar). Recibe la lista completa y devuelve lo que
// quedó guardado: nombres recortados, sin repetidos (el último gana) y con JSON
// válido. Rechaza una lista con un diseño inválido o con más de 30.
func (a *App) SaveDockLayouts(list []store.DockLayoutEntry) ([]store.DockLayoutEntry, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	out, err := store.NormalizeDockLayouts(list)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{
			ES: "los diseños guardados no son válidos (nombre vacío, JSON roto o más de 30 diseños)",
			EN: "the saved layouts are not valid (empty name, broken JSON or more than 30 layouts)",
		})
	}
	return out, a.store.SetSetting("dockLayouts", out)
}
