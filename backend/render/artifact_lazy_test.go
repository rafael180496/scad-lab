package render

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"scadlab/backend/threemf"
)

// openscad falso para RenderArtifact: anota los argumentos, escribe un archivo no vacío en la
// ruta de -o y, con reject, rechaza --enable como una versión sin esa opción.
func newArtifactFake(t *testing.T, reject bool) (bin, runs string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("el openscad falso es un script sh")
	}
	dir := t.TempDir()
	bin, runs = filepath.Join(dir, "openscad"), filepath.Join(dir, "runs")
	rej := ":"
	if reject {
		rej = `echo "unknown option '--enable'" >&2; exit 1`
	}
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "OpenSCAD version 2026.10.11" >&2; exit 0; fi
echo "$*" >> '` + runs + `'
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -O) shift 2 ;;
    --enable=*) ` + rej + `; shift ;;
    *) shift ;;
  esac
done
echo x > "$out"
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, runs
}

func artifactRuns(t *testing.T, runs string) []string {
	t.Helper()
	b, err := os.ReadFile(runs)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// LazyUnion agrega --enable=lazy-union solo al 3MF que lo pide, y su resultado vive en otra
// clave de la caché: el 3MF fundido de siempre no se confunde con el de lazy-union.
func TestArtifactLazyUnionFlagAndCacheKey(t *testing.T) {
	bin, runs := newArtifactFake(t, false)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "001-a.scad"), []byte("cube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := newTestEngine(t)
	req := Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-a.scad"}
	dir := t.TempDir()
	ctx := context.Background()
	opts := Options{OpenSCADPath: bin, Backend: "manifold", TimeoutSeconds: 30}
	lazy := opts
	lazy.LazyUnion = true

	plain, err := eng.RenderArtifact(ctx, req, opts, ArtifactExt3MF, dir)
	if err != nil || plain.Status != StatusOK {
		t.Fatalf("3MF normal: %+v err=%v", plain, err)
	}
	l, err := eng.RenderArtifact(ctx, req, lazy, ArtifactExt3MF, dir)
	if err != nil || l.Status != StatusOK || l.CacheHit {
		t.Fatalf("3MF lazy-union: %+v err=%v (no puede salir de la caché del normal)", l, err)
	}
	if plain.MeshPath == l.MeshPath {
		t.Error("el 3MF normal y el de lazy-union comparten archivo de caché")
	}
	got := artifactRuns(t, runs)
	if len(got) != 2 || strings.Contains(got[0], "lazy-union") || !strings.Contains(got[1], "--enable=lazy-union") {
		t.Errorf("argumentos: %q", got)
	}
	// Las dos salen de la caché la segunda vez.
	for _, o := range []Options{opts, lazy} {
		again, _ := eng.RenderArtifact(ctx, req, o, ArtifactExt3MF, dir)
		if !again.CacheHit {
			t.Errorf("segunda llamada (LazyUnion=%v) sin caché: %+v", o.LazyUnion, again)
		}
	}
	if len(artifactRuns(t, runs)) != 2 {
		t.Error("las cachés no evitaron renderizar otra vez")
	}
}

// Una versión de OpenSCAD que no conoce --enable lo rechaza: se reintenta sin él y se recuerda.
func TestArtifactLazyUnionRejectedFallsBack(t *testing.T) {
	bin, runs := newArtifactFake(t, true)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "001-a.scad"), []byte("cube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := newTestEngine(t)
	req := Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-a.scad"}
	opts := Options{OpenSCADPath: bin, Backend: "manifold", TimeoutSeconds: 30, LazyUnion: true}
	r, err := eng.RenderArtifact(context.Background(), req, opts, ArtifactExt3MF, t.TempDir())
	if err != nil || r.Status != StatusOK {
		t.Fatalf("3MF: %+v err=%v", r, err)
	}
	got := artifactRuns(t, runs)
	if len(got) != 2 || !strings.Contains(got[0], "--enable=lazy-union") || strings.Contains(got[1], "--enable") {
		t.Errorf("argumentos: %q (primero con --enable, luego sin)", got)
	}
}

// El motor integrado recibe el mismo flag: sin él, el OFF sale con los colores fundidos.
func TestWASMArtifactLazyUnionKeepsTouchingColorsApart(t *testing.T) {
	ws := writeScad(t, map[string]string{
		"001-touch.scad": "color(\"red\") cube(10);\ncolor(\"blue\") translate([0, 0, 10]) cube(10);\n",
	})
	eng := newTestEngine(t)
	req := Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-touch.scad"}
	dir := filepath.Join(t.TempDir(), "exports")
	count := func(lazy bool) (open int, parts int) {
		o := wasmOpts()
		o.LazyUnion = lazy
		r, err := eng.RenderArtifact(context.Background(), req, o, ArtifactExt3MF, dir)
		if err != nil || r.Status != StatusOK {
			t.Fatalf("3MF (lazy=%v): %+v err=%v", lazy, r, err)
		}
		return openEdges(t, r.MeshPath)
	}
	if open, parts := count(false); parts != 2 || open == 0 {
		t.Errorf("sin lazy-union: partes %d, aristas abiertas %d; los cubos se tocan y tendrían que salir abiertos", parts, open)
	}
	if open, parts := count(true); parts != 2 || open != 0 {
		t.Errorf("con lazy-union: partes %d, aristas abiertas %d; tendrían que salir cerrados", parts, open)
	}
}

// openEdges lee un 3MF, lo reparte por color y cuenta los bordes abiertos y las partes.
func openEdges(t *testing.T, path string) (open, parts int) {
	t.Helper()
	m, err := threemf.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	split := threemf.Split(m)
	return threemf.OpenEdgeCount(split), len(split)
}
