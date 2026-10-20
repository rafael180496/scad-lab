package mcp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scadlab/backend/clichat"
)

// La tabla de efectos (effects.go) cruzada con el catálogo y con los modos: toda
// herramienta de «Agente» declara qué le hace al proyecto, ninguna de «Solo
// consulta» lo declara, y no hay entradas huérfanas. Es lo que impide que una
// herramienta nueva que crea checkpoints se olvide de la red de seguridad del chat
// (guardaría un bloque de más) o del enlace de su tarjeta.
func TestEffectsCoverTheWriteTools(t *testing.T) {
	tb, _ := newToolbox(t, false)
	catalog := map[string]bool{}
	for _, tl := range tb.Tools() {
		catalog[tl.Name] = true
	}
	for name := range effects {
		if !catalog[name] {
			t.Errorf("effects.go declara %q pero no está en el catálogo", name)
		}
	}
	for _, name := range clichat.WriteTools() {
		if EffectOf(name) == EffectNone {
			t.Errorf("%s es de escritura (solo «Agente») y no declara su efecto en effects.go", name)
		}
	}
	for _, name := range clichat.ReadTools() {
		if EffectOf(name) != EffectNone {
			t.Errorf("%s es de lectura y declara un efecto en effects.go", name)
		}
	}
	// Los grupos que usa el resto del código, fijos a propósito: cambiarlos es una
	// decisión (hermes, el autosave, el árbol y CREATING_TOOLS del frontend).
	wantCreate := []string{"create_checkpoint", "create_from_template", "edit_mesh", "import_mesh", "set_parameters", "update_checkpoint"}
	if got := ToolsWithEffect(EffectCheckpoint); !slices.Equal(got, wantCreate) {
		t.Errorf("crean checkpoint: %v, esperaba %v", got, wantCreate)
	}
	wantTree := []string{"rename_checkpoint", "restore_checkpoint", "set_active_parent", "trash_checkpoint"}
	if got := ToolsWithEffect(EffectTree); !slices.Equal(got, wantTree) {
		t.Errorf("cambian el árbol: %v, esperaba %v", got, wantTree)
	}
	// Escriben fuera del árbol (exports/, lib/, refs/): nada que enlazar ni refrescar.
	wantFiles := []string{"export", "image_to_heightmap", "install_library", "trace_image_outline"}
	if got := ToolsWithEffect(EffectFiles); !slices.Equal(got, wantFiles) {
		t.Errorf("escriben archivos: %v, esperaba %v", got, wantFiles)
	}
	for _, n := range wantFiles {
		if CreatesCheckpoint(n) || ChangesTree(n) || ChangesConfig(n) {
			t.Errorf("%s solo escribe archivos: no crea checkpoints ni cambia el árbol ni la configuración", n)
		}
	}
	// update_checkpoint es la única que reescribe en el lugar (no crea un .scad nuevo).
	for _, n := range wantCreate {
		if UpdatesInPlace(n) != (n == "update_checkpoint") {
			t.Errorf("UpdatesInPlace(%s) = %v", n, UpdatesInPlace(n))
		}
	}
	if UpdatesInPlace("render") || UpdatesInPlace("no_existe") || UpdatesInPlace("") {
		t.Error("UpdatesInPlace solo es cierto para update_checkpoint")
	}
	for _, n := range wantCreate {
		if !CreatesCheckpoint(n) || ChangesTree(n) {
			t.Errorf("%s: CreatesCheckpoint/ChangesTree no coinciden con la tabla", n)
		}
	}
	for _, n := range wantTree {
		if CreatesCheckpoint(n) || !ChangesTree(n) {
			t.Errorf("%s: CreatesCheckpoint/ChangesTree no coinciden con la tabla", n)
		}
	}
	if CreatesCheckpoint("render") || ChangesTree("render") || CreatesCheckpoint("no_existe") {
		t.Error("render y lo desconocido no cambian nada")
	}
}

