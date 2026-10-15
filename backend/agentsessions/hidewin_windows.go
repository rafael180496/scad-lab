//go:build windows

package agentsessions

import (
	"os/exec"
	"syscall"
)

// hideWindow evita el destello de consola al correr `opencode session list`.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
