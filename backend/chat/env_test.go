package chat

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidEnvName(t *testing.T) {
	for _, ok := range []string{"ANTHROPIC_API_KEY", "_x", "a1"} {
		if !ValidEnvName(ok) {
			t.Errorf("%q debería valer", ok)
		}
	}
	for _, bad := range []string{"", "1A", "A B", "A;rm -rf", "A$(x)", "a-b", "ñ"} {
		if ValidEnvName(bad) {
			t.Errorf("%q no debería valer", bad)
		}
	}
}

func TestLookupAPIKeyFromProcessEnv(t *testing.T) {
	t.Setenv("SCADLAB_TEST_KEY", "  sk-1  ")
	if v, ok := LookupAPIKey("SCADLAB_TEST_KEY"); !ok || v != "sk-1" {
		t.Errorf("%q, %v", v, ok)
	}
	if _, ok := LookupAPIKey("A;B"); ok {
		t.Error("un nombre inválido nunca se busca")
	}
}

// Sin la variable en el proceso, se la pide a la shell de login. Se simula con
// un SHELL falso que escribe ruido y después el valor.
func TestLookupAPIKeyFromLoginShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("en Windows las variables de usuario llegan a las apps gráficas")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fakeshell")
	// Recibe: -ilc <script> sh <NOMBRE>.
	script := "#!/bin/sh\necho 'aviso de la shell'\nif [ \"$4\" = \"SCADLAB_ONLY_IN_SHELL\" ]; then echo 'sk-desde-la-shell'; exit 0; fi\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", fake)
	t.Setenv("SCADLAB_ONLY_IN_SHELL", "")
	shellMu.Lock()
	shellCache = map[string]shellEntry{}
	shellMu.Unlock()

	if v, ok := LookupAPIKey("SCADLAB_ONLY_IN_SHELL"); !ok || v != "sk-desde-la-shell" {
		t.Errorf("%q, %v", v, ok)
	}
	if _, ok := LookupAPIKey("SCADLAB_NOPE"); ok {
		t.Error("una variable que la shell no tiene no se inventa")
	}
}
