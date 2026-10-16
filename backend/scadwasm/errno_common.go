package scadwasm

import "syscall"

// errnoFromSyscall mapea las constantes de syscall (que existen en todos los
// sistemas, con valores distintos) a los códigos de emscripten.
func errnoFromSyscall(se syscall.Errno) errno {
	switch se {
	case syscall.ENOENT:
		return errnoNoEnt
	case syscall.EEXIST:
		return errnoExist
	case syscall.EACCES:
		return errnoAccess
	case syscall.EPERM:
		return errnoPerm
	case syscall.ENOTDIR:
		return errnoNotDir
	case syscall.EISDIR:
		return errnoIsDir
	case syscall.ENOTEMPTY:
		return errnoNotEmpty
	case syscall.ENOSPC:
		return errnoNoSpace
	case syscall.EROFS:
		return errnoROFS
	case syscall.EXDEV:
		return errnoXDev
	case syscall.EINVAL:
		return errnoInval
	case syscall.EMFILE:
		return errnoMFile
	case syscall.ENAMETOOLONG:
		return errnoNameTooLong
	}
	return errnoIO
}
