//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"scadlab/backend/detect"
	"scadlab/backend/hermes"
	"scadlab/backend/mcp"
)

// Cambios del árbol hechos por la IA sin un .scad nuevo (S12.c, T13): tras un paso
// bueno de set_active_parent, rename_checkpoint, trash_checkpoint o
// restore_checkpoint la app reescanea el workspace y emite checkpoints:changed.

// treeViews acumula los checkpoints:changed que emite la app, además de lo que ya
// captura hermesEvents.
type treeViews struct {
	mu    sync.Mutex
	views []WorkspaceView
}

func captureViews(a *App) *treeViews {
	tv := &treeViews{}
	prev := a.emitter
	a.emitter = func(name string, payload any) {
		if v, ok := payload.(WorkspaceView); ok && name == evCheckpointsChanged {
			tv.mu.Lock()
			tv.views = append(tv.views, v)
			tv.mu.Unlock()
		}
		if prev != nil {
			prev(name, payload)
		}
	}
	return tv
}

func (tv *treeViews) all() []WorkspaceView {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	return append([]WorkspaceView(nil), tv.views...)
}

func fileNames(v WorkspaceView) []string {
	out := make([]string, len(v.Checkpoints))
	for i, c := range v.Checkpoints {
		out[i] = c.FileName
	}
	return out
}

