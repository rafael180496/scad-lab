// Package appdata resuelve dónde guarda ScadLab sus datos por usuario.
//
//	macOS    ~/Library/Application Support/ScadLab/
//	Windows  %AppData%\ScadLab\
//	Linux    ~/.config/ScadLab/
//
// os.UserConfigDir ya resuelve la convención de cada sistema, así que no hay
// ramas por GOOS acá: lo que cambia entre plataformas lo decide la stdlib.
package appdata

import (
	"os"
	"path/filepath"
)

const dirName = "ScadLab"

// OverrideEnv redirige todo el directorio de datos a otra ruta.
//
// Existe por un incidente real en mini-tools (el repo hermano): sus tests
// corrían contra el directorio de datos del usuario y borraban la base antes
// de cada caso. Un test que necesite una base la abre bajo t.TempDir() con
// esta variable puesta; nunca contra el directorio real.
const OverrideEnv = "SCADLAB_DATA_DIR"

// Dir devuelve el directorio de datos, creándolo si hace falta.
func Dir() (string, error) {
	if base := os.Getenv(OverrideEnv); base != "" {
		return base, os.MkdirAll(base, 0o700)
	}
	configBase, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configBase, dirName)
	return dir, os.MkdirAll(dir, 0o700)
}

// DBPath es la base SQLite con workspaces, checkpoints, configuración, skills
// y proveedores de IA.
func DBPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "scadlab.db"), nil
}

// DefaultWorkspacesRoot es la carpeta donde se crean los workspaces cuando el
// usuario no eligió otra: ~/Documents/ScadLab en los dos sistemas. Va en
// Documentos y no en el directorio de datos porque los .scad son del usuario —
// los abre con otras herramientas, los respalda, los versiona con git.
func DefaultWorkspacesRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Documents", dirName)
}
