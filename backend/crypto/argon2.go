package crypto

import "golang.org/x/crypto/argon2"

// Parámetros de Argon2id pensados para una derivación interactiva, una vez por
// desbloqueo (los mismos que usa mini-tools).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	// KeySize son los bytes de la llave derivada (AES-256).
	KeySize = 32
)

// DeriveKey deriva una llave AES-256 de 32 bytes de la contraseña maestra y del
// salt de esta instalación con Argon2id. Es deliberadamente lenta (~100–300 ms):
// de eso depende que adivinar la contraseña con la base y el salt en la mano
// cueste.
func DeriveKey(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, KeySize)
}
