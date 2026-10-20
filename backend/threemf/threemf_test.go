package threemf

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"scadlab/backend/mesh"
)

// cubeXML devuelve los <vertices> y <triangles> de un cubo de lado 10 en
// (x, 0, 0). pids[i] es el atributo de color del triángulo i ("" = sin color).
func cubeXML(x float64, pids [12]string) string {
	v := [][3]float64{{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0}, {0, 0, 10}, {10, 0, 10}, {10, 10, 10}, {0, 10, 10}}
	tri := [][3]int{
		{0, 2, 1}, {0, 3, 2}, // abajo
		{4, 5, 6}, {4, 6, 7}, // arriba
		{0, 1, 5}, {0, 5, 4}, // frente
		{1, 2, 6}, {1, 6, 5}, // derecha
		{2, 3, 7}, {2, 7, 6}, // fondo
		{3, 0, 4}, {3, 4, 7}, // izquierda
	}
	var b strings.Builder
	b.WriteString("<mesh><vertices>")
	for _, p := range v {
		fmt.Fprintf(&b, `<vertex x="%g" y="%g" z="%g"/>`, p[0]+x, p[1], p[2])
	}
	b.WriteString("</vertices><triangles>")
	for i, t := range tri {
		fmt.Fprintf(&b, `<triangle v1="%d" v2="%d" v3="%d" %s/>`, t[0], t[1], t[2], pids[i])
	}
	b.WriteString("</triangles></mesh>")
	return b.String()
}

func zipOf(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

const coreNS = `xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"`

// Una malla en el formato de OpenSCAD: un objeto, <basematerials> en otro
// orden que el de declaración, color por triángulo y un color repetido con
// otro alfa.
func openscadLike(t *testing.T) *bytes.Reader {
	var red, green, redAlpha [12]string
	for i := range red {
		red[i] = `pid="1" p1="3"`
		green[i] = `pid="1" p1="1"`
		redAlpha[i] = `pid="1" p1="2"`
	}
	var none [12]string // sin color: toma el del objeto (Default)
	model := `<?xml version="1.0"?><model unit="millimeter" ` + coreNS + `>
<metadata name="Title">cubos</metadata>
<resources>
 <basematerials id="1">
  <base name="Default" displaycolor="#F9D72CFF"/>
  <base name="Color 2" displaycolor="#00FF00FF"/>
  <base name="Color 3" displaycolor="#FF00007F"/>
  <base name="Color 1" displaycolor="#FF0000FF"/>
 </basematerials>
 <object id="2" type="model" pid="1" pindex="0">` +
		// Los cuatro cubos van en un solo mesh: se concatenan vértices a mano.
		combine(cubeXML(0, red), cubeXML(20, green), cubeXML(40, redAlpha), cubeXML(60, none)) + `</object>
</resources>
<build><item objectid="2"/></build></model>`
	return zipOf(t, map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"_rels/.rels":         `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Target="/3D/3dmodel.model" Id="r0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/></Relationships>`,
		"3D/3dmodel.model":    model,
	})
}

// combine une varios <mesh> en uno solo, reindexando los triángulos.
func combine(meshes ...string) string {
	var verts, tris strings.Builder
	base := 0
	for _, m := range meshes {
		vs := between(m, "<vertices>", "</vertices>")
		n := strings.Count(vs, "<vertex ")
		verts.WriteString(vs)
		for _, tri := range strings.SplitAfter(between(m, "<triangles>", "</triangles>"), "/>") {
			if tri == "" {
				continue
			}
			tris.WriteString(shift(tri, base))
		}
		base += n
	}
	return "<mesh><vertices>" + verts.String() + "</vertices><triangles>" + tris.String() + "</triangles></mesh>"
}

func between(s, a, b string) string {
	i := strings.Index(s, a) + len(a)
	return s[i:strings.Index(s, b)]
}

func shift(tri string, by int) string {
	for _, k := range []string{"v1", "v2", "v3"} {
		i := strings.Index(tri, k+`="`) + 4
		j := i + strings.Index(tri[i:], `"`)
		n, _ := strconv.Atoi(tri[i:j])
		tri = tri[:i] + strconv.Itoa(n+by) + tri[j:]
	}
	return tri
}

