// Package chat orquesta los turnos del chat de diseño: qué conversación está
// respondiendo ahora, cómo se cancela, cómo se agrupan los trozos que llegan
// del modelo antes de mandarlos a la pantalla y qué parte del historial viaja
// en cada turno.
//
// No toca la base ni Wails: app_chat.go persiste los mensajes y emite los
// eventos. Así este paquete se prueba con un llm.Client falso, sin red.
package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"scadlab/backend/i18n"
	"scadlab/backend/llm"
)

// ErrBusy lo devuelve Begin si el chat ya está respondiendo: dos turnos a la
// vez sobre la misma conversación se pisarían el historial.
var ErrBusy = i18n.New(i18n.Msg{ES: "este chat todavía está respondiendo; espera o detenelo", EN: "this chat is still answering; wait or stop it"})

// FlushInterval es cada cuánto se manda a la pantalla lo acumulado. Un modelo
// local puede soltar un delta por token (50–100 por segundo); un evento de
// Wails por cada uno satura el puente y obliga a redibujar a esa cadencia sin
// que el ojo lo note. ~25 cuadros por segundo se ven fluidos.
const FlushInterval = 40 * time.Millisecond

// Manager sabe qué chats están respondiendo. Es seguro para uso concurrente.
type Manager struct {
	mu      sync.Mutex
	running map[int64]*Turn
	wg      sync.WaitGroup
}

// NewManager crea un Manager vacío.
func NewManager() *Manager { return &Manager{running: map[int64]*Turn{}} }

// Turn es una respuesta en curso.
type Turn struct {
	m      *Manager
	chatID int64
	cancel context.CancelFunc
	ctx    context.Context

	mu        sync.Mutex
	partial   strings.Builder // lo ya enviado a la pantalla
	seq       int             // cuántos trozos se enviaron
	cancelled bool
}

// Begin registra el turno de chatID. El contexto del turno nace de parent (la
// app) y se cancela con Cancel o CancelAll.
func (m *Manager) Begin(parent context.Context, chatID int64) (*Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, busy := m.running[chatID]; busy {
		return nil, ErrBusy
	}
	ctx, cancel := context.WithCancel(parent)
	t := &Turn{m: m, chatID: chatID, cancel: cancel, ctx: ctx}
	m.running[chatID] = t
	m.wg.Add(1)
	return t, nil
}

// Cancel corta la respuesta de chatID. false si no estaba respondiendo.
func (m *Manager) Cancel(chatID int64) bool {
	m.mu.Lock()
	t := m.running[chatID]
	m.mu.Unlock()
	if t == nil {
		return false
	}
	t.mu.Lock()
	t.cancelled = true
	t.mu.Unlock()
	t.cancel()
	return true
}

