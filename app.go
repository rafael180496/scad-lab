package main

import (
	"context"
	"log"
	"path/filepath"
	"runtime"
	"sync"

	"scadlab/backend/appdata"
	"scadlab/backend/i18n"
	"scadlab/backend/render"
	"scadlab/backend/store"
	"scadlab/backend/watch"
)

// App es el único objeto enlazado al frontend. Sus métodos públicos son los
// bindings (ver claude/06-go-react-contract.md); se reparten por área en
// app_*.go.
type App struct {
	ctx     context.Context
	store   *store.Store
	dataDir string
	initErr error

	// Render (S1, app_render.go). engine es nil si no se pudo crear la caché:
	// la app sigue abriendo y RenderCheckpoint devuelve el motivo.
	engine    *render.Engine
	engineErr error
	renderMu  sync.Mutex
	results   map[int64]map[string]render.Result // último resultado por workspace y checkpoint
	scad      openscadCache
	emitter   func(name string, payload any) // nil en tests: ver emit

	// Watcher del workspace abierto (app_workspaces.go). Uno solo en S1.
	watchMu   sync.Mutex
	watcher   *watch.Watcher
	watchedID int64
}

// NewApp crea la App; el trabajo real va en startup.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.emitter = wailsEmitter(ctx)
	dir, err := appdata.Dir()
	if err != nil {
		a.initErr = err
		log.Printf("scadlab: directorio de datos: %v", err)
		return
	}
	a.dataDir = dir
	// La caché de mallas no depende de la base: se crea aunque scadlab.db no
	// abra, así un render sigue funcionando para diagnosticar.
	if a.engine, a.engineErr = render.NewEngine(filepath.Join(dir, "cache", "meshes")); a.engineErr != nil {
		log.Printf("scadlab: motor de render: %v", a.engineErr)
	}
	dbPath, err := appdata.DBPath()
	if err != nil {
		a.initErr = err
		return
	}
	st, err := store.Open(dbPath)
	if err != nil {
		a.initErr = err
		log.Printf("scadlab: abrir %s: %v", dbPath, err)
		return
	}
	a.store = st
	if s, err := st.GetSettings(); err == nil {
		i18n.SetLang(s.Language)
	}
	if err := a.refreshSkillRegistry(); err != nil {
		log.Printf("scadlab: registro de skills: %v", err)
	}
}

func (a *App) shutdown(context.Context) {
	// Primero el watcher (no debe disparar renders nuevos) y después el motor,
	// que mata los OpenSCAD en curso: si no, quedarían procesos huérfanos
	// consumiendo CPU después de cerrar la ventana.
	a.stopWatcher()
	if a.engine != nil {
		a.engine.Close()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
}

// ready devuelve un error legible si la base no abrió al arrancar.
func (a *App) ready() error {
	if a.store == nil {
		return i18n.Errorf(i18n.Msg{ES: "la base de datos local no está disponible: %v", EN: "the local database is not available: %v"}, a.initErr)
	}
	return nil
}

// Bootstrap es lo que el frontend necesita para pintar la primera pantalla.
type Bootstrap struct {
	Version       string         `json:"version"`
	Platform      string         `json:"platform"` // "darwin" | "windows" | "linux"
	DataDir       string         `json:"dataDir"`
	SchemaVersion int            `json:"schemaVersion"`
	Settings      store.Settings `json:"settings"`
	InitError     string         `json:"initError"`
}

// GetBootstrap devuelve versión, plataforma y configuración.
func (a *App) GetBootstrap() Bootstrap {
	b := Bootstrap{Version: appVersion, Platform: runtime.GOOS, DataDir: a.dataDir, Settings: store.DefaultSettings()}
	if err := a.ready(); err != nil {
		b.InitError = err.Error()
		return b
	}
	if s, err := a.store.GetSettings(); err == nil {
		b.Settings = s
	}
	b.SchemaVersion, _ = a.store.SchemaVersion()
	return b
}
