package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/meshedit"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/refmodel"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

// Editar un STL (S13). La edición nunca toca el STL: vive intacto en refs/ y
// cada cambio es un checkpoint nuevo que hace `import("refs/x.stl")`. La lógica
// (generar el código, importar, convertir) está en backend/meshedit; aquí se
// resuelve el workspace, el render del checkpoint y la caché del visor.
// Notas: claude/23-edicion-stl.md.

// ImportOptions ajusta ImportModel.
type ImportOptions struct {
	// CenterOnBed centra el modelo en XY y lo apoya en Z = 0 (un translate() en el
	// checkpoint base; el STL no cambia).
	CenterOnBed bool `json:"centerOnBed"`
	// Label es el título del checkpoint (da el nombre del archivo). Vacío = el
	// nombre del modelo.
	Label string `json:"label"`
}

// ImportResult es lo que devuelve ImportModel.
type ImportResult struct {
	File     string          `json:"file"` // el checkpoint base creado
	Ref      string          `json:"ref"`  // refs/…stl que importa (el original, o el convertido de un 3MF/OBJ)
	Report   refmodel.Report `json:"report"`
	Warnings []string        `json:"warnings"` // malla no cerrada, muy grande, fuera de la placa…
}

// EditBase es el punto de partida de una edición.
type EditBase struct {
	// Ref es el STL de refs/ sobre el que se edita (Scene.Ref).
	Ref string `json:"ref"`
	// Parent es el checkpoint del que cuelga la edición (el que se está editando).
	Parent string `json:"parent"`
	// MeshPath es la malla CRUDA de Ref (sin Model), ya en la caché de mallas del
	// visor: su nombre base es la clave de GET /mesh/<clave>.stl. Es siempre la
	// geometría de Ref; la pose que se le aplica es Model.
	MeshPath string          `json:"meshPath"`
	Report   refmodel.Report `json:"report"`
	// FromImport: el checkpoint era un import base y Ref es el STL original. Si es
	// falso, Ref es la malla renderizada del checkpoint, congelada en refs/.
	FromImport bool `json:"fromImport"`
	// Model es la pose con la que ya se importaba Ref: la del import base, o la
	// identidad si la malla se congeló.
	Model meshedit.Transform `json:"model"`
	// Color es el color del modelo en el import base ("" = el de fábrica, y
	// siempre "" cuando se congeló la malla: un 3MF multicolor pierde sus colores).
	Color string `json:"color"`
}

// ChooseModelFile abre el diálogo del sistema para elegir un STL, 3MF u OBJ.
// "" si se cancela.
func (a *App) ChooseModelFile() (string, error) {
	return wruntime.OpenFileDialog(a.appCtx(), wruntime.OpenDialogOptions{
		Title: i18n.T(i18n.Msg{ES: "Importar un modelo 3D", EN: "Import a 3D model"}),
		Filters: []wruntime.FileFilter{
			{DisplayName: i18n.T(i18n.Msg{ES: "Modelos 3D (STL, 3MF, OBJ)", EN: "3D models (STL, 3MF, OBJ)"}), Pattern: "*.stl;*.3mf;*.obj"},
		},
	})
}

// AnalyzeModelFile mide un STL, 3MF u OBJ del disco antes de importarlo: medidas,
// volumen, triángulos, si la malla es cerrada y las partes por color de un 3MF.
// No copia nada.
func (a *App) AnalyzeModelFile(path string) (refmodel.Report, error) {
	return refmodel.Analyze(filepath.Clean(strings.TrimSpace(path)))
}

// ImportModel trae un modelo al proyecto: lo copia a refs/ (sin pisar nada),
// convierte un 3MF u OBJ a STL (el original queda al lado) y crea el checkpoint
// base —una raíz nueva del árbol— que lo importa, con la opción de centrarlo y
// apoyarlo en la placa. Devuelve también los avisos (malla no cerrada, muy
// grande, fuera de la placa de la impresora del proyecto).
func (a *App) ImportModel(workspaceId int64, path string, opts ImportOptions) (ImportResult, error) {
	if err := a.ready(); err != nil {
		return ImportResult{}, err
	}
	w, err := a.store.GetWorkspace(workspaceId)
	if err != nil {
		return ImportResult{}, err
	}
	rel, err := refmodel.CopyToRefs(w.Path, filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return ImportResult{}, err
	}
	got, err := meshedit.ImportRef(w.Path, rel, a.importOptions(w, opts, filepath.Base(path)))
	if err != nil {
		// La copia es de esta llamada (CopyToRefs nunca pisa): si el modelo no
		// sirve, no se deja en el proyecto.
		_ = os.Remove(filepath.Join(w.Path, filepath.FromSlash(rel)))
		return ImportResult{}, i18n.Errorf(i18n.Msg{ES: "no se pudo importar %q como modelo 3D: %w", EN: "could not import %q as a 3D model: %w"}, filepath.Base(path), err)
	}
	a.checkpoints().markOwn(w.ID, got.File)
	return ImportResult{File: got.File, Ref: got.Ref, Report: got.Report, Warnings: got.Warnings}, nil
}

