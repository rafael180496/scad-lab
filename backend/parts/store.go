package parts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/render"
	"scadlab/backend/threemf"
)

const (
	// manifestVersion cambia cuando cambia el formato del manifest o cómo se
	// reparten las partes: una caché con otra versión se regenera.
	manifestVersion = 1
	manifestName    = "manifest.json"

	// maxCacheBytes es el tope de la caché de partes: 512 MB. Se poda por
	// fecha de acceso, igual que la de mallas.
	maxCacheBytes int64 = 512 << 20
	// staleTempAge: una carpeta temporal más vieja que esto es de un proceso
	// que murió a mitad de generar.
	staleTempAge = 2 * time.Hour
)

// Source es lo que hace falta para generar las partes de un render.
type Source struct {
	Engine Renderer
	// Req es el pedido del render 3MF. Su WorkspaceKey tiene que ser propio
	// (no el del visor ni el de la exportación): el motor cancela por clave.
	Req  render.Request
	Opts render.Options
	// ArtifactDir es la caché de los 3MF (<datos>/cache/exports).
	ArtifactDir string
	// MeshPath es el STL del render normal del checkpoint (Status OK): de él
	// salen la clave, la malla completa y la parte única de un modelo sin color.
	MeshPath string
}

// Store es la caché de partes en disco. Es seguro para uso concurrente y
// genera una sola vez por clave aunque lo pidan varios a la vez.
type Store struct {
	dir string

	mu      sync.Mutex
	flights map[string]*flight
}

type flight struct {
	done chan struct{}
	m    Manifest
	err  error
}

// NewStore crea el almacén en dir (<datos>/cache/parts); la carpeta se crea al
// generar la primera clave.
func NewStore(dir string) *Store {
	return &Store{dir: dir, flights: map[string]*flight{}}
}

// Dir es la carpeta raíz de la caché (la que sirve meshserve bajo /parts/).
func (s *Store) Dir() string { return s.dir }

func (s *Store) keyDir(key string) string { return filepath.Join(s.dir, key+DirSuffix) }

// Lookup devuelve el manifest de una clave si la caché lo tiene completo.
func (s *Store) Lookup(key string) (Manifest, bool) {
	if !ValidKey(key) {
		return Manifest{}, false
	}
	return s.load(key)
}

// Ensure devuelve las partes de un render, generándolas si no están en caché:
// render 3MF con color, reparto por color y un STL por parte. Un modelo sin
// color() (o con un solo color) es una sola parte, la malla del render normal.
//
// Dos llamadas simultáneas con la misma clave comparten una sola generación.
// Si la llamada que genera se cancela, las que esperaban reintentan en vez de
// heredar una cancelación que no pidieron.
func (s *Store) Ensure(ctx context.Context, src Source) (Manifest, error) {
	key, err := KeyFromMeshPath(src.MeshPath)
	if err != nil {
		return Manifest{}, err
	}
	for {
		if m, ok := s.load(key); ok {
			return m, nil
		}
		s.mu.Lock()
		f, running := s.flights[key]
		if !running {
			f = &flight{done: make(chan struct{})}
			s.flights[key] = f
		}
		s.mu.Unlock()

		if running {
			select {
			case <-f.done:
			case <-ctx.Done():
				return Manifest{}, ctx.Err()
			}
			if f.err == nil {
				return f.m, nil
			}
			if errors.Is(f.err, context.Canceled) && ctx.Err() == nil {
				continue // canceló el que generaba, no yo: reintento
			}
			return Manifest{}, f.err
		}

		f.m, f.err = s.generate(ctx, key, src)
		s.mu.Lock()
		delete(s.flights, key)
		s.mu.Unlock()
		close(f.done)
		return f.m, f.err
	}
}

// load lee y valida el manifest de una clave. Toca su fecha: la poda va por
// antigüedad de acceso.
func (s *Store) load(key string) (Manifest, bool) {
	dir := s.keyDir(key)
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return Manifest{}, false
	}
	var m Manifest
	if json.Unmarshal(b, &m) != nil || m.Version != manifestVersion || m.Key != key || len(m.Parts) == 0 {
		return Manifest{}, false
	}
	for i, p := range m.Parts {
		if p.Index != i {
			return Manifest{}, false
		}
		if st, err := os.Stat(filepath.Join(dir, FileName(i))); err != nil || st.Size() == 0 {
			return Manifest{}, false
		}
	}
	now := time.Now()
	_ = os.Chtimes(filepath.Join(dir, manifestName), now, now)
	return m, true
}

