package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
	"scadlab/backend/clichat"
	"scadlab/backend/store"
	"scadlab/backend/turnmark"
)

func scadFiles(t *testing.T, ws string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(ws, "*.scad"))
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range files {
		files[i] = filepath.Base(f)
	}
	return files
}

const srcBigger = "color(\"red\") cube([40, 10, 5]);\n"

// update_checkpoint edita el archivo en el lugar: ningún .scad nuevo, la cabecera
// intacta, la versión anterior en el historial, y devuelve lint y render del
// resultado.
func TestUpdateCheckpointEditsInPlace(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	base := tb.create(t, "caja", srcCube, nil)
	other := tb.create(t, "otra", srcCube, nil)
	tb.mustJSON(t, "set_active_parent", map[string]any{"file": other})
	active := checkpoint.ActiveParent(ws)
	before := scadFiles(t, ws)
	oldCode, _ := os.ReadFile(filepath.Join(ws, base))

	got := tb.mustJSON(t, "update_checkpoint", map[string]any{"file": base, "source": srcBigger, "note": "la hago más larga"})
	if got["file"] != base || got["updated"] != true || got["unchanged"] != false {
		t.Fatalf("resultado: %v", got)
	}
	if got["description"] != "la hago más larga" || got["parent"] == nil {
		t.Errorf("descripción/padre: %v", got)
	}
	verID, _ := got["version"].(string)
	if verID == "" {
		t.Errorf("versión: %v", got["version"])
	}
	if lint, ok := got["lint"].([]any); !ok {
		t.Errorf("lint debe ser una lista: %v", got["lint"])
	} else {
		for _, d := range lint {
			if m, _ := d.(map[string]any); m["severity"] == "error" {
				t.Errorf("lint con un error: %v", m)
			}
		}
	}
	r, _ := got["render"].(map[string]any)
	if r["ok"] != true || r["status"] != "ok" || r["fitsBuildVolume"] != true {
		t.Fatalf("render: %v", r)
	}
	if mm, _ := r["mesh"].(map[string]any); mm == nil || mm["watertight"] != true {
		t.Errorf("malla: %v", r["mesh"])
	}

	// En el disco: mismo nombre, sin archivos nuevos, cabecera con el desc nuevo y su padre.
	if after := scadFiles(t, ws); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("update_checkpoint no crea .scad: %v -> %v", before, after)
	}
	raw, _ := os.ReadFile(filepath.Join(ws, base))
	if !strings.HasPrefix(string(raw), `// @checkpoint parent= desc="la hago más larga"`) || !strings.Contains(string(raw), "cube([40, 10, 5])") {
		t.Errorf("archivo: %q", raw)
	}
	// El respaldo es lo que había.
	hist, err := checkpoint.History(ws, base)
	if err != nil || len(hist) != 1 || hist[0].ID != verID || hist[0].Reason != "ai" || hist[0].Tool != "update_checkpoint" || hist[0].Note != "la hago más larga" {
		t.Fatalf("historial: %+v %v", hist, err)
	}
	if v, _ := checkpoint.ReadVersion(ws, base, hist[0].ID); v != string(oldCode) {
		t.Errorf("respaldo: %q", v)
	}
	if !checkpoint.EditedByAI(ws, base) {
		t.Error("el archivo debe figurar editado por la IA")
	}
	// No toca el padre activo ni los demás.
	if checkpoint.ActiveParent(ws) != active || active != other {
		t.Errorf("padre activo: %q (era %q)", checkpoint.ActiveParent(ws), active)
	}
	if h, _ := checkpoint.History(ws, other); len(h) != 0 {
		t.Error("otro archivo ganó historial")
	}
	// Es de escritura de verdad para la interfaz: refresca el archivo, no el árbol.
	if !CreatesCheckpoint("update_checkpoint") || !UpdatesInPlace("update_checkpoint", `{"updated":true}`) || UpdatesInPlace("create_checkpoint", `{"updated":true}`) {
		t.Error("effects.go no clasifica update_checkpoint")
	}
}

// Idéntico: no escribe ni respalda (y aun así devuelve el render).
func TestUpdateCheckpointUnchanged(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	base := tb.create(t, "caja", srcCube, nil)
	raw, _ := os.ReadFile(filepath.Join(ws, base))
	for _, src := range []string{string(raw), srcCube} { // con y sin su cabecera
		got := tb.mustJSON(t, "update_checkpoint", map[string]any{"file": base, "source": src})
		if got["unchanged"] != true || got["version"] != nil {
			t.Fatalf("resultado: %v", got)
		}
		if r, _ := got["render"].(map[string]any); r["ok"] != true {
			t.Errorf("render: %v", r)
		}
	}
	if h, _ := checkpoint.History(ws, base); len(h) != 0 {
		t.Errorf("un cambio nulo no respalda: %+v", h)
	}
}

