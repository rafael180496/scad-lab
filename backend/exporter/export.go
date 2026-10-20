// Package export prepara y escribe lo que ScadLab entrega al slicer: el STL del
// checkpoint, un 3MF multicolor (una parte por color con su extrusor: para Bambu
// Studio, Orca, Creality Print, Anycubic Slicer Next…), el 3MF de PrusaSlicer
// (una sola malla con un volumen por color) o un STL por color. No sabe de Wails ni de diálogos: app_export.go
// pone el diálogo de guardar y este paquete hace el resto.
//
// Spec: .claude/specs/s9-exportacion.md. Notas: claude/11-exportacion.md.
package exporter

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/threemf"
)

// Formatos de exportación (contrato con el binding ExportCheckpoint).
const (
	FormatSTL         = "stl"
	Format3MF         = "3mf"
	FormatSTLPerColor = "stl-per-color"
	// Format3MFPrusa (S14) es el 3MF para PrusaSlicer: una sola malla con un
	// volumen por color en Metadata/Slic3r_PE_model.config. PrusaSlicer no lee el
	// model_settings.config del 3MF normal.
	Format3MFPrusa = "3mf-prusa"
	// FormatPNG no pasa por Prepare: es la imagen del visor (o el preview de las
	// herramientas MCP), que solo necesita nombre y extensión.
	FormatPNG = "png"
)

// nearColorDistance: dos colores a menos de esta distancia RGB (de 441 posibles)
// se avisan como «casi iguales». ~2 % del rango: no se distinguen a simple vista
// sobre una pieza impresa.
const nearColorDistance = 9

// PartInfo describe una parte de la salida, para la vista previa y el resultado.
type PartInfo struct {
	Name      string `json:"name"`
	Color     string `json:"color"` // #RRGGBB
	Triangles int    `json:"triangles"`
	Extruder  int    `json:"extruder"`  // extrusor 1..N (el filamento que el slicer le asigna)
	Uncolored bool   `json:"uncolored"` // geometría sin color(): lleva el amarillo por defecto de OpenSCAD
}

// Preview es lo que saldría de un checkpoint, sin escribir nada.
type Preview struct {
	Parts    []PartInfo `json:"parts"`
	Warnings []string   `json:"warnings"`
}

// Result es lo que devuelve una exportación. Path es "" si el usuario canceló.
type Result struct {
	Path     string     `json:"path"`
	Format   string     `json:"format"`
	Parts    []PartInfo `json:"parts"`
	Warnings []string   `json:"warnings"`
}

// Renderer es la parte del motor de render que usa la exportación (la cumple
// *render.Engine; la interfaz permite probar sin OpenSCAD). Es la misma que usa
// backend/parts para repartir el modelo por color.
type Renderer = parts.Renderer

// Job es lo que hace falta para preparar una exportación.
type Job struct {
	Engine Renderer
	Req    render.Request
	Opts   render.Options
	// CacheDir es la caché de los 3MF intermedios (<datos>/cache/exports).
	CacheDir string
	Printer  printers.Printer
}

// Prepared es una exportación lista para escribir.
type Prepared struct {
	Format   string
	Title    string
	meshPath string // STL de la caché (FormatSTL)
	parts    []threemf.Part
	printer  printers.Printer
	warnings []string
}

// Prepare renderiza lo que haga falta y analiza las partes. Para FormatSTL usa
// la malla del render (caché si ya se renderizó); para los otros dos pide a
// OpenSCAD un 3MF con color y lo reparte por color.
func Prepare(ctx context.Context, job Job, format string) (*Prepared, error) {
	title := strings.TrimSuffix(job.Req.FileName, filepath.Ext(job.Req.FileName))
	p := &Prepared{Format: format, Title: title, printer: job.Printer}
	switch format {
	case FormatSTL:
		res, err := job.Engine.Render(ctx, job.Req, job.Opts)
		if err != nil {
			return nil, err
		}
		if err := parts.RenderFailure(res); err != nil {
			return nil, err
		}
		p.meshPath = res.MeshPath
		return p, nil
	case Format3MF, FormatSTLPerColor, Format3MFPrusa:
		// El render 3MF y el reparto por color son los mismos que usa el visor
		// para las partes del modelo (backend/parts).
		split, _, err := parts.Load3MF(ctx, job.Engine, job.Req, job.Opts, job.CacheDir)
		if err != nil {
			return nil, err
		}
		p.parts = split
		p.warnings = warnings(p.parts, job.Printer)
		return p, nil
	}
	return nil, i18n.Errorf(i18n.Msg{ES: "formato de exportación desconocido: %q", EN: "unknown export format: %q"}, format)
}

// Infos son las partes para la interfaz. En FormatSTL no hay reparto: vacío.
func (p *Prepared) Infos() []PartInfo {
	out := make([]PartInfo, len(p.parts))
	for i, pt := range p.parts {
		out[i] = PartInfo{
			Name: pt.Name, Color: pt.Color.Hex(), Triangles: len(pt.Triangles),
			Extruder: pt.Extruder, Uncolored: pt.Uncolored,
		}
	}
	return out
}

// Warnings son los avisos (no errores) de la exportación.
func (p *Prepared) Warnings() []string { return append([]string{}, p.warnings...) }

// Preview devuelve partes y avisos.
func (p *Prepared) Preview() Preview { return Preview{Parts: p.Infos(), Warnings: p.Warnings()} }

