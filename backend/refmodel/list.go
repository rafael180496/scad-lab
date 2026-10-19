package refmodel

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Ref es un modelo de refs/ tal como lo lista List.
type Ref struct {
	Name     string `json:"name"`     // pieza.stl
	Path     string `json:"path"`     // refs/pieza.stl: lo que se le pasa a import() y a las herramientas
	Format   string `json:"format"`   // stl | 3mf | obj
	Bytes    int64  `json:"bytes"`    // tamaño del archivo
	Modified string `json:"modified"` // RFC 3339, UTC
}

// maxListed acota la lista: una carpeta con miles de archivos no le sirve a
// quien la lee (una persona o un modelo).
const maxListed = 500

// List devuelve los modelos de <workspaceDir>/refs/ (STL, 3MF y OBJ), por nombre.
// Es la lectura de ResolveRef en bloque: ignora subcarpetas, archivos ocultos,
// lo que no es un modelo y los enlaces simbólicos cuyo destino sale de refs/.
// Sin carpeta refs/ la lista es vacía (no es un error: todavía no hay nada).
func List(workspaceDir string) ([]Ref, error) {
	dir, err := refsDir(workspaceDir, false)
	if err != nil {
		if _, serr := os.Stat(filepath.Join(workspaceDir, RefsDir)); os.IsNotExist(serr) {
			return []Ref{}, nil
		}
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []Ref{}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") || FormatOf(name) == "" {
			continue
		}
		// ResolveRef es la puerta de cada archivo: la misma comprobación de
		// enlaces que hace cualquier herramienta al abrirlo.
		abs, rel, err := ResolveRef(workspaceDir, name)
		if err != nil {
			continue
		}
		st, err := os.Stat(abs)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		out = append(out, Ref{
			Name: name, Path: rel, Format: FormatOf(name), Bytes: st.Size(),
			Modified: st.ModTime().UTC().Format(time.RFC3339),
		})
		if len(out) >= maxListed {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
