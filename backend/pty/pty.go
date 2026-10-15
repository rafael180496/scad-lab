// Package pty abre procesos interactivos —los CLIs de los agentes de IA— sobre
// un pseudo-terminal real y reparte su salida al frontend por eventos.
//
// Porta lo esencial de backend/localterm de mini-tools. El PTY sale de
// github.com/aymanbagabas/go-pty: Go puro (openpty en Unix, ConPTY en Windows),
// así que sigue cross-compilando a Windows desde macOS sin CGO.
//
// Por qué un pseudo-terminal y no pipes: Claude Code, opencode o Codex son
// aplicaciones de terminal (TUI). Sin una TTY detectan que no es interactivo,
// no dibujan su interfaz y no aceptan teclas sueltas ni el cambio de tamaño.
package pty

import (
	"encoding/base64"
	"os"
	"sync"
	"sync/atomic"
	"time"

	gopty "github.com/aymanbagabas/go-pty"

	"scadlab/backend/i18n"
)

// Eventos Wails de la terminal (Go → React). Los nombres son contrato con
// frontend/src/lib/events.ts.
const (
	EventData = "terminal:data"
	EventExit = "terminal:exit"
)

// EmitFunc entrega un evento al frontend. Se inyecta (App.emit) para que este
// paquete no conozca a Wails y se pruebe con go test.
type EmitFunc func(event string, payload any)

// DataEvent es el payload de terminal:data. Data es base64 de los bytes crudos
// del PTY: un CLI puede emitir secuencias que no son UTF-8 válido y eso
// rompería el JSON si viajara como string. Seq es el desplazamiento (bytes
// emitidos antes de este trozo): con él, el frontend que se reengancha tras
// recargar la UI une la captura del buffer con los eventos en vivo sin
// duplicar ni perder bytes.
type DataEvent struct {
	ID   string `json:"id"`
	Data string `json:"data"`
	Seq  int64  `json:"seq"`
}

// ExitEvent es el payload de terminal:exit. Code es -1 si el proceso murió por
// una señal o lo cerró ScadLab.
type ExitEvent struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
}

// Info describe una terminal abierta.
type Info struct {
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	StartedAt int64  `json:"startedAt"` // unix ms
}

// Snapshot es lo último que mostró la terminal. Seq es el desplazamiento
// justo después de su último byte: los eventos con Seq+len(data) <= Seq ya
// están incluidos.
type Snapshot struct {
	Data []byte
	Seq  int64
}

const (
	readChunk = 8192
	// ringCap es cuánta salida reciente se conserva por terminal para
	// reenganchar la UI tras una recarga (~512 KB: varias pantallas de un TUI
	// y bastante scrollback, sin crecer sin límite con un agente hablador).
	ringCap = 512 * 1024
	// quietPeriod: tras morir el proceso se espera a que la salida pendiente
	// termine de leerse antes de cerrar el PTY; cerrarlo antes pierde las
	// últimas líneas (justo las del error por el que murió).
	quietPeriod = 60 * time.Millisecond
	maxDrain    = time.Second
)

type term struct {
	id   string
	p    gopty.Pty
	cmd  *gopty.Cmd
	info Info

	mu       sync.Mutex // protege ring, total, info.Cols/Rows
	ring     []byte
	total    int64
	capacity int

	lastRead  atomic.Int64 // unix nano de la última lectura
	closeOnce sync.Once
}

// Manager tiene las terminales abiertas. Es seguro para uso concurrente.
type Manager struct {
	mu    sync.Mutex
	terms map[string]*term
	emit  EmitFunc
	cap   int
}

// NewManager crea el gestor. emit puede ser nil (se descartan los eventos).
func NewManager(emit EmitFunc) *Manager {
	return &Manager{terms: map[string]*term{}, emit: emit, cap: ringCap}
}

func (m *Manager) send(event string, payload any) {
	if m.emit != nil {
		m.emit(event, payload)
	}
}

