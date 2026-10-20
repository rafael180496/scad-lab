package wsconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/skills"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

func printer(id string) printers.Printer {
	p, _ := printers.ByID(id)
	return p
}

func f64(v float64) *float64 { return &v }
func ip(v int) *int          { return &v }

// --- Impresora efectiva ----------------------------------------------------

func TestEffectivePrinterUsesTheWorkspaceColors(t *testing.T) {
	// Sin ajuste: default_colors de la impresora, y el máximo intacto.
	p := EffectivePrinter(store.Workspace{PrinterID: "bambu-h2d"})
	if p.ID != "bambu-h2d" || p.MulticolorSlots != 4 || p.MaxColors != 25 {
		t.Errorf("por defecto: slots %d, máx %d", p.MulticolorSlots, p.MaxColors)
	}
	// Con ajuste: los colores del workspace; recortados al máximo de la máquina.
	for _, c := range []struct{ in, want int }{{2, 2}, {25, 25}, {99, 25}, {-1, 4}} {
		w := store.Workspace{PrinterID: "bambu-h2d", Print: store.WorkspacePrint{Colors: c.in}}
		if got := EffectivePrinter(w).MulticolorSlots; got != c.want {
			t.Errorf("colores %d → %d, esperaba %d", c.in, got, c.want)
		}
	}
	// Una impresora que ya no existe cae en la A1 mini (no rompe).
	if p := EffectivePrinter(store.Workspace{PrinterID: "no-existe"}); p.ID != "bambu-a1-mini" {
		t.Errorf("fallback: %s", p.ID)
	}
}

// --- Ajustes efectivos -----------------------------------------------------

func TestEffectiveSettingsInheritanceOrder(t *testing.T) {
	g := store.DefaultSettings()
	g.DefaultMaterial, g.InfillPercent, g.WallLoops, g.LineWidthMm, g.FilamentDiameterMm = "pla", 15, 2, 0.42, 1.75

	// Un workspace sin ajustes hereda todo de Configuración.
	w := store.Workspace{PrinterID: "bambu-a1-mini"}
	got := EffectiveSettings(g, w, EffectivePrinter(w))
	if got.DefaultMaterial != "pla" || got.InfillPercent != 15 || got.WallLoops != 2 || got.LineWidthMm != 0.42 || got.FilamentDiameterMm != 1.75 {
		t.Errorf("sin ajustes: %+v", got)
	}
	if g.DefaultMaterial != "pla" || g.InfillPercent != 15 {
		t.Error("EffectiveSettings no debe mutar la configuración de entrada")
	}

	// Con ajustes propios, ganan (el relleno 0 % cuenta como fijado).
	w.Print = store.WorkspacePrint{Material: "petg", InfillPercent: f64(0), WallLoops: ip(4), LineWidthMm: f64(0.6)}
	got = EffectiveSettings(g, w, EffectivePrinter(w))
	if got.DefaultMaterial != "petg" || got.InfillPercent != 0 || got.WallLoops != 4 || got.LineWidthMm != 0.6 {
		t.Errorf("con ajustes: %+v", got)
	}

	// Filamento: override del workspace > el de la impresora > el global.
	ult := printers.Printer{ID: "ult", FilamentDiameterMM: 2.85}
	g.FilamentDiameterMm = 1.75
	w = store.Workspace{}
	if got := EffectiveSettings(g, w, ult).FilamentDiameterMm; got != 2.85 {
		t.Errorf("la impresora gana sobre el global: %v", got)
	}
	w.Print.FilamentDiameterMm = f64(3.0)
	if got := EffectiveSettings(g, w, ult).FilamentDiameterMm; got != 3.0 {
		t.Errorf("el workspace gana sobre la impresora: %v", got)
	}
	if got := EffectiveSettings(g, store.Workspace{}, printers.Printer{}).FilamentDiameterMm; got != 1.75 {
		t.Errorf("sin dato de impresora rige el global: %v", got)
	}
	g.FilamentDiameterMm = 1.8
	if got := EffectiveSettings(g, store.Workspace{}, printers.Printer{FilamentDiameterMM: 1.75}).FilamentDiameterMm; got != 1.75 {
		t.Errorf("la impresora gana aunque el global sea otro: %v", got)
	}

	// Valores fuera de rango (una base editada a mano) se ignoran.
	w = store.Workspace{Print: store.WorkspacePrint{InfillPercent: f64(250), WallLoops: ip(0), LineWidthMm: f64(9), FilamentDiameterMm: f64(0.1)}}
	g = store.DefaultSettings()
	got = EffectiveSettings(g, w, printers.Printer{})
	if got.InfillPercent != g.InfillPercent || got.WallLoops != g.WallLoops || got.LineWidthMm != g.LineWidthMm || got.FilamentDiameterMm != g.FilamentDiameterMm {
		t.Errorf("fuera de rango debía heredar: %+v", got)
	}
}