func (a *App) importOptions(w store.Workspace, opts ImportOptions, source string) meshedit.ImportOptions {
	o := meshedit.ImportOptions{CenterOnBed: opts.CenterOnBed, Label: opts.Label, Source: source}
	if p, ok := printers.ByID(w.PrinterID); ok {
		o.Plate, o.PlateName = p.BuildVolume, p.Name
	}
	return o
}

// PrepareMeshEdit prepara la edición de un checkpoint y devuelve su base.
//
//   - Si es un import base (solo el `import()` de un STL con su transformación y
//     su color, que se reconoce por la marca `// @scadlab-mesh` Y porque el código
//     coincide con lo que Generate escribiría), la base es ese STL de refs/ y su
//     pose: se sigue editando desde el original.
//   - Si no, es un diseño cualquiera: se CONGELA su malla renderizada (la del
//     último render correcto en la sesión; si no hay, se renderiza ahora) en
//     refs/<checkpoint>.stl —reutilizando el archivo si ya existe con el mismo
//     contenido— y la edición será un hijo que importa esa malla. Los colores de
//     un checkpoint multicolor se pierden en la base (es una sola malla); el
//     editor permite recolorear por pieza.
//
// MeshPath es la malla cruda de Ref en la caché del visor: no hace falta otra ruta
// HTTP, el visor la pide a /mesh/ como cualquier render.
func (a *App) PrepareMeshEdit(workspaceId int64, file string) (EditBase, error) {
	if err := a.ready(); err != nil {
		return EditBase{}, err
	}
	w, err := a.store.GetWorkspace(workspaceId)
	if err != nil {
		return EditBase{}, err
	}
	if err := validCheckpointName(file); err != nil {
		return EditBase{}, err
	}
	raw, err := os.ReadFile(filepath.Join(w.Path, file))
	if err != nil {
		return EditBase{}, err
	}

	// La decisión (import base o malla congelada) es meshedit.ResolveBase, la misma
	// que usa la herramienta edit_mesh del MCP: una sola definición. Aquí solo se
	// pone el render (que vive en la sesión de la app) y la caché de mallas del visor.
	frozenMesh := ""
	base, err := meshedit.ResolveBase(w.Path, file, string(raw), func() (string, error) {
		res, err := a.okRender(workspaceId, file)
		frozenMesh = res.MeshPath
		return res.MeshPath, err
	})
	if err != nil {
		return EditBase{}, err
	}
	out := EditBase{
		Ref: base.Ref, Parent: file, Report: base.Report,
		FromImport: base.FromImport, Model: base.Model, Color: base.Color,
	}
	if base.FromImport {
		abs, _, err := refmodel.ResolveRef(w.Path, base.Ref)
		if err != nil {
			return EditBase{}, err
		}
		out.MeshPath = a.viewerMesh(abs)
		return out, nil
	}
	// La copia congelada es idéntica byte a byte a la malla del render, que ya
	// está en la caché del visor: se usa esa.
	out.MeshPath = frozenMesh
	return out, nil
}

// okRender devuelve el último render correcto del checkpoint en esta sesión —si
// su malla sigue en la caché— o lo renderiza ahora. Un checkpoint que no
// renderiza no se puede editar, y el error dice por qué.
func (a *App) okRender(workspaceId int64, file string) (render.Result, error) {
	a.renderMu.Lock()
	res, ok := a.results[workspaceId][file]
	a.renderMu.Unlock()
	if ok && res.Status == render.StatusOK && res.MeshPath != "" {
		if st, err := os.Stat(res.MeshPath); err == nil && st.Size() > 0 {
			return res, nil
		}
	}
	res, err := a.RenderCheckpoint(workspaceId, file)
	if err != nil {
		return res, err
	}
	if err := parts.RenderFailure(res); err != nil {
		return res, err
	}
	if res.MeshPath == "" {
		return res, i18n.New(i18n.Msg{ES: "el checkpoint no produjo una malla", EN: "the checkpoint did not produce a mesh"})
	}
	return res, nil
}

