package agentsessions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Todos los fixtures son INVENTADOS: la forma es la de los archivos reales
// (verificada de solo lectura), el contenido no sale de ninguna sesión.

const (
	claudeID1 = "11111111-1111-4111-8111-111111111111"
	claudeID2 = "22222222-2222-4222-8222-222222222222"
	claudeID3 = "33333333-3333-4333-8333-333333333333"
	codexID1  = "0aaa0001-0000-7000-8000-000000000001"
	codexID2  = "0aaa0002-0000-7000-8000-000000000002"
	codexID3  = "0aaa0003-0000-7000-8000-000000000003"
)

func write(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func ids(s []Session) []string {
	out := []string{}
	for _, x := range s {
		out = append(out, x.ID)
	}
	return out
}

func workspaceDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "mi.caja_rpi")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	// Como lo guarda el CLI: el cwd real del proceso.
	if real, err := filepath.EvalSymlinks(d); err == nil {
		d = real
	}
	return d
}

func TestClaudeDirName(t *testing.T) {
	cases := map[string]string{
		"/Users/maker/Documents/ScadLab/rpi5-case": "-Users-maker-Documents-ScadLab-rpi5-case",
		"/Users/maker/github_project/.claude/wt":   "-Users-maker-github-project--claude-wt",
		`C:\Users\maker\Documents\ScadLab\caja`:    "C--Users-maker-Documents-ScadLab-caja",
		"/Users/maker/Mis Diseños/caja":            "-Users-maker-Mis-Dise-os-caja",
	}
	for in, want := range cases {
		if got := claudeDirName(in); got != want {
			t.Errorf("claudeDirName(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}

func TestListClaude(t *testing.T) {
	home, ws := t.TempDir(), workspaceDir(t)
	proj := filepath.Join(home, ".claude", "projects", claudeDirName(ws))
	now := time.Now()

	// Sesión normal: el primer bloque es contexto del IDE, el segundo es lo
	// que escribió la persona.
	write(t, filepath.Join(proj, claudeID1+".jsonl"),
		`{"type":"ai-title","aiTitle":"Título de la IA"}
{"type":"queue-operation","operation":"enqueue"}
{"type":"user","isSidechain":false,"cwd":`+q(ws)+`,"message":{"role":"user","content":[{"type":"text","text":"<ide_opened_file>algo</ide_opened_file>"},{"type":"text","text":"Haz una caja de 92x64 con tapa deslizante"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}
`, now.Add(-2*time.Hour))
	// Contenido como string, y mucho más reciente.
	write(t, filepath.Join(proj, claudeID2+".jsonl"),
		`{"type":"user","cwd":`+q(ws)+`,"message":{"role":"user","content":"agrega un logo bicolor\ncon dos líneas"}}`+"\n", now.Add(-time.Minute))
	// Misma carpeta codificada, otro cwd real: no es de este workspace.
	write(t, filepath.Join(proj, claudeID3+".jsonl"),
		`{"type":"user","cwd":"/otra/carpeta","message":{"role":"user","content":"ajeno"}}`+"\n", now)
	// Sin mensajes de usuario (solo metadatos), subagente y basura: se saltan.
	write(t, filepath.Join(proj, "44444444-4444-4444-8444-444444444444.jsonl"), `{"type":"file-history-snapshot"}`+"\n", now)
	write(t, filepath.Join(proj, "agent-abc123.jsonl"), `{"type":"user","cwd":`+q(ws)+`,"message":{"content":"sub"}}`+"\n", now)
	write(t, filepath.Join(proj, "notas.txt"), "x", now)
	// Solo título de la IA como respaldo, con línea rota en medio.
	write(t, filepath.Join(proj, "55555555-5555-4555-8555-555555555555.jsonl"),
		`{"type":"ai-title","aiTitle":"Solo el título"}
{rota
{"type":"user","isMeta":true,"cwd":`+q(ws)+`,"message":{"content":"<command-name>/clear</command-name>"}}
`, now.Add(-3*time.Hour))

	got, err := List(context.Background(), Claude, ws, Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{claudeID2, claudeID1, "55555555-5555-4555-8555-555555555555"}
	if !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("sesiones %v, se esperaba %v", ids(got), want)
	}
	if got[0].Title != "agrega un logo bicolor con dos líneas" {
		t.Errorf("título 0 = %q", got[0].Title)
	}
	if got[1].Title != "Haz una caja de 92x64 con tapa deslizante" {
		t.Errorf("título 1 = %q (debía saltar el bloque <ide_opened_file>)", got[1].Title)
	}
	if got[2].Title != "Solo el título" {
		t.Errorf("título 2 = %q (respaldo: aiTitle)", got[2].Title)
	}
	if got[0].UpdatedAt <= got[1].UpdatedAt || got[0].UpdatedAt == 0 {
		t.Errorf("fechas fuera de orden: %+v", got)
	}

	// Límite.
	lim, _ := List(context.Background(), Claude, ws, Options{Home: home, Limit: 1})
	if len(lim) != 1 || lim[0].ID != claudeID2 {
		t.Errorf("Limit 1: %+v", lim)
	}
	// Workspace sin sesiones: lista vacía, no error ni nil.
	none, err := List(context.Background(), Claude, workspaceDir(t), Options{Home: home})
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("sin sesiones: %+v, %v", none, err)
	}
}

func q(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func TestClaudeBigRecordBeforeTheFirstMessage(t *testing.T) {
	home, ws := t.TempDir(), workspaceDir(t)
	proj := filepath.Join(home, ".claude", "projects", claudeDirName(ws))
	// Un registro de ~1,2 MB antes del primer mensaje (un snapshot de archivos):
	// se lee entero, no corta la línea a mitad ni pierde el mensaje que sigue.
	huge := `{"type":"file-history-snapshot","x":"` + strings.Repeat("a", 1200<<10) + `"}` + "\n"
	write(t, filepath.Join(proj, claudeID1+".jsonl"),
		huge+`{"type":"user","cwd":`+q(ws)+`,"message":{"content":"después del registro grande"}}`+"\n", time.Now())
	got, err := List(context.Background(), Claude, ws, Options{Home: home})
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
}

func codexFixture(id, cwd, source, firstUser string) string {
	var b strings.Builder
	b.WriteString(`{"timestamp":"2026-08-14T23:05:54.197Z","type":"session_meta","payload":{"session_id":"` + id + `","id":"` + id + `","cwd":` + q(cwd) + `,"originator":"codex_cli","source":"` + source + `","base_instructions":{"text":"` + strings.Repeat("x", 3000) + `"}}}` + "\n")
	b.WriteString(`{"type":"event_msg","payload":{"type":"task_started"}}` + "\n")
	b.WriteString(`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>x</environment_context>"}]}}` + "\n")
	if firstUser != "" {
		b.WriteString(`{"type":"event_msg","payload":{"type":"user_message","message":` + q(firstUser) + `}}` + "\n")
	}
	return b.String()
}

func TestListCodex(t *testing.T) {
	home, ws := t.TempDir(), workspaceDir(t)
	root := filepath.Join(home, ".codex", "sessions")
	now := time.Now()
	write(t, filepath.Join(root, "2026", "08", "13", "rollout-2026-08-13T10-00-00-"+codexID1+".jsonl"), codexFixture(codexID1, ws, "cli", "Primera sesión de la caja"), now.Add(-48*time.Hour))
	write(t, filepath.Join(root, "2026", "08", "14", "rollout-2026-08-14T09-00-00-"+codexID2+".jsonl"), codexFixture(codexID2, ws, "cli", "Segunda: agregar ventilación"), now.Add(-time.Hour))
	// De otro proyecto, no interactiva, y sin primera línea session_meta.
	write(t, filepath.Join(root, "2026", "08", "14", "rollout-2026-08-14T11-00-00-"+codexID3+".jsonl"), codexFixture(codexID3, "/otro/proyecto", "cli", "ajena"), now)
	write(t, filepath.Join(root, "2026", "08", "14", "rollout-2026-08-14T12-00-00-0aaa0004-0000-7000-8000-000000000004.jsonl"), codexFixture("0aaa0004-0000-7000-8000-000000000004", ws, "exec", "automatizada"), now)
	write(t, filepath.Join(root, "2026", "08", "14", "rollout-2026-08-14T13-00-00-x.jsonl"), `{"type":"turn_context"}`+"\n", now)

	got, err := List(context.Background(), Codex, ws, Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{codexID2, codexID1}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("sesiones %v, se esperaba %v", ids(got), want)
	}
	if got[0].Title != "Segunda: agregar ventilación" {
		t.Errorf("título = %q", got[0].Title)
	}
}

func TestListOpenCode(t *testing.T) {
	ws := workspaceDir(t)
	out := `aviso del servicio
[
  {"id":"ses_aaaaaaaaaaaaAAAAAAAAAAAAAA","title":"Caja con tapa","updated":1792081064548,"created":1792081063292,"projectId":"p","directory":` + q(ws) + `},
  {"id":"ses_bbbbbbbbbbbbBBBBBBBBBBBBBB","updated":1790000000000,"created":1790000000000,"projectId":"p","directory":` + q(ws) + `},
  {"id":"ses_cccccccccccc","title":"de otra carpeta","updated":1793000000000,"created":1,"projectId":"p","directory":"/otra"},
  {"id":"ses_con espacio","title":"id inválido","updated":1,"created":1,"projectId":"p","directory":` + q(ws) + `}
]`
	var gotDir string
	var gotArgs []string
	run := func(_ context.Context, dir, exe string, args ...string) ([]byte, error) {
		gotDir, gotArgs = dir, append([]string{exe}, args...)
		return []byte(out), nil
	}
	got, err := List(context.Background(), OpenCode, ws, Options{Exe: "/bin/opencode", Run: run, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ses_aaaaaaaaaaaaAAAAAAAAAAAAAA", "ses_bbbbbbbbbbbbBBBBBBBBBBBBBB"}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("sesiones %v", ids(got))
	}
	if got[0].Title != "Caja con tapa" || got[0].UpdatedAt != 1792081064548 || got[1].Title != "" {
		t.Errorf("campos: %+v", got)
	}
	if gotDir != ws || strings.Join(gotArgs, " ") != "/bin/opencode session list --format json -n 30" {
		t.Errorf("comando: dir=%q args=%v", gotDir, gotArgs)
	}

	// Errores: sin ejecutable, comando que falla, salida que no es JSON.
	if _, err := List(context.Background(), OpenCode, ws, Options{}); err == nil {
		t.Error("sin ejecutable debía fallar")
	}
	boom := func(context.Context, string, string, ...string) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := List(context.Background(), OpenCode, ws, Options{Exe: "x", Run: boom}); err == nil {
		t.Error("un comando que falla debía fallar")
	}
	junk := func(context.Context, string, string, ...string) ([]byte, error) { return []byte("no es json"), nil }
	if _, err := List(context.Background(), OpenCode, ws, Options{Exe: "x", Run: junk}); err == nil {
		t.Error("una salida ilegible debía fallar")
	}
}

func TestListUnsupportedAgent(t *testing.T) {
	if _, err := List(context.Background(), Antigravity, t.TempDir(), Options{}); err == nil {
		t.Error("Antigravity no tiene listado")
	}
	if CanList(Antigravity) || !CanList(Claude) {
		t.Error("CanList")
	}
}

func TestArgs(t *testing.T) {
	cases := []struct {
		agent, mode, id string
		want            []string
	}{
		{Claude, ModeNew, "", nil},
		{Claude, ModeContinue, "", []string{"-c"}},
		{Claude, ModeResume, claudeID1, []string{"--resume", claudeID1}},
		{Codex, ModeContinue, "", []string{"resume", "--last"}},
		{Codex, ModeResume, codexID1, []string{"resume", codexID1}},
		{OpenCode, ModeContinue, "", []string{"-c"}},
		{OpenCode, ModeResume, "ses_aaaaaaaaaaaaAAAAAAAAAAAAAA", []string{"--session", "ses_aaaaaaaaaaaaAAAAAAAAAAAAAA"}},
		{Antigravity, ModeContinue, "", []string{"--continue"}},
		{Antigravity, ModeNew, "", nil},
	}
	for _, c := range cases {
		got, err := Args(c.agent, c.mode, c.id)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Args(%s,%s,%s) = %v, %v; se esperaba %v", c.agent, c.mode, c.id, got, err, c.want)
		}
	}
	bad := []struct{ agent, mode, id string }{
		{Claude, ModeResume, ""},
		{Claude, ModeResume, "--dangerously-skip-permissions"},
		{Claude, ModeResume, claudeID1 + " --x"},
		{Claude, ModeResume, "$(rm -rf ~)"},
		{Codex, ModeResume, "ses_aaaaaaaaaaaaAAAAAAAAAAAAAA"},
		{OpenCode, ModeResume, claudeID1},
		{OpenCode, ModeResume, "-s"},
		{Antigravity, ModeResume, "abc"},
		{Claude, "otro", ""},
		{"gemini", ModeNew, ""},
	}
	for _, c := range bad {
		if _, err := Args(c.agent, c.mode, c.id); err == nil {
			t.Errorf("Args(%s,%s,%q) debía fallar", c.agent, c.mode, c.id)
		}
	}
}

func TestSamePathCaseRules(t *testing.T) {
	forms := []string{filepath.Clean("/Users/Maker/Caja")}
	got := samePath("/users/maker/caja", forms)
	want := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if got != want {
		t.Errorf("samePath insensible a mayúsculas = %v en %s", got, runtime.GOOS)
	}
	if !samePath("/Users/Maker/Caja/", forms) {
		t.Error("la barra final no debe importar")
	}
}

func TestCleanTitle(t *testing.T) {
	long := strings.Repeat("ñandú ", 60)
	if got := cleanTitle(long); len([]rune(got)) != maxTitleRune || !strings.HasSuffix(got, "…") {
		t.Errorf("título largo: %d runas", len([]rune(got)))
	}
	if got := cleanTitle("  a \n\t b  "); got != "a b" {
		t.Errorf("%q", got)
	}
}