func TestSplitGroupsByColorInDeclarationOrder(t *testing.T) {
	r := openscadLike(t)
	m, err := Read(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Triangles) != 48 || m.Title != "cubos" {
		t.Fatalf("triángulos=%d título=%q", len(m.Triangles), m.Title)
	}
	parts := Split(m)
	// <basematerials> los lista Default, 2, 3, 1; el orden de salida es el de
	// «Color N»: rojo (1 y 3, mismo RGB con otro alfa), verde (2), y al final lo
	// que no tiene color.
	type want struct {
		color string
		tris  int
		unc   bool
	}
	wants := []want{{"#FF0000", 24, false}, {"#00FF00", 12, false}, {"#F9D72C", 12, true}}
	if len(parts) != len(wants) {
		t.Fatalf("partes=%d: %+v", len(parts), parts)
	}
	for i, w := range wants {
		p := parts[i]
		if p.Color.Hex() != w.color || len(p.Triangles) != w.tris || p.Uncolored != w.unc || p.Extruder != i+1 {
			t.Errorf("parte %d = %s %d tri unc=%v ext=%d, quería %+v", i, p.Color.Hex(), len(p.Triangles), p.Uncolored, p.Extruder, w)
		}
	}
	if parts[0].Name != "Color 1 (#FF0000)" || parts[2].Name != "Default" {
		t.Errorf("nombres: %q %q", parts[0].Name, parts[2].Name)
	}
	// Vértices compactados: dos cubos disjuntos (rojo y rojo/alfa) = 16, no 32.
	if len(parts[0].Vertices) != 16 || len(parts[1].Vertices) != 8 {
		t.Errorf("vértices por parte: %d %d", len(parts[0].Vertices), len(parts[1].Vertices))
	}
	for _, p := range parts {
		for _, tr := range p.Triangles {
			for _, v := range tr {
				if int(v) >= len(p.Vertices) {
					t.Fatalf("índice %d fuera de rango en %s", v, p.Name)
				}
			}
		}
	}
}

func TestSplitWithoutColorsIsOnePart(t *testing.T) {
	var none [12]string
	model := `<model ` + coreNS + ` unit="millimeter"><resources>
 <basematerials id="1"><base name="Default" displaycolor="#F9D72CFF"/></basematerials>
 <object id="2" pid="1" pindex="0">` + cubeXML(0, none) + `</object></resources><build><item objectid="2"/></build></model>`
	r := zipOf(t, map[string]string{"3D/3dmodel.model": model})
	m, err := Read(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	parts := Split(m)
	if len(parts) != 1 || !parts[0].Uncolored || parts[0].Extruder != 1 || len(parts[0].Triangles) != 12 {
		t.Fatalf("partes: %+v", parts)
	}
	// Sin <basematerials> ni nada: también una parte.
	model = `<model ` + coreNS + `><resources><object id="2">` + cubeXML(0, none) + `</object></resources><build><item objectid="2"/></build></model>`
	r = zipOf(t, map[string]string{"3D/3dmodel.model": model})
	if m, err = Read(r, r.Size()); err != nil {
		t.Fatal(err)
	}
	if parts = Split(m); len(parts) != 1 {
		t.Fatalf("partes: %+v", parts)
	}
}

// Componentes con transformación, también en otro archivo del paquete (como
// guarda Bambu Studio) y colorgroup del Materials Extension.
func TestReadComponentsTransformsAndColorGroup(t *testing.T) {
	var c0, c1 [12]string
	for i := range c0 {
		c0[i] = `pid="9" p1="0"`
		c1[i] = `pid="9" p1="1"`
	}
	sub := `<model ` + coreNS + ` xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"><resources>
 <m:colorgroup id="9"><m:color color="#112233"/><m:color color="#445566FF"/></m:colorgroup>
 <object id="1">` + cubeXML(0, c0) + `</object><object id="2">` + cubeXML(0, c1) + `</object></resources></model>`
	root := `<model ` + coreNS + `><resources>
 <object id="5"><components>
  <component objectid="1" p:path="/3D/Objects/o.model" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"/>
  <component objectid="2" p:path="/3D/Objects/o.model" xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06" transform="1 0 0 0 1 0 0 0 1 100 0 0"/>
 </components></object></resources>
 <build><item objectid="5" transform="1 0 0 0 1 0 0 0 1 0 0 5"/></build></model>`
	r := zipOf(t, map[string]string{"3D/3dmodel.model": root, "3D/Objects/o.model": sub})
	m, err := Read(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	parts := Split(m)
	if len(parts) != 2 {
		t.Fatalf("partes: %+v", parts)
	}
	byColor := map[string]Part{}
	for _, p := range parts {
		byColor[p.Color.Hex()] = p
	}
	a, b := byColor["#112233"], byColor["#445566"]
	if len(a.Triangles) != 12 || len(b.Triangles) != 12 {
		t.Fatalf("a=%d b=%d", len(a.Triangles), len(b.Triangles))
	}
	// El item sube todo 5 en Z; el segundo componente además se corre 100 en X.
	if a.Vertices[0] != [3]float64{0, 0, 5} {
		t.Errorf("a[0] = %v", a.Vertices[0])
	}
	if b.Vertices[0] != [3]float64{100, 0, 5} {
		t.Errorf("b[0] = %v", b.Vertices[0])
	}
}

func TestReadRejectsGarbage(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte("no soy un zip")), 13); err == nil {
		t.Error("un archivo cualquiera no puede leerse como 3MF")
	}
	r := zipOf(t, map[string]string{"3D/3dmodel.model": `<model ` + coreNS + `><resources/><build/></model>`})
	if _, err := Read(r, r.Size()); err == nil {
		t.Error("un 3MF sin mallas tiene que dar error")
	}
	r = zipOf(t, map[string]string{"otra.cosa": "x"})
	if _, err := Read(r, r.Size()); err == nil {
		t.Error("un zip sin modelo tiene que dar error")
	}
}

