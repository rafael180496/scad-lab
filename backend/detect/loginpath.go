package detect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// El PATH de la shell de login. Una app gráfica de macOS arranca con
// /usr/bin:/bin:/usr/sbin:/sbin; el PATH completo del usuario (el de nvm, volta,
// pnpm, mise…) solo lo arma su shell al leer ~/.zprofile y ~/.zshrc. Un CLI
// instalado en un directorio que ninguna lista fija conoce solo se encuentra
// preguntándole a esa shell.
//
// Es un último recurso y es perezoso: solo se consulta cuando un CLI no se
// encontró por los medios baratos, y se recuerda un rato, porque abrir una shell
// de login interactiva con un .zshrc pesado puede tardar un segundo o más.

const (
	loginPathTTL     = 10 * time.Minute
	loginPathFailTTL = 2 * time.Minute
	loginPathTimeout = 5 * time.Second
)

// LoginShellPath devuelve el PATH de la shell de login. Es variable para que las
// pruebas no abran la shell real del usuario.
var LoginShellPath = defaultLoginShellPath

var (
	loginMu   sync.Mutex
	loginAt   time.Time
	loginPath string
	loginTTL  time.Duration
)

var loginMarkRe = regexp.MustCompile(`__SL_PATH__(.*?)__SL_PATH__`)

func defaultLoginShellPath() string {
	if runtime.GOOS == "windows" {
		// Las variables de usuario sí llegan a las apps gráficas de Windows.
		return ""
	}
	loginMu.Lock()
	if !loginAt.IsZero() && time.Since(loginAt) < loginTTL {
		p := loginPath
		loginMu.Unlock()
		return p
	}
	loginMu.Unlock()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginPathTimeout)
	defer cancel()
	// Marcas alrededor del valor: una shell interactiva puede imprimir avisos
	// (motd, oh-my-zsh) antes y después.
	cmd := exec.CommandContext(ctx, shell, "-ilc", `printf '__SL_PATH__%s__SL_PATH__' "$PATH"`)
	cmd.Stdin = nil
	out, err := cmd.Output() // stderr se descarta
	path := ""
	if err == nil {
		if m := loginMarkRe.FindSubmatch(out); m != nil {
			path = strings.TrimSpace(string(m[1]))
		}
	}
	loginMu.Lock()
	loginAt, loginPath = time.Now(), path
	loginTTL = loginPathTTL
	if path == "" {
		loginTTL = loginPathFailTTL
	}
	loginMu.Unlock()
	return path
}

// FindLogin busca cmd en los directorios del PATH de la shell de login. "" si
// no está o si la shell no se pudo consultar. Llamarla es caro la primera vez:
// úsese solo después de Find.
func FindLogin(cmd string) string {
	if cmd == "" || os.Getenv(NoExtraDirsEnv) == "1" {
		return ""
	}
	path := LoginShellPath()
	if path == "" {
		return ""
	}
	for _, d := range filepath.SplitList(path) {
		if d == "" {
			continue
		}
		for _, n := range exeNames(cmd) {
			p := filepath.Join(d, n)
			if isFile(p) {
				return p
			}
		}
	}
	return ""
}