// Snapshot dice si chatID está respondiendo y qué se le mostró hasta ahora
// (texto y número de trozo), para que una pantalla que se abre a mitad de
// respuesta retome sin duplicar ni perder texto.
func (m *Manager) Snapshot(chatID int64) (running bool, partial string, seq int) {
	m.mu.Lock()
	t := m.running[chatID]
	m.mu.Unlock()
	if t == nil {
		return false, "", 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return true, t.partial.String(), t.seq
}

// CancelAll corta todo lo que esté respondiendo y espera (hasta wait) a que
// los turnos terminen de guardar: se usa al cerrar la app, antes de cerrar la
// base.
func (m *Manager) CancelAll(wait time.Duration) {
	m.mu.Lock()
	turns := make([]*Turn, 0, len(m.running))
	for _, t := range m.running {
		turns = append(turns, t)
	}
	m.mu.Unlock()
	for _, t := range turns {
		m.Cancel(t.chatID)
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(wait):
	}
}

// Context es el contexto del turno.
func (t *Turn) Context() context.Context { return t.ctx }

// End libera el turno. Siempre se llama, con defer, cuando el turno terminó de
// guardar todo (también por error o cancelación).
func (t *Turn) End() {
	t.cancel()
	t.m.mu.Lock()
	delete(t.m.running, t.chatID)
	t.m.mu.Unlock()
	t.m.wg.Done()
}

// Outcome es cómo terminó un turno.
type Outcome struct {
	Text string
	// Cancelled: lo cortó el usuario (Cancel) o el cierre de la app. Text trae
	// lo escrito hasta ahí, que se conserva.
	Cancelled bool
}

// Run envía la conversación y reparte la respuesta por emit(seq, texto) en
// trozos agrupados cada FlushInterval. Cuando vuelve, todo lo recibido ya pasó
// por emit: el "listo" que mande quien llama no puede adelantarse al texto.
func (t *Turn) Run(client llm.Client, req llm.ChatRequest, emit func(seq int, text string)) (Outcome, error) {
	b := &batcher{interval: FlushInterval, emit: func(s string) {
		// Bajo el candado del turno, para que Snapshot vea el mismo orden que
		// la pantalla: el texto de un trozo nunca queda en el snapshot y en el
		// evento de otro.
		t.mu.Lock()
		defer t.mu.Unlock()
		t.partial.WriteString(s)
		t.seq++
		emit(t.seq, s)
	}}
	res, err := client.Chat(t.ctx, req, b.add)
	b.close()

	t.mu.Lock()
	cancelled := t.cancelled
	t.mu.Unlock()
	out := Outcome{Text: res.Text}
	if err != nil && errors.Is(err, context.Canceled) {
		// Cancelado por el usuario o por el cierre de la app: no es un fallo.
		out.Cancelled = true
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Cancelled = cancelled && t.ctx.Err() != nil
	return out, nil
}

// batcher agrupa trozos: el primero sale de inmediato (la respuesta se ve
// arrancar sin esperar un intervalo) y los siguientes se juntan hasta el
// próximo vencimiento.
type batcher struct {
	interval time.Duration
	emit     func(string)

	mu     sync.Mutex
	buf    strings.Builder
	timer  *time.Timer
	last   time.Time
	closed bool
}

func (b *batcher) add(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.buf.WriteString(s)
	if b.timer != nil {
		return // ya hay un vencimiento pendiente que lo lleva
	}
	if wait := b.interval - time.Since(b.last); wait > 0 {
		b.timer = time.AfterFunc(wait, b.onTimer)
		return
	}
	b.flushLocked()
}

func (b *batcher) onTimer() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timer = nil
	b.flushLocked()
}

func (b *batcher) flushLocked() {
	if b.buf.Len() == 0 {
		return
	}
	s := b.buf.String()
	b.buf.Reset()
	b.last = time.Now()
	b.emit(s)
}

func (b *batcher) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.flushLocked()
	b.closed = true
}

// Title arma el título de un chat con las primeras palabras de su primer
// mensaje: lo bastante largo para reconocerlo en la lista y lo bastante corto
// para que quepa. Colapsa saltos de línea y espacios.
func Title(text string) string {
	const maxWords, maxRunes = 8, 48
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	cut := len(words) > maxWords
	if cut {
		words = words[:maxWords]
	}
	// Si pasa del largo, se quitan palabras enteras del final: un título cortado
	// a mitad de palabra se lee como un error.
	title := strings.Join(words, " ")
	for utf8.RuneCountInString(title) > maxRunes && len(words) > 1 {
		words = words[:len(words)-1]
		title = strings.Join(words, " ")
		cut = true
	}
	if r := []rune(title); len(r) > maxRunes {
		title = strings.TrimSpace(string(r[:maxRunes]))
		cut = true
	}
	if cut {
		title += "…"
	}
	return title
}

// TrimHistory deja los mensajes más recientes que entran en maxChars, sin
// partir ningún mensaje, siempre con el último (la pregunta de ahora) y
// empezando por uno de usuario: un modelo que arranca la conversación con una
// respuesta suya se confunde y la API de Anthropic lo rechaza.
func TrimHistory(msgs []llm.Message, maxChars int) []llm.Message {
	if len(msgs) == 0 {
		return nil
	}
	total, start := 0, len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		n := len(msgs[i].Content)
		if total+n > maxChars && i < len(msgs)-1 {
			break
		}
		total += n
		start = i
	}
	for start < len(msgs)-1 && msgs[start].Role != llm.RoleUser {
		start++
	}
	return msgs[start:]
}

// MergeRoles junta los mensajes consecutivos del mismo rol en uno. Pasa tras un
// turno que falló sin respuesta guardada: quedan dos mensajes de usuario
// seguidos, y las plantillas de chat de varios modelos locales (Mistral,
// algunos Llama en llama.cpp) exigen que los roles alternen y devuelven un
// error de plantilla en vez de contestar.
func MergeRoles(msgs []llm.Message) []llm.Message {
	var out []llm.Message
	for _, m := range msgs {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == m.Role {
			out[n-1].Content += "\n\n" + m.Content
			continue
		}
		out = append(out, m)
	}
	return out
}

// HistoryBudget es cuántos caracteres de historial viajan por tipo de
// proveedor. Un modelo local corre con la ventana chica que se le pide (ver
// llm.Options.NumCtx) y el prompt de sistema ya ocupa buena parte; uno en la
// nube admite conversaciones largas.
func HistoryBudget(kind string) int {
	if kind == llm.KindOllama {
		return 24_000
	}
	return 200_000
}
