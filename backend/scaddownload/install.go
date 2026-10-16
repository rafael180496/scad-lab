package scaddownload

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"scadlab/backend/i18n"
)

// Variables de paquete solo para las pruebas (la tabla de descargas queda fija).
var (
	httpClient = newHTTPClient()
	// stallTimeout: sin recibir un byte durante este tiempo la descarga se
	// da por colgada. No es un plazo total: bajar 60 MB con una conexión lenta
	// puede tardar minutos y no es un error.
	stallTimeout = 60 * time.Second
	// progressEvery acota los avisos de progreso (~10 por segundo): un callback
	// por cada lectura de 256 KB inundaría el canal de eventos de la interfaz.
	progressEvery = 100 * time.Millisecond
	// validateBinary comprueba que el OpenSCAD instalado arranca y es la
	// versión esperada, antes de dar la instalación por buena.
	validateBinary = checkVersion
	// spaceFactor: espacio libre exigido como múltiplo del tamaño de la
	// descarga (archivo + contenido extraído + margen).
	spaceFactor int64 = 5
)

// maxExtractBytes es el tope de lo que se extrae de un zip (defensa contra un
// archivo que descomprime muchísimo más de lo que declara).
const maxExtractBytes = 3 << 30

var errStalled = errors.New("download stalled") // i18n-ignore: causa interna del contexto; netError la traduce

func newHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

// Install descarga el archivo, verifica su SHA-256, lo extrae y lo deja en
// InstallDir(dataDir). Devuelve la ruta del ejecutable. Bloquea hasta terminar;
// se cancela con ctx (el error es entonces ctx.Err()). Si algo falla o se
// cancela no queda nada: ni temporales ni una instalación a medias, y una
// instalación anterior sigue como estaba.
func Install(ctx context.Context, dataDir string, a Asset, onProgress func(Progress)) (string, error) {
	if dataDir == "" || a.URL == "" || a.SHA256 == "" || (a.Kind != KindDMG && a.Kind != KindZIP) {
		return "", ErrUnsupported
	}
	prog := &throttle{f: onProgress, every: progressEvery}
	tools := toolsDir(dataDir)
	if err := os.MkdirAll(tools, 0o700); err != nil {
		return "", writeError(err)
	}
	created := false
	defer func() {
		// Sin instalación final no se deja ni siquiera tools/openscad vacío
		// (os.Remove no borra carpetas con contenido, así que no daña nada).
		if !created {
			_ = os.Remove(tools)
			_ = os.Remove(filepath.Dir(tools))
		}
	}()
	sweepStale(tools)

	if need := requiredBytes(a); need > 0 {
		if free, err := freeBytes(tools); err == nil && free < need {
			return "", kindf(ErrNoSpace, i18n.Msg{
				ES: "no hay espacio suficiente en el disco para instalar OpenSCAD: se necesitan unos %s y hay %s libres",
				EN: "not enough disk space to install OpenSCAD: about %s are needed and %s are free",
			}, humanBytes(need), humanBytes(free))
		}
	}

	work, err := os.MkdirTemp(tools, ".tmp-")
	if err != nil {
		return "", writeError(err)
	}
	defer os.RemoveAll(work)

	file := filepath.Join(work, "download.part")
	sum, err := download(ctx, a, file, prog)
	if err != nil {
		return "", err
	}
	prog.emit(Progress{Phase: PhaseVerify, Received: a.Size, Total: a.Size}, true)
	if sum != lowerHex(a.SHA256) {
		return "", ErrChecksum
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	stage := filepath.Join(work, "stage")
	prog.emit(Progress{Phase: PhaseExtract}, true)
	switch a.Kind {
	case KindDMG:
		err = extractDMG(ctx, file, work, stage)
	case KindZIP:
		err = extractZip(ctx, file, stage, prog)
	}
	if err != nil {
		return "", err
	}
	_ = os.Remove(file)

	bin := findBinary(stage, a.OS)
	if bin == "" {
		return "", i18n.New(i18n.Msg{
			ES: "el archivo descargado no contiene el ejecutable de OpenSCAD",
			EN: "the downloaded file does not contain the OpenSCAD executable",
		})
	}
	if a.OS == "darwin" {
		dropQuarantine(ctx, stage)
	}
	if err := validateBinary(ctx, bin); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Renombre atómico: la carpeta final aparece entera o no aparece. Una
	// instalación previa se aparta primero para poder volver a ponerla si el
	// renombre falla.
	final := InstallDir(dataDir)
	previous := ""
	if _, err := os.Stat(final); err == nil {
		previous = filepath.Join(work, "previous")
		if err := os.Rename(final, previous); err != nil {
			return "", i18n.Errorf(i18n.Msg{
				ES: "no se pudo reemplazar la instalación anterior de OpenSCAD (¿está en uso?): %v",
				EN: "could not replace the previous OpenSCAD install (is it in use?): %v",
			}, err)
		}
	}
	if err := os.Rename(stage, final); err != nil {
		if previous != "" {
			_ = os.Rename(previous, final)
		}
		return "", writeError(err)
	}
	created = true
	prog.emit(Progress{Phase: PhaseDone}, true)
	return findBinary(final, a.OS), nil
}

// requiredBytes es el espacio libre que se exige antes de empezar.
func requiredBytes(a Asset) int64 {
	if a.Size <= 0 {
		return 0
	}
	return a.Size * spaceFactor
}

// sweepStale borra temporales que dejó un cierre brusco de la app (una
// descarga normal, fallida o cancelada limpia los suyos). Solo los de más de
// una hora: no se pisa a una instalación que esté corriendo.
func sweepStale(tools string) {
	ents, err := os.ReadDir(tools)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
			_ = os.RemoveAll(filepath.Join(tools, e.Name()))
		}
	}
}

