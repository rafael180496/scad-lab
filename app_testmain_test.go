package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain cambia el llavero del SO por uno en memoria para TODO el paquete:
// un test que, sin querer, llegue a la llave del llavero propio (vaultgate)
// nunca debe leer ni escribir el Keychain / Administrador de credenciales real
// del usuario. Los tests que necesitan un llavero caído siguen llamando a
// keyring.MockInitWithError por su cuenta.
//
// Con `agy` pasa lo mismo (S12.c): ScadLab ahora registra su servidor MCP en la
// configuración GLOBAL de Antigravity (~/.gemini) al elegirlo en el chat, así que
// un test que llegue ahí sin querer tocaría la del usuario. Por defecto `agy` es
// un doble que no modifica nada: `mcp list` dice que no hay servidores y
// cualquier otra cosa falla. Los tests que ejercitan esa ruta usan withFakeAgy.
func TestMain(m *testing.M) {
	keyring.MockInit()
	agyExec = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "mcp" && args[1] == "list" {
			return "No MCP servers configured.\n", nil
		}
		return "", errors.New("agy real bloqueado en las pruebas: usa withFakeAgy")
	}
	os.Exit(m.Run())
}
