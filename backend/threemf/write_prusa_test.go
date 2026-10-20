package threemf

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// prusaParts son las partes del 3MF de OpenSCAD simulado (rojo, verde y la
// geometría sin color), con sus extrusores 1..3.
func prusaParts(t *testing.T) []Part {
	t.Helper()
	r := openscadLike(t)
	m, err := Read(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	parts := Split(m)
	if len(parts) != 3 {
		t.Fatalf("esperaba 3 partes, hay %d", len(parts))
	}
	return parts
}

// meshVolume es el volumen de una malla (suma de tetraedros con signo contra el
// origen, en valor absoluto). Los cubos de las pruebas son cerrados.
func meshVolume(verts [][3]float64, tris [][3]int32) float64 {
	var v float64
	for _, t := range tris {
		a, b, c := verts[t[0]], verts[t[1]], verts[t[2]]
		v += a[0]*(b[1]*c[2]-b[2]*c[1]) - a[1]*(b[0]*c[2]-b[2]*c[0]) + a[2]*(b[0]*c[1]-b[1]*c[0])
	}
	return math.Abs(v / 6)
}

type prusaConfig struct {
	Objects []struct {
		ID        string `xml:"id,attr"`
		Instances string `xml:"instances_count,attr"`
		Meta      []struct {
			Type  string `xml:"type,attr"`
			Key   string `xml:"key,attr"`
			Value string `xml:"value,attr"`
		} `xml:"metadata"`
		Volumes []struct {
			First int `xml:"firstid,attr"`
			Last  int `xml:"lastid,attr"`
			Meta  []struct {
				Type  string `xml:"type,attr"`
				Key   string `xml:"key,attr"`
				Value string `xml:"value,attr"`
			} `xml:"metadata"`
		} `xml:"volume"`
	} `xml:"object"`
}

func (c prusaConfig) volumeMeta(i int, key string) string {
	for _, m := range c.Objects[0].Volumes[i].Meta {
		if m.Key == key {
			return m.Value
		}
	}
	return ""
}

// El 3MF de PrusaSlicer es un paquete válido con UN objeto (una sola malla, sin
// componentes) y un config cuyos volúmenes son rangos contiguos de triángulos
// que cubren la malla entera, uno por color, con su extrusor 1-based.
func TestWritePrusaStructure(t *testing.T) {
	parts := prusaParts(t)
	dest := filepath.Join(t.TempDir(), "prusa.3mf")
	if err := WritePrusaFile(dest, parts, WriteOptions{Title: "cubos <&>", Place: true, BedCenter: [2]float64{125, 105}}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	names := map[string]bool{}
	for i, f := range zr.File {
		names[f.Name] = true
		if i == 0 && f.Name != "[Content_Types].xml" {
			t.Errorf("la primera entrada es %s", f.Name)
		}
		// XML bien formado en todo el paquete.
		dec := xml.NewDecoder(strings.NewReader(readEntry(t, &zr.Reader, f.Name)))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Errorf("%s no es XML válido: %v", f.Name, err)
				break
			}
		}
	}
	for _, need := range []string{"[Content_Types].xml", "_rels/.rels", "3D/3dmodel.model", "Metadata/Slic3r_PE_model.config"} {
		if !names[need] {
			t.Errorf("falta %s", need)
		}
	}
	if names["Metadata/model_settings.config"] {
		t.Error("el 3MF de PrusaSlicer no lleva model_settings.config (es el de Bambu/Orca)")
	}

	var model struct {
		Unit      string `xml:"unit,attr"`
		Resources struct {
			Objects []struct {
				ID       string     `xml:"id,attr"`
				Vertices []struct{} `xml:"mesh>vertices>vertex"`
				Tris     []struct {
					V1 int `xml:"v1,attr"`
					V2 int `xml:"v2,attr"`
					V3 int `xml:"v3,attr"`
				} `xml:"mesh>triangles>triangle"`
				Components []struct{} `xml:"components>component"`
			} `xml:"object"`
		} `xml:"resources"`
		Build struct {
			Items []struct {
				ObjectID  string `xml:"objectid,attr"`
				Transform string `xml:"transform,attr"`
			} `xml:"item"`
		} `xml:"build"`
	}
	raw := readEntry(t, &zr.Reader, "3D/3dmodel.model")
	if err := xml.Unmarshal([]byte(raw), &model); err != nil {
		t.Fatal(err)
	}
	if model.Unit != "millimeter" {
		t.Errorf("unit = %q", model.Unit)
	}
	if strings.Contains(raw, "<component") || strings.Contains(raw, "<basematerials") || strings.Contains(raw, "Version3mf") {
		t.Errorf("sin componentes, materiales ni versión de PrusaSlicer:\n%s", raw[:min(len(raw), 600)])
	}
	if len(model.Resources.Objects) != 1 || len(model.Resources.Objects[0].Components) != 0 {
		t.Fatalf("tiene que haber UN objeto con malla: %+v", model.Resources.Objects)
	}
	obj := model.Resources.Objects[0]
	if len(model.Build.Items) != 1 || model.Build.Items[0].ObjectID != obj.ID {
		t.Fatalf("el <item> tiene que apuntar al único objeto: %+v", model.Build.Items)
	}
	// Cubos de 10 mm: 0..70 en X, 0..10 en Y/Z → centrado en (125, 105).
	if want := `transform="1 0 0 0 1 0 0 0 1 90 100 0"`; !strings.Contains(raw, want) {
		t.Errorf("transform del item: falta %s", want)
	}

	wantVerts, wantTris := 0, 0
	for _, p := range parts {
		wantVerts += len(p.Vertices)
		wantTris += len(p.Triangles)
	}
	if len(obj.Vertices) != wantVerts || len(obj.Tris) != wantTris {
		t.Fatalf("malla concatenada: %d vértices / %d triángulos, esperaba %d / %d", len(obj.Vertices), len(obj.Tris), wantVerts, wantTris)
	}
	for i, tr := range obj.Tris {
		for _, v := range []int{tr.V1, tr.V2, tr.V3} {
			if v < 0 || v >= len(obj.Vertices) {
				t.Fatalf("triángulo %d: índice %d fuera de rango", i, v)
			}
		}
	}

	var cfg prusaConfig
	if err := xml.Unmarshal([]byte(readEntry(t, &zr.Reader, "Metadata/Slic3r_PE_model.config")), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Objects) != 1 || cfg.Objects[0].ID != obj.ID || cfg.Objects[0].Instances != "1" {
		t.Fatalf("el config tiene que describir el mismo objeto: %+v", cfg.Objects)
	}
	vols := cfg.Objects[0].Volumes
	if len(vols) != len(parts) {
		t.Fatalf("%d volúmenes para %d partes", len(vols), len(parts))
	}
	next, vertBase := 0, 0
	for i, v := range vols {
		if v.First != next {
			t.Errorf("volumen %d: firstid %d, tiene que continuar en %d (rangos contiguos, sin huecos)", i, v.First, next)
		}
		if v.Last < v.First {
			t.Errorf("volumen %d: lastid %d < firstid %d", i, v.Last, v.First)
		}
		if n := v.Last - v.First + 1; n != len(parts[i].Triangles) {
			t.Errorf("volumen %d: %d triángulos, la parte tiene %d", i, n, len(parts[i].Triangles))
		}
		next = v.Last + 1
		if got := cfg.volumeMeta(i, "extruder"); got != strconv.Itoa(parts[i].Extruder) {
			t.Errorf("volumen %d: extrusor %q, esperaba %d (1-based)", i, got, parts[i].Extruder)
		}
		if got := cfg.volumeMeta(i, "name"); got != parts[i].Name {
			t.Errorf("volumen %d: nombre %q, esperaba %q", i, got, parts[i].Name)
		}
		if cfg.volumeMeta(i, "volume_type") != "ModelPart" {
			t.Errorf("volumen %d: volume_type %q", i, cfg.volumeMeta(i, "volume_type"))
		}
		// Los triángulos de este rango solo usan los vértices de su parte.
		for ti := v.First; ti <= v.Last; ti++ {
			tr := obj.Tris[ti]
			for _, vi := range []int{tr.V1, tr.V2, tr.V3} {
				if vi < vertBase || vi >= vertBase+len(parts[i].Vertices) {
					t.Fatalf("volumen %d: el triángulo %d usa el vértice %d, fuera de los de su parte [%d,%d)", i, ti, vi, vertBase, vertBase+len(parts[i].Vertices))
				}
			}
		}
		vertBase += len(parts[i].Vertices)
	}
	if next != wantTris {
		t.Errorf("los volúmenes cubren %d triángulos de %d", next, wantTris)
	}
}