func TestNearColors(t *testing.T) {
	parts := []Part{
		{Color: RGB{255, 0, 0}}, {Color: RGB{254, 1, 0}}, {Color: RGB{0, 0, 255}},
		{Color: RGB{250, 214, 44}, Uncolored: true},
	}
	got := NearColors(parts, 9)
	if len(got) != 1 || got[0][0] != (RGB{255, 0, 0}) || got[0][1] != (RGB{254, 1, 0}) {
		t.Errorf("casi iguales: %v", got)
	}
}

// El 3MF que se escribe es un paquete válido: tipos de contenido, relación
// raíz, XML bien formado, referencias que existen y extrusores 1..N.
func TestWriteProducesValidPackage(t *testing.T) {
	r := openscadLike(t)
	m, err := Read(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	parts := Split(m)
	dest := filepath.Join(t.TempDir(), "out.3mf")
	if err := WriteFile(dest, parts, WriteOptions{Title: "cubos <&> \"x\"", Place: true, BedCenter: [2]float64{90, 90}}); err != nil {
		t.Fatal(err)
	}
	checkPackage(t, dest, len(parts))

	// Ida y vuelta: leer lo escrito da las mismas partes.
	back, err := ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	again := Split(back)
	if len(again) != len(parts) {
		t.Fatalf("partes tras releer: %d, antes %d", len(again), len(parts))
	}
	for i := range parts {
		if again[i].Color != parts[i].Color || len(again[i].Triangles) != len(parts[i].Triangles) {
			t.Errorf("parte %d cambió al releer: %+v vs %+v", i, again[i].Color, parts[i].Color)
		}
	}
	// El <item> centra la pieza en la cama y la apoya en Z=0: el cajón de los
	// cubos es X 0..70, Y 0..10, Z 0..10 → traslación (55, 85, 0).
	zr, _ := zip.OpenReader(dest)
	defer zr.Close()
	model := readEntry(t, &zr.Reader, "3D/3dmodel.model")
	if !strings.Contains(model, `transform="1 0 0 0 1 0 0 0 1 55 85 0"`) {
		t.Errorf("transform del item incorrecto:\n%s", model[strings.Index(model, "<build>"):])
	}
}

func readEntry(t *testing.T, zr *zip.Reader, name string) string {
	t.Helper()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			return string(b)
		}
	}
	t.Fatalf("falta %s en el paquete", name)
	return ""
}

