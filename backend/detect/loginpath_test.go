//go:build !windows

package detect

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// FindLogin busca en los directorios que devuelve la shell de login, y solo si
// las rutas extra no están apagadas.
func TestFindLoginSearchesTheLoginShellPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "migrate-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := LoginShellPath
	defer func() { LoginShellPath = old }()
	LoginShellPath = func() string { return "/no/existe" + string(os.PathListSeparator) + dir }

	if got := FindLogin("migrate-cli"); got != bin {
		t.Fatalf("FindLogin = %q, quería %q", got, bin)
	}
	if got := FindLogin("otro"); got != "" {
		t.Errorf("no está: %q", got)
	}
	t.Setenv(NoExtraDirsEnv, "1")
	if got := FindLogin("migrate-cli"); got != "" {
		t.Errorf("con NoExtraDirsEnv no se mira la shell de login: %q", got)
	}
	if Find("migrate-cli") != "" {
		t.Error("ni Find sale del PATH")
	}
}

// La consulta real a la shell: se simula con un script propio que imprime ruido
// antes y después del valor entre marcas (una shell interactiva real lo hace) y
// que cuenta cuántas veces lo llamaron.
func TestDefaultLoginShellPathParsesAndCaches(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	shell := filepath.Join(dir, "fakeshell")
	script := "#!/bin/sh\necho x >> '" + count + "'\necho 'bienvenido a la shell'\nprintf '__SL_PATH__/uno:/dos__SL_PATH__'\necho 'chau'\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)

	loginMu.Lock()
	loginAt = zeroTime()
	loginMu.Unlock()

	if got := defaultLoginShellPath(); got != "/uno:/dos" {
		t.Fatalf("PATH = %q", got)
	}
	if got := defaultLoginShellPath(); got != "/uno:/dos" {
		t.Fatalf("PATH (caché) = %q", got)
	}
	raw, _ := os.ReadFile(count)
	if n := len(raw) / 2; n != 1 {
		t.Errorf("la shell se abrió %d veces; se recuerda un rato", n)
	}
}

func zeroTime() (t time.Time) { return }