// Releer el 3MF de PrusaSlicer da la misma geometría: el volumen total es la suma
// de las partes (cubos de 10 mm).
func TestWritePrusaKeepsTheGeometry(t *testing.T) {
	parts := prusaParts(t)
	dest := filepath.Join(t.TempDir(), "prusa.3mf")
	if err := WritePrusaFile(dest, parts, WriteOptions{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	back, err := ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	tris := make([][3]int32, len(back.Triangles))
	for i, tr := range back.Triangles {
		tris[i] = tr.V
	}
	var want float64
	for _, p := range parts {
		want += meshVolume(p.Vertices, p.Triangles)
	}
	if got := meshVolume(back.Vertices, tris); math.Abs(got-want) > 1e-6 || math.Abs(got-4000) > 1e-6 {
		t.Errorf("volumen releído %.6f, de las partes %.6f (cuatro cubos = 4000)", got, want)
	}
}

func TestWritePrusaRejectsNoMeshAndIsAtomic(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.3mf")
	if err := WritePrusaFile(dest, nil, WriteOptions{}); err == nil {
		t.Fatal("sin partes tiene que fallar")
	}
	if err := WritePrusaFile(dest, []Part{{Name: "vacía", Extruder: 1}}, WriteOptions{}); err == nil {
		t.Fatal("una parte sin triángulos no es una malla")
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "*")); len(entries) != 0 {
		t.Errorf("quedó algo tras un error: %v", entries)
	}
}

// Una parte vacía entre dos buenas no deja un rango firstid > lastid en el config.
func TestWritePrusaSkipsEmptyParts(t *testing.T) {
	parts := prusaParts(t)
	withEmpty := []Part{parts[0], {Name: "vacía", Extruder: 9}, parts[1]}
	dest := filepath.Join(t.TempDir(), "p.3mf")
	if err := WritePrusaFile(dest, withEmpty, WriteOptions{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.OpenReader(dest)
	defer zr.Close()
	var cfg prusaConfig
	if err := xml.Unmarshal([]byte(readEntry(t, &zr.Reader, "Metadata/Slic3r_PE_model.config")), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := len(cfg.Objects[0].Volumes); got != 2 {
		t.Fatalf("%d volúmenes, esperaba 2", got)
	}
	for _, v := range cfg.Objects[0].Volumes {
		if v.Last < v.First {
			t.Errorf("rango invertido: %+v", v)
		}
	}
}

// El 3MF normal lleva extruder_nr (0-based) por parte para Cura, antes del <mesh>,
// sin romper el resto: el paquete sigue pasando la misma comprobación y los
// extrusores de model_settings.config (1-based) no cambian. Sin la opción no hay
// ningún metadatagroup.
func TestWriteCuraExtruderNr(t *testing.T) {
	parts := prusaParts(t)
	dir := t.TempDir()
	withCura, without := filepath.Join(dir, "cura.3mf"), filepath.Join(dir, "plain.3mf")
	if err := WriteFile(withCura, parts, WriteOptions{Title: "x", CuraExtruders: true}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(without, parts, WriteOptions{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	checkPackage(t, withCura, len(parts))
	checkPackage(t, without, len(parts))

	zr, _ := zip.OpenReader(withCura)
	defer zr.Close()
	model := readEntry(t, &zr.Reader, "3D/3dmodel.model")
	for i := range parts {
		want := `<metadatagroup><metadata name="extruder_nr">` + strconv.Itoa(i) + `</metadata></metadatagroup>`
		if strings.Count(model, want) != 1 {
			t.Errorf("parte %d: falta %s", i, want)
		}
	}
	// El grupo va antes del <mesh> de su objeto (orden del esquema 3MF).
	for _, obj := range strings.Split(model, "<object ")[1:] {
		if i := strings.Index(obj, "<metadatagroup>"); i >= 0 {
			if j := strings.Index(obj, "<mesh>"); j < 0 || i > j {
				t.Errorf("metadatagroup después de <mesh>:\n%s", obj[:min(len(obj), 300)])
			}
		}
	}
	// Orca, Bambu y Creality leen esto, no el grupo de Cura: no cambió.
	cfg := readEntry(t, &zr.Reader, "Metadata/model_settings.config")
	for i := range parts {
		if !strings.Contains(cfg, `<metadata key="extruder" value="`+strconv.Itoa(i+1)+`"/>`) {
			t.Errorf("model_settings sin el extrusor %d", i+1)
		}
	}
	// Sin la opción, el modelo no menciona a Cura.
	zp, _ := zip.OpenReader(without)
	defer zp.Close()
	if strings.Contains(readEntry(t, &zp.Reader, "3D/3dmodel.model"), "metadatagroup") {
		t.Error("sin CuraExtruders no debe haber metadatagroup")
	}
	// Releer el 3MF con el grupo da las mismas partes.
	back, err := ReadFile(withCura)
	if err != nil {
		t.Fatal(err)
	}
	if again := Split(back); len(again) != len(parts) {
		t.Errorf("partes al releer: %d, esperaba %d", len(again), len(parts))
	}
}