// --- Skills -----------------------------------------------------------------

func fakeSkills(names ...string) []skills.Skill {
	out := make([]skills.Skill, len(names))
	for i, n := range names {
		out[i] = skills.Skill{Name: n, Source: "builtin"}
	}
	return out
}

func TestDefaultSkillsExcludeForeignBrands(t *testing.T) {
	all := fakeSkills("bambu-lab-printers", "mi-skill", "openscad-core", "openscad-libraries")
	on := map[string]bool{"bambu-lab-printers": true, "mi-skill": true, "openscad-core": true, "openscad-libraries": false}

	// Bambu: las encendidas que no son de marca + la de su marca.
	got := DefaultSkills(all, on, printer("bambu-a1"))
	if want := []string{"bambu-lab-printers", "mi-skill", "openscad-core"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Bambu: %v, esperaba %v", got, want)
	}
	// Otra marca (su skill todavía no existe en el binario): la de Bambu, aunque
	// esté encendida, no entra.
	other := printers.Printer{ID: "x", Skill: "prusa-printers"}
	got = DefaultSkills(all, on, other)
	if want := []string{"mi-skill", "openscad-core"}; !reflect.DeepEqual(got, want) {
		t.Errorf("otra marca sin su skill: %v, esperaba %v", got, want)
	}
	// Con la skill de esa marca disponible, entra aunque Configuración la tenga
	// apagada: se elige por la impresora, no por el interruptor global.
	all2 := append(fakeSkills("prusa-printers"), all...)
	got = DefaultSkills(all2, on, other)
	if want := []string{"mi-skill", "openscad-core", "prusa-printers"}; !reflect.DeepEqual(got, want) {
		t.Errorf("otra marca con su skill: %v, esperaba %v", got, want)
	}
	// La skill de marca apagada en Configuración tampoco impide que entre la de su impresora.
	on["bambu-lab-printers"] = false
	if got := DefaultSkills(all, on, printer("bambu-a1")); !reflect.DeepEqual(got, []string{"bambu-lab-printers", "mi-skill", "openscad-core"}) {
		t.Errorf("Bambu con el interruptor apagado: %v", got)
	}
	// Todo apagado y una impresora sin skill: lista vacía, no nil.
	got = DefaultSkills(all, map[string]bool{}, printers.Printer{})
	if got == nil || len(got) != 0 {
		t.Errorf("vacía: %#v", got)
	}
}

func TestEffectiveSkills(t *testing.T) {
	all := fakeSkills("bambu-lab-printers", "mi-skill", "openscad-core")
	on := map[string]bool{"openscad-core": true}
	p := printer("bambu-a1")

	// Sin lista: por defecto.
	if got := EffectiveSkills(store.Workspace{}, all, on, p); !reflect.DeepEqual(got, []string{"bambu-lab-printers", "openscad-core"}) {
		t.Errorf("por defecto: %v", got)
	}
	// Con lista: la suya, sin lo que ya no existe ni repetidos, y sin mirar el global.
	w := store.Workspace{Skills: []string{"mi-skill", "borrada", "mi-skill"}}
	if got := EffectiveSkills(w, all, on, p); !reflect.DeepEqual(got, []string{"mi-skill"}) {
		t.Errorf("propia: %v", got)
	}
	// Una lista vacía es «ninguna», no «por defecto».
	if got := EffectiveSkills(store.Workspace{Skills: []string{}}, all, on, p); len(got) != 0 {
		t.Errorf("vacía: %v", got)
	}
}

// --- ValidatePrint -----------------------------------------------------------

