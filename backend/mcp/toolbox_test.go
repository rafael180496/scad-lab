package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"scadlab/backend/appdata"
	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/parts"
	"scadlab/backend/refmodel"
	"scadlab/backend/render"
	"scadlab/backend/scadwasm"
	"scadlab/backend/store"
	"scadlab/backend/threemf"
	"scadlab/backend/workspace"
)

// Estas pruebas llaman a las herramientas EN PROCESO, sin JSON-RPC, sobre un
// workspace temporal y un SCADLAB_DATA_DIR temporal (jamás se toca la base de
// la app). Las que renderizan con el OpenSCAD instalado se saltan si falta; las
// del motor integrado corren siempre.

func toolboxEnv(t *testing.T) (dataDir, ws string) {
	t.Helper()
	dataDir = t.TempDir()
	t.Setenv(appdata.OverrideEnv, dataDir)
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("en") })
	ws, err := workspace.Create(t.TempDir(), "Prueba caja", miniPrinter())
	if err != nil {
		t.Fatal(err)
	}
	return dataDir, ws
}

func newToolbox(t *testing.T, inApp bool) (*Toolbox, string) {
	t.Helper()
	_, ws := toolboxEnv(t)
	tb, err := NewToolbox(ToolboxOptions{WorkspaceDir: ws, InApp: inApp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tb.Close() })
	return tb, ws
}

func (tb *Toolbox) must(t *testing.T, name string, args map[string]any) ToolOutput {
	t.Helper()
	out, err := tb.Call(context.Background(), name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if out.IsError {
		t.Fatalf("%s: isError inesperado: %s", name, truncate(out.Text))
	}
	return out
}

func (tb *Toolbox) mustJSON(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	out := tb.must(t, name, args)
	var m map[string]any
	if err := json.Unmarshal([]byte(out.Text), &m); err != nil {
		t.Fatalf("%s: el resultado no es JSON: %v\n%s", name, err, truncate(out.Text))
	}
	return m
}

func (tb *Toolbox) fails(t *testing.T, name string, args map[string]any) ToolOutput {
	t.Helper()
	out, err := tb.Call(context.Background(), name, args)
	if err != nil {
		t.Fatalf("%s: un fallo de la herramienta no es un error de Go: %v", name, err)
	}
	if !out.IsError || out.Text == "" {
		t.Fatalf("%s: esperaba IsError con texto, obtuve %+v", name, out)
	}
	return out
}

func toolNames(specs []ToolSpec) []string {
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.Name
	}
	return names
}

func TestToolboxToolsAreTheServersTools(t *testing.T) {
	tb, _ := newToolbox(t, false)
	got := toolNames(tb.Tools())
	if strings.Join(got, ",") != strings.Join(wantTools, ",") {
		t.Errorf("herramientas sin InApp:\n got %v\nwant %v", got, wantTools)
	}
	for _, s := range tb.Tools() {
		if len(s.Description) < 20 || s.InputSchema["type"] != "object" || s.InputSchema["properties"] == nil {
			t.Errorf("%s: spec incompleta: %+v", s.Name, s)
		}
	}

	// InApp ya no cambia el catálogo: la app y el servidor ofrecen lo mismo.
	_, ws := toolboxEnv(t)
	app, err := NewToolbox(ToolboxOptions{WorkspaceDir: ws, InApp: true})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if got = toolNames(app.Tools()); strings.Join(got, ",") != strings.Join(wantTools, ",") {
		t.Errorf("herramientas InApp:\n got %v\nwant %v", got, wantTools)
	}
	byName := map[string]ToolSpec{}
	for _, s := range app.Tools() {
		byName[s.Name] = s
	}
	for name, req := range map[string]string{"read_skill": "name", "inspect_reference": "file"} {
		r, _ := byName[name].InputSchema["required"].([]string)
		if len(r) != 1 || r[0] != req {
			t.Errorf("%s required = %v", name, r)
		}
	}
}

