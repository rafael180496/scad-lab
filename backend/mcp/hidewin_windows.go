//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
)

// hideWindow evita que cada OpenSCAD que lanza el servidor abra una consola en
// Windows: el .exe de ScadLab es de subsistema gráfico y OpenSCAD de consola.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
