package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"scadlab/backend/llm"
)

// fakeClient emite los trozos dados (con una pausa entre ellos) y termina con
// err; con hold se queda esperando a que lo cancelen.
type fakeClient struct {
	chunks []string
	pause  time.Duration
	err    error
	hold   bool
}

func (f *fakeClient) Version(context.Context) (string, error) { return "", nil }

func (f *fakeClient) Models(context.Context) ([]string, error) { return nil, nil }

// Chat imita a los clientes reales: ante un error o una cancelación devuelve el
// resultado vacío (lo escrito hasta ahí solo está en los deltas).
func (f *fakeClient) Chat(ctx context.Context, _ llm.ChatRequest, onDelta func(llm.Delta)) (llm.ChatResult, error) {
	for _, c := range f.chunks {
		onDelta(llm.Delta{Thinking: "pienso"}) // el pensamiento nunca entra en el texto
		onDelta(llm.Delta{Text: c})
		time.Sleep(f.pause)
	}
	if f.hold {
		<-ctx.Done()
		return llm.ChatResult{}, ctx.Err()
	}
	if f.err != nil {
		return llm.ChatResult{}, f.err
	}
	return llm.ChatResult{Message: llm.Message{Role: llm.RoleAssistant, Content: strings.Join(f.chunks, "")}, Usage: llm.Usage{Input: 12, Output: 3}, Model: "m-real"}, nil
}

type sink struct {
	mu   sync.Mutex
	seqs []int
	text strings.Builder
}

func (s *sink) emit(seq int, t string) {
	s.mu.Lock()
	s.seqs = append(s.seqs, seq)
	s.text.WriteString(t)
	s.mu.Unlock()
}

