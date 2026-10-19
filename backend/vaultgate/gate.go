// Package vaultgate es el llavero propio de ScadLab (S12, 12.10): la contraseña
// maestra que protege las claves de API de los servidores de modelos.
//
// Es el vaultgate de mini-tools (gate en memoria, salt en archivo, verificador
// cifrado, «recordar» en el llavero del SO) con dos diferencias a propósito:
//
//   - **No hay pantalla de bloqueo al arrancar.** ScadLab no tiene nada que
//     proteger de entrada: la contraseña se crea la primera vez que se guarda
//     una clave de API, y se pide después solo cuando algo necesita descifrar y
//     la llave no se pudo recuperar sola.
//   - **La llave se lee del llavero una sola vez y perezosa** (la primera vez
//     que algo necesita cifrar o descifrar), no al arrancar: abrir la app sin
//     usar un proveedor con clave nunca toca el llavero ni muestra un aviso del
//     sistema.
//
// La contraseña y la llave derivada nunca se guardan en la base ni salen en un
// log, un error o un JSON hacia la interfaz. La base solo tiene un verificador
// (un texto conocido cifrado con la llave): si abre, la contraseña es la buena.
package vaultgate

import (
	"sync"

	"scadlab/backend/crypto"
	"scadlab/backend/i18n"
)

// Códigos estables de los errores ante los que la interfaz reacciona (ver
// i18n.NewCoded): abrir el diálogo de crear o de desbloquear y reintentar.
const (
	CodeLocked        = "vault-locked"
	CodeUninitialized = "vault-uninitialized"
	CodeWrongPassword = "vault-wrong-password"
)

// ErrLocked: hay contraseña maestra pero la llave no está disponible —no se
// recordó, o el llavero del sistema la negó— y hay que escribir la contraseña.
var ErrLocked = i18n.NewCoded(CodeLocked, i18n.Msg{
	ES: "las claves de API están bloqueadas: escribe la contraseña maestra para usarlas",
	EN: "the API keys are locked: type the master password to use them",
})

// ErrNotInitialized: todavía no se creó la contraseña maestra.
var ErrNotInitialized = i18n.NewCoded(CodeUninitialized, i18n.Msg{
	ES: "primero crea la contraseña maestra que protege las claves de API",
	EN: "first create the master password that protects the API keys",
})

// ErrWrongPassword: la contraseña no abre el verificador.
var ErrWrongPassword = i18n.NewCoded(CodeWrongPassword, i18n.Msg{
	ES: "contraseña maestra incorrecta",
	EN: "wrong master password",
})

// Gate guarda la llave derivada en memoria mientras la app está desbloqueada.
// No hay atajo: todo lo que necesita la llave pasa por Key y falla cerrado
// (ErrLocked) hasta que alguien la pone con Set. Es seguro entre goroutines.
type Gate struct {
	mu  sync.Mutex
	key []byte
}

// NewGate crea un Gate bloqueado.
func NewGate() *Gate { return &Gate{} }

// Set guarda una copia de key y desbloquea. Pone a cero la llave anterior.
func (g *Gate) Set(key []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	crypto.Zero(g.key)
	g.key = append([]byte(nil), key...)
}

// Key devuelve una copia de la llave —quien la use la pone a cero al terminar,
// sin pisar la que sigue en uso por otro— o ErrLocked.
func (g *Gate) Key() ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.key == nil {
		return nil, ErrLocked
	}
	return append([]byte(nil), g.key...), nil
}

// Unlocked dice si el Gate tiene una llave.
func (g *Gate) Unlocked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.key != nil
}

// Lock pone a cero la llave en memoria y vuelve a bloquear.
func (g *Gate) Lock() {
	g.mu.Lock()
	defer g.mu.Unlock()
	crypto.Zero(g.key)
	g.key = nil
}
