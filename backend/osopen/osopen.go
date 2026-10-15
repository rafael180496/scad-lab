// Package osopen abre una ruta con la aplicación por defecto del sistema
// (Finder en macOS, Explorador en Windows).
package osopen

import (
	"os/exec"
	"runtime"
)

// Open abre path. No espera a que la aplicación termine.
func Open(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// explorer.exe devuelve código 1 aunque abra bien: no se mira el error
		// de Wait, solo el de Start.
		cmd = exec.Command("explorer.exe", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
