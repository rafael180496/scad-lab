//go:build !windows

package scaddownload

import (
	"errors"
	"syscall"
)

// freeBytes es el espacio libre para un usuario común en el volumen de dir.
func freeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize)), nil
}

// isDiskFull dice si un error de escritura es «disco lleno» (ENOSPC).
func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}
