package scadwasm

import (
	"context"
	"crypto/rand"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tetratelabs/wazero/api"
)

// Las funciones del host que no son excepciones: syscalls de archivos, fd_*
// de WASI, reloj, entorno y memoria. Reproducen lo que hace el glue de
// emscripten (library_syscall.js, library_wasi.js…) de este mismo build, pero
// sobre el árbol virtual de vfs.go en vez del MEMFS de JavaScript.

const atFDCWD = -100

// ret escribe un i32 como resultado de la función del host.
func ret(st []uint64, v int32) { st[0] = api.EncodeI32(v) }

// retErr devuelve -errno (convención de las syscalls de Linux).
func retErr(st []uint64, e errno) { ret(st, -int32(e)) }

// at resuelve una ruta relativa a un descriptor de directorio (calculateAt del
// glue).
func (p *proc) at(dirfd int32, path string, allowEmpty bool) (string, errno) {
	if strings.HasPrefix(path, "/") {
		return path, errnoOK
	}
	var dir string
	if dirfd == atFDCWD {
		dir = p.fs.cwd
	} else {
		f, e := p.fs.get(dirfd)
		if e != errnoOK {
			return "", e
		}
		if !f.isDir {
			return "", errnoNotDir
		}
		dir = f.path
	}
	if path == "" {
		if !allowEmpty {
			return "", errnoNoEnt
		}
		return dir, errnoOK
	}
	return dir + "/" + path, errnoOK
}

// pathArg lee la ruta del argumento n de la syscall.
func (p *proc) pathArg(ptr uint32) (string, errno) {
	s, ok := p.cstr(ptr)
	if !ok {
		return "", errnoFault
	}
	return s, errnoOK
}

// writeStat escribe un struct stat de emscripten (96 bytes).
func (p *proc) writeStat(buf uint32, s statInfo) errno {
	var b [96]byte
	le := func(off int, v uint64, n int) {
		for i := 0; i < n; i++ {
			b[off+i] = byte(v >> (8 * i))
		}
	}
	le(0, 1, 4)                           // dev
	le(4, uint64(s.mode), 4)              // mode
	le(8, 1, 4)                           // nlink
	le(12, 0, 4)                          // uid
	le(16, 0, 4)                          // gid
	le(20, 0, 4)                          // rdev
	le(24, uint64(s.size), 8)             // size
	le(32, 4096, 4)                       // blksize
	le(36, uint64((s.size+4095)/4096), 4) // blocks
	sec := s.mtime.Unix()
	ns := uint64(s.mtime.Nanosecond())
	for _, off := range []int{40, 56, 72} { // atime, mtime, ctime
		le(off, uint64(sec), 8)
		le(off+8, ns, 4)
	}
	le(88, s.ino, 8) // ino
	if !p.mem.Write(buf, b[:]) {
		return errnoFault
	}
	return errnoOK
}

func (p *proc) sysStat(st []uint64, pathPtr, buf uint32) {
	path, e := p.pathArg(pathPtr)
	if e == errnoOK {
		var si statInfo
		if si, e = p.fs.stat(path); e == errnoOK {
			e = p.writeStat(buf, si)
		}
	}
	retErr(st, e)
}

// execArgs lee los varargs (i32 consecutivos desde varargs).
type varargs struct {
	p    *proc
	addr uint32
}

func (v *varargs) i32() int32 {
	x := v.p.u32(v.addr)
	v.addr += 4
	return int32(x)
}