func TestRunBatchesAndKeepsAllText(t *testing.T) {
	m := NewManager()
	turn, err := m.Begin(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer turn.End()
	var chunks []string
	for i := 0; i < 200; i++ {
		chunks = append(chunks, "x")
	}
	var s sink
	out, err := turn.Run(&fakeClient{chunks: chunks, pause: 500 * time.Microsecond}, llm.ChatRequest{}, s.emit)
	if err != nil || out.Cancelled {
		t.Fatalf("%+v, %v", out, err)
	}
	// Todo el texto llegó (nada se pierde en el último vencimiento) y en
	// bastantes menos eventos que trozos.
	if s.text.String() != strings.Repeat("x", 200) || out.Text != s.text.String() {
		t.Errorf("texto: %d, %d", s.text.Len(), len(out.Text))
	}
	if len(s.seqs) >= 150 {
		t.Errorf("no se agrupó: %d eventos para 200 trozos", len(s.seqs))
	}
	for i, q := range s.seqs {
		if q != i+1 {
			t.Fatalf("seq no consecutivo: %v", s.seqs)
		}
	}
}

func TestFirstChunkIsImmediate(t *testing.T) {
	m := NewManager()
	turn, _ := m.Begin(context.Background(), 1)
	defer turn.End()
	got := make(chan string, 4)
	_, _ = turn.Run(&fakeClient{chunks: []string{"hola"}}, llm.ChatRequest{}, func(_ int, s string) { got <- s })
	if s := <-got; s != "hola" {
		t.Errorf("primer trozo: %q", s)
	}
}

func TestBusyAndEnd(t *testing.T) {
	m := NewManager()
	turn, err := m.Begin(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Begin(context.Background(), 7); !errors.Is(err, ErrBusy) {
		t.Errorf("segundo Begin: %v", err)
	}
	if _, err := m.Begin(context.Background(), 8); err != nil {
		t.Errorf("otro chat no debería estar ocupado: %v", err)
	}
	turn.End()
	if running, _, _ := m.Snapshot(7); running {
		t.Error("tras End no debería estar corriendo")
	}
	if _, err := m.Begin(context.Background(), 7); err != nil {
		t.Errorf("tras End debería poder empezar otro: %v", err)
	}
}

func TestCancelKeepsPartial(t *testing.T) {
	m := NewManager()
	turn, _ := m.Begin(context.Background(), 1)
	defer turn.End()
	var s sink
	done := make(chan Outcome, 1)
	go func() {
		out, err := turn.Run(&fakeClient{chunks: []string{"a medias"}, hold: true}, llm.ChatRequest{}, s.emit)
		if err != nil {
			t.Errorf("una cancelación no es un error: %v", err)
		}
		done <- out
	}()
	// Espera a que el texto haya salido y recién ahí cancela.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, p, _ := m.Snapshot(1); p == "a medias" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("el texto nunca llegó")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !m.Cancel(1) {
		t.Fatal("Cancel debería encontrar el turno")
	}
	out := <-done
	if !out.Cancelled || out.Text != "a medias" {
		t.Errorf("resultado: %+v", out)
	}
	if m.Cancel(99) {
		t.Error("cancelar un chat inexistente da false")
	}
}

func TestRunPropagatesError(t *testing.T) {
	m := NewManager()
	turn, _ := m.Begin(context.Background(), 1)
	defer turn.End()
	boom := errors.New("el servidor se cayó")
	out, err := turn.Run(&fakeClient{chunks: []string{"par"}, err: boom}, llm.ChatRequest{}, func(int, string) {})
	if !errors.Is(err, boom) || out.Cancelled || out.Text != "par" {
		t.Errorf("%+v, %v", out, err)
	}
}

// Snapshot y eventos se corresponden: el texto del snapshot es exactamente la
// suma de los trozos con seq <= su seq.
func TestSnapshotMatchesEvents(t *testing.T) {
	m := NewManager()
	turn, _ := m.Begin(context.Background(), 1)
	defer turn.End()
	var mu sync.Mutex
	got := map[int]string{}
	go func() {
		_, _ = turn.Run(&fakeClient{chunks: strings.Split(strings.Repeat("ab", 100), ""), pause: time.Millisecond}, llm.ChatRequest{}, func(seq int, s string) {
			mu.Lock()
			got[seq] = s
			mu.Unlock()
		})
	}()
	time.Sleep(60 * time.Millisecond)
	running, partial, seq := m.Snapshot(1)
	if !running || seq == 0 {
		t.Fatalf("snapshot: %v %d", running, seq)
	}
	// Los eventos con seq <= snapshot reconstruyen el snapshot... pero el
	// snapshot se tomó bajo el mismo candado, así que ya los tiene todos.
	mu.Lock()
	var sum strings.Builder
	for i := 1; i <= seq; i++ {
		sum.WriteString(got[i])
	}
	mu.Unlock()
	if sum.String() != partial {
		t.Errorf("snapshot %q != eventos %q", partial, sum.String())
	}
	m.CancelAll(time.Second)
}

func TestCancelAllWaits(t *testing.T) {
	m := NewManager()
	turn, _ := m.Begin(context.Background(), 1)
	finished := make(chan struct{})
	go func() {
		defer turn.End()
		_, _ = turn.Run(&fakeClient{hold: true}, llm.ChatRequest{}, func(int, string) {})
		time.Sleep(30 * time.Millisecond) // guardando el parcial
		close(finished)
	}()
	time.Sleep(10 * time.Millisecond)
	m.CancelAll(2 * time.Second)
	select {
	case <-finished:
	default:
		t.Error("CancelAll volvió antes de que el turno terminara de guardar")
	}
}

func TestTitle(t *testing.T) {
	cases := map[string]string{
		"":                         "",
		"   \n ":                   "",
		"Una caja para Raspberry":  "Una caja para Raspberry",
		"haz  una\ncaja\tcon tapa": "haz una caja con tapa",
		"Necesito un soporte para auriculares con base ancha y cable":                  "Necesito un soporte para auriculares con base…",
		"supercalifragilisticoespialidosocomoseescribeestapalabratanlargaquenocabe en": "supercalifragilisticoespialidosocomoseescribeest…",
	}
	for in, want := range cases {
		if got := Title(in); got != want {
			t.Errorf("Title(%q) = %q, quiero %q", in, got, want)
		}
	}
}

func TestTrimHistory(t *testing.T) {
	u := func(s string) llm.Message { return llm.Message{Role: llm.RoleUser, Content: s} }
	a := func(s string) llm.Message { return llm.Message{Role: llm.RoleAssistant, Content: s} }
	msgs := []llm.Message{u("uno"), a("dos"), u("tres"), a("cuatro"), u("cinco")}
	if got := TrimHistory(msgs, 1000); len(got) != 5 {
		t.Errorf("todo entra: %d", len(got))
	}
	// 14 chars: entran "cinco"(5) + "cuatro"(6) = 11; "tres"(4) no (15 > 14). El
	// recorte cae en una respuesta, así que arranca en el siguiente usuario.
	got := TrimHistory(msgs, 14)
	if len(got) != 1 || got[0].Content != "cinco" {
		t.Errorf("recorte: %+v", got)
	}
	// El último siempre viaja, aunque solo no entre.
	big := []llm.Message{u("x"), u(strings.Repeat("y", 100))}
	if got := TrimHistory(big, 10); len(got) != 1 || len(got[0].Content) != 100 {
		t.Errorf("el último siempre va: %+v", got)
	}
	if TrimHistory(nil, 10) != nil {
		t.Error("nil -> nil")
	}
	if HistoryBudget(llm.KindOllama) >= HistoryBudget(llm.KindAnthropic) {
		t.Error("un modelo local lleva menos historial")
	}
}

func TestMergeRoles(t *testing.T) {
	in := []llm.Message{
		{Role: llm.RoleUser, Content: "uno"},
		{Role: llm.RoleUser, Content: "dos"},
		{Role: llm.RoleAssistant, Content: "  "},
		{Role: llm.RoleAssistant, Content: "tres"},
		{Role: llm.RoleUser, Content: "cuatro"},
	}
	got := MergeRoles(in)
	if len(got) != 3 || got[0].Content != "uno\n\ndos" || got[1].Content != "tres" || got[2].Content != "cuatro" {
		t.Errorf("%+v", got)
	}
	if in[0].Content != "uno" {
		t.Error("no debe mutar la entrada")
	}
}

// CleanTitle valida el nombre que la persona escribió para una conversación.
func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		" \n\t ":                 "",
		"  Mi   caja \n de Pi  ": "Mi caja de Pi",
		"ñandú":                  "ñandú",
		strings.Repeat("ñ", 300): strings.Repeat("ñ", MaxTitleRunes),
	} {
		if got := CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q)=%q, esperaba %q", in, got, want)
		}
	}
	// Un corte a media palabra no deja un espacio al final.
	long := strings.Repeat("a ", 100)
	if got := CleanTitle(long); got != strings.TrimSpace(got) || len([]rune(got)) > MaxTitleRunes {
		t.Errorf("título largo: %q", got)
	}
}

