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
	Format   string `json:"format"`   // stl | 3mf | obj | png | jpeg | gif | webp
	Kind     string `json:"kind"`     // model | image
	Bytes    int64  `json:"bytes"`    // tamaño del archivo
	Modified string `json:"modified"` // RFC 3339, UTC
}

// maxListed acota la lista: una carpeta con miles de archivos no le sirve a
// quien la lee (una persona o un modelo).
const maxListed = 500

// List devuelve los modelos de <workspaceDir>/refs/ (STL, 3MF y OBJ) y las
// imágenes de refs/ y refs/images/ (PNG, JPEG, GIF y WebP, con Kind «image»),
// por nombre.
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
			continue // las imágenes se listan aparte (listImages)
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
			Name: name, Path: rel, Format: FormatOf(name), Kind: KindModel, Bytes: st.Size(),
			Modified: st.ModTime().UTC().Format(time.RFC3339),
		})
		if len(out) >= maxListed {
			break
		}
	}
	out = listImages(workspaceDir, out)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// listImages suma a out las imágenes de refs/ y de refs/images/, con el mismo
// criterio que los modelos: nada oculto, nada que no sea un archivo y nada cuyo
// destino salga de refs/ (la puerta es ResolveImage).
func listImages(workspaceDir string, out []Ref) []Ref {
	dir, err := refsDir(workspaceDir, false)
	if err != nil {
		return out
	}
	for _, sub := range []string{"", ImagesDir} {
		ents, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if strings.HasPrefix(name, ".") || ImageFormatOf(name) == "" {
				continue
			}
			want := RefsDir + "/" + name
			if sub != "" {
				want = RefsDir + "/" + sub + "/" + name
			}
			abs, rel, err := ResolveImage(workspaceDir, want)
			if err != nil || rel != want {
				continue
			}
			st, err := os.Stat(abs)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			out = append(out, Ref{
				Name: name, Path: rel, Format: ImageFormatOf(name), Kind: KindImage, Bytes: st.Size(),
				Modified: st.ModTime().UTC().Format(time.RFC3339),
			})
			if len(out) >= maxListed {
				return out
			}
		}
	}
	return out
}
