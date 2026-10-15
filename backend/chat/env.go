package chat

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ValidEnvName dice si name sirve como nombre de variable de entorno. Se
// valida antes de tocar el entorno o pasárselo a una shell: el nombre viene de
// la configuración del proveedor y no tiene por qué ser inocente.
func ValidEnvName(name string) bool { return envNameRe.MatchString(name) }

// LookupAPIKey lee la clave de API de la variable de entorno que nombra el
// proveedor (apiKeyEnv). La clave solo vive en el entorno del usuario: ScadLab
// no la guarda, no la loguea y no la manda a la interfaz.
//
// Una app gráfica de macOS o Linux lanzada desde el Finder o un lanzador NO
// hereda lo que la shell exporta en ~/.zshrc o ~/.profile —el mismo motivo por
// el que backend/detect busca los CLIs en rutas fijas—, así que una clave
// puesta ahí sería invisible aunque en la terminal funcione. Si no está en el
// proceso, se la pide una vez a la shell de login del usuario. En Windows las
// variables de usuario sí llegan a las apps gráficas y no hace falta.
func LookupAPIKey(name string) (string, bool) {
	if !ValidEnvName(name) {
		return "", false
	}
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v, true
	}
	if runtime.GOOS == "windows" {
		return "", false
	}
	return loginShellEnv(name)
}

type shellEntry struct {
	value string
	at    time.Time
}

var (
	shellMu    sync.Mutex
	shellCache = map[string]shellEntry{}
)

// shellEnvTTL es cuánto se recuerda lo que contestó la shell: abrir una shell
// de login interactiva puede tardar un segundo si el .zshrc es pesado, y no
// debe pagarse en cada mensaje. Lo hallado se recuerda un rato; lo que no
// está, no (así exportar la variable y reintentar funciona sin reiniciar).
const shellEnvTTL = 10 * time.Minute

func loginShellEnv(name string) (string, bool) {
	shellMu.Lock()
	e, ok := shellCache[name]
	shellMu.Unlock()
	if ok && time.Since(e.at) < shellEnvTTL {
		return e.value, true
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// El nombre va como argumento posicional, nunca dentro del script.
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printenv "$1"`, "sh", name)
	cmd.Stdin = nil
	out, err := cmd.Output() // stderr se descarta: no se lee ni se loguea
	if err != nil {
		return "", false
	}
	// Una shell interactiva puede escribir avisos antes: el valor es la última
	// línea con contenido.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	v := strings.TrimSpace(lines[len(lines)-1])
	if v == "" {
		return "", false
	}
	shellMu.Lock()
	shellCache[name] = shellEntry{value: v, at: time.Now()}
	shellMu.Unlock()
	return v, true
}
