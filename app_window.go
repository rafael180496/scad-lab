package main

import (
	"os/exec"
	goruntime "runtime"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// TitleBarDoubleClick hace lo que haría un doble clic en la barra de título
// nativa. En macOS la barra está oculta (TitleBarHiddenInset) y la cabecera de
// la app hace de barra, así que el doble clic hay que implementarlo: se respeta
// la preferencia del sistema (Ajustes → Escritorio y Dock → «Al hacer doble
// clic en la barra de título de una ventana»): Maximize (por defecto) agranda
// o restaura, Minimize minimiza y None no hace nada. En Windows la cabecera no
// es la barra de título (se conserva el marco nativo), pero agrandar con doble
// clic es lo que se espera de una cabecera que arrastra la ventana.
func (a *App) TitleBarDoubleClick() {
	if a.ctx == nil {
		return
	}
	switch doubleClickAction() {
	case "none":
		return
	case "minimize":
		wruntime.WindowMinimise(a.ctx)
	default:
		wruntime.WindowToggleMaximise(a.ctx)
	}
}

// doubleClickAction lee AppleActionOnDoubleClick («Maximize», «Minimize»,
// «None»; sin la clave, Maximize). Fuera de macOS siempre es "maximize".
func doubleClickAction() string {
	if goruntime.GOOS != "darwin" {
		return "maximize"
	}
	out, err := exec.Command("defaults", "read", "-g", "AppleActionOnDoubleClick").Output()
	if err != nil {
		return "maximize"
	}
	switch strings.ToLower(strings.TrimSpace(string(out))) {
	case "minimize":
		return "minimize"
	case "none":
		return "none"
	}
	return "maximize"
}
