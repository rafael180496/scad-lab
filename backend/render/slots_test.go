package render

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Con varios proyectos abiertos cada uno puede pedir un render a la vez: el
// motor deja correr como mucho maxConcurrentRenders OpenSCAD y el resto espera
// su turno (sin cancelarse ni fallar).
func TestConcurrentRendersAreCapped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("el openscad falso es un script sh")
	}
	dir := t.TempDir()
	running := filepath.Join(dir, "running")
	peaks := filepath.Join(dir, "peaks")
	if err := os.Mkdir(running, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "openscad")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "OpenSCAD version 2025.03.15" >&2; exit 0; fi
out=""
while [ $# -gt 0 ]; do
  case "$1" in -o) out="$2"; shift 2 ;; *) shift ;; esac
done
touch '` + running + `'/$$
ls '` + running + `' | wc -l >> '` + peaks + `'
sleep 0.6
rm -f '` + running + `'/$$
head -c 84 /dev/zero > "$out"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	const n = 4
	var wg sync.WaitGroup
	results := make([]Result, n)
	for i := range n {
		ws := filepath.Join(dir, "ws"+strconv.Itoa(i))
		os.MkdirAll(ws, 0o755)
		// Contenido distinto por proyecto: si no, la caché los resolvería sin correr nada.
		os.WriteFile(filepath.Join(ws, "001-a.scad"), []byte("cube("+strconv.Itoa(i+1)+");\n"), 0o644)
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := eng.Render(context.Background(), Request{WorkspaceKey: "ws" + strconv.Itoa(i), WorkspaceDir: ws, FileName: "001-a.scad"},
				Options{OpenSCADPath: bin, Backend: "cgal", TimeoutSeconds: 30})
			if err != nil {
				t.Errorf("render %d: %v", i, err)
			}
			results[i] = r
		}()
	}
	wg.Wait()

	for i, r := range results {
		if r.Status != StatusOK {
			t.Errorf("render %d: %s %+v", i, r.Status, r.Diagnostics)
		}
	}
	raw, err := os.ReadFile(peaks)
	if err != nil {
		t.Fatal(err)
	}
	maxSeen := 0
	for _, l := range strings.Fields(string(raw)) {
		if v, _ := strconv.Atoi(l); v > maxSeen {
			maxSeen = v
		}
	}
	if maxSeen > maxConcurrentRenders || maxSeen == 0 {
		t.Errorf("hubo %d OpenSCAD a la vez; el tope es %d", maxSeen, maxConcurrentRenders)
	}
}
