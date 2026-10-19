package mcp

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"scadlab/backend/appdata"
	"scadlab/backend/checkpoint"
	"scadlab/backend/detect"
	"scadlab/backend/estimate"
	"scadlab/backend/i18n"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/scadwasm"
	"scadlab/backend/store"
)

// env es todo lo que comparten las herramientas: el workspace, el motor de
// render, la configuración y lo último que se renderizó.
type env struct {
	ws      string // absoluto, sin enlaces simbólicos
	dataDir string
	log     *log.Logger

	engine    *render.Engine
	engineErr error
	// wasm es el motor integrado (OpenSCAD en WebAssembly): el respaldo cuando
	// no hay un OpenSCAD instalado. Un solo Runner por proceso.
	wasm  *scadwasm.Runner
	store *store.Store // nil si scadlab.db no existe o no abrió: se trabaja con los defaults
	parts *parts.Store // caché de partes por color (la misma de la app); nil sin directorio de datos

	// owned dice qué recursos abrió este env y por lo tanto cierra. Lo que le
	// inyectó la app (Toolbox) es de la app: cerrarlo aquí le apagaría el motor
	// y la base a todo lo demás.
	owned struct{ engine, wasm, store bool }

	mu      sync.Mutex
	scad    scadCache
	results map[string]render.Result // último render de este proceso, por checkpoint
}

// injected son los recursos que la app le presta al env en vez de que abra los
// suyos (nil = abrir como hace el servidor `--mcp`).
type injected struct {
	store  *store.Store
	engine *render.Engine
	wasm   *scadwasm.Runner
	parts  *parts.Store
}

// scadCache recuerda dónde está OpenSCAD: detect.OpenSCAD corre `openscad
// --version` (cientos de ms), y un agente que renderiza en bucle lo pagaría en
// cada llamada. Un «no encontrado» no se cachea: instalarlo con el agente
// abierto funciona en la próxima llamada.
type scadCache struct {
	override string
	tool     detect.Tool
}

func newEnv(opts Options, lg *log.Logger) (*env, error) { return newEnvWith(opts, lg, injected{}) }

// newEnvWith es newEnv con recursos prestados: el servidor `--mcp` no presta
// ninguno y lo abre todo; la caja de herramientas de la app presta los suyos.
func newEnvWith(opts Options, lg *log.Logger, in injected) (*env, error) {
	ws, err := resolveWorkspace(opts.WorkspaceDir)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo abrir la carpeta del workspace: %w", EN: "could not open the workspace folder: %w"}, err)
	}
	if st, err := os.Stat(ws); err != nil || !st.IsDir() {
		return nil, i18n.Errorf(i18n.Msg{ES: "el workspace %s no es una carpeta", EN: "the workspace %s is not a folder"}, ws)
	}
	e := &env{ws: ws, log: lg, results: map[string]render.Result{}}
	e.store, e.engine, e.wasm, e.parts = in.store, in.engine, in.wasm, in.parts

	dir, derr := appdata.Dir()
	if derr != nil {
		lg.Printf("directorio de datos: %v", derr)
		e.engineErr = derr
		if e.wasm == nil {
			e.wasm, e.owned.wasm = scadwasm.New(""), true
		}
	} else {
		e.dataDir = dir
		// La caché de compilación del wasm es la misma que usa la app: el
		// primer render de un agente no paga los 2-3 s si la app ya compiló.
		if e.wasm == nil {
			e.wasm, e.owned.wasm = scadwasm.New(filepath.Join(dir, "cache", "wasm")), true
		}
		if e.parts == nil {
			e.parts = parts.NewStore(filepath.Join(dir, "cache", "parts"))
		}
		// La caché de mallas es la misma que usa la app: lo que renderiza el
		// agente queda disponible para el visor y viceversa.
		if e.engine == nil {
			e.engine, e.engineErr = render.NewEngine(filepath.Join(dir, "cache", "meshes"))
			e.owned.engine = e.engine != nil
			if e.engineErr != nil {
				lg.Printf("motor de render: %v", e.engineErr)
			}
		}
		// La base solo se abre si ya existe: crearla sería un efecto
		// colateral de lanzar un agente, y la app la crea al arrancar.
		if e.store == nil {
			if dbPath, err := appdata.DBPath(); err == nil {
				if _, err := os.Stat(dbPath); err == nil {
					if st, err := store.Open(dbPath); err != nil {
						lg.Printf("abrir %s: %v", dbPath, err)
					} else {
						e.store, e.owned.store = st, true
					}
				}
			}
		}
	}
	e.settings() // fija el idioma desde la configuración
	return e, nil
}

