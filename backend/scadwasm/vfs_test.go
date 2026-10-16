package scadwasm

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// newTestVFS: /work (solo lectura) con a.txt y sub/b.txt, /out escribible,
// /lib/0 y /lib/1 de solo lectura, /cache en memoria y /fonts embebido.
func newTestVFS(t *testing.T) (*vfs, string, string) {
	t.Helper()
	ws, out := t.TempDir(), t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(ws, "sub"), 0o755))
	must(os.WriteFile(filepath.Join(ws, "a.txt"), []byte("hola"), 0o644))
	must(os.WriteFile(filepath.Join(ws, "sub", "b.txt"), []byte("b"), 0o644))
	lib0, lib1 := t.TempDir(), t.TempDir()
	p, err := newProc(RunSpec{Cwd: "/work", Mounts: []Mount{
		{Host: ws, Virtual: "/work", ReadOnly: true},
		{Host: out, Virtual: "/out"},
		{Host: lib0, Virtual: "/lib/0", ReadOnly: true},
		{Host: lib1, Virtual: "/lib/1", ReadOnly: true},
	}})
	must(err)
	return p.fs, ws, out
}

func TestVFSLookupAndStat(t *testing.T) {
	v, _, _ := newTestVFS(t)
	for path, want := range map[string]errno{
		"/":              errnoOK,
		"/work":          errnoOK,
		"/work/a.txt":    errnoOK,
		"/work/sub":      errnoOK,
		"/work/nope":     errnoNoEnt,
		"/lib":           errnoOK, // directorio sintético
		"/lib/0":         errnoOK,
		"/lib/2":         errnoNoEnt,
		"/etc":           errnoNoEnt,
		"/etc/passwd":    errnoNoEnt,
		"/work/../etc":   errnoNoEnt, // .. se resuelve en el árbol virtual
		"/work/../out":   errnoOK,
		"/work/../../..": errnoOK, // la raíz virtual, no la del disco
		"/fonts":         errnoOK,
		"/cache":         errnoOK,
		"a.txt":          errnoOK, // relativo al cwd (/work)
		"../out":         errnoOK,
	} {
		if _, e := v.stat(path); e != want {
			t.Errorf("stat(%q) = %d, quería %d", path, e, want)
		}
	}
	if st, _ := v.stat("/work/a.txt"); st.size != 4 || st.isDir() {
		t.Errorf("a.txt: %+v", st)
	}
	if st, _ := v.stat("/lib"); !st.isDir() {
		t.Error("/lib es un directorio")
	}
}

