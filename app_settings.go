package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"scadlab/backend/appdata"
	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/store"
)

// SetLanguage cambia y persiste el idioma ("en" | "es").
func (a *App) SetLanguage(lang string) error {
	if err := a.ready(); err != nil {
		return err
	}
	i18n.SetLang(lang)
	return a.store.SetSetting("language", i18n.Lang())
}

// GetSettings lee la configuración.
func (a *App) GetSettings() (store.Settings, error) {
	if err := a.ready(); err != nil {
		return store.DefaultSettings(), err
	}
	return a.store.GetSettings()
}

// SaveSettings guarda la configuración completa.
func (a *App) SaveSettings(s store.Settings) error {
	if err := a.ready(); err != nil {
		return err
	}
	if s.InfillPercent < 0 || s.InfillPercent > 100 {
		return i18n.New(i18n.Msg{ES: "el relleno tiene que estar entre 0 y 100 %", EN: "infill must be between 0 and 100%"})
	}
	if err := validateRenderSettings(s); err != nil {
		return err
	}
	i18n.SetLang(s.Language)
	s.Language = i18n.Lang()
	return a.store.SaveSettings(s)
}

// EffectiveWorkspacesRoot es la carpeta de workspaces configurada o la default.
func (a *App) EffectiveWorkspacesRoot() string {
	if a.store != nil {
		if s, err := a.store.GetSettings(); err == nil && s.WorkspacesRoot != "" {
			return s.WorkspacesRoot
		}
	}
	return appdata.DefaultWorkspacesRoot()
}

// ChooseDirectory abre el diálogo nativo de carpetas. "" si se cancela.
//
// start puede no existir todavía (la carpeta de workspaces se crea recién al
// crear el primero): el diálogo de Wails falla con «default directory … does
// not exist» en vez de abrirse, así que se le pasa la carpeta existente más
// cercana.
func (a *App) ChooseDirectory(title, start string) (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, DefaultDirectory: existingDir(start)})
}

// existingDir devuelve p si es una carpeta existente, o su ancestro existente
// más cercano. "" si p está vacío o no es absoluto (el diálogo abre donde
// quiera el sistema).
func existingDir(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	for d := filepath.Clean(p); ; d = filepath.Dir(d) {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
		if parent := filepath.Dir(d); parent == d {
			return ""
		}
	}
}

// ChooseFile abre el diálogo nativo para elegir un ejecutable (p. ej. OpenSCAD).
func (a *App) ChooseFile(title string) (string, error) {
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: title})
}

// DetectTools busca OpenSCAD y cada CLI agéntico registrado.
func (a *App) DetectTools() ([]detect.Tool, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s, _ := a.store.GetSettings()
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	out := []detect.Tool{detect.OpenSCAD(ctx, s.OpenSCADPath)}
	provs, err := a.store.ListProviders()
	if err != nil {
		return out, err
	}
	// Los --version corren en paralelo: un CLI de node tarda ~1 s en arrancar
	// y en serie la pantalla de Configuración quedaría esperando la suma.
	results := make([]detect.Tool, len(provs))
	done := make(chan struct{}, len(provs))
	for i, p := range provs {
		go func() {
			defer func() { done <- struct{}{} }()
			if p.Kind == "cli" {
				results[i] = detect.CLI(ctx, p.ID, p.Command)
			}
		}()
	}
	for range provs {
		<-done
	}
	for i, p := range provs {
		if p.Kind == "cli" {
			out = append(out, results[i])
		}
	}
	return out, nil
}

// ListPrinters devuelve el catálogo de impresoras.
func (a *App) ListPrinters() ([]printers.Printer, error) { return printers.All() }

// ListMaterials devuelve los filamentos configurados.
func (a *App) ListMaterials() ([]store.Material, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListMaterials()
}

// SaveMaterial crea o actualiza un filamento.
func (a *App) SaveMaterial(m store.Material) error {
	if err := a.ready(); err != nil {
		return err
	}
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Name) == "" || m.DensityGCm3 <= 0 || m.CostPerKg < 0 {
		return i18n.New(i18n.Msg{ES: "material inválido: id, nombre, densidad > 0 y costo ≥ 0", EN: "invalid material: id, name, density > 0 and cost ≥ 0"})
	}
	return a.store.SaveMaterial(m)
}

// ListProviders devuelve los CLIs agénticos y Ollama.
func (a *App) ListProviders() ([]store.AIProvider, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListProviders()
}

// SaveProvider guarda comando, config y habilitado de un proveedor.
func (a *App) SaveProvider(p store.AIProvider) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.UpdateProvider(p)
}
