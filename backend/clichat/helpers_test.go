package clichat

import (
	"testing"
	"time"

	"scadlab/backend/detect"
)

// has dice si args contiene la secuencia seq seguida.
func has(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		ok := true
		for j, s := range seq {
			if args[i+j] != s {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func hasArg(args []string, a string) bool { return has(args, a) }

// valueOf devuelve el argumento que sigue a flag ("" si no está).
func valueOf(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// waitFor espera hasta 3 s a que cond se cumpla.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no se cumplió a tiempo: %s", what)
}

// swapLoginPath reemplaza la consulta a la shell de login y devuelve cómo deshacerlo.
func swapLoginPath(fn func() string) func() {
	old := detect.LoginShellPath
	detect.LoginShellPath = fn
	return func() { detect.LoginShellPath = old }
}
