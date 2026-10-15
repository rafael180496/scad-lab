package main

import "scadlab/backend/store"

// SaveLayout guarda la disposición de los paneles (anchos, alto del panel
// inferior, barra lateral plegada). Es un binding aparte de SaveSettings por
// la misma razón que SaveSwatches: soltar un divisor no debe reescribir la
// configuración entera con lo que la ventana tenga en memoria. Los valores
// fuera de rango se acotan; devuelve lo que quedó guardado.
func (a *App) SaveLayout(l store.Layout) (store.Layout, error) {
	if err := a.ready(); err != nil {
		return l, err
	}
	l = l.Normalize()
	return l, a.store.SetSetting("layout", l)
}