func (s *Store) generate(ctx context.Context, key string, src Source) (Manifest, error) {
	total, err := mesh.InfoFromFile(src.MeshPath)
	if err != nil {
		return Manifest{}, i18n.Errorf(i18n.Msg{ES: "no se pudo analizar la malla: %w", EN: "could not analyze the mesh: %w"}, err)
	}
	split, res, err := Load3MF(ctx, src.Engine, src.Req, src.Opts, src.ArtifactDir)
	if err != nil {
		// El STL del render normal ya salió bien. Si solo falla el 3MF (una
		// versión de OpenSCAD sin exportación 3MF, por ejemplo), el modelo se
		// ve y se estima entero como una sola parte; un OpenSCAD ausente o un
		// render cancelado sí son errores.
		if res.Status == render.StatusCancelled || ctx.Err() != nil {
			return Manifest{}, i18n.Errorf(i18n.Msg{
				ES: "se canceló la preparación de las partes: %w",
				EN: "preparing the parts was cancelled: %w",
			}, context.Canceled)
		}
		if res.Status != render.StatusError {
			return Manifest{}, err
		}
		split = nil
	}
	if len(split) > MaxParts {
		return Manifest{}, i18n.Errorf(i18n.Msg{
			ES: "el modelo tiene %d colores y el máximo es %d: unifica colores en el .scad",
			EN: "the model has %d colors and the maximum is %d: merge colors in the .scad",
		}, len(split), MaxParts)
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return Manifest{}, i18n.Errorf(i18n.Msg{ES: "no se pudo crear la caché de partes %s: %w", EN: "could not create the parts cache %s: %w"}, s.dir, err)
	}
	tmp, err := os.MkdirTemp(s.dir, key+DirSuffix+".tmp-*")
	if err != nil {
		return Manifest{}, i18n.Errorf(i18n.Msg{ES: "no se pudo crear la caché de partes %s: %w", EN: "could not create the parts cache %s: %w"}, s.dir, err)
	}
	defer os.RemoveAll(tmp) // si todo salió bien ya no existe

	m := Manifest{Version: manifestVersion, Key: key, Total: total}
	if len(split) <= 1 {
		// Una sola parte: es la malla del render normal, que ya está
		// calculada y es idéntica a la que ve el visor en /mesh/.
		p := Part{Index: 0, Name: "Default", Color: threemf.RGB{0xF9, 0xD7, 0x2C}.Hex(), Uncolored: true, URL: URL(key, 0), Info: total}
		if len(split) == 1 {
			p.Name, p.Color, p.Uncolored = split[0].Name, split[0].Color.Hex(), split[0].Uncolored
		}
		if err := copyFile(src.MeshPath, filepath.Join(tmp, FileName(0))); err != nil {
			return Manifest{}, err
		}
		m.Parts = []Part{p}
	} else {
		for i, sp := range split {
			path := filepath.Join(tmp, FileName(i))
			if err := writePart(path, sp); err != nil {
				return Manifest{}, err
			}
			info, err := mesh.InfoFromFile(path)
			if err != nil {
				return Manifest{}, i18n.Errorf(i18n.Msg{ES: "no se pudo analizar la malla: %w", EN: "could not analyze the mesh: %w"}, err)
			}
			m.Parts = append(m.Parts, Part{
				Index: i, Name: sp.Name, Color: sp.Color.Hex(), Uncolored: sp.Uncolored,
				URL: URL(key, i), Info: info,
			})
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	// El manifest se escribe al final: una carpeta sin manifest es un miss.
	if err := os.WriteFile(filepath.Join(tmp, manifestName), b, 0o644); err != nil {
		return Manifest{}, err
	}

	final := s.keyDir(key)
	if err := os.Rename(tmp, final); err != nil {
		// Otro proceso (la app y el servidor MCP comparten la caché) pudo
		// ganar la carrera: si lo que quedó es válido, vale. Si es una
		// carpeta vieja o rota, se reemplaza.
		if got, ok := s.load(key); ok {
			return got, nil
		}
		_ = os.RemoveAll(final)
		if err := os.Rename(tmp, final); err != nil {
			return Manifest{}, i18n.Errorf(i18n.Msg{ES: "no se pudo guardar las partes en la caché: %w", EN: "could not store the parts in the cache: %w"}, err)
		}
	}
	s.prune()
	return m, nil
}

func writePart(path string, p threemf.Part) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := threemf.WriteSTL(f, p); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// prune deja la caché por debajo del tope borrando las claves de acceso más
// viejo y limpia carpetas temporales abandonadas. Nunca falla la generación.
func (s *Store) prune() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	type item struct {
		dir   string
		size  int64
		mtime time.Time
	}
	var items []item
	var total int64
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		name := de.Name()
		path := filepath.Join(s.dir, name)
		info, err := de.Info()
		if err != nil {
			continue
		}
		if strings.Contains(name, DirSuffix+".tmp-") {
			if time.Since(info.ModTime()) > staleTempAge {
				_ = os.RemoveAll(path)
			}
			continue
		}
		if key, ok := strings.CutSuffix(name, DirSuffix); !ok || !ValidKey(key) {
			continue
		}
		var size int64
		files, _ := os.ReadDir(path)
		for _, f := range files {
			if fi, err := f.Info(); err == nil {
				size += fi.Size()
			}
		}
		mtime := info.ModTime()
		if st, err := os.Stat(filepath.Join(path, manifestName)); err == nil {
			mtime = st.ModTime()
		}
		items = append(items, item{dir: path, size: size, mtime: mtime})
		total += size
	}
	if total <= maxCacheBytes {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
	for _, it := range items {
		if total <= maxCacheBytes {
			break
		}
		_ = os.RemoveAll(it.dir)
		total -= it.size
	}
}
