// Package detect encuentra las herramientas externas que ScadLab usa:
// OpenSCAD y los CLIs agénticos (claude, opencode, codex, antigravity).
//
// Una app de escritorio lanzada desde el Finder o el menú Inicio NO hereda el
// PATH de la shell del usuario: en macOS arranca con /usr/bin:/bin:/usr/sbin:/sbin.
// Por eso, además de exec.LookPath, se prueban las rutas donde los instaladores
// de cada herramienta las dejan de verdad. Sin esto, "claude no encontrado"
// sería lo primero que vería quien sí lo tiene instalado.
package detect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"scadlab/backend/appdata"
	"scadlab/backend/scaddownload"
)

// Tool es el resultado de buscar una herramienta.
type Tool struct {
	ID      string `json:"id"`
	Found   bool   `json:"found"`
	Path    string `json:"path"`
	Version string `json:"version"`
	// Source dice de dónde salió (solo OpenSCAD): SourceCustom (la ruta que fijó
	// el usuario), SourceManaged (el que descargó la app) o SourceSystem (PATH o
	// rutas de instalación). "" = no encontrado, o un CLI agéntico.
	Source string `json:"source"`
}

// Orígenes posibles de Tool.Source.
const (
	SourceCustom  = "custom"
	SourceManaged = "managed"
	SourceSystem  = "system"
)

// NoSystemOpenSCADEnv es una variable SOLO para pruebas: con valor "1", OpenSCAD
// ignora el PATH y las rutas de instalación del sistema. Sirve para simular
// «sin OpenSCAD» (y ejercitar el motor integrado y la descarga) sin desinstalar
// el del usuario. La ruta fijada y el OpenSCAD descargado siguen valiendo.
const NoSystemOpenSCADEnv = "SCADLAB_NO_SYSTEM_OPENSCAD"

// extraDirs son directorios de instalación habituales que no están en el PATH
// de una app gráfica.
func extraDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".opencode", "bin"),
		filepath.Join(home, ".claude", "local"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
	}
	switch runtime.GOOS {
	case "darwin":
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
		// nvm: cualquier versión de node instalada.
		if m, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); len(m) > 0 {
			dirs = append(dirs, m...)
		}
	case "windows":
		appData := os.Getenv("APPDATA")
		local := os.Getenv("LOCALAPPDATA")
		dirs = append(dirs,
			filepath.Join(appData, "npm"),
			filepath.Join(local, "Programs"),
			filepath.Join(home, "AppData", "Roaming", "npm"),
		)
	default:
		dirs = append(dirs, "/usr/local/bin", "/usr/bin")
	}
	return dirs
}

// exeNames son los nombres de archivo posibles de un comando en este SO.
func exeNames(cmd string) []string {
	if runtime.GOOS == "windows" {
		return []string{cmd + ".exe", cmd + ".cmd", cmd + ".bat", cmd + ".ps1"}
	}
	return []string{cmd}
}

// Find busca un comando en el PATH y en los directorios habituales.
func Find(cmd string) string {
	if cmd == "" {
		return ""
	}
	if filepath.IsAbs(cmd) {
		if isFile(cmd) {
			return cmd
		}
		return ""
	}
	if p, err := exec.LookPath(cmd); err == nil {
		return p
	}
	for _, d := range extraDirs() {
		for _, n := range exeNames(cmd) {
			p := filepath.Join(d, n)
			if isFile(p) {
				return p
			}
		}
	}
	return ""
}

// openscadCandidates son las rutas de instalación conocidas de OpenSCAD.
func openscadCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return []string{
			"/Applications/OpenSCAD.app/Contents/MacOS/OpenSCAD",
			"/Applications/OpenSCAD-Nightly.app/Contents/MacOS/OpenSCAD",
			filepath.Join(home, "Applications", "OpenSCAD.app", "Contents", "MacOS", "OpenSCAD"),
		}
	case "windows":
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		return []string{
			filepath.Join(pf, "OpenSCAD", "openscad.exe"),
			filepath.Join(pf, "OpenSCAD (Nightly)", "openscad.exe"),
			filepath.Join(pf86, "OpenSCAD", "openscad.exe"),
		}
	}
	return nil
}

// dataDir es el directorio de datos de ScadLab SIN crearlo (appdata.Dir lo
// crea, y detectar herramientas no debe dejar carpetas, menos aún en los tests
// de un usuario real). Misma regla que appdata.Dir, incluida SCADLAB_DATA_DIR.
func dataDir() string {
	if base := os.Getenv(appdata.OverrideEnv); base != "" {
		return base
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cfg, "ScadLab")
}

// OpenSCAD busca el ejecutable de OpenSCAD. Orden: la ruta que eligió el usuario
// en Configuración (`override`, si existe) → el OpenSCAD que descargó la app
// (<datos>/tools/openscad) → el PATH → las rutas de instalación de cada SO. El
// descargado va antes que el del sistema porque es la versión que ScadLab
// verificó (SHA-256); quien quiera otro lo fija en Configuración.
func OpenSCAD(ctx context.Context, override string) Tool {
	t := Tool{ID: "openscad"}
	path, source := "", ""
	if override != "" && isFile(override) {
		path, source = override, SourceCustom
	}
	if path == "" {
		if p := scaddownload.InstalledBinary(dataDir()); p != "" {
			path, source = p, SourceManaged
		}
	}
	if path == "" && os.Getenv(NoSystemOpenSCADEnv) != "1" {
		path = Find("openscad")
		if path == "" {
			for _, c := range openscadCandidates() {
				if isFile(c) {
					path = c
					break
				}
			}
		}
		if path != "" {
			source = SourceSystem
		}
	}
	if path == "" {
		return t
	}
	t.Found, t.Path, t.Source = true, path, source
	t.Version = version(ctx, path, "--version")
	return t
}

// altCommands son nombres anteriores del mismo CLI. Antigravity se llamaba
// Gemini CLI: quien todavía tiene `gemini` lo ve detectado bajo el nombre
// actual (mismo criterio que el catálogo de mini-tools).
var altCommands = map[string][]string{
	"antigravity": {"agy", "gemini"},
}

// CLI busca un CLI agéntico por su comando (o sus nombres anteriores).
func CLI(ctx context.Context, id, command string) Tool {
	t := Tool{ID: id}
	path := Find(command)
	for _, alt := range altCommands[id] {
		if path != "" {
			break
		}
		path = Find(alt)
	}
	if path == "" {
		return t
	}
	t.Found, t.Path = true, path
	t.Version = version(ctx, path, "--version")
	return t
}

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?([.-][0-9A-Za-z]+)*`)

// version corre `<path> --version` con un plazo corto y extrae el número.
// OpenSCAD imprime su versión por stderr, de ahí CombinedOutput.
func version(ctx context.Context, path string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return versionRe.FindString(strings.TrimSpace(string(out)))
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