func TestValidatePrintReportsEachOutOfRange(t *testing.T) {
	pr := printer("bambu-a1")
	bad := map[string]store.WorkspacePrint{
		"colores de más":    {Colors: 17},
		"colores negativos": {Colors: -1},
		"relleno alto":      {InfillPercent: f64(101)},
		"relleno negativo":  {InfillPercent: f64(-1)},
		"perímetros":        {WallLoops: ip(0)},
		"perímetros altos":  {WallLoops: ip(11)},
		"ancho de línea":    {LineWidthMm: f64(0.1)},
		"filamento":         {FilamentDiameterMm: f64(5)},
	}
	for name, p := range bad {
		for _, lang := range []string{"es", "en"} {
			i18n.SetLang(lang)
			err := ValidatePrint(pr, p)
			if err == nil {
				t.Errorf("%s: debía fallar", name)
			} else if err.Error() == "" {
				t.Errorf("%s: error sin texto", name)
			}
		}
	}
	i18n.SetLang("es")
	if err := ValidatePrint(pr, store.WorkspacePrint{Colors: 17}); err == nil || !strings.Contains(err.Error(), "entre 1 y 16") {
		t.Errorf("el error de colores debe decir el máximo: %v", err)
	}
	i18n.SetLang("en")
	if err := ValidatePrint(pr, store.WorkspacePrint{Colors: 17}); err == nil || !strings.Contains(err.Error(), "between 1 and 16") {
		t.Errorf("el error de colores en inglés: %v", err)
	}
	i18n.SetLang("es")
	t.Cleanup(func() { i18n.SetLang("en") })

	// Los bordes son válidos, el relleno 0 % también.
	for _, p := range []store.WorkspacePrint{
		{}, {Colors: 4}, {Colors: 1}, {InfillPercent: f64(0)}, {InfillPercent: f64(100)}, {WallLoops: ip(1)}, {WallLoops: ip(10)},
		{LineWidthMm: f64(0.2)}, {LineWidthMm: f64(1.5)}, {FilamentDiameterMm: f64(1.0)}, {FilamentDiameterMm: f64(3.5)},
	} {
		if err := ValidatePrint(pr, p); err != nil {
			t.Errorf("%+v debía ser válido: %v", p, err)
		}
	}
}

// --- Apply -------------------------------------------------------------------

type rig struct {
	st      *store.Store
	userDir string
	w       store.Workspace
}

// newRig arma una base temporal con el registro de skills poblado y un workspace
// real en disco con su lista de skills por defecto ya instalada.
func newRig(t *testing.T, printerID string) rig {
	t.Helper()
	i18n.SetLang("es")
	t.Cleanup(func() { i18n.SetLang("en") })
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	userDir := filepath.Join(dir, "skills")
	if _, err := skills.CreateFromTemplate(userDir, "mi-skill"); err != nil {
		t.Fatal(err)
	}
	all, err := skills.Discover(userDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if err := st.UpsertSkill(store.SkillRow{Name: s.Name, Source: s.Source, Description: s.Description}); err != nil {
			t.Fatal(err)
		}
	}
	wdir, err := workspace.Create(filepath.Join(dir, "ws"), "Caja", printer(printerID))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.InsertWorkspace(store.Workspace{Name: "Caja", Path: wdir, PrinterID: printerID})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := st.GetWorkspace(id)
	// Instala lo que le corresponde por defecto.
	if _, err := Install(st, userDir, w); err != nil {
		t.Fatal(err)
	}
	return rig{st: st, userDir: userDir, w: w}
}

func installed(t *testing.T, ws string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, rel := range skills.AgentDirs {
		ents, err := os.ReadDir(filepath.Join(ws, rel))
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				out[e.Name()] = true
			}
		}
	}
	return out
}

func installedIn(ws, rel, name string) bool {
	_, err := os.Stat(filepath.Join(ws, rel, name, "SKILL.md"))
	return err == nil
}

