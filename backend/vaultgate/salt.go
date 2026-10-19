package vaultgate

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"scadlab/backend/i18n"
)

const (
	// SaltFile es el nombre del archivo del salt dentro del directorio de datos.
	SaltFile = "vault.salt"
	saltSize = 16
)

// ErrSaltMissing: hay contraseña maestra en la base pero falta vault.salt (se
// copió la base sin el archivo, o se borró). Sin el salt la llave no se puede
// derivar, y decir «contraseña incorrecta» mandaría a probar una que es buena.
var ErrSaltMissing = i18n.New(i18n.Msg{
	ES: "falta el archivo vault.salt de la carpeta de datos, así que la contraseña maestra no puede abrir las claves guardadas: restablece el llavero y vuelve a escribir las claves",
	EN: "the vault.salt file is missing from the data folder, so the master password cannot open the saved keys: reset the keychain and type the keys again",
})

// SaltPath es la ruta de vault.salt dentro de dir.
func SaltPath(dir string) string { return filepath.Join(dir, SaltFile) }

// CreateSalt genera un salt nuevo de 16 bytes y lo guarda con permisos 0600
// (reemplaza el que hubiera: solo se llama al crear la contraseña, cuando
// ningún verificador depende de él). El salt no es secreto —solo tiene que ser
// estable en esta instalación para que la misma contraseña derive la misma
// llave—, pero tampoco hay motivo para que lo lea otro usuario del equipo.
func CreateSalt(dir string) ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "vaultgate: generando el salt: %w", EN: "vaultgate: generating salt: %w"}, err)
	}
	// Escritura atómica: un salt a medias dejaría una contraseña que nunca abre.
	tmp, err := os.CreateTemp(dir, SaltFile+".*")
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "vaultgate: escribiendo el salt: %w", EN: "vaultgate: writing salt: %w"}, err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(salt)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o600)
	}
	if werr == nil {
		werr = os.Rename(name, SaltPath(dir))
	}
	if werr != nil {
		_ = os.Remove(name)
		return nil, i18n.Errorf(i18n.Msg{ES: "vaultgate: escribiendo el salt: %w", EN: "vaultgate: writing salt: %w"}, werr)
	}
	return salt, nil
}

// LoadSalt lee el salt; ErrSaltMissing si no existe o no tiene el tamaño.
func LoadSalt(dir string) ([]byte, error) {
	data, err := os.ReadFile(SaltPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrSaltMissing
	}
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "vaultgate: leyendo el salt: %w", EN: "vaultgate: reading salt: %w"}, err)
	}
	if len(data) != saltSize {
		return nil, ErrSaltMissing
	}
	return data, nil
}

// RemoveSalt borra vault.salt; que no exista no es un error.
func RemoveSalt(dir string) error {
	if err := os.Remove(SaltPath(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return i18n.Errorf(i18n.Msg{ES: "vaultgate: borrando el salt: %w", EN: "vaultgate: deleting salt: %w"}, err)
	}
	return nil
}
