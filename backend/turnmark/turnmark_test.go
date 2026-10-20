package turnmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func readMarkFile(t *testing.T, dir string) markFile {
	t.Helper()
	f, err := readFile(Path(dir))
	if err != nil {
		t.Fatalf("leer la marca: %v", err)
	}
	return f
}

func TestAskTurnWritesTheMarkAndEndRemovesIt(t *testing.T) {
	dir := t.TempDir()
	if ReadOnly(dir) {
		t.Fatal("sin marca es Agente")
	}
	end, err := Begin(dir, Ask)
	if err != nil {
		t.Fatal(err)
	}
	defer end()

	f := readMarkFile(t, dir)
	if len(f.Turns) != 1 || f.Turns[0].Mode != Ask || f.Turns[0].PID != os.Getpid() || !f.Turns[0].Expires.After(time.Now()) {
		t.Fatalf("la entrada: %+v", f)
	}
	if !ReadOnly(dir) {
		t.Error("con la marca viva es Solo consulta")
	}
	if filepath.Base(filepath.Dir(Path(dir))) != ".scadlab" {
		t.Errorf("la marca va en .scadlab: %s", Path(dir))
	}

	end()
	end() // idempotente
	if _, err := os.Stat(Path(dir)); err == nil {
		t.Error("al terminar el turno se retira la marca")
	}
	if ReadOnly(dir) {
		t.Error("sin turno, vuelve a Agente")
	}
}

func TestAgentTurnLeavesNoMark(t *testing.T) {
	dir := t.TempDir()
	end, err := Begin(dir, Agent)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if _, err := os.Stat(Path(dir)); err == nil {
		t.Error("en Agente no hay marca")
	}
	if ReadOnly(dir) {
		t.Error("Agente deja pasar todo")
	}
}