// Write escribe la exportación en dest (sobrescribe, de forma atómica).
func (p *Prepared) Write(dest string) error {
	switch p.Format {
	case FormatSTL:
		return atomicWrite(dest, func(w io.Writer) error {
			f, err := os.Open(p.meshPath)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(w, f)
			return err
		})
	case Format3MF:
		// Sin `extruder_nr` para Cura (WriteOptions.CuraExtruders): el 3MF normal es
		// el que el usuario abre en Bambu Studio y ese bloque no se probó en ningún
		// slicer. Para Cura se recomienda el STL por color (ver claude/11).
		return threemf.WriteFile(dest, p.parts, p.writeOptions())
	case Format3MFPrusa:
		return threemf.WritePrusaFile(dest, p.parts, p.writeOptions())
	case FormatSTLPerColor:
		base := SafeName(strings.TrimSuffix(filepath.Base(dest), filepath.Ext(dest)))
		return atomicWrite(dest, func(w io.Writer) error { return writeSTLZip(w, base, p.parts) })
	}
	return nil
}

// writeOptions son las opciones comunes a los dos 3MF: el título y dónde poner la
// pieza en la cama de la impresora del workspace.
func (p *Prepared) writeOptions() threemf.WriteOptions {
	return threemf.WriteOptions{
		Title:     p.Title,
		Place:     p.printer.BuildVolume.X > 0 && p.printer.BuildVolume.Y > 0,
		BedCenter: [2]float64{p.printer.BuildVolume.X / 2, p.printer.BuildVolume.Y / 2},
	}
}

// writeSTLZip escribe un STL por parte: <base>-<color>.stl.
func writeSTLZip(w io.Writer, base string, parts []threemf.Part) error {
	zw := zip.NewWriter(w)
	for _, p := range parts {
		ew, err := zw.Create(StlEntryName(base, p))
		if err != nil {
			return err
		}
		if err := threemf.WriteSTL(ew, p); err != nil {
			return err
		}
	}
	return zw.Close()
}

// StlEntryName es el nombre de un STL por color: <base>-<rrggbb>.stl, o
// <base>-default.stl para lo que no tiene color().
func StlEntryName(base string, p threemf.Part) string {
	tag := strings.ToLower(strings.TrimPrefix(p.Color.Hex(), "#"))
	if p.Uncolored {
		tag = "default"
	}
	return base + "-" + tag + ".stl"
}

// warnings arma los avisos. Ninguno impide exportar: son cosas que el usuario
// querrá saber antes de cargar el archivo en el slicer.
func warnings(parts []threemf.Part, pr printers.Printer) []string {
	var out []string
	if pr.MulticolorSlots > 0 && len(parts) > pr.MulticolorSlots {
		out = append(out, i18n.T(i18n.Msg{
			ES: "El modelo usa %d colores y tu %s admite %d a la vez (%s). Unifica algunos colores o imprime por partes.",
			EN: "The model uses %d colors and your %s supports %d at a time (%s). Merge some colors or print in separate parts.",
		}, len(parts), pr.Name, pr.MulticolorSlots, pr.MulticolorSys))
	}
	colored, uncolored := 0, 0
	for _, p := range parts {
		if p.Uncolored {
			uncolored++
		} else {
			colored++
		}
	}
	if uncolored > 0 && colored > 0 {
		out = append(out, i18n.T(i18n.Msg{
			ES: "Hay geometría sin color(): saldrá como una parte más (\"Default\", amarillo) con su propio filamento. Envuélvela en color() para asignarle uno.",
			EN: "Some geometry has no color(): it will come out as one more part (\"Default\", yellow) using its own filament. Wrap it in color() to assign one.",
		}))
	}
	for _, pair := range threemf.NearColors(parts, nearColorDistance) {
		out = append(out, i18n.T(i18n.Msg{
			ES: "Los colores %s y %s son casi iguales y usarán dos filamentos distintos. Si querías uno solo, unifícalos en el .scad.",
			EN: "Colors %s and %s are almost identical and will use two different filaments. If you meant one, merge them in the .scad.",
		}, pair[0].Hex(), pair[1].Hex()))
	}
	return out
}

// atomicWrite escribe en un temporal junto a dest y lo renombra: una
// exportación cortada no deja un archivo roto con el nombre elegido, ni pisa
// uno bueno anterior.
func atomicWrite(dest string, fn func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".scadlab-*.tmp")
	if err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo escribir en %s: %w", EN: "could not write to %s: %w"}, filepath.Dir(dest), err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := fn(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, dest)
}

// SafeName deja un nombre usable como archivo en Windows y macOS: sin los
// caracteres que Windows prohíbe, sin espacios ni puntos al final y nunca vacío.
func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.TrimRight(s, " .")
	if s == "" {
		return "scadlab"
	}
	return s
}

// SuggestedName es el nombre que propone el diálogo: <workspace>-<NNN-slug>.<ext>.
// El 3MF de PrusaSlicer lleva «-prusa» antes de la extensión: es otro archivo con
// la misma extensión, y sin la marca una exportación pisaría a la otra en exports/.
func SuggestedName(workspace, fileName, format string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	name := SafeName(fmt.Sprintf("%s-%s", workspace, base))
	if format == Format3MFPrusa {
		name += "-prusa"
	}
	return name + Ext(format)
}

// IsFileFormat dice si format es uno de los formatos que Prepare sabe armar
// (STL, los dos 3MF y el STL por color). El PNG no: no pasa por Prepare.
func IsFileFormat(format string) bool {
	switch format {
	case FormatSTL, Format3MF, FormatSTLPerColor, Format3MFPrusa:
		return true
	}
	return false
}

// Ext es la extensión (con punto) del archivo que produce cada formato.
func Ext(format string) string {
	switch format {
	case Format3MF, Format3MFPrusa:
		return ".3mf"
	case FormatSTLPerColor:
		return ".zip"
	case FormatPNG:
		return ".png"
	}
	return ".stl"
}
