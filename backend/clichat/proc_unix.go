//go:build !windows

package clichat

import (
	"os/exec"
	"syscall"
)

// prepareProc pone al CLI en su propio grupo de procesos: así su pid es también
// el id del grupo y matarlo se lleva a los servidores MCP y shells que lanzó.
func prepareProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree mata el grupo de procesos entero.
func killTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

func hideWindow(*exec.Cmd) {}
