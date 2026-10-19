package mcp

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"scadlab/backend/i18n"
)

// Librerías de OpenSCAD (list_libraries / install_library).
//
// La skill openscad-libraries manda instalar BOSL2 y compañía en lib/ con
// `git clone`, pero ni Hermes ni los CLIs en modo «Agente» tienen shell. Esto es
// ese `git clone` como herramienta, con las mismas garantías que la descarga de
// OpenSCAD (S11, scaddownload):
//
//   - un catálogo FIJO: la URL de cada librería (un commit concreto, no una rama)
//     y el SHA-256 de su zip están en el código. La IA elige un nombre del
//     catálogo, nunca una URL: no se baja nada que el mantenedor no haya
//     comprobado;
//   - el zip se verifica ANTES de extraerse; un SHA distinto no instala nada;
//   - todo o nada: se extrae a una carpeta temporal dentro de lib/ y se renombra
//     de una vez, así que una descarga cortada no deja media librería;
//   - nunca se actualiza en el lugar: los checkpoints son inmutables y uno que ya
//     usa BOSL2 no debe cambiar porque la librería cambió. Si ya está instalada,
//     no se toca.
//
// Las librerías las baja el usuario (la herramienta corre en su máquina, a su
// pedido); ScadLab no las redistribuye. Cada licencia va en el catálogo.

// libSpec es una librería del catálogo.
type libSpec struct {
	Name        string
	Description string
	Version     string // etiqueta del proyecto
	Commit      string // el commit exacto que se descarga
	License     string
	URL         string // zip de ese commit
	SHA256      string // hex, del zip
	Size        int64  // bytes del zip
	Marker      string // archivo que debe existir al extraer (comprueba que es la librería)
	Include     string // la línea que la usa
}

// libraryCatalog es el catálogo. Los SHA-256 se comprobaron descargando el zip
// tres veces (codeload.github.com y github.com/…/archive daban los mismos bytes).
// Subir una versión = cambiar Commit, URL, SHA256 y Size a la vez.
var libraryCatalog = []libSpec{
	{
		Name:        "BOSL2",
		Description: "Belfry OpenSCAD Library v2: threads, rounded/chamfered cuboids and cylinders, attachments (attach/position/diff), gears, screws, sweeps, paths. The usual choice when plain OpenSCAD is not enough.",
		Version:     "v2.0.774",
		Commit:      "f1493a8425f51bd0585dd6d89aea962561100d32",
		License:     "BSD-2-Clause",
		URL:         "https://codeload.github.com/BelfrySCAD/BOSL2/zip/f1493a8425f51bd0585dd6d89aea962561100d32",
		SHA256:      "ec62aa81d4ec15b5caf5d03f1865cac78e3ad75f22319b55783184787236a17a",
		Size:        7478144,
		Marker:      "std.scad",
		Include:     "include <lib/BOSL2/std.scad>",
	},
	{
		Name:        "MCAD",
		Description: "The library that ships with OpenSCAD (2019.05): boxes, bearings, nuts and bolts, involute gears, motors. Old and little maintained; prefer BOSL2 or your own code.",
		Version:     "openscad-2019.05",
		Commit:      "a7be3d623669d635b7249a327cfce5796ea200b3",
		License:     "LGPL-2.1",
		URL:         "https://codeload.github.com/openscad/MCAD/zip/a7be3d623669d635b7249a327cfce5796ea200b3",
		SHA256:      "c5079143da27189007c372336576c2a2f5f9de295283043b1585dcad891ab839",
		Size:        65458,
		Marker:      "boxes.scad",
		Include:     "use <lib/MCAD/boxes.scad>",
	},
	{
		Name:        "NopSCADlib",
		Description: "«Vitamins» (bought parts: screws, nuts, PCBs, fans, displays, connectors) to check that real components fit. Big (45 MB) and slow to load: include it only in checkpoints that need it, and mark its parts with % so they are not exported.",
		Version:     "v21.43.1",
		Commit:      "53775b7e099b1256723d5ecbbe28176491a82729",
		License:     "GPL-3.0",
		URL:         "https://codeload.github.com/nophead/NopSCADlib/zip/53775b7e099b1256723d5ecbbe28176491a82729",
		SHA256:      "1d2907ed09212c8a7b5aad465a31b2f2eaf452ceaa08cedbc52871399884283b",
		Size:        30553816,
		Marker:      "lib.scad",
		Include:     "include <lib/NopSCADlib/lib.scad>",
	},
}

