//go:build !windows

package clichat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scadlab/backend/detect"
)

// recorder junta los eventos de un turno.
type recorder struct {
	evs []Event
}

func (r *recorder) emit(e Event) { r.evs = append(r.evs, e) }

// run corre un turno del CLI de mentira `cli` (binario `bin`) en una carpeta
// temporal y devuelve el resultado, el error y lo que se emitió.
func run(t *testing.T, ctx context.Context, f *fakeCLIs, cli string, mutate func(*Turn)) (Result, error, *recorder, string) {
	t.Helper()
	ws := t.TempDir()
	c, _ := Lookup(cli)
	tr := Turn{
		CLI: cli, Exe: Resolve(cli), Cwd: ws, Prompt: "haz una caja de 40 mm", Mode: ModeAsk, Lang: "es", MCPEnabled: true,
	}
	_ = c
	if mutate != nil {
		mutate(&tr)
	}
	rec := &recorder{}
	res, err := Run(ctx, tr, rec.emit)
	return res, err, rec, ws
}

func TestRunStreamsATurnWithToolsAndSession(t *testing.T) {
	f := newFakeCLIs(t)
	f.install("claude", "claude-stream.jsonl")

	res, err, rec, ws := run(t, context.Background(), f, "claude", func(tr *Turn) {
		tr.MCP = mcpClaude
		tr.Model = "opus"
	})
	if err != nil || res.Cancelled {
		t.Fatalf("Run: %+v, %v", res, err)
	}
	if got := text(rec.evs); got != "Voy a renderizar y quedó bien: una caja de 40 mm." {
		t.Errorf("texto: %q", got)
	}
	want := []EventKind{KindStart, KindText, KindTool, KindToolResult, KindTool, KindToolResult, KindText, KindDone}
	if got := kinds(rec.evs); !equalKinds(got, want) {
		t.Errorf("secuencia de eventos:\n got %v\nwant %v", got, want)
	}
	if res.Session != "9cb82bf7-284f-4617-a552-dfe908f87b7f" || res.Model != "claude-opus-5[1m]" {
		t.Errorf("sesión y modelo: %+v", res)
	}
	if res.Usage == nil || res.Usage.Output != 120 || res.Usage.CostUSD == 0 {
		t.Errorf("uso: %+v", res.Usage)
	}

	calls := f.calls("claude")
	if len(calls) != 1 {
		t.Fatalf("llamadas: %v", calls)
	}
	args := calls[0]
	if args[0] != "-p" || args[1] != "haz una caja de 40 mm" || !has(args, "--output-format", "stream-json") || !has(args, "--model", "opus") {
		t.Errorf("argv: %v", args)
	}
	if !has(args, mcpClaude...) {
		t.Errorf("los flags MCP de ScadLab llegaron al CLI: %v", args)
	}
	// Corre en la carpeta del workspace.
	gotCwd, _ := filepath.EvalSymlinks(strings.TrimSpace(f.read("cwd.claude")))
	wantCwd, _ := filepath.EvalSymlinks(ws)
	if gotCwd != wantCwd {
		t.Errorf("cwd = %q, quería %q", gotCwd, wantCwd)
	}
}

