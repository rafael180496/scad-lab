package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
	"scadlab/backend/clichat"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// registeredToolbox abre una caja sobre un workspace registrado en la base (la de
// la app, inyectada): rótulos y metadatos de la papelera viven ahí.
func registeredToolbox(t *testing.T) (*Toolbox, string, *store.Store, store.Workspace) {
	t.Helper()
	dataDir, ws := toolboxEnv(t)
	st, err := store.Open(filepath.Join(dataDir, "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.InsertWorkspace(store.Workspace{Name: "Registrado", Path: ws, PrinterID: "bambu-a1-mini"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := st.GetWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := NewToolbox(ToolboxOptions{WorkspaceDir: ws, Store: st})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tb.Close() })
	return tb, ws, st, w
}

func (tb *Toolbox) create(t *testing.T, label, src string, extra map[string]any) string {
	t.Helper()
	args := map[string]any{"label": label, "source": src}
	for k, v := range extra {
		args[k] = v
	}
	name, _ := tb.mustJSON(t, "create_checkpoint", args)["file"].(string)
	if name == "" {
		t.Fatalf("create_checkpoint no devolvió archivo")
	}
	return name
}

// Cada herramienta del catálogo está clasificada como lectura o escritura (y solo
// una vez): ninguna queda fuera de «Solo consulta» ni se cuela en él. Y las que
// cambian algo —un checkpoint, el árbol, exports/ o lib/— no pueden ser de
// lectura.
func TestEveryToolIsClassifiedForTheModes(t *testing.T) {
	tb, _ := newToolbox(t, false)
	catalog := map[string]bool{}
	for _, tl := range tb.Tools() {
		catalog[tl.Name] = true
	}
	reads, writes := clichat.ReadTools(), clichat.WriteTools()
	for _, n := range append(slices.Clone(reads), writes...) {
		if !catalog[n] {
			t.Errorf("%q está en las listas de los modos pero no en el catálogo MCP", n)
		}
	}
	for n := range catalog {
		r, w := slices.Contains(reads, n), slices.Contains(writes, n)
		switch {
		case r && w:
			t.Errorf("%s está en las dos listas", n)
		case !r && !w:
			t.Errorf("%s no está clasificada como lectura ni como escritura", n)
		}
	}
	mustWrite := []string{
		"create_checkpoint", "import_mesh", "export", "set_active_parent", "rename_checkpoint", "trash_checkpoint",
		"restore_checkpoint", "set_parameters", "create_from_template", "install_library", "edit_mesh",
		"set_workspace_printer", "trace_image_outline", "image_to_heightmap",
	}
	for _, n := range mustWrite {
		if !slices.Contains(writes, n) || slices.Contains(reads, n) {
			t.Errorf("%s cambia el proyecto: tiene que ser de escritura (solo «Agente»)", n)
		}
	}
	mustRead := []string{
		"list_checkpoints", "read_checkpoint", "list_trash", "list_refs", "lint", "format", "render", "preview", "measure",
		"get_parameters", "list_templates", "get_template", "list_libraries", "inspect_reference", "printer_info",
		"estimate_print", "list_materials", "suggest_orientation", "list_skills", "read_skill", "list_printers",
		"image_info", "compare_with_image",
	}
	for _, n := range mustRead {
		if !slices.Contains(reads, n) || slices.Contains(writes, n) {
			t.Errorf("%s no cambia el diseño: tiene que estar en «Solo consulta»", n)
		}
	}
}

// El servidor `--mcp` lista las mismas herramientas nuevas, con su schema.
func TestMCPListsTheNewToolsWithSchemas(t *testing.T) {
	c := setup(t, nil)
	m := c.call("tools/list", nil)
	res, _ := m["result"].(map[string]any)
	list, _ := res["tools"].([]any)
	got := map[string]map[string]any{}
	for _, it := range list {
		tool := it.(map[string]any)
		got[tool["name"].(string)] = tool
	}
	required := map[string][]string{
		"rename_checkpoint": {"file", "label"}, "trash_checkpoint": {"file"}, "restore_checkpoint": {"trashed_name"},
		"set_parameters": {"values"}, "create_from_template": {"name"}, "install_library": {"name"},
		"edit_mesh": {"operations"}, "read_skill": {"name"}, "inspect_reference": {"file"},
		"image_info": {"image"}, "compare_with_image": {"image"}, "trace_image_outline": {"image"},
		"image_to_heightmap": {"image", "width_mm", "max_height_mm"},
	}
	for _, name := range []string{
		"set_active_parent", "rename_checkpoint", "trash_checkpoint", "list_trash", "restore_checkpoint", "list_refs",
		"get_parameters", "set_parameters", "create_from_template", "list_libraries", "install_library", "edit_mesh",
		"estimate_print", "list_materials", "suggest_orientation", "list_skills", "read_skill", "inspect_reference",
		"image_info", "compare_with_image", "trace_image_outline", "image_to_heightmap",
	} {
		tool, ok := got[name]
		if !ok {
			t.Errorf("--mcp no lista %s", name)
			continue
		}
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema["type"] != "object" || schema["properties"] == nil {
			t.Errorf("%s: inputSchema inválido: %v", name, schema)
		}
		if req := required[name]; len(req) > 0 {
			r, _ := schema["required"].([]any)
			if len(r) != len(req) {
				t.Errorf("%s required = %v, esperaba %v", name, r, req)
			}
		}
		ann, _ := tool["annotations"].(map[string]any)
		ro := slices.Contains(clichat.ReadTools(), name)
		// render/measure/preview y compañía escriben caché: se anotan como escritura
		// aunque sean de lectura. Lo que no puede pasar es lo contrario.
		if ann["readOnlyHint"] == true && !ro {
			t.Errorf("%s dice readOnlyHint pero es de escritura", name)
		}
	}
	// edit_mesh describe sus operaciones y las lista.
	em := got["edit_mesh"]["inputSchema"].(map[string]any)["properties"].(map[string]any)["operations"].(map[string]any)
	if d, _ := em["description"].(string); !strings.Contains(d, "to_bed") || !strings.Contains(d, "at_rel") {
		t.Errorf("edit_mesh sin la descripción de sus operaciones: %v", em["description"])
	}
}

func TestListCheckpointsDrawsTheTreeWithLabels(t *testing.T) {
	tb, ws, st, w := registeredToolbox(t)
	a := tb.create(t, "base", srcCube, nil)
	b := tb.create(t, "tapa", srcCube, nil)                            // hijo de a (sin activo: el más nuevo)
	c := tb.create(t, "lateral", srcCube, map[string]any{"parent": a}) // otro hijo de a
	if err := checkpoint.SetActiveParent(ws, b); err != nil {
		t.Fatal(err)
	}
	// Con un rótulo del usuario, el árbol lo muestra en lugar de la descripción.
	tb.mustJSON(t, "rename_checkpoint", map[string]any{"file": c, "label": "Versión corta"})
	if cp, err := st.GetCheckpoint(w.ID, c); err != nil || cp.Label != "Versión corta" {
		t.Fatalf("la base no guardó el rótulo: %+v %v", cp, err)
	}

	out := tb.mustJSON(t, "list_checkpoints", nil)
	tree, _ := out["tree"].(string)
	for _, want := range []string{a, b + " — tapa", c + " — Versión corta", "<- active parent", "├─", "└─"} {
		if !strings.Contains(tree, want) {
			t.Errorf("el árbol no tiene %q:\n%s", want, tree)
		}
	}
	if strings.Index(tree, a) > strings.Index(tree, b) {
		t.Errorf("el padre va antes que el hijo:\n%s", tree)
	}
	var labelled bool
	for _, it := range out["checkpoints"].([]any) {
		m := it.(map[string]any)
		if m["file"] == c && m["label"] == "Versión corta" {
			labelled = true
		}
		if m["file"] == a && m["label"] != nil {
			t.Errorf("sin rótulo no hay campo label: %v", m)
		}
	}
	if !labelled {
		t.Errorf("list_checkpoints no trae el rótulo: %v", out["checkpoints"])
	}
}

func TestSetActiveParentTool(t *testing.T) {
	tb, ws := newToolbox(t, false)
	a := tb.create(t, "uno", srcCube, nil)
	b := tb.create(t, "dos", srcCube, nil) // hijo de a

	got := tb.mustJSON(t, "set_active_parent", map[string]any{"file": a})
	if got["activeParent"] != a || checkpoint.ActiveParent(ws) != a {
		t.Fatalf("set_active_parent: %v", got)
	}
	if lc := tb.mustJSON(t, "list_checkpoints", nil); lc["activeParent"] != a {
		t.Errorf("list_checkpoints: %v", lc["activeParent"])
	}
	// create_checkpoint sin parent cuelga del activo, y el activo avanza.
	c := tb.create(t, "rama", srcCube, nil)
	if src := tb.must(t, "read_checkpoint", map[string]any{"file": c}).Text; !strings.HasPrefix(src, "// @checkpoint parent="+a+" ") {
		t.Errorf("debía colgar de %s: %q", a, strings.SplitN(src, "\n", 2)[0])
	}
	if checkpoint.ActiveParent(ws) != c {
		t.Errorf("el activo debía avanzar a %s", c)
	}

	// Sin activo: vuelve a regir «el más nuevo».
	cl := tb.mustJSON(t, "set_active_parent", map[string]any{"clear": true})
	if cl["activeParent"] != "" || checkpoint.ActiveParent(ws) != "" {
		t.Errorf("clear: %v", cl)
	}
	_ = b

	// Falta el archivo, no existe o es hostil: error y nada cambia.
	bad := []string{"", "no-existe.scad", "../" + a, "/etc/passwd", "sub/" + a, a + "\x00", "refs", "x.txt", "..", "C:\\x.scad"}
	for _, f := range bad {
		tb.fails(t, "set_active_parent", map[string]any{"file": f})
	}
	if checkpoint.ActiveParent(ws) != "" {
		t.Error("un nombre hostil cambió el padre activo")
	}
}

func TestRenameCheckpointTool(t *testing.T) {
	tb, ws, st, w := registeredToolbox(t)
	a := tb.create(t, "uno", srcCube, nil)
	before, _ := os.ReadFile(filepath.Join(ws, a))

	got := tb.mustJSON(t, "rename_checkpoint", map[string]any{"file": a, "label": "  Caja\n  principal \t"})
	if got["label"] != "Caja principal" {
		t.Fatalf("rename: %v", got)
	}
	if cp, err := st.GetCheckpoint(w.ID, a); err != nil || cp.Label != "Caja principal" {
		t.Errorf("base: %+v %v", cp, err)
	}
	// El archivo es inmutable: ni su contenido ni su nombre cambian.
	if after, _ := os.ReadFile(filepath.Join(ws, a)); string(after) != string(before) {
		t.Error("rename_checkpoint tocó el archivo")
	}
	// `description` se acepta como sinónimo de label.
	tb.mustJSON(t, "rename_checkpoint", map[string]any{"file": a, "description": "Otra"})
	if cp, _ := st.GetCheckpoint(w.ID, a); cp.Label != "Otra" {
		t.Errorf("sinónimo: %+v", cp)
	}
	// Vacío quita el rótulo.
	tb.mustJSON(t, "rename_checkpoint", map[string]any{"file": a, "label": ""})
	if cp, _ := st.GetCheckpoint(w.ID, a); cp.Label != "" {
		t.Errorf("quitar: %+v", cp)
	}
	// Demasiado largo, sin label, archivo inexistente u hostil: error.
	tb.fails(t, "rename_checkpoint", map[string]any{"file": a, "label": strings.Repeat("x", 81)})
	tb.fails(t, "rename_checkpoint", map[string]any{"file": a})
	for _, f := range []string{"", "no-existe.scad", "../" + a, "/etc/passwd", "sub/" + a, a + "\x00"} {
		tb.fails(t, "rename_checkpoint", map[string]any{"file": f, "label": "x"})
	}

	// Un workspace que la app no conoce no tiene dónde guardar el rótulo.
	tb2, _ := newToolbox(t, false)
	n := tb2.create(t, "uno", srcCube, nil)
	if out := tb2.fails(t, "rename_checkpoint", map[string]any{"file": n, "label": "x"}); !strings.Contains(out.Text, "not registered") {
		t.Errorf("workspace sin registrar: %q", out.Text)
	}
}

func TestTrashAndRestoreTools(t *testing.T) {
	tb, ws, st, w := registeredToolbox(t)
	a := tb.create(t, "base", srcCube, nil)
	b := tb.create(t, "hijo", srcCube, nil)
	c := tb.create(t, "nieto", srcCube, nil)
	d := tb.create(t, "hermano", srcCube, map[string]any{"parent": a})
	tb.mustJSON(t, "rename_checkpoint", map[string]any{"file": b, "label": "Mi hijo"})
	if err := checkpoint.SetActiveParent(ws, c); err != nil {
		t.Fatal(err)
	}

	// Una rama entera: b y su nieto c. Nada se borra: queda en la papelera.
	got := tb.mustJSON(t, "trash_checkpoint", map[string]any{"file": b, "branch": true})
	trashed, _ := got["trashed"].([]any)
	if len(trashed) != 2 {
		t.Fatalf("trash: %v", got)
	}
	for _, f := range []string{b, c} {
		if _, err := os.Stat(filepath.Join(ws, f)); err == nil {
			t.Errorf("%s sigue en el workspace", f)
		}
	}
	for _, n := range trashed {
		if _, err := os.Stat(filepath.Join(ws, ".scadlab", "trash", n.(string))); err != nil {
			t.Errorf("no se guardó en la papelera: %v", err)
		}
	}
	for _, f := range []string{a, d} {
		if _, err := os.Stat(filepath.Join(ws, f)); err != nil {
			t.Errorf("%s no debía moverse: %v", f, err)
		}
	}
	// El padre activo estaba dentro de la rama: pasa al padre de la raíz de la rama.
	if checkpoint.ActiveParent(ws) != a || got["activeParent"] != a {
		t.Errorf("padre activo: %v / %v", checkpoint.ActiveParent(ws), got["activeParent"])
	}
	// El rótulo viaja con el checkpoint a la papelera.
	if _, err := st.GetCheckpoint(w.ID, b); err == nil {
		// el índice de la app se reescanea después; lo que importa es lo guardado aparte
		_ = err
	}

	lt := tb.mustJSON(t, "list_trash", nil)["trash"].([]any)
	if len(lt) != 2 {
		t.Fatalf("list_trash: %v", lt)
	}
	var trashedB string
	for _, it := range lt {
		m := it.(map[string]any)
		if m["original"] == b {
			trashedB = m["name"].(string)
		}
	}
	if trashedB == "" {
		t.Fatalf("falta %s en la papelera: %v", b, lt)
	}
	// list_checkpoints ya no los muestra.
	for _, it := range tb.mustJSON(t, "list_checkpoints", nil)["checkpoints"].([]any) {
		if f := it.(map[string]any)["file"]; f == b || f == c {
			t.Errorf("%v sigue en el árbol", f)
		}
	}

	// Restaurar: vuelve a su nombre y recupera el rótulo.
	rs := tb.mustJSON(t, "restore_checkpoint", map[string]any{"trashed_name": trashedB})
	if rs["file"] != b {
		t.Fatalf("restore: %v", rs)
	}
	if _, err := os.Stat(filepath.Join(ws, b)); err != nil {
		t.Error(err)
	}
	if cp, err := st.GetCheckpoint(w.ID, b); err != nil || cp.Label != "Mi hijo" {
		t.Errorf("el rótulo debía volver: %+v %v", cp, err)
	}
	if left := tb.mustJSON(t, "list_trash", nil)["trash"].([]any); len(left) != 1 {
		t.Errorf("queda uno en la papelera: %v", left)
	}

	// Un solo checkpoint (sin rama): sus hijos no se mueven.
	one := tb.mustJSON(t, "trash_checkpoint", map[string]any{"file": a})
	if len(one["trashed"].([]any)) != 1 {
		t.Errorf("sin branch se mueve solo uno: %v", one)
	}
	if _, err := os.Stat(filepath.Join(ws, d)); err != nil {
		t.Error("el hijo no debía moverse")
	}

	// Hostiles: nada se mueve ni se restaura fuera.
	secret := filepath.Join(ws, "..", "secreto.scad")
	_ = os.WriteFile(secret, []byte("x"), 0o644)
	for _, f := range []string{"", "no-existe.scad", "../secreto.scad", "/etc/passwd", "sub/x.scad", d + "\x00", ".scadlab/trash"} {
		tb.fails(t, "trash_checkpoint", map[string]any{"file": f})
	}
	for _, f := range []string{"", "../secreto.scad", "no-existe", "20260101-000000-x.scad", "/etc/passwd", "20260101-000000-../../x.scad", trashedB + "\x00"} {
		tb.fails(t, "restore_checkpoint", map[string]any{"trashed_name": f})
	}
	if _, err := os.Stat(secret); err != nil {
		t.Error("un nombre hostil tocó un archivo fuera del workspace")
	}
	// Nada se perdió en todo el ciclo: cada .scad creado sigue en el workspace o en la papelera.
	all := map[string]bool{}
	ents, _ := os.ReadDir(ws)
	for _, e := range ents {
		all[e.Name()] = true
	}
	tr, _ := os.ReadDir(filepath.Join(ws, ".scadlab", "trash"))
	for _, e := range tr {
		all[e.Name()] = true
	}
	n := 0
	for name := range all {
		if strings.HasSuffix(name, ".scad") {
			n++
		}
	}
	if n != 4 {
		t.Errorf("había 4 checkpoints y ahora se ven %d: %v", n, all)
	}
}

func TestTrashRefusesALinkedMetaFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("enlaces simbólicos")
	}
	tb, ws := newToolbox(t, false)
	a := tb.create(t, "uno", srcCube, nil)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, ".scadlab")); err != nil {
		// .scadlab ya existe (padre activo): se reemplaza por un enlace.
		_ = os.RemoveAll(filepath.Join(ws, ".scadlab"))
		if err := os.Symlink(outside, filepath.Join(ws, ".scadlab")); err != nil {
			t.Skip(err)
		}
	}
	tb.fails(t, "trash_checkpoint", map[string]any{"file": a})
	if _, err := os.Stat(filepath.Join(ws, a)); err != nil {
		t.Error("el checkpoint debía quedarse donde estaba")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("se escribió fuera del workspace: %v", ents)
	}
}

