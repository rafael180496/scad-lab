// Package meshserve sirve la caché de mallas del render por el AssetServer de
// Wails, en GET /mesh/<clave>.stl. El visor la pide con fetch → ArrayBuffer:
// por un binding, un []byte cruzaría como base64 dentro de JSON (un STL de
// 15 MB serían 20 MB de texto y un JSON.parse que congela la interfaz).
//
// Es un Middleware y no un Handler: en `wails dev` las rutas que el embed no
// conoce las responde el servidor de Vite (con index.html), así que un Handler
// nunca se llamaría. El middleware va delante de todo, en dev y en producción.
package meshserve

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// Prefix es la ruta bajo la que se sirven las mallas.
const Prefix = "/mesh/"

// ContentType de las mallas servidas.
const ContentType = "model/stl"

// nameRe es la forma de un archivo de la caché de render: sha256 en hex
// minúscula + .stl (ver backend/render/cache.go). Nada que no cumpla esto se
// convierte en ruta: ni `..`, ni separadores, ni mayúsculas, ni otra extensión.
var nameRe = regexp.MustCompile(`^[0-9a-f]{64}\.stl$`)

// ValidName dice si name (sin directorio) es el nombre de una malla de la
// caché. Lo comparten el middleware y el binding GetMeshInfo.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Middleware sirve GET/HEAD /mesh/<clave>.stl desde cacheDir y deja pasar al
// siguiente handler todo lo que no empiece con /mesh/.
//
//   - nombre que no cumple la clave, o archivo inexistente → 404
//   - método distinto de GET/HEAD → 405
//   - Cache-Control: no-store: el frontend tiene su propia caché por clave, y
//     la del WebView no se poda cuando la caché de render borra una malla.
func Middleware(cacheDir string) assetserver.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// r.URL.Path ya viene decodificado: un %2e%2e llega como «..» y no
			// pasa la expresión.
			if !strings.HasPrefix(r.URL.Path, Prefix) {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
				return
			}
			name := strings.TrimPrefix(r.URL.Path, Prefix)
			if !ValidName(name) {
				http.NotFound(w, r)
				return
			}
			f, err := os.Open(filepath.Join(cacheDir, name))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil || !st.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			h := w.Header()
			h.Set("Content-Type", ContentType)
			h.Set("Cache-Control", "no-store")
			// Fecha cero: sin Last-Modified ni respuestas 304. La clave ya
			// identifica el contenido y no-store dice que no se guarde.
			http.ServeContent(w, r, name, time.Time{}, f)
		})
	}
}
