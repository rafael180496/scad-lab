//go:build windows

package scad

import (
	"os/exec"
	"syscall"
)

// hideWindow evita que cada lint abra una consola en Windows: OpenSCAD es
// un ejecutable de consola y la app es gráfica, así que sin CREATE_NO_WINDOW
// cada tecla en el editor (lint con debounce) haría parpadear una ventana negra.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
