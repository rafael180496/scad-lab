//go:build !windows

package render

import "os/exec"

func hideWindow(*exec.Cmd) {}
