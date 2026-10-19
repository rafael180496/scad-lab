package main

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain cambia el llavero del SO por uno en memoria para TODO el paquete:
// un test que, sin querer, llegue a la llave del llavero propio (vaultgate)
// nunca debe leer ni escribir el Keychain / Administrador de credenciales real
// del usuario. Los tests que necesitan un llavero caído siguen llamando a
// keyring.MockInitWithError por su cuenta.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