// close libera lo que el env abrió; lo prestado queda como estaba.
func (e *env) close() {
	if e.owned.engine && e.engine != nil {
		e.engine.Close()
	}
	if e.owned.wasm && e.wasm != nil {
		_ = e.wasm.Close()
	}
	if e.owned.store && e.store != nil {
		_ = e.store.Close()
	}
}

// settings lee la configuración vigente. Se relee en cada llamada: el usuario
// puede cambiar la ruta de OpenSCAD, el motor o el idioma en la app mientras el
// agente sigue trabajando, y un proceso MCP dura toda su sesión. También fija
// el idioma de los mensajes (i18n es global del proceso, y este proceso es solo
// del servidor).
func (e *env) settings() store.Settings {
	s := store.DefaultSettings()
	if e.store != nil {
		if got, err := e.store.GetSettings(); err == nil {
			s = got
			i18n.SetLang(s.Language)
		}
	}
	return s
}

// openscad resuelve OpenSCAD con caché. Found=false si no está.
func (e *env) openscad(ctx context.Context, override string) detect.Tool {
	e.mu.Lock()
	c := e.scad
	e.mu.Unlock()
	if c.tool.Found && c.override == override {
		if _, err := os.Stat(c.tool.Path); err == nil {
			return c.tool
		}
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tool := detect.OpenSCAD(dctx, override)
	if tool.Found {
		e.mu.Lock()
		e.scad = scadCache{override: override, tool: tool}
		e.mu.Unlock()
	}
	return tool
}

// renderOptions arma las opciones del motor desde la configuración.
func (e *env) renderOptions(ctx context.Context, s store.Settings) (render.Options, detect.Tool) {
	tool := e.openscad(ctx, s.OpenSCADPath)
	return render.Options{
		OpenSCADPath:   tool.Path,
		WASM:           e.wasm,
		Backend:        s.RenderBackend,
		TimeoutSeconds: s.RenderTimeoutSec,
	}, tool
}

// usesWASM dice si, sin un OpenSCAD instalado, las herramientas trabajan con el
// motor integrado.
func (e *env) usesWASM(tool detect.Tool) bool { return !tool.Found && e.wasm.Available() }

// engineNote es el aviso que acompaña a un resultado hecho con el motor
// integrado (vacío si se usó el OpenSCAD instalado).
func engineNote() string {
	return msgf(i18n.Msg{
		ES: "Hecho con el motor integrado de ScadLab (OpenSCAD %s compilado a WebAssembly) porque no hay un OpenSCAD instalado. Es más lento que el instalado; el resultado es el mismo.",
		EN: "Done with ScadLab's built-in engine (OpenSCAD %s compiled to WebAssembly) because no OpenSCAD is installed. It is slower than a native install; the result is the same.",
	}, scadwasm.Version)
}

// storedWorkspace busca el workspace registrado en la app que apunta a esta
// carpeta ("" nombre si no está registrado: un agente puede lanzarse en una
// carpeta que la app no conoce).
func (e *env) storedWorkspace() (store.Workspace, bool) {
	if e.store == nil {
		return store.Workspace{}, false
	}
	list, err := e.store.ListWorkspaces()
	if err != nil {
		return store.Workspace{}, false
	}
	for _, w := range list {
		if samePath(w.Path, e.ws) {
			return w, true
		}
	}
	return store.Workspace{}, false
}

func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	rel, err := filepath.Rel(a, b)
	return err == nil && rel == "."
}

// printer es la impresora del workspace: la registrada en la app; si no, la de
// printer.json (que escribe ScadLab al crear el workspace); si no, la de
// Configuración.
func (e *env) printer(s store.Settings) printers.Printer {
	if w, ok := e.storedWorkspace(); ok {
		p, _ := printers.ByID(w.PrinterID)
		return p
	}
	if raw, err := os.ReadFile(filepath.Join(e.ws, "printer.json")); err == nil {
		var pj struct {
			Printer string `json:"printer"`
		}
		if json.Unmarshal(raw, &pj) == nil && pj.Printer != "" {
			p, _ := printers.ByID(pj.Printer)
			return p
		}
	}
	p, _ := printers.ByID(s.DefaultPrinter)
	return p
}

// wsName es el nombre del workspace para los archivos exportados.
func (e *env) wsName() string {
	if w, ok := e.storedWorkspace(); ok && w.Name != "" {
		return w.Name
	}
	return filepath.Base(e.ws)
}

