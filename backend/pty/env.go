package pty

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Environ arma el entorno de una terminal a partir de base (normalmente
// os.Environ()): agrega al PATH los directorios extra, fija TERM y COLORTERM
// y, fuera de Windows, un LANG UTF-8 si el proceso no tiene ninguno.
//
// Una app lanzada desde el Finder o el menú Inicio no hereda el PATH de la
// shell: sin los directorios extra, un CLI de node no encuentra a `node` y uno
// instalado en ~/.local/bin no encuentra a sus propias herramientas. Van
// DESPUÉS del PATH existente —el mismo orden que detect.Find— para no tapar
// con una versión vieja la que el sistema ya resuelve.
func Environ(base, extraPathDirs []string) []string {
	pathKey := "PATH"
	pathVal := ""
	hasLang := false
	out := make([]string, 0, len(base)+4)
	for _, kv := range base {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		switch {
		case strings.EqualFold(k, "PATH"):
			// Windows lo llama «Path»: se respeta el nombre que tenga.
			pathKey, pathVal = k, v
			continue
		case k == "TERM" || k == "COLORTERM":
			continue
		case k == "LANG" || k == "LC_ALL":
			hasLang = hasLang || v != ""
		}
		out = append(out, kv)
	}
	out = append(out, pathKey+"="+joinPath(pathVal, extraPathDirs), "TERM=xterm-256color", "COLORTERM=truecolor")
	if runtime.GOOS != "windows" && !hasLang {
		out = append(out, "LANG=en_US.UTF-8")
	}
	return out
}

// joinPath agrega a current los directorios que existan y no estén ya.
func joinPath(current string, extra []string) string {
	sep := string(os.PathListSeparator)
	seen := map[string]bool{}
	parts := []string{}
	for _, d := range filepath.SplitList(current) {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		parts = append(parts, d)
	}
	for _, d := range extra {
		if d == "" || seen[d] {
			continue
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			continue
		}
		seen[d] = true
		parts = append(parts, d)
	}
	return strings.Join(parts, sep)
}
