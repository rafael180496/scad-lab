package pty

import (
	"encoding/base64"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder junta los eventos que el Manager emitiría hacia el frontend.
type recorder struct {
	mu     sync.Mutex
	data   map[string][]byte
	chunks []DataEvent
	exits  map[string]int
	exited chan string
}

func newRecorder() *recorder {
	return &recorder{data: map[string][]byte{}, exits: map[string]int{}, exited: make(chan string, 8)}
}

func (r *recorder) emit(event string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch ev := payload.(type) {
	case DataEvent:
		b, _ := base64.StdEncoding.DecodeString(ev.Data)
		r.data[ev.ID] = append(r.data[ev.ID], b...)
		r.chunks = append(r.chunks, ev)
	case ExitEvent:
		r.exits[ev.ID] = ev.Code
		r.exited <- ev.ID
	}
}

func (r *recorder) output(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.data[id])
}

func (r *recorder) waitExit(t *testing.T, id string) int {
	t.Helper()
	select {
	case got := <-r.exited:
		if got != id {
			t.Fatalf("salió %q, se esperaba %q", got, id)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("la terminal %q no terminó a tiempo; salida: %q", id, r.output(id))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.exits[id]
}

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("usa /bin/sh")
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no se cumplió a tiempo: %s", what)
}

// La prueba real: un proceso de verdad sobre un PTY de verdad, y su salida
// llega por los eventos.
func TestOpenRunsRealProcessAndReadsOutput(t *testing.T) {
	skipWindows(t)
	rec := newRecorder()
	m := NewManager(rec.emit)
	dir := t.TempDir()
	if err := m.Open("t1", dir, []string{"/bin/sh", "-c", "echo hola"}, Environ(os.Environ(), nil), 80, 24); err != nil {
		t.Fatal(err)
	}
	if code := rec.waitExit(t, "t1"); code != 0 {
		t.Errorf("código de salida %d, se esperaba 0", code)
	}
	if out := rec.output("t1"); !strings.Contains(out, "hola") {
		t.Errorf("salida %q: no contiene «hola»", out)
	}
	if m.Has("t1") || len(m.List()) != 0 {
		t.Errorf("la terminal terminada sigue en la lista: %+v", m.List())
	}
}

func TestExitCodeAndWorkingDir(t *testing.T) {
	skipWindows(t)
	rec := newRecorder()
	m := NewManager(rec.emit)
	dir := t.TempDir()
	if err := m.Open("t1", dir, []string{"/bin/sh", "-c", "pwd; exit 3"}, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	if code := rec.waitExit(t, "t1"); code != 3 {
		t.Errorf("código %d, se esperaba 3", code)
	}
	// macOS resuelve /var → /private/var: se compara por el nombre final.
	if out := rec.output("t1"); !strings.Contains(out, dir[strings.LastIndex(dir, "/")+1:]) {
		t.Errorf("el proceso no corrió en %s: %q", dir, out)
	}
}

// Write llega al stdin del proceso, y Snapshot + Seq permiten reengancharse
// sin perder ni duplicar bytes.
func TestWriteAndSnapshot(t *testing.T) {
	skipWindows(t)
	rec := newRecorder()
	m := NewManager(rec.emit)
	if err := m.Open("t1", t.TempDir(), []string{"/bin/sh", "-c", "echo listo; read x; echo recibido:$x"}, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "el prompt inicial", func() bool { return strings.Contains(rec.output("t1"), "listo") })

	snap, err := m.Snapshot("t1")
	if err != nil || !strings.Contains(string(snap.Data), "listo") {
		t.Fatalf("snapshot: %q, %v", snap.Data, err)
	}
	if snap.Seq != int64(len(rec.output("t1"))) && snap.Seq < int64(len(snap.Data)) {
		t.Errorf("Seq %d inconsistente con %d bytes", snap.Seq, len(snap.Data))
	}

	if err := m.Write("t1", "abc\r"); err != nil {
		t.Fatal(err)
	}
	rec.waitExit(t, "t1")
	if out := rec.output("t1"); !strings.Contains(out, "recibido:abc") {
		t.Errorf("el stdin no llegó al proceso: %q", out)
	}
	// Los trozos son contiguos: cada Seq empieza donde terminó el anterior.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var next int64
	for _, c := range rec.chunks {
		b, _ := base64.StdEncoding.DecodeString(c.Data)
		if c.Seq != next {
			t.Fatalf("hueco en Seq: %d, se esperaba %d", c.Seq, next)
		}
		next += int64(len(b))
	}
}

func TestResizeReachesTheProcess(t *testing.T) {
	skipWindows(t)
	rec := newRecorder()
	m := NewManager(rec.emit)
	if err := m.Open("t1", t.TempDir(), []string{"/bin/sh", "-c", "sleep 0.5; stty size"}, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := m.Resize("t1", 101, 31); err != nil {
		t.Fatal(err)
	}
	if got := m.List(); len(got) != 1 || got[0].Cols != 101 || got[0].Rows != 31 {
		t.Errorf("List no refleja el tamaño: %+v", got)
	}
	rec.waitExit(t, "t1")
	if out := rec.output("t1"); !strings.Contains(out, "31 101") {
		t.Errorf("stty size = %q, se esperaba «31 101»", out)
	}
}

func TestCloseKillsProcessAndFreesSlot(t *testing.T) {
	skipWindows(t)
	rec := newRecorder()
	m := NewManager(rec.emit)
	if err := m.Open("t1", t.TempDir(), []string{"/bin/sh", "-c", "sleep 60"}, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	if !m.Has("t1") {
		t.Fatal("la terminal debería estar abierta")
	}
	if err := m.Close("t1"); err != nil {
		t.Fatal(err)
	}
	if m.Has("t1") {
		t.Error("Close debe liberar el lugar enseguida")
	}
	rec.waitExit(t, "t1")
	if err := m.Close("t1"); err != nil {
		t.Errorf("Close tiene que ser idempotente: %v", err)
	}
	if err := m.Write("t1", "x"); err == nil {
		t.Error("escribir en una terminal cerrada debe fallar")
	}
}

func TestCloseAll(t *testing.T) {
	skipWindows(t)
	rec := newRecorder()
	m := NewManager(rec.emit)
	for _, id := range []string{"a", "b"} {
		if err := m.Open(id, t.TempDir(), []string{"/bin/sh", "-c", "sleep 60"}, nil, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	m.CloseAll()
	for range 2 {
		select {
		case <-rec.exited:
		case <-time.After(10 * time.Second):
			t.Fatal("CloseAll no terminó todos los procesos")
		}
	}
}

func TestOpenValidation(t *testing.T) {
	skipWindows(t)
	m := NewManager(nil)
	dir := t.TempDir()
	cases := []struct {
		name string
		id   string
		cwd  string
		argv []string
	}{
		{"sin id", "", dir, []string{"/bin/sh"}},
		{"sin comando", "t", dir, nil},
		{"carpeta inexistente", "t", dir + "/nope", []string{"/bin/sh"}},
		{"ejecutable inexistente", "t", dir, []string{dir + "/no-existe"}},
	}
	for _, c := range cases {
		if err := m.Open(c.id, c.cwd, c.argv, nil, 80, 24); err == nil {
			t.Errorf("%s: debía fallar", c.name)
		}
	}
	if len(m.List()) != 0 {
		t.Errorf("un Open fallido no debe dejar terminales: %+v", m.List())
	}
	// El id reservado por un Open fallido queda libre.
	if err := m.Open("t", dir, []string{"/bin/sh", "-c", "exit 0"}, nil, 80, 24); err != nil {
		t.Errorf("reabrir tras un fallo: %v", err)
	}
	if err := m.Open("t", dir, []string{"/bin/sh", "-c", "sleep 1"}, nil, 80, 24); err == nil {
		m.CloseAll()
	}
}

func TestRingKeepsOnlyTheTail(t *testing.T) {
	skipWindows(t)
	rec := newRecorder()
	m := NewManager(rec.emit)
	m.cap = 1024
	// ~30 KB de salida: bastante más que 2×cap.
	if err := m.Open("t1", t.TempDir(), []string{"/bin/sh", "-c", "i=0; while [ $i -lt 600 ]; do echo linea-$i-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done; sleep 1"}, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "toda la salida", func() bool { return strings.Contains(rec.output("t1"), "linea-599-") })
	snap, err := m.Snapshot("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Data) > 1024 {
		t.Errorf("el snapshot pesa %d bytes, tope 1024", len(snap.Data))
	}
	if !strings.Contains(string(snap.Data), "linea-599-") || strings.Contains(string(snap.Data), "linea-0-") {
		t.Errorf("el snapshot debe tener el final y no el principio")
	}
	if snap.Seq != int64(len(rec.output("t1"))) {
		t.Errorf("Seq %d, bytes emitidos %d", snap.Seq, len(rec.output("t1")))
	}
	m.CloseAll()
}

func TestEnviron(t *testing.T) {
	extra := t.TempDir()
	base := []string{"PATH=/usr/bin:/bin", "TERM=dumb", "COLORTERM=no", "HOME=/h", "NO_EQUAL"}
	got := Environ(base, []string{extra, "/no/existe/seguro", "/usr/bin"})
	env := map[string]string{}
	for _, kv := range got {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	if env["TERM"] != "xterm-256color" || env["COLORTERM"] != "truecolor" {
		t.Errorf("TERM/COLORTERM: %v", env)
	}
	if env["HOME"] != "/h" {
		t.Errorf("no conserva el resto del entorno: %v", env)
	}
	want := "/usr/bin" + string(os.PathListSeparator) + "/bin" + string(os.PathListSeparator) + extra
	if env["PATH"] != want {
		t.Errorf("PATH = %q, se esperaba %q", env["PATH"], want)
	}
	for _, kv := range got {
		if kv == "NO_EQUAL" {
			t.Error("una entrada sin = no es una variable")
		}
	}
	if n := strings.Count(strings.Join(got, "\n"), "TERM="); n < 1 {
		t.Error("falta TERM")
	}
}

func TestEnvironKeepsWindowsPathName(t *testing.T) {
	got := Environ([]string{`Path=C:\Windows`}, nil)
	if !slices.ContainsFunc(got, func(kv string) bool { return strings.HasPrefix(kv, "Path=") }) {
		t.Errorf("tiene que respetar el nombre «Path»: %v", got)
	}
	for _, kv := range got {
		if strings.HasPrefix(kv, "PATH=") {
			t.Errorf("no debe duplicar el PATH: %v", got)
		}
	}
}
