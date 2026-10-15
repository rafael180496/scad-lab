//go:build !windows

package pty

import (
	"syscall"

	gopty "github.com/aymanbagabas/go-pty"
)

// killTree mata el grupo de procesos entero. go-pty lanza el hijo con Setsid,
// así que su pid es también el id del grupo: matar solo el CLI dejaría vivos
// los servidores MCP o shells que lanzó.
func killTree(cmd *gopty.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
