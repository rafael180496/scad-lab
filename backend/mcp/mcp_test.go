package mcp

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"scadlab/backend/appdata"
	"scadlab/backend/checkpoint"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

var wantTools = []string{
	"list_checkpoints", "read_checkpoint", "create_checkpoint", "import_mesh", "render", "lint", "format",
	"measure", "preview", "printer_info", "export", "list_templates", "get_template",
	"set_active_parent", "rename_checkpoint", "trash_checkpoint", "list_trash", "restore_checkpoint", "list_refs",
	"get_parameters", "set_parameters", "create_from_template",
	"list_libraries", "install_library",
	"edit_mesh",
	"estimate_print", "list_materials", "suggest_orientation",
	"list_skills", "read_skill", "inspect_reference",
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	c := setup(t, nil)
	cases := map[string]string{
		"2025-06-18": "2025-06-18",
		"2024-11-05": "2024-11-05",
		"2025-11-25": "2025-06-18", // un cliente más nuevo: la más alta que hablamos
		"2025-03-26": "2024-11-05",
		"":           "2024-11-05",
		"basura":     "2024-11-05",
	}
	for asked, want := range cases {
		m := c.call("initialize", map[string]any{"protocolVersion": asked, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
		res, _ := m["result"].(map[string]any)
		if res["protocolVersion"] != want {
			t.Errorf("pidió %q: %v, quería %q", asked, res["protocolVersion"], want)
		}
		info, _ := res["serverInfo"].(map[string]any)
		if info["name"] != ServerName || info["version"] != "test" {
			t.Errorf("serverInfo: %v", info)
		}
		caps, _ := res["capabilities"].(map[string]any)
		if caps["tools"] == nil || caps["resources"] != nil || caps["prompts"] != nil {
			t.Errorf("capabilities: %v", caps)
		}
	}
	// Una notificación no lleva respuesta: la siguiente respuesta es la del ping.
	c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if m := c.call("ping", nil); m["error"] != nil {
		t.Errorf("ping: %v", m)
	}
}

func TestServerNameAndArgs(t *testing.T) {
	if ServerName != "scadlab" {
		t.Errorf("ServerName = %q", ServerName)
	}
	got := Args("/tmp/ws")
	if len(got) != 3 || got[0] != "--mcp" || got[1] != "--workspace" || got[2] != "/tmp/ws" {
		t.Errorf("Args = %v", got)
	}
}

func TestToolsListHasEverythingWithSchema(t *testing.T) {
	c := setup(t, nil)
	m := c.call("tools/list", nil)
	res, _ := m["result"].(map[string]any)
	list, _ := res["tools"].([]any)
	got := map[string]map[string]any{}
	for _, it := range list {
		tool := it.(map[string]any)
		got[tool["name"].(string)] = tool
	}
	for _, name := range wantTools {
		tool, ok := got[name]
		if !ok {
			t.Errorf("falta la herramienta %s", name)
			continue
		}
		if d, _ := tool["description"].(string); len(d) < 20 {
			t.Errorf("%s: descripción corta o ausente", name)
		}
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema["type"] != "object" || schema["properties"] == nil {
			t.Errorf("%s: inputSchema inválido: %v", name, schema)
		}
		if _, ok := tool["annotations"].(map[string]any); !ok {
			t.Errorf("%s: sin annotations", name)
		}
	}
	if len(got) != len(wantTools) {
		t.Errorf("hay %d herramientas, esperaba %d", len(got), len(wantTools))
	}
	// Las que exigen argumentos los declaran como required.
	for name, req := range map[string][]string{"create_checkpoint": {"label", "source"}, "format": {"source"}, "get_template": {"name"}, "import_mesh": {"file"}} {
		schema := got[name]["inputSchema"].(map[string]any)
		r, _ := schema["required"].([]any)
		if len(r) != len(req) {
			t.Errorf("%s required = %v", name, r)
		}
	}
}

func TestProtocolErrors(t *testing.T) {
	c := setup(t, nil)
	if m := c.call("nope/nada", nil); m["error"] == nil || int(m["error"].(map[string]any)["code"].(float64)) != -32601 {
		t.Errorf("método desconocido: %v", m)
	}
	if m := c.call("tools/call", map[string]any{"name": "no_existe"}); m["error"] == nil || int(m["error"].(map[string]any)["code"].(float64)) != -32602 {
		t.Errorf("herramienta desconocida: %v", m)
	}
	if m := c.call("tools/call", map[string]any{"name": "render", "arguments": []any{1}}); m["error"] == nil {
		t.Errorf("argumentos que no son un objeto: %v", m)
	}
	// Una línea rota se contesta con -32700 (id null) y la sesión sigue.
	if _, err := c.in.Write([]byte("{esto no es json\n")); err != nil {
		t.Fatal(err)
	}
	m := c.recv()
	if m["error"] == nil || int(m["error"].(map[string]any)["code"].(float64)) != -32700 {
		t.Errorf("JSON roto: %v", m)
	}
	if id, present := m["id"]; !present || id != nil {
		t.Errorf("el error de parseo tiene que llevar id null: %v", m)
	}
	if m := c.call("ping", nil); m["error"] != nil {
		t.Errorf("la sesión no sobrevivió: %v", m)
	}
}

func TestCreateCheckpointNeverOverwrites(t *testing.T) {
	c := setup(t, nil)
	first := c.create("Caja base", srcCube, map[string]any{"description": `Cubo "base"`})
	if first != "001-caja-base.scad" {
		t.Fatalf("primer checkpoint: %s", first)
	}
	if got := c.readWs(first); got != "// @checkpoint parent= desc=\"Cubo \\\"base\\\"\"\n"+srcCube {
		t.Errorf("contenido: %q", got)
	}
	// Sin parent continúa desde el más nuevo.
	second := c.create("Caja base", "color(\"blue\") cube(3);", nil)
	if second != "002-caja-base.scad" {
		t.Fatalf("segundo: %s", second)
	}
	if got := c.readWs(second); !strings.HasPrefix(got, "// @checkpoint parent=001-caja-base.scad desc=\"Caja base\"\n") {
		t.Errorf("padre por defecto: %q", got)
	}
	// Mismo rótulo otra vez: archivo nuevo, el anterior intacto.
	third := c.create("Caja base", "color(\"green\") cube(4);", map[string]any{"parent": first})
	if third != "003-caja-base.scad" {
		t.Fatalf("tercero: %s", third)
	}
	if got := c.readWs(third); !strings.HasPrefix(got, "// @checkpoint parent=001-caja-base.scad ") {
		t.Errorf("padre explícito (rama): %q", got)
	}
	if got := c.readWs(first); !strings.Contains(got, "cube([20, 10, 5])") {
		t.Errorf("se modificó un checkpoint existente: %q", got)
	}
	// Un archivo que apareció por fuera tampoco se pisa.
	if err := os.WriteFile(filepath.Join(c.Ws, "004-otro.scad"), []byte("// del agente\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fifth := c.create("otro", "cube(1);", nil)
	if fifth != "005-otro.scad" {
		t.Errorf("quinto: %s", fifth)
	}
	if got := c.readWs("004-otro.scad"); got != "// del agente\n" {
		t.Errorf("pisó el archivo externo: %q", got)
	}

	out := c.mustOK("list_checkpoints", nil)
	if out["latest"] != "005-otro.scad" {
		t.Errorf("latest: %v", out["latest"])
	}
	list, _ := out["checkpoints"].([]any)
	if len(list) != 5 {
		t.Fatalf("checkpoints: %d", len(list))
	}
	root := list[0].(map[string]any)
	kids, _ := root["children"].([]any)
	if root["file"] != first || root["parent"] != "" || len(kids) != 2 || root["description"] != `Cubo "base"` || root["render"] != "unknown" {
		t.Errorf("raíz: %v", root)
	}
	if code := c.readWs(first); code != c.tool("read_checkpoint", map[string]any{"file": first}).Text {
		t.Error("read_checkpoint no devuelve el código tal cual")
	}
	if got := c.tool("read_checkpoint", nil).Text; got != c.readWs("005-otro.scad") {
		t.Errorf("read_checkpoint sin file debería leer el más nuevo: %q", got)
	}
}

func TestActiveParentInMCP(t *testing.T) {
	c := setup(t, nil)
	a := c.create("uno", srcCube, nil)  // 001 (raíz)
	c.create("dos", srcCube, nil)       // 002, hijo de 001
	d := c.create("tres", srcCube, nil) // 003, hijo de 002 (sin activo: el más nuevo)

	// Sin activo, list_checkpoints lo informa vacío y no aparece uno solo.
	out := c.mustOK("list_checkpoints", nil)
	if v, ok := out["activeParent"]; !ok || v != "" {
		t.Errorf("activeParent sin activo: %#v", out["activeParent"])
	}

	// El usuario ramifica desde 001: el próximo checkpoint sin `parent` cuelga de él.
	if err := checkpoint.SetActiveParent(c.Ws, a); err != nil {
		t.Fatal(err)
	}
	if got := c.mustOK("list_checkpoints", nil)["activeParent"]; got != a {
		t.Errorf("activeParent = %v, quiero %s", got, a)
	}
	e := c.create("rama", srcCube, nil)
	if got := c.readWs(e); !strings.HasPrefix(got, "// @checkpoint parent="+a+" ") {
		t.Errorf("sin parent debía usar el activo (%s): %q", a, got)
	}
	// ...y el activo avanzó al hijo recién creado.
	if got := c.mustOK("list_checkpoints", nil)["activeParent"]; got != e {
		t.Errorf("el activo debía avanzar a %s: %v", e, got)
	}
	f := c.create("rama dos", srcCube, nil)
	if got := c.readWs(f); !strings.HasPrefix(got, "// @checkpoint parent="+e+" ") {
		t.Errorf("segundo hijo de la rama: %q", got)
	}

	// Un parent explícito distinto del activo no lo mueve.
	g := c.create("lateral", srcCube, map[string]any{"parent": d})
	if got := c.readWs(g); !strings.HasPrefix(got, "// @checkpoint parent="+d+" ") {
		t.Errorf("parent explícito: %q", got)
	}
	if got := c.mustOK("list_checkpoints", nil)["activeParent"]; got != f {
		t.Errorf("un hijo de otro no mueve el activo: %v", got)
	}

	// Un activo que ya no existe se ignora y rige «el más nuevo».
	if err := os.WriteFile(filepath.Join(c.Ws, ".scadlab", "active-parent"), []byte("099-fantasma.scad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := c.create("sigue", srcCube, nil)
	if got := c.readWs(h); !strings.HasPrefix(got, "// @checkpoint parent="+g+" ") {
		t.Errorf("con el activo colgado debía usar el más nuevo (%s): %q", g, got)
	}
}

func TestCreateCheckpointValidation(t *testing.T) {
	c := setup(t, nil)
	if r := c.tool("create_checkpoint", map[string]any{"source": "cube(1);"}); !r.IsError {
		t.Error("sin label: quiero isError")
	}
	if r := c.tool("create_checkpoint", map[string]any{"label": "x"}); !r.IsError {
		t.Error("sin source: quiero isError")
	}
	first := c.create("uno", srcCube, nil)
	for _, parent := range []string{"../001-uno.scad", "009-no-existe.scad", "sub/001-uno.scad", "001-uno.txt"} {
		r := c.tool("create_checkpoint", map[string]any{"label": "x", "source": "cube(1);", "parent": parent})
		if !r.IsError {
			t.Errorf("parent %q: quiero isError", parent)
		}
	}
	// Nada de eso dejó archivos.
	files, _ := filepath.Glob(filepath.Join(c.Ws, "*.scad"))
	if len(files) != 1 || filepath.Base(files[0]) != first {
		t.Errorf("quedaron archivos de más: %v", files)
	}
}

func TestPathsOutsideWorkspaceAreErrors(t *testing.T) {
	c := setup(t, nil)
	c.create("uno", srcCube, nil)
	outside := filepath.Join(t.TempDir(), "secreto.scad")
	if err := os.WriteFile(outside, []byte("secreto"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := []string{"../secreto.scad", outside, "sub/001-uno.scad", "001-uno.txt", "..", ".", "001-uno.scad/../../x.scad", "nul\x00.scad"}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(c.Ws, "002-enlace.scad")); err != nil {
			t.Fatal(err)
		}
		bad = append(bad, "002-enlace.scad")
	}
	for _, tool := range []string{"read_checkpoint", "render", "lint", "measure", "preview", "export"} {
		for _, file := range bad {
			r := c.tool(tool, map[string]any{"file": file})
			if !r.IsError {
				t.Errorf("%s con file=%q: quiero isError, obtuve %s", tool, file, truncate(r.Text))
			}
			if strings.Contains(r.Text, "secreto") && !strings.Contains(file, "secreto") {
				t.Errorf("%s con file=%q filtró el contenido de un archivo externo", tool, file)
			}
		}
	}
	// `file` en lint junto a source solo da nombre: tiene que ser válido igual.
	if r := c.tool("lint", map[string]any{"file": "../x.scad", "source": "cube(1);"}); !r.IsError {
		t.Error("lint con source y file inválido: quiero isError")
	}
	if c.exists("exports") {
		if ents, _ := os.ReadDir(filepath.Join(c.Ws, "exports")); len(ents) != 0 {
			t.Errorf("se exportó algo con rutas inválidas: %v", ents)
		}
	}
}

func TestFormat(t *testing.T) {
	c := setup(t, nil)
	r := c.tool("format", map[string]any{"source": "module m(){\ncube(1);\n}"})
	if r.IsError || r.Text != "module m() {\n    cube(1);\n}\n" {
		t.Errorf("format: %v %q", r.IsError, r.Text)
	}
	if r := c.tool("format", map[string]any{"source": "s = \"sin cerrar"}); !r.IsError {
		t.Error("format de un string sin cerrar: quiero isError")
	}
	if r := c.tool("format", nil); !r.IsError {
		t.Error("format sin source: quiero isError")
	}
}

func TestTemplates(t *testing.T) {
	c := setup(t, nil)
	out := c.mustOK("list_templates", nil)
	list, _ := out["templates"].([]any)
	if len(list) < 10 {
		t.Fatalf("plantillas: %d", len(list))
	}
	names := map[string]bool{}
	for _, it := range list {
		tp := it.(map[string]any)
		names[tp["name"].(string)] = true
		if tp["description"] == "" {
			t.Errorf("%v sin descripción", tp["name"])
		}
	}
	if !names["knob"] || !names["box-with-lid"] {
		t.Errorf("faltan plantillas: %v", names)
	}
	for _, n := range []string{"knob", "knob.scad"} {
		if r := c.tool("get_template", map[string]any{"name": n}); r.IsError || !strings.Contains(r.Text, "knob_d") {
			t.Errorf("get_template %s: %v %s", n, r.IsError, truncate(r.Text))
		}
	}
	for _, n := range []string{"no-existe", "../knob", "..", "", "knob/../knob", "templates/knob"} {
		if r := c.tool("get_template", map[string]any{"name": n}); !r.IsError {
			t.Errorf("get_template %q: quiero isError", n)
		}
	}
}

func TestPrinterInfoFromStoredWorkspace(t *testing.T) {
	var want printers.Printer
	all, err := printers.All()
	if err != nil || len(all) < 3 {
		t.Fatal(err)
	}
	want = all[2]
	c := setup(t, func(t *testing.T, st *store.Store, ws string) {
		if _, err := st.InsertWorkspace(store.Workspace{Name: "Mi Caja", Path: ws, PrinterID: want.ID}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSetting("defaultMaterial", "petg"); err != nil {
			t.Fatal(err)
		}
	})
	out := c.mustOK("printer_info", nil)
	pr, _ := out["printer"].(map[string]any)
	if pr["id"] != want.ID || pr["name"] != want.Name {
		t.Errorf("impresora: %v, quería %s", pr, want.ID)
	}
	vol, _ := pr["buildVolumeMm"].(map[string]any)
	if vol["x"] != want.BuildVolume.X || vol["z"] != want.BuildVolume.Z {
		t.Errorf("volumen: %v", vol)
	}
	if int(pr["multicolorSlots"].(float64)) != want.MulticolorSlots {
		t.Errorf("slots: %v", pr["multicolorSlots"])
	}
	if out["workspace"] != "Mi Caja" {
		t.Errorf("workspace: %v", out["workspace"])
	}
	if mat, _ := out["defaultMaterial"].(map[string]any); mat["id"] != "petg" {
		t.Errorf("material por defecto: %v", mat)
	}
	if rules, _ := out["fdmRules"].([]any); len(rules) < 5 {
		t.Errorf("fdmRules: %v", rules)
	}
}

func TestPrinterInfoFallsBackToPrinterJSON(t *testing.T) {
	c := setup(t, nil) // sin base: la impresora sale de printer.json
	out := c.mustOK("printer_info", nil)
	pr, _ := out["printer"].(map[string]any)
	if pr["id"] != "bambu-a1-mini" {
		t.Errorf("printer.json: %v", pr)
	}
}

func TestSpanishMessagesUseTu(t *testing.T) {
	c := setup(t, func(t *testing.T, st *store.Store, ws string) {
		if err := st.SetSetting("language", "es"); err != nil {
			t.Fatal(err)
		}
	})
	r := c.tool("render", nil)
	if !r.IsError || !strings.Contains(r.Text, "no tiene checkpoints") {
		t.Errorf("render sin checkpoints en español: %v %q", r.IsError, r.Text)
	}
	r = c.tool("create_checkpoint", map[string]any{"source": "cube(1);"})
	if !r.IsError || !strings.Contains(r.Text, "falta `label`") {
		t.Errorf("mensaje en español: %q", r.Text)
	}
	for _, voseo := range []string{"Corregí", "Pedí", "Creá", "podés"} {
		if strings.Contains(r.Text, voseo) {
			t.Errorf("voseo en %q", r.Text)
		}
	}
}

func TestRenderOKAndMeasure(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	name := c.create("cubo", srcCube, nil)
	out := c.mustOK("render", map[string]any{"file": name})
	if out["status"] != "ok" || out["file"] != name {
		t.Fatalf("render: %v", out)
	}
	m, _ := out["mesh"].(map[string]any)
	size, _ := m["sizeMm"].(map[string]any)
	if size["x"] != 20.0 || size["y"] != 10.0 || size["z"] != 5.0 {
		t.Errorf("tamaño: %v", size)
	}
	if m["volumeMm3"] != 1000.0 || m["watertight"] != true || m["triangles"].(float64) != 12 {
		t.Errorf("malla: %v", m)
	}
	if out["fitsBuildVolume"] != true || out["openscadVersion"] == "" || out["backend"] == "" {
		t.Errorf("render: %v", out)
	}
	if c.exists("RENDER_ERRORS.md") {
		t.Error("un render bueno no deja RENDER_ERRORS.md")
	}
	// Sin file renderiza el más nuevo, y la segunda vez sale de la caché.
	again := c.mustOK("render", nil)
	if again["file"] != name || again["cacheHit"] != true {
		t.Errorf("render del más nuevo: %v", again)
	}
	if st := c.mustOK("list_checkpoints", nil)["checkpoints"].([]any)[0].(map[string]any)["render"]; st != "ok" {
		t.Errorf("estado en el árbol: %v", st)
	}

	// measure: cubo de 20×10×5 (1000 mm³, 700 mm² de área). Estimación con los
	// defaults (15 % de relleno, 2 perímetros de 0,42): cáscara = min(1000,
	// 700×0,84) = 588; efectivo = 588 + 412×0,15 = 649,8 mm³; PLA 1,24 → 0,806 g.
	// Ya no es la pieza sólida al 100 % (1,24 g).
	ms := c.mustOK("measure", map[string]any{"file": name})
	if ms["estimatedWeightG"] != 0.81 || ms["fitsBuildVolume"] != true {
		t.Errorf("measure: %v", ms)
	}
	if _, old := ms["weightNote"]; old {
		t.Error("weightNote ya no existe: los supuestos van en estimate.assumptions")
	}
	if mat, _ := ms["material"].(map[string]any); mat["id"] != "pla" || mat["densityGCm3"] != 1.24 {
		t.Errorf("material: %v", mat)
	}
	est, _ := ms["estimate"].(map[string]any)
	if est["infillPercent"] != 15.0 || est["wallLoops"] != 2.0 || est["lineWidthMm"] != 0.42 || est["filamentDiameterMm"] != 1.75 || est["currency"] != "USD" {
		t.Errorf("parámetros de la estimación: %v", est)
	}
	total, _ := est["total"].(map[string]any)
	if total["massG"] != 0.81 || total["lengthM"] != 0.27 || total["cost"] != 0.016 {
		t.Errorf("total: %v", total)
	}
	eparts, _ := est["parts"].([]any)
	if len(eparts) != 1 {
		t.Fatalf("partes de la estimación: %v", eparts)
	}
	if p0, _ := eparts[0].(map[string]any); p0["materialId"] != "pla" || p0["effectiveMm3"] != 649.8 || p0["volumeMm3"] != 1000.0 {
		t.Errorf("parte estimada: %v", p0)
	}
	assumptions, _ := est["assumptions"].([]any)
	if len(assumptions) < 4 || !strings.Contains(assumptions[0].(string), "estimate") {
		t.Errorf("supuestos: %v", assumptions)
	}
	mparts, _ := ms["parts"].([]any)
	if len(mparts) != 1 {
		t.Fatalf("partes: %v", mparts)
	}
	if p0, _ := mparts[0].(map[string]any); p0["color"] != "#FF0000" || p0["uncolored"] != false || p0["volumeMm3"] != 1000.0 || p0["watertight"] != true {
		t.Errorf("parte: %v", p0)
	}
	pr, _ := ms["printer"].(map[string]any)
	if pr["id"] != "bambu-a1-mini" {
		t.Errorf("printer: %v", pr)
	}
}

// measure reparte el modelo por color y estima cada parte; el total es la suma.
func TestMeasureParts(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	name := c.create("dos", "color(\"red\") cube(10);\ntranslate([20,0,0]) color(\"blue\") cube(10);\n", nil)
	ms := c.mustOK("measure", map[string]any{"file": name})
	parts, _ := ms["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("partes: %v", ms["parts"])
	}
	p0, _ := parts[0].(map[string]any)
	p1, _ := parts[1].(map[string]any)
	if p0["color"] != "#FF0000" || p1["color"] != "#0000FF" || p0["volumeMm3"] != 1000.0 || p1["volumeMm3"] != 1000.0 {
		t.Errorf("partes: %v %v", p0, p1)
	}
	est, _ := ms["estimate"].(map[string]any)
	eparts, _ := est["parts"].([]any)
	total, _ := est["total"].(map[string]any)
	if len(eparts) != 2 {
		t.Fatalf("estimación: %v", est)
	}
	// Cubo de 10: cáscara = min(1000, 600×0,84 = 504); efectivo = 504 + 496×0,15 = 578,4 mm³ = 0,717 g.
	for _, ep := range eparts {
		if m, _ := ep.(map[string]any); m["effectiveMm3"] != 578.4 || m["massG"] != 0.72 {
			t.Errorf("parte estimada: %v", m)
		}
	}
	if total["massG"] != 1.43 || ms["estimatedWeightG"] != 1.43 {
		t.Errorf("total: %v / %v", total, ms["estimatedWeightG"])
	}
	// Las partes quedan en la caché compartida con la app.
	dataDir, _ := appdata.Dir()
	if _, err := os.Stat(filepath.Join(dataDir, "cache", "parts")); err != nil {
		t.Errorf("no se creó la caché de partes: %v", err)
	}
}

// El relleno de Configuración manda: al 100 % el peso es el de la pieza sólida.
func TestMeasureFollowsSettingsInfill(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, func(t *testing.T, st *store.Store, _ string) {
		s := store.DefaultSettings()
		s.InfillPercent = 100
		s.DefaultMaterial = "petg"
		if err := st.SaveSettings(s); err != nil {
			t.Fatal(err)
		}
	})
	name := c.create("cubo", srcCube, nil)
	ms := c.mustOK("measure", map[string]any{"file": name})
	// 1000 mm³ macizos de PETG (1,27) = 1,27 g, a 22 por kg.
	est, _ := ms["estimate"].(map[string]any)
	total, _ := est["total"].(map[string]any)
	if ms["estimatedWeightG"] != 1.27 || est["infillPercent"] != 100.0 || total["cost"] != 0.028 {
		t.Errorf("measure al 100 %%: %v", ms)
	}
	if mat, _ := ms["material"].(map[string]any); mat["id"] != "petg" {
		t.Errorf("material: %v", mat)
	}
}

func TestMeasureDoesNotFitAndWarnings(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	name := c.create("enorme", "color(\"red\") translate([0,0,-10]) cube([200, 50, 20]);", nil)
	ms := c.mustOK("measure", map[string]any{"file": name})
	if ms["fitsBuildVolume"] != false {
		t.Errorf("200 mm no cabe en 180: %v", ms)
	}
	over, _ := ms["overflowMm"].(map[string]any)
	if over["x"] != 20.0 || over["y"] != nil {
		t.Errorf("overflow: %v", over)
	}
	warns, _ := ms["warnings"].([]any)
	if len(warns) != 1 || !strings.Contains(warns[0].(string), "z=0") {
		t.Errorf("aviso de z=0: %v", warns)
	}
}

func TestRenderErrorHasLineAndRenderErrorsMd(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	bad := c.create("roto", srcBroken, nil)
	out, r := c.toolJSON("render", map[string]any{"file": bad})
	if !r.IsError || out["status"] != "error" {
		t.Fatalf("render roto: isError=%v %v", r.IsError, out)
	}
	diags, _ := out["diagnostics"].([]any)
	found := false
	for _, d := range diags {
		dm := d.(map[string]any)
		if dm["severity"] == "error" && dm["line"].(float64) > 0 {
			found = true
			if dm["code"] == nil || dm["code"] == "" {
				t.Errorf("el diagnóstico no trae la línea de código: %v", dm)
			}
		}
	}
	if !found {
		t.Fatalf("sin error con línea: %v", diags)
	}
	if msg, _ := out["message"].(string); !strings.Contains(msg, "NEW checkpoint") || !strings.Contains(msg, bad) {
		t.Errorf("mensaje de corrección: %q", msg)
	}
	md := c.readWs("RENDER_ERRORS.md")
	if !strings.Contains(md, bad) {
		t.Errorf("RENDER_ERRORS.md no nombra el archivo: %s", md)
	}
	if st := c.mustOK("list_checkpoints", nil)["checkpoints"].([]any)[0].(map[string]any)["render"]; st != "error" {
		t.Errorf("estado en el árbol: %v", st)
	}

	// Un checkpoint nuevo que anda borra RENDER_ERRORS.md...
	good := c.create("arreglado", srcCube, map[string]any{"parent": bad})
	c.mustOK("render", map[string]any{"file": good})
	if c.exists("RENDER_ERRORS.md") {
		t.Error("el render bueno del más nuevo debería borrar RENDER_ERRORS.md")
	}
	// ...y renderizar el viejo roto NO lo vuelve a crear: no es el más nuevo.
	_, r = c.toolJSON("render", map[string]any{"file": bad})
	if !r.IsError {
		t.Error("el viejo sigue roto")
	}
	if c.exists("RENDER_ERRORS.md") {
		t.Error("renderizar un checkpoint viejo no debe tocar RENDER_ERRORS.md")
	}
	// En cambio, si el más nuevo falla, sí.
	worse := c.create("peor", srcBroken, map[string]any{"parent": good})
	c.toolJSON("render", map[string]any{"file": worse})
	if md := c.readWs("RENDER_ERRORS.md"); !strings.Contains(md, worse) {
		t.Errorf("RENDER_ERRORS.md debería nombrar %s: %s", worse, md)
	}
}

func TestLintTool(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	out := c.mustOK("lint", map[string]any{"source": "cube(1);\n"})
	diags, _ := out["diagnostics"].([]any)
	foundColor := false
	for _, d := range diags {
		if d.(map[string]any)["code"] == "SL003" {
			foundColor = true
		}
	}
	if !foundColor || out["warnings"].(float64) < 1 {
		t.Errorf("SL003 (sin color): %v", out)
	}
	out = c.mustOK("lint", map[string]any{"source": "color(\"red\") cube(1\n"})
	if out["errors"].(float64) < 1 {
		t.Errorf("error de sintaxis: %v", out)
	}
	for _, d := range out["diagnostics"].([]any) {
		dm := d.(map[string]any)
		if dm["severity"] == "error" && dm["line"].(float64) < 1 {
			t.Errorf("error sin línea: %v", dm)
		}
	}
	// De un archivo: usa su encabezado (SL002 = padre inexistente).
	name := c.create("lint", "color(\"red\") cube(1);", nil)
	out = c.mustOK("lint", map[string]any{"file": name})
	if out["file"] != name || out["errors"].(float64) != 0 || out["warnings"].(float64) != 0 {
		t.Errorf("checkpoint limpio: %v", out)
	}
	if err := os.WriteFile(filepath.Join(c.Ws, "009-hijo.scad"), []byte("// @checkpoint parent=007-nada.scad desc=\"x\"\ncolor(\"red\") cube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = c.mustOK("lint", map[string]any{"file": "009-hijo.scad"})
	if ds, _ := out["diagnostics"].([]any); len(ds) != 1 || ds[0].(map[string]any)["code"] != "SL002" {
		t.Errorf("SL002: %v", out)
	}
}

func TestPreviewReturnsPNG(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	name := c.create("cubo", srcCube, nil)

	check := func(args map[string]any, wantSide int) {
		t.Helper()
		r := c.tool("preview", args)
		if r.IsError {
			t.Fatalf("preview %v: %s", args, r.Text)
		}
		var img map[string]any
		for _, ct := range r.Content {
			if ct["type"] == "image" {
				img = ct
			}
		}
		if img == nil || img["mimeType"] != "image/png" {
			t.Fatalf("falta el contenido image/png: %v", r.Content)
		}
		raw, err := base64.StdEncoding.DecodeString(img["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(raw, []byte("\x89PNG")) {
			t.Fatalf("no es un PNG: %q", raw[:min(8, len(raw))])
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Width != wantSide || cfg.Height != wantSide {
			t.Errorf("tamaño %dx%d, quería %d", cfg.Width, cfg.Height, wantSide)
		}
	}
	check(map[string]any{"file": name}, 640)
	check(map[string]any{"file": name, "view": "top", "size": 300}, 300)
	check(map[string]any{"view": "front", "size": "400"}, 400) // número como texto
	check(map[string]any{"file": name, "size": 10}, minPreviewSize)
	check(map[string]any{"file": name, "size": 99999}, maxPreviewSize)
	for _, v := range viewNames {
		if r := c.tool("preview", map[string]any{"file": name, "view": v, "size": 256}); r.IsError {
			t.Errorf("vista %s: %s", v, r.Text)
		}
	}
	if r := c.tool("preview", map[string]any{"file": name, "view": "diagonal-rara"}); !r.IsError {
		t.Error("vista desconocida: quiero isError")
	}
	// No deja temporales ni sale del workspace.
	if ents, _ := os.ReadDir(filepath.Join(c.Ws, ".scadlab", "preview")); len(ents) != 0 {
		t.Errorf("temporales del preview sin borrar: %v", ents)
	}

	// Un modelo roto no da imagen: da el error con su línea.
	bad := c.create("roto", srcBroken, nil)
	r := c.tool("preview", map[string]any{"file": bad})
	if !r.IsError || !strings.Contains(r.Text, "line") {
		t.Errorf("preview de un modelo roto: %v %q", r.IsError, r.Text)
	}
	for _, ct := range r.Content {
		if ct["type"] == "image" {
			t.Error("un modelo roto no puede devolver imagen")
		}
	}
}

func TestExportWritesInsideExports(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	two := "color(\"red\") cube([20, 10, 5]);\ncolor(\"blue\") translate([0, 0, 5]) cube([20, 10, 5]);\n"
	name := c.create("dos colores", two, nil)

	stl := c.mustOK("export", map[string]any{"file": name, "format": "stl"})
	stlPath, _ := stl["path"].(string)
	if filepath.Dir(stlPath) != mustReal(t, filepath.Join(c.Ws, "exports")) || !strings.HasSuffix(stlPath, ".stl") {
		t.Errorf("stl: %v", stl)
	}
	if st, err := os.Stat(stlPath); err != nil || st.Size() < 84 {
		t.Errorf("el STL no se escribió: %v", err)
	}
	if rel, _ := stl["relativePath"].(string); !strings.HasPrefix(rel, "exports/") {
		t.Errorf("relativePath: %v", rel)
	}

	tm := c.mustOK("export", map[string]any{"file": name}) // default 3mf
	if tm["format"] != "3mf" {
		t.Errorf("formato por defecto: %v", tm["format"])
	}
	parts, _ := tm["parts"].([]any)
	if len(parts) != 2 {
		t.Errorf("partes: %v", parts)
	}
	zr, err := zip.OpenReader(tm["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, f := range zr.File {
		has[f.Name] = true
	}
	zr.Close()
	if !has["3D/3dmodel.model"] || !has["[Content_Types].xml"] {
		t.Errorf("3MF sin sus partes: %v", has)
	}

	pc := c.mustOK("export", map[string]any{"file": name, "format": "stl-per-color"})
	if !strings.HasSuffix(pc["path"].(string), ".zip") {
		t.Errorf("stl-per-color: %v", pc)
	}
	zr, err = zip.OpenReader(pc["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 {
		t.Errorf("el zip debería tener un STL por color: %d", len(zr.File))
	}
	zr.Close()

	if r := c.tool("export", map[string]any{"file": name, "format": "obj"}); !r.IsError {
		t.Error("formato desconocido: quiero isError")
	}
	// Todo lo exportado quedó en <ws>/exports.
	ents, _ := os.ReadDir(filepath.Join(c.Ws, "exports"))
	if len(ents) != 3 {
		t.Errorf("exports: %v", ents)
	}
	// Una carpeta exports que apunta afuera no se usa.
	if runtime.GOOS != "windows" {
		if err := os.RemoveAll(filepath.Join(c.Ws, "exports")); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(c.Ws, "exports")); err != nil {
			t.Fatal(err)
		}
		if r := c.tool("export", map[string]any{"file": name, "format": "stl"}); !r.IsError {
			t.Error("exports -> afuera: quiero isError")
		}
		if ents, _ := os.ReadDir(outside); len(ents) != 0 {
			t.Errorf("se escribió fuera del workspace: %v", ents)
		}
	}
}

func mustReal(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Sin OpenSCAD instalado el motor devuelve StatusMissing y la herramienta tiene
// que decirlo claro (con el camino a Configuración), no «error de render».
func TestMissingOpenSCADIsReported(t *testing.T) {
	e := &env{ws: t.TempDir()}
	r, err := e.failedRender("001-x.scad", render.Result{Status: render.StatusMissing})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError || !strings.Contains(r.Content[0]["text"].(string), "OpenSCAD is not installed") {
		t.Errorf("missing: %+v", r)
	}
}

// El cliente que cancela una petición larga (notifications/cancelled) mata el
// OpenSCAD en curso en vez de dejarlo consumiendo CPU, y la petición se contesta.
func TestCancelledNotificationStopsRender(t *testing.T) {
	needOpenSCAD(t)
	c := setup(t, nil)
	// 100 000 esferas: más de ocho segundos con Manifold.
	name := c.create("lento", "for (i = [0:99999]) translate([i * 0.01, sin(i) * 3, 0]) color(\"red\") sphere(2, $fn = 24);\n", nil)
	c.next++
	id := c.next
	start := time.Now()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "render", "arguments": map[string]any{"file": name}}})
	time.Sleep(1500 * time.Millisecond)
	// Mientras el render corre, el servidor sigue contestando otras peticiones.
	if m := c.call("ping", nil); m["error"] != nil {
		t.Errorf("ping durante el render: %v", m)
	}
	c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id, "reason": "test"}})
	m := c.recv()
	if got, _ := m["id"].(float64); int(got) != id {
		t.Fatalf("respuesta de otra petición: %v", m)
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("la cancelación tardó %v", time.Since(start))
	}
	res, _ := m["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), `"status": "cancelled"`) {
		t.Errorf("render cancelado: %v", res)
	}
	if c.exists("RENDER_ERRORS.md") {
		t.Error("un render cancelado no debe dejar RENDER_ERRORS.md")
	}
}

// S12.c: las instrucciones del servidor dicen el flujo obligatorio.
func TestServerInstructionsMandateFlow(t *testing.T) {
	for _, want := range []string{"create_checkpoint", "render", "preview", "measure", "without asking", "never write .scad files through the shell", "color()",
		"create_from_template", "set_parameters", "edit_mesh", "suggest_orientation", "install_library", "trash_checkpoint"} {
		if !strings.Contains(serverInstructions, want) {
			t.Errorf("serverInstructions sin %q", want)
		}
	}
	if len(serverInstructions) > 2200 {
		t.Errorf("serverInstructions demasiado larga (%d): deben ser pocas líneas", len(serverInstructions))
	}
}