// Open lanza argv en cwd sobre un PTY nuevo. argv[0] ya viene resuelto
// (detect.Find) y los argumentos se pasan tal cual, sin shell intermedia: un
// id de sesión o un texto del usuario nunca se interpreta como comando.
// env es el entorno completo del proceso (ver Environ); nil hereda el actual.
func (m *Manager) Open(id, cwd string, argv, env []string, cols, rows int) error {
	if id == "" || len(argv) == 0 || argv[0] == "" {
		return i18n.New(i18n.Msg{ES: "terminal: falta el identificador o el comando", EN: "terminal: missing the identifier or the command"})
	}
	// Un cwd que no existe haría fallar el arranque con un error críptico del
	// SO; acá no se cae al home en silencio como en una shell: el agente tiene
	// que trabajar DENTRO del workspace o no arrancar.
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return i18n.Errorf(i18n.Msg{ES: "terminal: la carpeta %q no existe", EN: "terminal: the folder %q does not exist"}, cwd)
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}

	m.mu.Lock()
	if _, dup := m.terms[id]; dup {
		m.mu.Unlock()
		return i18n.Errorf(i18n.Msg{ES: "terminal: ya hay una terminal %q abierta", EN: "terminal: a terminal %q is already open"}, id)
	}
	// Se reserva el id antes de lanzar para que dos Open simultáneos no
	// arranquen dos procesos con el mismo id.
	t := &term{id: id, capacity: m.cap, info: Info{ID: id, Cwd: cwd, Cols: cols, Rows: rows, StartedAt: time.Now().UnixMilli()}}
	m.terms[id] = t
	m.mu.Unlock()

	fail := func(err error) error {
		m.remove(id)
		return err
	}

	p, err := gopty.New()
	if err != nil {
		return fail(i18n.Errorf(i18n.Msg{ES: "terminal: no se pudo abrir el pseudo-terminal: %w", EN: "terminal: could not open the pseudo-terminal: %w"}, err))
	}
	// Un fallo al fijar el tamaño no es fatal: arranca en 80x24 y el primer
	// Resize del frontend (su ResizeObserver) lo corrige.
	_ = p.Resize(cols, rows)

	cmd := p.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	if env != nil {
		cmd.Env = env
	}
	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return fail(i18n.Errorf(i18n.Msg{ES: "terminal: no se pudo iniciar %s: %w", EN: "terminal: could not start %s: %w"}, argv[0], err))
	}
	t.p, t.cmd = p, cmd
	t.lastRead.Store(time.Now().UnixNano())

	readerDone := make(chan struct{})
	go m.stream(t, readerDone)
	go m.wait(t, readerDone)
	return nil
}

// stream bombea la salida del PTY al ring y al frontend hasta que el PTY se
// cierra.
func (m *Manager) stream(t *term, done chan<- struct{}) {
	defer close(done)
	buf := make([]byte, readChunk)
	for {
		n, err := t.p.Read(buf)
		if n > 0 {
			t.lastRead.Store(time.Now().UnixNano())
			chunk := append([]byte(nil), buf[:n]...)
			// El ring y el evento van bajo el mismo orden de Seq: el offset se
			// reserva con el candado y el emit sale después, en este mismo
			// hilo, así que los eventos de una terminal llegan en orden.
			t.mu.Lock()
			seq := t.total
			t.total += int64(n)
			t.ring = append(t.ring, chunk...)
			// Histéresis: se recorta al llegar a 2×cap, no en cada lectura
			// (copiar 512 KB por cada trozo de 8 KB frenaría una salida rápida).
			if len(t.ring) > 2*t.capacity {
				t.ring = append(t.ring[:0], t.ring[len(t.ring)-t.capacity:]...)
			}
			t.mu.Unlock()
			m.send(EventData, DataEvent{ID: t.id, Data: base64.StdEncoding.EncodeToString(chunk), Seq: seq})
		}
		if err != nil {
			return
		}
	}
}

