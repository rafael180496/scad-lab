package main

import (
	"os"
	"path/filepath"
	"sync"

	"scadlab/backend/estimate"
	"scadlab/backend/i18n"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

// Bindings de medición e impresión (S7): las partes del modelo por color y la
// estimación de peso, filamento y costo. La lógica vive en backend/parts y
// backend/estimate; acá se resuelve el workspace, la impresora y los ajustes.

// partsCacheDir es la caché de partes: <datos>/cache/parts. main.go la pasa a
// meshserve para servirlas por /parts/<clave>/NN.stl.
func partsCacheDir(dataDir string) string { return filepath.Join(dataDir, "cache", "parts") }

// partsStores guarda un parts.Store por carpeta de datos. Vive a nivel de
// paquete y no en App porque app.go no es de este bloque; al ser la caché por
// contenido, compartirla entre instancias de App es correcto. Cada Store
// garantiza una sola generación por clave.
var partsStores = struct {
	sync.Mutex
	m map[string]*parts.Store
}{m: map[string]*parts.Store{}}

func partsStoreFor(dataDir string) *parts.Store {
	partsStores.Lock()
	defer partsStores.Unlock()
	st := partsStores.m[dataDir]
	if st == nil {
		st = parts.NewStore(partsCacheDir(dataDir))
		partsStores.m[dataDir] = st
	}
	return st
}

// partsContext reúne lo que hace falta para pedir las partes de un checkpoint.
type partsContext struct {
	src      parts.Source
	store    *parts.Store
	printer  printers.Printer
	settings store.Settings
}

// partsFor resuelve el checkpoint de un workspace. Exige un render correcto
// reciente (el que dejó RenderCheckpoint): las partes salen de ese modelo, y
// medir algo que no renderiza no tiene sentido.
func (a *App) partsFor(id int64, fileName string) (partsContext, error) {
	if err := a.ready(); err != nil {
		return partsContext{}, err
	}
	if a.dataDir == "" {
		return partsContext{}, i18n.Errorf(i18n.Msg{ES: "el directorio de datos no está disponible: %v", EN: "the data directory is not available: %v"}, a.initErr)
	}
	if a.engine == nil {
		return partsContext{}, i18n.Errorf(i18n.Msg{ES: "el motor de render no está disponible: %v", EN: "the render engine is not available: %v"}, a.engineErr)
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return partsContext{}, err
	}
	if err := validCheckpointName(fileName); err != nil {
		return partsContext{}, err
	}
	a.renderMu.Lock()
	res, ok := a.results[id][fileName]
	a.renderMu.Unlock()
	if !ok || res.Status != render.StatusOK || res.MeshPath == "" {
		return partsContext{}, i18n.Errorf(i18n.Msg{
			ES: "no hay un render correcto de %s: renderízalo antes de medirlo",
			EN: "there is no successful render of %s: render it before measuring it",
		}, fileName)
	}
	if _, err := os.Stat(res.MeshPath); err != nil {
		return partsContext{}, i18n.Errorf(i18n.Msg{
			ES: "la malla de %s ya no está en la caché: vuelve a renderizarlo",
			EN: "the mesh of %s is no longer in the cache: render it again",
		}, fileName)
	}
	s, err := a.store.GetSettings()
	if err != nil {
		return partsContext{}, err
	}
	pr, _ := printers.ByID(w.PrinterID)
	return partsContext{
		src: parts.Source{
			Engine: a.engine,
			// Cola propia: preparar las partes no cancela el render del visor
			// ni la exportación, ni al revés.
			Req: render.Request{WorkspaceKey: renderKey(id) + ":parts", WorkspaceDir: w.Path, FileName: fileName},
			Opts: render.Options{
				OpenSCADPath:   a.openscadPath(s.OpenSCADPath),
				Backend:        s.RenderBackend,
				TimeoutSeconds: s.RenderTimeoutSec,
			},
			ArtifactDir: filepath.Join(a.dataDir, "cache", "exports"),
			MeshPath:    res.MeshPath,
		},
		store:    partsStoreFor(a.dataDir),
		printer:  pr,
		settings: s,
	}, nil
}

// GetModelParts devuelve las partes por color del checkpoint (una malla STL por
// parte, servida por GET /parts/<clave>/NN.stl), la malla completa y si cabe en
// la placa de la impresora del workspace. Requiere un render correcto del
// checkpoint. La primera vez por clave corre OpenSCAD otra vez a 3MF con color
// (queda en caché, y la exportación posterior lo reutiliza); un modelo sin
// color() es una sola parte.
func (a *App) GetModelParts(workspaceId int64, fileName string) (parts.Model, error) {
	pc, err := a.partsFor(workspaceId, fileName)
	if err != nil {
		return parts.Model{}, err
	}
	m, err := pc.store.Ensure(a.ctx, pc.src)
	if err != nil {
		return parts.Model{}, err
	}
	return m.Model(pc.printer), nil
}

// EstimatePrint estima peso, largo de filamento y costo de cada parte y del
// total. Es rápido: usa el manifest de partes en caché (si no está, lo genera
// primero, como GetModelParts). Con opts vacíos usa los ajustes de la app; un
// campo sin valor se completa con el ajuste (ver estimate.Merge, que explica el
// caso del relleno 0 %).
func (a *App) EstimatePrint(workspaceId int64, fileName string, opts estimate.Options) (estimate.Result, error) {
	pc, err := a.partsFor(workspaceId, fileName)
	if err != nil {
		return estimate.Result{}, err
	}
	key, err := parts.KeyFromMeshPath(pc.src.MeshPath)
	if err != nil {
		return estimate.Result{}, err
	}
	m, ok := pc.store.Lookup(key)
	if !ok {
		if m, err = pc.store.Ensure(a.ctx, pc.src); err != nil {
			return estimate.Result{}, err
		}
	}
	mats, err := a.store.ListMaterials()
	if err != nil {
		return estimate.Result{}, err
	}
	em := make([]estimate.Material, len(mats))
	for i, mt := range mats {
		em[i] = estimate.Material{ID: mt.ID, Name: mt.Name, DensityGCm3: mt.DensityGCm3, CostPerKg: mt.CostPerKg}
	}
	ep := make([]estimate.Part, len(m.Parts))
	for i, p := range m.Parts {
		ep[i] = estimate.PartOf(p.Index, p.Info)
	}
	ep = estimate.ClosedAsWhole(ep, m.Total.Watertight)
	return estimate.Estimate(ep, em, estimate.Merge(opts, estimate.OptionsFromSettings(pc.settings)))
}
