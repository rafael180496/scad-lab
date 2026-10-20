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
//
// Y con el settings.json de Antigravity (T14): ScadLab AGREGA ahí sus reglas
// `mcp(scadlab/…)`, y una prueba nunca debe leer ni escribir el del usuario. Por
// defecto no hay ruta («no se sabe») y no se toca nada; las que ejercitan los
// permisos ponen la suya, en un directorio temporal.
//
// El mismo binario de pruebas hace de servidor `ScadLab --mcp` cuando lo relanza el
// «ScadLab» de mentira con SCADLAB_TEST_AS_MCP=1 (el patrón de re-ejecución de
// os/exec): así el `agy` de mentira ejecuta las herramientas contra el servidor real,
// el que hace cumplir «Solo consulta».
func TestMain(m *testing.M) {
	if os.Getenv("SCADLAB_TEST_AS_MCP") == "1" && len(os.Args) > 1 && os.Args[1] == "--mcp" {
		os.Exit(runMCP(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	agySettingsPath = func() string { return "" }
	keyring.MockInit()
	agyExec = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "mcp" && args[1] == "list" {
			return "No MCP servers configured.\n", nil
		}
		return "", errors.New("agy real bloqueado en las pruebas: usa withFakeAgy")
	}
	os.Exit(m.Run())
}
