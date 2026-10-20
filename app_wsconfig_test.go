package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"scadlab/backend/i18n"
	"scadlab/backend/render"
	"scadlab/backend/skills"
	"scadlab/backend/slicers"
	"scadlab/backend/store"
)

// Ninguna prueba de este paquete debe depender de qué slicers tenga instalados la
// máquina de quien las corre (ni, por supuesto, abrirlos): el entorno de búsqueda
// apunta a carpetas que no existen. Las pruebas que necesitan un slicer instalado
// lo arman con isolateSlicers y lanzan por el registrador de recordLaunch.
func init() {
	none := filepath.Join(os.TempDir(), "scadlab-test-sin-slicers")
	slicerEnv = func() slicers.Env {
		return slicers.Env{
			GOOS: runtime.GOOS, Home: none, Applications: filepath.Join(none, "Applications"),
			ProgramFiles: filepath.Join(none, "pf"), ProgramFilesX86: filepath.Join(none, "pf86"), LocalAppData: filepath.Join(none, "local"),
		}
	}
}

// isolateSlicers apunta la búsqueda de slicers a carpetas temporales y las devuelve.
func isolateSlicers(t *testing.T) slicers.Env {
	t.Helper()
	root := t.TempDir()
	env := slicers.Env{
		GOOS: runtime.GOOS, Home: filepath.Join(root, "home"), Applications: filepath.Join(root, "Applications"),
		ProgramFiles: filepath.Join(root, "pf"), ProgramFilesX86: filepath.Join(root, "pf86"), LocalAppData: filepath.Join(root, "local"),
	}
	prev := slicerEnv
	slicerEnv = func() slicers.Env { return env }
	t.Cleanup(func() { slicerEnv = prev })
	return env
}

