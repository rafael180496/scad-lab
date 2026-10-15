package main

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"scadlab/backend/exporter"
)

// exportRig arma un workspace con un checkpoint de dos colores.
func exportRig(t *testing.T) (*App, int64, string) {
	t.Helper()
	if _, err := exec.LookPath("openscad"); err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Mi Placa", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	name := "001-dos.scad"
	body := "// @checkpoint parent= desc=\"dos colores\"\ncolor(\"red\") cube(10);\ntranslate([20,0,0]) color(\"blue\") cube(10);\n"
	if err := os.WriteFile(filepath.Join(w.Path, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, w.ID, name
}

func TestExportCheckpointFormats(t *testing.T) {
	a, id, name := exportRig(t)
	out := t.TempDir()
	for _, c := range []struct{ format, file string }{
		{exporter.FormatSTL, "salida.stl"},
		{exporter.Format3MF, "salida.3mf"},
		{exporter.FormatSTLPerColor, "salida.zip"},
	} {
		var gotDir, gotName string
		res, err := a.exportCheckpoint(id, name, c.format, func(dir, suggested, format string) (string, error) {
			gotDir, gotName = dir, suggested
			return filepath.Join(out, c.file), nil
		})
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		if res.Path != filepath.Join(out, c.file) || res.Format != c.format {
			t.Errorf("%s: resultado %+v", c.format, res)
		}
		if want := "mi-placa-001-dos" + exporter.Ext(c.format); gotName != want {
			t.Errorf("%s: nombre sugerido %q, quería %q", c.format, gotName, want)
		}
		if st, err := os.Stat(res.Path); err != nil || st.Size() == 0 {
			t.Errorf("%s: no se escribió %s", c.format, res.Path)
		}
		_ = gotDir
		if c.format != exporter.FormatSTL && len(res.Parts) != 2 {
			t.Errorf("%s: partes %+v", c.format, res.Parts)
		}
	}
	zr, err := zip.OpenReader(filepath.Join(out, "salida.3mf"))
	if err != nil {
		t.Fatal(err)
	}
	zr.Close()
}

func TestExportCheckpointCancelAndAddExtension(t *testing.T) {
	a, id, name := exportRig(t)
	res, err := a.exportCheckpoint(id, name, exporter.Format3MF, func(string, string, string) (string, error) { return "", nil })
	if err != nil || res.Path != "" || len(res.Parts) != 2 {
		t.Errorf("cancelar: %+v err=%v", res, err)
	}
	// Sin extensión (Windows no siempre la agrega): se completa.
	dest := filepath.Join(t.TempDir(), "sinext")
	res, err = a.exportCheckpoint(id, name, exporter.Format3MF, func(string, string, string) (string, error) { return dest, nil })
	if err != nil || res.Path != dest+".3mf" {
		t.Errorf("extensión: %+v err=%v", res, err)
	}
	if _, err := os.Stat(dest + ".3mf"); err != nil {
		t.Error(err)
	}
}

func TestPreviewExportParts(t *testing.T) {
	a, id, name := exportRig(t)
	p, err := a.PreviewExportParts(id, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Parts) != 2 || p.Parts[0].Color != "#FF0000" || p.Parts[1].Color != "#0000FF" || p.Parts[1].Extruder != 2 {
		t.Errorf("partes: %+v", p.Parts)
	}
	if p.Warnings == nil || len(p.Warnings) != 0 {
		t.Errorf("avisos: %#v", p.Warnings)
	}
}

func TestExportCheckpointRejectsBadInput(t *testing.T) {
	a, id, name := exportRig(t)
	never := func(string, string, string) (string, error) { t.Fatal("no debía pedir ruta"); return "", nil }
	if _, err := a.exportCheckpoint(id, name, "obj", never); err == nil {
		t.Error("formato desconocido")
	}
	if _, err := a.exportCheckpoint(id, "../x.scad", exporter.FormatSTL, never); err == nil {
		t.Error("nombre con carpeta")
	}
	if _, err := a.exportCheckpoint(id, "009-no-existe.scad", exporter.FormatSTL, never); err == nil || !strings.Contains(err.Error(), "009-no-existe.scad") {
		t.Errorf("checkpoint inexistente: %v", err)
	}
	// Un modelo roto falla antes de abrir el diálogo.
	w, _ := a.store.GetWorkspace(id)
	os.WriteFile(filepath.Join(w.Path, "002-roto.scad"), []byte("cube(10;\n"), 0o644)
	if _, err := a.exportCheckpoint(id, "002-roto.scad", exporter.Format3MF, never); err == nil {
		t.Error("modelo roto")
	}
}

// --- Abrir en Bambu Studio -------------------------------------------------
// Ninguna prueba abre una aplicación: launchApp se reemplaza por un registrador
// (regla 9 de .claude/rules/technical.md).

type launchCall struct {
	name string
	args []string
}

// recordLaunch reemplaza launchApp y devuelve lo que se le pidió lanzar.
func recordLaunch(t *testing.T) *[]launchCall {
	t.Helper()
	var calls []launchCall
	old := launchApp
	launchApp = func(name string, args ...string) error {
		calls = append(calls, launchCall{name, args})
		return nil
	}
	t.Cleanup(func() { launchApp = old })
	return &calls
}

// fakeSlicer crea el «Bambu Studio» falso de este sistema y devuelve su ruta.
func fakeSlicer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		p := filepath.Join(dir, "bambu-studio.exe")
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := filepath.Join(dir, "BambuStudio.app")
	if err := os.MkdirAll(filepath.Join(p, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func slicerRig(t *testing.T) (*App, string) {
	t.Helper()
	a := newTestApp(t)
	s, _ := a.store.GetSettings()
	s.BambuStudioPath = fakeSlicer(t)
	if err := a.store.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	return a, s.BambuStudioPath
}

func TestOpenInSlicerArguments(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("solo macOS y Windows")
	}
	a, app := slicerRig(t)
	calls := recordLaunch(t)
	file := filepath.Join(t.TempDir(), "pieza.3mf")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenInSlicer("bambu-studio", file); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("lanzamientos: %+v", *calls)
	}
	got := (*calls)[0]
	var want launchCall
	if runtime.GOOS == "darwin" {
		want = launchCall{"open", []string{"-a", app, file}}
	} else {
		want = launchCall{app, []string{file}}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lanzó %+v, quería %+v", got, want)
	}
	// Un .stl también se acepta, con mayúsculas.
	stl := filepath.Join(t.TempDir(), "PIEZA.STL")
	os.WriteFile(stl, []byte("x"), 0o644)
	if err := a.OpenInSlicer("bambu-studio", stl); err != nil {
		t.Errorf("stl: %v", err)
	}
}

// El comando de cada sistema se verifica sin importar en cuál corre el test.
func TestSlicerLaunchPerOS(t *testing.T) {
	name, args, err := slicerLaunch("darwin", "/Applications/BambuStudio.app", "/tmp/a.3mf")
	if err != nil || name != "open" || !reflect.DeepEqual(args, []string{"-a", "/Applications/BambuStudio.app", "/tmp/a.3mf"}) {
		t.Errorf("darwin: %q %v %v", name, args, err)
	}
	name, args, err = slicerLaunch("windows", `C:\Program Files\Bambu Studio\bambu-studio.exe`, `C:\x\a.3mf`)
	if err != nil || name != `C:\Program Files\Bambu Studio\bambu-studio.exe` || !reflect.DeepEqual(args, []string{`C:\x\a.3mf`}) {
		t.Errorf("windows: %q %v %v", name, args, err)
	}
	if _, _, err := slicerLaunch("linux", "x", "y"); err == nil {
		t.Error("linux no está soportado")
	}
	// Nunca flags de la CLI de Bambu.
	for _, goos := range []string{"darwin", "windows"} {
		_, args, _ := slicerLaunch(goos, "app", "file")
		for _, a := range args {
			if strings.HasPrefix(a, "--") {
				t.Errorf("%s: flag de CLI %q", goos, a)
			}
		}
	}
}

func TestOpenInSlicerRejects(t *testing.T) {
	a, _ := slicerRig(t)
	calls := recordLaunch(t)
	dir := t.TempDir()
	ok := filepath.Join(dir, "a.3mf")
	os.WriteFile(ok, []byte("x"), 0o644)
	txt := filepath.Join(dir, "a.txt")
	os.WriteFile(txt, []byte("x"), 0o644)
	scad := filepath.Join(dir, "a.scad")
	os.WriteFile(scad, []byte("x"), 0o644)
	for name, c := range map[string]struct{ id, path string }{
		"inexistente":     {"bambu-studio", filepath.Join(dir, "no-existe.3mf")},
		"extensión .txt":  {"bambu-studio", txt},
		"extensión .scad": {"bambu-studio", scad},
		"sin extensión":   {"bambu-studio", filepath.Join(dir, "a")},
		"ruta relativa":   {"bambu-studio", "a.3mf"},
		"opción de CLI":   {"bambu-studio", "--slice.3mf"},
		"carpeta":         {"bambu-studio", filepath.Join(dir, "carpeta.3mf")},
		"slicer inválido": {"orca", ok},
	} {
		if name == "carpeta" {
			os.MkdirAll(c.path, 0o755)
		}
		if err := a.OpenInSlicer(c.id, c.path); err == nil {
			t.Errorf("%s: debía fallar", name)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("no debía lanzar nada: %+v", *calls)
	}
}

func TestOpenInSlicerNotInstalled(t *testing.T) {
	a := newTestApp(t)
	// Sin ruta configurada y sin instalación en las rutas habituales no hay nada
	// que abrir; si esta máquina sí tiene Bambu Studio, se prueba con una ruta
	// configurada que no existe y se ignora.
	s, _ := a.store.GetSettings()
	s.BambuStudioPath = filepath.Join(t.TempDir(), "no-existe", "BambuStudio.app")
	a.store.SaveSettings(s)
	calls := recordLaunch(t)
	file := filepath.Join(t.TempDir(), "a.3mf")
	os.WriteFile(file, []byte("x"), 0o644)
	if sl := a.DetectSlicers(); len(sl) != 1 || sl[0].ID != "bambu-studio" || sl[0].Name != "Bambu Studio" {
		t.Fatalf("slicers: %+v", sl)
	} else if !sl[0].Found {
		if err := a.OpenInSlicer("bambu-studio", file); err == nil {
			t.Error("Bambu Studio no está: debía fallar")
		}
		if len(*calls) != 0 {
			t.Errorf("no debía lanzar nada: %+v", *calls)
		}
	}
}

func TestDetectBambuStudio(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		os.MkdirAll(p, 0o755)
		return p
	}
	a, b := mk("a.app"), mk("b.app")
	missing := filepath.Join(dir, "falta.app")
	if got := detectBambuStudio("darwin", "", []string{missing, b, a}); got != b {
		t.Errorf("primera existente: %q", got)
	}
	if got := detectBambuStudio("darwin", a, []string{b}); got != a {
		t.Errorf("la configurada gana: %q", got)
	}
	if got := detectBambuStudio("darwin", missing, []string{b}); got != b {
		t.Errorf("configurada inexistente se ignora: %q", got)
	}
	if got := detectBambuStudio("darwin", "", []string{missing}); got != "" {
		t.Errorf("ninguna: %q", got)
	}
	// El ejecutable de dentro del .app se lleva al propio .app.
	inner := filepath.Join(a, "Contents", "MacOS", "BambuStudio")
	os.MkdirAll(filepath.Dir(inner), 0o755)
	os.WriteFile(inner, []byte("x"), 0o755)
	if got := detectBambuStudio("darwin", inner, nil); got != a {
		t.Errorf("bundle: %q", got)
	}
	if c := bambuStudioCandidates("darwin", "/Users/x", ""); len(c) != 2 || c[0] != "/Applications/BambuStudio.app" || c[1] != "/Users/x/Applications/BambuStudio.app" {
		t.Errorf("candidatos macOS: %v", c)
	}
	if c := bambuStudioCandidates("windows", "", "D:/PF"); len(c) != 1 || !strings.HasSuffix(filepath.ToSlash(c[0]), "Bambu Studio/bambu-studio.exe") {
		t.Errorf("candidatos Windows: %v", c)
	}
}

func TestRevealPath(t *testing.T) {
	a := newTestApp(t)
	calls := recordLaunch(t)
	f := filepath.Join(t.TempDir(), "x.3mf")
	os.WriteFile(f, []byte("x"), 0o644)
	if err := a.RevealPath(filepath.Join(t.TempDir(), "no.3mf")); err == nil {
		t.Error("ruta inexistente")
	}
	if err := a.RevealPath("relativa.3mf"); err == nil {
		t.Error("ruta relativa")
	}
	if len(*calls) != 0 {
		t.Fatalf("no debía lanzar nada: %+v", *calls)
	}
	if err := a.RevealPath(f); err != nil || len(*calls) != 1 {
		t.Fatalf("revelar: %v %+v", err, *calls)
	}
	if name, args := revealLaunch("darwin", "/tmp/x.3mf"); name != "open" || !reflect.DeepEqual(args, []string{"-R", "/tmp/x.3mf"}) {
		t.Errorf("darwin: %q %v", name, args)
	}
	if name, args := revealLaunch("windows", `C:\x\a.3mf`); name != "explorer.exe" || !reflect.DeepEqual(args, []string{`/select,C:\x\a.3mf`}) {
		t.Errorf("windows: %q %v", name, args)
	}
}
