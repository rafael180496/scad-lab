package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// La caché guarda <clave>.stl + <clave>.json (el Result) en cacheDir. La clave
// depende del CONTENIDO, no de la ruta ni de la fecha: un checkpoint es
// inmutable, pero sus include/use no (lib/ se edita), y una malla vieja servida
// porque el .scad no cambió sería justo el error que nadie sabe explicar.

// defaultMaxCacheBytes es el tope de la caché: 1 GB. Un STL de una pieza
// detallada pesa decenas de MB y los checkpoints se acumulan; sin tope la
// carpeta de datos crece para siempre sin que el usuario lo vea.
const defaultMaxCacheBytes int64 = 1 << 30

// staleTempAge: un temporal de render más viejo que esto es de un proceso que
// murió a mitad (cierre forzado); ningún render vivo dura tanto con el tope
// máximo de configuración (3600 s).
const staleTempAge = 2 * time.Hour

var (
	depRe          = regexp.MustCompile(`\b(?:include|use)\s*<([^>]+)>`)
	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentRe  = regexp.MustCompile(`//[^\n]*`)
)

// contentHash es el sha256 del archivo más el de cada include/use resuelto
// recursivamente. Busca cada dependencia relativa al archivo que la nombra,
// al workspace y a <workspace>/lib, en ese orden (los mismos lugares donde la
// buscaría OpenSCAD para un workspace de ScadLab). Una dependencia que no se
// encuentra entra en el hash por nombre: si después aparece, la clave cambia.
func contentHash(workspaceDir, fileName string, main []byte) string {
	h := sha256.New()
	visited := map[string]bool{}
	var walk func(path string, data []byte)
	walk = func(path string, data []byte) {
		h.Write([]byte("\x00file\x00"))
		h.Write(skipHeader(data))
		for _, dep := range findDeps(data) {
			resolved := resolveDep(workspaceDir, filepath.Dir(path), dep)
			if resolved == "" {
				h.Write([]byte("\x00missing\x00" + dep))
				continue
			}
			if visited[resolved] {
				// Un include circular o repetido entra una sola vez: OpenSCAD
				// tampoco lo expande dos veces con use<>, y así el hash termina.
				h.Write([]byte("\x00seen\x00" + dep))
				continue
			}
			visited[resolved] = true
			b, err := os.ReadFile(resolved)
			if err != nil {
				h.Write([]byte("\x00unreadable\x00" + dep))
				continue
			}
			h.Write([]byte("\x00dep\x00" + dep))
			walk(resolved, b)
		}
	}
	mainPath := filepath.Clean(filepath.Join(workspaceDir, fileName))
	visited[mainPath] = true
	walk(mainPath, main)
	return hex.EncodeToString(h.Sum(nil))
}

var checkpointHeaderRe = regexp.MustCompile(`^\s*//\s*@checkpoint\b`)

// skipHeader quita la línea `// @checkpoint parent=… desc="…"` del contenido
// que entra en el hash. Es un comentario: no cambia la geometría, y el padre
// SÍ puede cambiar sin que el modelo cambie (al mandar a la papelera un
// checkpoint, sus hijos se re-cuelgan del abuelo reescribiendo esa línea). Con
// ella en la clave, cada re-colgado invalidaría el render de los hijos —minutos,
// en un modelo pesado— por nada. Dos checkpoints con el mismo código y distinta
// descripción comparten entrada de caché, y está bien: el resultado se devuelve
// con el nombre de quien lo pide. Las líneas siguientes (y sus números, que usan
// los diagnósticos) no se tocan.
func skipHeader(data []byte) []byte {
	d := data
	if len(d) >= 3 && d[0] == 0xEF && d[1] == 0xBB && d[2] == 0xBF {
		d = d[3:]
	}
	end := len(d)
	for i, c := range d {
		if c == '\n' {
			end = i
			break
		}
	}
	if !checkpointHeaderRe.Match(d[:end]) {
		return data
	}
	return d[end:]
}

// findDeps lista los include<>/use<> del código, sin los comentados (un
// include comentado no se carga, y si se contara una edición a ese archivo
// invalidaría la caché por nada).
func findDeps(src []byte) []string {
	s := strings.ReplaceAll(string(src), "\r\n", "\n")
	s = blockCommentRe.ReplaceAllString(s, "")
	s = lineCommentRe.ReplaceAllString(s, "")
	var deps []string
	for _, m := range depRe.FindAllStringSubmatch(s, -1) {
		if d := strings.TrimSpace(m[1]); d != "" {
			deps = append(deps, d)
		}
	}
	return deps
}

func resolveDep(workspaceDir, fromDir, dep string) string {
	dep = filepath.FromSlash(dep)
	candidates := []string{dep}
	if !filepath.IsAbs(dep) {
		candidates = []string{
			filepath.Join(fromDir, dep),
			filepath.Join(workspaceDir, dep),
			filepath.Join(workspaceDir, "lib", dep),
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return filepath.Clean(c)
		}
	}
	return ""
}

// cacheKey combina el contenido con lo que cambia la malla sin cambiar el
// .scad: el backend (Manifold y CGAL triangulan distinto) y la versión de
// OpenSCAD.
func cacheKey(contentSum, backend, version string) string {
	h := sha256.Sum256([]byte(contentSum + "\x00" + backend + "\x00" + version))
	return hex.EncodeToString(h[:])
}

