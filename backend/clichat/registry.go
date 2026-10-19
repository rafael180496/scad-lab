package clichat

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"scadlab/backend/detect"
)

// CLI es una entrada del catálogo: los CLIs agénticos que ScadLab sabe usar como
// proveedores del chat. Es el catálogo de backend/agents de mini-tools
// (Claude Code, Codex, Antigravity, OpenCode).
//
// Decisión de fondo, heredada: un CLI NO se integra por su API, se ejecuta como
// el programa que ya es. Cada uno maneja su propia autenticación (su login por
// navegador, su token, su archivo de configuración); ScadLab no la replica, no la
// lee y no guarda nada de ella. Por eso un CLI no se «configura» en ScadLab: se
// detecta, y si está instalado aparece en el chat.
type CLI struct {
	// ID es el id corto: `claude`, `codex`, `antigravity`, `opencode`. En el
	// chat el proveedor se llama `cli:<id>`.
	ID     string
	Name   string
	Vendor string
	// Bin es el comando; AltBins, nombres anteriores del mismo ejecutable (el
	// Gemini CLI se renombró Antigravity: quien todavía tiene `gemini` lo ve
	// detectado bajo el nombre actual, no como un CLI aparte).
	Bin     string
	AltBins []string
}

var catalog = []CLI{
	{ID: "claude", Name: "Claude Code", Vendor: "Anthropic", Bin: "claude"},
	{ID: "codex", Name: "Codex", Vendor: "OpenAI", Bin: "codex"},
	// El binario se llama `agy`, no «antigravity».
	{ID: "antigravity", Name: "Antigravity", Vendor: "Google", Bin: "agy", AltBins: []string{"gemini"}},
	{ID: "opencode", Name: "OpenCode", Vendor: "SST", Bin: "opencode"},
}

// ProviderPrefix es el prefijo del id de un CLI como proveedor del chat.
const ProviderPrefix = "cli:"

// Catalog devuelve el catálogo, en el orden en que se muestra.
func Catalog() []CLI { return append([]CLI(nil), catalog...) }

// Lookup busca un CLI por su id corto.
func Lookup(id string) (CLI, bool) {
	for _, c := range catalog {
		if c.ID == id {
			return c, true
		}
	}
	return CLI{}, false
}

// ProviderID arma el id de proveedor del chat de un CLI.
func ProviderID(cliID string) string { return ProviderPrefix + cliID }

// ParseProviderID devuelve el id corto si providerID es `cli:<id>` de un CLI del
// catálogo. Un `cli:` desconocido no es un CLI: no hay a qué mandarlo.
func ParseProviderID(providerID string) (string, bool) {
	id, ok := strings.CutPrefix(providerID, ProviderPrefix)
	if !ok {
		return "", false
	}
	_, known := Lookup(id)
	return id, known
}

// IsProviderID dice si providerID tiene la forma de un CLI (conocido o no).
func IsProviderID(providerID string) bool { return strings.HasPrefix(providerID, ProviderPrefix) }

// Detected es un CLI del catálogo resuelto contra este equipo.
type Detected struct {
	CLI
	Installed bool
	Path      string
}

// resolveBin busca el ejecutable: el PATH del proceso, las rutas donde los
// instaladores lo dejan de verdad (backend/detect) y, en macOS y Linux, como
// último recurso, el PATH de la shell de login — una app abierta desde el Finder
// no hereda el de ~/.zshrc, y quien instaló el CLI con nvm, volta o mise lo
// tiene ahí. Devuelve "" si no está instalado.
func resolveBin(c CLI) string {
	if p := detect.CLIPath(c.ID, c.Bin); p != "" {
		return p
	}
	for _, name := range append([]string{c.Bin}, c.AltBins...) {
		if p := detect.FindLogin(name); p != "" {
			return p
		}
	}
	return ""
}

// DetectTTL es cuánto vale una detección. Corta a propósito: un CLI recién
// instalado tiene que aparecer sin reiniciar la app.
const DetectTTL = 30 * time.Second

// versionTTL es cuánto se recuerda la versión de un CLI. Cambia solo al
// actualizarlo, y pedirla arranca el CLI (~1 s si es de node).
const versionTTL = 10 * time.Minute

// Detector detecta los CLIs con una caché corta. Es seguro entre goroutines.
type Detector struct {
	ttl     time.Duration
	now     func() time.Time
	resolve func(CLI) string

	mu       sync.Mutex
	at       time.Time
	list     []Detected
	versions map[string]versionEntry
}

type versionEntry struct {
	version string
	at      time.Time
}

// NewDetector crea un detector. resolve nil usa la detección real (resolveBin).
func NewDetector(ttl time.Duration, resolve func(CLI) string) *Detector {
	if resolve == nil {
		resolve = resolveBin
	}
	return &Detector{ttl: ttl, now: time.Now, resolve: resolve, versions: map[string]versionEntry{}}
}

// Default es el detector de la app.
var Default = NewDetector(DetectTTL, nil)

