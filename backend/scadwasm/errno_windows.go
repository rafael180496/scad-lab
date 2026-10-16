//go:build windows

package scadwasm

import (
	"errors"
	"syscall"
)

// Códigos de Windows que no tienen una constante equivalente en syscall.
const (
	winErrorDiskFull       = 112
	winErrorHandleDiskFull = 39
	winErrorDirNotEmpty    = 145
	winErrorNotSameDevice  = 17
)

func errnoOf2(err error) errno {
	var se syscall.Errno
	if !errors.As(err, &se) {
		return errnoIO
	}
	switch uintptr(se) {
	case winErrorDiskFull, winErrorHandleDiskFull:
		return errnoNoSpace
	case winErrorDirNotEmpty:
		return errnoNotEmpty
	case winErrorNotSameDevice:
		return errnoXDev
	}
	return errnoFromSyscall(se)
}