// throttle limita la frecuencia de los avisos; los cambios de fase siempre pasan.
type throttle struct {
	f     func(Progress)
	every time.Duration
	last  time.Time
}

func (t *throttle) emit(p Progress, force bool) {
	if t == nil || t.f == nil {
		return
	}
	now := time.Now()
	if !force && now.Sub(t.last) < t.every {
		return
	}
	t.last = now
	t.f(p)
}

// download baja el archivo a dst y devuelve su SHA-256 (hex).
func download(ctx context.Context, a Asset, dst string, prog *throttle) (string, error) {
	dctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stall := time.AfterFunc(stallTimeout, func() { cancel(errStalled) })
	defer stall.Stop()

	req, err := http.NewRequestWithContext(dctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", netError(ctx, dctx, err)
	}
	req.Header.Set("User-Agent", "ScadLab")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", netError(ctx, dctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", kindf(ErrHTTP, i18n.Msg{
			ES: "el servidor respondió HTTP %d al descargar OpenSCAD",
			EN: "the server answered HTTP %d while downloading OpenSCAD",
		}, resp.StatusCode)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = a.Size
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", writeError(err)
	}
	h := sha256.New()
	buf := make([]byte, 256<<10)
	var got int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			h.Write(buf[:n])
			if _, werr := f.Write(buf[:n]); werr != nil {
				_ = f.Close()
				return "", writeError(werr)
			}
			got += int64(n)
			prog.emit(Progress{Phase: PhaseDownload, Received: got, Total: total}, false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			return "", netError(ctx, dctx, rerr)
		}
	}
	if err := f.Close(); err != nil {
		return "", writeError(err)
	}
	prog.emit(Progress{Phase: PhaseDownload, Received: got, Total: got}, true)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// netError traduce un fallo de red: una cancelación del usuario se devuelve tal
