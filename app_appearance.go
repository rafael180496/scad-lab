package main

import "scadlab/backend/store"

// SaveAppearance guarda el tamaño de la interfaz y la letra del editor, la
// terminal y el chat. Es un binding aparte de SaveSettings (igual que
// SaveLayout y SaveSwatches) porque Configuración → Apariencia aplica y guarda
// al instante, con el diálogo todavía abierto: reescribir la configuración
// entera con lo que el diálogo tenga en pantalla pisaría lo que el usuario
// aún no confirmó. Los valores fuera de rango se acotan (escala fuera de los
// presets = 100 %, letras a 9–28 px); devuelve lo que quedó guardado.
func (a *App) SaveAppearance(in store.Appearance) (store.Appearance, error) {
	if err := a.ready(); err != nil {
		return in, err
	}
	in = in.Normalize()
	for k, v := range map[string]int{
		"uiScale":          in.UIScale,
		"editorFontSize":   in.EditorFontSize,
		"terminalFontSize": in.TerminalFontSize,
		"chatFontSize":     in.ChatFontSize,
	} {
		if err := a.store.SetSetting(k, v); err != nil {
			return in, err
		}
	}
	return in, nil
}
