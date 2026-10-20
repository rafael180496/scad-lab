package render

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"scadlab/backend/i18n"
)

// fakeOpenSCAD crea un `openscad` falso (script sh) que se comporta según el
// contenido del .scad:
//
//	FAIL  → ERROR de parser en la línea 3 y sale con 1
//	EMPTY → aviso de modelo vacío, sale con 0 sin escribir el STL
//	SLOW  → avisa que arrancó (archivo started) y duerme
//	WARN  → además del STL, un WARNING con línea
//	otro  → STL binario mínimo (cabecera + 0 triángulos) en la ruta de -o
//
// Cada render (no --version) agrega una línea a `runs`, con los argumentos,
// para contar ejecuciones y ver qué opciones recibió. Con rejectBackend
// rechaza --backend como una versión vieja.
type fake struct {
	bin, runs, started string
}

func newFake(t *testing.T, rejectBackend bool) fake {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("el openscad falso es un script sh")
	}
	dir := t.TempDir()
	f := fake{
		bin:     filepath.Join(dir, "openscad"),
		runs:    filepath.Join(dir, "runs"),
		started: filepath.Join(dir, "started"),
	}
	reject := ""
	if rejectBackend {
		reject = `echo "unknown option '$1'" >&2; exit 1`
	} else {
		reject = `:`
	}
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "OpenSCAD version 2025.03.15" >&2; exit 0; fi
echo "$*" >> '` + f.runs + `'
out=""; file=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    --export-format) shift 2 ;;
    --backend=*) ` + reject + `; shift ;;
    *) file="$1"; shift ;;
  esac
done
if grep -q FAIL "$file"; then
  echo 'ERROR: Parser error in file "'"$file"'", line 3: syntax error' >&2
  exit 1
fi
if grep -q EMPTY "$file"; then
  echo "Current top level object is empty." >&2
  exit 0
fi
if grep -q SLOW "$file"; then
  touch '` + f.started + `'
  exec sleep 20
fi
if grep -q WARN "$file"; then
  echo "WARNING: Ignoring unknown variable 'w', in file $file, line 2." >&2