// material es el material por defecto de la configuración (para el peso). Sin
// base o sin ese material, PLA.
func (e *env) material(s store.Settings) store.Material {
	fallback := store.Material{ID: "pla", Name: "PLA", DensityGCm3: 1.24}
	if e.store == nil {
		return fallback
	}
	list, err := e.store.ListMaterials()
	if err != nil {
		return fallback
	}
	for _, m := range list {
		if m.ID == s.DefaultMaterial && m.DensityGCm3 > 0 {
			return m
		}
	}
	return fallback
}

// estimateMaterials son los materiales de la app para estimar el peso. Sin base
// (o sin materiales) queda PLA, como en material().
func (e *env) estimateMaterials() []estimate.Material {
	out := []estimate.Material{{ID: "pla", Name: "PLA", DensityGCm3: 1.24, CostPerKg: 20}}
	if e.store == nil {
		return out
	}
	list, err := e.store.ListMaterials()
	if err != nil || len(list) == 0 {
		return out
	}
	out = out[:0]
	for _, m := range list {
		out = append(out, estimate.Material{ID: m.ID, Name: m.Name, DensityGCm3: m.DensityGCm3, CostPerKg: m.CostPerKg})
	}
	return out
}

// within dice si p está dentro de root (o es root).
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// checkpointFile resuelve el argumento `file` de una herramienta a un
// checkpoint que existe DENTRO del workspace. Vacío = el más nuevo. Devuelve el
// nombre y la ruta absoluta.
//
// Toda ruta pasa por aquí: el nombre es un archivo suelto terminado en .scad
// (checkpoint.ValidName) y, si en el disco es un enlace simbólico, su destino
// tiene que seguir dentro del workspace — un `001-x.scad -> /etc/passwd` no
// puede salir por read_checkpoint.
func (e *env) checkpointFile(file string) (string, string, error) {
	if file == "" {
		file = checkpoint.Latest(e.ws)
		if file == "" {
			return "", "", i18n.New(i18n.Msg{
				ES: "el workspace no tiene checkpoints todavía: crea el primero con create_checkpoint",
				EN: "the workspace has no checkpoints yet: create the first one with create_checkpoint",
			})
		}
	}
	if err := checkpoint.ValidName(file); err != nil {
		return "", "", err
	}
	path := filepath.Join(e.ws, file)
	st, err := os.Lstat(path)
	if err != nil || st.IsDir() {
		return "", "", i18n.Errorf(i18n.Msg{ES: "no se encontró el checkpoint %s", EN: "checkpoint %s was not found"}, file)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(path)
		if err != nil || !within(e.ws, real) {
			return "", "", i18n.Errorf(i18n.Msg{ES: "el checkpoint %s apunta fuera del workspace", EN: "checkpoint %s points outside the workspace"}, file)
		}
	}
	return file, path, nil
}

// render renderiza un checkpoint con el motor de la app y aplica la regla de
// RENDER_ERRORS.md de app_render.go: el archivo describe el estado del
// checkpoint MÁS NUEVO (sobre el que itera el agente). Renderizar uno viejo no
// lo toca; un cancelado o un OpenSCAD ausente tampoco: no dicen nada sobre si el
// modelo está bien.
func (e *env) render(ctx context.Context, name string) (render.Result, store.Settings, error) {
	s := e.settings()
	if e.engine == nil {
		return render.Result{}, s, i18n.Errorf(i18n.Msg{ES: "el motor de render no está disponible: %v", EN: "the render engine is not available: %v"}, e.engineErr)
	}
	opts, _ := e.renderOptions(ctx, s)
	// Una cola por checkpoint: dos renders en paralelo de archivos distintos no
	// se cancelan entre sí (el motor cancela por clave), y repetir el mismo sí
	// reemplaza al anterior.
	req := render.Request{WorkspaceKey: "mcp:" + e.ws + ":" + name, WorkspaceDir: e.ws, FileName: name}
	res, err := e.engine.Render(ctx, req, opts)
	if err != nil {
		return res, s, err
	}
	if res.FileName == "" {
		res.FileName = name
	}
	switch {
	case checkpoint.Latest(e.ws) != name:
	case res.Status == render.StatusError:
		if werr := render.WriteRenderErrors(e.ws, res); werr != nil {
			e.log.Printf("escribir RENDER_ERRORS.md: %v", werr)
		}
	case res.Status == render.StatusOK:
		if cerr := render.ClearRenderErrors(e.ws); cerr != nil {
			e.log.Printf("borrar RENDER_ERRORS.md: %v", cerr)
		}
	}
	if res.Status != render.StatusCancelled {
		e.mu.Lock()
		e.results[name] = res
		e.mu.Unlock()
	}
	return res, s, nil
}
