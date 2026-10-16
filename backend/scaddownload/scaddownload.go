// Package scaddownload descarga e instala OpenSCAD desde openscad.org en
// <datos>/tools/openscad/<versión>, para quien no lo tiene instalado.
//
// La versión es un snapshot FIJADO (2025.12.30) y cada archivo lleva su SHA-256
// en el código: ScadLab no ejecuta nada que no coincida con lo que el
// mantenedor comprobó. Lo descargado no lo distribuye ScadLab (lo baja el
// usuario de openscad.org) y se instala sin modificar.
//
// Una instalación es todo o nada: se descarga y se extrae a una carpeta
// temporal hermana de la final y se renombra de una vez. Una descarga
// cancelada o fallida no deja nada, y una instalación anterior sigue intacta
// hasta el último momento. Sin CGO y sin golang.org/x/sys: solo la stdlib.
package scaddownload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"scadlab/backend/i18n"
)

// Version es la versión de OpenSCAD que se descarga (la misma del motor
// integrado, sin el sufijo wasm).
const Version = "2025.12.30"

// baseURL es donde openscad.org publica los snapshots.
const baseURL = "https://files.openscad.org/snapshots/"

// Tipos de archivo que sabe instalar Install.
const (
	KindDMG = "dmg"
	KindZIP = "zip"
)

// Fases de Progress.
const (
	PhaseDownload = "download"
	PhaseVerify   = "verify"
	PhaseExtract  = "extract"
	PhaseDone     = "done"
)

// Asset es un archivo descargable para una plataforma.
type Asset struct {
	OS     string // GOOS
	Arch   string // GOARCH
	URL    string
	SHA256 string // hex en minúscula, de openscad.org (.sha256)
	Kind   string // KindDMG | KindZIP
	Size   int64  // bytes; se usa para el espacio libre y como total si el servidor no lo dice
}

// Progress es el avance de Install. En la fase extract de un dmg no hay forma
// de medir (ditto no informa): Total vale 0.
type Progress struct {
	Phase    string
	Received int64
	Total    int64
}

// assets es la tabla fija. El dmg de macOS es un solo archivo para arm64 y
// amd64 (build universal), y el zip de Windows es solo x86-64.
var assets = []Asset{
	{
		OS: "darwin", Arch: "arm64",
		URL:    baseURL + "OpenSCAD-" + Version + ".dmg",
		SHA256: "fbc17e223979884fc17a4bc19b76a50c8797646cbc2f3083cd407389d90a1060",
		Kind:   KindDMG, Size: 62687889,
	},
	{
		OS: "darwin", Arch: "amd64",
		URL:    baseURL + "OpenSCAD-" + Version + ".dmg",
		SHA256: "fbc17e223979884fc17a4bc19b76a50c8797646cbc2f3083cd407389d90a1060",
		Kind:   KindDMG, Size: 62687889,
	},
	{
		OS: "windows", Arch: "amd64",
		URL:    baseURL + "OpenSCAD-" + Version + "-x86-64.zip",
		SHA256: "f04d315b1f1fbe20b363383f45ea8ed65713fe9913b4368d90a94175b71423cb",
		Kind:   KindZIP, Size: 41511427,
	},
}

// ForPlatform devuelve el archivo para esa plataforma, o false si no hay
// (Linux, Windows arm64…).
func ForPlatform(goos, goarch string) (Asset, bool) {
	for _, a := range assets {
		if a.OS == goos && a.Arch == goarch {
			return a, true
		}
	}
	return Asset{}, false
}

// ErrUnsupported: no hay descarga para esta plataforma.
var ErrUnsupported = i18n.New(i18n.Msg{
	ES: "no hay una descarga de OpenSCAD para este sistema; instálalo desde openscad.org",
	EN: "there is no OpenSCAD download for this system; install it from openscad.org",
})