// cual (ctx.Err()); un corte por falta de datos y el resto, con su texto.
func netError(ctx, dctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	if errors.Is(context.Cause(dctx), errStalled) {
		return kindf(ErrNetwork, i18n.Msg{
			ES: "la descarga de OpenSCAD se detuvo: no llegaron datos en %d segundos. Revisa tu conexión a internet",
			EN: "the OpenSCAD download stalled: no data arrived for %d seconds. Check your internet connection",
		}, int(stallTimeout/time.Second))
	}
	return kindf(ErrNetwork, i18n.Msg{
		ES: "no se pudo descargar OpenSCAD; revisa tu conexión a internet (%v)",
		EN: "could not download OpenSCAD; check your internet connection (%v)",
	}, err)
}

// writeError distingue «disco lleno» de cualquier otro fallo de escritura.
func writeError(err error) error {
	if isDiskFull(err) {
		return diskFullError()
	}
	return i18n.Errorf(i18n.Msg{
		ES: "no se pudo escribir en la carpeta de datos de ScadLab: %v",
		EN: "could not write to the ScadLab data folder: %v",
	}, err)
}

func diskFullError() error {
	return kindf(ErrNoSpace, i18n.Msg{
		ES: "se llenó el disco al instalar OpenSCAD; libera espacio e inténtalo de nuevo",
		EN: "the disk filled up while installing OpenSCAD; free some space and try again",
	})
}

// ctxReader corta una lectura larga en cuanto se cancela el contexto.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// extractZip extrae el zip de Windows en dst. Rechaza rutas que salgan de dst
// (zip slip), enlaces y un tamaño descomprimido absurdo.
func extractZip(ctx context.Context, zipPath, dst string, prog *throttle) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return i18n.Errorf(i18n.Msg{
			ES: "el archivo descargado no es un zip válido: %v",
			EN: "the downloaded file is not a valid zip: %v",
		}, err)
	}
	defer zr.Close()
	var total uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
	}
	if total > maxExtractBytes {
		return i18n.New(i18n.Msg{
			ES: "el archivo descargado descomprime a un tamaño inesperado; no se instaló",
			EN: "the downloaded file expands to an unexpected size; nothing was installed",
		})
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return writeError(err)
	}
	var done int64
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(name) {
			return i18n.Errorf(i18n.Msg{
				ES: "el zip trae una ruta fuera de la carpeta de destino (%q); no se instaló",
				EN: "the zip has a path outside the destination folder (%q); nothing was installed",
			}, f.Name)
		}
		target := filepath.Join(dst, name)
		switch {
		case f.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o700); err != nil {
				return writeError(err)
			}
		case f.Mode().IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return writeError(err)
			}
			if err := writeZipFile(ctx, f, target, int64(total), &done, prog); err != nil {
				return err
			}
		default:
			// Enlaces y otros tipos: el zip oficial no los trae; se ignoran.
		}
	}
	prog.emit(Progress{Phase: PhaseExtract, Received: done, Total: int64(total)}, true)
	return nil
}

func writeZipFile(ctx context.Context, f *zip.File, target string, total int64, done *int64, prog *throttle) error {
	rc, err := f.Open()
	if err != nil {
		return i18n.Errorf(i18n.Msg{
			ES: "no se pudo leer %q del zip: %v",
			EN: "could not read %q from the zip: %v",
		}, f.Name, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm()|0o600)
	if err != nil {
		return writeError(err)
	}
	buf := make([]byte, 256<<10)
	var written int64
	// Un byte de más que lo declarado: así se detecta un archivo que miente.
	src := ctxReader{ctx: ctx, r: io.LimitReader(rc, int64(f.UncompressedSize64)+1)}
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				return writeError(werr)
			}
			written += int64(n)
			*done += int64(n)
			prog.emit(Progress{Phase: PhaseExtract, Received: *done, Total: total}, false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = out.Close()
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			return i18n.Errorf(i18n.Msg{
				ES: "no se pudo descomprimir %q: %v",
				EN: "could not unzip %q: %v",
			}, f.Name, rerr)
		}
	}
	if err := out.Close(); err != nil {
		return writeError(err)
	}
	if written > int64(f.UncompressedSize64) {
		return i18n.Errorf(i18n.Msg{
			ES: "%q pesa más de lo que declara el zip; no se instaló",
			EN: "%q is larger than the zip declares; nothing was installed",
		}, f.Name)
	}
	return nil
}

