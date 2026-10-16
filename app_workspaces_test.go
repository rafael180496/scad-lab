package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/i18n"
	"scadlab/backend/store"
)

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Un padre que no existe se crea (con sus intermedios) y el workspace queda en
// <padre>/<slug> con sus archivos base; la ruta devuelta es la de la vista previa.
func TestCreateWorkspaceInCreatesNewParent(t *testing.T) {
	a := newTestApp(t)
	parent := filepath.Join(t.TempDir(), "otro", "disco", "modelos")
	want := a.PreviewWorkspacePath("Soporte de Auriculares", parent)
	if want != filepath.Join(parent, "soporte-de-auriculares") {
		t.Fatalf("vista previa: %q", want)
	}
	w, err := a.CreateWorkspaceIn("Soporte de Auriculares", "bambu-a1-mini", parent)
	if err != nil {
		t.Fatal(err)
	}
	if w.Path != want {
		t.Errorf("ruta creada %q != vista previa %q", w.Path, want)
	}
	for _, f := range []string{"CLAUDE.md", "AGENTS.md", "printer.json", ".gitignore", "lib", "exports"} {
		if !exists(filepath.Join(want, f)) {
			t.Errorf("falta %s", f)
		}
	}
	// No se creó nada en la carpeta predeterminada.
	if exists(filepath.Join(a.EffectiveWorkspacesRoot(), "soporte-de-auriculares")) {
		t.Error("se creó también en la carpeta predeterminada")
	}
	got, err := a.store.GetWorkspace(w.ID)
	if err != nil || got.Path != want || got.Name != "Soporte de Auriculares" {
		t.Errorf("registro: %+v, %v", got, err)
	}
}

// Sin ubicación rige la predeterminada, y CreateWorkspace es su alias.
func TestCreateWorkspaceDefaultsToConfiguredRoot(t *testing.T) {
	a := newTestApp(t)
	root := a.EffectiveWorkspacesRoot()
	w1, err := a.CreateWorkspaceIn("  Uno  ", "bambu-a1", "")
	if err != nil {
		t.Fatal(err)
	}
	w2, err := a.CreateWorkspace("Dos", "bambu-a1")
	if err != nil {
		t.Fatal(err)
	}
	if w1.Path != filepath.Join(root, "uno") || w2.Path != filepath.Join(root, "dos") {
		t.Errorf("rutas: %q, %q (raíz %q)", w1.Path, w2.Path, root)
	}
	if a.PreviewWorkspacePath("Tres", "   ") != filepath.Join(root, "tres") {
		t.Error("la vista previa con ubicación vacía debe usar la predeterminada")
	}
}

