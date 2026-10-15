package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"scadlab/backend/i18n"
	"scadlab/backend/store"
)

// Bindings del visor 3D (S2). La malla en sí no pasa por acá: viaja por HTTP
// (ver claude/06-go-react-contract.md).

// SaveSwatches guarda las 6 muestras de color del visor (#rrggbb). Es un
// binding aparte de SaveSettings para que cambiar un color desde el visor no
// reescriba la configuración entera con lo que el visor tenga en memoria.
func (a *App) SaveSwatches(colors []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !store.ValidSwatches(colors) {
		return i18n.New(i18n.Msg{ES: "las muestras tienen que ser 6 colores #rrggbb", EN: "swatches must be 6 #rrggbb colors"})
	}
	norm := make([]string, len(colors))
	for i, c := range colors {
		norm[i] = strings.ToLower(c)
	}
	return a.store.SetSetting("swatches", norm)
}

// SaveScreenshot pide dónde guardar la captura del visor con el diálogo
// nativo y escribe el PNG. Devuelve la ruta, o "" si el usuario cancela.
func (a *App) SaveScreenshot(dataURL, suggestedName string) (string, error) {
	data, err := decodePNGDataURL(dataURL)
	if err != nil {
		return "", err
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           i18n.T(i18n.Msg{ES: "Guardar captura", EN: "Save screenshot"}),
		DefaultFilename: screenshotName(suggestedName),
		Filters:         []wruntime.FileFilter{{DisplayName: "PNG (*.png)", Pattern: "*.png"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	// En Windows el diálogo no siempre agrega la extensión del filtro.
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		path += ".png"
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", i18n.Errorf(i18n.Msg{ES: "no se pudo guardar la captura: %v", EN: "could not save the screenshot: %v"}, err)
	}
	return path, nil
}

const pngDataPrefix = "data:image/png;base64,"

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// decodePNGDataURL acepta solo un data URL PNG en base64 (lo que devuelve
// canvas.toDataURL) y comprueba la firma del archivo: el binding escribe en
// disco lo que recibe, así que no acepta otra cosa.
func decodePNGDataURL(s string) ([]byte, error) {
	if !strings.HasPrefix(s, pngDataPrefix) {
		return nil, i18n.New(i18n.Msg{ES: "la captura no es un PNG", EN: "the screenshot is not a PNG"})
	}
	data, err := base64.StdEncoding.DecodeString(s[len(pngDataPrefix):])
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "captura corrupta: %v", EN: "corrupt screenshot: %v"}, err)
	}
	if !bytes.HasPrefix(data, pngMagic) {
		return nil, i18n.New(i18n.Msg{ES: "la captura no es un PNG", EN: "the screenshot is not a PNG"})
	}
	return data, nil
}

// screenshotName limpia el nombre sugerido: sin carpetas ni caracteres que
// Windows no admite, y con extensión .png.
func screenshotName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" || name == "." || name == ".." {
		name = "scadlab"
	}
	return name + ".png"
}
