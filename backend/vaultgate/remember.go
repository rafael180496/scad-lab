package vaultgate

import (
	"errors"
	"time"

	"github.com/zalando/go-keyring"

	"scadlab/backend/i18n"
)

// Identidad de las entradas de ScadLab en el llavero del sistema (Keychain en
// macOS, Credential Manager en Windows, Secret Service en Linux).
const (
	// Service es el mismo que usó T1 (backend/secrets): una sola «carpeta».
	Service = "ScadLab"
	// MasterAccount guarda la LLAVE DERIVADA (no la contraseña) en base64.
	MasterAccount = "master-key"
	// LegacyAccount es la clave de datos aleatoria de T1, previa a la contraseña
	// maestra. Solo se lee para migrar las claves que cifró y se borra.
	LegacyAccount = "model-servers-key"
	// probeAccount no existe nunca: leerla dice si el llavero responde sin
	// tocar ninguna entrada real.
	probeAccount = "probe"
)

// keychainTimeout acota una llamada al llavero. En macOS, si hay que
// desbloquear el llavero de inicio de sesión, el sistema muestra un diálogo y
// `security` espera a que se conteste: un minuto da tiempo a escribir la clave
// del Mac y evita que la app quede colgada si el diálogo se pierde.
const keychainTimeout = 60 * time.Second

// ErrKeychainTimeout: el llavero no contestó a tiempo.
var ErrKeychainTimeout = i18n.New(i18n.Msg{
	ES: "el llavero del sistema no contestó a tiempo",
	EN: "the system keychain did not answer in time",
})

// Backend es lo único que ScadLab le pide al llavero: tres operaciones sobre
// (servicio, cuenta). Existe como interfaz para que las pruebas cuenten cuántas
// veces se lee —el contrato es UNA lectura por sesión— y para simular un
// llavero denegado.
type Backend interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// OSBackend es el llavero real, a través de go-keyring. En macOS go-keyring no
// enlaza Security.framework: ejecuta /usr/bin/security (sin CGO), y por eso la
// entrada queda con `security` como aplicación de confianza —no ScadLab—: la
// firma ad-hoc nueva de cada build no cambia quién tiene acceso. Ver
// claude/19-hermes.md.
func OSBackend() Backend { return osBackend{} }

type osBackend struct{}

func (osBackend) Get(service, account string) (string, error) {
	var v string
	err := withTimeout(func() (err error) { v, err = keyring.Get(service, account); return })
	return v, err
}

func (osBackend) Set(service, account, secret string) error {
	return withTimeout(func() error { return keyring.Set(service, account, secret) })
}

func (osBackend) Delete(service, account string) error {
	return withTimeout(func() error { return keyring.Delete(service, account) })
}

func withTimeout(f func() error) error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(keychainTimeout):
		return ErrKeychainTimeout
	}
}

// isNotFound: la entrada no existe. Es el estado normal (nunca se recordó, o ya
// se olvidó), no un fallo del llavero.
func isNotFound(err error) bool { return errors.Is(err, keyring.ErrNotFound) }
