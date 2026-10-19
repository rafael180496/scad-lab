package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scadlab/backend/appdata"
	"scadlab/backend/detect"
	"scadlab/backend/mesh"
	"scadlab/backend/render"
	"scadlab/backend/scad"
	"scadlab/backend/scadwasm"
	"scadlab/backend/threemf"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func scadFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".scad") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestWriteStarterCreatesTheFirstCheckpoint(t *testing.T) {
	for _, lang := range []string{"es", "en", "", "fr"} {
		dir := t.TempDir()
		name, err := WriteStarter(dir, "Soporte de Auriculares", lang)
		if err != nil {
			t.Fatalf("%q: %v", lang, err)
		}
		if name != "001-soporte-de-auriculares.scad" {
			t.Fatalf("%q: nombre %q", lang, name)
		}
		src := readFile(t, filepath.Join(dir, name))
		if first, _, _ := strings.Cut(src, "\n"); first != `// @checkpoint parent= desc="Soporte de Auriculares"` {
			t.Errorf("%q: primera línea %q", lang, first)
		}
		// Lo lee el escáner de checkpoints: sin padre y con la descripción.
		files, err := Scan(dir)
		if err != nil || len(files) != 1 || files[0].ParentFile != "" || files[0].Description != "Soporte de Auriculares" {
			t.Errorf("%q: Scan = %+v %v", lang, files, err)
		}
		if strings.Contains(src, "{{") || strings.Contains(src, "<no value>") {
			t.Errorf("%q: plantilla sin resolver", lang)
		}
		want := "knob"
		if lang == "es" {
			want = "pomo"
		}
		if !strings.Contains(strings.ToLower(src), want) {
			t.Errorf("%q: el idioma de los comentarios no es el pedido (busco %q)", lang, want)
		}
		// Lo que pide el contrato del starter.
		for _, must := range []string{"/* [", "/* [Hidden] */", "$fn", "module ", "color(", "// [", "assert("} {
			if !strings.Contains(src, must) {
				t.Errorf("%q: falta %q", lang, must)
			}
		}
	}
}

// El nombre del workspace llega al encabezado escapado y en una sola línea, y
// el slug del archivo sale igual que el de la carpeta.
func TestWriteStarterEscapesTheNameInTheHeader(t *testing.T) {
	cases := map[string]struct{ file, desc string }{
		`Caja "Pro" \ v2`:            {"001-caja-pro-v2.scad", `Caja "Pro" \ v2`},
		"Línea uno\nlínea dos\r\n":   {"001-linea-uno-linea-dos.scad", "Línea uno línea dos"},
		"???":                        {"001-model.scad", "???"},
		"   ":                        {"001-model.scad", "ScadLab"},
		strings.Repeat("ñandú ", 40): {"", string([]rune(strings.Repeat("ñandú ", 40))[:80])},
	}
	for in, want := range cases {
		dir := t.TempDir()
		name, err := WriteStarter(dir, in, "es")
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if want.file != "" && name != want.file {
			t.Errorf("%q: archivo %q, quería %q", in, name, want.file)
		}
		first, _, _ := strings.Cut(readFile(t, filepath.Join(dir, name)), "\n")
		if !headerFullRe2(first) {
			t.Errorf("%q: cabecera mal formada: %q", in, first)
		}
		files, _ := Scan(dir)
		if len(files) != 1 || files[0].Description != want.desc {
			t.Errorf("%q: desc %q, quería %q", in, files[0].Description, want.desc)
		}
	}
}

// El mismo formato que exige la regla SL001 del linter.
func headerFullRe2(line string) bool {
	ds := scad.Lint(context.Background(), line+"\n", scad.LintOptions{WorkspaceDir: os.TempDir(), FileName: "001-x.scad", ExistingFiles: []string{}})
	for _, d := range ds {
		if d.Code == scad.CodeHeader {
			return false
		}
	}
	return true
}

// Con cualquier .scad en la raíz no se escribe nada y nada cambia.
func TestWriteStarterLeavesFoldersWithModelsAlone(t *testing.T) {
	for _, existing := range []string{"001-mio.scad", "modelo.scad", "MODELO.SCAD", "999-x.Scad"} {
		dir := t.TempDir()
		mine := filepath.Join(dir, existing)
		const body = "// mi modelo\ncube(7);\n"
		if err := os.WriteFile(mine, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		before, _ := os.Stat(mine)
		name, err := WriteStarter(dir, "Caja", "es")
		if err != nil || name != "" {
			t.Errorf("%s: (%q, %v), esperaba vacío y sin error", existing, name, err)
		}
		if got := scadFiles(t, dir); len(got) != 1 || got[0] != existing {
			t.Errorf("%s: archivos %v", existing, got)
		}
		after, _ := os.Stat(mine)
		if readFile(t, mine) != body || !after.ModTime().Equal(before.ModTime()) {
			t.Errorf("%s: se tocó el archivo del usuario", existing)
		}
	}
	// Un .scad dentro de una subcarpeta no cuenta: Hermes y el árbol miran la raíz.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", "util.scad"), []byte("module m(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if name, err := WriteStarter(dir, "Con lib", "en"); err != nil || name != "001-con-lib.scad" {
		t.Errorf("con un .scad solo en lib/: (%q, %v)", name, err)
	}
	// Segunda llamada: ya hay uno (el que se acaba de escribir), no se duplica.
	if name, err := WriteStarter(dir, "Con lib", "en"); err != nil || name != "" {
		t.Errorf("segunda llamada: (%q, %v)", name, err)
	}
}