// Límites de la extracción: protegen del zip bomb y del archivo que dice ser una
// librería y es otra cosa.
const (
	maxLibFiles    = 20000
	maxLibUnpacked = 400 << 20
	libDownloadMax = 200 << 20
)

func libByName(name string) (libSpec, bool) {
	for _, l := range libraryCatalog {
		if strings.EqualFold(l.Name, strings.TrimSpace(name)) {
			return l, true
		}
	}
	return libSpec{}, false
}

func libNames() string {
	names := make([]string, len(libraryCatalog))
	for i, l := range libraryCatalog {
		names[i] = l.Name
	}
	return strings.Join(names, ", ")
}

func (e *env) libraryTools() []Tool {
	return []Tool{
		{
			Name: "list_libraries",
			Description: "List the OpenSCAD libraries ScadLab can install into the workspace's lib/ folder (BOSL2, MCAD, NopSCADlib) with version, license, whether each is installed and the exact include line to use, plus any other folder already in lib/. " +
				"Plain OpenSCAD comes first: use a library only for threads, edge rounding, attachments, helical gears or real component models (see the openscad-libraries skill).",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.listLibraries,
		},
		{
			Name: "install_library",
			Description: "Install one library from the fixed catalog (see list_libraries) into <workspace>/lib/<name>. It DOWNLOADS the library from GitHub (needs internet): a pinned commit whose SHA-256 is checked before anything is extracted, all-or-nothing. " +
				"Never replaces or updates an installed library (checkpoints are immutable and may depend on it): if it is already there it does nothing and says so. Records the install in lib/LIBRARIES.txt. Install only when the design needs it.",
			InputSchema: obj(map[string]any{"name": enumProp("Library name from list_libraries.", libNamesList()...)}, "name"),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true},
			handler:     e.installLibrary,
		},
	}
}

func libNamesList() []string {
	out := make([]string, len(libraryCatalog))
	for i, l := range libraryCatalog {
		out[i] = l.Name
	}
	return out
}