// Una marca que dice «agent» (la escribe a mano alguien, o una versión futura) no
// bloquea nada.
func TestAMarkThatSaysAgentLetsEverythingThrough(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		if err := os.MkdirAll(filepath.Dir(Path(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	write(`{"turns":[{"id":"x","mode":"agent","pid":1,"expires":"` + future + `"}]}`)
	if ReadOnly(dir) {
		t.Error("mode=agent no bloquea")
	}
	write(`{"turns":[]}`)
	if ReadOnly(dir) {
		t.Error("sin turnos no bloquea")
	}
}

// Una marca vencida —el proceso murió sin retirarla— deja de bloquear sola.
func TestAnExpiredMarkNoLongerBlocks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(Path(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute).Format(time.RFC3339)
	body := `{"turns":[{"id":"x","mode":"ask","pid":99999,"expires":"` + old + `"}]}`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if ReadOnly(dir) {
		t.Error("una marca vencida no bloquea")
	}
	// Y a la vigente de otro proceso sí se le hace caso.
	live := time.Now().Add(time.Minute).Format(time.RFC3339)
	body = `{"turns":[{"id":"x","mode":"ask","pid":99999,"expires":"` + live + `"}]}`
	if err := os.WriteFile(Path(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ReadOnly(dir) {
		t.Error("una marca vigente de otro proceso bloquea")
	}
	if ModeOf(dir, time.Now().Add(5*time.Minute)) != Agent {
		t.Error("pasado el vencimiento, vuelve a Agente")
	}
}

// El latido renueva el vencimiento mientras el turno corre: una consulta de una
// hora no pierde la marca a los 90 s.
func TestHeartbeatRenewsTheExpiry(t *testing.T) {
	prev := heartbeat
	heartbeat = 10 * time.Millisecond
	t.Cleanup(func() { heartbeat = prev })

	dir := t.TempDir()
	end, err := Begin(dir, Ask)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	first := readMarkFile(t, dir).Turns[0].Expires
	deadline := time.Now().Add(2 * time.Second)
	for {
		if readMarkFile(t, dir).Turns[0].Expires.After(first) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("el latido no renovó el vencimiento")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Una marca que existe pero no se lee bloquea (ante la duda no se escribe) mientras
// es reciente; una vieja es un resto y se ignora.
func TestAnUnreadableMarkBlocksOnlyWhileFresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(Path(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir), []byte(`{"turns": [no es json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ReadOnly(dir) {
		t.Error("una marca ilegible y reciente bloquea")
	}
	old := time.Now().Add(-10 * TTL)
	if err := os.Chtimes(Path(dir), old, old); err != nil {
		t.Fatal(err)
	}
	if ReadOnly(dir) {
		t.Error("una marca ilegible y vieja es un resto: no bloquea")
	}
}

// Dos turnos de consulta a la vez en el mismo workspace: la marca vale hasta que
// termina el ÚLTIMO, y las dos entradas conviven sin pisarse.
func TestTwoAskTurnsKeepTheMarkUntilTheLastEnds(t *testing.T) {
	dir := t.TempDir()
	end1, err := Begin(dir, Ask)
	if err != nil {
		t.Fatal(err)
	}
	end2, err := Begin(dir, Ask)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(readMarkFile(t, dir).Turns); n != 2 {
		t.Fatalf("dos entradas, hay %d", n)
	}
	end1()
	if !ReadOnly(dir) || len(readMarkFile(t, dir).Turns) != 1 {
		t.Fatal("el segundo turno sigue en curso: la marca sigue")
	}
	end2()
	if _, err := os.Stat(Path(dir)); err == nil {
		t.Error("terminó el último turno: se retira")
	}
}

// Muchos turnos que empiezan y terminan a la vez (-race): ni se pierde una entrada
// ni queda una marca de más.
func TestConcurrentTurnsDoNotCorruptTheMark(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			end, err := Begin(dir, Ask)
			if err != nil {
				t.Error(err)
				return
			}
			if !ReadOnly(dir) {
				t.Error("con mi turno vivo tiene que estar marcado")
			}
			end()
		}()
	}
	wg.Wait()
	if _, err := os.Stat(Path(dir)); err == nil {
		t.Errorf("no queda marca: %s", func() string { b, _ := os.ReadFile(Path(dir)); return string(b) }())
	}
	if ReadOnly(dir) {
		t.Error("sin turnos vuelve a Agente")
	}
}

// Las entradas vigentes de OTRO proceso (dos ScadLab sobre el mismo workspace) se
// conservan al reescribir; las vencidas se descartan.
func TestWritingKeepsLiveEntriesOfOtherProcessesAndDropsExpiredOnes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(Path(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	other := os.Getpid() + 1
	live := time.Now().Add(time.Minute)
	dead := time.Now().Add(-time.Minute)
	raw, _ := json.Marshal(markFile{Turns: []entry{
		{ID: "vivo", Mode: Ask, PID: other, Expires: live},
		{ID: "vencido", Mode: Ask, PID: other, Expires: dead},
	}})
	if err := os.WriteFile(Path(dir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	end, err := Begin(dir, Ask)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range readMarkFile(t, dir).Turns {
		ids[e.ID] = true
	}
	if !ids["vivo"] || ids["vencido"] || len(ids) != 2 {
		t.Errorf("entradas: %v", ids)
	}
	end()
	if f := readMarkFile(t, dir); len(f.Turns) != 1 || f.Turns[0].ID != "vivo" {
		t.Errorf("al terminar queda lo del otro proceso: %+v", f)
	}
}

// Si no se puede escribir la marca (la carpeta es un archivo), Begin lo dice y NO
// registra nada: quien llama no corre un turno de consulta sin marca.
func TestBeginFailsWhenTheMarkCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Dir), []byte("soy un archivo"), 0o644); err != nil {
		t.Fatal(err)
	}
	end, err := Begin(dir, Ask)
	if err == nil {
		end()
		t.Fatal("tenía que fallar")
	}
	registry.Lock()
	defer registry.Unlock()
	if len(registry.dirs[filepath.Clean(dir)]) != 0 {
		t.Errorf("no queda registrado: %+v", registry.dirs)
	}
}