func TestApplySyncsOnlyTheWorkspaceSkills(t *testing.T) {
	r := newRig(t, "bambu-a1-mini")
	ws := r.w.Path
	// Punto de partida: todo lo encendido por defecto (incluida la marca).
	for _, n := range []string{"openscad-core", "bambu-lab-printers", "mi-skill", "openscad-libraries"} {
		if !installedIn(ws, ".claude/skills", n) {
			t.Fatalf("antes de aplicar tenía que estar %s: %v", n, installed(t, ws))
		}
	}
	// Una skill que el usuario dejó a mano no es de ScadLab.
	manual := filepath.Join(ws, ".claude", "skills", "mine")
	if err := os.MkdirAll(manual, 0o755); err != nil {
		t.Fatal(err)
	}
	// libraries: editada a mano en una carpeta de agente → se queda y se avisa.
	if err := os.WriteFile(filepath.Join(ws, ".agents", "skills", "openscad-libraries", "SKILL.md"), []byte("editada\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	saved, res, err := Apply(r.st, r.userDir, r.w, "bambu-a1-mini", []string{"openscad-core", "mi-skill"}, store.WorkspacePrint{})
	if err != nil {
		t.Fatal(err)
	}
	if res.SyncError != "" {
		t.Fatalf("SyncError: %s", res.SyncError)
	}
	if !reflect.DeepEqual(saved.Skills, []string{"mi-skill", "openscad-core"}) {
		t.Errorf("lista guardada: %v", saved.Skills)
	}
	got := installed(t, ws)
	if !got["openscad-core"] || !got["mi-skill"] || got["bambu-lab-printers"] || got["openscad-3mf-multicolor"] || got["openscad-templates"] {
		t.Errorf("instaladas: %v", got)
	}
	for _, rel := range skills.AgentDirs {
		if !installedIn(ws, rel, "openscad-core") {
			t.Errorf("%s: falta openscad-core", rel)
		}
		if installedIn(ws, rel, "bambu-lab-printers") {
			t.Errorf("%s: la skill que salió de la lista sigue instalada", rel)
		}
	}
	if !reflect.DeepEqual(res.Kept, []string{"openscad-libraries"}) {
		t.Errorf("Kept %v", res.Kept)
	}
	if want := []string{"bambu-lab-printers", "openscad-3mf-multicolor", "openscad-from-image", "openscad-libraries", "openscad-stl-editing", "openscad-templates"}; !reflect.DeepEqual(res.Removed, want) {
		t.Errorf("Removed %v, esperaba %v", res.Removed, want)
	}
	// La copia editada no se borró ni se tocó; la ajena tampoco.
	if raw, _ := os.ReadFile(filepath.Join(ws, ".agents", "skills", "openscad-libraries", "SKILL.md")); string(raw) != "editada\n" {
		t.Errorf("la copia editada cambió: %q", raw)
	}
	if _, err := os.Stat(manual); err != nil {
		t.Error("Apply borró una skill que no gestiona")
	}
	// Volver a aplicar lo mismo es estable.
	_, res2, err := Apply(r.st, r.userDir, saved, "bambu-a1-mini", []string{"openscad-core", "mi-skill"}, store.WorkspacePrint{})
	if err != nil || len(res2.Removed) != 0 || !reflect.DeepEqual(res2.Kept, []string{"openscad-libraries"}) {
		t.Errorf("segunda vez: %+v, %v", res2, err)
	}
	// Volver a prender una skill la reinstala.
	if _, _, err := Apply(r.st, r.userDir, saved, "bambu-a1-mini", []string{"openscad-core", "mi-skill", "bambu-lab-printers"}, store.WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	if !installedIn(ws, ".opencode/skills", "bambu-lab-printers") {
		t.Error("la skill vuelta a la lista no se reinstaló")
	}
}

func TestApplyRewritesRulesWithTheEffectivePrinter(t *testing.T) {
	r := newRig(t, "bambu-a1-mini")
	p := store.WorkspacePrint{Colors: 2}
	saved, res, err := Apply(r.st, r.userDir, r.w, "bambu-h2d", nil, p)
	if err != nil || res.SyncError != "" {
		t.Fatalf("%v / %s", err, res.SyncError)
	}
	if saved.PrinterID != "bambu-h2d" || saved.Print.Colors != 2 {
		t.Errorf("guardado: %+v", saved)
	}
	var pj struct {
		Printer   string             `json:"printer"`
		Colors    int                `json:"colors"`
		MaxColors int                `json:"max_colors"`
		Volume    map[string]float64 `json:"build_volume"`
	}
	raw, _ := os.ReadFile(filepath.Join(saved.Path, "printer.json"))
	if err := json.Unmarshal(raw, &pj); err != nil {
		t.Fatal(err)
	}
	if pj.Printer != "bambu-h2d" || pj.Colors != 2 || pj.MaxColors != 25 || pj.Volume["x"] != 325 {
		t.Errorf("printer.json: %s", raw)
	}
	for _, f := range []string{"CLAUDE.md", "AGENTS.md"} {
		b, _ := os.ReadFile(filepath.Join(saved.Path, f))
		if !strings.Contains(string(b), "Bambu Lab H2D") || !strings.Contains(string(b), "325") {
			t.Errorf("%s no nombra la impresora nueva", f)
		}
	}
	// nil pidió las de por defecto: explícitas en el workspace.
	if saved.Skills == nil || len(saved.Skills) == 0 {
		t.Errorf("las skills por defecto se guardan explícitas: %#v", saved.Skills)
	}
}

func TestApplyValidatesBeforeTouchingAnything(t *testing.T) {
	r := newRig(t, "bambu-a1")
	before := installed(t, r.w.Path)
	cases := map[string]struct {
		printer string
		skills  []string
		print   store.WorkspacePrint
	}{
		"impresora inexistente": {"no-existe", nil, store.WorkspacePrint{}},
		"skill inexistente":     {"bambu-a1", []string{"openscad-core", "fantasma"}, store.WorkspacePrint{}},
		"colores de más":        {"bambu-a1", nil, store.WorkspacePrint{Colors: 17}},
		"relleno fuera":         {"bambu-a1", nil, store.WorkspacePrint{InfillPercent: f64(150)}},
		"perímetros fuera":      {"bambu-a1", nil, store.WorkspacePrint{WallLoops: ip(99)}},
		"material inexistente":  {"bambu-a1", nil, store.WorkspacePrint{Material: "unobtanium"}},
	}
	for name, c := range cases {
		if _, _, err := Apply(r.st, r.userDir, r.w, c.printer, c.skills, c.print); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	w, _ := r.st.GetWorkspace(r.w.ID)
	if !reflect.DeepEqual(w, r.w) {
		t.Errorf("un Apply inválido cambió el workspace:\n antes %+v\n ahora %+v", r.w, w)
	}
	if after := installed(t, r.w.Path); !reflect.DeepEqual(before, after) {
		t.Errorf("un Apply inválido cambió las skills instaladas: %v → %v", before, after)
	}
	// Un material que existe pasa.
	if _, _, err := Apply(r.st, r.userDir, r.w, "bambu-a1", nil, store.WorkspacePrint{Material: "petg", Colors: 4}); err != nil {
		t.Errorf("material válido: %v", err)
	}
}

// Si la carpeta del workspace desapareció, la configuración igual se guarda y el
// problema vuelve en SyncError (no se crea una carpeta de la nada).
func TestApplyWithAMissingFolderStillSaves(t *testing.T) {
	r := newRig(t, "bambu-a1")
	if err := os.RemoveAll(r.w.Path); err != nil {
		t.Fatal(err)
	}
	saved, res, err := Apply(r.st, r.userDir, r.w, "bambu-p1s", nil, store.WorkspacePrint{})
	if err != nil {
		t.Fatal(err)
	}
	if saved.PrinterID != "bambu-p1s" || res.SyncError == "" {
		t.Errorf("guardado %q, SyncError %q", saved.PrinterID, res.SyncError)
	}
	if _, err := os.Stat(r.w.Path); err == nil {
		t.Error("Apply recreó la carpeta del workspace")
	}
}

// Install con nombres solo mira esas skills.
func TestInstallWithNamesOnlyTouchesThose(t *testing.T) {
	r := newRig(t, "bambu-a1")
	// El usuario quita una skill de la lista a mano en la base, sin sincronizar.
	if err := r.st.SetWorkspaceConfig(r.w.ID, "bambu-a1", []string{"openscad-core"}, store.WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	w, _ := r.st.GetWorkspace(r.w.ID)
	res, err := Install(r.st, r.userDir, w, "openscad-templates")
	if err != nil || !reflect.DeepEqual(res.Removed, []string{"openscad-templates"}) {
		t.Fatalf("%+v, %v", res, err)
	}
	if !installedIn(w.Path, ".claude/skills", "mi-skill") {
		t.Error("Install acotado no debía tocar mi-skill")
	}
	if installedIn(w.Path, ".claude/skills", "openscad-templates") {
		t.Error("openscad-templates tenía que retirarse")
	}
}
