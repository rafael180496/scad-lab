package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"scadlab/backend/scadwasm"
)

// Este archivo es el lado del motor de render que habla con el OpenSCAD
// integrado (backend/scadwasm). El nativo corre con `exec`; el integrado, con
// montajes: el workspace (solo lectura) y una carpeta de salida propia.

// engineKind dice con qué motor se renderiza con estas opciones: el nativo si
// hay ruta, el integrado si no la hay y está disponible, y "" si ninguno.
func (o Options) engineKind() string {
	switch {
	case o.OpenSCADPath != "":
		return EngineNative
	case o.WASM != nil && o.WASM.Available():
		return EngineWASM
	}
	return ""
}

// wasmExitError es un OpenSCAD integrado que terminó con código ≠ 0: el
// equivalente de *exec.ExitError del nativo.
type wasmExitError struct{ code int }

func (e *wasmExitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// wasmBackend es el valor de --backend para el motor integrado.
func wasmBackend(manifold bool) string {
	if manifold {
		return "manifold"
	}
	return "cgal"
}

// maxLibDirs acota cuántas carpetas de bibliotecas del sistema se montan.
const maxLibDirs = 8

// WASMSpec arma la ejecución del motor integrado para un workspace: el
// workspace en /work (cwd, solo lectura), outDir —si no es ""— en /out, y las
// carpetas de bibliotecas de OpenSCAD del usuario en /lib/N, con
// OPENSCADPATH=/work:/work/lib:/lib/0:… (lo mismo que ve el OpenSCAD nativo:
// el workspace, su lib/ y las bibliotecas instaladas). args van con rutas
// virtuales.
func WASMSpec(workspaceDir, outDir string, args ...string) scadwasm.RunSpec {
	mounts := []scadwasm.Mount{{Host: workspaceDir, Virtual: "/work", ReadOnly: true}}
	if outDir != "" {
		mounts = append(mounts, scadwasm.Mount{Host: outDir, Virtual: "/out"})
	}
	path := []string{"/work", "/work/lib"}
	for i, d := range libraryDirs(workspaceDir) {
		virtual := "/lib/" + strconv.Itoa(i)
		mounts = append(mounts, scadwasm.Mount{Host: d, Virtual: virtual, ReadOnly: true})
		path = append(path, virtual)
	}
	return scadwasm.RunSpec{
		Args:   args,
		Cwd:    "/work",
		Mounts: mounts,
		Env:    map[string]string{"OPENSCADPATH": strings.Join(path, ":")},
	}
}

// libraryDirs son las carpetas de bibliotecas que el OpenSCAD nativo
// encontraría fuera del workspace: las de la variable OPENSCADPATH y la
// carpeta de bibliotecas del usuario. Solo las que existen.
func libraryDirs(workspaceDir string) []string {
	var cands []string
	for _, p := range filepath.SplitList(os.Getenv("OPENSCADPATH")) {
		if p != "" {
			cands = append(cands, p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		switch runtime.GOOS {
		case "darwin", "windows":
			cands = append(cands, filepath.Join(home, "Documents", "OpenSCAD", "libraries"))
		default:
			cands = append(cands, filepath.Join(home, ".local", "share", "OpenSCAD", "libraries"))
		}
	}
	ws := filepath.Clean(workspaceDir)
	var out []string
	seen := map[string]bool{}
	for _, c := range cands {
		c = filepath.Clean(c)
		if seen[c] || c == ws || len(out) >= maxLibDirs {
			continue
		}
		if st, err := os.Stat(c); err != nil || !st.IsDir() {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// runWASM ejecuta OpenSCAD integrado y deja el resultado en out. flags son las
// opciones de OpenSCAD (sin -o ni el archivo). Escribe en una carpeta temporal
// propia dentro de la caché (el motor solo ve esa carpeta, no la caché entera)
// y mueve el archivo al final, igual que hace el nativo con su temporal.
func (e *engine) runWASM(ctx context.Context, w WASMRunner, parent, dir, file, out string, flags []string) (string, error) {
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	outDir, err := os.MkdirTemp(parent, wasmTempPrefix)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(outDir)

	base := "render" + filepath.Ext(out)
	args := append([]string{"-o", "/out/" + base}, flags...)
	args = append(args, file)
	output, code, err := w.Run(ctx, WASMSpec(dir, outDir, args...))
	if err != nil {
		return output, err
	}
	if code != 0 {
		return output, &wasmExitError{code: code}
	}
	if err := os.Rename(filepath.Join(outDir, base), out); err != nil && !os.IsNotExist(err) {
		return output, err
	}
	return output, nil
}

// wasmTempPrefix marca las carpetas temporales de salida del motor integrado.
const wasmTempPrefix = "wasm-"
