//go:build windows

package scaddownload

import (
	"os/exec"
	"syscall"
)

// hideWindow evita el flash de consola al correr `openscad --version`.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
