package threemf

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"scadlab/backend/threemf/bbscheck"
)

// Estas pruebas releen el 3MF con las reglas del lector de Bambu Studio (paquete
// bbscheck, un port de bbs_3mf.cpp 02.08.02.61). No abren Bambu Studio (regla 9).

func writeSplit(t *testing.T) (string, []Part) {
	t.Helper()
	r := openscadLike(t)
	m, err := Read(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	parts := Split(m)
	dest := filepath.Join(t.TempDir(), "out.3mf")
	if err := WriteFile(dest, parts, WriteOptions{Title: "maceta", Place: true, BedCenter: [2]float64{90, 90}}); err != nil {
		t.Fatal(err)
	}
	return dest, parts
}

func rgbaOf(c RGB) [4]uint8 { return [4]uint8{c[0], c[1], c[2], 255} }

// Bambu Studio tiene que ver cada parte con su color (sale del <m:colorgroup>) y su
// extrusor (sale de model_settings.config), y abrir el cuadro de «colores del 3MF» que
// empareja cada color con un filamento y asigna el extrusor de cada parte.
func TestBambuReaderGetsPartsColorsAndExtruders(t *testing.T) {
	dest, parts := writeSplit(t)
	res, err := bbscheck.Import(dest)
	if err != nil {
		t.Fatal(err)
	}
	// Archivo «de otro fabricante»: ni «BambuStudio-» (se descartaría el color por
	// triángulo y se exigiría un project_settings.config completo) ni «PrusaSlicer».
	if res.IsBBL || res.FromPrusa || res.HasProjectConfig {
		t.Fatalf("application=%q bbl=%v prusa=%v projectConfig=%v", res.Application, res.IsBBL, res.FromPrusa, res.HasProjectConfig)
	}
	if len(res.ColorGroups) != len(parts) {
		t.Fatalf("grupos de color: %v", res.ColorGroups)
	}
	if len(res.Objects) != 1 || len(res.Objects[0].Volumes) != len(parts) {
		t.Fatalf("objetos: %+v", res.Objects)
	}
	obj := res.Objects[0]
	if obj.Name != "maceta" {
		t.Errorf("nombre del objeto %q", obj.Name)
	}
	for i, v := range obj.Volumes {
		if v.Extruder != i+1 || v.Name != parts[i].Name {
			t.Errorf("parte %d: extrusor %d nombre %q (esperaba %d, %q)", i, v.Extruder, v.Name, i+1, parts[i].Name)
		}
		if v.Triangles != len(parts[i].Triangles) || len(v.TriangleColors) != v.Triangles {
			t.Errorf("parte %d: %d triángulos, %d con color", i, v.Triangles, len(v.TriangleColors))
		}
	}

	d := res.ColorDialog()
	if !d.Shown {
		t.Fatal("Bambu Studio no abriría el cuadro de colores del 3MF: sin <m:colorgroup> los colores no llegan")
	}
	if !d.AssignsVolumeExtruders {
		t.Error("apply_volume_filament_ids no asignaría el extrusor por parte")
	}
	if len(d.Colors) != len(parts) {
		t.Errorf("colores a emparejar: %v, partes: %d", d.Colors, len(parts))
	}
	for i, row := range d.VolumeColors[0] {
		if row == nil || *row != rgbaOf(parts[i].Color) {
			t.Errorf("parte %d: color %v, esperaba %v", i, row, rgbaOf(parts[i].Color))
		}
	}

	// Sin el cuadro (cancelado, o una versión que no lo tiene) el extrusor 2 solo
	// sobrevive si el proyecto ya tiene al menos 2 filamentos: con 1, vuelve al 1.
	// (GLCanvas3D::reload_scene → ModelVolume::update_extruder_count.)
	if got := res.ExtruderAfterLoad(1); got[0][len(parts)-1] != 1 {
		t.Errorf("con 1 filamento todo debería caer al 1: %v", got)
	}
	got := res.ExtruderAfterLoad(len(parts))
	for i, e := range got[0] {
		if e != i+1 {
			t.Errorf("con %d filamentos la parte %d debería quedar en el extrusor %d, quedó %d", len(parts), i, i+1, e)
		}
	}
}

// El 3MF anterior llevaba el color en <basematerials>, que Bambu Studio no lee (solo
// «m:colorgroup»): no había grupos de color, el cuadro nunca se abría y las partes se
// quedaban con el extrusor del archivo, que se pierde si el proyecto tiene un solo
// filamento. Este es el caso que vio el usuario.
func TestBambuReaderIgnoresBaseMaterials(t *testing.T) {
	dest, parts := writeSplit(t)

	// Reconstruye el modelo en el formato viejo a partir del actual.
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		body := string(b)
		if f.Name == "3D/3dmodel.model" {
			var bases strings.Builder
			bases.WriteString(`<basematerials id="1">`)
			for _, p := range parts {
				bases.WriteString(`<base name="` + p.Name + `" displaycolor="` + p.Color.Hex() + `FF"/>`)
			}
			bases.WriteString(`</basematerials>`)
			body = regexp.MustCompile(`(?m)^\s*<m:colorgroup.*\n`).ReplaceAllString(body, "")
			body = strings.Replace(body, "<resources>\n", "<resources>\n"+bases.String()+"\n", 1)
			body = regexp.MustCompile(`pid="\d+" pindex="0"`).ReplaceAllString(body, `pid="1" pindex="0"`)
		}
		w, _ := zw.Create(f.Name)
		io.WriteString(w, body)
	}
	zr.Close()
	zw.Close()
	legacy := filepath.Join(t.TempDir(), "legacy.3mf")
	if err := os.WriteFile(legacy, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := bbscheck.Import(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ColorGroups) != 0 || res.ColorDialog().Shown {
		t.Fatalf("con <basematerials> Bambu Studio no ve colores: grupos=%v dialog=%+v", res.ColorGroups, res.ColorDialog())
	}
}

// Un 3MF con el pid que apunta a un grupo inexistente, o un color mal escrito, no abre
// el cuadro: lo que escribimos tiene que ser un color que Bambu pueda interpretar.
func TestBambuReaderRejectsBadColors(t *testing.T) {
	for name, color := range map[string]string{"sin_hex": "rojo", "corto": "#FFF", "no_hex": "#GGGGGGFF"} {
		t.Run(name, func(t *testing.T) {
			dest, _ := writeSplit(t)
			mutate(t, dest, "3D/3dmodel.model", func(s string) string {
				return regexp.MustCompile(`color="#[0-9A-F]{8}"`).ReplaceAllString(s, `color="`+color+`"`)
			})
			res, err := bbscheck.Import(dest)
			if err != nil {
				t.Fatal(err)
			}
			if res.ColorDialog().Shown {
				t.Errorf("color %q: el cuadro no debería abrirse", color)
			}
		})
	}
}

// Una sola parte (un modelo de un color): el objeto no tiene varias partes, así que
// Bambu no asigna extrusor por parte; el color se empareja igual con un filamento.
func TestBambuReaderSinglePart(t *testing.T) {
	r := openscadLike(t)
	m, _ := Read(r, r.Size())
	parts := Split(m)[:1]
	dest := filepath.Join(t.TempDir(), "uno.3mf")
	if err := WriteFile(dest, parts, WriteOptions{Title: "uno"}); err != nil {
		t.Fatal(err)
	}
	res, err := bbscheck.Import(dest)
	if err != nil {
		t.Fatal(err)
	}
	d := res.ColorDialog()
	if !d.Shown || len(d.Colors) != 1 || d.AssignsVolumeExtruders {
		t.Errorf("una parte: %+v", d)
	}
	if got := res.ExtruderAfterLoad(1); got[0][0] != 1 {
		t.Errorf("extrusor %v", got)
	}
}

// El XML tiene que declarar el prefijo m: que Bambu busca literal.
func TestWriteDeclaresMaterialsNamespace(t *testing.T) {
	dest, parts := writeSplit(t)
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	model := readEntry(t, &zr.Reader, "3D/3dmodel.model")
	if !strings.Contains(model, `xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`) {
		t.Error("falta xmlns:m de la extensión Materials")
	}
	if n := strings.Count(model, "<m:colorgroup "); n != len(parts) {
		t.Errorf("%d colorgroups para %d partes", n, len(parts))
	}
	if strings.Contains(model, "basematerials") {
		t.Error("ya no se escribe <basematerials>")
	}
	if !strings.Contains(model, `<metadata name="Application">ScadLab</metadata>`) {
		t.Error("Application tiene que seguir siendo ScadLab")
	}
	for _, name := range []string{"Metadata/project_settings.config", "Metadata/Slic3r_PE_model.config"} {
		for _, f := range zr.File {
			if f.Name == name {
				t.Errorf("no debe llevar %s", name)
			}
		}
	}
}

// mutate reescribe una entrada del zip.
func mutate(t *testing.T, path, entry string, fn func(string) string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	found := false
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		body := string(b)
		if f.Name == entry {
			found = true
			body = fn(body)
		}
		w, _ := zw.Create(f.Name)
		io.WriteString(w, body)
	}
	zr.Close()
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("falta %s", entry)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