// Stream.Flush manda ya lo pendiente: el texto que precede a un paso de
// herramienta no puede llegar después de él. Close lo manda y no acepta más, y
// Snapshot ve el mismo orden que la pantalla.
func TestStreamFlushAndSnapshot(t *testing.T) {
	m := NewManager()
	turn, err := m.Begin(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	defer turn.End()
	var mu sync.Mutex
	var got []string
	s := turn.Stream(func(seq int, text string) {
		mu.Lock()
		got = append(got, fmt.Sprintf("%d:%s", seq, text))
		mu.Unlock()
	})
	s.Add("uno ")
	s.Add("dos") // dentro del intervalo: se agrupa con lo anterior si no hubo flush
	s.Flush()
	mu.Lock()
	n := len(got)
	joined := strings.Join(got, "|")
	mu.Unlock()
	if n == 0 || !strings.Contains(joined, "uno ") || !strings.Contains(joined, "dos") {
		t.Fatalf("Flush debía mandar todo lo pendiente: %v", got)
	}
	running, partial, seq := m.Snapshot(7)
	if !running || partial != "uno dos" || seq != n {
		t.Errorf("snapshot: %v %q %d (eventos %d)", running, partial, seq, n)
	}
	s.Add("tres")
	s.Close()
	s.Add("cuatro") // tras Close se ignora
	if _, partial, _ := m.Snapshot(7); partial != "uno dostres" {
		t.Errorf("tras Close: %q", partial)
	}
	if turn.WasCancelled() {
		t.Error("no se canceló")
	}
	m.Cancel(7)
	if !turn.WasCancelled() {
		t.Error("tras Cancel debía figurar cancelado")
	}
}

// El historial es proporcional a la ventana: más ventana, más historial; sin
// ventana rige el presupuesto de siempre; nunca baja de 4 000 ni pasa del techo.
func TestHistoryBudgetForScalesWithTheWindow(t *testing.T) {
	small := HistoryBudgetFor(llm.KindOllama, 8192)
	mid := HistoryBudgetFor(llm.KindOllama, 16384)
	big := HistoryBudgetFor(llm.KindOllama, 32768)
	if !(small < mid && mid < big) {
		t.Errorf("debía crecer con la ventana: %d %d %d", small, mid, big)
	}
	if small != 8192*4*35/100 {
		t.Errorf("8k = %d", small)
	}
	if HistoryBudgetFor(llm.KindOllama, 0) != HistoryBudget(llm.KindOllama) {
		t.Error("sin ventana rige el presupuesto de siempre")
	}
	if HistoryBudgetFor(llm.KindOllama, 1024) != 4000 {
		t.Error("el piso es 4 000")
	}
	if HistoryBudgetFor(llm.KindOllama, 10_000_000) != 60_000 || HistoryBudgetFor(llm.KindAnthropic, 10_000_000) != HistoryBudget(llm.KindAnthropic) {
		t.Error("el techo no se pasa")
	}
}
