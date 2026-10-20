package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scadlab/backend/checkpoint"
)

// Pruebas del historial de un checkpoint (S16): los tres bindings, el indicador
// «editado por la IA» del árbol, el evento checkpoint:updated y que el watcher lo emita
// cuando otro proceso (el `--mcp` de un CLI) reescribe un archivo. Todo con un workspace
// temporal: nada toca la base ni las carpetas del usuario.

const (
	histOrig = "// @checkpoint parent=001-base.scad desc=\"x\"\ncolor(\"red\") cube(20);\n"
	histAI   = "color(\"red\") cube([20, 20, 30]);\n"
	histAI2  = "color(\"red\") cube([20, 20, 40]);\n"
)

// namedEvents anota los eventos de checkpoint (updated y changed) de una App.
type namedEvents struct {
	mu      sync.Mutex
	updated []CheckpointUpdatedEvent
	changed int
}

func captureNamedEvents(a *App) *namedEvents {
	ne := &namedEvents{}
	prev := a.emitter
	a.emitter = func(name string, payload any) {
		ne.mu.Lock()
		switch p := payload.(type) {
		case CheckpointUpdatedEvent:
			ne.updated = append(ne.updated, p)
		case WorkspaceView:
			if name == evCheckpointsChanged {
				ne.changed++
			}
		}
		ne.mu.Unlock()
		if prev != nil {
			prev(name, payload)
		}
	}
	return ne
}

func (ne *namedEvents) updatedFiles() []string {
	ne.mu.Lock()
	defer ne.mu.Unlock()
	var out []string
	for _, u := range ne.updated {
		out = append(out, u.File)
	}
	return out
}