// viewerMesh deja una copia binaria del STL en la caché de mallas del visor,
// nombrada por el SHA-256 de su contenido (la forma de clave que sirve
// /mesh/<clave>.stl) y devuelve su ruta. "" si no se pudo: la interfaz puede
// pedir entonces otra cosa; no es un error de la edición. Una copia que la poda
// de la caché se llevó se vuelve a crear la próxima vez que se abre el editor.
func (a *App) viewerMesh(abs string) string {
	if a.dataDir == "" {
		return ""
	}
	sum, err := refmodel.FileHash(abs)
	if err != nil {
		return ""
	}
	dir := filepath.Join(a.dataDir, "cache", "meshes")
	dst := filepath.Join(dir, sum+".stl")
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		now := time.Now()
		_ = os.Chtimes(dst, now, now) // la poda va por antigüedad de acceso
		return dst
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	// Temporal con el patrón <clave>-*.stl: la poda de la caché sabe que no es una
	// entrada. Se renombra al final para que el visor nunca lea una copia a medias.
	tmp, err := os.CreateTemp(dir, sum+"-*.stl")
	if err != nil {
		return ""
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpName) // WriteBinarySTL crea el destino con O_EXCL
	if err := refmodel.WriteBinarySTL(abs, tmpName); err != nil {
		return ""
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return ""
	}
	return dst
}

// sceneForWorkspace resuelve la referencia de la escena contra el workspace y, si
// hay corte, le pone la caja del STL (Generate la necesita para dimensionar el
// plano y para separar las dos mitades).
func sceneForWorkspace(wsDir string, scene meshedit.Scene) (meshedit.Scene, error) {
	abs, rel, err := refmodel.ResolveRef(wsDir, scene.Ref)
	if err != nil {
		return scene, err
	}
	scene.Ref = rel
	scene.Bounds = nil
	if scene.Cut != nil {
		info, err := mesh.InfoFromFile(abs)
		if err != nil {
			return scene, i18n.Errorf(i18n.Msg{ES: "no se pudo medir %s: %w", EN: "could not measure %s: %w"}, rel, err)
		}
		scene.Bounds = &meshedit.Bounds{Min: info.Min, Max: info.Max}
	}
	return scene, nil
}

// PreviewMeshEdit devuelve el código que generaría la escena, sin escribir nada.
func (a *App) PreviewMeshEdit(workspaceId int64, scene meshedit.Scene) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(workspaceId)
	if err != nil {
		return "", err
	}
	scene, err = sceneForWorkspace(w.Path, scene)
	if err != nil {
		return "", err
	}
	desc := i18n.T(i18n.Msg{ES: "Edición de un STL", EN: "STL edit"})
	return meshedit.Generate(scene, meshedit.Header{Desc: desc}, i18n.Lang())
}

// ApplyMeshEdit guarda la escena como un checkpoint NUEVO hijo de parent y
// devuelve su nombre. No renderiza: lo hace el watcher al ver el archivo, como con
// cualquier checkpoint nuevo. Como SaveAsCheckpoint, el hijo se vuelve el padre
// activo si el usuario estaba parado en parent, y no se le atribuye a ninguna
// sesión de agente. label es el título (da el nombre del archivo y la descripción).
func (a *App) ApplyMeshEdit(workspaceId int64, parent, label string, scene meshedit.Scene) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(workspaceId)
	if err != nil {
		return "", err
	}
	if err := validCheckpointName(parent); err != nil {
		return "", err
	}
	scene, err = sceneForWorkspace(w.Path, scene)
	if err != nil {
		return "", err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = i18n.T(i18n.Msg{ES: "Edición de STL", EN: "STL edit"})
	}
	src, err := meshedit.Generate(scene, meshedit.Header{Parent: parent, Desc: label}, i18n.Lang())
	if err != nil {
		return "", err
	}
	name, err := checkpoint.Create(w.Path, parent, label, label, src)
	if err != nil {
		return "", err
	}
	a.checkpoints().markOwn(w.ID, name)
	return name, nil
}