// extractDMG monta el dmg de macOS, copia OpenSCAD.app a stage con ditto
// (conserva firma, enlaces simbólicos y atributos) y lo desmonta.
func extractDMG(ctx context.Context, dmg, work, stage string) error {
	mnt := filepath.Join(work, "mnt")
	if err := os.MkdirAll(mnt, 0o700); err != nil {
		return writeError(err)
	}
	attach := exec.CommandContext(ctx, "/usr/bin/hdiutil", "attach",
		"-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg)
	// Si la imagen trajera un acuerdo de licencia, hdiutil lo pregunta por
	// stdin: sin esto se quedaría esperando una respuesta que nadie da.
	attach.Stdin = strings.NewReader("Y\n")
	if out, err := attach.CombinedOutput(); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return i18n.Errorf(i18n.Msg{
			ES: "no se pudo abrir la imagen de disco de OpenSCAD: %v (%s)",
			EN: "could not open the OpenSCAD disk image: %v (%s)",
		}, err, strings.TrimSpace(string(out)))
	}
	defer detach(mnt)

	app := findApp(mnt)
	if app == "" {
		return i18n.New(i18n.Msg{
			ES: "la imagen de disco no contiene OpenSCAD.app",
			EN: "the disk image does not contain OpenSCAD.app",
		})
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return writeError(err)
	}
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", app, filepath.Join(stage, "OpenSCAD.app")).CombinedOutput(); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if strings.Contains(string(out), "No space left on device") {
			return diskFullError()
		}
		return i18n.Errorf(i18n.Msg{
			ES: "no se pudo copiar OpenSCAD.app: %v (%s)",
			EN: "could not copy OpenSCAD.app: %v (%s)",
		}, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// findApp es el .app de OpenSCAD en la raíz del volumen montado.
func findApp(mnt string) string {
	ents, err := os.ReadDir(mnt)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".app") {
			continue
		}
		p := filepath.Join(mnt, e.Name())
		if isFile(filepath.Join(p, "Contents", "MacOS", "OpenSCAD")) {
			return p
		}
	}
	return ""
}

// detach desmonta el dmg. Corre aunque se haya cancelado el contexto (si no,
// quedaría un volumen montado), y si el desmontaje normal falla lo fuerza.
func detach(mnt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "/usr/bin/hdiutil", "detach", mnt).Run(); err != nil {
		_ = exec.CommandContext(ctx, "/usr/bin/hdiutil", "detach", "-force", mnt).Run()
	}
}

// dropQuarantine quita com.apple.quarantine. El cliente HTTP de Go no la pone
// (la agregan los navegadores y otros clientes con LSFileQuarantineEnabled), así
// que normalmente no hay nada que quitar; si el dmg la trajera, Gatekeeper
// bloquearía un binario sin notarizar la primera vez que corra.
func dropQuarantine(ctx context.Context, dir string) {
	_ = exec.CommandContext(ctx, "/usr/bin/xattr", "-dr", "com.apple.quarantine", dir).Run()
}

// checkVersion corre `openscad --version` y comprueba que sea la esperada.
func checkVersion(ctx context.Context, bin string) error {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "--version")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	text := strings.TrimSpace(string(out))
	// Salida vacía con código 0: arrancó pero no hay forma de leer la versión
	// por tuberías (un ejecutable gráfico de Windows); se acepta.
	if err != nil || (text != "" && !strings.Contains(text, Version)) {
		return i18n.Errorf(i18n.Msg{
			ES: "el OpenSCAD descargado no arrancó o no es la versión esperada (%s); no se instaló. Salida: %s",
			EN: "the downloaded OpenSCAD did not start or is not the expected version (%s); nothing was installed. Output: %s",
		}, Version, text)
	}
	return nil
}