func (ne *namedEvents) waitUpdated(t *testing.T, file string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range ne.updatedFiles() {
			if f == file {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no llegó checkpoint:updated de %s (llegaron %v)", file, ne.updatedFiles())
}

// historyApp deja 001-base.scad y 002-x.scad (hijo del primero).
func historyApp(t *testing.T) (*App, int64, string) {
	t.Helper()
	a, id, dir := editorWorkspace(t)
	if err := os.WriteFile(filepath.Join(dir, "002-x.scad"), []byte(histOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, id, dir
}

func checkpointOf(t *testing.T, v WorkspaceView, file string) (editedByAI bool) {
	t.Helper()
	for _, c := range v.Checkpoints {
		if c.FileName == file {
			return c.EditedByAI
		}
	}
	t.Fatalf("%s no está en el árbol", file)
	return false
}

func TestHistoryListReadAndRestoreRoundTrip(t *testing.T) {
	a, id, dir := historyApp(t)
	ne := captureNamedEvents(a)

	// Sin historial: lista vacía (no null) y sin indicador.
	h, err := a.ListCheckpointHistory(id, "002-x.scad")
	if err != nil || h.File != "002-x.scad" || h.Versions == nil || len(h.Versions) != 0 || h.EditedByAI || h.Max != checkpoint.DefaultHistoryMax {
		t.Fatalf("sin historial: %+v %v", h, err)
	}
	v, _ := a.RescanWorkspace(id)
	if checkpointOf(t, v, "002-x.scad") || checkpointOf(t, v, "001-base.scad") {
		t.Fatal("nadie lo editó todavía")
	}

	// La IA edita en el lugar (lo que hace update_checkpoint).
	if _, err := checkpoint.Update(dir, "002-x.scad", histAI, "más alto", checkpoint.DefaultHistoryMax); err != nil {
		t.Fatal(err)
	}
	h, err = a.ListCheckpointHistory(id, "002-x.scad")
	if err != nil || len(h.Versions) != 1 || !h.EditedByAI || h.Versions[0].Reason != checkpoint.ReasonAI || h.Versions[0].Note != "más alto" {
		t.Fatalf("tras la edición de la IA: %+v %v", h, err)
	}
	old, err := a.ReadCheckpointVersion(id, "002-x.scad", h.Versions[0].ID)
	if err != nil || old != histOrig {
		t.Fatalf("la versión guardada es la anterior, entera: %q %v", old, err)
	}
	v, _ = a.RescanWorkspace(id)
	if !checkpointOf(t, v, "002-x.scad") || checkpointOf(t, v, "001-base.scad") {
		t.Errorf("el árbol marca solo el archivo que editó la IA: %+v", v.Checkpoints)
	}

	// «Deshacer cambio de la IA» = restaurar esa versión: guarda la actual, enciende
	// los eventos y apaga el indicador solo.
	saved, err := a.RestoreCheckpointVersion(id, "002-x.scad", h.Versions[0].ID)
	if err != nil || saved.Reason != checkpoint.ReasonRestore {
		t.Fatalf("restaurar: %+v %v", saved, err)
	}
	if got := readFile(t, filepath.Join(dir, "002-x.scad")); got != histOrig {
		t.Errorf("el archivo volvió a lo de antes: %q", got)
	}
	h, _ = a.ListCheckpointHistory(id, "002-x.scad")
	if len(h.Versions) != 2 || h.Versions[0].Reason != checkpoint.ReasonRestore || h.EditedByAI {
		t.Fatalf("restaurar guarda lo que había y apaga «editado por la IA»: %+v", h)
	}
	if kept, _ := a.ReadCheckpointVersion(id, "002-x.scad", saved.ID); !strings.Contains(kept, "cube([20, 20, 30])") {
		t.Errorf("el cambio de la IA quedó en el historial: %q", kept)
	}
	if got := ne.updatedFiles(); len(got) != 1 || got[0] != "002-x.scad" {
		t.Errorf("checkpoint:updated tras restaurar: %v", got)
	}
	if ne.changed == 0 {
		t.Error("checkpoints:changed tras restaurar (el árbol relee la descripción y el indicador)")
	}
	v, _ = a.RescanWorkspace(id)
	if checkpointOf(t, v, "002-x.scad") {
		t.Error("el indicador del árbol se apaga")
	}

	// Y el deshacer se puede deshacer: restaurar la versión de la IA la trae de vuelta.
	if _, err := a.RestoreCheckpointVersion(id, "002-x.scad", saved.ID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "002-x.scad")); !strings.Contains(got, "cube([20, 20, 30])") || !strings.HasPrefix(got, "// @checkpoint parent=001-base.scad") {
		t.Errorf("rehacer: %q", got)
	}
}

func TestHistoryRespectsTheConfiguredLimit(t *testing.T) {
	a, id, dir := historyApp(t)
	s, err := a.store.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	s.HistoryMaxVersions = 2
	if err := a.store.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	h, _ := a.ListCheckpointHistory(id, "002-x.scad")
	if h.Max != 2 {
		t.Fatalf("el tope sale de la configuración: %d", h.Max)
	}
	for i, src := range []string{histAI, histAI2, "color(\"red\") cube([20, 20, 50]);\n"} {
		if _, err := checkpoint.Update(dir, "002-x.scad", src, "", h.Max); err != nil {
			t.Fatalf("edición %d: %v", i, err)
		}
	}
	h, _ = a.ListCheckpointHistory(id, "002-x.scad")
	if len(h.Versions) != 2 {
		t.Fatalf("al pasar el tope se descartan las más viejas: %d versiones", len(h.Versions))
	}
	// Restaurar también poda con el tope de la configuración.
	if _, err := a.RestoreCheckpointVersion(id, "002-x.scad", h.Versions[1].ID); err != nil {
		t.Fatal(err)
	}
	if h, _ = a.ListCheckpointHistory(id, "002-x.scad"); len(h.Versions) != 2 {
		t.Errorf("restaurar respeta el tope: %d", len(h.Versions))
	}
}

func TestHistoryBindingsRejectHostileInput(t *testing.T) {
	a, id, dir := historyApp(t)
	if _, err := checkpoint.Update(dir, "002-x.scad", histAI, "", 0); err != nil {
		t.Fatal(err)
	}
	h, _ := a.ListCheckpointHistory(id, "002-x.scad")
	good := h.Versions[0].ID

	// Algo fuera del workspace que un nombre hostil podría alcanzar.
	outside := filepath.Join(filepath.Dir(dir), "secreto.scad")
	if err := os.WriteFile(outside, []byte("cube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// (Con barra invertida o letra de unidad el nombre es legal en macOS —un archivo raro de la raíz—
	// y no sale del workspace; en Windows lo rechaza ValidName, y lo prueba su propio test.)
	files := []string{"", "../secreto.scad", "sub/002-x.scad", "/etc/passwd", "002-x.txt", "002-x.scad/..", ".scadlab/history/002-x.scad"}
	for _, f := range files {
		if _, err := a.ListCheckpointHistory(id, f); err == nil {
			t.Errorf("ListCheckpointHistory(%q) debía fallar", f)
		}
		if _, err := a.ReadCheckpointVersion(id, f, good); err == nil {
			t.Errorf("ReadCheckpointVersion(%q) debía fallar", f)
		}
		if _, err := a.RestoreCheckpointVersion(id, f, good); err == nil {
			t.Errorf("RestoreCheckpointVersion(%q) debía fallar", f)
		}
	}
	ids := []string{"", "../x", "../../../etc/passwd", good + "/../x", good + ".scad", "20261019", "x", good + "\x00"}
	for _, v := range ids {
		if _, err := a.ReadCheckpointVersion(id, "002-x.scad", v); err == nil {
			t.Errorf("ReadCheckpointVersion con id %q debía fallar", v)
		}
		if _, err := a.RestoreCheckpointVersion(id, "002-x.scad", v); err == nil {
			t.Errorf("RestoreCheckpointVersion con id %q debía fallar", v)
		}
	}
	// Un id con forma válida pero que no existe.
	if _, err := a.ReadCheckpointVersion(id, "002-x.scad", "19990101-000000"); err == nil {
		t.Error("una versión que no existe es un error")
	}
	if _, err := a.RestoreCheckpointVersion(id, "002-x.scad", "19990101-000000"); err == nil {
		t.Error("restaurar una versión que no existe es un error")
	}
	// Un archivo con nombre válido que no existe no tiene qué restaurar.
	if _, err := a.RestoreCheckpointVersion(id, "009-no-existe.scad", good); err == nil {
		t.Error("restaurar sobre un archivo que no existe es un error")
	}
	// Workspace inexistente.
	if _, err := a.ListCheckpointHistory(99999, "002-x.scad"); err == nil {
		t.Error("workspace inexistente")
	}
	if _, err := a.ReadCheckpointVersion(99999, "002-x.scad", good); err == nil {
		t.Error("workspace inexistente")
	}
	if _, err := a.RestoreCheckpointVersion(99999, "002-x.scad", good); err == nil {
		t.Error("workspace inexistente")
	}
	if got := readFile(t, outside); got != "cube(1);\n" {
		t.Errorf("algo tocó un archivo fuera del workspace: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "002-x.scad")); !strings.Contains(got, "cube([20, 20, 30])") {
		t.Errorf("las llamadas rechazadas no cambian nada: %q", got)
	}
}

// El historial viaja con el archivo a la papelera y vuelve con él.
func TestHistoryFollowsTheCheckpointThroughTheTrash(t *testing.T) {
	a, id, dir := historyApp(t)
	if _, err := checkpoint.Update(dir, "002-x.scad", histAI, "", 0); err != nil {
		t.Fatal(err)
	}
	names, err := a.TrashCheckpoint(id, "002-x.scad", false)
	if err != nil || len(names) != 1 {
		t.Fatalf("papelera: %v %v", names, err)
	}
	// Mientras está en la papelera no hay historial a la vista, y editarlo no se puede.
	if h, err := a.ListCheckpointHistory(id, "002-x.scad"); err != nil || len(h.Versions) != 0 {
		t.Errorf("el historial se fue con el archivo: %+v %v", h, err)
	}
	back, err := a.RestoreCheckpoint(id, names[0])
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.ListCheckpointHistory(id, back)
	if err != nil || len(h.Versions) != 1 || !h.EditedByAI {
		t.Fatalf("el historial volvió con el archivo: %+v %v", h, err)
	}
	if old, _ := a.ReadCheckpointVersion(id, back, h.Versions[0].ID); old != histOrig {
		t.Errorf("la versión sigue entera: %q", old)
	}
}

// El historial no es un checkpoint: no aparece en el árbol ni en el escaneo.
func TestHistoryFolderIsNotPartOfTheTree(t *testing.T) {
	a, id, dir := historyApp(t)
	if _, err := checkpoint.Update(dir, "002-x.scad", histAI, "", 0); err != nil {
		t.Fatal(err)
	}
	v, err := a.RescanWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range v.Checkpoints {
		names = append(names, c.FileName)
	}
	if strings.Join(names, ",") != "001-base.scad,002-x.scad" {
		t.Errorf("árbol: %v", names)
	}
}

// onWorkspaceChange: un archivo reescrito avisa con checkpoint:updated y NO es un
// checkpoint nuevo (no se vincula ni se renderiza como tal).
func TestWorkspaceChangeEmitsCheckpointUpdatedForRewrittenFiles(t *testing.T) {
	a, id, _ := historyApp(t)
	ne := captureNamedEvents(a)
	// Sin auto-render: el render de un archivo nuevo corre en otra goroutine y sobreviviría a la prueba.
	st, _ := a.store.GetSettings()
	st.AutoRender = false
	if err := a.store.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	a.onWorkspaceChange(id, nil, []string{"002-x.scad"})
	if got := ne.updatedFiles(); len(got) != 1 || got[0] != "002-x.scad" {
		t.Fatalf("eventos: %v", got)
	}
	if ne.changed != 1 {
		t.Errorf("el árbol también se refresca: %d", ne.changed)
	}
	// Un archivo nuevo sigue sin ser un «updated».
	a.onWorkspaceChange(id, []string{"003-nuevo.scad"}, nil)
	if got := ne.updatedFiles(); len(got) != 1 {
		t.Errorf("un archivo nuevo no emite checkpoint:updated: %v", got)
	}
}

// Una escritura de otro proceso (lo que hace el `--mcp` de un CLI) llega a la interfaz por
// el watcher, sin que la app la haya hecho.
func TestWatcherReportsAnExternalRewriteAsCheckpointUpdated(t *testing.T) {
	a, id, dir := historyApp(t)
	ne := captureNamedEvents(a)
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	a.watchWorkspace(w)
	t.Cleanup(a.stopWatcher)

	if _, err := checkpoint.Update(dir, "002-x.scad", histAI, "desde afuera", 0); err != nil {
		t.Fatal(err)
	}
	ne.waitUpdated(t, "002-x.scad")
	for _, f := range ne.updatedFiles() {
		if f != "002-x.scad" {
			t.Errorf("solo el archivo reescrito: %v", ne.updatedFiles())
		}
	}
	if h, _ := a.ListCheckpointHistory(id, "002-x.scad"); !h.EditedByAI {
		t.Errorf("el árbol que emite el watcher ya trae el indicador: %+v", h)
	}
}
