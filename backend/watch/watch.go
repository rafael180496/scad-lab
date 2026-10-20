// Package watch vigila la raíz de un workspace y avisa cuando cambian sus
// .scad. Es lo que hace que la UI se entere sola de un checkpoint que creó un
// agente desde la terminal, sin que el usuario tenga que pulsar «volver a leer».
//
// Usa fsnotify (Go puro: FSEvents/kqueue en macOS, ReadDirectoryChangesW en
// Windows), así que no rompe la compilación cruzada sin CGO.
package watch

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Debounce es cuánto se espera sin eventos antes de avisar. Un agente que
// escribe un archivo dispara varios eventos (crear, escribir, a veces
// renombrar desde un temporal); sin esta espera se reescanearía —y se
// renderizaría— un archivo a medio escribir.
const Debounce = 300 * time.Millisecond

// Change es lo que cambió entre dos escaneos de la raíz, ya pasado el debounce.
type Change struct {
	// Added son los .scad que no estaban en el escaneo anterior (ordenados por
	// nombre, puede ir vacío).
	Added []string
	// Updated son los .scad que ya existían y cuyo contenido se reescribió: lo hace
	// un agente que edita un checkpoint en el lugar (update_checkpoint desde el
	// subproceso --mcp de un CLI), «Restaurar versión» o cualquier editor externo.
	// Se detecta por el tamaño y la fecha de modificación, no por el tipo de evento:
	// una escritura atómica (temporal + rename) llega como Write, Create o Rename
	// según el sistema y la API de fsnotify, y los tres son «el archivo cambió».
	Updated []string
}

// sig identifica una versión del contenido de un archivo sin leerlo.
type sig struct {
	size  int64
	mtime int64
}

// Watcher vigila un directorio (no recursivo). Close lo detiene.
type Watcher struct {
	fsw      *fsnotify.Watcher
	dir      string
	onChange func(Change)
	known    map[string]sig
	done     chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
}

// Start empieza a vigilar dir. onChange se llama, ya pasado el debounce,
// cada vez que algún .scad se crea, cambia, se renombra o se borra; added son
// los .scad que no estaban en el scan anterior (ordenados por nombre, puede ir
// vacío). Las llamadas a onChange son secuenciales, desde una sola goroutine:
// si va a hacer algo largo (un render) tiene que lanzarlo aparte, o Close
// esperaría a que termine.
func Start(dir string, onChange func(added []string)) (*Watcher, error) {
	return StartChanges(dir, func(c Change) { onChange(c.Added) })
}

// StartChanges es Start para quien además quiere saber qué .scad existentes se
// reescribieron (Change.Updated). Mismas garantías: debounce, llamadas
// secuenciales desde una sola goroutine.
func StartChanges(dir string, onChange func(Change)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Solo la raíz: los checkpoints viven ahí, y vigilar lib/ o .claude/
	// dispararía rescans por cambios que no afectan al árbol.
	if err := fsw.Add(dir); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	w := &Watcher{fsw: fsw, dir: dir, onChange: onChange, known: map[string]sig{}, done: make(chan struct{})}
	// El scan inicial fija la línea de base: lo que ya estaba al abrir el
	// workspace no cuenta como «nuevo» (si no, abrir un proyecto con 40
	// checkpoints los renderizaría todos) ni como «reescrito».
	w.known = scadSigs(dir)
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// Close detiene el watcher y espera a que termine la goroutine. Es idempotente.
func (w *Watcher) Close() error {
	var err error
	w.once.Do(func() {
		close(w.done)
		err = w.fsw.Close()
		w.wg.Wait()
	})
	return err
}

func (w *Watcher) loop() {
	defer w.wg.Done()
	var timer *time.Timer
	var fire <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if !isScad(ev.Name) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(Debounce)
			} else {
				timer.Reset(Debounce)
			}
			fire = timer.C
		case _, ok := <-w.fsw.Errors:
			// Un error de fsnotify (cola desbordada, carpeta borrada) no se
			// puede arreglar desde acá; el rescan manual sigue funcionando.
			if !ok {
				return
			}
		case <-fire:
			fire = nil
			w.flush()
		}
	}
}

// flush reescanea la raíz, calcula qué .scad son nuevos y cuáles se reescribieron,
// y avisa.
func (w *Watcher) flush() {
	now := scadSigs(w.dir)
	var c Change
	for f, sg := range now {
		prev, was := w.known[f]
		switch {
		case !was:
			c.Added = append(c.Added, f)
		case prev != sg:
			c.Updated = append(c.Updated, f)
		}
	}
	sort.Strings(c.Added)
	sort.Strings(c.Updated)
	if c.Added == nil {
		c.Added = []string{}
	}
	w.known = now
	select {
	case <-w.done:
		return
	default:
	}
	w.onChange(c)
}

// isScad dice si un evento es de un .scad. RENDER_ERRORS.md, printer.json y
// los temporales de los editores no lo son, así que se ignoran.
func isScad(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".scad")
}

// scadSigs lista los .scad de la raíz con su tamaño y fecha de modificación.
func scadSigs(dir string) map[string]sig {
	ents, err := os.ReadDir(dir)
	out := map[string]sig{}
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() || !isScad(e.Name()) {
			continue
		}
		// Un enlace simbólico se sigue a propósito (Stat y no Lstat): lo que cuenta
		// es si cambió el contenido que se ve, igual que workspace.Scan.
		fi, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil || fi.IsDir() {
			continue
		}
		out[e.Name()] = sig{size: fi.Size(), mtime: fi.ModTime().UnixNano()}
	}
	return out
}

var numRe = regexp.MustCompile(`^(\d+)-`)

// Newest elige, de varios checkpoints, el de número más alto (NNN-slug.scad).
// Si llegaron varios de golpe, ese es el último que escribió el agente y el
// único que vale la pena renderizar. Sin número, gana el último por nombre.
func Newest(files []string) string {
	best, bestN := "", -1
	for _, f := range files {
		n := -1
		if m := numRe.FindStringSubmatch(f); m != nil {
			n, _ = strconv.Atoi(m[1])
		}
		if n > bestN || (n == bestN && f > best) {
			best, bestN = f, n
		}
	}
	return best
}