// La declaración es verdad: cada herramienta que dice crear un checkpoint devuelve
// en `file` un .scad que no existía y que está en el workspace; las que cambian el
// árbol no devuelven uno nuevo.
func TestDeclaredEffectsMatchWhatTheToolsDo(t *testing.T) {
	tb, ws, _, _ := registeredToolbox(t)
	scads := func() []string {
		files, _ := filepath.Glob(filepath.Join(ws, "*.scad"))
		for i, f := range files {
			files[i] = filepath.Base(f)
		}
		return files
	}
	base := tb.create(t, "caja", paramsModel, nil)
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "soporte.stl"))

	calls := map[string]map[string]any{
		"create_checkpoint":    {"label": "otra", "source": srcCube},
		"import_mesh":          {"file": "refs/soporte.stl"},
		"set_parameters":       {"file": base, "values": map[string]any{"width": 35}},
		"create_from_template": {"name": "knob"},
		"edit_mesh":            {"file": base, "operations": []any{map[string]any{"op": "center"}}},
		"update_checkpoint":    {"file": base, "source": srcCube},
	}
	// edit_mesh congela la malla del checkpoint: necesita un motor (el instalado o el integrado).
	if tool := tb.e.openscad(context.Background(), ""); !tool.Found && !tb.e.wasm.Available() {
		delete(calls, "edit_mesh")
	}
	if len(calls) < 5 {
		t.Fatalf("faltan herramientas por probar: %v", calls)
	}
	for _, name := range ToolsWithEffect(EffectCheckpoint) {
		args, ok := calls[name]
		if !ok {
			if name == "edit_mesh" {
				continue // sin motor, arriba
			}
			t.Fatalf("%s crea checkpoints y esta prueba no la llama: agrégala a calls", name)
		}
		before := scads()
		file, _ := tb.mustJSON(t, name, args)["file"].(string)
		if UpdatesInPlace(name) {
			// Reescribe un .scad que ya existía: devuelve su nombre y no aparece ninguno nuevo.
			if file != base || !slices.Equal(scads(), before) {
				t.Errorf("%s: `file` = %q, esperaba %q y ningún .scad nuevo (%v -> %v)", name, file, base, before, scads())
			}
			continue
		}
		if !strings.HasSuffix(file, ".scad") || slices.Contains(before, file) {
			t.Errorf("%s: `file` = %q, esperaba un .scad nuevo", name, file)
			continue
		}
		if !slices.Contains(scads(), file) || len(scads()) != len(before)+1 {
			t.Errorf("%s: el workspace tenía %d .scad y ahora %v", name, len(before), scads())
		}
	}

	// Las del árbol: ninguna crea un nombre que no existiera ya (restaurar trae de vuelta uno).
	known := scads()
	tb.mustJSON(t, "set_active_parent", map[string]any{"file": base})
	tb.mustJSON(t, "rename_checkpoint", map[string]any{"file": base, "label": "Caja"})
	if after := scads(); !slices.Equal(after, known) {
		t.Errorf("set_active_parent y rename_checkpoint no deben tocar los .scad: %v -> %v", known, after)
	}
	trashed := tb.mustJSON(t, "trash_checkpoint", map[string]any{"file": base})["trashed"].([]any)
	if after := scads(); slices.Contains(after, base) || len(after) != len(known)-1 {
		t.Errorf("trash_checkpoint: %v", after)
	}
	back := tb.mustJSON(t, "restore_checkpoint", map[string]any{"trashed_name": trashed[0].(string)})["file"].(string)
	if !slices.Contains(known, back) {
		t.Errorf("restore_checkpoint devolvió un nombre nuevo: %s", back)
	}
}

// EffectConfig (S14) lo declara una sola herramienta, set_workspace_printer, y no
// puede colarse en las listas de los otros efectos.
func TestEffectConfigIsOnlySetWorkspacePrinter(t *testing.T) {
	if EffectConfig == EffectNone || EffectConfig == EffectCheckpoint || EffectConfig == EffectTree || EffectConfig == EffectFiles {
		t.Fatalf("EffectConfig repite el valor de otro efecto: %d", EffectConfig)
	}
	if got := ToolsWithEffect(EffectConfig); !slices.Equal(got, []string{"set_workspace_printer"}) {
		t.Errorf("EffectConfig lo declara solo set_workspace_printer: %v", got)
	}
	if !ChangesConfig("set_workspace_printer") || CreatesCheckpoint("set_workspace_printer") || ChangesTree("set_workspace_printer") {
		t.Error("set_workspace_printer cambia la configuración, no el árbol ni crea checkpoints")
	}
	if ChangesConfig("render") || ChangesConfig("no_existe") {
		t.Error("ChangesConfig devolvió true para una herramienta que no cambia la configuración")
	}
	for _, name := range ToolsWithEffect(EffectTree) {
		if ChangesConfig(name) {
			t.Errorf("%s cambia el árbol, no la configuración", name)
		}
	}
}