// Un render roto no es un error de la herramienta (el archivo SÍ cambió): trae los
// diagnósticos con su línea y el consejo de corregir con update_checkpoint; el
// intento roto queda en el historial y la corrección lo limpia todo.
func TestUpdateCheckpointBrokenRenderThenFix(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	base := tb.create(t, "caja", srcCube, nil)

	got := tb.mustJSON(t, "update_checkpoint", map[string]any{"file": base, "source": srcBroken})
	r, _ := got["render"].(map[string]any)
	if r["ok"] != false || r["status"] != "error" {
		t.Fatalf("render: %v", r)
	}
	ds, _ := r["diagnostics"].([]any)
	foundLine := false
	for _, d := range ds {
		if m, _ := d.(map[string]any); m["severity"] == "error" && m["line"] != nil && m["code"] != nil {
			foundLine = true
		}
	}
	if !foundLine {
		t.Errorf("el render roto debe traer la línea y el código: %v", ds)
	}
	if msg, _ := r["message"].(string); !strings.Contains(msg, "edit_checkpoint") || !strings.Contains(msg, base) {
		t.Errorf("consejo: %q", msg)
	}
	if got["updated"] != true || got["version"] == nil {
		t.Errorf("el archivo sí se actualizó: %v", got)
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, "RENDER_ERRORS.md")); !strings.Contains(string(raw), "update_checkpoint") || !strings.Contains(string(raw), base) {
		t.Errorf("RENDER_ERRORS.md: %q", raw)
	}

	fixed := tb.mustJSON(t, "update_checkpoint", map[string]any{"file": base, "source": srcBigger})
	if r, _ := fixed["render"].(map[string]any); r["ok"] != true {
		t.Fatalf("la corrección debe renderizar: %v", r)
	}
	if _, err := os.Stat(filepath.Join(ws, "RENDER_ERRORS.md")); err == nil {
		t.Error("un render bueno borra RENDER_ERRORS.md")
	}
	hist, _ := checkpoint.History(ws, base)
	if len(hist) != 2 {
		t.Fatalf("historial: %+v", hist)
	}
	if broken, _ := checkpoint.ReadVersion(ws, base, hist[0].ID); !strings.Contains(broken, "intento roto") {
		t.Errorf("el intento roto debe quedar en el historial: %q", broken)
	}
	if len(scadFiles(t, ws)) != 1 {
		t.Errorf("ningún .scad nuevo: %v", scadFiles(t, ws))
	}
}

func TestUpdateCheckpointRejects(t *testing.T) {
	tb, ws := newToolbox(t, false)
	base := tb.create(t, "caja", srcCube, nil)
	before, _ := os.ReadFile(filepath.Join(ws, base))
	big := strings.Repeat("a", maxSourceBytes+1)

	for name, args := range map[string]map[string]any{
		"sin file":       {"source": srcCube},
		"file en blanco": {"file": "  ", "source": srcCube},
		"sin source":     {"file": base},
		"source vacío":   {"file": base, "source": " \n"},
		"solo cabecera":  {"file": base, "source": "// @checkpoint parent= desc=\"x\"\n"},
		"demasiado":      {"file": base, "source": big},
		"no existe":      {"file": "009-no-existe.scad", "source": srcCube},
		"ruta":           {"file": "../" + base, "source": srcCube},
		"absoluta":       {"file": filepath.Join(ws, base), "source": srcCube},
		"subcarpeta":     {"file": "sub/" + base, "source": srcCube},
		"sin extensión":  {"file": "001-caja", "source": srcCube},
	} {
		if out := tb.fails(t, "update_checkpoint", args); out.Text == "" {
			t.Errorf("%s: sin mensaje", name)
		}
	}
	after, _ := os.ReadFile(filepath.Join(ws, base))
	if string(after) != string(before) {
		t.Errorf("un rechazo tocó el archivo: %q", after)
	}
	if h, _ := checkpoint.History(ws, base); len(h) != 0 {
		t.Errorf("un rechazo dejó historial: %+v", h)
	}
	if got := scadFiles(t, ws); len(got) != 1 {
		t.Errorf("archivos: %v", got)
	}
}

