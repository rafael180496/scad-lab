//go:build !windows

package scadwasm

import (
	"errors"
	"syscall"
)

// errnoOf2 traduce los errores del sistema que errors.Is no clasifica solo.
func errnoOf2(err error) errno {
	var se syscall.Errno
	if !errors.As(err, &se) {
		return errnoIO
	}
	return errnoFromSyscall(se)
}
