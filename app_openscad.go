package main

import (
	"context"
	"errors"
	"runtime"
	"sync"

	"scadlab/backend/i18n"
	"scadlab/backend/scaddownload"
	"scadlab/backend/scadwasm"
)

// «Descargar OpenSCAD» (S11, tarea 11.3). La lógica vive en backend/scaddownload;
// acá están los bindings, la exclusión mutua de una sola descarga a la vez y el
// evento de progreso. Ver claude/18-descarga-openscad.md.

// evOpenSCADDownload es el evento de progreso (Go → React). Payload:
// OpenSCADDownloadEvent. El nombre es contrato con frontend/src/lib/events.ts.
const evOpenSCADDownload = "openscad:download"

// Fases del evento: las de scaddownload.Progress más los tres finales.
const (
	dlPhaseError     = "error"
	dlPhaseCancelled = "cancelled"
)

// ErrCodeDownloadCancelled es el código (invisible en el texto, ver i18n.NewCoded)
// del error que devuelve DownloadOpenSCAD cuando el usuario cancela: la interfaz
// lo reconoce y no muestra un toast de fallo.
const ErrCodeDownloadCancelled = "download-cancelled"

// OpenSCADDownloadEvent es el payload de openscad:download.
type OpenSCADDownloadEvent struct {
	Phase    string `json:"phase"` // download | verify | extract | done | error | cancelled
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Message  string `json:"message"` // solo con phase = error
}

// OpenSCADDownload es el estado de la descarga que ve Configuración.
type OpenSCADDownload struct {
	Version       string `json:"version"`       // versión que se descarga
	SizeBytes     int64  `json:"sizeBytes"`     // tamaño del archivo (0 si la plataforma no está soportada)
	Supported     bool   `json:"supported"`     // hay descarga para este sistema
	Installed     bool   `json:"installed"`     // hay un OpenSCAD descargado
	InstalledPath string `json:"installedPath"` // su ejecutable ("" si no hay)
	Running       bool   `json:"running"`       // hay una descarga en curso
	WasmVersion   string `json:"wasmVersion"`   // versión del motor integrado
}

// openscadDL es el estado de la descarga en curso. Como el resto de los
// servicios de S5+, vive en un registro por App y no en app.go.
type openscadDL struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

var openscadDLs sync.Map // *App → *openscadDL

func (a *App) openscadDL() *openscadDL {
	if v, ok := openscadDLs.Load(a); ok {
		return v.(*openscadDL)
	}
	d := &openscadDL{}
	actual, loaded := openscadDLs.LoadOrStore(a, d)
	if !loaded {
		a.onShutdown(func() {
			// Cerrar la app corta la descarga y no deja temporales.
			d.mu.Lock()
			if d.cancel != nil {
				d.cancel()
			}
			d.mu.Unlock()
			openscadDLs.Delete(a)
		})
	}
	return actual.(*openscadDL)
}

// Para sustituirlos en las pruebas: la tabla real queda fija en scaddownload.
var (
	openscadAsset   = func() (scaddownload.Asset, bool) { return scaddownload.ForPlatform(runtime.GOOS, runtime.GOARCH) }
	installOpenSCAD = scaddownload.Install
)

// GetOpenSCADDownload dice qué se puede descargar y si ya está descargado.
func (a *App) GetOpenSCADDownload() (OpenSCADDownload, error) {
	d := a.openscadDL()
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()
	out := OpenSCADDownload{Version: scaddownload.Version, Running: running, WasmVersion: scadwasm.Version}
	if asset, ok := openscadAsset(); ok {
		out.Supported, out.SizeBytes = true, asset.Size
	}
	if p := scaddownload.InstalledBinary(a.dataDir); p != "" {
		out.Installed, out.InstalledPath = true, p
	}
	return out, nil
}