// ErrChecksum: el archivo bajado no coincide con el SHA-256 fijado.
var ErrChecksum = i18n.New(i18n.Msg{
	ES: "el archivo descargado no coincide con el SHA-256 esperado; no se instaló. Vuelve a intentarlo",
	EN: "the downloaded file does not match the expected SHA-256; nothing was installed. Try again",
})

// ErrNoSpace: no hay espacio libre para descargar e instalar.
var ErrNoSpace = i18n.New(i18n.Msg{ES: "no hay espacio suficiente en el disco", EN: "not enough disk space"})

// ErrNetwork: no se pudo hablar con el servidor.
var ErrNetwork = i18n.New(i18n.Msg{ES: "no se pudo descargar OpenSCAD", EN: "could not download OpenSCAD"})

// ErrHTTP: el servidor contestó algo distinto de 200.
var ErrHTTP = i18n.New(i18n.Msg{ES: "el servidor rechazó la descarga", EN: "the server refused the download"})

// kindError agrega a un error con texto propio una identidad para errors.Is
// (las pruebas y la interfaz distinguen «sin espacio» de «sin red» sin leer
// el texto, que cambia con el idioma).
type kindError struct {
	kind error
	err  error
}

func (e kindError) Error() string        { return e.err.Error() }
func (e kindError) Unwrap() error        { return e.err }
func (e kindError) Is(target error) bool { return target == e.kind }

func kindf(kind error, m i18n.Msg, args ...any) error {
	return kindError{kind: kind, err: i18n.Errorf(m, args...)}
}

// toolsDir es <datos>/tools/openscad, donde viven todas las versiones.
func toolsDir(dataDir string) string { return filepath.Join(dataDir, "tools", "openscad") }

// InstallDir es la carpeta de esta versión: <datos>/tools/openscad/2025.12.30.
func InstallDir(dataDir string) string { return filepath.Join(toolsDir(dataDir), Version) }

// InstalledBinary es la ruta del ejecutable de OpenSCAD descargado, o "" si no
// hay una instalación completa. Como la carpeta final aparece con un renombre
// atómico, si existe está entera.
func InstalledBinary(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return findBinary(InstallDir(dataDir), runtime.GOOS)
}

// findBinary busca el ejecutable de OpenSCAD dentro de una carpeta instalada.
// Va por el SO del archivo y no por runtime.GOOS para poder probar el zip de
// Windows desde cualquier máquina.
func findBinary(dir, goos string) string {
	switch goos {
	case "darwin":
		p := filepath.Join(dir, "OpenSCAD.app", "Contents", "MacOS", "OpenSCAD")
		if isFile(p) {
			return p
		}
	case "windows":
		// El zip oficial lleva una carpeta raíz (OpenSCAD-<versión>-x86-64/);
		// se busca hasta dos niveles para no depender de cómo la nombre el
		// mantenedor. Es openscad.exe, igual que el instalado en Archivos de
		// programa. El zip trae también openscad.com (lanzador de consola); no
		// se usa porque, si es un proceso aparte que lanza al .exe, cancelar un
		// render mataría solo al lanzador. Sin verificar en una Windows real.
		for _, pat := range []string{"openscad.exe", "*/openscad.exe", "*/*/openscad.exe"} {
			m, _ := filepath.Glob(filepath.Join(dir, pat))
			for _, p := range m {
				if isFile(p) {
					return p
				}
			}
		}
	}
	return ""
}

// Remove borra todo lo que ScadLab descargó de OpenSCAD (todas las versiones y
// temporales sueltos). No es error que no haya nada.
func Remove(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	dir := toolsDir(dataDir)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return i18n.Errorf(i18n.Msg{
			ES: "no se pudo quitar OpenSCAD (¿está en uso?): %v",
			EN: "could not remove OpenSCAD (is it in use?): %v",
		}, err)
	}
	return nil
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// humanBytes es "62 MB" / "1,2 GB" (sin locale: lo ve una persona en un error).
func humanBytes(n int64) string {
	const mb = 1 << 20
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%d MB", (n+mb/2)/mb)
}

func lowerHex(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