func TestVFSReadOnlyAndWritable(t *testing.T) {
	v, ws, out := newTestVFS(t)
	// Solo lectura: nada de crear, borrar, renombrar ni abrir para escribir.
	if _, e := v.open("/work/nuevo.txt", oCreat|oWrOnly); e != errnoROFS {
		t.Errorf("crear en solo lectura = %d", e)
	}
	if _, e := v.open("/work/a.txt", oWrOnly); e != errnoROFS {
		t.Errorf("abrir para escribir = %d", e)
	}
	if e := v.mkdir("/work/d"); e != errnoROFS {
		t.Errorf("mkdir = %d", e)
	}
	if e := v.unlink("/work/a.txt"); e != errnoROFS {
		t.Errorf("unlink = %d", e)
	}
	if e := v.rmdir("/work/sub"); e != errnoROFS {
		t.Errorf("rmdir = %d", e)
	}
	if e := v.rename("/work/a.txt", "/work/c.txt"); e != errnoROFS {
		t.Errorf("rename = %d", e)
	}
	if e := v.access("/work/a.txt", 2); e != errnoROFS {
		t.Errorf("access(W_OK) = %d", e)
	}
	if e := v.access("/work/a.txt", 4); e != errnoOK {
		t.Errorf("access(R_OK) = %d", e)
	}
	// La raíz sintética tampoco se escribe.
	if _, e := v.open("/suelto.txt", oCreat|oWrOnly); e != errnoROFS {
		t.Errorf("crear en la raíz = %d", e)
	}
	if e := v.mkdir("/otra"); e != errnoROFS {
		t.Errorf("mkdir en la raíz = %d", e)
	}
	if _, err := os.Stat(filepath.Join(ws, "nuevo.txt")); err == nil {
		t.Error("se creó un archivo en el montaje de solo lectura")
	}

	// Escribible: crear, escribir, leer, renombrar, borrar.
	fd, e := v.open("/out/x.txt", oCreat|oWrOnly|oTrunc)
	if e != errnoOK {
		t.Fatalf("crear en /out = %d", e)
	}
	f, _ := v.get(fd)
	if n, e := v.write(f, []byte("datos")); n != 5 || e != errnoOK {
		t.Fatalf("write = %d, %d", n, e)
	}
	if e := v.close(fd); e != errnoOK {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "x.txt")); string(b) != "datos" {
		t.Errorf("contenido en disco: %q", b)
	}
	if e := v.mkdir("/out/d"); e != errnoOK {
		t.Errorf("mkdir /out/d = %d", e)
	}
	if e := v.mkdir("/out/d"); e != errnoExist {
		t.Errorf("mkdir repetido = %d", e)
	}
	if e := v.rename("/out/x.txt", "/out/d/y.txt"); e != errnoOK {
		t.Errorf("rename = %d", e)
	}
	if e := v.rename("/out/d/y.txt", "/work/z.txt"); e != errnoROFS && e != errnoXDev {
		t.Errorf("rename entre montajes = %d", e)
	}
	if e := v.rmdir("/out/d"); e != errnoNotEmpty {
		t.Errorf("rmdir con contenido = %d", e)
	}
	if e := v.unlink("/out/d/y.txt"); e != errnoOK {
		t.Errorf("unlink = %d", e)
	}
	if e := v.rmdir("/out/d"); e != errnoOK {
		t.Errorf("rmdir = %d", e)
	}
	if _, e := v.open("/out/x.txt", oCreat|oExcl|oWrOnly); e != errnoOK {
		t.Errorf("O_EXCL sobre uno que ya no existe = %d", e)
	}
	if _, e := v.open("/out/x.txt", oCreat|oExcl|oWrOnly); e != errnoExist {
		t.Errorf("O_EXCL sobre uno que existe = %d", e)
	}
}

func TestVFSReadDirAndDescriptors(t *testing.T) {
	v, _, _ := newTestVFS(t)
	root, _ := v.lookup("/")
	got, _ := v.readdirNode(root)
	if want := []string{".", "..", "cache", "fonts", "lib", "out", "work"}; !reflect.DeepEqual(got, want) {
		t.Errorf("raíz = %v, quería %v", got, want)
	}
	lib, _ := v.lookup("/lib")
	got, _ = v.readdirNode(lib)
	if want := []string{".", "..", "0", "1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("/lib = %v", got)
	}
	work, _ := v.lookup("/work")
	got, _ = v.readdirNode(work)
	if want := []string{".", "..", "a.txt", "sub"}; !reflect.DeepEqual(got, want) {
		t.Errorf("/work = %v", got)
	}

	// Descriptores: el más bajo libre, dup comparte la posición, close los libera.
	fd1, _ := v.open("/work/a.txt", 0)
	if fd1 != 3 {
		t.Errorf("primer descriptor = %d, quería 3 (0-2 son stdio)", fd1)
	}
	fd2, e := v.dup(fd1, 0)
	if e != errnoOK || fd2 != 4 {
		t.Fatalf("dup = %d, %d", fd2, e)
	}
	f1, _ := v.get(fd1)
	buf := make([]byte, 2)
	v.read(f1, buf)
	f2, _ := v.get(fd2)
	v.read(f2, buf)
	if string(buf) != "la" {
		t.Errorf("dup debe compartir la posición: %q", buf)
	}
	v.close(fd1)
	if _, e := v.get(fd1); e != errnoBadF {
		t.Error("el descriptor cerrado sigue abierto")
	}
	if e := v.close(fd1); e != errnoBadF {
		t.Errorf("cerrar dos veces = %d", e)
	}
	if fd3, _ := v.open("/work/a.txt", 0); fd3 != 3 {
		t.Errorf("el descriptor 3 se reusa: %d", fd3)
	}
	// stdio no se puede buscar.
	if _, e := v.seek(v.files[1], 0, 0); e != errnoSPipe {
		t.Errorf("seek en stdout = %d", e)
	}
	if _, e := v.open("/work/sub", oDirectory); e != errnoOK {
		t.Errorf("abrir un directorio con O_DIRECTORY = %d", e)
	}
	if _, e := v.open("/work/a.txt", oDirectory); e != errnoNotDir {
		t.Errorf("O_DIRECTORY sobre un archivo = %d", e)
	}
	if e := v.chdir("/work/a.txt"); e != errnoNotDir {
		t.Errorf("chdir a un archivo = %d", e)
	}
	if e := v.chdir("/out"); e != errnoOK || v.cwd != "/out" {
		t.Errorf("chdir = %d cwd=%s", e, v.cwd)
	}
}