fi
head -c 84 /dev/zero > "$out"
exit 0
`
	if err := os.WriteFile(f.bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fake) runCount(t *testing.T) int {
	b, err := os.ReadFile(f.runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

type fixture struct {
	t   *testing.T
	ws  string
	eng *Engine
	f   fake
}

func newFixture(t *testing.T, rejectBackend bool) *fixture {
	f := newFake(t, rejectBackend)
	eng, err := NewEngine(filepath.Join(t.TempDir(), "cache", "meshes"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	return &fixture{t: t, ws: t.TempDir(), eng: eng, f: f}
}

func (x *fixture) write(name, body string) {
	x.t.Helper()
	p := filepath.Join(x.ws, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		x.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		x.t.Fatal(err)
	}
}

func (x *fixture) render(name string, opts Options) Result {
	x.t.Helper()
	if opts.OpenSCADPath == "" {
		opts.OpenSCADPath = x.f.bin
	}
	r, err := x.eng.Render(context.Background(), Request{WorkspaceKey: "ws1", WorkspaceDir: x.ws, FileName: name}, opts)
	if err != nil {
		x.t.Fatal(err)
	}
	return r
}

func TestRenderOKAndCacheHit(t *testing.T) {
	x := newFixture(t, false)
	x.write("001-box.scad", "// @checkpoint parent= desc=\"caja\"\n// WARN\ncube(10);\n")

	r := x.render("001-box.scad", Options{Backend: "manifold"})
	if r.Status != StatusOK || r.CacheHit || r.Backend != "manifold" || r.Version != "2025.03.15" {
		t.Fatalf("primer render: %+v", r)
	}
	if st, err := os.Stat(r.MeshPath); err != nil || st.Size() != 84 {
		t.Fatalf("malla: %v %v", st, err)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Line != 2 || r.Diagnostics[0].File != "" {
		t.Fatalf("warning esperado en línea 2: %+v", r.Diagnostics)
	}
	if b, _ := os.ReadFile(x.f.runs); !strings.Contains(string(b), "--backend=manifold") || !strings.Contains(string(b), "--export-format binstl") {
		t.Errorf("argumentos: %s", b)
	}

	r2 := x.render("001-box.scad", Options{Backend: "manifold"})
	if r2.Status != StatusOK || !r2.CacheHit || r2.MeshPath != r.MeshPath || len(r2.Diagnostics) != 1 {
		t.Fatalf("segundo render debía ser hit con los warnings: %+v", r2)
	}
	if n := x.f.runCount(t); n != 1 {
		t.Errorf("el hit ejecutó OpenSCAD: %d corridas", n)
	}

	// Otro backend es otra malla: no puede salir de la caché de manifold.
	if r3 := x.render("001-box.scad", Options{Backend: "cgal"}); r3.CacheHit || r3.Backend != "cgal" {
		t.Errorf("cgal reusó la malla de manifold: %+v", r3)
	}
}

func TestRenderErrorWithLine(t *testing.T) {
	x := newFixture(t, false)
	x.write("002-bad.scad", "// @checkpoint parent=001-box.scad desc=\"x\"\n// FAIL\ncube(10\n")
	r := x.render("002-bad.scad", Options{Backend: "manifold"})
	if r.Status != StatusError || r.MeshPath != "" {
		t.Fatalf("esperaba error sin malla: %+v", r)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Line != 3 || r.Diagnostics[0].Severity != SeverityError || r.Diagnostics[0].File != "" {
		t.Fatalf("diagnóstico: %+v", r.Diagnostics)
	}
	if !strings.Contains(r.Log, "syntax error") {
		t.Errorf("log: %q", r.Log)
	}
	// Un error no se cachea: el segundo intento vuelve a correr.
	x.render("002-bad.scad", Options{Backend: "manifold"})
	if n := x.f.runCount(t); n != 2 {
		t.Errorf("corridas = %d, esperaba 2", n)
	}
}

func TestRenderEmptyModel(t *testing.T) {
	x := newFixture(t, false)
	x.write("003-empty.scad", "// EMPTY\nmodule m() cube(1);\n")
	r := x.render("003-empty.scad", Options{})
	if r.Status != StatusError || len(r.Diagnostics) == 0 || r.Diagnostics[len(r.Diagnostics)-1].Severity != SeverityError {
		t.Fatalf("modelo vacío: %+v", r)
	}
}

func TestRenderMissing(t *testing.T) {
	eng, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	r, err := eng.Render(context.Background(), Request{WorkspaceKey: "k", WorkspaceDir: t.TempDir(), FileName: "001-x.scad"}, Options{})
	if err != nil || r.Status != StatusMissing || r.FileName != "001-x.scad" || r.Diagnostics == nil {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCacheInvalidatedByInclude(t *testing.T) {
	x := newFixture(t, false)
	x.write("lib/parts.scad", "module part() cube(1);\n")
	x.write("sub/shared.scad", "use <../lib/parts.scad>\n")
	x.write("001-a.scad", "include <sub/shared.scad>\n// include <ignored.scad>\npart();\n")

	if r := x.render("001-a.scad", Options{}); r.Status != StatusOK {
		t.Fatal(r)
	}
	if r := x.render("001-a.scad", Options{}); !r.CacheHit {
		t.Fatal("esperaba hit")
	}
	// Cambia un archivo que se alcanza solo por un use<> dentro de un include.
	x.write("lib/parts.scad", "module part() cube(2);\n")
	if r := x.render("001-a.scad", Options{}); r.CacheHit {
		t.Fatal("la caché no se invalidó al cambiar un include transitivo")
	}
	if n := x.f.runCount(t); n != 2 {
		t.Errorf("corridas = %d, esperaba 2", n)
	}
	// Un archivo comentado no entra: crearlo no invalida.
	x.write("ignored.scad", "cube(3);\n")
	if r := x.render("001-a.scad", Options{}); !r.CacheHit {
		t.Error("un include comentado invalidó la caché")
	}
}

func TestNewRenderCancelsPrevious(t *testing.T) {
	x := newFixture(t, false)
	x.write("001-slow.scad", "// SLOW\n")
	x.write("002-fast.scad", "cube(1);\n")

	done := make(chan Result, 1)
	go func() { done <- x.render("001-slow.scad", Options{}) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(x.f.started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("el render lento no arrancó")
		}
		time.Sleep(20 * time.Millisecond)
	}

	r2 := x.render("002-fast.scad", Options{})
	if r2.Status != StatusOK {
		t.Fatalf("el render nuevo: %+v", r2)
	}
	select {
	case r1 := <-done:
		if r1.Status != StatusCancelled {
			t.Fatalf("el anterior debía quedar cancelado: %+v", r1)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("el render cancelado no volvió")
	}
}

func TestCancelAndTimeout(t *testing.T) {
	x := newFixture(t, false)
	x.write("001-slow.scad", "// SLOW\n")

	done := make(chan Result, 1)
	go func() { done <- x.render("001-slow.scad", Options{}) }()
	for i := 0; i < 250; i++ {
		if _, err := os.Stat(x.f.started); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	x.eng.Cancel("ws1")
	if r := <-done; r.Status != StatusCancelled {
		t.Fatalf("Cancel: %+v", r)
	}

	i18n.SetLang("es")
	defer i18n.SetLang("en")
	r := x.render("001-slow.scad", Options{TimeoutSeconds: 1})
	if r.Status != StatusError || len(r.Diagnostics) == 0 || !strings.Contains(r.Diagnostics[len(r.Diagnostics)-1].Message, "tiempo máximo") {
		t.Fatalf("timeout: %+v", r)
	}
}

func TestManifoldFallsBackToCGAL(t *testing.T) {
	x := newFixture(t, true)
	x.write("001-a.scad", "cube(1);\n")
	r := x.render("001-a.scad", Options{Backend: "manifold"})
	if r.Status != StatusOK || r.Backend != "cgal" {
		t.Fatalf("esperaba caída a cgal: %+v", r)
	}
	if n := x.f.runCount(t); n != 2 {
		t.Fatalf("corridas = %d, esperaba 2 (rechazo + reintento)", n)
	}
	// Recordado: el siguiente ni intenta --backend.
	x.write("002-b.scad", "cube(2);\n")
	if r := x.render("002-b.scad", Options{Backend: "manifold"}); r.Status != StatusOK || r.Backend != "cgal" {
		t.Fatalf("%+v", r)
	}
	if n := x.f.runCount(t); n != 3 {
		t.Errorf("corridas = %d, esperaba 3", n)
	}
	// Y el primero sale de la caché aunque se pida manifold.
	if r := x.render("001-a.scad", Options{Backend: "manifold"}); !r.CacheHit {
		t.Errorf("tras la caída, la malla cgal no se reusó: %+v", r)
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	e, err := newEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.maxCacheBytes = 250
	old := time.Now().Add(-time.Hour)
	keys := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	for i, k := range keys {
		_ = os.WriteFile(e.meshPath(k), make([]byte, 100), 0o644)
		_ = os.WriteFile(e.metaPath(k), []byte("{}"), 0o644)
		ts := old.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(e.meshPath(k), ts, ts)
		_ = os.Chtimes(e.metaPath(k), ts, ts)
	}
	stale := filepath.Join(dir, "deadbeef-123.stl")
	_ = os.WriteFile(stale, []byte("x"), 0o644)
	_ = os.Chtimes(stale, old.Add(-3*time.Hour), old.Add(-3*time.Hour))

	e.prune()
	if _, err := os.Stat(e.meshPath(keys[0])); !os.IsNotExist(err) {
		t.Error("la entrada más vieja debía borrarse")
	}
	for _, k := range keys[1:] {
		if _, err := os.Stat(e.meshPath(k)); err != nil {
			t.Errorf("se borró %s de más", k[:4])
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("el temporal huérfano debía borrarse")
	}
}

func TestWriteAndClearRenderErrors(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	dir := t.TempDir()
	src := "// @checkpoint parent=001-a.scad desc=\"x\"\nw = 2;\nmodule m() {\n  cube(10\n}\nm();\nx();\n// fin\n"
	if err := os.WriteFile(filepath.Join(dir, "002-b.scad"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Result{FileName: "002-b.scad", Status: StatusError, Backend: "manifold", Version: "2025.03.15", Diagnostics: []Diagnostic{
		{Severity: SeverityError, Line: 4, Message: "Parser error: syntax error"},
		{Severity: SeverityWarning, Line: 7, Message: "Ignoring unknown module 'x'"},
	}}
	if err := WriteRenderErrors(dir, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "RENDER_ERRORS.md"))
	if err != nil {
		t.Fatal(err)
	}
	md := string(b)
	for _, want := range []string{
		"# Errores de render — 002-b.scad",
		"`002-b.scad`, línea 4",
		"> 4 |   cube(10",
		"  1 | // @checkpoint", // contexto -3
		"  7 | x();",           // contexto +3
		"Ignoring unknown module 'x'",
		"`update_checkpoint` (file=`002-b.scad`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("falta %q en:\n%s", want, md)
		}
	}
	if strings.Contains(md, "// fin") {
		t.Error("el contexto pasó de ±3 líneas")
	}

	if err := ClearRenderErrors(dir); err != nil {
		t.Fatal(err)
	}
	if err := ClearRenderErrors(dir); err != nil {
		t.Fatalf("borrar lo que no existe no es error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "RENDER_ERRORS.md")); !os.IsNotExist(err) {
		t.Error("no se borró")
	}
}

// La clave de caché ignora la línea `// @checkpoint …`: re-colgar un checkpoint
// (mandar su padre a la papelera) reescribe esa línea y no debe invalidar su
// render. El resto del archivo sí cuenta, incluida la segunda línea.
func TestContentHashIgnoresCheckpointHeader(t *testing.T) {
	dir := t.TempDir()
	h := func(src string) string { return contentHash(dir, "002-x.scad", []byte(src)) }
	a := h("// @checkpoint parent=001-a.scad desc=\"x\"\ncube(1);\n")
	if b := h("// @checkpoint parent= desc=\"x\"\ncube(1);\n"); a != b {
		t.Error("cambiar parent= no debe cambiar la clave")
	}
	// Con BOM y CRLF en la cabecera: es la misma línea, y lo de abajo igual.
	if b := h("\xEF\xBB\xBF// @checkpoint parent=009-z.scad desc=\"otra\"\r\ncube(1);\n"); a != b {
		t.Error("el BOM y el CRLF de la cabecera no cuentan")
	}
	if b := h("// @checkpoint parent=001-a.scad desc=\"x\"\ncube(2);\n"); a == b {
		t.Error("cambiar el código debe cambiar la clave")
	}
	if b := h("cube(1);\n"); a == b {
		t.Error("sin cabecera es otro contenido")
	}
	if b := h("// otro comentario\ncube(1);\n"); h("// distinto comentario\ncube(1);\n") == b {
		t.Error("solo la línea @checkpoint se ignora")
	}
}
