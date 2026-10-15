//go:build windows

package pty

import (
	"os/exec"
	"strconv"
	"syscall"

	gopty "github.com/aymanbagabas/go-pty"
)

// killTree mata el CLI y todo su árbol de procesos. Process.Kill solo mata el
// proceso directo: un CLI de node deja vivos sus hijos (servidores MCP).
// taskkill /T recorre el árbol; sin consola visible (CREATE_NO_WINDOW), si no
// cada cierre de terminal abriría un destello de ventana.
func killTree(cmd *gopty.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	k := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	k.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := k.Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