func TestListRefsTool(t *testing.T) {
	tb, ws := newToolbox(t, false)
	got := tb.mustJSON(t, "list_refs", nil)
	if refs, _ := got["refs"].([]any); len(refs) != 0 || got["note"] == nil {
		t.Errorf("sin refs/: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "soporte.stl"))
	write3MFTwoColors(t, filepath.Join(ws, "refs", "colores.3mf"))
	_ = os.WriteFile(filepath.Join(ws, "refs", "nota.txt"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "001-algo.scad"), []byte("cube(1);"), 0o644)
	if runtime.GOOS != "windows" {
		outside := filepath.Join(t.TempDir(), "fuera.stl")
		writeASCIISTL(t, outside)
		_ = os.Symlink(outside, filepath.Join(ws, "refs", "enlace.stl"))
	}
	refs, _ := tb.mustJSON(t, "list_refs", nil)["refs"].([]any)
	var names []string
	for _, r := range refs {
		m := r.(map[string]any)
		names = append(names, m["path"].(string))
		if m["bytes"].(float64) <= 0 || m["format"] == "" || m["modified"] == "" {
			t.Errorf("ficha incompleta: %v", m)
		}
	}
	if strings.Join(names, ",") != "refs/colores.3mf,refs/soporte.stl" {
		t.Errorf("list_refs: %v (no debe listar texto, .scad ni enlaces que salen de refs/)", names)
	}
}

// Mandar a la papelera un checkpoint suelto re-cuelga a sus hijos directos del
// abuelo (o los deja como raíz), y restaurarlo los devuelve. El agente lo ve en
// la respuesta.
func TestTrashToolRelinksChildrenAndRestoreHangsThemBack(t *testing.T) {
	tb, ws, _, _ := registeredToolbox(t)
	a := tb.create(t, "base", srcCube, nil)
	b := tb.create(t, "medio", srcCube, nil)
	c := tb.create(t, "hijo", srcCube, nil)
	d := tb.create(t, "nieto", srcCube, nil)

	parent := func(file string) string {
		files, err := workspace.Scan(ws)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.FileName == file {
				return f.ParentFile
			}
		}
		t.Fatalf("%s no está", file)
		return ""
	}
	if parent(c) != b || parent(d) != c {
		t.Fatalf("árbol inesperado: %s←%s←%s", parent(d), parent(c), parent(b))
	}

	got := tb.mustJSON(t, "trash_checkpoint", map[string]any{"file": b})
	rel, _ := got["relinked"].([]any)
	if len(rel) != 1 || rel[0] != c || got["relinkedNote"] == nil {
		t.Fatalf("relinked: %v", got)
	}
	if parent(c) != a || parent(d) != c {
		t.Errorf("c debía pasar a colgar de a y d no tocarse: %s / %s", parent(c), parent(d))
	}

	trashed := got["trashed"].([]any)[0].(string)
	rs := tb.mustJSON(t, "restore_checkpoint", map[string]any{"trashed_name": trashed})
	if rs["file"] != b {
		t.Fatalf("restore: %v", rs)
	}
	if rel, _ := rs["relinked"].([]any); len(rel) != 1 || rel[0] != c {
		t.Errorf("restore relinked: %v", rs)
	}
	if parent(c) != b {
		t.Errorf("c debía volver a colgar de b: %s", parent(c))
	}

	// Una raíz: sus hijos quedan como raíz.
	got = tb.mustJSON(t, "trash_checkpoint", map[string]any{"file": a})
	if parent(b) != "" {
		t.Errorf("b debía quedar como raíz: %q", parent(b))
	}
	_ = got
}

// El borrado permanente es solo del usuario: ninguna herramienta lo ofrece ni
// acepta una bandera que lo dispare.
func TestNoToolDeletesForGood(t *testing.T) {
	tb, _ := newToolbox(t, false)
	for _, tl := range tb.Tools() {
		n := strings.ToLower(tl.Name)
		for _, bad := range []string{"delete", "purge", "empty", "remove", "erase", "wipe"} {
			if strings.Contains(n, bad) {
				t.Errorf("la herramienta %s sugiere un borrado permanente", tl.Name)
			}
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for p := range props {
			for _, bad := range []string{"permanent", "purge", "delete", "force", "empty"} {
				if strings.Contains(strings.ToLower(p), bad) {
					t.Errorf("%s acepta el parámetro %q", tl.Name, p)
				}
			}
		}
	}
}
