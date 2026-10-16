package scadwasm_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scadlab/backend/mesh"
	"scadlab/backend/render"
	"scadlab/backend/scadwasm"
	"scadlab/backend/threemf"
)

func TestAvailableAndVersion(t *testing.T) {
	if !shared.Available() {
		t.Fatal("el wasm embebido no está disponible o su SHA-256 no coincide")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, code, err := shared.Run(ctx, scadwasm.RunSpec{Args: []string{"--version"}})
	if err != nil || code != 0 {
		t.Fatalf("--version: code=%d err=%v out=%q", code, err, out)
	}
	if !strings.Contains(out, scadwasm.Version) {
		t.Fatalf("versión inesperada: %q (esperaba %s)", out, scadwasm.Version)
	}
	if (*scadwasm.Runner)(nil).Available() {
		t.Fatal("un Runner nil no puede estar disponible")
	}
}

// Todas las importaciones del wasm tienen nombre y función en el host: una
// versión nueva del wasm con una importación más lo delata aquí.
func TestImportsAreCovered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	missing, err := shared.UnimplementedImports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Fatalf("importaciones sin implementar: %v", missing)
	}
}

func TestCubeSTL(t *testing.T) {
	s := newScene(t, map[string]string{"001-cube.scad": "cube([10,20,30]);\n"})
	out, code, err := s.run(t, "-o", "/out/x.stl", "--export-format", "binstl", "--backend=manifold", "001-cube.scad")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	info, err := mesh.InfoFromFile(s.outFile("x.stl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Triangles != 12 || !info.Watertight {
		t.Fatalf("malla inesperada: %+v", info)
	}
	if info.Size != [3]float64{10, 20, 30} || info.VolumeMM3 < 5999.9 || info.VolumeMM3 > 6000.1 {
		t.Fatalf("medidas inesperadas: %+v", info)
	}
}

// Un solo Manifold con operaciones booleanas: ejercita excepciones de C++ y
// el crecimiento de la memoria del módulo.
func TestBooleanAndMemoryGrowth(t *testing.T) {
	src := `
difference() {
    union() { for (i = [0:15]) translate([i*7, 0, 0]) sphere(4, $fn = 48); }
    for (i = [0:15]) translate([i*7, 0, 0]) cylinder(r = 1.5, h = 20, center = true, $fn = 32);
}
`
	s := newScene(t, map[string]string{"001-b.scad": src})
	out, code, err := s.run(t, "-o", "/out/x.stl", "--export-format", "binstl", "--backend=manifold", "001-b.scad")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	info, err := mesh.InfoFromFile(s.outFile("x.stl"))
	if err != nil || !info.Watertight || info.Triangles < 1000 {
		t.Fatalf("malla inesperada: %+v err=%v", info, err)
	}
}

func TestSyntaxErrorHasLine(t *testing.T) {
	s := newScene(t, map[string]string{"001-bad.scad": "cube(1);\ncube(2);\ncube(;\n"})
	out, code, err := s.run(t, "-o", "/out/x.stl", "001-bad.scad")
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 {
		t.Fatalf("un error de sintaxis tiene que salir con código ≠ 0: %q", out)
	}
	ds := render.ParseOutput(out, s.ws, "001-bad.scad")
	var found bool
	for _, d := range ds {
		if d.Severity == render.SeverityError && d.Line == 3 && d.File == "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("falta el ERROR con línea 3 del propio checkpoint: %+v\nsalida: %q", ds, out)
	}
	if _, err := os.Stat(s.outFile("x.stl")); err == nil {
		t.Fatal("no debería haber STL")
	}
}

func TestAssertAndEchoAreReported(t *testing.T) {
	s := newScene(t, map[string]string{"001-a.scad": "echo(\"hola\");\nassert(false, \"boom\");\ncube(1);\n"})
	out, code, err := s.run(t, "-o", "/out/x.stl", "001-a.scad")
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 || !strings.Contains(out, "ECHO: \"hola\"") || !strings.Contains(out, "boom") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	ds := render.ParseOutput(out, s.ws, "001-a.scad")
	if len(ds) == 0 || ds[0].Line != 2 {
		t.Fatalf("diagnósticos: %+v", ds)
	}
	// Las rutas de la salida son las del disco, no las virtuales.
	if strings.Contains(out, "/work/") {
		t.Fatalf("quedó una ruta virtual en la salida: %q", out)
	}
}

func TestIncludeFromMountedLib(t *testing.T) {
	s := newScene(t, map[string]string{"001-i.scad": "include <caja.scad>\ncaja(5);\n"})
	lib := t.TempDir()
	if err := os.WriteFile(filepath.Join(lib, "caja.scad"), []byte("module caja(n) { cube(n); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := s.spec("-o", "/out/x.stl", "--export-format", "binstl", "001-i.scad")
	spec.Mounts = append(spec.Mounts, scadwasm.Mount{Host: lib, Virtual: "/lib/0", ReadOnly: true})
	spec.Env = map[string]string{"OPENSCADPATH": "/work:/work/lib:/lib/0"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, code, err := shared.Run(ctx, spec)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	if strings.Contains(out, "Can't find") {
		t.Fatalf("no encontró el include: %q", out)
	}
	info, err := mesh.InfoFromFile(s.outFile("x.stl"))
	if err != nil || info.Triangles != 12 {
		t.Fatalf("malla: %+v err=%v", info, err)
	}
}

// Un include que no existe es WARNING con la ruta y la línea; el render sigue.
func TestMissingIncludeIsWarningWithLine(t *testing.T) {
	s := newScene(t, map[string]string{"001-m.scad": "cube(1);\ninclude <nada.scad>\n"})
	out, code, err := s.run(t, "-o", "/out/x.stl", "001-m.scad")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	ds := render.ParseOutput(out, s.ws, "001-m.scad")
	if len(ds) != 1 || ds[0].Severity != render.SeverityWarning || ds[0].Line != 2 || ds[0].File != "" {
		t.Fatalf("diagnósticos: %+v (salida %q)", ds, out)
	}
}

func TestTextUsesEmbeddedFonts(t *testing.T) {
	s := newScene(t, map[string]string{"001-t.scad": "linear_extrude(2) text(\"ScadLab\", size = 10);\n"})
	out, code, err := s.run(t, "-o", "/out/x.stl", "--export-format", "binstl", "--backend=manifold", "001-t.scad")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	if strings.Contains(strings.ToLower(out), "fontconfig") {
		t.Fatalf("fontconfig se queja: %q", out)
	}
	info, err := mesh.InfoFromFile(s.outFile("x.stl"))
	if err != nil || info.Triangles < 200 || info.Size[0] < 30 {
		t.Fatalf("el texto no generó geometría: %+v err=%v", info, err)
	}
	// Negrita: otra fuente embebida, otra malla.
	s2 := newScene(t, map[string]string{"001-t.scad": "linear_extrude(2) text(\"ScadLab\", size = 10, font = \"Liberation Sans:style=Bold\");\n"})
	if out, code, err := s2.run(t, "-o", "/out/x.stl", "--export-format", "binstl", "001-t.scad"); err != nil || code != 0 {
		t.Fatalf("negrita: code=%d err=%v out=%q", code, err, out)
	}
	bold, err := mesh.InfoFromFile(s2.outFile("x.stl"))
	if err != nil || bold.VolumeMM3 <= info.VolumeMM3 {
		t.Fatalf("la negrita debería pesar más: %v vs %v (err %v)", bold.VolumeMM3, info.VolumeMM3, err)
	}
}

// Un modelo de dos colores sale como un 3MF con dos partes: OFF con color por
// cara → ConvertOFFTo3MF → threemf.Split.
func TestTwoColorsGiveTwoParts(t *testing.T) {
	src := "color(\"red\") cube(5);\ncolor(\"blue\") translate([10,0,0]) sphere(3, $fn = 16);\n"
	s := newScene(t, map[string]string{"001-c.scad": src})
	out, code, err := s.run(t, "-o", "/out/x.off", "--backend=manifold", "001-c.scad")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	dest := filepath.Join(s.out, "x.3mf")
	if err := scadwasm.ConvertOFFTo3MF(s.outFile("x.off"), dest, "001-c"); err != nil {
		t.Fatal(err)
	}
	m, err := threemf.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	parts := threemf.Split(m)
	if len(parts) != 2 {
		t.Fatalf("partes: %d, quería 2", len(parts))
	}
	if parts[0].Color != (threemf.RGB{255, 0, 0}) || parts[1].Color != (threemf.RGB{0, 0, 255}) {
		t.Fatalf("colores: %v %v", parts[0].Color, parts[1].Color)
	}
	if parts[0].Uncolored || parts[1].Uncolored {
		t.Fatal("ninguna parte es «sin color»")
	}
}

func TestUncoloredAndMixedModels(t *testing.T) {
	// Sin ningún color(): una sola parte «sin color».
	s := newScene(t, map[string]string{"001-u.scad": "cube(5);\n"})
	if out, code, err := s.run(t, "-o", "/out/x.off", "--backend=manifold", "001-u.scad"); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	dest := filepath.Join(s.out, "x.3mf")
	if err := scadwasm.ConvertOFFTo3MF(s.outFile("x.off"), dest, "u"); err != nil {
		t.Fatal(err)
	}
	m, err := threemf.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	parts := threemf.Split(m)
	if len(parts) != 1 || !parts[0].Uncolored || len(parts[0].Triangles) != 12 {
		t.Fatalf("partes: %+v", parts)
	}

	// Un color() y geometría sin color: la parte de color y la «Default».
	s2 := newScene(t, map[string]string{"001-m.scad": "cube(2);\ncolor(\"green\") translate([5,0,0]) cube(2);\n"})
	if out, code, err := s2.run(t, "-o", "/out/x.off", "--backend=manifold", "001-m.scad"); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	dest2 := filepath.Join(s2.out, "x.3mf")
	if err := scadwasm.ConvertOFFTo3MF(s2.outFile("x.off"), dest2, "m"); err != nil {
		t.Fatal(err)
	}
	m2, err := threemf.ReadFile(dest2)
	if err != nil {
		t.Fatal(err)
	}
	p2 := threemf.Split(m2)
	if len(p2) != 2 || p2[0].Uncolored || !p2[1].Uncolored {
		t.Fatalf("partes: %+v", p2)
	}
}

// heavyModel tarda minutos aun con el OpenSCAD nativo: un minkowski de una
// esfera hueca (decenas de miles de partes convexas).
const heavyModel = "minkowski() { difference() { sphere(10, $fn = 60); sphere(8, $fn = 60); } sphere(1, $fn = 40); }\n"

// Cancelar el contexto mata la ejecución aunque esté dentro de un cálculo.
func TestCancelStopsLongRun(t *testing.T) {
	s := newScene(t, map[string]string{"001-long.scad": heavyModel})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1500 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, _, err := shared.Run(ctx, s.spec("-o", "/out/x.stl", "001-long.scad"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, quería context.Canceled", err)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("tardó %v en cancelar", d)
	}
}

func TestAlreadyCancelledContext(t *testing.T) {
	s := newScene(t, map[string]string{"001-c.scad": "cube(1);\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := shared.Run(ctx, s.spec("-o", "/out/x.stl", "001-c.scad")); err == nil {
		t.Fatal("con el contexto cancelado tiene que fallar")
	}
}

func TestDeadlineExceeded(t *testing.T) {
	s := newScene(t, map[string]string{"001-long.scad": heavyModel})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, _, err := shared.Run(ctx, s.spec("-o", "/out/x.stl", "001-long.scad"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, quería DeadlineExceeded", err)
	}
}

// Un montaje de solo lectura no se puede escribir: EROFS, y el archivo no
// aparece en el disco.
func TestReadOnlyMount(t *testing.T) {
	s := newScene(t, map[string]string{"001-r.scad": "cube(1);\n"})
	out, _, err := s.run(t, "-o", "/work/salida.stl", "001-r.scad")
	if err != nil {
		t.Fatal(err)
	}
	// OpenSCAD avisa que no pudo abrir el archivo (y sale con 0, como el nativo).
	if !strings.Contains(out, "Can't open file") {
		t.Fatalf("escribir en un montaje de solo lectura tiene que fallar: %q", out)
	}
	if _, err := os.Stat(filepath.Join(s.ws, "salida.stl")); err == nil {
		t.Fatal("se escribió en un montaje de solo lectura")
	}
}

// Lo que está fuera de los montajes no existe, y un «..» no sale del montaje.
func TestNoEscapeFromMounts(t *testing.T) {
	// Un secreto junto al workspace (fuera de cualquier montaje).
	parent := t.TempDir()
	ws := filepath.Join(parent, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := "echo(\"LEAK-SECRET\");\n"
	if err := os.WriteFile(filepath.Join(parent, "secret.scad"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "include </work/../secret.scad>\ninclude <../secret.scad>\ninclude <" + filepath.ToSlash(filepath.Join(parent, "secret.scad")) + ">\ncube(1);\n"
	if err := os.WriteFile(filepath.Join(ws, "001-e.scad"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	spec := scadwasm.RunSpec{
		Args: []string{"-o", "/out/x.stl", "001-e.scad"},
		Cwd:  "/work",
		Mounts: []scadwasm.Mount{
			{Host: ws, Virtual: "/work", ReadOnly: true},
			{Host: out, Virtual: "/out"},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	got, _, err := shared.Run(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "LEAK-SECRET") {
		t.Fatalf("el motor leyó un archivo fuera de los montajes: %q", got)
	}
	if n := strings.Count(got, "Can't find include file"); n != 3 {
		t.Fatalf("esperaba 3 includes no encontrados, hay %d: %q", n, got)
	}

	// Escribir con «..» tampoco escapa: /out/../fuera.stl es /fuera.stl, que no
	// existe en el árbol virtual.
	spec.Args = []string{"-o", "/out/../fuera.stl", "001-e.scad"}
	if got, _, err := shared.Run(ctx, spec); err != nil || !strings.Contains(got, "Can't open file") {
		t.Fatalf("escribir fuera del montaje tiene que fallar: err=%v out=%q", err, got)
	}
	for _, p := range []string{filepath.Join(filepath.Dir(out), "fuera.stl"), filepath.Join(out, "fuera.stl"), "/fuera.stl"} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("apareció %s", p)
		}
	}
}

func TestInvalidMounts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for name, spec := range map[string]scadwasm.RunSpec{
		"directorio inexistente": {Args: []string{"--version"}, Mounts: []scadwasm.Mount{{Host: filepath.Join(t.TempDir(), "no"), Virtual: "/work"}}},
		"virtual relativa":       {Args: []string{"--version"}, Mounts: []scadwasm.Mount{{Host: t.TempDir(), Virtual: "work"}}},
		"virtual raíz":           {Args: []string{"--version"}, Mounts: []scadwasm.Mount{{Host: t.TempDir(), Virtual: "/"}}},
		"cwd inexistente":        {Args: []string{"--version"}, Cwd: "/nada"},
	} {
		if _, _, err := shared.Run(ctx, spec); err == nil {
			t.Errorf("%s: debería fallar", name)
		}
	}
}

// Varias instancias a la vez, cada una con su memoria: el resultado es el mismo
// y el detector de carreras no ve nada.
func TestParallelRuns(t *testing.T) {
	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	tris := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := newScene(t, map[string]string{"001-p.scad": "difference(){ sphere(10, $fn = 32); cube(12, center = true); }\n"})
			out, code, err := s.run(t, "-o", "/out/x.stl", "--export-format", "binstl", "--backend=manifold", "001-p.scad")
			if err != nil || code != 0 {
				errs[i] = errors.Join(err, errors.New(out))
				return
			}
			info, err := mesh.InfoFromFile(s.outFile("x.stl"))
			errs[i], tris[i] = err, info.Triangles
		}()
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("ejecución %d: %v", i, errs[i])
		}
		if tris[i] != tris[0] || tris[i] == 0 {
			t.Fatalf("resultados distintos: %v", tris)
		}
	}
}

// El chequeo de sintaxis del linter: -o x.ast sobre un archivo del workspace.
func TestASTExport(t *testing.T) {
	s := newScene(t, map[string]string{"001-a.scad": "cube(1);\n"})
	if out, code, err := s.run(t, "-o", "/out/x.ast", "001-a.scad"); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, out)
	}
	if _, err := os.Stat(s.outFile("x.ast")); err != nil {
		t.Fatal(err)
	}
}

func TestRewritePaths(t *testing.T) {
	mounts := []scadwasm.Mount{
		{Host: "/home/u/ws", Virtual: "/work"},
		{Host: "/opt/lib one", Virtual: "/lib/0"},
		{Host: "/opt/lib ten", Virtual: "/lib/10"},
	}
	for in, want := range map[string]string{
		"WARNING: x in file /work/001-a.scad, line 3":   "WARNING: x in file /home/u/ws/001-a.scad, line 3",
		"ERROR: y in file \"/work/lib/a.scad\", line 9": "ERROR: y in file \"/home/u/ws/lib/a.scad\", line 9",
		"Can't open library '/lib/10/BOSL2/std.scad'.":  "Can't open library '/opt/lib ten/BOSL2/std.scad'.",
		"Can't open library '/lib/0/x.scad'.":           "Can't open library '/opt/lib one/x.scad'.",
		"ya existe /workshop/otro y /work.":             "ya existe /workshop/otro y /home/u/ws.",
		"sin ruta virtual: /etc/passwd":                 "sin ruta virtual: /etc/passwd",
		"texto/work/pegado":                             "texto/work/pegado",
		"/work/a.scad:3":                                "/home/u/ws/a.scad:3",
	} {
		if got := scadwasm.RewritePaths(in, mounts); got != want {
			t.Errorf("\nentrada: %q\n  salida: %q\n  quería: %q", in, got, want)
		}
	}
}