// DownloadOpenSCAD baja e instala el OpenSCAD fijado en <datos>/tools/openscad y
// bloquea hasta terminar, informando con openscad:download. Cancelada con
// CancelOpenSCADDownload devuelve un error con código ErrCodeDownloadCancelled
// (y el evento «cancelled»); no deja nada a medias. Al terminar bien olvida la
// ruta de OpenSCAD cacheada: el próximo render usa el descargado.
func (a *App) DownloadOpenSCAD() (OpenSCADDownload, error) {
	asset, ok := openscadAsset()
	if !ok {
		return a.downloadFailed(scaddownload.ErrUnsupported)
	}
	if a.dataDir == "" {
		return a.downloadFailed(i18n.New(i18n.Msg{
			ES: "la carpeta de datos de ScadLab no está disponible",
			EN: "the ScadLab data folder is not available",
		}))
	}
	d := a.openscadDL()
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		// No es un fallo de la descarga que ya corre: no se emite «error».
		st, _ := a.GetOpenSCADDownload()
		return st, i18n.New(i18n.Msg{
			ES: "ya hay una descarga de OpenSCAD en curso",
			EN: "an OpenSCAD download is already in progress",
		})
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	d.running, d.cancel = true, cancel
	d.mu.Unlock()
	defer func() {
		cancel()
		d.mu.Lock()
		d.running, d.cancel = false, nil
		d.mu.Unlock()
	}()

	a.emit(evOpenSCADDownload, OpenSCADDownloadEvent{Phase: scaddownload.PhaseDownload, Total: asset.Size})
	_, err := installOpenSCAD(ctx, a.dataDir, asset, func(p scaddownload.Progress) {
		a.emit(evOpenSCADDownload, OpenSCADDownloadEvent{Phase: p.Phase, Received: p.Received, Total: p.Total})
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.emit(evOpenSCADDownload, OpenSCADDownloadEvent{Phase: dlPhaseCancelled})
			st := a.downloadState(false)
			return st, i18n.NewCoded(ErrCodeDownloadCancelled, i18n.Msg{ES: "descarga cancelada", EN: "download cancelled"})
		}
		return a.downloadFailed(err)
	}
	a.resetOpenSCADCache()
	a.emit(evOpenSCADDownload, OpenSCADDownloadEvent{Phase: scaddownload.PhaseDone, Received: asset.Size, Total: asset.Size})
	return a.downloadState(false), nil
}

// downloadFailed emite el evento «error» y devuelve el estado con el error.
func (a *App) downloadFailed(err error) (OpenSCADDownload, error) {
	a.emit(evOpenSCADDownload, OpenSCADDownloadEvent{Phase: dlPhaseError, Message: err.Error()})
	return a.downloadState(false), err
}

// downloadState es GetOpenSCADDownload con Running forzado (la descarga acaba
// de terminar pero su defer todavía no apagó la marca).
func (a *App) downloadState(running bool) OpenSCADDownload {
	st, _ := a.GetOpenSCADDownload()
	st.Running = running
	return st
}

// CancelOpenSCADDownload corta la descarga en curso, si la hay.
func (a *App) CancelOpenSCADDownload() {
	d := a.openscadDL()
	d.mu.Lock()
	cancel := d.cancel
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// RemoveDownloadedOpenSCAD borra el OpenSCAD que descargó la app. No toca el
// instalado por el usuario ni la ruta fijada en Configuración. Con una descarga
// en curso se rechaza: borrar la carpeta que se está llenando la rompería.
func (a *App) RemoveDownloadedOpenSCAD() (OpenSCADDownload, error) {
	d := a.openscadDL()
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()
	if running {
		return a.downloadState(true), i18n.New(i18n.Msg{
			ES: "espera a que termine la descarga de OpenSCAD o cancélala antes de quitarla",
			EN: "wait for the OpenSCAD download to finish or cancel it before removing it",
		})
	}
	if err := scaddownload.Remove(a.dataDir); err != nil {
		return a.downloadState(false), err
	}
	a.resetOpenSCADCache()
	return a.downloadState(false), nil
}
