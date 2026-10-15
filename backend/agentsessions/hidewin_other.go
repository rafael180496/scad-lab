//go:build !windows

package agentsessions

import "os/exec"

func hideWindow(*exec.Cmd) {}
