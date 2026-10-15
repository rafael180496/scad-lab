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

// Watcher vigila un directorio (no recursivo). Close lo detiene.
type Watcher struct {
	fsw      *fsnotify.Watcher
	dir      string
	onChange func(added []string)
	known    map[string]bool
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
	w := &Watcher{fsw: fsw, dir: dir, onChange: onChange, known: map[string]bool{}, done: make(chan struct{})}
	// El scan inicial fija la línea de base: lo que ya estaba al abrir el
	// workspace no cuenta como «nuevo» (si no, abrir un proyecto con 40
	// checkpoints los renderizaría todos).
	for _, f := range scadFiles(dir) {
		w.known[f] = true
	}
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

// flush reescanea la raíz, calcula qué .scad son nuevos y avisa.
func (w *Watcher) flush() {
	now := scadFiles(w.dir)
	added := []string{}
	next := make(map[string]bool, len(now))
	for _, f := range now {
		next[f] = true
		if !w.known[f] {
			added = append(added, f)
		}
	}
	w.known = next
	select {
	case <-w.done:
		return
	default:
	}
	w.onChange(added)
}

// isScad dice si un evento es de un .scad. RENDER_ERRORS.md, printer.json y
// los temporales de los editores no lo son, así que se ignoran.
func isScad(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".scad")
}

func scadFiles(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, e := range ents {
		if !e.IsDir() && isScad(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
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
