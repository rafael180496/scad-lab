package scadwasm

import (
	"errors"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Este archivo es el sistema de archivos virtual del motor integrado. El
// OpenSCAD en WebAssembly no ve el disco del usuario: ve un árbol propio cuya
// raíz es sintética y solo contiene los montajes que pidió quien lo llama
// (RunSpec.Mounts) más /fonts. Lo que queda fuera de un montaje no existe
// (ENOENT), y escribir en uno de solo lectura falla con EROFS.
//
// Las rutas se limpian en el espacio VIRTUAL antes de tocar el disco: `..` se
// resuelve contra el árbol virtual (`/work/../etc` es `/etc`, que no existe) y
// la ruta que llega al disco ya no lleva `..`, así que no hay forma de salir de
// un montaje escribiendo la ruta de otra manera.

// errno son los códigos de error de emscripten/WASI (los mismos que usa el
// glue de JS). Las syscalls devuelven -errno; las funciones fd_* devuelven el
// código tal cual.
type errno int32

const (
	errnoOK           errno = 0
	errnoNoSpace      errno = 51
	errnoAccess       errno = 2
	errnoBadF         errno = 8
	errnoExist        errno = 20
	errnoFault        errno = 21
	errnoInval        errno = 28
	errnoIO           errno = 29
	errnoIsDir        errno = 31
	errnoMFile        errno = 33
	errnoNameTooLong  errno = 37
	errnoNoEnt        errno = 44
	errnoNoMem        errno = 48
	errnoNoSys        errno = 52
	errnoNotDir       errno = 54
	errnoNotEmpty     errno = 55
	errnoNotTTY       errno = 59
	errnoPerm         errno = 63
	errnoRange        errno = 68
	errnoROFS         errno = 69
	errnoSPipe        errno = 70
	errnoXDev         errno = 75
	errnoOverflow     errno = 61
	errnoNotSupported errno = 138
)

func (e errno) Error() string { return "errno " + itoa(int(e)) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// errnoOf traduce un error de Go al errno equivalente.
func errnoOf(err error) errno {
	if err == nil {
		return errnoOK
	}
	var e errno
	if errors.As(err, &e) {
		return e
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errnoNoEnt
	case errors.Is(err, fs.ErrExist):
		return errnoExist
	case errors.Is(err, fs.ErrPermission):
		return errnoAccess
	case errors.Is(err, fs.ErrInvalid):
		return errnoInval
	}
	return errnoOf2(err)
}

// Flags de open (los de musl en wasm32).
const (
	oAccMode   = 3
	oWrOnly    = 1
	oRdWr      = 2
	oCreat     = 0o100
	oExcl      = 0o200
	oTrunc     = 0o1000
	oAppend    = 0o2000
	oDirectory = 0o200000
)

// Tipos de archivo del campo mode de stat.
const (
	sIFDir  = 0o040000
	sIFReg  = 0o100000
	sIFChr  = 0o020000
	permDir = 0o555
	permReg = 0o444
	permRW  = 0o666
	permDRW = 0o777
)

// statInfo es lo que devuelven stat/fstat.
type statInfo struct {
	mode  uint32
	size  int64
	mtime time.Time
	ino   uint64
}

func (s statInfo) isDir() bool { return s.mode&0o170000 == sIFDir }

// backend es un montaje: dónde viven de verdad los archivos de un directorio
// virtual. Las rutas que recibe son relativas al montaje, con "/" y ya
// limpias (sin "..").
type backend interface {
	stat(rel string) (statInfo, errno)
	open(rel string, flags int) (backFile, errno)
	readdir(rel string) ([]string, errno)
	mkdir(rel string) errno
	rmdir(rel string) errno
	unlink(rel string) errno
	rename(from, to string) errno
	readOnly() bool
}

// backFile es un archivo abierto de un montaje.
type backFile interface {
	readAt(p []byte, off int64) (int, error)
	writeAt(p []byte, off int64) (int, error)
	size() (int64, error)
	truncate(n int64) error
	close() error
}

// --- montaje de un directorio del disco --------------------------------------

type hostBackend struct {
	root string
	ro   bool
}

func (h *hostBackend) readOnly() bool { return h.ro }

// hostPath une la raíz del montaje con una ruta relativa ya limpia. filepath.Join
// la normaliza al separador del sistema (en Windows, "a/b" pasa a "a\b" sobre
// "C:\Users\...").
func (h *hostBackend) hostPath(rel string) string {
	if rel == "" {
		return h.root
	}
	return filepath.Join(h.root, filepath.FromSlash(rel))
}

func infoOf(fi fs.FileInfo, key string) statInfo {
	m := uint32(permReg)
	if fi.IsDir() {
		m = sIFDir | permDRW
	} else {
		m = sIFReg | permRW
	}
	size := fi.Size()
	if fi.IsDir() {
		size = 4096
	}
	return statInfo{mode: m, size: size, mtime: fi.ModTime(), ino: inoOf(key)}
}

func inoOf(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64() & 0x1fffffffffffff // cabe en el entero de 53 bits que usaba el glue
}

func (h *hostBackend) stat(rel string) (statInfo, errno) {
	fi, err := os.Stat(h.hostPath(rel))
	if err != nil {
		return statInfo{}, errnoOf(err)
	}
	return infoOf(fi, "host:"+h.root+"/"+rel), errnoOK
}

func (h *hostBackend) open(rel string, flags int) (backFile, errno) {
	hp := h.hostPath(rel)
	fi, err := os.Stat(hp)
	if err == nil && fi.IsDir() {
		return nil, errnoIsDir
	}
	osFlags := os.O_RDONLY
	switch flags & oAccMode {
	case oWrOnly:
		osFlags = os.O_WRONLY
	case oRdWr:
		osFlags = os.O_RDWR
	}
	if flags&oCreat != 0 {
		osFlags |= os.O_CREATE
	}
	if flags&oExcl != 0 {
		osFlags |= os.O_EXCL
	}
	if flags&oTrunc != 0 {
		osFlags |= os.O_TRUNC
	}
	// O_APPEND se resuelve con el desplazamiento, no con el flag del sistema:
	// WriteAt falla en un archivo abierto con O_APPEND.
	f, err := os.OpenFile(hp, osFlags, 0o644)
	if err != nil {
		return nil, errnoOf(err)
	}
	return &hostFile{f: f}, errnoOK
}

func (h *hostBackend) readdir(rel string) ([]string, errno) {
	ents, err := os.ReadDir(h.hostPath(rel))
	if err != nil {
		return nil, errnoOf(err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names, errnoOK
}

func (h *hostBackend) mkdir(rel string) errno {
	if rel == "" {
		return errnoExist
	}
	return errnoOf(os.Mkdir(h.hostPath(rel), 0o755))
}

func (h *hostBackend) rmdir(rel string) errno {
	hp := h.hostPath(rel)
	fi, err := os.Stat(hp)
	if err != nil {
		return errnoOf(err)
	}
	if !fi.IsDir() {
		return errnoNotDir
	}
	// Se comprueba a mano: el código de error de «no está vacío» cambia según
	// el sistema y en Windows no coincide con ENOTEMPTY.
	if ents, err := os.ReadDir(hp); err == nil && len(ents) > 0 {
		return errnoNotEmpty
	}
	return errnoOf(os.Remove(hp))
}

func (h *hostBackend) unlink(rel string) errno {
	hp := h.hostPath(rel)
	fi, err := os.Stat(hp)
	if err != nil {
		return errnoOf(err)
	}
	if fi.IsDir() {
		return errnoIsDir
	}
	return errnoOf(os.Remove(hp))
}

func (h *hostBackend) rename(from, to string) errno {
	return errnoOf(os.Rename(h.hostPath(from), h.hostPath(to)))
}

type hostFile struct{ f *os.File }

func (h *hostFile) readAt(p []byte, off int64) (int, error)  { return h.f.ReadAt(p, off) }
func (h *hostFile) writeAt(p []byte, off int64) (int, error) { return h.f.WriteAt(p, off) }
func (h *hostFile) truncate(n int64) error                   { return h.f.Truncate(n) }
func (h *hostFile) close() error                             { return h.f.Close() }
func (h *hostFile) size() (int64, error) {
	fi, err := h.f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// --- montaje de un fs.FS (las fuentes embebidas) -----------------------------

type fsBackend struct {
	fsys fs.FS
	tag  string
}

func (b *fsBackend) readOnly() bool { return true }

func fsPath(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

func (b *fsBackend) stat(rel string) (statInfo, errno) {
	fi, err := fs.Stat(b.fsys, fsPath(rel))
	if err != nil {
		return statInfo{}, errnoOf(err)
	}
	si := infoOf(fi, b.tag+"/"+rel)
	if fi.IsDir() {
		si.mode = sIFDir | permDir
	} else {
		si.mode = sIFReg | permReg
	}
	// El embed no guarda fecha: una fija y estable mantiene igual la caché de
	// fontconfig entre ejecuciones.
	si.mtime = time.Unix(1_700_000_000, 0)
	return si, errnoOK
}

func (b *fsBackend) open(rel string, flags int) (backFile, errno) {
	if flags&oAccMode != 0 || flags&(oCreat|oTrunc|oAppend) != 0 {
		return nil, errnoROFS
	}
	f, err := b.fsys.Open(fsPath(rel))
	if err != nil {
		return nil, errnoOf(err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, errnoOf(err)
	}
	if fi.IsDir() {
		_ = f.Close()
		return nil, errnoIsDir
	}
	if ra, ok := f.(io.ReaderAt); ok {
		return &fsFile{f: f, ra: ra, n: fi.Size()}, errnoOK
	}
	// Sin ReaderAt (otro fs.FS): se lee entero una vez.
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return nil, errnoIO
	}
	return &memFile{data: data}, errnoOK
}

func (b *fsBackend) readdir(rel string) ([]string, errno) {
	ents, err := fs.ReadDir(b.fsys, fsPath(rel))
	if err != nil {
		return nil, errnoOf(err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names, errnoOK
}

func (b *fsBackend) mkdir(string) errno       { return errnoROFS }
func (b *fsBackend) rmdir(string) errno       { return errnoROFS }
func (b *fsBackend) unlink(string) errno      { return errnoROFS }
func (b *fsBackend) rename(_, _ string) errno { return errnoROFS }

type fsFile struct {
	f  fs.File
	ra io.ReaderAt
	n  int64
}

func (f *fsFile) readAt(p []byte, off int64) (int, error) { return f.ra.ReadAt(p, off) }
func (f *fsFile) writeAt([]byte, int64) (int, error)      { return 0, errnoROFS }
func (f *fsFile) size() (int64, error)                    { return f.n, nil }
func (f *fsFile) truncate(int64) error                    { return errnoROFS }
func (f *fsFile) close() error                            { return f.f.Close() }

type memFile struct{ data []byte }

func (m *memFile) readAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (m *memFile) writeAt([]byte, int64) (int, error) { return 0, errnoROFS }
func (m *memFile) size() (int64, error)               { return int64(len(m.data)), nil }
func (m *memFile) truncate(int64) error               { return errnoROFS }
func (m *memFile) close() error                       { return nil }

// --- el árbol virtual ---------------------------------------------------------

type mountPoint struct {
	virtual string // "/work", sin barra final
	be      backend
}

// vfs es el árbol virtual de una ejecución. No es seguro para uso concurrente:
// una instancia de OpenSCAD es de un solo hilo y cada Run tiene su propio vfs.
type vfs struct {
	mounts []*mountPoint // más específico primero
	cwd    string

	files map[int32]*openFile
}

// openFile es un descriptor abierto (o varios, si se duplicó).
type openFile struct {
	refs int
	path string // ruta virtual, para openat con dirfd

	// Archivo común.
	bf     backFile
	pos    int64
	append bool
	flags  int

	// Directorio: la lista se fija al abrirse (como el glue de emscripten).
	isDir   bool
	entries []string // incluye "." y ".."
	dirNode *vnode

	// Dispositivos de caracteres: stdin, stdout y stderr (ttyFD 0, 1, 2).
	tty   bool
	ttyFD int
}

// vnode es lo que encontró una búsqueda de ruta.
type vnode struct {
	synthetic bool        // directorio que solo existe porque cuelga de un montaje
	mount     *mountPoint // nil si es sintético
	rel       string
	virtual   string
}

func newVFS(mounts []*mountPoint) *vfs {
	v := &vfs{files: map[int32]*openFile{}, cwd: "/"}
	v.mounts = slices.Clone(mounts)
	sort.SliceStable(v.mounts, func(i, j int) bool { return len(v.mounts[i].virtual) > len(v.mounts[j].virtual) })
	v.files[0] = &openFile{refs: 1, tty: true, ttyFD: 0, path: "/dev/stdin"}
	v.files[1] = &openFile{refs: 1, tty: true, ttyFD: 1, path: "/dev/stdout"}
	v.files[2] = &openFile{refs: 1, tty: true, ttyFD: 2, path: "/dev/stderr"}
	return v
}

// abs deja p absoluta y limpia contra el directorio actual.
func (v *vfs) abs(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = v.cwd + "/" + p
	}
	return path.Clean(p)
}

// lookup ubica una ruta virtual ya limpia.
func (v *vfs) lookup(clean string) (*vnode, errno) {
	for _, m := range v.mounts {
		if clean == m.virtual {
			return &vnode{mount: m, rel: "", virtual: clean}, errnoOK
		}
		if strings.HasPrefix(clean, m.virtual+"/") {
			return &vnode{mount: m, rel: clean[len(m.virtual)+1:], virtual: clean}, errnoOK
		}
	}
	if clean == "/" {
		return &vnode{synthetic: true, virtual: "/"}, errnoOK
	}
	for _, m := range v.mounts {
		if strings.HasPrefix(m.virtual, clean+"/") {
			return &vnode{synthetic: true, virtual: clean}, errnoOK
		}
	}
	return nil, errnoNoEnt
}

// syntheticEntries lista los hijos de un directorio sintético.
func (v *vfs) syntheticEntries(dir string) []string {
	prefix := dir
	if prefix != "/" {
		prefix += "/"
	}
	seen := map[string]bool{}
	var names []string
	for _, m := range v.mounts {
		if !strings.HasPrefix(m.virtual, prefix) {
			continue
		}
		first, _, _ := strings.Cut(m.virtual[len(prefix):], "/")
		if first != "" && !seen[first] {
			seen[first] = true
			names = append(names, first)
		}
	}
	sort.Strings(names)
	return names
}

func (v *vfs) statNode(n *vnode) (statInfo, errno) {
	if n.synthetic {
		return statInfo{mode: sIFDir | permDir, size: 4096, mtime: time.Unix(1_700_000_000, 0), ino: inoOf("synthetic:" + n.virtual)}, errnoOK
	}
	return n.mount.be.stat(n.rel)
}

func (v *vfs) stat(p string) (statInfo, errno) {
	n, e := v.lookup(v.abs(p))
	if e != errnoOK {
		return statInfo{}, e
	}
	return v.statNode(n)
}

func (v *vfs) readdirNode(n *vnode) ([]string, errno) {
	var names []string
	if n.synthetic {
		names = v.syntheticEntries(n.virtual)
	} else {
		var e errno
		names, e = n.mount.be.readdir(n.rel)
		if e != errnoOK {
			return nil, e
		}
		sort.Strings(names)
	}
	return append([]string{".", ".."}, names...), errnoOK
}

func (v *vfs) allocFD() int32 {
	for fd := int32(0); ; fd++ {
		if _, used := v.files[fd]; !used {
			return fd
		}
	}
}

const maxOpenFiles = 1024

// open abre una ruta y devuelve su descriptor (o un errno).
func (v *vfs) open(p string, flags int) (int32, errno) {
	if len(v.files) >= maxOpenFiles {
		return -1, errnoMFile
	}
	clean := v.abs(p)
	n, e := v.lookup(clean)
	writing := flags&oAccMode != 0 || flags&(oCreat|oTrunc|oAppend) != 0
	if e != errnoOK {
		// Ni siquiera con O_CREAT se crea nada fuera de un montaje: la raíz
		// del árbol virtual es de solo lectura.
		if flags&oCreat != 0 {
			if parent, pe := v.lookup(path.Dir(clean)); pe == errnoOK && parent.mount != nil && !parent.mount.be.readOnly() {
				// El padre es un montaje escribible: crear el archivo.
				return v.openNode(&vnode{mount: parent.mount, rel: relIn(parent, clean), virtual: clean}, flags, false)
			}
			if _, pe := v.lookup(path.Dir(clean)); pe == errnoOK {
				return -1, errnoROFS
			}
		}
		return -1, e
	}
	if n.synthetic {
		if writing {
			return -1, errnoIsDir
		}
		return v.openNode(n, flags, true)
	}
	st, se := n.mount.be.stat(n.rel)
	if se != errnoOK {
		// No existe dentro del montaje.
		if flags&oCreat != 0 {
			if n.mount.be.readOnly() {
				return -1, errnoROFS
			}
			return v.openNode(n, flags, false)
		}
		return -1, se
	}
	if flags&oCreat != 0 && flags&oExcl != 0 {
		return -1, errnoExist
	}
	if st.isDir() {
		if writing {
			return -1, errnoIsDir
		}
		return v.openNode(n, flags, true)
	}
	if flags&oDirectory != 0 {
		return -1, errnoNotDir
	}
	if writing && n.mount.be.readOnly() {
		return -1, errnoROFS
	}
	return v.openNode(n, flags, false)
}

// relIn calcula la ruta relativa al montaje de clean, que cuelga del padre.
func relIn(parent *vnode, clean string) string {
	if parent.rel == "" {
		return path.Base(clean)
	}
	return parent.rel + "/" + path.Base(clean)
}

func (v *vfs) openNode(n *vnode, flags int, dir bool) (int32, errno) {
	of := &openFile{refs: 1, path: n.virtual, flags: flags}
	if dir {
		names, e := v.readdirNode(n)
		if e != errnoOK {
			return -1, e
		}
		of.isDir = true
		of.entries = names
		of.dirNode = n
	} else {
		bf, e := n.mount.be.open(n.rel, flags)
		if e != errnoOK {
			return -1, e
		}
		of.bf = bf
		of.append = flags&oAppend != 0
	}
	fd := v.allocFD()
	v.files[fd] = of
	return fd, errnoOK
}

func (v *vfs) get(fd int32) (*openFile, errno) {
	f, ok := v.files[fd]
	if !ok {
		return nil, errnoBadF
	}
	return f, errnoOK
}

func (v *vfs) dup(fd int32, min int32) (int32, errno) {
	f, e := v.get(fd)
	if e != errnoOK {
		return -1, e
	}
	if len(v.files) >= maxOpenFiles {
		return -1, errnoMFile
	}
	nfd := min
	for {
		if _, used := v.files[nfd]; !used {
			break
		}
		nfd++
	}
	f.refs++
	v.files[nfd] = f
	return nfd, errnoOK
}

func (v *vfs) close(fd int32) errno {
	f, e := v.get(fd)
	if e != errnoOK {
		return e
	}
	delete(v.files, fd)
	f.refs--
	if f.refs <= 0 && f.bf != nil {
		if err := f.bf.close(); err != nil {
			return errnoIO
		}
	}
	return errnoOK
}

// closeAll cierra lo que haya quedado abierto al terminar la ejecución.
func (v *vfs) closeAll() {
	for fd := range v.files {
		_ = v.close(fd)
	}
}

func (v *vfs) fstat(fd int32) (statInfo, errno) {
	f, e := v.get(fd)
	if e != errnoOK {
		return statInfo{}, e
	}
	switch {
	case f.tty:
		return statInfo{mode: sIFChr | permRW, mtime: time.Unix(1_700_000_000, 0), ino: inoOf(f.path)}, errnoOK
	case f.isDir:
		return v.statNode(f.dirNode)
	}
	n, e := v.lookup(f.path)
	if e == errnoOK {
		if st, se := v.statNode(n); se == errnoOK {
			if sz, err := f.bf.size(); err == nil {
				st.size = sz
			}
			return st, errnoOK
		}
	}
	// El archivo se borró mientras estaba abierto: se informa el tamaño.
	sz, _ := f.bf.size()
	return statInfo{mode: sIFReg | permRW, size: sz, mtime: time.Now(), ino: inoOf(f.path)}, errnoOK
}

func (v *vfs) read(f *openFile, p []byte) (int, errno) {
	if f.isDir {
		return 0, errnoIsDir
	}
	if f.tty || f.bf == nil {
		return 0, errnoOK // stdin: sin datos
	}
	n, err := f.bf.readAt(p, f.pos)
	f.pos += int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, errnoIO
	}
	return n, errnoOK
}

func (v *vfs) write(f *openFile, p []byte) (int, errno) {
	if f.isDir {
		return 0, errnoIsDir
	}
	if f.tty || f.bf == nil {
		return 0, errnoBadF
	}
	if f.flags&oAccMode == 0 {
		return 0, errnoBadF // abierto solo para leer
	}
	if f.append {
		sz, err := f.bf.size()
		if err != nil {
			return 0, errnoIO
		}
		f.pos = sz
	}
	n, err := f.bf.writeAt(p, f.pos)
	f.pos += int64(n)
	if err != nil {
		return n, errnoOf(err)
	}
	return n, errnoOK
}

// seek devuelve la nueva posición.
func (v *vfs) seek(f *openFile, off int64, whence int32) (int64, errno) {
	if f.tty {
		return 0, errnoSPipe
	}
	var base int64
	switch whence {
	case 0:
	case 1:
		base = f.pos
	case 2:
		if f.bf != nil {
			sz, err := f.bf.size()
			if err != nil {
				return 0, errnoIO
			}
			base = sz
		}
	default:
		return 0, errnoInval
	}
	if base+off < 0 {
		return 0, errnoInval
	}
	f.pos = base + off
	return f.pos, errnoOK
}

func (v *vfs) mkdir(p string) errno {
	clean := v.abs(p)
	// lookup solo ubica el montaje: que la ruta exista lo dice el stat.
	if n, e := v.lookup(clean); e == errnoOK {
		if _, se := v.statNode(n); se == errnoOK {
			return errnoExist
		}
	}
	parent, e := v.lookup(path.Dir(clean))
	if e != errnoOK {
		return e
	}
	if parent.synthetic || parent.mount.be.readOnly() {
		return errnoROFS
	}
	return parent.mount.be.mkdir(relIn(parent, clean))
}

func (v *vfs) rmdir(p string) errno {
	n, e := v.lookup(v.abs(p))
	if e != errnoOK {
		return e
	}
	if n.synthetic {
		return errnoROFS
	}
	if n.mount.be.readOnly() {
		return errnoROFS
	}
	if n.rel == "" {
		return errnoAccess // la raíz del montaje no se borra
	}
	return n.mount.be.rmdir(n.rel)
}

func (v *vfs) unlink(p string) errno {
	n, e := v.lookup(v.abs(p))
	if e != errnoOK {
		return e
	}
	if n.synthetic {
		return errnoIsDir
	}
	if n.mount.be.readOnly() {
		return errnoROFS
	}
	if n.rel == "" {
		return errnoIsDir
	}
	return n.mount.be.unlink(n.rel)
}

func (v *vfs) rename(from, to string) errno {
	a, e := v.lookup(v.abs(from))
	if e != errnoOK {
		return e
	}
	cleanTo := v.abs(to)
	var b *vnode
	if bn, be := v.lookup(cleanTo); be == errnoOK {
		b = bn
	} else {
		parent, pe := v.lookup(path.Dir(cleanTo))
		if pe != errnoOK {
			return pe
		}
		if parent.synthetic {
			return errnoROFS
		}
		b = &vnode{mount: parent.mount, rel: relIn(parent, cleanTo), virtual: cleanTo}
	}
	if a.synthetic || b.synthetic {
		return errnoROFS
	}
	if a.mount != b.mount {
		return errnoXDev
	}
	if a.mount.be.readOnly() {
		return errnoROFS
	}
	if a.rel == "" || b.rel == "" {
		return errnoAccess
	}
	return a.mount.be.rename(a.rel, b.rel)
}

// chdir cambia el directorio actual; tiene que existir y ser un directorio.
func (v *vfs) chdir(p string) errno {
	clean := v.abs(p)
	n, e := v.lookup(clean)
	if e != errnoOK {
		return e
	}
	st, e := v.statNode(n)
	if e != errnoOK {
		return e
	}
	if !st.isDir() {
		return errnoNotDir
	}
	v.cwd = clean
	return errnoOK
}

// access comprueba existencia y, si se pide escribir, que el montaje lo permita.
func (v *vfs) access(p string, amode int32) errno {
	n, e := v.lookup(v.abs(p))
	if e != errnoOK {
		return e
	}
	if _, e := v.statNode(n); e != errnoOK {
		return e
	}
	if amode&2 != 0 && (n.synthetic || n.mount.be.readOnly()) {
		return errnoROFS
	}
	return errnoOK
}