// libDir devuelve <ws>/lib (opcionalmente la crea) comprobando que no sea un
// enlace que saque las librerías del workspace. "" sin error si no existe y no
// se pidió crearla.
func (e *env) libDir(create bool) (string, error) {
	dir := filepath.Join(e.ws, "lib")
	if st, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		if !create {
			return "", nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if !st.IsDir() && st.Mode()&os.ModeSymlink == 0 {
		return "", i18n.New(i18n.Msg{ES: "lib existe pero no es una carpeta", EN: "lib exists but is not a folder"})
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || !within(e.ws, real) {
		return "", i18n.New(i18n.Msg{ES: "la carpeta lib apunta fuera del workspace", EN: "the lib folder points outside the workspace"})
	}
	return real, nil
}

// installedLibs son las carpetas de lib/ (nombre → true).
func (e *env) installedLibs() (map[string]bool, error) {
	dir, err := e.libDir(false)
	if err != nil || dir == "" {
		return map[string]bool{}, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, en := range ents {
		if strings.HasPrefix(en.Name(), ".") {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, en.Name())); err == nil && st.IsDir() {
			out[en.Name()] = true
		}
	}
	return out, nil
}

func (e *env) listLibraries(_ context.Context, _ map[string]any) (*toolResult, error) {
	e.settings()
	have, err := e.installedLibs()
	if err != nil {
		return nil, err
	}
	type libOut struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Commit      string `json:"commit"`
		License     string `json:"license"`
		SizeMB      int64  `json:"downloadMb"`
		Installed   bool   `json:"installed"`
		Path        string `json:"path"`
		Include     string `json:"include"`
	}
	list := make([]libOut, 0, len(libraryCatalog))
	known := map[string]bool{}
	for _, l := range libraryCatalog {
		known[l.Name] = true
		list = append(list, libOut{
			Name: l.Name, Description: l.Description, Version: l.Version, Commit: l.Commit, License: l.License,
			SizeMB: (l.Size + (1 << 19)) >> 20, Installed: have[l.Name], Path: "lib/" + l.Name, Include: l.Include,
		})
	}
	var other []string
	for n := range have {
		if !known[n] {
			other = append(other, "lib/"+n)
		}
	}
	sort.Strings(other)
	return jsonResult(map[string]any{
		"libraries":  list,
		"otherInLib": other,
		"note": msgf(i18n.Msg{
			ES: "Instala con install_library (necesita internet). Con la librería instalada, el checkpoint la incluye con la línea `include`/`use` indicada; la ruta relativa lib/… funciona sin configurar nada.",
			EN: "Install with install_library (needs internet). Once installed, the checkpoint includes it with the given `include`/`use` line; the relative lib/… path works with no configuration.",
		}),
	})
}

// libraryFetch descarga url. Es una variable para que las pruebas no salgan a la
// red; en producción solo se llama con la URL de un libSpec del catálogo.
var libraryFetch = func(ctx context.Context, url string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "ScadLab")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, i18n.Errorf(i18n.Msg{ES: "el servidor respondió HTTP %d", EN: "the server answered HTTP %d"}, resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

func (e *env) installLibrary(ctx context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	spec, ok := libByName(strArg(args, "name"))
	if !ok {
		return nil, i18n.Errorf(i18n.Msg{ES: "librería desconocida: %q (disponibles: %s)", EN: "unknown library: %q (available: %s)"}, strArg(args, "name"), libNames())
	}
	have, err := e.installedLibs()
	if err != nil {
		return nil, err
	}
	if have[spec.Name] {
		return jsonResult(map[string]any{
			"name": spec.Name, "path": "lib/" + spec.Name, "installed": true, "alreadyInstalled": true, "include": spec.Include,
			"note": msgf(i18n.Msg{
				ES: "Ya estaba instalada: no se toca (los checkpoints que la usan no deben cambiar).",
				EN: "It was already installed: left untouched (checkpoints that use it must not change).",
			}),
		})
	}
	// La instalación real puede tardar: la carpeta y el temporal viven dentro de lib/.
	dir, err := e.libDir(true)
	if err != nil {
		return nil, err
	}
	files, size, err := installZip(ctx, dir, spec)
	if err != nil {
		return nil, err
	}
	if err := recordLibrary(dir, spec); err != nil {
		e.log.Printf("lib/LIBRARIES.txt: %v", err)
	}
	return jsonResult(map[string]any{
		"name": spec.Name, "path": "lib/" + spec.Name, "version": spec.Version, "commit": spec.Commit, "license": spec.License,
		"files": files, "sizeBytes": size, "installed": true, "alreadyInstalled": false, "include": spec.Include,
		"next": msgf(i18n.Msg{
			ES: "Úsala con `%s` al principio del checkpoint (después de los parámetros) y renderiza.",
			EN: "Use it with `%s` near the top of the checkpoint (after the parameters) and render.",
		}, spec.Include),
	})
}

// installZip baja el zip de spec, comprueba su SHA-256 y lo extrae en
// <libDir>/<Name> de una vez. Devuelve cuántos archivos y cuántos bytes.
func installZip(ctx context.Context, libDir string, spec libSpec) (int, int64, error) {
	netErr := func(err error) error {
		if ctx.Err() != nil {
			return i18n.New(i18n.Msg{ES: "la descarga se canceló", EN: "the download was cancelled"})
		}
		return i18n.Errorf(i18n.Msg{
			ES: "no se pudo descargar %s (¿hay internet?): %v",
			EN: "could not download %s (is there internet?): %v",
		}, spec.Name, err)
	}
	body, _, err := libraryFetch(ctx, spec.URL)
	if err != nil {
		return 0, 0, netErr(err)
	}
	defer body.Close()

	tmpZip, err := os.CreateTemp(libDir, ".download-"+spec.Name+"-*.zip")
	if err != nil {
		return 0, 0, err
	}
	zipPath := tmpZip.Name()
	defer os.Remove(zipPath)
	h := sha256.New()
	limit := int64(libDownloadMax)
	if spec.Size > 0 && spec.Size*3 < limit {
		limit = spec.Size * 3
	}
	n, cerr := io.Copy(io.MultiWriter(tmpZip, h), io.LimitReader(body, limit+1))
	if clerr := tmpZip.Close(); cerr == nil {
		cerr = clerr
	}
	if cerr != nil {
		return 0, 0, netErr(cerr)
	}
	if n > limit {
		return 0, 0, i18n.Errorf(i18n.Msg{ES: "la descarga de %s es más grande de lo esperado: no se instaló", EN: "the %s download is bigger than expected: nothing was installed"}, spec.Name)
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(spec.SHA256) {
		return 0, 0, i18n.Errorf(i18n.Msg{
			ES: "lo descargado de %s no coincide con el SHA-256 esperado: no se instaló nada. Vuelve a intentarlo",
			EN: "what was downloaded for %s does not match the expected SHA-256: nothing was installed. Try again",
		}, spec.Name)
	}

	stage, err := os.MkdirTemp(libDir, ".install-"+spec.Name+"-")
	if err != nil {
		return 0, 0, err
	}
	defer os.RemoveAll(stage)
	files, size, err := extractLibZip(ctx, zipPath, stage, spec)
	if err != nil {
		return 0, 0, err
	}
	final := filepath.Join(libDir, spec.Name)
	// os.Rename sobre una carpeta que ya existe falla (no la mezcla ni la pisa):
	// si otro proceso la creó en el medio, no se instala encima.
	if _, err := os.Lstat(final); err == nil {
		return 0, 0, i18n.Errorf(i18n.Msg{ES: "lib/%s ya existe: no se toca", EN: "lib/%s already exists: left untouched"}, spec.Name)
	}
	if err := os.Rename(stage, final); err != nil {
		return 0, 0, err
	}
	return files, size, nil
}

// extractLibZip extrae el zip sin su carpeta raíz (GitHub pone <repo>-<commit>/)
// en dst. Rechaza rutas que salgan de dst, ignora enlaces simbólicos y acota el
// tamaño y la cantidad de archivos.
func extractLibZip(ctx context.Context, zipPath, dst string, spec libSpec) (int, int64, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, 0, i18n.Errorf(i18n.Msg{ES: "el archivo de %s no es un zip válido: %v", EN: "the %s file is not a valid zip: %v"}, spec.Name, err)
	}
	defer zr.Close()
	bad := func(name string) error {
		return i18n.Errorf(i18n.Msg{ES: "el zip de %s trae una ruta no permitida (%q): no se instaló", EN: "the %s zip has a path that is not allowed (%q): nothing was installed"}, spec.Name, name)
	}
	var count int
	var total int64
	hasMarker := false
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return 0, 0, i18n.New(i18n.Msg{ES: "la instalación se canceló", EN: "the installation was cancelled"})
		}
		name := f.Name
		if strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
			return 0, 0, bad(name)
		}
		// Sin la carpeta raíz del repositorio.
		_, rel, ok := strings.Cut(name, "/")
		if !ok || rel == "" {
			continue // la propia carpeta raíz
		}
		clean := path.Clean(rel)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return 0, 0, bad(name)
		}
		mode := f.Mode()
		switch {
		case mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0:
			continue // un enlace dentro de una librería puede apuntar fuera: se omite
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(filepath.Join(dst, filepath.FromSlash(clean)), 0o755); err != nil {
				return 0, 0, err
			}
			continue
		}
		count++
		total += int64(f.UncompressedSize64)
		if count > maxLibFiles || total > maxLibUnpacked {
			return 0, 0, i18n.Errorf(i18n.Msg{ES: "el zip de %s es demasiado grande: no se instaló", EN: "the %s zip is too large: nothing was installed"}, spec.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(clean))
		if !within(dst, target) {
			return 0, 0, bad(name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, 0, err
		}
		if err := writeZipEntry(f, target); err != nil {
			return 0, 0, err
		}
		if clean == spec.Marker {
			hasMarker = true
		}
	}
	if !hasMarker {
		return 0, 0, i18n.Errorf(i18n.Msg{ES: "el zip de %s no trae %s: no es la librería esperada", EN: "the %s zip has no %s: it is not the expected library"}, spec.Name, spec.Marker)
	}
	return count, total, nil
}

func writeZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	// Un zip miente sobre el tamaño: se corta al declarado.
	_, cerr := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if clerr := out.Close(); cerr == nil {
		cerr = clerr
	}
	return cerr
}

// recordLibrary anota la instalación en lib/LIBRARIES.txt (nombre, versión,
// commit, URL y SHA-256), como pide la skill openscad-libraries.
func recordLibrary(libDir string, spec libSpec) error {
	f, err := os.OpenFile(filepath.Join(libDir, "LIBRARIES.txt"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(f, "%s %s (%s) commit %s sha256 %s %s installed %s\n",
		spec.Name, spec.Version, spec.License, spec.Commit, spec.SHA256, spec.URL, time.Now().UTC().Format("2006-01-02"))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}