// Un archivo que solo está en la papelera no se edita: el error lo dice y sugiere
// restore_checkpoint (con el nombre que hay que pasar).
func TestUpdateCheckpointOfATrashedFile(t *testing.T) {
	tb, ws := newToolbox(t, false)
	a := tb.create(t, "caja", srcCube, nil)
	tb.create(t, "tapa", srcCube, map[string]any{"parent": a})
	trashed := tb.mustJSON(t, "trash_checkpoint", map[string]any{"file": a})["trashed"].([]any)[0].(string)

	out := tb.fails(t, "update_checkpoint", map[string]any{"file": a, "source": srcBigger})
	if !strings.Contains(out.Text, "restore_checkpoint") || !strings.Contains(out.Text, trashed) {
		t.Errorf("el error debe sugerir restore_checkpoint con %s: %q", trashed, out.Text)
	}
	if _, err := os.Stat(filepath.Join(ws, a)); err == nil {
		t.Error("update_checkpoint resucitó el archivo")
	}
	tb.mustJSON(t, "restore_checkpoint", map[string]any{"trashed_name": trashed})
	tb.mustJSON(t, "update_checkpoint", map[string]any{"file": a, "source": srcBigger})
}

// El tope del historial sale de Configuración (store.Settings.HistoryMaxVersions).
func TestUpdateCheckpointUsesTheConfiguredCap(t *testing.T) {
	tb, ws, st, _ := registeredToolbox(t)
	s, err := st.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.HistoryMaxVersions != store.DefaultHistoryVersions {
		t.Errorf("tope de fábrica: %d", s.HistoryMaxVersions)
	}
	s.HistoryMaxVersions = 2
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	base := tb.create(t, "caja", srcCube, nil)
	for i := 0; i < 4; i++ {
		src := "color(\"red\") cube([" + strings.Repeat("1", i+1) + ", 10, 5]);\n"
		tb.mustJSON(t, "update_checkpoint", map[string]any{"file": base, "source": src})
	}
	if h, _ := checkpoint.History(ws, base); len(h) != 2 {
		t.Errorf("con tope 2 quedaron %d", len(h))
	}
}

// «Solo consulta» (la marca del turno) la rechaza sin tocar el archivo; en
// «Agente» pasa. clichat la tiene en «Agente» y no en «Solo consulta».
func TestUpdateCheckpointIsAgentOnly(t *testing.T) {
	c := setup(t, nil)
	base := c.create("caja", srcCube, nil)
	before := c.readWs(base)

	end, err := turnmark.Begin(c.Ws, turnmark.Ask)
	if err != nil {
		t.Fatal(err)
	}
	r := c.tool("update_checkpoint", map[string]any{"file": base, "source": srcBigger})
	if !r.IsError || !strings.Contains(r.Text, "“update_checkpoint” tool changes the project") {
		t.Errorf("en consulta: isError=%v %q", r.IsError, truncate(r.Text))
	}
	if c.readWs(base) != before {
		t.Error("el rechazo tocó el archivo")
	}
	if c.exists(".scadlab/history") {
		t.Error("el rechazo creó historial")
	}
	end()

	needOpenSCAD(t)
	out := c.mustOK("update_checkpoint", map[string]any{"file": base, "source": srcBigger})
	if out["updated"] != true || c.readWs(base) == before {
		t.Errorf("en Agente debía editar: %v", out)
	}

	inWrite, inRead := false, false
	for _, n := range clichat.WriteTools() {
		inWrite = inWrite || n == "update_checkpoint"
	}
	for _, n := range clichat.ReadTools() {
		inRead = inRead || n == "update_checkpoint"
	}
	if !inWrite || inRead {
		t.Errorf("clichat: write=%v read=%v", inWrite, inRead)
	}
}

// El catálogo la expone con su esquema y la anotación de escritura, y Hermes la
// recibe por la misma caja.
func TestUpdateCheckpointInTheCatalog(t *testing.T) {
	tb, _ := newToolbox(t, false)
	var spec *ToolSpec
	for _, s := range tb.Tools() {
		if s.Name == "update_checkpoint" {
			s := s
			spec = &s
		}
	}
	if spec == nil {
		t.Fatal("falta update_checkpoint en la caja de Hermes")
	}
	req, _ := spec.InputSchema["required"].([]string)
	props, _ := spec.InputSchema["properties"].(map[string]any)
	if strings.Join(req, ",") != "file,source" || props["note"] == nil {
		t.Errorf("esquema: %v", spec.InputSchema)
	}
	for _, want := range []string{"IN PLACE", "history", "read_checkpoint", "something new, a variant"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("descripción sin %q: %s", want, spec.Description)
		}
	}
	for _, tool := range tb.e.tools() {
		if tool.Name == "update_checkpoint" && tool.Annotations["readOnlyHint"] != false {
			t.Errorf("anotaciones de escritura: %v", tool.Annotations)
		}
	}
	// El texto viejo no sobrevive en ninguna descripción del catálogo.
	for _, s := range tb.Tools() {
		for _, bad := range []string{"immutable", "never by editing this one", "never edit"} {
			if strings.Contains(s.Description, bad) {
				t.Errorf("%s dice %q: %s", s.Name, bad, s.Description)
			}
		}
	}
}