// Hermes con el modelo simulado: el padre activo, el rótulo y la papelera llegan a
// la interfaz en el mismo turno, un aviso por cada paso que cambió algo. Un
// trash_checkpoint de Hermes saca el checkpoint del árbol que recibe la interfaz.
func TestHermesTreeToolsEmitCheckpointsChanged(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.script = []simReply{
		{calls: []simCall{{"set_active_parent", map[string]any{"file": "001-base.scad"}}}},
		{calls: []simCall{{"rename_checkpoint", map[string]any{"file": "001-base.scad", "label": "La base"}}}},
		{calls: []simCall{{"trash_checkpoint", map[string]any{"file": "002-hijo.scad"}}}},
		{text: "Listo."},
	}
	a, wsID, dir, ev := hermesApp(t, sim)
	if err := os.WriteFile(filepath.Join(dir, "002-hijo.scad"), []byte("// @checkpoint parent=001-base.scad desc=\"hijo\"\ncolor(\"red\") cube(5);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// El árbol que la interfaz ya conocía, con los dos checkpoints.
	w, err := a.store.GetWorkspace(wsID)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := a.rescan(w); err != nil || len(v.Checkpoints) != 2 {
		t.Fatalf("rescan inicial: %v %v", v.Checkpoints, err)
	}
	if a.isWatched(wsID) {
		t.Fatal("la prueba supone que el watcher no está activo: el aviso tiene que venir del paso")
	}
	tv := captureViews(a)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "ordena el árbol", "001-base.scad", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)

	views := tv.all()
	if len(views) != 3 {
		t.Fatalf("esperaba un checkpoints:changed por paso (3), llegaron %d", len(views))
	}
	for _, v := range views {
		if v.Workspace.ID != wsID {
			t.Errorf("aviso de otro proyecto: %+v", v.Workspace)
		}
	}
	if views[0].ActiveParent != "001-base.scad" {
		t.Errorf("padre activo tras set_active_parent: %q", views[0].ActiveParent)
	}
	label := ""
	for _, cp := range views[1].Checkpoints {
		if cp.FileName == "001-base.scad" {
			label = cp.Label
		}
	}
	if label != "La base" {
		t.Errorf("rótulo tras rename_checkpoint: %q", label)
	}
	if got := fileNames(views[2]); !slices.Equal(got, []string{"001-base.scad"}) {
		t.Errorf("tras trash_checkpoint el árbol debía quedar sin 002-hijo.scad: %v", got)
	}
}

// Un paso que falla, uno en curso o de otra herramienta no refresca nada; el que
// termina bien sí. restore_checkpoint además marca el archivo como restaurado: el
// watcher no lo trata como trabajo nuevo (no avanza el padre activo).
func TestRefreshTreeAfterStep(t *testing.T) {
	a, wsID, dir, _ := hermesApp(t, newSim())
	tv := captureViews(a)
	step := func(tool string, status string, text string) hermes.Step {
		return hermes.Step{Tool: tool, Status: status, Text: text}
	}
	for _, st := range []hermes.Step{
		step("trash_checkpoint", hermes.StepRunning, ""),
		step("trash_checkpoint", hermes.StepError, "error: no existe"),
		step("render", hermes.StepOK, `{"status":"ok"}`),
		step("create_checkpoint", hermes.StepOK, `{"file":"002-x.scad"}`), // lo ve el watcher
		step("list_trash", hermes.StepOK, `{"trash":[]}`),
	} {
		a.refreshTreeAfterStep(wsID, st)
	}
	if n := len(tv.all()); n != 0 {
		t.Fatalf("no debía avisar: %d avisos", n)
	}
	for _, tool := range mcp.ToolsWithEffect(mcp.EffectTree) {
		before := len(tv.all())
		a.refreshTreeAfterStep(wsID, step(tool, hermes.StepOK, `{"file":"002-hijo.scad"}`))
		if len(tv.all()) != before+1 {
			t.Errorf("%s terminó bien y no refrescó el árbol", tool)
		}
	}
	// Una herramienta que no existe en el workspace: se loguea y no revienta.
	a.refreshTreeAfterStep(9999, step("trash_checkpoint", hermes.StepOK, `{}`))

	// restore_checkpoint dejó marcado el archivo: el watcher lo descarta de los «nuevos».
	if left := a.checkpoints().dropRestored(wsID, []string{"002-hijo.scad", "003-otro.scad"}); !slices.Equal(left, []string{"003-otro.scad"}) {
		t.Errorf("002-hijo.scad debía estar marcado como restaurado: %v", left)
	}
	_ = dir
}

// La red de seguridad de un CLI no guarda un bloque de más tras ninguna
// herramienta que ya dejó un checkpoint (la lista es mcp.ToolsWithEffect): set_parameters,
// create_from_template, edit_mesh… Con una que solo cambia el árbol sí lo guarda.
func TestCLISafetyNetCountsEveryCheckpointCreatingTool(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	block := "Aquí está:\n```openscad\n// @checkpoint parent= desc=\"Caja\"\ncolor(\"red\") cube([20, 20, 10]);\n```"
	run := func(t *testing.T, tool string) (saved bool, res *hermes.Result, dir string) {
		a, wsID, dir, _, f := cliApp(t)
		f.install("claude", "")
		c, err := a.NewChat(wsID, "cli:claude", "")
		if err != nil {
			t.Fatal(err)
		}
		w, _ := a.store.GetWorkspace(wsID)
		res = &hermes.Result{Text: block, Steps: []hermes.Step{{ID: "st-1", Index: 1, Tool: tool, Status: hermes.StepOK, Text: `{"file":"002-x.scad"}`}}}
		j := chatJob{chat: c, workspace: w, currentFile: "001-base.scad", agent: true}
		saved = a.cliSafetyNet(context.Background(), j, workspaceScadSet(dir), res)
		return saved, res, dir
	}
	for _, tool := range mcp.ToolsWithEffect(mcp.EffectCheckpoint) {
		t.Run(tool, func(t *testing.T) {
			saved, res, dir := run(t, tool)
			if !saved || len(res.Steps) != 1 {
				t.Errorf("la red no debía hacer nada: saved=%v pasos=%+v", saved, res.Steps)
			}
			if files := rootScads(t, dir); len(files) != 1 {
				t.Errorf("archivos: %v", files)
			}
		})
	}
	t.Run("trash_checkpoint no cuenta", func(t *testing.T) {
		saved, res, dir := run(t, "trash_checkpoint")
		if !saved || len(res.Steps) < 2 || res.Steps[1].Tool != autoStepTool {
			t.Errorf("la red debía guardar el bloque: saved=%v pasos=%+v", saved, res.Steps)
		}
		if files := rootScads(t, dir); len(files) != 2 {
			t.Errorf("archivos: %v", files)
		}
	})
}
