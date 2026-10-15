package scad

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scadlab/backend/mesh"
)

func templateFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "assets", "skills", "openscad-templates", "templates", "*.scad"))
	if err != nil || len(files) != 10 {
		t.Fatalf("plantillas: %d, %v", len(files), err)
	}
	return files
}

// Las 10 plantillas reales: formatear es idempotente y no cambia los tokens.
func TestFormatTemplatesIdempotent(t *testing.T) {
	for _, f := range templateFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		once, err := Format(string(raw), FormatOptions{IndentSize: 4})
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		twice, _ := Format(once, FormatOptions{IndentSize: 4})
		if twice != once {
			t.Errorf("%s: no es idempotente", filepath.Base(f))
		}
		assertSameTokens(t, string(raw), once)
	}
}

// Formatear no puede cambiar la geometría: cada plantilla se renderiza con
// OpenSCAD real antes y después de formatear y se comparan volumen y
// triángulos de la malla.
func TestFormatTemplatesSameGeometry(t *testing.T) {
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	dir := t.TempDir()
	for _, f := range templateFiles(t) {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			formatted, err := Format(string(raw), FormatOptions{IndentSize: 2})
			if err != nil {
				t.Fatal(err)
			}
			base := strings.TrimSuffix(filepath.Base(f), ".scad")
			orig := renderVolume(t, bin, dir, base+"-orig", string(raw))
			got := renderVolume(t, bin, dir, base+"-fmt", formatted)
			t.Logf("%s: volumen original %.3f mm³ (%d triángulos), formateada %.3f mm³ (%d triángulos)",
				base, orig.VolumeMM3, orig.Triangles, got.VolumeMM3, got.Triangles)
			if orig.Triangles != got.Triangles || math.Abs(orig.VolumeMM3-got.VolumeMM3) > 1e-6*math.Max(1, orig.VolumeMM3) {
				t.Errorf("la geometría cambió: %+v vs %+v", orig, got)
			}
		})
	}
}

func renderVolume(t *testing.T, bin, dir, name, src string) mesh.Info {
	t.Helper()
	scadPath := filepath.Join(dir, name+".scad")
	stl := filepath.Join(dir, name+".stl")
	if err := os.WriteFile(scadPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-o", stl, "--export-format", "binstl", "--backend=manifold", scadPath).CombinedOutput()
	if err != nil && strings.Contains(string(out), "backend") { // OpenSCAD sin Manifold (2021.01)
		out, err = exec.CommandContext(ctx, bin, "-o", stl, scadPath).CombinedOutput()
	}
	if err != nil {
		t.Fatalf("openscad %s: %v\n%s", name, err, out)
	}
	info, err := mesh.InfoFromFile(stl)
	if err != nil {
		t.Fatal(err)
	}
	if info.Triangles == 0 {
		t.Fatalf("%s: malla vacía", name)
	}
	return info
}

// El editor lintea y formatea código a medio escribir: cada plantilla cortada
// en cientos de puntos no puede hacer entrar en pánico a Lint ni a Format, y
// el formateo sigue siendo idempotente y sin cambiar tokens.
func TestTruncatedSourcesAreSafe(t *testing.T) {
	for _, f := range templateFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for cut := 0; cut < len(src); cut += 13 {
			part := strings.ToValidUTF8(src[:cut], "")
			_ = Lint(context.Background(), part, LintOptions{FileName: "001-x.scad"})
			once, err := Format(part, FormatOptions{IndentSize: 4})
			if err != nil {
				continue // string o comentario cortado a la mitad
			}
			if twice, _ := Format(once, FormatOptions{IndentSize: 4}); twice != once {
				t.Fatalf("%s cortada en %d: no es idempotente\n%s\n---\n%s", filepath.Base(f), cut, once, twice)
			}
			assertSameTokens(t, part, once)
		}
	}
}
