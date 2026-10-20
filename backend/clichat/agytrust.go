package clichat

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Confianza de Antigravity en una carpeta. YA NO es un requisito de ScadLab: los
// permisos van en `permissions.allow` del settings.json GLOBAL y no dependen de la
// carpeta (la confianza era la condición para que Antigravity cargara el hooks.json
// de un proyecto, mecanismo que se retiró). Se conserva SOLO como pista informativa:
// cuando un turno igual termina con una herramienta bloqueada, el mensaje menciona
// que la carpeta tampoco figura en `trustedWorkspaces` por si es la causa. Nunca
// bloquea un turno ni escribe en el archivo (la confianza se concede en el diálogo
// interactivo de `agy`, «Yes, I trust this folder»).
//
// Lo que se midió con agy 1.4.x: la coincidencia es por carpeta exacta (el home en
// la lista no cubre sus subcarpetas).

type agySettings struct {
	TrustedWorkspaces []string `json:"trustedWorkspaces"`
	// WorkspaceTrust es la opción «Workspace Trust» de /config. No se pudo
	// probar qué hace al apagarse, así que solo se toma la lectura evidente:
	// `false` = no pide confianza.
	WorkspaceTrust *bool `json:"workspaceTrust"`
}

// AgyTrusted dice si Antigravity confía en workspaceDir según settingsPath. known es
// false cuando no se puede saber (no hay ruta, el archivo no se pudo leer o no es
// JSON, o Antigravity nunca corrió en este equipo): quien llama no debe afirmar nada.
func AgyTrusted(settingsPath, workspaceDir string) (trusted, known bool) {
	if settingsPath == "" || workspaceDir == "" {
		return false, false
	}
	raw, err := os.ReadFile(settingsPath)
	if errors.Is(err, fs.ErrNotExist) {
		// Sin archivo: solo es «sin confianza» si Antigravity ya corrió alguna vez
		// en este equipo (su carpeta de datos existe).
		if st, e := os.Stat(filepath.Dir(settingsPath)); e == nil && st.IsDir() {
			return false, true
		}
		return false, false
	}
	if err != nil {
		return false, false
	}
	var s agySettings
	if json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &s) != nil {
		return false, false
	}
	if s.WorkspaceTrust != nil && !*s.WorkspaceTrust {
		return true, true
	}
	want := normTrustPath(workspaceDir)
	for _, p := range s.TrustedWorkspaces {
		if normTrustPath(p) == want {
			return true, true
		}
	}
	return false, true
}

// normTrustPath deja una ruta comparable: limpia, con los enlaces simbólicos
// resueltos si la carpeta existe (macOS: /tmp ↔ /private/tmp) y sin distinguir
// mayúsculas donde el sistema de archivos no lo hace (Windows).
func normTrustPath(p string) string {
	p = filepath.Clean(filepath.FromSlash(strings.TrimSpace(p)))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