func equalKinds(a, b []EventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRunContinuesTheConversationWithTheSavedSession(t *testing.T) {
	f := newFakeCLIs(t)
	f.install("claude", "claude-stream.jsonl")

	first, err, _, _ := run(t, context.Background(), f, "claude", nil)
	if err != nil || first.Session == "" {
		t.Fatalf("primer turno: %+v, %v", first, err)
	}
	second, err, _, _ := run(t, context.Background(), f, "claude", func(tr *Turn) { tr.Session = first.Session })
	if err != nil {
		t.Fatal(err)
	}
	calls := f.calls("claude")
	if len(calls) != 2 {
		t.Fatalf("llamadas: %v", calls)
	}
	if hasArg(calls[0], "--resume") {
		t.Errorf("el primer turno es una conversación nueva: %v", calls[0])
	}
	if !has(calls[1], "--resume", first.Session) || second.Session != first.Session {
		t.Errorf("el segundo turno retoma la sesión: %v / %+v", calls[1], second)
	}
}

func TestRunCodexAntigravityAndOpencode(t *testing.T) {
	f := newFakeCLIs(t)
	f.install("codex", "codex-stream.jsonl")
	f.install("agy", "antigravity-stream.jsonl")
	f.install("opencode", "opencode-stream.jsonl")
	f.put("export.opencode", mustRead(t, "testdata/opencode-export.json"))

	// Codex: el mensaje llega entero y el hilo es la sesión.
	res, err, rec, _ := run(t, context.Background(), f, "codex", func(tr *Turn) { tr.MCP = mcpCodex; tr.Images = []string{"/data/x.png"} })
	if err != nil || res.Session != "01a00217-12d1-7492-b8df-b6a2c1f0bc86" || text(rec.evs) != "Cabe en la placa." {
		t.Fatalf("codex: %+v, %v, %q", res, err, text(rec.evs))
	}
	cargs := f.calls("codex")[0]
	if !has(cargs, "-i", "/data/x.png") || !has(cargs, "--sandbox", "read-only") || !has(cargs, mcpCodex...) {
		t.Errorf("codex argv: %v", cargs)
	}

	// Antigravity: trozos de texto y un paso de herramienta con su cierre.
	res, err, rec, _ = run(t, context.Background(), f, "antigravity", func(tr *Turn) { tr.Mode = ModeEdit })
	if err != nil || res.Session != "5f757d90-12f2-4cbf-bd3b-851e61070cc8" || text(rec.evs) != "La caja está bien." {
		t.Fatalf("antigravity: %+v, %v, %q", res, err, text(rec.evs))
	}
	if !has(f.calls("agy")[0], "--mode", "accept-edits") {
		t.Errorf("agy argv: %v", f.calls("agy")[0])
	}

	// OpenCode: el stream no cierra el turno; el uso sale de `session export`.
	res, err, rec, ws := run(t, context.Background(), f, "opencode", nil)
	if err != nil || res.Session != "ses_ebf3d5642ffesbIf6ikLOXZQDR" || text(rec.evs) != "Listo: renderiza bien." {
		t.Fatalf("opencode: %+v, %v, %q", res, err, text(rec.evs))
	}
	if res.Usage == nil || res.Usage.Input != 4160+4532+20 || res.Model != "opencode/nemotron-3.5-super-vl-free" {
		t.Errorf("uso leído de la sesión: %+v / %q", res.Usage, res.Model)
	}
	if last := rec.evs[len(rec.evs)-1]; last.Kind != KindDone {
		t.Errorf("el finisher cierra el turno: %+v", last)
	}
	calls := f.calls("opencode")
	if len(calls) != 2 || !has(calls[1], "session", "export", "ses_ebf3d5642ffesbIf6ikLOXZQDR", "--standalone") {
		t.Fatalf("llamadas de OpenCode: %v", calls)
	}
	if !has(calls[0], "--agent", "plan") {
		t.Errorf("opencode argv: %v", calls[0])
	}
	cfg := f.read("occ.opencode")
	if !strings.Contains(cfg, `"bash":"deny"`) || !strings.Contains(cfg, "scadlab_create_checkpoint") {
		t.Errorf("OPENCODE_CONFIG_CONTENT: %q", cfg)
	}
	_ = ws
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Cancelar a mitad de turno mata el grupo de procesos entero —también el nieto
// que lanzó el CLI— y conserva lo escrito hasta ahí.
func TestRunCancelKillsTheProcessGroupAndKeepsPartialText(t *testing.T) {
	f := newFakeCLIs(t)
	f.install("claude", "claude-stream.jsonl")
	f.put("hang.claude", "1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotText := make(chan struct{})
	rec := &recorder{}
	done := make(chan struct{})
	var res Result
	var err error
	go func() {
		defer close(done)
		tr := Turn{CLI: "claude", Exe: Resolve("claude"), Cwd: t.TempDir(), Prompt: "x", Lang: "es"}
		res, err = Run(ctx, tr, func(e Event) {
			rec.emit(e)
			if e.Kind == KindDone {
				close(gotText)
			}
		})
	}()
	// El script imprime todo el stream y se queda dormido: esperamos a ver el
	// cierre del stream (y al nieto vivo) antes de cancelar.
	select {
	case <-gotText:
	case <-time.After(5 * time.Second):
		t.Fatal("el stream nunca llegó")
	}
	waitFor(t, "el nieto sleep arrancó", func() bool { return sleeperPID(f, "claude") > 0 })
	pid := sleeperPID(f, "claude")
	if !alive(pid) {
		t.Fatal("el nieto debería estar vivo antes de cancelar")
	}

	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run no volvió tras cancelar")
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancelar tardó %v", time.Since(start))
	}
	if err != nil || !res.Cancelled {
		t.Errorf("cancelar no es un error: %+v, %v", res, err)
	}
	if !strings.Contains(text(rec.evs), "Voy a renderizar") {
		t.Errorf("el texto escrito hasta ahí se conserva: %q", text(rec.evs))
	}
	waitFor(t, "el nieto sleep murió con el grupo", func() bool { return !alive(pid) })
}

func TestRunErrorsAreExplainedByTheCLI(t *testing.T) {
	f := newFakeCLIs(t)
	f.install("claude", "claude-error.jsonl")
	f.install("codex", "")
	f.put("exit.codex", "1")
	f.put("stderr.codex", "error: model gpt-9 does not exist\n")
	f.install("opencode", "opencode-error.jsonl")

	// Un fallo que el CLI explicó con un evento: el error lleva su mensaje.
	_, err, rec, _ := run(t, context.Background(), f, "claude", nil)
	if err == nil || err.Error() != "Not logged in · Please run /login" {
		t.Fatalf("claude: %v", err)
	}
	if len(find(rec.evs, KindError)) != 1 {
		t.Errorf("el evento de error también se reparte: %+v", rec.evs)
	}
	// Murió sin explicarse: el error lleva lo que dijo por stderr.
	_, err, _, _ = run(t, context.Background(), f, "codex", nil)
	if err == nil || !strings.Contains(err.Error(), "model gpt-9 does not exist") {
		t.Fatalf("codex: %v", err)
	}
	// OpenCode con un error en el stream no se cierra como terminado.
	_, err, rec, _ = run(t, context.Background(), f, "opencode", nil)
	if err == nil || !strings.Contains(err.Error(), "Model unavailable") || len(find(rec.evs, KindDone)) != 0 {
		t.Fatalf("opencode: %v / %+v", err, rec.evs)
	}
}

func TestRunWithAbsentCLI(t *testing.T) {
	f := newFakeCLIs(t) // PATH sin ningún CLI
	if Resolve("claude") != "" {
		t.Fatal("no debería haber claude")
	}
	_, err, _, _ := run(t, context.Background(), f, "claude", nil)
	if err == nil {
		t.Fatal("un CLI ausente es un error claro, no un chat mudo")
	}
	if _, err := Run(context.Background(), Turn{CLI: "claude", Exe: "/no/existe/claude", Prompt: "x"}, func(Event) {}); err == nil {
		t.Error("un ejecutable que no existe no arranca")
	}
	if _, err := Run(context.Background(), Turn{CLI: "claude", Exe: "/bin/sh", Prompt: "  "}, func(Event) {}); err == nil {
		t.Error("un mensaje vacío no corre un CLI")
	}
	if _, err := Run(context.Background(), Turn{CLI: "otro", Exe: "/bin/sh", Prompt: "x"}, func(Event) {}); err == nil {
		t.Error("un CLI fuera del catálogo")
	}
}

// Con un shim de Windows el prompt viaja por stdin, no por argv.
func TestRunSendsThePromptByStdinWhenAsked(t *testing.T) {
	f := newFakeCLIs(t)
	f.install("claude", "claude-stream.jsonl")
	f.put("stdin.want.claude", "1")

	_, err, _, _ := run(t, context.Background(), f, "claude", func(tr *Turn) {
		tr.StdinPrompt = true
		tr.Prompt = "línea uno\nlínea dos & \"comillas\" %PATH%"
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range f.calls("claude")[0] {
		if strings.Contains(a, "línea uno") {
			t.Errorf("el prompt no va por argv: %q", a)
		}
	}
	if got := f.read("stdin.claude"); got != "línea uno\nlínea dos & \"comillas\" %PATH%" {
		t.Errorf("stdin: %q", got)
	}
}

func TestDetectionWithFakeCLIsOnPath(t *testing.T) {
	f := newFakeCLIs(t)
	f.install("claude", "claude-stream.jsonl")
	f.install("agy", "antigravity-stream.jsonl")

	list := Default.List(false)
	byID := map[string]Detected{}
	for _, c := range list {
		byID[c.ID] = c
	}
	if len(list) != 4 || !byID["claude"].Installed || !byID["antigravity"].Installed || byID["codex"].Installed || byID["opencode"].Installed {
		t.Fatalf("detección: %+v", list)
	}
	if byID["claude"].Path != filepath.Join(f.dir, "claude") {
		t.Errorf("ruta: %q", byID["claude"].Path)
	}

	// La versión se pide solo de los instalados, en paralelo.
	vers := Default.Versions(context.Background(), list)
	if vers["claude"] != "9.8.7" || vers["antigravity"] != "9.8.7" || len(vers) != 2 {
		t.Errorf("versiones: %v", vers)
	}

}

func TestLoginShellPathIsTheLastResort(t *testing.T) {
	f := newFakeCLIs(t)
	// OpenCode vive en un directorio que solo conoce la shell de login.
	other := t.TempDir()
	bin := filepath.Join(other, "opencode")
	if err := os.WriteFile(bin, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	f.install("claude", "")
	if Resolve("opencode") != "" {
		t.Fatal("con las rutas extra apagadas no debería verse")
	}

	// Encendidas, la shell de login lo encuentra, y solo se la consulta cuando
	// los medios baratos fallan.
	t.Setenv(detect.NoExtraDirsEnv, "")
	t.Setenv("HOME", t.TempDir())
	loginCalls := 0
	defer swapLoginPath(func() string { loginCalls++; return other })()
	claude, _ := Lookup("claude")
	if got := resolveBin(claude); got != filepath.Join(f.dir, "claude") || loginCalls != 0 {
		t.Fatalf("claude está en el PATH: %q, consultas a la shell: %d", got, loginCalls)
	}
	opencode, _ := Lookup("opencode")
	if got := resolveBin(opencode); got != bin || loginCalls == 0 {
		t.Fatalf("opencode = %q, quería %q (PATH de la shell de login)", got, bin)
	}
}
