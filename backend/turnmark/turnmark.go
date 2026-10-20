// Package turnmark es la marca de modo de un turno de Antigravity (S12.c, T14).
//
// El problema: Antigravity aprueba las herramientas de ScadLab con reglas GLOBALES
// (`permissions.allow` de su settings.json: `mcp(scadlab/<herramienta>)`), que valen
// igual en «Agente» que en «Solo consulta». El modo no puede viajar en la regla, así
// que lo hace cumplir el servidor `ScadLab --mcp`: durante un turno de «Solo consulta»
// el chat deja una marca en el workspace (`.scadlab/turn-mode`) y el servidor, antes
// de ejecutar una herramienta de ESCRITURA, la lee y la rechaza con el motivo.
//
// La marca vive lo que dura el turno y no queda huérfana aunque la app muera a
// mitad: cada entrada lleva su vencimiento y mientras el turno corre la app lo
// renueva (latido). Si el latido se corta, a los `TTL` la marca deja de valer y el
// servidor vuelve a permitir las escrituras.
//
// Solo se marcan los turnos de «Solo consulta»: en «Agente» no hay marca y todo pasa.
// Con dos turnos a la vez en el mismo workspace gana el más restrictivo (basta uno
// de consulta vivo para que no se escriba): el servidor no sabe a qué turno
// pertenece cada llamada, y equivocarse hacia permitir rompería el modo que el
// usuario eligió.
package turnmark

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Mode es el modo del turno.
type Mode string

const (
	Ask   Mode = "ask"   // «Solo consulta»: las herramientas de escritura se rechazan
	Agent Mode = "agent" // «Agente»: todo pasa (no se escribe marca)
)

const (
	// Dir y FileName: la marca es <workspace>/.scadlab/turn-mode (la carpeta ya es
	// de ScadLab, está en el .gitignore del workspace y el watcher no la mira).
	Dir      = ".scadlab"
	FileName = "turn-mode"
	// TTL es cuánto vale una entrada sin renovarse.
	TTL = 90 * time.Second
)

// heartbeat es cada cuánto se renueva la entrada de un turno en curso. Variable
// para las pruebas.
var heartbeat = TTL / 4

// Path es la ruta de la marca de un workspace.
func Path(workspaceDir string) string {
	return filepath.Join(workspaceDir, Dir, FileName)
}

type entry struct {
	ID      string    `json:"id"`
	Mode    Mode      `json:"mode"`
	PID     int       `json:"pid"`
	Expires time.Time `json:"expires"`
}

type markFile struct {
	Turns []entry `json:"turns"`
}

// registry son los turnos de ESTE proceso por carpeta: la marca se reescribe entera
// desde aquí, así dos turnos simultáneos no se pisan.
var registry = struct {
	sync.Mutex
	dirs map[string]map[string]entry
}{dirs: map[string]map[string]entry{}}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("150405.000000")
	}
	return hex.EncodeToString(b[:])
}

// Begin marca el turno que empieza en workspaceDir. Devuelve la función que lo
// retira (idempotente, también para un pánico: úsala con defer). En «Agente» no
// escribe nada y end no hace nada.
//
// Un error significa que NO quedó marcado: quien llama no debe correr un turno de
// consulta sin marca, porque el servidor dejaría escribir.
func Begin(workspaceDir string, mode Mode) (end func(), err error) {
	if mode != Ask {
		return func() {}, nil
	}
	dir := filepath.Clean(workspaceDir)
	id := newID()
	e := entry{ID: id, Mode: mode, PID: os.Getpid(), Expires: time.Now().Add(TTL)}

	registry.Lock()
	if registry.dirs[dir] == nil {
		registry.dirs[dir] = map[string]entry{}
	}
	registry.dirs[dir][id] = e
	err = writeLocked(dir)
	if err != nil {
		delete(registry.dirs[dir], id)
		if len(registry.dirs[dir]) == 0 {
			delete(registry.dirs, dir)
		}
	}
	registry.Unlock()
	if err != nil {
		return func() {}, err
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				registry.Lock()
				if cur, ok := registry.dirs[dir][id]; ok {
					cur.Expires = time.Now().Add(TTL)
					registry.dirs[dir][id] = cur
					_ = writeLocked(dir) // si falla, el siguiente latido lo reintenta
				}
				registry.Unlock()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
			registry.Lock()
			defer registry.Unlock()
			delete(registry.dirs[dir], id)
			if len(registry.dirs[dir]) == 0 {
				delete(registry.dirs, dir)
			}
			_ = writeLocked(dir)
		})
	}, nil
}

// writeLocked reescribe la marca de dir con los turnos de este proceso y los
// vigentes de OTROS procesos (dos ScadLab abiertas sobre el mismo workspace). Sin
// ninguno, borra el archivo. Atómico: el servidor nunca lee una marca a medias.
// Se llama con registry bloqueado.
func writeLocked(dir string) error {
	path := Path(dir)
	now := time.Now()
	var live []entry
	pid := os.Getpid()
	if cur, err := readFile(path); err == nil {
		for _, e := range cur.Turns {
			if e.PID != pid && e.Expires.After(now) {
				live = append(live, e)
			}
		}
	}
	for _, e := range registry.dirs[dir] {
		live = append(live, e)
	}
	if len(live) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	out, err := json.MarshalIndent(markFile{Turns: live}, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".turn-mode.tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func readFile(path string) (markFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return markFile{}, err
	}
	var f markFile
	if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &f); err != nil {
		return markFile{}, err
	}
	return f, nil
}

// ModeOf devuelve el modo vigente en un workspace según su marca: Ask si hay al
// menos un turno de consulta que no venció; Agent si no hay marca, está vacía, solo
// trae turnos de «Agente» o todos vencieron.
//
// Una marca que EXISTE pero no se puede leer cuenta como Ask (ante la duda no se
// escribe) mientras sea reciente; si su fecha de modificación es más vieja que TTL
// es un resto de otra época —el latido la habría renovado— y se ignora.
func ModeOf(workspaceDir string, now time.Time) Mode {
	path := Path(workspaceDir)
	f, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Agent
	}
	if err != nil {
		if st, serr := os.Stat(path); serr == nil && now.Sub(st.ModTime()) > TTL {
			return Agent
		}
		return Ask
	}
	for _, e := range f.Turns {
		if e.Mode == Ask && e.Expires.After(now) {
			return Ask
		}
	}
	return Agent
}

// ReadOnly dice si en este momento el workspace está en «Solo consulta».
func ReadOnly(workspaceDir string) bool { return ModeOf(workspaceDir, time.Now()) == Ask }
