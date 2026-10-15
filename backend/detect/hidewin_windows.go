//go:build windows

package detect

import (
	"os/exec"
	"syscall"
)

// hideWindow evita que cada `--version` abra un flash de consola en Windows.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
