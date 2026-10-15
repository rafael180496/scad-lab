//go:build !windows

package detect

import "os/exec"

func hideWindow(*exec.Cmd) {}
