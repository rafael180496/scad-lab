package slicers

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func mkfile(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func def(t *testing.T, id string) Def {
	t.Helper()
	d, ok := ByID(id)
	if !ok {
		t.Fatalf("el slicer %q no está en el registro", id)
	}
	return d
}

// El registro trae los 8 ids del spec, sin repetir, y cada entrada dice dónde
// buscar en los dos sistemas.
func TestRegistry(t *testing.T) {
	want := []string{"bambu-studio", "orcaslicer", "prusaslicer", "creality-print", "elegoo-slicer", "anycubic-slicer-next", "snapmaker-orca", "ultimaker-cura"}
	var got []string
	seen := map[string]bool{}
	for _, d := range Registry {
		got = append(got, d.ID)
		if seen[d.ID] {
			t.Errorf("id repetido %s", d.ID)
		}
		seen[d.ID] = true
		if d.Name == "" || len(d.Mac) == 0 || len(d.Win) == 0 {
			t.Errorf("%s incompleto: %+v", d.ID, d)
		}
		for _, w := range d.Win {
			if strings.Contains(w, `\`) {
				t.Errorf("%s: %q usa «\\»; las rutas de Windows van con «/»", d.ID, w)
			}
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("registro %v, esperaba %v", got, want)
	}
	if _, ok := ByID("no-existe"); ok {
		t.Error("ByID inventó un slicer")
	}
}

func TestFindOnMac(t *testing.T) {
	apps, home := t.TempDir(), t.TempDir()
	// Applications apunta a una carpeta falsa: la /Applications real de esta
	// máquina puede tener los slicers de verdad.
	sys := filepath.Join(t.TempDir(), "Applications")
	env := Env{GOOS: "darwin", Home: home, Applications: sys}
	d := def(t, "orcaslicer")
	// Nada instalado.
	if got := Find(d, env, ""); got != "" {
		t.Errorf("sin instalación: %q", got)
	}
	// Candidatos: /Applications y ~/Applications, en ese orden.
	c := d.Candidates(env)
	if c[0] != filepath.Join(sys, "OrcaSlicer.app") || c[1] != filepath.Join(home, "Applications", "OrcaSlicer.app") {
		t.Errorf("candidatos: %v", c)
	}
	// Instalado en ~/Applications.
	app := filepath.Join(home, "Applications", "OrcaSlicer.app")
	mkdirs(t, filepath.Join(app, "Contents", "MacOS"))
	if got := Find(d, env, ""); got != app {
		t.Errorf("~/Applications: %q", got)
	}
	// Una ruta configurada que existe gana sobre lo autodetectado.
	custom := filepath.Join(apps, "Mi Orca.app")
	mkdirs(t, filepath.Join(custom, "Contents", "MacOS"))
	if got := Find(d, env, custom); got != custom {
		t.Errorf("la configurada debía ganar: %q", got)
	}
	// Apuntada al ejecutable de dentro, se lleva al .app.
	inner := mkfile(t, filepath.Join(custom, "Contents", "MacOS", "OrcaSlicer"))
	if got := Find(d, env, inner); got != custom {
		t.Errorf("bundle: %q", got)
	}
	// Una configurada que no existe se ignora y se autodetecta.
	if got := Find(d, env, filepath.Join(apps, "no-esta.app")); got != app {
		t.Errorf("configurada inexistente: %q", got)
	}
	// Los espacios en blanco de lo configurado no cuentan.
	if got := Find(d, env, "   "); got != app {
		t.Errorf("configurada en blanco: %q", got)
	}
}

func TestFindOnMacWildcardAndVariants(t *testing.T) {
	home := t.TempDir()
	env := Env{GOOS: "darwin", Home: home, Applications: filepath.Join(t.TempDir(), "Applications")}
	// Un comodín en el nombre del .app: gana la versión más nueva en orden natural
	// (el registro no lo usa hoy en macOS, pero la tabla lo admite).
	mkdirs(t,
		filepath.Join(home, "Applications", "Algo 6.9.app"),
		filepath.Join(home, "Applications", "Algo 6.10.app"),
	)
	want := filepath.Join(home, "Applications", "Algo 6.10.app")
	if got := Find(Def{ID: "x", Mac: []string{"Algo *.app"}}, env, ""); got != want {
		t.Errorf("comodín: %q, esperaba %q", got, want)
	}
	// Bambu Studio se llama BambuStudio.app y también Bambu Studio.app.
	mkdirs(t, filepath.Join(home, "Applications", "Bambu Studio.app"))
	if got := Find(def(t, "bambu-studio"), env, ""); got != filepath.Join(home, "Applications", "Bambu Studio.app") {
		t.Errorf("variante con espacio: %q", got)
	}
}

// Los nombres de instalación que salieron de la investigación del fabricante
// (S14): cada slicer se encuentra donde su instalador lo deja.
func TestFindsEachSlicerWhereItsInstallerPutsIt(t *testing.T) {
	mac := Env{GOOS: "darwin", Home: t.TempDir(), Applications: t.TempDir()}
	for id, app := range map[string]string{
		"bambu-studio": "BambuStudio.app", "orcaslicer": "OrcaSlicer.app", "prusaslicer": "PrusaSlicer.app",
		"creality-print": "CrealityPrint.app", "elegoo-slicer": "ElegooSlicer.app",
		"snapmaker-orca": "Snapmaker_Orca.app", "ultimaker-cura": "UltiMaker Cura.app",
	} {
		p := filepath.Join(mac.Applications, app)
		mkdirs(t, p)
		if got := Find(def(t, id), mac, ""); got != p {
			t.Errorf("macOS %s: %q, esperaba %q", id, got, p)
		}
	}
	pf := t.TempDir()
	win := Env{GOOS: "windows", ProgramFiles: pf}
	for id, rel := range map[string]string{
		"bambu-studio": "Bambu Studio/bambu-studio.exe", "orcaslicer": "OrcaSlicer/orca-slicer.exe",
		"prusaslicer":    "Prusa3D/PrusaSlicer/prusa-slicer.exe",
		"creality-print": "Creality/Creality Print 7.0/CrealityPrint.exe", "elegoo-slicer": "ElegooSlicer/elegoo-slicer.exe",
		"snapmaker-orca": "Snapmaker_Orca/snapmaker-orca.exe", "ultimaker-cura": "UltiMaker Cura 5.13.0/UltiMaker-Cura.exe",
	} {
		p := mkfile(t, filepath.Join(pf, filepath.FromSlash(rel)))
		if got := Find(def(t, id), win, ""); got != p {
			t.Errorf("Windows %s: %q, esperaba %q", id, got, p)
		}
	}
}

func TestFindOnWindowsWithVersionedFolders(t *testing.T) {
	pf, pf86, local := t.TempDir(), t.TempDir(), t.TempDir()
	env := Env{GOOS: "windows", ProgramFiles: pf, ProgramFilesX86: pf86, LocalAppData: local}
	d := def(t, "ultimaker-cura")
	if got := Find(d, env, ""); got != "" {
		t.Errorf("sin instalación: %q", got)
	}
	mkfile(t, filepath.Join(pf, "UltiMaker Cura 5.9.0", "UltiMaker-Cura.exe"))
	newest := mkfile(t, filepath.Join(pf, "UltiMaker Cura 5.10.1", "UltiMaker-Cura.exe"))
	mkfile(t, filepath.Join(pf, "UltiMaker Cura 5.2.0", "UltiMaker-Cura.exe"))
	if got := Find(d, env, ""); got != newest {
		t.Errorf("la versión más nueva: %q, esperaba %q", got, newest)
	}
	// Sin comodín: ruta exacta bajo Program Files.
	prusa := mkfile(t, filepath.Join(pf, "Prusa3D", "PrusaSlicer", "prusa-slicer.exe"))
	if got := Find(def(t, "prusaslicer"), env, ""); got != prusa {
		t.Errorf("PrusaSlicer: %q", got)
	}
	// Instalación por usuario: %LocalAppData%\Programs.
	orca := mkfile(t, filepath.Join(local, "Programs", "OrcaSlicer", "orca-slicer.exe"))
	if got := Find(def(t, "orcaslicer"), env, ""); got != orca {
		t.Errorf("LocalAppData: %q", got)
	}
	// Y la de 32 bits.
	elegoo := mkfile(t, filepath.Join(pf86, "ElegooSlicer", "elegoo-slicer.exe"))
	if got := Find(def(t, "elegoo-slicer"), env, ""); got != elegoo {
		t.Errorf("Program Files (x86): %q", got)
	}
	// La configurada gana.
	mine := mkfile(t, filepath.Join(t.TempDir(), "cura.exe"))
	if got := Find(d, env, mine); got != mine {
		t.Errorf("la configurada debía ganar: %q", got)
	}
}

func TestCandidatesWindowsDefaultAndUnsupportedOS(t *testing.T) {
	c := def(t, "bambu-studio").Candidates(Env{GOOS: "windows"})
	if len(c) == 0 || !strings.HasPrefix(c[0], `C:\Program Files`) {
		t.Errorf("sin %%ProgramFiles%% se asume C:\\Program Files: %v", c)
	}
	if c := def(t, "bambu-studio").Candidates(Env{GOOS: "linux"}); len(c) != 0 {
		t.Errorf("linux no está soportado: %v", c)
	}
	if got := Find(def(t, "bambu-studio"), Env{GOOS: "linux"}, ""); got != "" {
		t.Errorf("linux: %q", got)
	}
}

// El comando de cada sistema se arma sin lanzar nada y sin opciones de la CLI del
// slicer.
func TestLaunchCommand(t *testing.T) {
	name, args, err := LaunchCommand("darwin", "/Applications/OrcaSlicer.app", "/tmp/a.3mf")
	if err != nil || name != "open" || !reflect.DeepEqual(args, []string{"-a", "/Applications/OrcaSlicer.app", "/tmp/a.3mf"}) {
		t.Errorf("darwin: %q %v %v", name, args, err)
	}
	name, args, err = LaunchCommand("windows", `C:\Program Files\OrcaSlicer\orca-slicer.exe`, `C:\x\a.3mf`)
	if err != nil || name != `C:\Program Files\OrcaSlicer\orca-slicer.exe` || !reflect.DeepEqual(args, []string{`C:\x\a.3mf`}) {
		t.Errorf("windows: %q %v %v", name, args, err)
	}
	if _, _, err := LaunchCommand("linux", "x", "y"); err == nil {
		t.Error("linux no está soportado")
	}
	for _, goos := range []string{"darwin", "windows"} {
		_, args, _ := LaunchCommand(goos, "app", "file")
		for _, a := range args {
			if strings.HasPrefix(a, "--") {
				t.Errorf("%s: opción de CLI %q", goos, a)
			}
		}
	}
}

func TestAppBundlePath(t *testing.T) {
	sep := string(filepath.Separator)
	in := "/Applications/X.app" + sep + "Contents" + sep + "MacOS" + sep + "X"
	if got := AppBundlePath(in); got != "/Applications/X.app" {
		t.Errorf("%q", got)
	}
	if got := AppBundlePath("/Applications/X.app"); got != "/Applications/X.app" {
		t.Errorf("%q", got)
	}
	if got := AppBundlePath(`C:\p\x.exe`); got != `C:\p\x.exe` {
		t.Errorf("%q", got)
	}
}

func TestNaturalOrder(t *testing.T) {
	in := []string{"v5.10", "v5.9", "v5.2", "v10.0", "v1.0", "V5.9a"}
	sorted := append([]string(nil), in...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if naturalLess(sorted[j], sorted[i]) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	want := []string{"v1.0", "v5.2", "v5.9", "V5.9a", "v5.10", "v10.0"}
	if !reflect.DeepEqual(sorted, want) {
		t.Errorf("orden natural: %v", sorted)
	}
}

// El recomendado es el primero de la lista que el registro conoce: un id mal
// escrito se salta en vez de dejar a la impresora sin recomendación.
func TestRecommended(t *testing.T) {
	d, ok := Recommended([]string{"prusaslicer", "orcaslicer"})
	if !ok || d.ID != "prusaslicer" || d.Name != "PrusaSlicer" {
		t.Errorf("primero conocido: %+v %v", d, ok)
	}
	d, ok = Recommended([]string{"no-existe", "orcaslicer"})
	if !ok || d.ID != "orcaslicer" {
		t.Errorf("salta el desconocido: %+v %v", d, ok)
	}
	if _, ok := Recommended(nil); ok {
		t.Error("sin slicers no hay recomendado")
	}
	if _, ok := Recommended([]string{"x", "y"}); ok {
		t.Error("ninguno conocido: no hay recomendado")
	}
}