func init() {
	// --- archivos y directorios -------------------------------------------------

	hostImpls["___syscall_openat"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		dirfd, flags := int32(st[0]), int32(st[2])
		path, e := p.pathArg(uint32(st[1]))
		if e == errnoOK {
			path, e = p.at(dirfd, path, false)
		}
		if e != errnoOK {
			retErr(st, e)
			return
		}
		fd, e := p.fs.open(path, int(flags))
		if e != errnoOK {
			retErr(st, e)
			return
		}
		ret(st, fd)
	}
	hostImpls["___syscall_stat64"] = func(ctx context.Context, _ api.Module, st []uint64) {
		procOf(ctx).sysStat(st, uint32(st[0]), uint32(st[1]))
	}
	// Los enlaces simbólicos del disco se siguen de forma transparente: lstat
	// es stat, y así realpath/canonical no se pierden en un destino que el
	// árbol virtual no conoce.
	hostImpls["___syscall_lstat64"] = hostImpls["___syscall_stat64"]
	hostImpls["___syscall_fstat64"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		si, e := p.fs.fstat(int32(st[0]))
		if e == errnoOK {
			e = p.writeStat(uint32(st[1]), si)
		}
		retErr(st, e)
	}
	hostImpls["___syscall_newfstatat"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		dirfd, flags, buf := int32(st[0]), int32(st[3]), uint32(st[2])
		path, e := p.pathArg(uint32(st[1]))
		if e == errnoOK {
			path, e = p.at(dirfd, path, flags&4096 != 0)
		}
		if e == errnoOK {
			var si statInfo
			if si, e = p.fs.stat(path); e == errnoOK {
				e = p.writeStat(buf, si)
			}
		}
		retErr(st, e)
	}
	hostImpls["___syscall_faccessat"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		dirfd, amode := int32(st[0]), int32(st[2])
		if amode&^7 != 0 {
			retErr(st, errnoInval)
			return
		}
		path, e := p.pathArg(uint32(st[1]))
		if e == errnoOK {
			path, e = p.at(dirfd, path, false)
		}
		if e == errnoOK {
			e = p.fs.access(path, amode)
		}
		retErr(st, e)
	}
	hostImpls["___syscall_mkdirat"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		path, e := p.pathArg(uint32(st[1]))
		if e == errnoOK {
			path, e = p.at(int32(st[0]), path, false)
		}
		if e == errnoOK {
			e = p.fs.mkdir(path)
		}
		retErr(st, e)
	}
	hostImpls["___syscall_rmdir"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		path, e := p.pathArg(uint32(st[0]))
		if e == errnoOK {
			e = p.fs.rmdir(path)
		}
		retErr(st, e)
	}
	hostImpls["___syscall_unlinkat"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		flags := int32(st[2])
		path, e := p.pathArg(uint32(st[1]))
		if e == errnoOK {
			path, e = p.at(int32(st[0]), path, false)
		}
		if e == errnoOK {
			switch flags {
			case 0:
				e = p.fs.unlink(path)
			case 512: // AT_REMOVEDIR
				e = p.fs.rmdir(path)
			default:
				e = errnoInval
			}
		}
		retErr(st, e)
	}
	hostImpls["___syscall_renameat"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		oldp, e := p.pathArg(uint32(st[1]))
		var newp string
		if e == errnoOK {
			newp, e = p.pathArg(uint32(st[3]))
		}
		if e == errnoOK {
			oldp, e = p.at(int32(st[0]), oldp, false)
		}
		if e == errnoOK {
			newp, e = p.at(int32(st[2]), newp, false)
		}
		if e == errnoOK {
			e = p.fs.rename(oldp, newp)
		}
		retErr(st, e)
	}
	hostImpls["___syscall_readlinkat"] = func(ctx context.Context, _ api.Module, st []uint64) {
		// No hay enlaces simbólicos visibles: lo que existe «no es un enlace».
		p := procOf(ctx)
		if int32(st[3]) <= 0 {
			retErr(st, errnoInval)
			return
		}
		path, e := p.pathArg(uint32(st[1]))
		if e == errnoOK {
			path, e = p.at(int32(st[0]), path, false)
		}
		if e == errnoOK {
			if _, e = p.fs.stat(path); e == errnoOK {
				e = errnoInval
			}
		}
		retErr(st, e)
	}
	hostImpls["___syscall_chdir"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		path, e := p.pathArg(uint32(st[0]))
		if e == errnoOK {
			e = p.fs.chdir(path)
		}
		retErr(st, e)
	}
	hostImpls["___syscall_getcwd"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		buf, size := uint32(st[0]), uint32(st[1])
		if size == 0 {
			retErr(st, errnoInval)
			return
		}
		cwd := p.fs.cwd
		n := uint32(len(cwd) + 1)
		if size < n {
			retErr(st, errnoRange)
			return
		}
		if !p.mem.Write(buf, append([]byte(cwd), 0)) {
			retErr(st, errnoFault)
			return
		}
		ret(st, int32(n))
	}
	// chmod/utimens: los archivos del disco no se tocan; basta con que la ruta
	// exista.
	existsOnly := func(pathArgIdx int, dirfdIdx int) hostFn {
		return func(ctx context.Context, _ api.Module, st []uint64) {
			p := procOf(ctx)
			path, e := p.pathArg(uint32(st[pathArgIdx]))
			if e == errnoOK {
				if dirfdIdx >= 0 {
					path, e = p.at(int32(st[dirfdIdx]), path, true)
				}
			}
			if e == errnoOK {
				_, e = p.fs.stat(path)
			}
			retErr(st, e)
		}
	}
	hostImpls["___syscall_chmod"] = existsOnly(0, -1)
	hostImpls["___syscall_utimensat"] = existsOnly(1, 0)
	hostImpls["___syscall_fchmod"] = func(ctx context.Context, _ api.Module, st []uint64) {
		_, e := procOf(ctx).fs.get(int32(st[0]))
		retErr(st, e)
	}
	hostImpls["___syscall_dup"] = func(ctx context.Context, _ api.Module, st []uint64) {
		fd, e := procOf(ctx).fs.dup(int32(st[0]), 0)
		if e != errnoOK {
			retErr(st, e)
			return
		}
		ret(st, fd)
	}
	hostImpls["___syscall_fcntl64"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		fd, cmd := int32(st[0]), int32(st[1])
		va := &varargs{p: p, addr: uint32(st[2])}
		f, e := p.fs.get(fd)
		if e != errnoOK {
			retErr(st, e)
			return
		}
		switch cmd {
		case 0: // F_DUPFD
			arg := va.i32()
			if arg < 0 {
				retErr(st, errnoInval)
				return
			}
			nfd, e := p.fs.dup(fd, arg)
			if e != errnoOK {
				retErr(st, e)
				return
			}
			ret(st, nfd)
		case 1, 2: // F_GETFD, F_SETFD
			ret(st, 0)
		case 3: // F_GETFL
			ret(st, int32(f.flags))
		case 4: // F_SETFL
			f.flags |= int(va.i32())
			ret(st, 0)
		case 12: // F_GETLK: sin bloqueos ajenos
			p.put16(uint32(va.i32()), 2)
			ret(st, 0)
		case 13, 14: // F_SETLK, F_SETLKW
			ret(st, 0)
		default:
			retErr(st, errnoInval)
		}
	}
	hostImpls["___syscall_fstatfs64"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		if _, e := p.fs.get(int32(st[0])); e != errnoOK {
			retErr(st, e)
			return
		}
		buf := uint32(st[2])
		p.put32(buf+4, 4096)  // bsize
		p.put32(buf+40, 4096) // frsize
		p.put32(buf+8, 1<<20) // blocks
		p.put32(buf+12, 1<<19)
		p.put32(buf+16, 1<<19)
		p.put32(buf+20, 1<<16)
		p.put32(buf+24, 1<<15)
		p.put32(buf+28, 0)
		p.put32(buf+44, 0)
		p.put32(buf+36, 255) // namelen
		ret(st, 0)
	}
	hostImpls["___syscall_getdents64"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		fd, dirp, count := int32(st[0]), uint32(st[1]), uint32(st[2])
		f, e := p.fs.get(fd)
		if e == errnoOK && !f.isDir {
			e = errnoNotDir
		}
		if e != errnoOK {
			retErr(st, e)
			return
		}
		if f.entries == nil {
			names, e := p.fs.readdirNode(f.dirNode)
			if e != errnoOK {
				retErr(st, e)
				return
			}
			f.entries = names
		}
		const size = 280
		start := int(f.pos / size)
		end := min(len(f.entries), start+int(count/size))
		pos := uint32(0)
		for idx := start; idx < end; idx++ {
			name := f.entries[idx]
			typ := byte(4)
			id := inoOf(f.path + "/" + name)
			if name != "." && name != ".." {
				if si, e := p.fs.stat(f.path + "/" + name); e != errnoOK {
					continue // desapareció entre la lista y la consulta
				} else if !si.isDir() {
					typ = 8
				}
			}
			var rec [size]byte
			putLE(rec[0:], id, 8)
			putLE(rec[8:], uint64((idx+1)*size), 8)
			putLE(rec[16:], size, 2)
			rec[18] = typ
			copy(rec[19:19+255], name)
			if !p.mem.Write(dirp+pos, rec[:]) {
				retErr(st, errnoFault)
				return
			}
			pos += size
		}
		f.pos = int64(max(end, start)) * size
		ret(st, int32(pos))
	}
	hostImpls["___syscall_ioctl"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		fd, op := int32(st[0]), int32(st[1])
		va := &varargs{p: p, addr: uint32(st[2])}
		f, e := p.fs.get(fd)
		if e != errnoOK {
			retErr(st, e)
			return
		}
		notTTY := func() bool {
			if !f.tty {
				retErr(st, errnoNotTTY)
				return true
			}
			return false
		}
		switch op {
		case 21505: // TCGETS
			if notTTY() {
				return
			}
			argp := uint32(va.i32())
			for i, v := range []uint32{25856, 5, 191, 35387} {
				p.put32(argp+uint32(i*4), v)
			}
			cc := []byte{3, 28, 127, 21, 4, 0, 1, 0, 17, 19, 26, 0, 18, 15, 23, 22}
			p.mem.Write(argp+17, append(cc, make([]byte, 16)...))
			ret(st, 0)
		case 21506, 21507, 21508, 21509, 21510, 21511, 21512, 21515, 21524: // TCSETS*, TCGETA…
			if notTTY() {
				return
			}
			ret(st, 0)
		case 21519: // TIOCGPGRP
			if notTTY() {
				return
			}
			p.put32(uint32(va.i32()), 0)
			ret(st, 0)
		case 21520:
			if notTTY() {
				return
			}
			retErr(st, errnoInval)
		case 21523: // TIOCGWINSZ: sin esto musl deja stdout en búfer completo
			if notTTY() {
				return
			}
			argp := uint32(va.i32())
			p.put16(argp, 24)
			p.put16(argp+2, 80)
			ret(st, 0)
		case 21531: // FIONREAD
			retErr(st, errnoNotTTY)
		default:
			retErr(st, errnoInval)
		}
	}

	// --- fd_* de WASI -------------------------------------------------------------

	hostImpls["_fd_close"] = func(ctx context.Context, _ api.Module, st []uint64) {
		ret(st, int32(procOf(ctx).fs.close(int32(st[0]))))
	}
	hostImpls["_fd_fdstat_get"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		f, e := p.fs.get(int32(st[0]))
		if e != errnoOK {
			ret(st, int32(e))
			return
		}
		typ := byte(4) // archivo
		switch {
		case f.tty:
			typ = 2
		case f.isDir:
			typ = 3
		}
		pbuf := uint32(st[1])
		p.put8(pbuf, typ)
		p.put16(pbuf+2, 0)
		p.put64(pbuf+8, 0)
		p.put64(pbuf+16, 0)
		ret(st, 0)
	}
	hostImpls["_fd_read"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		f, e := p.fs.get(int32(st[0]))
		if e != errnoOK {
			ret(st, int32(e))
			return
		}
		iov, cnt, pnum := uint32(st[1]), uint32(st[2]), uint32(st[3])
		total := 0
		for i := uint32(0); i < cnt; i++ {
			ptr, l := p.u32(iov), p.u32(iov+4)
			iov += 8
			if l == 0 {
				continue
			}
			buf, ok := p.mem.Read(ptr, l)
			if !ok {
				ret(st, int32(errnoFault))
				return
			}
			n, e := p.fs.read(f, buf)
			if e != errnoOK {
				ret(st, int32(e))
				return
			}
			total += n
			if uint32(n) < l {
				break
			}
		}
		p.put32(pnum, uint32(total))
		ret(st, 0)
	}
	hostImpls["_fd_write"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		f, e := p.fs.get(int32(st[0]))
		if e != errnoOK {
			ret(st, int32(e))
			return
		}
		iov, cnt, pnum := uint32(st[1]), uint32(st[2]), uint32(st[3])
		total := 0
		for i := uint32(0); i < cnt; i++ {
			ptr, l := p.u32(iov), p.u32(iov+4)
			iov += 8
			if l == 0 {
				continue
			}
			buf, ok := p.mem.Read(ptr, l)
			if !ok {
				ret(st, int32(errnoFault))
				return
			}
			if f.tty {
				if f.ttyFD == 0 {
					ret(st, int32(errnoBadF))
					return
				}
				p.out.Write(buf)
				total += int(l)
				continue
			}
			n, e := p.fs.write(f, buf)
			total += n
			if e != errnoOK {
				if total > 0 {
					break
				}
				ret(st, int32(e))
				return
			}
			if uint32(n) < l {
				break
			}
		}
		p.put32(pnum, uint32(total))
		ret(st, 0)
	}
	hostImpls["_fd_seek"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		f, e := p.fs.get(int32(st[0]))
		if e != errnoOK {
			ret(st, int32(e))
			return
		}
		off := int64(st[1])
		if off > 1<<53 || off < -(1<<53) {
			ret(st, int32(errnoOverflow))
			return
		}
		whence := int32(st[2])
		pos, e := p.fs.seek(f, off, whence)
		if e != errnoOK {
			ret(st, int32(e))
			return
		}
		// rewinddir: volver al principio vuelve a listar el directorio.
		if f.isDir && off == 0 && whence == 0 {
			f.entries = nil
		}
		p.put64(uint32(st[3]), uint64(pos))
		ret(st, 0)
	}

	// --- mmap ---------------------------------------------------------------------

	hostImpls["__mmap_js"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		length, prot, fd := uint32(st[0]), int32(st[1]), int32(st[3])
		offset := int64(st[4])
		allocated, addr := uint32(st[5]), uint32(st[6])
		_ = prot
		f, e := p.fs.get(fd)
		if e == errnoOK && (f.tty || f.isDir || f.bf == nil) {
			e = errnoNoSys
		}
		if e != errnoOK {
			retErr(st, e)
			return
		}
		// mmapAlloc: memoria alineada a 64 KB, puesta a cero, y el contenido del
		// archivo copiado. Un mapeo «de verdad» no existe en WebAssembly.
		size := (uint64(length) + 65535) &^ 65535
		p.scratch[0], p.scratch[1] = 65536, size
		if err := p.fnMemalign.CallWithStack(p.nctx, p.scratch[:2]); err != nil {
			panic(err)
		}
		ptr := uint32(p.scratch[0])
		if ptr == 0 {
			retErr(st, errnoNoMem)
			return
		}
		dst, ok := p.mem.Read(ptr, uint32(size))
		if !ok {
			retErr(st, errnoNoMem)
			return
		}
		clear(dst)
		n, _ := f.bf.readAt(dst[:length], offset)
		_ = n
		p.put32(allocated, 1)
		p.put32(addr, ptr)
		ret(st, 0)
	}
	hostImpls["__munmap_js"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		addr, length, prot, flags, fd := uint32(st[0]), uint32(st[1]), int32(st[2]), int32(st[3]), int32(st[4])
		offset := int64(st[5])
		f, e := p.fs.get(fd)
		if e != errnoOK {
			retErr(st, e)
			return
		}
		// Solo un mapeo compartido y escribible vuelve al archivo (MAP_PRIVATE = 2).
		if prot&2 != 0 && flags&2 == 0 && f.bf != nil && f.flags&oAccMode != 0 {
			if src, ok := p.mem.Read(addr, length); ok {
				_, _ = f.bf.writeAt(src, offset)
			}
		}
		ret(st, 0)
	}

	// --- entorno ------------------------------------------------------------------

	hostImpls["_environ_sizes_get"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		size := 0
		for _, s := range p.env {
			size += len(s) + 1
		}
		p.put32(uint32(st[0]), uint32(len(p.env)))
		p.put32(uint32(st[1]), uint32(size))
		ret(st, 0)
	}
	hostImpls["_environ_get"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		environ, buf := uint32(st[0]), uint32(st[1])
		off := uint32(0)
		for i, s := range p.env {
			p.put32(environ+uint32(i*4), buf+off)
			p.mem.Write(buf+off, append([]byte(s), 0))
			off += uint32(len(s) + 1)
		}
		ret(st, 0)
	}

	// --- tiempo -------------------------------------------------------------------

	hostImpls["_emscripten_date_now"] = func(_ context.Context, _ api.Module, st []uint64) {
		st[0] = api.EncodeF64(float64(time.Now().UnixMilli()))
	}
	hostImpls["_clock_time_get"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		id := int32(st[0])
		if id < 0 || id > 3 {
			ret(st, int32(errnoInval))
			return
		}
		var ns int64
		if id == 0 {
			ns = time.Now().UnixNano()
		} else {
			ns = int64(time.Since(p.start)) + 1_000_000 // monótono y nunca 0
		}
		p.put64(uint32(st[2]), uint64(ns))
		ret(st, 0)
	}
	hostImpls["__gmtime_js"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		writeTM(p, uint32(st[1]), time.Unix(int64(st[0]), 0).UTC(), false)
	}
	hostImpls["__localtime_js"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		writeTM(p, uint32(st[1]), time.Unix(int64(st[0]), 0).In(time.Local), true)
	}
	hostImpls["__mktime_js"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		tm := uint32(st[0])
		rd := func(off uint32) int { return int(int32(p.u32(tm + off))) }
		t := time.Date(rd(20)+1900, time.Month(rd(16)+1), rd(12), rd(8), rd(4), rd(0), 0, time.Local)
		if t.Unix() < math.MinInt32 || t.Unix() > math.MaxInt32*2 {
			st[0] = ^uint64(0) // -1: fuera de rango
			return
		}
		writeTM(p, tm, t, true)
		st[0] = uint64(t.Unix())
	}
	hostImpls["__tzset_js"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		year := time.Now().Year()
		_, winter := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local).Zone()
		_, summer := time.Date(year, 7, 1, 0, 0, 0, 0, time.Local).Zone()
		// getTimezoneOffset del navegador: minutos al OESTE de UTC.
		winterOff, summerOff := -winter/60, -summer/60
		std := max(winterOff, summerOff)
		p.put32(uint32(st[0]), uint32(int32(std*60)))
		dst := int32(0)
		if winterOff != summerOff {
			dst = 1
		}
		p.put32(uint32(st[1]), uint32(dst))
		zone := func(off int) string {
			sign := "-"
			if off < 0 {
				sign = "+"
			}
			a := off
			if a < 0 {
				a = -a
			}
			return "UTC" + sign + pad2(a/60) + pad2(a%60)
		}
		stdName, dstName := zone(summerOff), zone(winterOff)
		if summerOff < winterOff {
			stdName, dstName = zone(winterOff), zone(summerOff)
		}
		p.mem.Write(uint32(st[2]), append([]byte(stdName), 0))
		p.mem.Write(uint32(st[3]), append([]byte(dstName), 0))
	}
	hostImpls["_random_get"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		buf, ok := p.mem.Read(uint32(st[0]), uint32(st[1]))
		if !ok {
			ret(st, int32(errnoFault))
			return
		}
		_, _ = rand.Read(buf)
		ret(st, 0)
	}

	// --- memoria y proceso ----------------------------------------------------------

	hostImpls["_emscripten_get_heap_max"] = func(_ context.Context, _ api.Module, st []uint64) {
		st[0] = uint64(uint32(maxHeapBytes))
	}
	hostImpls["_emscripten_resize_heap"] = func(ctx context.Context, _ api.Module, st []uint64) {
		if procOf(ctx).resizeHeap(uint32(st[0])) {
			ret(st, 1)
		} else {
			ret(st, 0)
		}
	}
	// system(): no hay procesos que lanzar.
	hostImpls["__emscripten_system"] = func(_ context.Context, _ api.Module, st []uint64) {
		if uint32(st[0]) == 0 {
			ret(st, 0)
			return
		}
		retErr(st, errnoNoSys)
	}
}