// wait cosecha el proceso y cierra todo. Sin esto quedaría un zombi por cada
// CLI que termina y, en Windows, el handle nunca se liberaría.
func (m *Manager) wait(t *term, readerDone <-chan struct{}) {
	_ = t.cmd.Wait()
	code := -1
	if ps := t.cmd.ProcessState; ps != nil {
		code = ps.ExitCode()
	}
	// Hijos que sobrevivan al CLI (servidores MCP, shells) mueren con él.
	killTree(t.cmd)

	// El PTY no avisa fin de archivo al morir el hijo (el padre conserva el
	// extremo esclavo abierto), así que se espera a que la salida pendiente
	// termine de leerse antes de cerrarlo.
	drain := time.NewTimer(maxDrain)
	defer drain.Stop()
	tick := time.NewTicker(quietPeriod / 2)
	defer tick.Stop()
drained:
	for {
		select {
		case <-readerDone:
			break drained
		case <-drain.C:
			break drained
		case <-tick.C:
			if time.Since(time.Unix(0, t.lastRead.Load())) >= quietPeriod {
				break drained
			}
		}
	}
	m.closePTY(t)
	<-readerDone
	m.remove(t.id)
	m.send(EventExit, ExitEvent{ID: t.id, Code: code})
}

func (m *Manager) closePTY(t *term) {
	t.closeOnce.Do(func() { _ = t.p.Close() })
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	delete(m.terms, id)
	m.mu.Unlock()
}

func (m *Manager) get(id string) *term {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.terms[id]
	if t == nil || t.p == nil { // reservada pero todavía arrancando
		return nil
	}
	return t
}

func errNoTerminal(id string) error {
	return i18n.Errorf(i18n.Msg{ES: "terminal: no hay una terminal abierta con id %q", EN: "terminal: there is no open terminal with id %q"}, id)
}

// Write envía teclas o texto pegado al stdin del proceso.
func (m *Manager) Write(id, data string) error {
	t := m.get(id)
	if t == nil {
		return errNoTerminal(id)
	}
	_, err := t.p.Write([]byte(data))
	return err
}

// Resize reflowa el PTY tras el fit del frontend. Sin esto, un TUI dibuja
// sobre un tamaño equivocado.
func (m *Manager) Resize(id string, cols, rows int) error {
	t := m.get(id)
	if t == nil {
		return errNoTerminal(id)
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	if err := t.p.Resize(cols, rows); err != nil {
		return err
	}
	t.mu.Lock()
	t.info.Cols, t.info.Rows = cols, rows
	t.mu.Unlock()
	return nil
}

// Close mata el proceso de id (y sus hijos). Idempotente. El evento
// terminal:exit lo emite igual la goroutine que cosecha el proceso.
func (m *Manager) Close(id string) error {
	t := m.get(id)
	if t == nil {
		return nil
	}
	// Se mata antes de cerrar el PTY: un proceso vivo con su terminal cerrada
	// puede quedar colgado escribiendo en un descriptor muerto.
	killTree(t.cmd)
	// Se quita ya del mapa (no cuando termine de cosecharse): quien cierra una
	// terminal y abre otra enseguida no debe chocar con el tope de terminales.
	m.remove(id)
	return nil
}

// CloseAll mata todas las terminales (cierre de la app): lo que queda vivo es
// un proceso del SO, y no cerrarlo deja agentes huérfanos consumiendo cuota.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*term, 0, len(m.terms))
	for _, t := range m.terms {
		all = append(all, t)
	}
	m.mu.Unlock()
	for _, t := range all {
		if t.p != nil {
			killTree(t.cmd)
			m.closePTY(t)
		}
	}
}

// Has dice si id es una terminal abierta.
func (m *Manager) Has(id string) bool { return m.get(id) != nil }

// List devuelve las terminales abiertas (para reenganchar la UI al recargar).
func (m *Manager) List() []Info {
	m.mu.Lock()
	all := make([]*term, 0, len(m.terms))
	for _, t := range m.terms {
		if t.p != nil {
			all = append(all, t)
		}
	}
	m.mu.Unlock()
	out := make([]Info, 0, len(all))
	for _, t := range all {
		t.mu.Lock()
		out = append(out, t.info)
		t.mu.Unlock()
	}
	return out
}

// Snapshot devuelve la salida reciente de id (hasta ringCap bytes).
func (m *Manager) Snapshot(id string) (Snapshot, error) {
	t := m.get(id)
	if t == nil {
		return Snapshot{}, errNoTerminal(id)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	data := t.ring
	if len(data) > t.capacity {
		data = data[len(data)-t.capacity:]
	}
	return Snapshot{Data: append([]byte(nil), data...), Seq: t.total}, nil
}
