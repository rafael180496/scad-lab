package scaddownload

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Un dmg de verdad (creado con hdiutil) con un «OpenSCAD» de mentira que
// imprime la versión: prueba montar, copiar con ditto, desmontar y validar con
// `--version` sin bajar 60 MB. Solo en macOS.
func TestInstallDMG(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("hdiutil solo existe en macOS")
	}
	src := t.TempDir()
	macos := filepath.Join(src, "OpenSCAD.app", "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'OpenSCAD version " + Version + "' >&2\n"
	if err := os.WriteFile(filepath.Join(macos, "OpenSCAD"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dmgPath := filepath.Join(t.TempDir(), "fake.dmg")
	if out, err := exec.Command("/usr/bin/hdiutil", "create", "-quiet", "-srcfolder", src, "-fs", "HFS+", "-volname", "OpenSCAD", dmgPath).CombinedOutput(); err != nil {
		t.Skipf("no se pudo crear el dmg de prueba: %v %s", err, out)
	}
	body, err := os.ReadFile(dmgPath)
	if err != nil {
		t.Fatal(err)
	}
	a := testAsset(t, body)
	a.OS, a.Arch, a.Kind = "darwin", runtime.GOARCH, KindDMG

	data := t.TempDir()
	bin, err := Install(context.Background(), data, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bin != InstalledBinary(data) || !strings.HasSuffix(bin, filepath.Join("OpenSCAD.app", "Contents", "MacOS", "OpenSCAD")) {
		t.Errorf("binario = %q (InstalledBinary %q)", bin, InstalledBinary(data))
	}
	if got := entries(t, toolsDir(data)); len(got) != 1 || got[0] != Version {
		t.Errorf("contenido = %v", got)
	}
	// El volumen quedó desmontado: ningún montaje bajo la carpeta de trabajo.
	if out, _ := exec.Command("/sbin/mount").Output(); strings.Contains(string(out), ".tmp-") {
		t.Error("el dmg quedó montado")
	}

	// Un binario que no dice la versión esperada no se instala.
	if err := Remove(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macos, "OpenSCAD"), []byte("#!/bin/sh\necho 'OpenSCAD version 1.0' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dmg2 := filepath.Join(t.TempDir(), "fake2.dmg")
	if out, err := exec.Command("/usr/bin/hdiutil", "create", "-quiet", "-srcfolder", src, "-fs", "HFS+", "-volname", "OpenSCAD", dmg2).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil: %v %s", err, out)
	}
	body2, _ := os.ReadFile(dmg2)
	a2 := testAsset(t, body2)
	a2.OS, a2.Arch, a2.Kind = "darwin", runtime.GOARCH, KindDMG
	if _, err := Install(context.Background(), data, a2, nil); err == nil {
		t.Fatal("una versión distinta de la fijada no debería instalarse")
	}
	assertNothingLeft(t, data)
}
