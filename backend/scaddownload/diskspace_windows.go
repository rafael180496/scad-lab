//go:build windows

package scaddownload

import (
	"errors"
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeBytes es el espacio libre para el usuario en el volumen de dir
// (GetDiskFreeSpaceExW, lpFreeBytesAvailableToCaller): la stdlib no lo expone y
// golang.org/x/sys no es dependencia.
func freeBytes(dir string) (int64, error) {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	r, _, e := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if r == 0 {
		return 0, e
	}
	return int64(avail), nil
}

// Errores de Windows para «disco lleno».
const (
	errorHandleDiskFull syscall.Errno = 39  // ERROR_HANDLE_DISK_FULL
	errorDiskFull       syscall.Errno = 112 // ERROR_DISK_FULL
)

// isDiskFull dice si un error de escritura es «disco lleno».
func isDiskFull(err error) bool {
	return errors.Is(err, errorDiskFull) || errors.Is(err, errorHandleDiskFull)
}
