package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"scadlab/backend/i18n"
	"scadlab/backend/scaddownload"
)

// dlRecorder junta los eventos openscad:download que emite la App.
type dlRecorder struct {
	mu     sync.Mutex
	events []OpenSCADDownloadEvent
}

func (r *dlRecorder) hook(a *App) {
	a.emitter = func(name string, payload any) {
		if name != evOpenSCADDownload {
			return
		}
		r.mu.Lock()
		r.events = append(r.events, payload.(OpenSCADDownloadEvent))
		r.mu.Unlock()
	}
}

func (r *dlRecorder) phases() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		if len(out) == 0 || out[len(out)-1] != e.Phase {
			out = append(out, e.Phase)
		}
	}
	return out
}

// fakeInstall deja un «ejecutable» donde lo busca InstalledBinary.
func fakeInstall(dataDir string) (string, error) {
	var p string
	switch runtime.GOOS {
	case "darwin":
		p = filepath.Join(scaddownload.InstallDir(dataDir), "OpenSCAD.app", "Contents", "MacOS", "OpenSCAD")
	case "windows":
		p = filepath.Join(scaddownload.InstallDir(dataDir), "OpenSCAD", "openscad.exe")
	default:
		return "", errors.New("plataforma sin layout de prueba")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, []byte("x"), 0o755)
}

func stubDownload(t *testing.T, install func(ctx context.Context, dataDir string, a scaddownload.Asset, on func(scaddownload.Progress)) (string, error)) {
	t.Helper()
	oldAsset, oldInstall := openscadAsset, installOpenSCAD
	openscadAsset = func() (scaddownload.Asset, bool) { return scaddownload.Asset{Size: 1000}, true }
	installOpenSCAD = install
	t.Cleanup(func() { openscadAsset, installOpenSCAD = oldAsset, oldInstall })
}

func skipUnlessLayout(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("InstalledBinary solo conoce el layout de macOS y Windows")
	}
}

func TestDownloadOpenSCADFlow(t *testing.T) {
	skipUnlessLayout(t)
	a := newTestApp(t)
	var rec dlRecorder
	rec.hook(a)
	stubDownload(t, func(ctx context.Context, dataDir string, as scaddownload.Asset, on func(scaddownload.Progress)) (string, error) {
		on(scaddownload.Progress{Phase: scaddownload.PhaseDownload, Received: 500, Total: 1000})
		on(scaddownload.Progress{Phase: scaddownload.PhaseVerify})
		on(scaddownload.Progress{Phase: scaddownload.PhaseExtract})
		return fakeInstall(dataDir)
	})

	st, err := a.GetOpenSCADDownload()
	if err != nil || !st.Supported || st.Installed || st.Running || st.SizeBytes != 1000 || st.Version != scaddownload.Version || st.WasmVersion == "" {
		t.Fatalf("estado inicial: %+v %v", st, err)
	}

	// La ruta cacheada de OpenSCAD se olvida al terminar: el próximo render usa el descargado.
	a.scad = openscadCache{path: "/viejo/openscad"}
	st, err = a.DownloadOpenSCAD()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || st.InstalledPath == "" || st.Running {
		t.Errorf("estado tras descargar: %+v", st)
	}
	if a.scad.path != "" {
		t.Error("no se olvidó la ruta cacheada de OpenSCAD")
	}
	want := []string{"download", "verify", "extract", "done"}
	got := rec.phases()
	if len(got) != len(want) {
		t.Fatalf("fases = %v, quería %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fases = %v, quería %v", got, want)
		}
	}

	// Quitar la descarga: borra y olvida la ruta.
	a.scad = openscadCache{path: "/descargado/openscad"}
	st, err = a.RemoveDownloadedOpenSCAD()
	if err != nil || st.Installed {
		t.Fatalf("quitar: %+v %v", st, err)
	}
	if a.scad.path != "" {
		t.Error("quitar no olvidó la ruta cacheada")
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, "tools", "openscad")); err == nil {
		t.Error("quedó la carpeta de la descarga")
	}
}

// Una sola descarga a la vez; cancelar devuelve el error con código y no deja marca de «en curso».
func TestDownloadOpenSCADCancel(t *testing.T) {
	a := newTestApp(t)
	var rec dlRecorder
	rec.hook(a)
	started := make(chan struct{})
	stubDownload(t, func(ctx context.Context, dataDir string, as scaddownload.Asset, on func(scaddownload.Progress)) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})

	done := make(chan error, 1)
	go func() {
		_, err := a.DownloadOpenSCAD()
		done <- err
	}()
	<-started

	if st, _ := a.GetOpenSCADDownload(); !st.Running {
		t.Error("debería figurar en curso")
	}
	if _, err := a.DownloadOpenSCAD(); err == nil {
		t.Error("una segunda descarga a la vez debería rechazarse")
	}
	if _, err := a.RemoveDownloadedOpenSCAD(); err == nil {
		t.Error("quitar con una descarga en curso debería rechazarse")
	}

	a.CancelOpenSCADDownload()
	select {
	case err := <-done:
		if err == nil || i18n.Code(err) != ErrCodeDownloadCancelled {
			t.Fatalf("error = %v (código %q)", err, i18n.Code(err))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("la cancelación no terminó la descarga")
	}
	if st, _ := a.GetOpenSCADDownload(); st.Running {
		t.Error("sigue en curso tras cancelar")
	}
	phases := rec.phases()
	if phases[len(phases)-1] != dlPhaseCancelled {
		t.Errorf("fases = %v, la última debería ser cancelled", phases)
	}
	for _, p := range phases {
		if p == dlPhaseError {
			t.Errorf("rechazar la segunda descarga no es un error de la primera: %v", phases)
		}
	}
	// Se puede volver a descargar.
	a.CancelOpenSCADDownload() // sin descarga en curso: no hace nada
}

func TestDownloadOpenSCADErrorEvent(t *testing.T) {
	a := newTestApp(t)
	var rec dlRecorder
	rec.hook(a)
	stubDownload(t, func(context.Context, string, scaddownload.Asset, func(scaddownload.Progress)) (string, error) {
		return "", scaddownload.ErrChecksum
	})
	st, err := a.DownloadOpenSCAD()
	if !errors.Is(err, scaddownload.ErrChecksum) || st.Installed || st.Running {
		t.Fatalf("estado %+v, error %v", st, err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	last := rec.events[len(rec.events)-1]
	if last.Phase != dlPhaseError || last.Message == "" {
		t.Errorf("último evento = %+v, quería error con mensaje", last)
	}
}

func TestDownloadOpenSCADUnsupported(t *testing.T) {
	a := newTestApp(t)
	var rec dlRecorder
	rec.hook(a)
	old := openscadAsset
	openscadAsset = func() (scaddownload.Asset, bool) { return scaddownload.Asset{}, false }
	t.Cleanup(func() { openscadAsset = old })
	st, _ := a.GetOpenSCADDownload()
	if st.Supported || st.SizeBytes != 0 {
		t.Errorf("estado = %+v", st)
	}
	if _, err := a.DownloadOpenSCAD(); !errors.Is(err, scaddownload.ErrUnsupported) {
		t.Errorf("error = %v", err)
	}
	if p := rec.phases(); len(p) != 1 || p[0] != dlPhaseError {
		t.Errorf("fases = %v", p)
	}
}
