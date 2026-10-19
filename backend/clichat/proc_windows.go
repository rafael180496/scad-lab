//go:build windows

package clichat

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

// prepareProc evita la consola que abriría cada CLI: el .exe de ScadLab es de
// subsistema gráfico y los CLIs de consola.
func prepareProc(cmd *exec.Cmd) { hideWindow(cmd) }

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// killTree mata el CLI y todo su árbol de procesos. Process.Kill solo mata el
// proceso directo: un CLI de node deja vivos sus hijos (servidores MCP).
// taskkill /T recorre el árbol; sin consola visible, si no cada cancelación
// abriría un destello de ventana.
func killTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	k := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	hideWindow(k)
	if err := k.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