// CheckPackage valida un 3MF escrito por Write contra la estructura 3MF core.
func checkPackage(t *testing.T, path string, nParts int) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for i, f := range zr.File {
		if strings.HasPrefix(f.Name, "/") {
			t.Errorf("entrada con barra inicial: %s", f.Name)
		}
		names[f.Name] = true
		if i == 0 && f.Name != "[Content_Types].xml" {
			t.Errorf("la primera entrada es %s, tiene que ser [Content_Types].xml", f.Name)
		}
	}
	for _, need := range []string{"[Content_Types].xml", "_rels/.rels", "3D/3dmodel.model", "Metadata/model_settings.config"} {
		if !names[need] {
			t.Errorf("falta %s", need)
		}
	}
	// Todo es XML bien formado.
	for name := range names {
		dec := xml.NewDecoder(strings.NewReader(readEntry(t, &zr.Reader, name)))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Errorf("%s no es XML válido: %v", name, err)
				break
			}
		}
	}
	ct := readEntry(t, &zr.Reader, "[Content_Types].xml")
	for _, want := range []string{
		`Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"`,
		`Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"`,
	} {
		if !strings.Contains(ct, want) {
			t.Errorf("[Content_Types].xml sin %s", want)
		}
	}
	rels := readEntry(t, &zr.Reader, "_rels/.rels")
	if !strings.Contains(rels, `Target="/3D/3dmodel.model"`) || !strings.Contains(rels, "schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel") {
		t.Errorf("_rels/.rels no apunta al modelo 3D:\n%s", rels)
	}

	var model struct {
		Unit      string `xml:"unit,attr"`
		Resources struct {
			Groups []struct {
				ID     string `xml:"id,attr"`
				Colors []struct {
					Color string `xml:"color,attr"`
				} `xml:"color"`
			} `xml:"colorgroup"`
			Objects []struct {
				ID       string `xml:"id,attr"`
				PID      string `xml:"pid,attr"`
				PIndex   string `xml:"pindex,attr"`
				Vertices []struct {
					X string `xml:"x,attr"`
					Y string `xml:"y,attr"`
					Z string `xml:"z,attr"`
				} `xml:"mesh>vertices>vertex"`
				Tris []struct {
					V1 int `xml:"v1,attr"`
					V2 int `xml:"v2,attr"`
					V3 int `xml:"v3,attr"`
				} `xml:"mesh>triangles>triangle"`
				Components []struct {
					ObjectID string `xml:"objectid,attr"`
				} `xml:"components>component"`
			} `xml:"object"`
		} `xml:"resources"`
		Build struct {
			Items []struct {
				ObjectID  string `xml:"objectid,attr"`
				Transform string `xml:"transform,attr"`
			} `xml:"item"`
		} `xml:"build"`
	}
	if err := xml.Unmarshal([]byte(readEntry(t, &zr.Reader, "3D/3dmodel.model")), &model); err != nil {
		t.Fatal(err)
	}
	if model.Unit != "millimeter" {
		t.Errorf("unit = %q", model.Unit)
	}
	// Un <m:colorgroup> por parte, con un solo color (es lo que lee Bambu Studio).
	if len(model.Resources.Groups) != nParts {
		t.Fatalf("colorgroups: %+v", model.Resources.Groups)
	}
	groupByID := map[string]int{} // id del grupo → cantidad de colores
	ids := map[string]int{}       // id → índice de objeto
	for _, g := range model.Resources.Groups {
		if len(g.Colors) != 1 || !regexp.MustCompile(`^#[0-9A-F]{6}FF$`).MatchString(g.Colors[0].Color) {
			t.Errorf("grupo %s: colores %+v", g.ID, g.Colors)
		}
		groupByID[g.ID] = len(g.Colors)
		ids[g.ID] = -1
	}
	meshes, containers := 0, 0
	for i, o := range model.Resources.Objects {
		if _, dup := ids[o.ID]; dup {
			t.Errorf("id %s repetido: materiales y objetos comparten espacio de ids", o.ID)
		}
		ids[o.ID] = i
		if (len(o.Vertices) > 0) == (len(o.Components) > 0) {
			t.Errorf("objeto %s: tiene que ser malla o componentes, no ambos ni ninguno", o.ID)
		}
		if len(o.Vertices) > 0 {
			meshes++
			if n, ok := groupByID[o.PID]; !ok || n != 1 {
				t.Errorf("objeto %s: pid %q no es un colorgroup", o.ID, o.PID)
			}
			if o.PIndex != "0" {
				t.Errorf("objeto %s: pindex %q", o.ID, o.PIndex)
			}
			for _, tr := range o.Tris {
				for _, v := range []int{tr.V1, tr.V2, tr.V3} {
					if v < 0 || v >= len(o.Vertices) {
						t.Fatalf("objeto %s: índice %d fuera de rango", o.ID, v)
					}
				}
			}
		} else {
			containers++
			if len(o.Components) != nParts {
				t.Errorf("el contenedor tiene %d componentes, quería %d", len(o.Components), nParts)
			}
			for _, c := range o.Components {
				if idx, ok := ids[c.ObjectID]; !ok || idx < 0 {
					t.Errorf("componente a un objeto inexistente o declarado después: %s", c.ObjectID)
				}
			}
		}
	}
	if meshes != nParts || containers != 1 {
		t.Errorf("mallas=%d contenedores=%d", meshes, containers)
	}
	if len(model.Build.Items) != 1 {
		t.Fatalf("items: %+v", model.Build.Items)
	}
	if idx, ok := ids[model.Build.Items[0].ObjectID]; !ok || idx < 0 || len(model.Resources.Objects[idx].Components) == 0 {
		t.Errorf("el <item> tiene que apuntar al objeto contenedor")
	}

	var cfg struct {
		Objects []struct {
			ID    string `xml:"id,attr"`
			Parts []struct {
				ID      string `xml:"id,attr"`
				Subtype string `xml:"subtype,attr"`
				Meta    []struct {
					Key   string `xml:"key,attr"`
					Value string `xml:"value,attr"`
				} `xml:"metadata"`
			} `xml:"part"`
		} `xml:"object"`
	}
	if err := xml.Unmarshal([]byte(readEntry(t, &zr.Reader, "Metadata/model_settings.config")), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Objects) != 1 || len(cfg.Objects[0].Parts) != nParts {
		t.Fatalf("model_settings: %+v", cfg)
	}
	if cfg.Objects[0].ID != model.Build.Items[0].ObjectID {
		t.Errorf("model_settings apunta al objeto %s, el item es %s", cfg.Objects[0].ID, model.Build.Items[0].ObjectID)
	}
	for i, p := range cfg.Objects[0].Parts {
		ext := ""
		for _, m := range p.Meta {
			if m.Key == "extruder" {
				ext = m.Value
			}
		}
		if ext != strconv.Itoa(i+1) || p.Subtype != "normal_part" {
			t.Errorf("parte %d: extrusor %q subtype %q", i, ext, p.Subtype)
		}
		if idx, ok := ids[p.ID]; !ok || idx < 0 {
			t.Errorf("parte %s de model_settings no es un objeto del modelo", p.ID)
		}
	}
}