// Una ubicación relativa (o con «~») es un error claro y no deja nada en disco,
// ni en la carpeta de trabajo del proceso.
func TestCreateWorkspaceInRejectsRelativeParent(t *testing.T) {
	a := newTestApp(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	for _, parent := range []string{"modelos", "./modelos", "../x", "~/Documents/ScadLab"} {
		if _, err := a.CreateWorkspaceIn("Caja", "bambu-a1", parent); err == nil {
			t.Errorf("%q: debería fallar", parent)
		} else if !strings.Contains(err.Error(), "absoluta") && !strings.Contains(err.Error(), "absolute") {
			t.Errorf("%q: el mensaje no explica el problema: %v", parent, err)
		}
		if got := a.PreviewWorkspacePath("Caja", parent); got != "" {
			t.Errorf("%q: la vista previa de una ruta relativa debe ser vacía, no %q", parent, got)
		}
	}
	if ents, _ := os.ReadDir(cwd); len(ents) != 0 {
		t.Errorf("se escribió en el directorio de trabajo: %v", ents)
	}
	if ws, _ := a.store.ListWorkspaces(); len(ws) != 0 {
		t.Errorf("se registró un workspace: %+v", ws)
	}
}

// Un destino que ya existe y no está vacío no se toca: error con la ruta, en los
// dos idiomas. Vacío sí se acepta.
func TestCreateWorkspaceInRejectsNonEmptyTarget(t *testing.T) {
	a := newTestApp(t)
	parent := t.TempDir()
	target := filepath.Join(parent, "caja")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(target, "mio.scad")
	if err := os.WriteFile(mine, []byte("cube(1);"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"es", "en"} {
		i18n.SetLang(lang)
		t.Cleanup(func() { i18n.SetLang("en") })
		_, err := a.CreateWorkspaceIn("Caja", "bambu-a1", parent)
		if err == nil {
			t.Fatalf("%s: debería fallar", lang)
		}
		msg := err.Error()
		if !strings.Contains(msg, target) {
			t.Errorf("%s: el error no nombra la carpeta: %v", lang, err)
		}
		if lang == "es" && !strings.Contains(msg, "no está vacía") {
			t.Errorf("es: %v", err)
		}
		if lang == "en" && !strings.Contains(msg, "not empty") {
			t.Errorf("en: %v", err)
		}
	}
	if raw, _ := os.ReadFile(mine); string(raw) != "cube(1);" {
		t.Error("se tocó un archivo del usuario")
	}
	if exists(filepath.Join(target, "CLAUDE.md")) {
		t.Error("se escribieron las reglas en una carpeta ajena")
	}
	// Vacía: sí.
	if err := os.Remove(mine); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateWorkspaceIn("Caja", "bambu-a1", parent); err != nil {
		t.Errorf("destino vacío: %v", err)
	}
}

// El padre existe pero es un archivo: error que lo dice.
func TestCreateWorkspaceInRejectsFileAsParent(t *testing.T) {
	a := newTestApp(t)
	f := filepath.Join(t.TempDir(), "archivo")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateWorkspaceIn("Caja", "bambu-a1", f); err == nil || !strings.Contains(err.Error(), f) {
		t.Errorf("padre que es un archivo: %v", err)
	}
}

// Acentos, espacios y símbolos pasan por el mismo Slug en la vista previa y al crear.
func TestPreviewMatchesCreateForAwkwardNames(t *testing.T) {
	a := newTestApp(t)
	parent := t.TempDir()
	cases := map[string]string{
		"Caja de Electrónica":    "caja-de-electronica",
		"  Soporte / Pared: v2 ": "soporte-pared-v2",
		"Ñandú Ünico — ¡Ya!":     "nandu-unico-ya",
		"Tapa 2.0 (rev. B)":      "tapa-2-0-rev-b",
	}
	for name, slug := range cases {
		want := filepath.Join(parent, slug)
		if got := a.PreviewWorkspacePath(name, parent); got != want {
			t.Errorf("vista previa de %q = %q, esperaba %q", name, got, want)
			continue
		}
		w, err := a.CreateWorkspaceIn(name, "bambu-a1", parent)
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if w.Path != want {
			t.Errorf("%q: creó %q, la vista previa decía %q", name, w.Path, want)
		}
	}
	// Sin carácter válido no hay ruta ni se crea nada.
	if got := a.PreviewWorkspacePath("???", parent); got != "" {
		t.Errorf("nombre sin slug: %q", got)
	}
	if _, err := a.CreateWorkspaceIn("???", "bambu-a1", parent); err == nil {
		t.Error("un nombre sin letras debería fallar")
	}
	// Barras de más y «..» se limpian: la vista previa es la ruta exacta.
	messy := parent + string(filepath.Separator) + "a" + string(filepath.Separator) + ".." + string(filepath.Separator)
	if got := a.PreviewWorkspacePath("Caja", messy); got != filepath.Join(parent, "caja") {
		t.Errorf("ruta sin limpiar: %q", got)
	}
}

func TestSetWorkspacesRoot(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "mis-modelos")
	if err := a.SetWorkspacesRoot(dir); err != nil {
		t.Fatal(err)
	}
	if a.EffectiveWorkspacesRoot() != dir {
		t.Errorf("raíz: %q", a.EffectiveWorkspacesRoot())
	}
	w, err := a.CreateWorkspace("Nuevo", "bambu-a1")
	if err != nil || w.Path != filepath.Join(dir, "nuevo") {
		t.Errorf("crear en la nueva raíz: %+v, %v", w, err)
	}
	if err := a.SetWorkspacesRoot("relativa/ruta"); err == nil {
		t.Error("una ruta relativa no puede ser la predeterminada")
	}
	if a.EffectiveWorkspacesRoot() != dir {
		t.Error("el rechazo cambió la raíz")
	}
	// Vacío vuelve a ~/Documents/ScadLab.
	if err := a.SetWorkspacesRoot(""); err != nil {
		t.Fatal(err)
	}
	if got := a.EffectiveWorkspacesRoot(); got == dir || got == "" {
		t.Errorf("vacío debería volver a la predeterminada: %q", got)
	}
}

// El último checkpoint elegido se guarda por workspace y vuelve en WorkspaceView
// al reabrir; el binding rechaza un nombre que se sale del workspace.
func TestSetLastCheckpointRoundTrip(t *testing.T) {
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Memoria", "bambu-a1")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"001-base.scad", "002-tapa.scad"} {
		if err := os.WriteFile(filepath.Join(w.Path, f), []byte("cube(1);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SetLastCheckpoint(w.ID, "001-base.scad"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetLastCheckpoint(w.ID, "../../etc/passwd"); err == nil {
		t.Error("un nombre fuera del workspace debe rechazarse")
	}
	v, err := a.OpenProject(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopWatcher)
	if v.Workspace.LastCheckpoint != "001-base.scad" || len(v.Checkpoints) != 2 {
		t.Errorf("al reabrir: %q, %d checkpoints", v.Workspace.LastCheckpoint, len(v.Checkpoints))
	}
	if err := a.SetLastCheckpoint(w.ID, ""); err != nil {
		t.Errorf("vaciar: %v", err)
	}
}

func TestSaveViewerNormalizesAndPersists(t *testing.T) {
	a := newTestApp(t)
	got, err := a.SaveViewer(storeViewer(true, 9))
	if err != nil || got.ColorIndex != 0 {
		t.Fatalf("normalizar: %+v, %v", got, err)
	}
	s, _ := a.GetSettings()
	if !s.Viewer.Wireframe || s.Viewer.ColorIndex != 0 {
		t.Errorf("persistido: %+v", s.Viewer)
	}
}

func storeViewer(wire bool, color int) store.ViewerState {
	return store.ViewerState{Wireframe: wire, ColorIndex: color, ModelColors: true}
}
