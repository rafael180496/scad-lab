package mcp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// catalogBytes es el peso de ofrecerle estas herramientas a un modelo: el nombre,
// la descripción y el esquema, como los recibe un proveedor con herramientas.
func catalogBytes(t *testing.T, specs []ToolSpec) int {
	t.Helper()
	n := 0
	for _, s := range specs {
		b, err := json.Marshal(map[string]any{"name": s.Name, "description": s.Description, "parameters": s.InputSchema})
		if err != nil {
			t.Fatal(err)
		}
		n += len(b)
	}
	return n
}

func TestEssentialProfile(t *testing.T) {
	tb, _ := newToolbox(t, false)
	full := tb.ToolsFor(ProfileFull)
	if !slices.Equal(toolNames(full), toolNames(tb.Tools())) {
		t.Error("ProfileFull es el catálogo completo")
	}
	if got := toolNames(tb.ToolsFor("otro")); !slices.Equal(got, toolNames(full)) {
		t.Errorf("un perfil desconocido es el completo: %v", got)
	}

	want := []string{"outline_checkpoint", "find_in_checkpoint", "read_checkpoint", "edit_checkpoint", "set_parameters", "get_parameters", "render", "preview", "measure", "create_checkpoint", "list_checkpoints"}
	if got := EssentialTools(); !slices.Equal(got, want) {
		t.Fatalf("EssentialTools: %v", got)
	}
	// EssentialTools devuelve una copia.
	e := EssentialTools()
	e[0] = "otra"
	if EssentialTools()[0] == "otra" {
		t.Error("EssentialTools debe devolver una copia")
	}

	ess := tb.ToolsFor(ProfileEssential)
	if !slices.Equal(toolNames(ess), want) {
		t.Fatalf("herramientas esenciales: %v", toolNames(ess))
	}
	fullByName := map[string]ToolSpec{}
	for _, s := range full {
		fullByName[s.Name] = s
	}
	for _, s := range ess {
		f, ok := fullByName[s.Name]
		if !ok {
			t.Errorf("%s no está en el catálogo completo", s.Name)
			continue
		}
		// Descripción de 1-2 frases, mucho más corta que la completa.
		if len(s.Description) < 30 || len(s.Description) > 420 || len(s.Description) >= len(f.Description) {
			t.Errorf("%s: descripción corta de %d bytes (completa: %d)", s.Name, len(s.Description), len(f.Description))
		}
		// Esquema mínimo: mismo tipo, mismas obligatorias, solo propiedades que existen en el completo.
		if s.InputSchema["type"] != "object" || s.InputSchema["properties"] == nil {
			t.Errorf("%s: esquema inválido: %v", s.Name, s.InputSchema)
		}
		if !slices.Equal(requiredOf(s.InputSchema), requiredOf(f.InputSchema)) {
			t.Errorf("%s: obligatorias %v, el completo %v", s.Name, requiredOf(s.InputSchema), requiredOf(f.InputSchema))
		}
		fprops, _ := f.InputSchema["properties"].(map[string]any)
		for p := range s.InputSchema["properties"].(map[string]any) {
			if _, ok := fprops[p]; !ok {
				t.Errorf("%s: el esquema corto tiene `%s` y el completo no", s.Name, p)
			}
		}
		if bs, bf := catalogBytes(t, []ToolSpec{s}), catalogBytes(t, []ToolSpec{f}); bs >= bf {
			t.Errorf("%s: el perfil esencial no es más corto (%d vs %d bytes)", s.Name, bs, bf)
		}
	}
	// Las herramientas que se ofrecen en el esencial tienen que poder editar y mirar,
	// y ninguna de borrado ni de red.
	for _, n := range []string{"edit_checkpoint", "set_parameters", "outline_checkpoint", "find_in_checkpoint", "read_checkpoint"} {
		if !slices.Contains(toolNames(ess), n) {
			t.Errorf("falta %s en el esencial", n)
		}
	}

	// Call acepta cualquier herramienta del catálogo, esté o no en el perfil.
	if !slices.Contains(toolNames(full), "lint") || slices.Contains(toolNames(ess), "lint") {
		t.Fatal("lint es del completo y no del esencial")
	}
	if out, err := tb.Call(testCtx(), "lint", map[string]any{"source": "cube(1);"}); err != nil || out.IsError {
		t.Errorf("Call(lint) con el perfil esencial: %+v %v", out, err)
	}

	// El peso: lo que se ahorra de contexto. (Se registra para el informe.)
	fb, eb := catalogBytes(t, full), catalogBytes(t, ess)
	t.Logf("catálogo completo: %d herramientas, %d bytes ≈ %d tokens (chars/4)", len(full), fb, (fb+3)/4)
	t.Logf("perfil esencial:   %d herramientas, %d bytes ≈ %d tokens (chars/4)", len(ess), eb, (eb+3)/4)
	if (eb+3)/4 > 1400 {
		t.Errorf("el perfil esencial pesa %d tokens: demasiado para un modelo de 8k", (eb+3)/4)
	}
	if eb*4 > fb {
		t.Errorf("el esencial (%d) debería pesar menos de un cuarto del completo (%d)", eb, fb)
	}
}

func requiredOf(schema map[string]any) []string {
	switch r := schema["required"].(type) {
	case []string:
		out := append([]string(nil), r...)
		slices.Sort(out)
		return out
	case []any:
		var out []string
		for _, v := range r {
			out = append(out, v.(string))
		}
		slices.Sort(out)
		return out
	}
	return nil
}

// El servidor `--mcp` sigue ofreciendo el catálogo completo, sin las descripciones
// cortas (Short/ShortSchema no viajan por JSON-RPC).
func TestShortSpecsDoNotLeakIntoTheServer(t *testing.T) {
	c := setup(t, nil)
	m := c.call("tools/list", nil)
	res, _ := m["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	if len(tools) != len(wantTools) {
		t.Fatalf("el servidor ofrece %d herramientas, esperaba %d", len(tools), len(wantTools))
	}
	raw, _ := json.Marshal(tools)
	for _, bad := range []string{`"Short"`, `"ShortSchema"`, `"short"`} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("tools/list filtra %s", bad)
		}
	}
	for _, it := range tools {
		tool := it.(map[string]any)
		if tool["name"] == "edit_checkpoint" && len(tool["description"].(string)) < 600 {
			t.Errorf("el servidor debe dar la descripción completa de edit_checkpoint: %d bytes", len(tool["description"].(string)))
		}
	}
}
