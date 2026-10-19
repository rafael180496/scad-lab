// Package osopen abre una ruta con la aplicación por defecto del sistema
// (Finder en macOS, Explorador en Windows).
package osopen

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"scadlab/backend/i18n"
)

// openTimeout es lo que se espera a que `open` (macOS) devuelva: entrega la
// ruta al sistema y termina, en milisegundos.
const openTimeout = 8 * time.Second

// Open abre path. Si no existe, o si el sistema no puede abrirla, devuelve un
// error que dice por qué: antes se lanzaba el proceso y nadie miraba cómo
// terminaba, así que `open` fallando sobre una carpeta inexistente no se
// notaba en la interfaz.
//
// No espera a que la aplicación termine (el Finder queda abierto): en macOS
// espera solo a que `open` devuelva, que es cuando informa su error.
func Open(path string) error {
	if _, err := os.Stat(path); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no existe la ruta %s", EN: "the path %s does not exist"}, path)
	}
	switch runtime.GOOS {
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "open", path).CombinedOutput()
		if err != nil {
			return failed(path, err, string(out))
		}
		return nil
	case "windows":
		// explorer.exe devuelve código 1 aunque abra bien: no se mira el error
		// de Wait, solo el de Start.
		cmd := exec.Command("explorer.exe", path)
		if err := cmd.Start(); err != nil {
			return failed(path, err, "")
		}
		go func() { _ = cmd.Wait() }()
		return nil
	default:
		cmd := exec.Command("xdg-open", path)
		if err := cmd.Start(); err != nil {
			return failed(path, err, "")
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
}

func failed(path string, err error, out string) error {
	detail := strings.TrimSpace(out)
	if detail == "" {
		detail = err.Error()
	}
	return i18n.Errorf(i18n.Msg{ES: "no se pudo abrir %s: %s", EN: "could not open %s: %s"}, path, detail)
}
