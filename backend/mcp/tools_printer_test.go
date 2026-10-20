package mcp

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scadlab/backend/appdata"
	"scadlab/backend/printers"
	"scadlab/backend/store"
	"scadlab/backend/wsconfig"
)

// --- printer_info -----------------------------------------------------------

func toolSet(t *testing.T, tb *Toolbox, id string, extra map[string]any) map[string]any {
	t.Helper()
	args := map[string]any{"printer_id": id}
	for k, v := range extra {
		args[k] = v
	}
	return tb.mustJSON(t, "set_workspace_printer", args)
}

func str(m map[string]any, key string) string { s, _ := m[key].(string); return s }

// printer_info trae marca, modelo, sistema multicolor, colores del workspace y
// máximo, filamento, slicer, skill de la marca y las fuentes (S14).
func TestPrinterInfoDescribesTheWholePrinter(t *testing.T) {
	tb, _, st, w := registeredToolbox(t)
	if err := st.SetWorkspaceConfig(w.ID, "prusa-mk4s", nil, store.WorkspacePrint{Colors: 3}); err != nil {
		t.Fatal(err)
	}
	info := tb.mustJSON(t, "printer_info", nil)
	pr, _ := info["printer"].(map[string]any)
	for k, want := range map[string]any{
		"id": "prusa-mk4s", "brand": "Prusa Research", "model": "MK4S", "brandSkill": "prusa-printers",
		"multicolorKind": "mmu", "multicolorSystem": "Prusa MMU3 (opcional)", "colors": 3.0, "maxColors": 5.0, "multicolorSlots": 3.0,
		"nozzleMm": 0.4, "nozzleCount": 1.0, "filamentDiameterMm": 1.75, "maxNozzleTempC": 290.0, "maxBedTempC": 120.0,
		"enclosed": false, "heatedChamber": false,
	} {
		if pr[k] != want {
			t.Errorf("printer[%s] = %v, esperaba %v", k, pr[k], want)
		}
	}
	if !strings.Contains(str(pr, "name"), "Prusa MK4S") || str(pr, "bedSurface") == "" || str(pr, "notes") == "" {
		t.Errorf("nombre/superficie/notas: %v", pr)
	}
	if vol, _ := pr["buildVolumeMm"].(map[string]any); vol["x"] != 250.0 || vol["y"] != 210.0 || vol["z"] != 220.0 {
		t.Errorf("volumen: %v", pr["buildVolumeMm"])
	}
	sl, _ := pr["slicer"].(map[string]any)
	if sl["id"] != "prusaslicer" || sl["name"] != "PrusaSlicer" {
		t.Errorf("slicer recomendado: %v", sl)
	}
	var ids []string
	for _, s := range pr["slicers"].([]any) {
		ids = append(ids, s.(map[string]any)["id"].(string))
	}
	if !slices.Equal(ids, []string{"prusaslicer", "orcaslicer"}) {
		t.Errorf("slicers: %v", ids)
	}
	if src, _ := pr["sources"].([]any); len(src) == 0 || !strings.HasPrefix(src[0].(string), "https://") {
		t.Errorf("sources: %v", pr["sources"])
	}
	if v, ok := pr["unconfirmedFields"].([]any); !ok || len(v) != 0 {
		t.Errorf("unconfirmedFields: %#v", pr["unconfirmedFields"])
	}
	// Las reglas ya no hablan del AMS y mandan a la skill de la marca.
	rules := strings.Join(func() []string {
		var out []string
		for _, r := range info["fdmRules"].([]any) {
			out = append(out, r.(string))
		}
		return out
	}(), "\n")
	if strings.Contains(rules, "AMS") || strings.Contains(rules, "Bambu") || !strings.Contains(rules, "`colors`") || !strings.Contains(rules, "brandSkill") {
		t.Errorf("fdmRules:\n%s", rules)
	}
}