const maxHeapBytes = 4294901760

// resizeHeap crece la memoria del módulo (emscripten_resize_heap del glue:
// +20 % como mínimo, hasta 96 MB de holgura sobre lo pedido).
func (p *proc) resizeHeap(requested uint32) bool {
	oldSize := uint64(p.mem.Size())
	req := uint64(requested)
	if req > maxHeapBytes {
		return false
	}
	for cut := 1; cut <= 4; cut *= 2 {
		over := float64(oldSize) * (1 + 0.2/float64(cut))
		over = math.Min(over, float64(req)+100663296)
		want := max(req, uint64(over))
		newSize := min(uint64(maxHeapBytes), (want+65535)&^65535)
		if newSize <= oldSize {
			return true
		}
		pages := (newSize - oldSize + 65535) / 65536
		if _, ok := p.mem.Grow(uint32(pages)); ok {
			return true
		}
	}
	return false
}

func putLE(b []byte, v uint64, n int) {
	for i := 0; i < n; i++ {
		b[i] = byte(v >> (8 * i))
	}
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// writeTM escribe un struct tm (wasm32: 9 enteros + gmtoff).
func writeTM(p *proc, tm uint32, t time.Time, local bool) {
	w := func(off uint32, v int) { p.put32(tm+off, uint32(int32(v))) }
	w(0, t.Second())
	w(4, t.Minute())
	w(8, t.Hour())
	w(12, t.Day())
	w(16, int(t.Month())-1)
	w(20, t.Year()-1900)
	w(24, int(t.Weekday()))
	w(28, t.YearDay()-1)
	if local {
		_, off := t.Zone()
		w(36, off)
		dst := 0
		if t.IsDST() {
			dst = 1
		}
		w(32, dst)
	} else {
		w(36, 0)
		w(32, 0)
	}
}

// buildEnv arma el entorno de la ejecución: los valores por defecto del glue de
// emscripten más lo que pida quien llama (que gana).
func buildEnv(extra map[string]string, cwd string) []string {
	env := map[string]string{
		"USER":    "web_user",
		"LOGNAME": "web_user",
		"PATH":    "/",
		"PWD":     cwd,
		"HOME":    "/home/web_user",
		"LANG":    "en_US.UTF-8",
		"_":       "./this.program",
	}
	for k, v := range extra {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}
