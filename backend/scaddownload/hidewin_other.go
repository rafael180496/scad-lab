//go:build !windows

package scaddownload

import "os/exec"

func hideWindow(*exec.Cmd) {}