// El filamento sale de la impresora (UltiMaker: 2,85) y, si el workspace fijó uno,
// del workspace. Un workspace que la app no conoce lo toma de printer.json.
func TestPrinterInfoFilamentDiameter(t *testing.T) {
	tb, _, st, w := registeredToolbox(t)
	if err := st.SetWorkspaceConfig(w.ID, "ultimaker-s5", nil, store.WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	pr := tb.mustJSON(t, "printer_info", nil)["printer"].(map[string]any)
	if pr["filamentDiameterMm"] != 2.85 || pr["colors"] != 2.0 || pr["maxColors"] != 2.0 || str(pr, "brand") != "UltiMaker" {
		t.Errorf("UltiMaker S5: %v", pr)
	}
	if sl, _ := pr["slicer"].(map[string]any); sl["id"] != "ultimaker-cura" {
		t.Errorf("slicer: %v", pr["slicer"])
	}
	d := 2.2
	if err := st.SetWorkspaceConfig(w.ID, "ultimaker-s5", nil, store.WorkspacePrint{FilamentDiameterMm: &d}); err != nil {
		t.Fatal(err)
	}
	if got := tb.mustJSON(t, "printer_info", nil)["printer"].(map[string]any)["filamentDiameterMm"]; got != 2.2 {
		t.Errorf("el filamento del workspace gana: %v", got)
	}

	// Sin registrar: printer.json con la UltiMaker.
	c := setup(t, nil)
	if err := os.WriteFile(filepath.Join(c.Ws, "printer.json"), []byte(`{"printer":"ultimaker-s7","colors":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := c.mustOK("printer_info", nil)
	p2, _ := out["printer"].(map[string]any)
	if p2["id"] != "ultimaker-s7" || p2["filamentDiameterMm"] != 2.85 || p2["colors"] != 2.0 {
		t.Errorf("printer.json de una UltiMaker: %v", p2)
	}
}

// --- list_printers ----------------------------------------------------------

func listPrinters(t *testing.T, tb *Toolbox, args map[string]any) (int, []map[string]any) {
	t.Helper()
	out := tb.mustJSON(t, "list_printers", args)
	var rows []map[string]any
	for _, r := range out["printers"].([]any) {
		rows = append(rows, r.(map[string]any))
	}
	return int(out["count"].(float64)), rows
}

func TestListPrinters(t *testing.T) {
	tb, _ := newToolbox(t, false)
	all, _ := printers.All()
	n, rows := listPrinters(t, tb, nil)
	if n != len(all) || len(rows) != len(all) || n < 47 {
		t.Fatalf("sin filtros hay %d de %d", n, len(all))
	}
	// Cada fila trae lo que el modelo necesita para elegir.
	for _, r := range rows {
		for _, k := range []string{"id", "name", "brand", "brandId", "model", "buildVolumeMm", "maxColors", "multicolorSystem", "brandSkill"} {
			if r[k] == nil || r[k] == "" {
				t.Errorf("%v: falta %s", r["id"], k)
			}
		}
		if sl, _ := r["slicer"].(map[string]any); sl["id"] == nil {
			t.Errorf("%v: sin slicer recomendado", r["id"])
		}
	}
	// Por marca: el id, el nombre o la skill sirven, sin importar mayúsculas.
	for _, brand := range []string{"prusa", "Prusa Research", "PRUSA", "prusa-printers", "Prusa"} {
		n, rows := listPrinters(t, tb, map[string]any{"brand": brand})
		if n != 5 {
			t.Errorf("brand=%q: %d impresoras, esperaba 5", brand, n)
		}
		for _, r := range rows {
			if r["brandId"] != "prusa" {
				t.Errorf("brand=%q trajo %v", brand, r["id"])
			}
		}
	}
	if n, _ := listPrinters(t, tb, map[string]any{"brand": "bambu lab"}); n != 12 {
		t.Errorf("bambu lab: %d", n)
	}
	// Por texto: id, nombre, modelo o marca.
	n, rows = listPrinters(t, tb, map[string]any{"query": "MK4"})
	if n != 1 || rows[0]["id"] != "prusa-mk4s" {
		t.Errorf("query mk4: %v", rows)
	}
	if n, _ := listPrinters(t, tb, map[string]any{"query": "kobra 2"}); n != 3 {
		t.Errorf("query «kobra 2»: %d, esperaba las tres Kobra 2", n)
	}
	if n, rows := listPrinters(t, tb, map[string]any{"brand": "snapmaker", "query": "u1"}); n != 1 || rows[0]["id"] != "snapmaker-u1" {
		t.Errorf("marca + texto: %v", rows)
	}
	// Sin coincidencias: lista vacía con una nota, no un error.
	out := tb.mustJSON(t, "list_printers", map[string]any{"query": "no-existe-xyz"})
	if out["count"] != 0.0 || str(out, "note") == "" {
		t.Errorf("sin coincidencias: %v", out)
	}
	// Una marca que no existe sí es un error, y dice cuáles hay.
	bad := tb.fails(t, "list_printers", map[string]any{"brand": "acme"})
	if !strings.Contains(bad.Text, "acme") || !strings.Contains(bad.Text, "ultimaker") {
		t.Errorf("marca desconocida: %q", bad.Text)
	}
}

// La impresora del workspace sale marcada como `current`.
func TestListPrintersMarksTheCurrentOne(t *testing.T) {
	tb, _, st, w := registeredToolbox(t)
	if err := st.SetWorkspaceConfig(w.ID, "creality-k1c", nil, store.WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	out := tb.mustJSON(t, "list_printers", nil)
	if out["currentPrinter"] != "creality-k1c" {
		t.Errorf("currentPrinter: %v", out["currentPrinter"])
	}
	cur := 0
	for _, r := range out["printers"].([]any) {
		if row := r.(map[string]any); row["current"] == true {
			cur++
			if row["id"] != "creality-k1c" {
				t.Errorf("marcó %v", row["id"])
			}
		}
	}
	if cur != 1 {
		t.Errorf("%d marcadas como actuales", cur)
	}
}

// --- set_workspace_printer --------------------------------------------------

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// Cambiar de marca reescribe las reglas y printer.json, guarda la impresora y
// cambia la skill de la marca (la vieja sale, la nueva entra).
func TestSetWorkspacePrinterSwapsBrandAndRewritesRules(t *testing.T) {
	tb, ws, st, w := registeredToolbox(t)
	userDir := filepath.Join(os.Getenv(appdata.OverrideEnv), "skills")
	// Parte de un workspace Bambu con su skill instalada.
	if _, res, err := wsconfig.Apply(st, userDir, w, "bambu-a1-mini", nil, store.WorkspacePrint{}); err != nil || res.SyncError != "" {
		t.Fatalf("preparar: %v / %s", err, res.SyncError)
	}
	bambu := filepath.Join(ws, ".claude", "skills", "bambu-lab-printers", "SKILL.md")
	if !fileExists(bambu) {
		t.Fatal("la skill de Bambu tendría que estar instalada")
	}

	out := toolSet(t, tb, "prusa-mk4s", nil)
	pr, _ := out["printer"].(map[string]any)
	if pr["id"] != "prusa-mk4s" || pr["colors"] != 1.0 || pr["maxColors"] != 5.0 || str(pr, "brandSkill") != "prusa-printers" {
		t.Errorf("impresora nueva: %v", pr)
	}
	if prev, _ := out["previousPrinter"].(map[string]any); prev["id"] != "bambu-a1-mini" {
		t.Errorf("anterior: %v", out["previousPrinter"])
	}
	if sl, _ := pr["slicer"].(map[string]any); sl["name"] != "PrusaSlicer" {
		t.Errorf("slicer: %v", pr["slicer"])
	}
	if _, bad := out["syncError"]; bad {
		t.Errorf("syncError: %v", out["syncError"])
	}

	saved, _ := st.GetWorkspace(w.ID)
	if saved.PrinterID != "prusa-mk4s" {
		t.Errorf("la base sigue con %s", saved.PrinterID)
	}
	if !slices.Contains(saved.Skills, "prusa-printers") || slices.Contains(saved.Skills, "bambu-lab-printers") {
		t.Errorf("skills del workspace: %v", saved.Skills)
	}
	if !fileExists(filepath.Join(ws, ".claude", "skills", "prusa-printers", "SKILL.md")) {
		t.Error("la skill de Prusa no se instaló")
	}
	if fileExists(bambu) {
		t.Error("la skill de Bambu tenía que retirarse")
	}
	if rem, _ := out["removedSkills"].([]any); len(rem) != 1 || rem[0] != "bambu-lab-printers" {
		t.Errorf("removedSkills: %v", out["removedSkills"])
	}

	rules, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	for _, want := range []string{"Original Prusa MK4S", "250 × 210 × 220 mm", "`prusa-printers`", "PrusaSlicer"} {
		if !strings.Contains(string(rules), want) {
			t.Errorf("CLAUDE.md no dice %q", want)
		}
	}
	var pj struct {
		Printer string `json:"printer"`
		Colors  int    `json:"colors"`
		Slicer  string `json:"slicer"`
	}
	raw, _ := os.ReadFile(filepath.Join(ws, "printer.json"))
	if err := json.Unmarshal(raw, &pj); err != nil || pj.Printer != "prusa-mk4s" || pj.Colors != 1 || pj.Slicer != "PrusaSlicer" {
		t.Errorf("printer.json: %s (%v)", raw, err)
	}
	// Y la herramienta siguiente ya ve la impresora nueva.
	if got := tb.mustJSON(t, "printer_info", nil)["printer"].(map[string]any); got["id"] != "prusa-mk4s" {
		t.Errorf("printer_info tras el cambio: %v", got["id"])
	}
}

func TestSetWorkspacePrinterColorsAndSwapOff(t *testing.T) {
	tb, ws, st, w := registeredToolbox(t)
	userDir := filepath.Join(os.Getenv(appdata.OverrideEnv), "skills")
	if _, _, err := wsconfig.Apply(st, userDir, w, "bambu-a1-mini", nil, store.WorkspacePrint{Colors: 2}); err != nil {
		t.Fatal(err)
	}
	// Misma impresora, solo colores: se respeta lo pedido y las skills no cambian.
	out := toolSet(t, tb, "bambu-a1-mini", map[string]any{"colors": 3})
	if pr := out["printer"].(map[string]any); pr["colors"] != 3.0 || pr["maxColors"] != 16.0 {
		t.Errorf("colores: %v", pr)
	}
	// Misma impresora y sin `colors`: se conserva lo que había.
	out = toolSet(t, tb, "bambu-a1-mini", nil)
	if pr := out["printer"].(map[string]any); pr["colors"] != 3.0 {
		t.Errorf("sin colors en la misma impresora, siguen los de antes: %v", pr["colors"])
	}
	// Otra impresora y sin `colors`: nace con los suyos (la MK4S, uno).
	// swap_brand_skill=false deja las skills como están.
	out = toolSet(t, tb, "prusa-mk4s", map[string]any{"swap_brand_skill": false})
	if pr := out["printer"].(map[string]any); pr["colors"] != 1.0 {
		t.Errorf("la impresora nueva arranca con sus colores: %v", pr["colors"])
	}
	saved, _ := st.GetWorkspace(w.ID)
	if !slices.Contains(saved.Skills, "bambu-lab-printers") || slices.Contains(saved.Skills, "prusa-printers") {
		t.Errorf("sin swap las skills no cambian: %v", saved.Skills)
	}
	if !fileExists(filepath.Join(ws, ".claude", "skills", "bambu-lab-printers", "SKILL.md")) {
		t.Error("la skill de Bambu tiene que seguir instalada")
	}
	// `swap_brand_skill` también acepta el texto «false» (los modelos chicos lo mandan así).
	toolSet(t, tb, "bambu-a1", map[string]any{"swap_brand_skill": "false"})
	if saved, _ = st.GetWorkspace(w.ID); slices.Contains(saved.Skills, "bambu-lab-printers") != true {
		t.Errorf("skills: %v", saved.Skills)
	}
}

func TestSetWorkspacePrinterRejectsBadInput(t *testing.T) {
	tb, ws, st, w := registeredToolbox(t)
	userDir := filepath.Join(os.Getenv(appdata.OverrideEnv), "skills")
	if _, _, err := wsconfig.Apply(st, userDir, w, "bambu-a1-mini", nil, store.WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(ws, "printer.json"))
	for name, args := range map[string]map[string]any{
		"sin id":                {},
		"id vacío":              {"printer_id": "  "},
		"impresora inexistente": {"printer_id": "prusa-mk99"},
		"colores de más":        {"printer_id": "prusa-mk4s", "colors": 9},
		"colores cero":          {"printer_id": "prusa-mk4s", "colors": 0},
		"colores negativos":     {"printer_id": "prusa-mk4s", "colors": -2},
	} {
		out := tb.fails(t, "set_workspace_printer", args)
		if out.Text == "" {
			t.Errorf("%s: sin texto de error", name)
		}
	}
	if out := tb.fails(t, "set_workspace_printer", map[string]any{"printer_id": "prusa-mk4s", "colors": 9}); !strings.Contains(out.Text, "between 1 and 5") {
		t.Errorf("el error de colores debe decir el máximo: %q", out.Text)
	}
	if out := tb.fails(t, "set_workspace_printer", map[string]any{"printer_id": "prusa-mk99"}); !strings.Contains(out.Text, "list_printers") {
		t.Errorf("el error de impresora debe mandar a list_printers: %q", out.Text)
	}
	// Nada cambió.
	saved, _ := st.GetWorkspace(w.ID)
	after, _ := os.ReadFile(filepath.Join(ws, "printer.json"))
	if saved.PrinterID != "bambu-a1-mini" || string(before) != string(after) {
		t.Errorf("una llamada inválida cambió el workspace: %s / %s", saved.PrinterID, after)
	}
}

// Una copia de la skill vieja que el usuario editó no se borra: va en keptSkills.
func TestSetWorkspacePrinterKeepsAnEditedSkill(t *testing.T) {
	tb, ws, st, w := registeredToolbox(t)
	userDir := filepath.Join(os.Getenv(appdata.OverrideEnv), "skills")
	if _, _, err := wsconfig.Apply(st, userDir, w, "bambu-a1-mini", nil, store.WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	bambu := filepath.Join(ws, ".claude", "skills", "bambu-lab-printers", "SKILL.md")
	if err := os.WriteFile(bambu, []byte("---\nname: bambu-lab-printers\ndescription: editada a mano\n---\nmis notas\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := toolSet(t, tb, "creality-k1c", nil)
	if kept, _ := out["keptSkills"].([]any); len(kept) != 1 || kept[0] != "bambu-lab-printers" {
		t.Errorf("keptSkills: %v", out["keptSkills"])
	}
	if b, err := os.ReadFile(bambu); err != nil || !strings.Contains(string(b), "mis notas") {
		t.Errorf("la copia editada tenía que quedar: %v", err)
	}
	if !fileExists(filepath.Join(ws, ".claude", "skills", "creality-printers", "SKILL.md")) {
		t.Error("la skill de Creality tenía que instalarse")
	}
}

// Un workspace que la app no conoce no se puede reconfigurar desde aquí, y el
// error lo dice con claridad.
func TestSetWorkspacePrinterNeedsARegisteredWorkspace(t *testing.T) {
	tb, ws := newToolbox(t, false)
	before, _ := os.ReadFile(filepath.Join(ws, "printer.json"))
	out := tb.fails(t, "set_workspace_printer", map[string]any{"printer_id": "prusa-mk4s"})
	if !strings.Contains(out.Text, "not registered") {
		t.Errorf("mensaje: %q", out.Text)
	}
	after, _ := os.ReadFile(filepath.Join(ws, "printer.json"))
	if string(before) != string(after) {
		t.Error("tocó printer.json de un workspace no registrado")
	}
}

// Por el servidor `--mcp` (JSON-RPC): la herramienta está en el catálogo con su
// schema, cambia la base de la app y las reglas.
func TestSetWorkspacePrinterOverMCP(t *testing.T) {
	var wsID int64
	c := setup(t, func(t *testing.T, st *store.Store, ws string) {
		id, err := st.InsertWorkspace(store.Workspace{Name: "Registrado", Path: ws, PrinterID: "bambu-a1-mini"})
		if err != nil {
			t.Fatal(err)
		}
		wsID = id
	})
	m := c.call("tools/list", nil)
	list, _ := m["result"].(map[string]any)["tools"].([]any)
	found := map[string]map[string]any{}
	for _, it := range list {
		tool := it.(map[string]any)
		found[tool["name"].(string)] = tool
	}
	for _, name := range []string{"list_printers", "set_workspace_printer"} {
		tool, ok := found[name]
		if !ok {
			t.Fatalf("--mcp no lista %s", name)
		}
		if schema, _ := tool["inputSchema"].(map[string]any); schema["type"] != "object" || schema["properties"] == nil {
			t.Errorf("%s: inputSchema inválido: %v", name, schema)
		}
	}
	if req, _ := found["set_workspace_printer"]["inputSchema"].(map[string]any)["required"].([]any); len(req) != 1 || req[0] != "printer_id" {
		t.Errorf("required de set_workspace_printer: %v", req)
	}
	if ann, _ := found["list_printers"]["annotations"].(map[string]any); ann["readOnlyHint"] != true {
		t.Errorf("list_printers es de lectura: %v", ann)
	}
	if ann, _ := found["set_workspace_printer"]["annotations"].(map[string]any); ann["readOnlyHint"] != false {
		t.Errorf("set_workspace_printer es de escritura: %v", ann)
	}

	out := c.mustOK("set_workspace_printer", map[string]any{"printer_id": "elegoo-centauri-carbon", "colors": 4})
	pr, _ := out["printer"].(map[string]any)
	if pr["id"] != "elegoo-centauri-carbon" || pr["colors"] != 4.0 {
		t.Errorf("resultado: %v", out)
	}
	// El proceso `--mcp` escribió en la misma base: se relee desde fuera.
	dbPath, _ := appdata.DBPath()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	w, err := st.GetWorkspace(wsID)
	if err != nil || w.PrinterID != "elegoo-centauri-carbon" || w.Print.Colors != 4 {
		t.Errorf("workspace guardado: %+v (%v)", w, err)
	}
	if rules, _ := os.ReadFile(filepath.Join(c.Ws, "AGENTS.md")); !strings.Contains(string(rules), "Elegoo Centauri Carbon") {
		t.Error("AGENTS.md no nombra la impresora nueva")
	}
}

// export acepta 3mf-prusa y escribe el 3MF de PrusaSlicer en exports/, con el
// motor integrado (sin OpenSCAD instalado).
func TestExportPrusa3MFOverMCP(t *testing.T) {
	c := setupNoNative(t)
	name := c.create("dos colores", twoColors, nil)
	out := c.mustOK("export", map[string]any{"file": name, "format": "3mf-prusa"})
	if out["format"] != "3mf-prusa" || !strings.HasSuffix(out["path"].(string), "-prusa.3mf") {
		t.Fatalf("export: %v", out)
	}
	if ps, _ := out["parts"].([]any); len(ps) != 2 {
		t.Errorf("partes: %v", out["parts"])
	}
	zr, err := zipOpen(out["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !zr["Metadata/Slic3r_PE_model.config"] || zr["Metadata/model_settings.config"] {
		t.Errorf("entradas del 3MF de PrusaSlicer: %v", zr)
	}
	// Con el 3MF normal en el mismo checkpoint no se pisan: nombres distintos.
	plain := c.mustOK("export", map[string]any{"file": name, "format": "3mf"})
	if plain["path"] == out["path"] || !c.exists(filepath.Join("exports", filepath.Base(out["path"].(string)))) {
		t.Errorf("el 3MF normal y el de PrusaSlicer comparten archivo: %v / %v", plain["path"], out["path"])
	}
	// Un formato inventado sigue siendo un error que nombra los válidos.
	bad := c.tool("export", map[string]any{"file": name, "format": "obj"})
	if !bad.IsError || !strings.Contains(bad.Text, "3mf-prusa") {
		t.Errorf("formato inválido: %v %q", bad.IsError, bad.Text)
	}
}

// zipOpen lista las entradas de un zip como un conjunto de nombres.
func zipOpen(path string) (map[string]bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	has := map[string]bool{}
	for _, f := range zr.File {
		has[f.Name] = true
	}
	return has, nil
}