// installSlicer deja un slicer «instalado» donde lo busca el registro en este SO y
// devuelve la ruta que se debe encontrar. Solo macOS y Windows.
func installSlicer(t *testing.T, env slicers.Env, id string) string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		d, _ := slicers.ByID(id)
		p := filepath.Join(env.Applications, d.Mac[0])
		if err := os.MkdirAll(filepath.Join(p, "Contents", "MacOS"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	case "windows":
		d, _ := slicers.ByID(id)
		p := filepath.Join(env.ProgramFiles, filepath.FromSlash(d.Win[0]))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Skip("solo macOS y Windows")
	return ""
}

func TestDetectSlicersListsEveryOneOfTheRegistry(t *testing.T) {
	env := isolateSlicers(t)
	a := newTestApp(t)
	got := a.DetectSlicers()
	if len(got) != len(slicers.Registry) || len(got) < 8 {
		t.Fatalf("slicers: %+v", got)
	}
	for i, d := range slicers.Registry {
		if got[i].ID != d.ID || got[i].Name != d.Name || got[i].Found || got[i].Path != "" || got[i].Configured != "" {
			t.Errorf("%s sin instalar: %+v", d.ID, got[i])
		}
	}
	// Instalado en el lugar habitual: se encuentra, sin ruta configurada.
	prusa := installSlicer(t, env, "prusaslicer")
	for _, s := range a.DetectSlicers() {
		switch s.ID {
		case "prusaslicer":
			if !s.Found || s.Path != prusa || s.Configured != "" {
				t.Errorf("PrusaSlicer instalado: %+v", s)
			}
		default:
			if s.Found {
				t.Errorf("%s no está instalado y figura como encontrado: %+v", s.ID, s)
			}
		}
	}
}

func TestConfiguredSlicerPathWins(t *testing.T) {
	env := isolateSlicers(t)
	a := newTestApp(t)
	installed := installSlicer(t, env, "orcaslicer")
	custom := fakeSlicerAt(t, "orca-custom")
	s, _ := a.store.GetSettings()
	s.SlicerPaths = map[string]string{"orcaslicer": custom}
	if err := a.store.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	for _, sl := range a.DetectSlicers() {
		if sl.ID == "orcaslicer" && (sl.Path != custom || sl.Configured != custom || !sl.Found) {
			t.Errorf("la configurada debía ganar sobre %s: %+v", installed, sl)
		}
	}
	// Una configurada que no existe se ignora y rige la autodetección, pero se informa.
	s.SlicerPaths = map[string]string{"orcaslicer": filepath.Join(t.TempDir(), "no-esta")}
	a.store.SaveSettings(s)
	for _, sl := range a.DetectSlicers() {
		if sl.ID == "orcaslicer" && (sl.Path != installed || sl.Configured != s.SlicerPaths["orcaslicer"]) {
			t.Errorf("configurada inexistente: %+v", sl)
		}
	}
}

// bambuStudioPath (la clave anterior a S14) sigue valiendo para Bambu Studio, y
// slicerPaths["bambu-studio"] gana si están las dos.
func TestBambuStudioKeepsItsLegacyPath(t *testing.T) {
	isolateSlicers(t)
	a := newTestApp(t)
	legacy, newer := fakeSlicerAt(t, "bambu-viejo"), fakeSlicerAt(t, "bambu-nuevo")
	s, _ := a.store.GetSettings()
	s.BambuStudioPath = legacy
	a.store.SaveSettings(s)
	find := func() Slicer {
		for _, sl := range a.DetectSlicers() {
			if sl.ID == "bambu-studio" {
				return sl
			}
		}
		t.Fatal("falta bambu-studio")
		return Slicer{}
	}
	if sl := find(); sl.Path != legacy || !sl.Found {
		t.Errorf("bambuStudioPath: %+v", sl)
	}
	s.SlicerPaths = map[string]string{"bambu-studio": newer}
	a.store.SaveSettings(s)
	if sl := find(); sl.Path != newer {
		t.Errorf("slicerPaths gana: %+v", sl)
	}
}

// fakeSlicerAt crea el «slicer» falso de este sistema con ese nombre.
func fakeSlicerAt(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		p := filepath.Join(dir, name+".exe")
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := filepath.Join(dir, name+".app")
	if err := os.MkdirAll(filepath.Join(p, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// Cualquier slicer del registro se abre con el comando de su sistema, con el
// lanzador inyectado: solo se comprueban los argumentos.
func TestOpenInSlicerWorksForEveryRegisteredSlicer(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("solo macOS y Windows")
	}
	a := newTestApp(t)
	env := isolateSlicers(t)
	calls := recordLaunch(t)
	file := filepath.Join(t.TempDir(), "pieza.3mf")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range slicers.Registry {
		*calls = (*calls)[:0]
		app := installSlicer(t, env, d.ID)
		if err := a.OpenInSlicer(d.ID, file); err != nil {
			t.Errorf("%s: %v", d.ID, err)
			continue
		}
		var want launchCall
		if runtime.GOOS == "darwin" {
			want = launchCall{"open", []string{"-a", app, file}}
		} else {
			want = launchCall{app, []string{file}}
		}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("%s: lanzó %+v, quería %+v", d.ID, *calls, want)
		}
	}
	if err := a.OpenInSlicer("orca", file); err == nil {
		t.Error("un id fuera del registro debía fallar")
	}
}

func TestOpenInSlicerErrorsNameTheSlicer(t *testing.T) {
	a := newTestApp(t)
	isolateSlicers(t)
	calls := recordLaunch(t)
	file := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	for _, lang := range []string{"es", "en"} {
		i18n.SetLang(lang)
		err := a.OpenInSlicer("snapmaker-orca", file)
		if err == nil || !strings.Contains(err.Error(), "Snapmaker Orca") {
			t.Errorf("%s extensión: %v", lang, err)
		}
		os.WriteFile(file+".3mf", []byte("x"), 0o644)
		err = a.OpenInSlicer("snapmaker-orca", file+".3mf")
		if err == nil || !strings.Contains(err.Error(), "Snapmaker Orca") {
			t.Errorf("%s no instalado: %v", lang, err)
		}
	}
	i18n.SetLang("en")
	if len(*calls) != 0 {
		t.Errorf("no debía lanzar nada: %+v", *calls)
	}
}

// --- Configuración por workspace -------------------------------------------

// installedSkills lista, por carpeta de agente, las skills instaladas en un workspace.
func installedSkills(t *testing.T, ws string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, rel := range skills.AgentDirs {
		ents, err := os.ReadDir(filepath.Join(ws, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		names := []string{}
		for _, e := range ents {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		out[rel] = names
	}
	return out
}

func TestCreateWorkspaceWithInstallsOnlyItsSkills(t *testing.T) {
	a := newTestApp(t)
	w, err := a.CreateWorkspaceWith(NewWorkspaceInput{
		Name: "Soporte", PrinterID: "bambu-p1s",
		Skills: []string{"openscad-core", "openscad-templates"},
		Print:  store.WorkspacePrint{Colors: 2, Material: "petg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.PrinterID != "bambu-p1s" || w.Print.Colors != 2 || w.Print.Material != "petg" ||
		!reflect.DeepEqual(w.Skills, []string{"openscad-core", "openscad-templates"}) {
		t.Errorf("workspace: %+v", w)
	}
	for rel, names := range installedSkills(t, w.Path) {
		if !reflect.DeepEqual(names, []string{"openscad-core", "openscad-templates"}) {
			t.Errorf("%s: instaladas %v, solo debían estar las elegidas", rel, names)
		}
	}
	// Las reglas llevan la impresora efectiva: dos colores, no los 16 de la P1S.
	raw, err := os.ReadFile(filepath.Join(w.Path, "printer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pj map[string]any
	json.Unmarshal(raw, &pj)
	if pj["printer"] != "bambu-p1s" || pj["colors"] != 2.0 || pj["max_colors"] != 16.0 {
		t.Errorf("printer.json: %s", raw)
	}
	// La lista quedó guardada en la base, explícita.
	if got, _ := a.store.GetWorkspace(w.ID); !reflect.DeepEqual(got.Skills, w.Skills) || got.Print.Colors != 2 {
		t.Errorf("base: %+v", got)
	}
}

// Sin lista, el workspace lleva las skills por defecto de su impresora (con la
// skill de su marca), guardadas explícitas; apagar una skill en Configuración
// después NO le saca nada.
func TestCreateWorkspaceDefaultsAreExplicitAndFrozen(t *testing.T) {
	a := newTestApp(t)
	// El registro de skills se puebla al listarlas.
	if _, err := a.ListSkills(); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSkillEnabled("openscad-libraries", false); err != nil {
		t.Fatal(err)
	}
	w, err := a.CreateWorkspace("Llavero", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bambu-lab-printers", "openscad-3mf-multicolor", "openscad-core", "openscad-from-image", "openscad-stl-editing", "openscad-templates"}
	if !reflect.DeepEqual(w.Skills, want) {
		t.Fatalf("por defecto: %v, esperaba %v", w.Skills, want)
	}
	for rel, names := range installedSkills(t, w.Path) {
		if !reflect.DeepEqual(names, want) {
			t.Errorf("%s: %v", rel, names)
		}
	}
	// Encender o apagar en Configuración ya no toca un workspace existente.
	a.SetSkillEnabled("openscad-core", false)
	a.SetSkillEnabled("openscad-libraries", true)
	if err := a.SyncWorkspaceSkills(w.ID); err != nil {
		t.Fatal(err)
	}
	for rel, names := range installedSkills(t, w.Path) {
		if !reflect.DeepEqual(names, want) {
			t.Errorf("%s: el global cambió un workspace existente: %v", rel, names)
		}
	}
	// ... pero sí a los nuevos.
	w2, err := a.CreateWorkspace("Otro", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w2.Skills, []string{"bambu-lab-printers", "openscad-3mf-multicolor", "openscad-from-image", "openscad-libraries", "openscad-stl-editing", "openscad-templates"}) {
		t.Errorf("el nuevo hereda el global: %v", w2.Skills)
	}
}

func TestCreateWorkspaceWithRejectsBadInput(t *testing.T) {
	a := newTestApp(t)
	root := a.EffectiveWorkspacesRoot()
	for name, in := range map[string]NewWorkspaceInput{
		"impresora inexistente": {Name: "A", PrinterID: "no-existe"},
		"skill inexistente":     {Name: "B", PrinterID: "bambu-a1", Skills: []string{"fantasma"}},
		"colores de más":        {Name: "C", PrinterID: "bambu-a1", Print: store.WorkspacePrint{Colors: 17}},
		"relleno fuera":         {Name: "D", PrinterID: "bambu-a1", Print: store.WorkspacePrint{InfillPercent: ptr(130.0)}},
		"material inexistente":  {Name: "E", PrinterID: "bambu-a1", Print: store.WorkspacePrint{Material: "x"}},
		"sin nombre":            {Name: "  ", PrinterID: "bambu-a1"},
	} {
		if _, err := a.CreateWorkspaceWith(in); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	if ents, _ := os.ReadDir(root); len(ents) != 0 {
		t.Errorf("un alta inválida dejó carpetas: %v", ents)
	}
	if list, _ := a.store.ListWorkspaces(); len(list) != 0 {
		t.Errorf("un alta inválida registró workspaces: %+v", list)
	}
}

func ptr[T any](v T) *T { return &v }

func TestImportWorkspaceStoresExplicitSkillsForItsPrinter(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	w, err := a.ImportWorkspace(dir, "bambu-x1c")
	if err != nil {
		t.Fatal(err)
	}
	if w.PrinterID != "bambu-x1c" || w.Skills == nil || !contains(w.Skills, "bambu-lab-printers") || !contains(w.Skills, "openscad-core") {
		t.Errorf("importado: %+v", w)
	}
	for rel, names := range installedSkills(t, dir) {
		if !reflect.DeepEqual(names, w.Skills) {
			t.Errorf("%s: %v vs %v", rel, names, w.Skills)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// configEvents captura los workspace:config que emite la app.
type configEvents struct {
	mu    sync.Mutex
	views []WorkspaceConfigView
}

func captureConfig(a *App) *configEvents {
	ce := &configEvents{}
	prev := a.emitter
	a.emitter = func(name string, payload any) {
		if v, ok := payload.(WorkspaceConfigView); ok && name == evWorkspaceConfig {
			ce.mu.Lock()
			ce.views = append(ce.views, v)
			ce.mu.Unlock()
		}
		if prev != nil {
			prev(name, payload)
		}
	}
	return ce
}

func (ce *configEvents) all() []WorkspaceConfigView {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	return append([]WorkspaceConfigView(nil), ce.views...)
}

func TestSaveWorkspaceConfigAppliesAndEmits(t *testing.T) {
	a := newTestApp(t)
	ce := captureConfig(a)
	w, err := a.CreateWorkspace("Caja", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	// Una copia editada a mano de una skill que va a salir de la lista.
	edited := filepath.Join(w.Path, ".claude", "skills", "openscad-templates", "SKILL.md")
	if err := os.WriteFile(edited, []byte("editada\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := a.SaveWorkspaceConfig(w.ID, WorkspaceConfigInput{
		PrinterID: "bambu-h2d",
		Skills:    []string{"openscad-core", "bambu-lab-printers"},
		Print:     store.WorkspacePrint{Colors: 8, InfillPercent: ptr(0.0), WallLoops: ptr(4)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SyncError != "" {
		t.Fatalf("SyncError: %s", res.SyncError)
	}
	c := res.Config
	if c.Workspace.PrinterID != "bambu-h2d" || c.Printer.ID != "bambu-h2d" || c.Colors != 8 || c.Printer.MulticolorSlots != 8 || c.Printer.MaxColors != 25 {
		t.Errorf("vista: printer %s colores %d (%d/%d)", c.Printer.ID, c.Colors, c.Printer.MulticolorSlots, c.Printer.MaxColors)
	}
	if !reflect.DeepEqual(c.Skills, []string{"bambu-lab-printers", "openscad-core"}) || len(c.DefaultSkills) == 0 {
		t.Errorf("skills: %v / por defecto %v", c.Skills, c.DefaultSkills)
	}
	if c.Print.Colors != 8 || *c.Print.WallLoops != 4 || c.Effective.WallLoops != 4 || c.Effective.InfillPercent != 0 || c.Effective.FilamentDiameterMm != 1.75 {
		t.Errorf("ajustes: %+v / %+v", c.Print, c.Effective)
	}
	if !reflect.DeepEqual(res.Kept, []string{"openscad-templates"}) || !contains(res.Removed, "openscad-3mf-multicolor") {
		t.Errorf("Removed %v Kept %v", res.Removed, res.Kept)
	}
	if raw, _ := os.ReadFile(edited); string(raw) != "editada\n" {
		t.Errorf("la copia editada cambió: %q", raw)
	}
	if evs := ce.all(); len(evs) != 1 || evs[0].Workspace.ID != w.ID || evs[0].Printer.ID != "bambu-h2d" || evs[0].Colors != 8 {
		t.Errorf("workspace:config emitido: %+v", evs)
	}
	// Un valor inválido es error, no emite y no cambia nada.
	if _, err := a.SaveWorkspaceConfig(w.ID, WorkspaceConfigInput{PrinterID: "bambu-h2d", Print: store.WorkspacePrint{Colors: 99}}); err == nil {
		t.Error("colores de más debía fallar")
	}
	if _, err := a.SaveWorkspaceConfig(w.ID, WorkspaceConfigInput{PrinterID: "bambu-h2d", Print: store.WorkspacePrint{WallLoops: ptr(0)}}); err == nil {
		t.Error("perímetros fuera de rango debía fallar")
	}
	if evs := ce.all(); len(evs) != 1 {
		t.Errorf("un guardado inválido emitió: %d eventos", len(evs))
	}
	got, err := a.GetWorkspaceConfig(w.ID)
	if err != nil || got.Colors != 8 || got.Printer.ID != "bambu-h2d" {
		t.Errorf("GetWorkspaceConfig: %+v, %v", got, err)
	}
	// El id de impresora vacío conserva la actual.
	r2, err := a.SaveWorkspaceConfig(w.ID, WorkspaceConfigInput{Skills: []string{"openscad-core"}, Print: store.WorkspacePrint{Colors: 3}})
	if err != nil || r2.Config.Printer.ID != "bambu-h2d" || r2.Config.Colors != 3 {
		t.Errorf("sin impresora: %+v, %v", r2.Config.Printer.ID, err)
	}
}

// El mensaje de un valor fuera de rango sale en los dos idiomas.
func TestSaveWorkspaceConfigErrorsAreBilingual(t *testing.T) {
	a := newTestApp(t)
	w, _ := a.CreateWorkspace("Caja", "bambu-a1")
	t.Cleanup(func() { i18n.SetLang("en") })
	for lang, want := range map[string]string{"es": "entre 1 y 16", "en": "between 1 and 16"} {
		i18n.SetLang(lang)
		_, err := a.SaveWorkspaceConfig(w.ID, WorkspaceConfigInput{Print: store.WorkspacePrint{Colors: 17}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", lang, err)
		}
	}
}

func TestDefaultWorkspaceSkillsAndBrands(t *testing.T) {
	a := newTestApp(t)
	got, err := a.DefaultWorkspaceSkills("bambu-a1")
	if err != nil || !contains(got, "bambu-lab-printers") || !contains(got, "openscad-core") {
		t.Errorf("Bambu: %v, %v", got, err)
	}
	if _, err := a.DefaultWorkspaceSkills("no-existe"); err == nil {
		t.Error("impresora inexistente debía fallar")
	}
	if def, err := a.DefaultWorkspaceSkills(""); err != nil || !reflect.DeepEqual(def, got) {
		t.Errorf("impresora por defecto: %v, %v", def, err)
	}
	brands, err := a.ListPrinterBrands()
	if err != nil || len(brands) == 0 || brands[0].ID != "bambu" || brands[0].Skill != "bambu-lab-printers" {
		t.Errorf("marcas: %+v, %v", brands, err)
	}
}

// Renombrar una skill propia renombra su entrada en las listas de los workspaces
// que la tienen y deja instalada la copia nueva (en los abiertos).
func TestRenameSkillRenamesTheEntryInWorkspaceLists(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	with, err := a.CreateWorkspaceWith(NewWorkspaceInput{Name: "Con", PrinterID: "bambu-a1", Skills: []string{"openscad-core", "gear-helper"}})
	if err != nil {
		t.Fatal(err)
	}
	without, err := a.CreateWorkspaceWith(NewWorkspaceInput{Name: "Sin", PrinterID: "bambu-a1", Skills: []string{"openscad-core"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.OpenProject(with.ID); err != nil {
		t.Fatal(err)
	}
	res, err := a.RenameSkill("gear-helper", "gear-tools")
	if err != nil {
		t.Fatal(err)
	}
	if res.SyncError != "" {
		t.Fatalf("SyncError: %s", res.SyncError)
	}
	w1, _ := a.store.GetWorkspace(with.ID)
	if !reflect.DeepEqual(w1.Skills, []string{"gear-tools", "openscad-core"}) {
		t.Errorf("lista del que la tenía: %v", w1.Skills)
	}
	w2, _ := a.store.GetWorkspace(without.ID)
	if !reflect.DeepEqual(w2.Skills, []string{"openscad-core"}) {
		t.Errorf("lista del que no la tenía: %v", w2.Skills)
	}
	for rel, names := range installedSkills(t, with.Path) {
		if !contains(names, "gear-tools") || contains(names, "gear-helper") {
			t.Errorf("%s: %v", rel, names)
		}
	}
	// Al otro workspace no se le instaló nada nuevo.
	for rel, names := range installedSkills(t, without.Path) {
		if contains(names, "gear-tools") {
			t.Errorf("%s: se instaló en un workspace que no la tiene: %v", rel, names)
		}
	}
}

// Editar una skill propia solo la reinstala donde el workspace la tiene activa.
func TestSyncOpenWorkspacesRespectsEachList(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	with, _ := a.CreateWorkspaceWith(NewWorkspaceInput{Name: "Con", PrinterID: "bambu-a1", Skills: []string{"gear-helper"}})
	without, _ := a.CreateWorkspaceWith(NewWorkspaceInput{Name: "Sin", PrinterID: "bambu-a1", Skills: []string{"openscad-core"}})
	for _, id := range []int64{with.ID, without.ID} {
		if _, err := a.OpenProject(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(with.Path, ".claude", "skills", "gear-helper")); err != nil {
		t.Fatal(err)
	}
	if msg := a.syncOpenWorkspaces("gear-helper"); msg != "" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(filepath.Join(with.Path, ".claude", "skills", "gear-helper", "SKILL.md")); err != nil {
		t.Errorf("tenía que reinstalarse donde está activa: %v", err)
	}
	if _, err := os.Stat(filepath.Join(without.Path, ".claude", "skills", "gear-helper")); err == nil {
		t.Error("se instaló donde no está activa")
	}
}

// --- La config efectiva llega a lo que usa la impresora ---------------------

func TestExportJobAndPartsUseTheEffectivePrinter(t *testing.T) {
	a := newTestApp(t)
	w, err := a.CreateWorkspaceWith(NewWorkspaceInput{
		Name: "Efectiva", PrinterID: "bambu-p1s",
		Print: store.WorkspacePrint{Colors: 2, Material: "petg", InfillPercent: ptr(40.0), WallLoops: ptr(3), FilamentDiameterMm: ptr(2.85)},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Las advertencias de exportación se miden contra los colores del workspace.
	file := "001-x.scad"
	os.WriteFile(filepath.Join(w.Path, file), []byte("cube(10);\n"), 0o644)
	job, _, _, err := a.exportJob(w.ID, file)
	if err != nil {
		t.Fatal(err)
	}
	if job.Printer.ID != "bambu-p1s" || job.Printer.MulticolorSlots != 2 || job.Printer.MaxColors != 16 {
		t.Errorf("impresora de la exportación: %s %d/%d", job.Printer.ID, job.Printer.MulticolorSlots, job.Printer.MaxColors)
	}
	// La estimación usa los ajustes del workspace, no los globales.
	mesh := filepath.Join(a.dataDir, "cache", "meshes", strings.Repeat("a", 64)+".stl")
	os.MkdirAll(filepath.Dir(mesh), 0o755)
	os.WriteFile(mesh, []byte("x"), 0o644)
	a.results = map[int64]map[string]render.Result{w.ID: {file: {Status: render.StatusOK, MeshPath: mesh}}}
	pc, err := a.partsFor(w.ID, file)
	if err != nil {
		t.Fatal(err)
	}
	if pc.printer.MulticolorSlots != 2 || pc.printer.BuildVolume.X != 256 {
		t.Errorf("impresora de las partes: %+v", pc.printer)
	}
	s := pc.settings
	if s.DefaultMaterial != "petg" || s.InfillPercent != 40 || s.WallLoops != 3 || s.FilamentDiameterMm != 2.85 {
		t.Errorf("ajustes efectivos: %+v", s)
	}
	// La configuración global no se tocó.
	if g, _ := a.store.GetSettings(); g.DefaultMaterial != "pla" || g.InfillPercent != 15 {
		t.Errorf("la configuración global cambió: %+v", g)
	}
}

func TestChatPromptContextCarriesTheEffectivePrinter(t *testing.T) {
	a := newTestApp(t)
	w, err := a.CreateWorkspaceWith(NewWorkspaceInput{
		Name: "Prompt", PrinterID: "bambu-h2d",
		Print: store.WorkspacePrint{Colors: 6, FilamentDiameterMm: ptr(2.85)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := chatPromptContext(w, "")
	if ctx.PrinterName != "Bambu Lab H2D" || ctx.ColorSlots != 6 || ctx.Brand != "Bambu Lab" || ctx.NozzleMM != 0.4 ||
		ctx.SlicerName != "Bambu Studio" || ctx.FilamentDiameterMm != 2.85 || !strings.Contains(ctx.MulticolorSystem, "AMS") {
		t.Errorf("contexto: %+v", ctx)
	}
	if ctx.BuildVolume != [3]float64{325, 320, 325} {
		t.Errorf("volumen: %v", ctx.BuildVolume)
	}
	// Sin ajuste de filamento rige el de la máquina.
	w2, _ := a.CreateWorkspaceWith(NewWorkspaceInput{Name: "Prompt 2", PrinterID: "bambu-a1"})
	if c := chatPromptContext(w2, ""); c.FilamentDiameterMm != 1.75 || c.ColorSlots != 4 {
		t.Errorf("sin ajuste: %+v", c)
	}
}

// emitWorkspaceConfig (lo que dispara una herramienta de la IA con EffectConfig)
// relee el workspace de la base: el cambio que hizo otro proceso se ve.
func TestEmitWorkspaceConfigRereadsTheWorkspace(t *testing.T) {
	a := newTestApp(t)
	ce := captureConfig(a)
	w, _ := a.CreateWorkspace("Caja", "bambu-a1-mini")
	if err := a.store.SetWorkspaceConfig(w.ID, "bambu-x1c", w.Skills, store.WorkspacePrint{Colors: 3}); err != nil {
		t.Fatal(err)
	}
	a.emitWorkspaceConfig(w.ID)
	evs := ce.all()
	if len(evs) != 1 || evs[0].Printer.ID != "bambu-x1c" || evs[0].Colors != 3 {
		t.Errorf("evento: %+v", evs)
	}
	a.emitWorkspaceConfig(9999) // un id que no existe no emite ni falla
	if len(ce.all()) != 1 {
		t.Error("emitió por un workspace inexistente")
	}
}