// Nunca pisa: si algo ocupa el nombre del starter (aquí una carpeta, que Scan
// no cuenta como .scad) se devuelve un error y el contenido sigue ahí.
func TestWriteStarterNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	squatter := filepath.Join(dir, "001-caja.scad")
	if err := os.MkdirAll(squatter, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(squatter, "dentro.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if name, err := WriteStarter(dir, "Caja", "es"); err == nil || name != "" {
		t.Errorf("(%q, %v): debía fallar y no devolver nombre", name, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("se perdió el contenido")
	}
	// Una carpeta que no existe es un error, no una carpeta creada.
	if _, err := WriteStarter(filepath.Join(dir, "no-existe"), "Caja", "es"); err == nil {
		t.Error("una carpeta inexistente debía fallar")
	}
	if _, err := os.Stat(filepath.Join(dir, "no-existe")); err == nil {
		t.Error("se creó la carpeta")
	}
}

// El starter pasa el linter sin un solo hallazgo, en los dos idiomas: con las
// reglas propias (SL001–SL009) y con el chequeo de OpenSCAD, el nativo si está
// y el integrado.
func TestStarterPassesTheLinter(t *testing.T) {
	t.Setenv(appdata.OverrideEnv, t.TempDir())
	var native string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if tool := detect.OpenSCAD(ctx, ""); tool.Found {
		native = tool.Path
	}
	wasm := scadwasm.New("")
	defer wasm.Close()

	for _, lang := range []string{"es", "en"} {
		dir := t.TempDir()
		name, err := WriteStarter(dir, "Caja de prueba", lang)
		if err != nil {
			t.Fatal(err)
		}
		src := readFile(t, filepath.Join(dir, name))
		files, _ := Scan(dir)
		names := []string{files[0].FileName}
		for label, o := range map[string]scad.LintOptions{
			"solo reglas": {WorkspaceDir: dir, FileName: name, ExistingFiles: names},
			"nativo":      {WorkspaceDir: dir, FileName: name, ExistingFiles: names, OpenSCADPath: native},
			"wasm":        {WorkspaceDir: dir, FileName: name, ExistingFiles: names, WASM: wasm},
		} {
			if label == "nativo" && native == "" {
				continue
			}
			if label == "wasm" && !wasm.Available() {
				continue
			}
			if ds := scad.Lint(ctx, src, o); len(ds) != 0 {
				t.Errorf("%s/%s: hallazgos del linter: %+v", lang, label, ds)
			}
		}
	}
}

// Renderiza de verdad, con el OpenSCAD instalado y con el motor integrado: sale
// una malla cerrada de 50 × 50 × 20 mm apoyada en z = 0, sin avisos.
func TestStarterRenders(t *testing.T) {
	t.Setenv(appdata.OverrideEnv, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	native := detect.OpenSCAD(ctx, "")
	wasm := scadwasm.New("")
	defer wasm.Close()

	engines := map[string]render.Options{}
	if native.Found {
		engines["nativo"] = render.Options{OpenSCADPath: native.Path, Backend: "manifold"}
	} else {
		t.Log("OpenSCAD no está instalado: solo se prueba el motor integrado")
	}
	if wasm.Available() {
		engines["wasm"] = render.Options{WASM: wasm, Backend: "manifold"}
	}
	if len(engines) == 0 {
		t.Skip("ni OpenSCAD ni el motor integrado")
	}
	eng, err := render.NewEngine(filepath.Join(t.TempDir(), "meshes"))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	for label, opts := range engines {
		for _, lang := range []string{"es", "en"} {
			dir := t.TempDir()
			name, err := WriteStarter(dir, "Caja de prueba", lang)
			if err != nil {
				t.Fatal(err)
			}
			res, err := eng.Render(ctx, render.Request{WorkspaceKey: label + lang, WorkspaceDir: dir, FileName: name}, opts)
			if err != nil {
				t.Fatalf("%s/%s: %v", label, lang, err)
			}
			if res.Status != render.StatusOK {
				t.Fatalf("%s/%s: estado %q, diagnósticos %+v\n%s", label, lang, res.Status, res.Diagnostics, res.Log)
			}
			if len(res.Diagnostics) != 0 {
				t.Errorf("%s/%s: avisos del render: %+v", label, lang, res.Diagnostics)
			}
			info, err := mesh.InfoFromFile(res.MeshPath)
			if err != nil {
				t.Fatal(err)
			}
			// La cúpula es una esfera facetada: su cima queda unas décimas bajo los 20 mm.
			if !info.Watertight || info.Size[0] != 50 || info.Size[1] != 50 || info.Size[2] < 19.9 || info.Size[2] > 20 || info.Min[2] != 0 {
				t.Errorf("%s/%s: malla %+v", label, lang, info)
			}
		}
	}
}

// Con el OpenSCAD instalado el 3MF sale con las dos partes, una por color.
func TestStarterExportsTwoColors(t *testing.T) {
	t.Setenv(appdata.OverrideEnv, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tool := detect.OpenSCAD(ctx, "")
	if !tool.Found {
		t.Skip("OpenSCAD no está instalado")
	}
	dir := t.TempDir()
	name, err := WriteStarter(dir, "Dos colores", "en")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "salida.3mf")
	cmd := exec.CommandContext(ctx, tool.Path, "-o", out, name)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("este OpenSCAD no exporta 3MF: %v\n%s", err, b)
	}
	m, err := threemf.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	parts := threemf.Split(m)
	if len(parts) != 2 || parts[0].Color.Hex() != "#2F6FDE" || parts[1].Color.Hex() != "#F2B134" {
		t.Errorf("partes del 3MF: %+v", parts)
	}
}