// El STL por color es binario válido y conserva la geometría.
func TestWriteSTLRoundTrip(t *testing.T) {
	r := openscadLike(t)
	m, _ := Read(r, r.Size())
	for _, p := range Split(m) {
		var buf bytes.Buffer
		if err := WriteSTL(&buf, p); err != nil {
			t.Fatal(err)
		}
		if buf.Len() != 84+50*len(p.Triangles) {
			t.Fatalf("tamaño %d para %d triángulos", buf.Len(), len(p.Triangles))
		}
		info, err := mesh.ParseSTL(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		// Un cubo de 10 mm = 1000 mm³; los cubos de una parte son disjuntos.
		cubes := float64(len(p.Triangles) / 12)
		if math.Abs(info.VolumeMM3-1000*cubes) > 1e-3 || info.Triangles != len(p.Triangles) {
			t.Errorf("%s: volumen %.3f triángulos %d", p.Name, info.VolumeMM3, info.Triangles)
		}
	}
}

func TestWriteFileIsAtomic(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.3mf")
	if err := os.WriteFile(dest, []byte("bueno"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sin partes falla y no toca el archivo existente ni deja temporales.
	if err := WriteFile(dest, nil, WriteOptions{}); err == nil {
		t.Fatal("sin partes tiene que fallar")
	}
	if b, _ := os.ReadFile(dest); string(b) != "bueno" {
		t.Errorf("el archivo existente cambió: %q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("quedaron temporales: %v", entries)
	}
}
