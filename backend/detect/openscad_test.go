package detect

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"scadlab/backend/scaddownload"
)

// fakeScad escribe un «openscad» de mentira que dice su versión por stderr.
func fakeScad(t *testing.T, path, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'OpenSCAD version "+version+"' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// openscadEnv aísla la prueba: datos y HOME temporales, PATH con un openscad
// «del sistema» y sin la variable de prueba. Devuelve (datos, ruta del PATH).
func openscadEnv(t *testing.T) (data, onPath string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("el OpenSCAD descargado se simula con el layout de macOS")
	}
	data = t.TempDir()
	t.Setenv("SCADLAB_DATA_DIR", data)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(NoSystemOpenSCADEnv, "")
	bin := t.TempDir()
	onPath = filepath.Join(bin, "openscad")
	fakeScad(t, onPath, "2021.01")
	t.Setenv("PATH", bin)
	return data, onPath
}

func installManaged(t *testing.T, data string) string {
	t.Helper()
	p := filepath.Join(scaddownload.InstallDir(data), "OpenSCAD.app", "Contents", "MacOS", "OpenSCAD")
	fakeScad(t, p, scaddownload.Version)
	return p
}

// Sin nada fijado, el descargado gana al del PATH.
func TestOpenSCADManagedBeatsPath(t *testing.T) {
	data, _ := openscadEnv(t)
	managed := installManaged(t, data)
	got := OpenSCAD(context.Background(), "")
	if !got.Found || got.Path != managed || got.Source != SourceManaged || got.Version != scaddownload.Version {
		t.Fatalf("tool = %+v, quería el descargado %s", got, managed)
	}
}

// La ruta fijada por el usuario gana a todo, incluido el descargado.
func TestOpenSCADCustomBeatsManaged(t *testing.T) {
	data, _ := openscadEnv(t)
	installManaged(t, data)
	custom := filepath.Join(t.TempDir(), "mi-openscad")
	fakeScad(t, custom, "2024.05")
	got := OpenSCAD(context.Background(), custom)
	if got.Path != custom || got.Source != SourceCustom || got.Version != "2024.05" {
		t.Fatalf("tool = %+v", got)
	}
	// Una ruta fijada que ya no existe no bloquea: cae al descargado.
	got = OpenSCAD(context.Background(), filepath.Join(t.TempDir(), "no-existe"))
	if got.Source != SourceManaged {
		t.Fatalf("con la ruta fijada rota debería usar el descargado: %+v", got)
	}
}

// Sin descargado, el del PATH es «system».
func TestOpenSCADSystemFromPath(t *testing.T) {
	_, onPath := openscadEnv(t)
	got := OpenSCAD(context.Background(), "")
	if !got.Found || got.Path != onPath || got.Source != SourceSystem {
		t.Fatalf("tool = %+v, quería %s", got, onPath)
	}
}

// SCADLAB_NO_SYSTEM_OPENSCAD=1 ignora PATH y rutas del sistema, pero no el
// descargado ni la ruta fijada.
func TestOpenSCADNoSystemEnv(t *testing.T) {
	data, _ := openscadEnv(t)
	t.Setenv(NoSystemOpenSCADEnv, "1")
	if got := OpenSCAD(context.Background(), ""); got.Found {
		t.Fatalf("con la variable y sin descargado no debería encontrar nada: %+v", got)
	}
	managed := installManaged(t, data)
	if got := OpenSCAD(context.Background(), ""); got.Path != managed || got.Source != SourceManaged {
		t.Fatalf("el descargado debe seguir valiendo: %+v", got)
	}
	custom := filepath.Join(t.TempDir(), "mi-openscad")
	fakeScad(t, custom, "2024.05")
	if got := OpenSCAD(context.Background(), custom); got.Source != SourceCustom {
		t.Fatalf("la ruta fijada debe seguir valiendo: %+v", got)
	}
}

// Detectar no crea carpetas: sin SCADLAB_DATA_DIR no toca el directorio de
// datos real (aquí, un HOME temporal).
func TestOpenSCADDoesNotCreateDataDir(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("HOME define el directorio de datos solo en macOS")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SCADLAB_DATA_DIR", "")
	t.Setenv(NoSystemOpenSCADEnv, "1")
	if got := OpenSCAD(context.Background(), ""); got.Found {
		t.Fatalf("no debería encontrar nada: %+v", got)
	}
	if ents, _ := os.ReadDir(home); len(ents) != 0 {
		t.Errorf("detect creó archivos en HOME: %v", ents)
	}
}