// Sin dependencias inyectadas la caja abre lo suyo y lo cierra; las herramientas
// del servidor `--mcp` no cambian (TestToolsListHasEverythingWithSchema ya
// exige que sean exactamente wantTools).
func TestToolboxOwnsWhatItOpens(t *testing.T) {
	tb, _ := newToolbox(t, false)
	if o := tb.e.owned; !o.engine || !o.wasm || o.store {
		t.Errorf("sin inyección debía abrir motor y wasm (y no la base, que no existe): %+v", o)
	}
	if err := tb.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tb.Close(); err != nil {
		t.Errorf("Close debe ser idempotente: %v", err)
	}
	out, err := tb.Call(context.Background(), "list_checkpoints", nil)
	if !errors.Is(err, ErrToolboxClosed) || out.Text != "" {
		t.Errorf("llamar tras Close: %+v, %v", out, err)
	}
}

func TestToolboxUsesInjectedDependenciesAndDoesNotCloseThem(t *testing.T) {
	dataDir, ws := toolboxEnv(t)
	if err := os.MkdirAll(filepath.Join(dataDir, "otra"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "otra", "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	eng, err := render.NewEngine(filepath.Join(dataDir, "otra", "meshes"))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	wasm := scadwasm.New("")
	defer wasm.Close()
	ps := parts.NewStore(filepath.Join(dataDir, "otra", "parts"))

	// El workspace está registrado en la base de la APP (no en la que habría
	// abierto el servidor): su impresora tiene que salir de ahí.
	if _, err := st.InsertWorkspace(store.Workspace{Name: "Registrado", Path: ws, PrinterID: "bambu-x1c"}); err != nil {
		t.Fatal(err)
	}
	// Y no existe scadlab.db en el directorio de datos: abrir "como hoy" no
	// habría encontrado nada, y la caja no debe crear una.
	opts := ToolboxOptions{WorkspaceDir: ws, Store: st, Engine: eng, WASM: wasm, Parts: ps}
	tb, err := NewToolbox(opts)
	if err != nil {
		t.Fatal(err)
	}
	if tb.e.store != st || tb.e.engine != eng || tb.e.wasm != wasm || tb.e.parts != ps {
		t.Fatal("la caja no usa lo inyectado")
	}
	if o := tb.e.owned; o.engine || o.wasm || o.store {
		t.Errorf("no debe creerse dueña de lo inyectado: %+v", o)
	}
	info := tb.mustJSON(t, "printer_info", nil)
	if p, _ := info["printer"].(map[string]any); p["id"] != "bambu-x1c" || info["workspace"] != "Registrado" {
		t.Errorf("printer_info no leyó la base de la app: %v", info)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "scadlab.db")); err == nil {
		t.Error("se creó una scadlab.db en el directorio de datos")
	}

	if err := tb.Close(); err != nil {
		t.Fatal(err)
	}
	// Lo inyectado sigue vivo: la base responde y otra caja renderiza con el mismo motor.
	if _, err := st.GetSettings(); err != nil {
		t.Errorf("la caja cerró la base de la app: %v", err)
	}
	tb2, err := NewToolbox(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer tb2.Close()
	name := tb2.mustJSON(t, "create_checkpoint", map[string]any{"label": "caja", "source": srcCube})["file"].(string)
	if _, ok := tb2.mustJSON(t, "list_checkpoints", nil)["checkpoints"].([]any); !ok {
		t.Fatal("list_checkpoints")
	}
	out, err := tb2.Call(context.Background(), "render", map[string]any{"file": name})
	if err != nil || out.IsError && !strings.Contains(out.Text, "not installed") {
		// Sin OpenSCAD nativo ni WASM disponible el render puede fallar, pero no
		// por un motor cerrado.
		t.Fatalf("render con el motor inyectado tras Close: %+v %v", out, err)
	}
}

// Las herramientas que no renderizan funcionan igual que en el servidor.
func TestToolboxCheckpointsLintFormatTemplates(t *testing.T) {
	tb, ws := newToolbox(t, false)

	empty := tb.mustJSON(t, "list_checkpoints", nil)
	if cps, _ := empty["checkpoints"].([]any); len(cps) != 0 || empty["latest"] != "" {
		t.Errorf("workspace vacío: %v", empty)
	}
	first := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": "caja base", "description": "Caja", "source": srcCube})
	if first["file"] != "001-caja-base.scad" {
		t.Fatalf("create_checkpoint: %v", first)
	}
	second := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": "tapa", "source": srcCube, "parent": "001-caja-base.scad"})
	if second["file"] != "002-tapa.scad" || second["parent"] != "001-caja-base.scad" {
		t.Fatalf("segundo checkpoint: %v", second)
	}
	raw, err := os.ReadFile(filepath.Join(ws, "002-tapa.scad"))
	if err != nil || !strings.HasPrefix(string(raw), "// @checkpoint parent=001-caja-base.scad desc=\"tapa\"\n") {
		t.Errorf("cabecera: %q %v", raw, err)
	}
	if src := tb.must(t, "read_checkpoint", map[string]any{"file": "001-caja-base.scad"}).Text; !strings.Contains(src, "cube([20, 10, 5])") {
		t.Errorf("read_checkpoint: %q", src)
	}
	if src := tb.must(t, "read_checkpoint", nil).Text; !strings.Contains(src, "002-tapa") && !strings.Contains(src, "@checkpoint parent=001") {
		t.Errorf("read_checkpoint sin file debe leer el más nuevo: %q", src)
	}

	lint := tb.mustJSON(t, "lint", map[string]any{"source": "cube(1);\n"})
	if lint["warnings"].(float64) < 1 { // SL003: falta color()
		t.Errorf("lint: %v", lint)
	}
	if f := tb.must(t, "format", map[string]any{"source": "color(\"red\")cube(1);"}).Text; !strings.Contains(f, "cube(1)") {
		t.Errorf("format: %q", f)
	}
	tpls := tb.mustJSON(t, "list_templates", nil)["templates"].([]any)
	if len(tpls) < 5 {
		t.Errorf("list_templates: %v", tpls)
	}
	if src := tb.must(t, "get_template", map[string]any{"name": "knob"}).Text; !strings.Contains(src, "@checkpoint") {
		t.Errorf("get_template: %q", truncate(src))
	}
	info := tb.mustJSON(t, "printer_info", nil)
	if p, _ := info["printer"].(map[string]any); p["id"] != "bambu-a1-mini" {
		t.Errorf("printer_info (printer.json del workspace): %v", info["printer"])
	}
}

// Un error de herramienta llega como IsError con el texto que el modelo debe
// leer, nunca como error de Go ni como pánico.
func TestToolboxErrorsAreOutputsNotPanics(t *testing.T) {
	tb, ws := newToolbox(t, true)

	if out := tb.fails(t, "read_checkpoint", map[string]any{"file": "999-no-existe.scad"}); !strings.Contains(out.Text, "999-no-existe.scad") {
		t.Errorf("checkpoint inexistente: %q", out.Text)
	}
	if out := tb.fails(t, "create_checkpoint", map[string]any{"source": "cube(1);"}); !strings.Contains(out.Text, "label") {
		t.Errorf("falta label: %q", out.Text)
	}
	if out := tb.fails(t, "no_existe", nil); !strings.Contains(out.Text, "unknown tool") || !strings.Contains(out.Text, "render") {
		t.Errorf("herramienta desconocida: %q", out.Text)
	}
	// Un archivo con ruta no puede salirse del workspace.
	for _, bad := range []string{"../x.scad", "/etc/passwd", "sub/x.scad", `..\x.scad`} {
		tb.fails(t, "read_checkpoint", map[string]any{"file": bad})
	}
	// Argumentos de tipo equivocado no revientan.
	tb.fails(t, "read_checkpoint", map[string]any{"file": []any{1, 2}})
	if _, err := tb.Call(context.Background(), "list_checkpoints", map[string]any{"x": 1}); err != nil {
		t.Error(err)
	}

	// Un pánico dentro de una herramienta se atrapa.
	tb.tools = append(tb.tools, Tool{Name: "boom", handler: func(context.Context, map[string]any) (*toolResult, error) {
		panic("kaboom")
	}})
	if out := tb.fails(t, "boom", nil); strings.Contains(out.Text, "kaboom") || !strings.Contains(out.Text, "boom") {
		t.Errorf("pánico: %q", out.Text)
	}
	if _, err := os.Stat(filepath.Join(ws, "RENDER_ERRORS.md")); err == nil {
		t.Error("nada de esto debía escribir RENDER_ERRORS.md")
	}
}

func TestToolboxCancelledContextReturnsCtxErr(t *testing.T) {
	tb, _ := newToolbox(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tb.Call(ctx, "list_checkpoints", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ctx cancelado: %v", err)
	}
}

func isPNG(b []byte) bool { return bytes.HasPrefix(b, pngMagic) }

// Con el OpenSCAD instalado: render, measure, preview (la imagen vuelve cruda,
// sin base64) y export.
func TestToolboxRenderMeasurePreviewExportNative(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	name := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": "dos colores", "source": twoColors})["file"].(string)

	r := tb.mustJSON(t, "render", map[string]any{"file": name})
	if r["status"] != "ok" || r["fitsBuildVolume"] != true {
		t.Fatalf("render: %v", r)
	}
	m := tb.mustJSON(t, "measure", map[string]any{"file": name})
	if mm, _ := m["mesh"].(map[string]any); mm["watertight"] != true {
		t.Errorf("measure: %v", m["mesh"])
	}
	if ps, _ := m["parts"].([]any); len(ps) != 2 {
		t.Errorf("measure: partes %v", m["parts"])
	}

	pv := tb.must(t, "preview", map[string]any{"file": name, "view": "front", "size": float64(300)})
	if len(pv.Images) != 1 || pv.Images[0].MIME != "image/png" || !isPNG(pv.Images[0].Data) {
		t.Fatalf("preview: %d imágenes, %+v", len(pv.Images), pv.Images)
	}
	img, err := png.Decode(bytes.NewReader(pv.Images[0].Data))
	if err != nil || img.Bounds().Dx() != 300 {
		t.Errorf("preview: PNG ilegible o de otro tamaño: %v", err)
	}
	if !strings.Contains(pv.Text, name) || strings.Contains(pv.Text, "iVBOR") {
		t.Errorf("preview: el texto no debe llevar la imagen en base64: %q", truncate(pv.Text))
	}

	// Un modelo que no renderiza: IsError con la línea y el código.
	bad := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": "roto", "source": srcBroken, "parent": name})["file"].(string)
	out, err := tb.Call(context.Background(), "render", map[string]any{"file": bad})
	if err != nil || !out.IsError {
		t.Fatalf("render roto: %+v %v", out, err)
	}
	var diag map[string]any
	if err := json.Unmarshal([]byte(out.Text), &diag); err != nil {
		t.Fatalf("render roto: no es JSON: %s", truncate(out.Text))
	}
	ds, _ := diag["diagnostics"].([]any)
	if len(ds) == 0 || !strings.Contains(out.Text, "line") || !strings.Contains(out.Text, "edit_checkpoint") {
		t.Errorf("render roto sin línea o sin la instrucción de corregir: %s", truncate(out.Text))
	}
	if _, err := os.Stat(filepath.Join(ws, "RENDER_ERRORS.md")); err != nil {
		t.Errorf("el render fallido del más nuevo debe escribir RENDER_ERRORS.md: %v", err)
	}
	// Una preview de un modelo roto es un error de texto, sin imagen.
	if pf := tb.fails(t, "preview", map[string]any{"file": bad}); len(pf.Images) != 0 {
		t.Errorf("preview roto trajo imagen")
	}

	ex := tb.mustJSON(t, "export", map[string]any{"file": name, "format": "3mf"})
	if _, err := os.Stat(ex["path"].(string)); err != nil {
		t.Errorf("export: %v", err)
	}
}

// Sin OpenSCAD instalado todo sale del motor integrado, incluida la imagen del
// preview (dibujada con backend/raster).
func TestToolboxWASMRenderPreview(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	tb, _ := newToolbox(t, false)
	name := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": "dos colores", "source": twoColors})["file"].(string)
	r := tb.mustJSON(t, "render", map[string]any{"file": name})
	if r["status"] != "ok" || r["engine"] != "wasm" {
		t.Fatalf("render: %v", r)
	}
	pv := tb.must(t, "preview", map[string]any{"file": name})
	if len(pv.Images) != 1 || !isPNG(pv.Images[0].Data) || pv.Images[0].MIME != "image/png" {
		t.Fatalf("preview: %+v", pv.Images)
	}
	if !strings.Contains(pv.Text, "built-in engine") {
		t.Errorf("preview: falta la nota del motor integrado: %q", truncate(pv.Text))
	}
}

// --- skills -----------------------------------------------------------------

func writeSkill(t *testing.T, ws, dir, body string, files map[string]string) string {
	t.Helper()
	base := filepath.Join(ws, ".claude", "skills", dir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func TestToolboxListSkills(t *testing.T) {
	tb, ws := newToolbox(t, true)
	if out := tb.mustJSON(t, "list_skills", nil); out["note"] == nil {
		t.Errorf("sin skills debe avisarlo: %v", out)
	}

	writeSkill(t, ws, "alpha", "---\nname: alpha\ndescription: >\n  Primera línea\n  segunda línea.\n---\n# Alpha\n", nil)
	writeSkill(t, ws, "carpeta-beta", "---\n# comentario\nname: beta-skill\ndescription: 'Dice ''hola'': y sigue'\n---\ncuerpo", nil)
	writeSkill(t, ws, "gamma", "sin encabezado alguno", nil)
	writeSkill(t, ws, "delta", "---\nname: \"delta\"\ndescription: \"Con \\\"comillas\\\" dobles\"\nother: x\n---\n", nil)
	writeSkill(t, ws, ".oculta", "---\nname: oculta\n---\n", nil)
	if err := os.MkdirAll(filepath.Join(ws, ".claude", "skills", "sin-skill-md"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Una skill cuya SKILL.md es un enlace a un archivo de afuera no cuenta.
	outside := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(outside, []byte("---\nname: ajena\ndescription: no debe salir\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(ws, ".claude", "skills", "ajena")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinks := os.Symlink(outside, filepath.Join(linked, "SKILL.md")) == nil

	out := tb.mustJSON(t, "list_skills", nil)
	list, _ := out["skills"].([]any)
	got := map[string]string{}
	var order []string
	for _, it := range list {
		m := it.(map[string]any)
		got[m["name"].(string)] = m["description"].(string)
		order = append(order, m["name"].(string))
		if m["folder"] == nil {
			t.Errorf("sin carpeta: %v", m)
		}
	}
	want := map[string]string{
		"alpha":      "Primera línea segunda línea.",
		"beta-skill": "Dice 'hola': y sigue",
		"gamma":      "",
		"delta":      `Con "comillas" dobles`,
	}
	if len(got) != len(want) {
		t.Errorf("skills = %v (symlinks=%v)", got, symlinks)
	}
	for n, d := range want {
		if got[n] != d {
			t.Errorf("skill %q: descripción %q, quería %q", n, got[n], d)
		}
	}
	if !sort.StringsAreSorted(order) {
		t.Errorf("sin ordenar: %v", order)
	}

	// La misma lista, para el prompt.
	infos := ListWorkspaceSkills(ws)
	if len(infos) != len(want) || infos[0].Name != "alpha" || infos[0].Description != want["alpha"] {
		t.Errorf("ListWorkspaceSkills = %+v", infos)
	}
	if got := ListWorkspaceSkills(filepath.Join(ws, "no-existe")); len(got) != 0 {
		t.Errorf("workspace inexistente: %v", got)
	}
}

func TestToolboxReadSkill(t *testing.T) {
	tb, ws := newToolbox(t, true)
	secret := filepath.Join(t.TempDir(), "secreto.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "privado.md"), []byte("DEL-WORKSPACE"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := writeSkill(t, ws, "alpha", "---\nname: alpha\ndescription: Una skill\n---\n# Alpha\nContenido de la skill.\n", map[string]string{
		"references/boxes.md":  "# Cajas\n",
		"templates/box.scad":   "cube(1);",
		"grande.md":            strings.Repeat("x", maxSkillFileBytes+1),
		"justo.md":             strings.Repeat("y", maxSkillFileBytes),
		"binario.bin":          "abc\x00def",
		".oculto/dentro.md":    "no se lista",
		"references/a b/c.md":  "con espacio",
		"references/enlazado/": "",
	})
	_ = os.Remove(filepath.Join(base, "references", "enlazado")) // era un directorio vacío de relleno
	symlinks := true
	if err := os.Symlink(secret, filepath.Join(base, "fuga.md")); err != nil {
		symlinks = false
		t.Logf("sin enlaces simbólicos: %v", err)
	}
	if symlinks {
		_ = os.Symlink(filepath.Join(ws, "privado.md"), filepath.Join(base, "fuga-ws.md"))
		_ = os.Symlink(t.TempDir(), filepath.Join(base, "dir-fuga"))
		_ = os.Symlink("boxes.md", filepath.Join(base, "references", "alias.md")) // un enlace que se queda dentro es válido
	}

	// SKILL.md: por nombre y por carpeta, con la lista de archivos.
	for _, name := range []string{"alpha", " alpha "} {
		out := tb.must(t, "read_skill", map[string]any{"name": name})
		if !strings.Contains(out.Text, "Contenido de la skill.") {
			t.Errorf("%q: %q", name, truncate(out.Text))
		}
		for _, f := range []string{"references/boxes.md", "templates/box.scad", "references/a b/c.md"} {
			if !strings.Contains(out.Text, f) {
				t.Errorf("%q: falta %s en la lista de archivos", name, f)
			}
		}
		if strings.Contains(out.Text, "dentro.md") || strings.Contains(out.Text, "fuga") {
			t.Errorf("la lista incluye archivos ocultos o enlaces: %q", out.Text)
		}
	}
	// Un archivo de la skill.
	if out := tb.must(t, "read_skill", map[string]any{"name": "alpha", "file": "references/boxes.md"}); out.Text != "# Cajas\n" {
		t.Errorf("file: %q", out.Text)
	}
	if out := tb.must(t, "read_skill", map[string]any{"name": "alpha", "file": "references/a b/c.md"}); out.Text != "con espacio" {
		t.Errorf("file con espacio: %q", out.Text)
	}
	if symlinks {
		if out := tb.must(t, "read_skill", map[string]any{"name": "alpha", "file": "references/alias.md"}); out.Text != "# Cajas\n" {
			t.Errorf("enlace interno: %q", out.Text)
		}
	}
	// 256 KB justos entran; uno más, no.
	if out := tb.must(t, "read_skill", map[string]any{"name": "alpha", "file": "justo.md"}); len(out.Text) != maxSkillFileBytes {
		t.Errorf("justo: %d bytes", len(out.Text))
	}
	if out := tb.fails(t, "read_skill", map[string]any{"name": "alpha", "file": "grande.md"}); !strings.Contains(out.Text, "too large") {
		t.Errorf("grande: %q", out.Text)
	}
	if out := tb.fails(t, "read_skill", map[string]any{"name": "alpha", "file": "binario.bin"}); !strings.Contains(out.Text, "not a text file") {
		t.Errorf("binario: %q", out.Text)
	}

	// Rutas hostiles: nada de lo que hay afuera puede salir.
	hostile := []string{
		"../../../../privado.md", "../SKILL.md", "references/../../privado.md", "/etc/passwd", "/privado.md",
		`..\privado.md`, `references\boxes.md`, "C:/Windows/win.ini", "C:\\Windows\\win.ini", "c:boxes.md", "references//boxes.md",
		".", "./SKILL.md", "references/./boxes.md", "references/", "references", ".oculto/../SKILL.md", "SKILL.md\x00.txt",
		"no-existe.md",
	}
	if symlinks {
		hostile = append(hostile, "fuga.md", "fuga-ws.md", "dir-fuga", "dir-fuga/x.md")
	}
	for _, f := range hostile {
		out, err := tb.Call(context.Background(), "read_skill", map[string]any{"name": "alpha", "file": f})
		if err != nil || !out.IsError {
			t.Errorf("file=%q debía rechazarse: %+v %v", f, truncate(out.Text), err)
			continue
		}
		if strings.Contains(out.Text, "TOP-SECRET") || strings.Contains(out.Text, "DEL-WORKSPACE") {
			t.Errorf("file=%q filtró contenido de afuera: %q", f, out.Text)
		}
	}
	// El nombre de la skill tampoco arma una ruta.
	for _, n := range []string{"", "..", "../alpha", "alpha/../alpha", "/etc", ".claude/skills/alpha", `..\alpha`, "no-existe", "ALPHA"} {
		out, err := tb.Call(context.Background(), "read_skill", map[string]any{"name": n})
		if err != nil || !out.IsError {
			t.Errorf("name=%q debía rechazarse: %+v %v", n, truncate(out.Text), err)
		}
	}
	// Una skill que es un enlace a una carpeta de afuera no se puede leer.
	if symlinks {
		outDir := t.TempDir()
		_ = os.WriteFile(filepath.Join(outDir, "SKILL.md"), []byte("---\nname: ajena\n---\nTOP-SECRET"), 0o644)
		if os.Symlink(outDir, filepath.Join(ws, ".claude", "skills", "ajena")) == nil {
			if out, _ := tb.Call(context.Background(), "read_skill", map[string]any{"name": "ajena"}); !out.IsError || strings.Contains(out.Text, "TOP-SECRET") {
				t.Errorf("skill enlazada afuera: %+v", out)
			}
		}
	}
}

// --- referencias ------------------------------------------------------------

func writeASCIISTL(t *testing.T, path string) {
	t.Helper()
	// Cubo de 20×10×5 como STL ASCII: lo leen mesh y raster.
	v := [][3]float64{{0, 0, 0}, {20, 0, 0}, {20, 10, 0}, {0, 10, 0}, {0, 0, 5}, {20, 0, 5}, {20, 10, 5}, {0, 10, 5}}
	tr := [][3]int{{0, 3, 2}, {0, 2, 1}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2}, {0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5}}
	var b strings.Builder
	b.WriteString("solid t\n")
	for _, t := range tr {
		b.WriteString("facet normal 0 0 0\n outer loop\n")
		for _, i := range t {
			b.WriteString("  vertex " + ftoa(v[i][0]) + " " + ftoa(v[i][1]) + " " + ftoa(v[i][2]) + "\n")
		}
		b.WriteString(" endloop\nendfacet\n")
	}
	b.WriteString("endsolid t\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func TestToolboxInspectReference(t *testing.T) {
	tb, ws := newToolbox(t, true)
	src := filepath.Join(t.TempDir(), "Pieza Ñ.stl")
	writeASCIISTL(t, src)
	rel, err := refmodel.CopyToRefs(ws, src)
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range []string{rel, strings.TrimPrefix(rel, "refs/")} {
		out := tb.must(t, "inspect_reference", map[string]any{"file": file, "view": "top"})
		if len(out.Images) != 1 || !isPNG(out.Images[0].Data) {
			t.Fatalf("%s: imagen: %+v", file, out.Images)
		}
		for _, want := range []string{rel, "X 20.00", "Y 10.00", "Z 5.00", "Watertight", `import("` + rel + `")`, "STL"} {
			if !strings.Contains(out.Text, want) {
				t.Errorf("%s: falta %q en %q", file, want, out.Text)
			}
		}
		if strings.Contains(out.Text, "iVBOR") {
			t.Error("el texto lleva la imagen en base64")
		}
	}
	// En español el informe sale en español.
	i18n.SetLang("es")
	if out := tb.must(t, "inspect_reference", map[string]any{"file": rel}); !strings.Contains(out.Text, "Malla cerrada") {
		t.Errorf("es: %q", out.Text)
	}
	i18n.SetLang("en")

	// Un 3MF de dos colores: partes y el aviso del import.
	two := filepath.Join(ws, "refs", "colores.3mf")
	write3MFTwoColors(t, two)
	out := tb.must(t, "inspect_reference", map[string]any{"file": "refs/colores.3mf"})
	for _, want := range []string{"Parts (2)", "#FF0000", "#0000FF", "NOT reliable with a 3MF"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("3mf: falta %q en %q", want, out.Text)
		}
	}

	// Rutas hostiles y casos de error.
	secret := filepath.Join(ws, "secreto.stl")
	writeASCIISTL(t, secret)
	outside := filepath.Join(t.TempDir(), "afuera.stl")
	writeASCIISTL(t, outside)
	symlinks := os.Symlink(outside, filepath.Join(ws, "refs", "enlace.stl")) == nil
	_ = os.WriteFile(filepath.Join(ws, "refs", "nota.txt"), []byte("hola"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "refs", "roto.stl"), []byte("solid x\nfacet"), 0o644)
	bad := []string{
		"", "../secreto.stl", "refs/../secreto.stl", "refs/../../secreto.stl", "/etc/passwd", secret, outside, `..\secreto.stl`,
		"C:\\x.stl", "refs/sub/x.stl", "nota.txt", "no-existe.stl", "refs/", ".", "..", ".oculto.stl", "roto.stl",
	}
	if symlinks {
		bad = append(bad, "enlace.stl")
	}
	for _, f := range bad {
		out, err := tb.Call(context.Background(), "inspect_reference", map[string]any{"file": f})
		if err != nil || !out.IsError || len(out.Images) != 0 {
			t.Errorf("file=%q debía rechazarse: %+v %v", f, truncate(out.Text), err)
		}
	}
	tb.fails(t, "inspect_reference", map[string]any{"file": rel, "view": "diagonal"})
	// Sin refs/ en el workspace.
	if err := os.RemoveAll(filepath.Join(ws, "refs")); err != nil {
		t.Fatal(err)
	}
	if out := tb.fails(t, "inspect_reference", map[string]any{"file": "x.stl"}); !strings.Contains(out.Text, "refs") {
		t.Errorf("sin refs/: %q", out.Text)
	}
}

func write3MFTwoColors(t *testing.T, dest string) {
	t.Helper()
	box := func(x0, x1, z1 float64) ([][3]float64, [][3]int32) {
		v := [][3]float64{{x0, 0, 0}, {x1, 0, 0}, {x1, 10, 0}, {x0, 10, 0}, {x0, 0, z1}, {x1, 0, z1}, {x1, 10, z1}, {x0, 10, z1}}
		tr := [][3]int32{{0, 3, 2}, {0, 2, 1}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2}, {0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5}}
		return v, tr
	}
	v1, t1 := box(0, 10, 10)
	v2, t2 := box(20, 30, 5)
	parts := []threemf.Part{
		{Name: "Color 1", Color: threemf.RGB{0xFF, 0, 0}, Extruder: 1, Vertices: v1, Triangles: t1},
		{Name: "Color 2", Color: threemf.RGB{0, 0, 0xFF}, Extruder: 2, Vertices: v2, Triangles: t2},
	}
	if err := threemf.WriteFile(dest, parts, threemf.WriteOptions{Title: "dos colores"}); err != nil {
		t.Fatal(err)
	}
}
