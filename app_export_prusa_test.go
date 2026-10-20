package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/exporter"
)

// ExportCheckpoint acepta "3mf-prusa" (S14): escribe el 3MF de PrusaSlicer, con el
// nombre sugerido distinto del 3MF normal y la extensión .3mf.
func TestExportCheckpointPrusa3MF(t *testing.T) {
	a, id, name := exportRig(t)
	out := t.TempDir()
	var gotName string
	res, err := a.exportCheckpoint(id, name, "3mf-prusa", func(_, suggested, format string) (string, error) {
		gotName = suggested
		if format != "3mf-prusa" {
			t.Errorf("el diálogo recibió el formato %q", format)
		}
		return filepath.Join(out, "salida-prusa"), nil // sin extensión: se completa
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "mi-placa-001-dos-prusa.3mf" {
		t.Errorf("nombre sugerido: %q", gotName)
	}
	if res.Format != exporter.Format3MFPrusa || res.Path != filepath.Join(out, "salida-prusa.3mf") || len(res.Parts) != 2 {
		t.Errorf("resultado: %+v", res)
	}
	zr, err := zip.OpenReader(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		names[f.Name] = string(b)
	}
	if !strings.Contains(names["Metadata/Slic3r_PE_model.config"], `key="extruder" value="2"`) {
		t.Errorf("falta el volumen del segundo extrusor:\n%s", names["Metadata/Slic3r_PE_model.config"])
	}
	if _, ok := names["Metadata/model_settings.config"]; ok {
		t.Error("el 3MF de PrusaSlicer no lleva model_settings.config")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Error(err)
	}
}