func (e *engine) meshPath(key string) string { return filepath.Join(e.cacheDir, key+".stl") }
func (e *engine) metaPath(key string) string { return filepath.Join(e.cacheDir, key+".json") }

// loadCached devuelve el Result guardado si están la malla y su .json. Toca la
// fecha de ambos: la poda va por antigüedad de ACCESO, y el sistema de archivos
// no garantiza atime (macOS y Windows lo tienen relajado o apagado).
func (e *engine) loadCached(key string) (Result, bool) {
	mesh := e.meshPath(key)
	if st, err := os.Stat(mesh); err != nil || st.Size() == 0 {
		return Result{}, false
	}
	b, err := os.ReadFile(e.metaPath(key))
	if err != nil {
		return Result{}, false
	}
	var r Result
	if json.Unmarshal(b, &r) != nil || r.Status != StatusOK {
		return Result{}, false
	}
	now := time.Now()
	_ = os.Chtimes(mesh, now, now)
	_ = os.Chtimes(e.metaPath(key), now, now)
	r.MeshPath = mesh
	if r.Diagnostics == nil {
		r.Diagnostics = []Diagnostic{}
	}
	if r.Engine == "" {
		r.Engine = EngineNative // un .json anterior al motor integrado
	}
	return r, true
}

// storeCached mueve el STL temporal a la caché y escribe su .json. El .json va
// después del rename: un .stl sin .json es un miss (se re-renderiza), pero un
// .json sin malla nunca debe existir apuntando a nada.
func (e *engine) storeCached(key, tmpMesh string, r Result) (Result, error) {
	mesh := e.meshPath(key)
	if err := os.Rename(tmpMesh, mesh); err != nil {
		return r, err
	}
	r.MeshPath = mesh
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return r, err
	}
	tmp := e.metaPath(key) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return r, err
	}
	if err := os.Rename(tmp, e.metaPath(key)); err != nil {
		_ = os.Remove(tmp)
		return r, err
	}
	return r, nil
}

// prune deja la caché por debajo del tope borrando las entradas de acceso más
// viejo, y limpia temporales huérfanos. Nunca falla el render: un error acá es
// espacio que se recupera la próxima vez.
func (e *engine) prune() {
	e.pruneMu.Lock()
	defer e.pruneMu.Unlock()

	entries, err := os.ReadDir(e.cacheDir)
	if err != nil {
		return
	}
	type item struct {
		key   string
		size  int64
		mtime time.Time
	}
	byKey := map[string]*item{}
	var total int64
	for _, de := range entries {
		if de.IsDir() {
			removeStaleWASMDir(e.cacheDir, de)
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		name := de.Name()
		ext := filepath.Ext(name)
		key := strings.TrimSuffix(name, ext)
		if (ext != ".stl" && ext != ".json") || len(key) != 64 {
			// Temporal (<clave>-NNN.stl, .json.tmp) u otra cosa: se borra solo
			// si quedó abandonado, para no pisar un render en curso.
			if time.Since(info.ModTime()) > staleTempAge {
				_ = os.Remove(filepath.Join(e.cacheDir, name))
			}
			continue
		}
		it := byKey[key]
		if it == nil {
			it = &item{key: key}
			byKey[key] = it
		}
		it.size += info.Size()
		if info.ModTime().After(it.mtime) {
			it.mtime = info.ModTime()
		}
		total += info.Size()
	}
	if total <= e.maxCacheBytes {
		return
	}
	items := make([]*item, 0, len(byKey))
	for _, it := range byKey {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
	for _, it := range items {
		if total <= e.maxCacheBytes {
			break
		}
		_ = os.Remove(e.metaPath(it.key))
		_ = os.Remove(e.meshPath(it.key))
		total -= it.size
	}
}

// removeStaleWASMDir borra la carpeta temporal de salida del motor integrado
// que quedó de un proceso que murió a mitad de un render. Una más joven que
// staleTempAge puede ser de un render en curso y no se toca.
func removeStaleWASMDir(parent string, de os.DirEntry) {
	if !strings.HasPrefix(de.Name(), wasmTempPrefix) {
		return
	}
	if info, err := de.Info(); err == nil && time.Since(info.ModTime()) > staleTempAge {
		_ = os.RemoveAll(filepath.Join(parent, de.Name()))
	}
}

// limitedBuffer guarda como mucho max bytes de la salida de OpenSCAD. Un
// modelo con un echo() dentro de un bucle puede escribir cientos de MB; el
// parser y el log no necesitan más que el final.
type limitedBuffer struct {
	buf []byte
	max int
}

var _ io.Writer = (*limitedBuffer)(nil)

func (l *limitedBuffer) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	if len(l.buf) > 2*l.max {
		// Se conserva la cola: los ERROR y el aviso de modelo vacío van al
		// final. Se recorta al doble y no en cada escritura para no copiar el
		// buffer entero por cada línea.
		l.buf = append(l.buf[:0], l.buf[len(l.buf)-l.max:]...)
	}
	return len(p), nil
}

// String devuelve como mucho los últimos max bytes.
func (l *limitedBuffer) String() string {
	if len(l.buf) > l.max {
		return string(l.buf[len(l.buf)-l.max:])
	}
	return string(l.buf)
}
