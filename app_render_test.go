package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"scadlab/backend/render"
	"scadlab/backend/store"
)

// newTestApp arma una App con base y caché en t.TempDir(), sin Wails.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	eng, err := render.NewEngine(filepath.Join(dir, "cache", "meshes")) // misma ruta que app.go
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	s := store.DefaultSettings()
	s.WorkspacesRoot = filepath.Join(dir, "workspaces")
	if err := st.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	return &App{ctx: context.Background(), store: st, dataDir: dir, engine: eng}
}

// RENDER_ERRORS.md sigue al checkpoint más nuevo: es el que mira el agente.
func TestRenderErrorsFollowLatestCheckpoint(t *testing.T) {
	if _, err := exec.LookPath("openscad"); err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Prueba render", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(w.Path, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	errsMD := filepath.Join(w.Path, "RENDER_ERRORS.md")
	exists := func() bool { _, err := os.Stat(errsMD); return err == nil }

	write("001-ok.scad", "// @checkpoint parent= desc=\"base\"\ncolor(\"red\") cube(10);\n")
	write("002-broken.scad", "// @checkpoint parent=001-ok.scad desc=\"rota\"\ncube(10;\n")

	render1 := func(f string) render.Result {
		r, err := a.RenderCheckpoint(w.ID, f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		return r
	}
	if r := render1("002-broken.scad"); r.Status != render.StatusError || !exists() {
		t.Fatalf("el último roto debe dejar RENDER_ERRORS.md: %s exists=%v", r.Status, exists())
	}
	if r := render1("001-ok.scad"); r.Status != render.StatusOK || !exists() {
		t.Fatalf("renderizar uno viejo que anda no debe borrar el aviso del último: exists=%v", exists())
	}
	write("003-fixed.scad", "// @checkpoint parent=002-broken.scad desc=\"arreglada\"\ncolor(\"red\") cube(12);\n")
	if r := render1("003-fixed.scad"); r.Status != render.StatusOK || exists() {
		t.Fatalf("el último arreglado debe borrar RENDER_ERRORS.md: exists=%v", exists())
	}
	if r := render1("002-broken.scad"); r.Status != render.StatusError || exists() {
		t.Fatalf("abrir el roto viejo no debe volver a escribir el aviso: exists=%v", exists())
	}
	st, _ := a.GetRenderState(w.ID)
	if len(st) != 3 {
		t.Errorf("GetRenderState: %d resultados", len(st))
	}
}
