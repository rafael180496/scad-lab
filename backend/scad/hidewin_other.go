//go:build !windows

package scad

import "os/exec"

func hideWindow(*exec.Cmd) {}
