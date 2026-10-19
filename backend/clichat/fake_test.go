//go:build !windows

package clichat

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"scadlab/backend/detect"
)

// Los CLIs de mentira: scripts en un PATH temporal que imprimen el NDJSON de un
// archivo de testdata (copiado de la forma de las corridas reales de mini-tools,
// con ids y rutas inventados) y registran con qué argumentos y en qué carpeta los
// llamaron. NINGUNA prueba lanza un CLI de verdad: ni una sesión ni `--version`.

// fakeScript es el CLI de mentira. Un solo script para los cuatro: su
// comportamiento sale de los archivos que la prueba deja junto a él.
//
//	stream.<n>   lo que imprime
//	hang.<n>     si existe, imprime y se queda dormido (un nieto `sleep` incluido)
//	exit.<n>     código de salida
//	export.<n>   lo que contesta `session export …` (OpenCode)
//	models.<n>   lo que contesta `models` (Antigravity)
const fakeScript = `#!/bin/sh
n=$(basename "$0")
d="$FAKE_DIR"
if [ "$1" = "--version" ]; then echo "$n 9.8.7 (de mentira)"; exit 0; fi
{ printf '<<CALL>>\0'; for a in "$@"; do printf '%s\0' "$a"; done; } >> "$d/argv.$n"
pwd > "$d/cwd.$n"
printenv OPENCODE_CONFIG_CONTENT > "$d/occ.$n" 2>/dev/null
if [ -f "$d/stdin.want.$n" ]; then cat > "$d/stdin.$n"; fi
if [ "$1" = "session" ] && [ -f "$d/export.$n" ]; then cat "$d/export.$n"; exit 0; fi
if [ "$1" = "models" ] && [ -f "$d/models.$n" ]; then cat "$d/models.$n"; exit 0; fi
if [ -f "$d/hang.$n" ]; then
  cat "$d/stream.$n"
  sleep 60 &
  echo $! > "$d/sleeper.$n"
  wait
  exit 0
fi
[ -f "$d/stream.$n" ] && cat "$d/stream.$n"
if [ -f "$d/exit.$n" ]; then
  [ -f "$d/stderr.$n" ] && cat "$d/stderr.$n" >&2
  exit "$(cat "$d/exit.$n")"
fi
exit 0
`

type fakeCLIs struct {
	t   *testing.T
	dir string
}

// newFakeCLIs aísla la prueba: PATH con solo los CLIs de mentira (y las
// herramientas del sistema que usan sus scripts), HOME temporal, sin rutas de
// instalación ni shell de login, detector vacío.
func newFakeCLIs(t *testing.T) *fakeCLIs {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("los CLIs de mentira son scripts sh")
	}
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv(detect.NoExtraDirsEnv, "1")
	oldLogin := detect.LoginShellPath
	detect.LoginShellPath = func() string { return "" }
	Default.Reset()
	ResetModelsCache()
	t.Cleanup(func() {
		detect.LoginShellPath = oldLogin
		Default.Reset()
		ResetModelsCache()
	})
	return &fakeCLIs{t: t, dir: dir}
}

// install pone el CLI `bin` en el PATH imprimiendo testdata/<stream>.
func (f *fakeCLIs) install(bin, stream string) string {
	f.t.Helper()
	path := filepath.Join(f.dir, bin)
	if err := os.WriteFile(path, []byte(fakeScript), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if stream != "" {
		raw, err := os.ReadFile(filepath.Join("testdata", stream))
		if err != nil {
			f.t.Fatal(err)
		}
		f.put("stream."+bin, string(raw))
	}
	Default.Reset()
	return path
}

func (f *fakeCLIs) put(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeCLIs) read(name string) string {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		return ""
	}
	return string(raw)
}

// calls son los argv con que se llamó al CLI, en orden (sin el ejecutable).
func (f *fakeCLIs) calls(bin string) [][]string {
	f.t.Helper()
	var out [][]string
	for _, chunk := range strings.Split(f.read("argv."+bin), "<<CALL>>\x00") {
		if chunk == "" {
			continue
		}
		args := strings.Split(strings.TrimSuffix(chunk, "\x00"), "\x00")
		if len(args) == 1 && args[0] == "" {
			args = nil
		}
		out = append(out, args)
	}
	return out
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func sleeperPID(f *fakeCLIs, bin string) int {
	raw := strings.TrimSpace(f.read("sleeper." + bin))
	pid, _ := strconv.Atoi(raw)
	return pid
}