// List devuelve los CLIs del catálogo con su estado, instalados o no. Sin
// refresh, una detección de menos de ttl se reutiliza; con refresh se vuelve a
// hacer (lo que pide quien abre el selector).
func (d *Detector) List(refresh bool) []Detected {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !refresh && d.list != nil && d.now().Sub(d.at) < d.ttl {
		return append([]Detected(nil), d.list...)
	}
	out := make([]Detected, len(catalog))
	for i, c := range catalog {
		p := d.resolve(c)
		out[i] = Detected{CLI: c, Installed: p != "", Path: p}
	}
	d.list, d.at = out, d.now()
	return append([]Detected(nil), out...)
}

// Get devuelve un CLI del catálogo con su estado.
func (d *Detector) Get(id string, refresh bool) (Detected, bool) {
	for _, c := range d.List(refresh) {
		if c.ID == id {
			return c, true
		}
	}
	return Detected{}, false
}

// Reset olvida la detección y las versiones (las pruebas, al cambiar el PATH).
func (d *Detector) Reset() {
	d.mu.Lock()
	d.list, d.at = nil, time.Time{}
	d.versions = map[string]versionEntry{}
	d.mu.Unlock()
}

// Versions pide la versión de cada CLI instalado, en paralelo y con caché por
// ruta. Un CLI que no la dice (o tarda más del plazo de detect.Version) queda
// con "".
func (d *Detector) Versions(ctx context.Context, list []Detected) map[string]string {
	out := make(map[string]string, len(list))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, c := range list {
		if !c.Installed {
			continue
		}
		d.mu.Lock()
		e, ok := d.versions[c.Path]
		d.mu.Unlock()
		if ok && d.now().Sub(e.at) < versionTTL {
			out[c.ID] = e.version
			continue
		}
		wg.Add(1)
		go func(c Detected) {
			defer wg.Done()
			v := detect.Version(ctx, c.Path)
			d.mu.Lock()
			d.versions[c.Path] = versionEntry{version: v, at: d.now()}
			d.mu.Unlock()
			mu.Lock()
			out[c.ID] = v
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	return out
}

// Resolve devuelve la ruta del ejecutable de un CLI ("" si no está instalado).
func Resolve(id string) string {
	if c, ok := Default.Get(id, false); ok && c.Installed {
		return c.Path
	}
	return ""
}

// Env es el entorno con el que se lanza un CLI: **el del proceso entero**, con el
// PATH ampliado. Que herede todo no es comodidad, es requisito: `cmd.Env`
// REEMPLAZA el entorno del hijo, y sin HOME ninguno de los CLIs encuentra su
// sesión (Claude Code contesta «Not logged in» en una máquina donde el login
// está hecho, Antigravity aborta con «$HOME is not defined»).
//
// El PATH lleva primero el directorio del propio ejecutable —un CLI de node
// instalado con nvm lo encuentra junto a su `node`— y después los directorios de
// instalación habituales, que una app gráfica no hereda.
func Env(exe string, extra ...string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(extra)+1)
	for _, kv := range base {
		if !hasPathPrefix(kv) {
			out = append(out, kv)
		}
	}
	out = append(out, "PATH="+pathWith(exe))
	return append(out, extra...)
}

func hasPathPrefix(kv string) bool {
	if runtime.GOOS == "windows" {
		// Windows no distingue mayúsculas en los nombres de variable (Path).
		return len(kv) > 5 && strings.EqualFold(kv[:5], "PATH=")
	}
	return strings.HasPrefix(kv, "PATH=")
}

func pathWith(exe string) string {
	var parts []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			parts = append(parts, d)
		}
	}
	if exe != "" {
		add(filepath.Dir(exe))
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		add(d)
	}
	for _, d := range detect.ExtraDirs() {
		add(d)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// Launcher convierte la ruta de un ejecutable en el argv con el que se lanza.
//
// Casi siempre es la ruta sola. La excepción es Windows con un shim `.cmd` o
// `.bat` (lo que npm deja para los CLIs de node): CreateProcess no ejecuta un
// archivo por lotes por sí solo, así que hay que pasar por `cmd.exe /c`. Un
// `.ps1` se pasa a PowerShell como archivo. Sigue siendo argv, nunca una línea
// de comandos armada con texto del usuario: el prompt de un shim viaja por stdin
// (ver Turn.usesStdin).
func Launcher(path string) []string {
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".cmd", ".bat":
			shell := os.Getenv("COMSPEC")
			if shell == "" {
				shell = "cmd.exe"
			}
			return []string{shell, "/c", path}
		case ".ps1":
			ps := detect.Find("powershell")
			if ps == "" {
				ps = "powershell.exe"
			}
			return []string{ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path}
		}
	}
	return []string{path}
}

// isShim dice si el ejecutable pasa por un intérprete de Windows (cmd o
// PowerShell): ahí un argumento con saltos de línea o metacaracteres es
// inseguro, y el prompt no puede ir por argv.
func isShim(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat", ".ps1":
		return true
	}
	return false
}