func TestFontsAndCacheMounts(t *testing.T) {
	v, _, _ := newTestVFS(t)
	if st, e := v.stat("/fonts/LiberationSans-Regular.ttf"); e != errnoOK || st.size < 100_000 {
		t.Errorf("fuente embebida: %+v %d", st, e)
	}
	if _, e := v.stat("/fonts/fonts.conf"); e != errnoOK {
		t.Errorf("fonts.conf: %d", e)
	}
	if _, e := v.open("/fonts/nueva.ttf", oCreat|oWrOnly); e != errnoROFS {
		t.Errorf("las fuentes son de solo lectura: %d", e)
	}
	// /cache es memoria y se puede escribir.
	if e := v.mkdir("/cache/fontconfig"); e != errnoOK {
		t.Fatalf("mkdir /cache/fontconfig = %d", e)
	}
	fd, e := v.open("/cache/fontconfig/c.cache", oCreat|oRdWr)
	if e != errnoOK {
		t.Fatalf("crear en /cache = %d", e)
	}
	f, _ := v.get(fd)
	v.write(f, []byte("xyz"))
	v.seek(f, 0, 0)
	buf := make([]byte, 3)
	if n, _ := v.read(f, buf); n != 3 || string(buf) != "xyz" {
		t.Errorf("lectura de memoria: %q", buf)
	}
}

func TestErrnoMapping(t *testing.T) {
	if errnoOf(nil) != errnoOK {
		t.Error("nil")
	}
	_, err := os.Stat(filepath.Join(t.TempDir(), "no"))
	if errnoOf(err) != errnoNoEnt {
		t.Errorf("ENOENT = %d", errnoOf(err))
	}
	d := t.TempDir()
	if errnoOf(os.Mkdir(d, 0o755)) != errnoExist {
		t.Error("EEXIST")
	}
}

func TestHostSlotsFindsImportedFunctionsInTheTable(t *testing.T) {
	data, err := loadWasm()
	if err != nil {
		t.Fatal(err)
	}
	slots, err := hostSlots(data)
	if err != nil {
		t.Fatal(err)
	}
	// Este build guarda 4 funciones del host en la tabla indirecta (exit,
	// __cxa_throw, __cxa_rethrow y __cxa_end_catch).
	if len(slots) != 4 {
		t.Errorf("hostSlots = %v", slots)
	}
	if _, err := hostSlots([]byte("no es wasm")); err == nil {
		t.Error("un archivo que no es wasm tiene que dar error")
	}
}
