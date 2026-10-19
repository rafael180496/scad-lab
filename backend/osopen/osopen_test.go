package osopen

import (
	"path/filepath"
	"strings"
	"testing"
)

// Una ruta que no existe es un error claro y no lanza nada: antes `open` se
// ejecutaba igual y su fallo no llegaba a nadie. (Una ruta que existe no se
// prueba: abriría el Finder del usuario.)
func TestOpenMissingPathFailsWithoutLaunching(t *testing.T) {
	err := Open(filepath.Join(t.TempDir(), "no", "existe"))
	if err == nil {
		t.Fatal("abrir una ruta inexistente debería fallar")
	}
	if !strings.Contains(err.Error(), "existe") && !strings.Contains(err.Error(), "exist") {
		t.Errorf("el mensaje no explica el problema: %v", err)
	}
}
