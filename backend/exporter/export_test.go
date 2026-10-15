package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"scadlab/assets"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/threemf"
)

const nameplateTemplate = "skills/openscad-templates/templates/nameplate-two-color.scad"

// rig arma un workspace temporal con el motor real. Se salta sin OpenSCAD.
type rig struct {
	t       *testing.T
	dir     string
	eng     *render.Engine
	bin     string
	printer printers.Printer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	eng, err := render.NewEngine(filepath.Join(t.TempDir(), "meshes"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	pr, _ := printers.ByID("bambu-a1-mini")
	return &rig{t: t, dir: t.TempDir(), eng: eng, bin: bin, printer: pr}
}

func (r *rig) write(name, body string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) job(name string) Job {
	return Job{
		Engine:   r.eng,
		Req:      render.Request{WorkspaceKey: "test:export", WorkspaceDir: r.dir, FileName: name},
		Opts:     render.Options{OpenSCADPath: r.bin, Backend: "manifold", TimeoutSeconds: 120},
		CacheDir: filepath.Join(r.dir, ".cache-exports"),
		Printer:  r.printer,
	}
}

func (r *rig) nameplate() {
	r.t.Helper()
	b, err := assets.FS.ReadFile(nameplateTemplate)
	if err != nil {
		r.t.Fatal(err)
	}
	r.write("001-nameplate.scad", string(b))
}

func hexes(p *Prepared) []string {
	var out []string
	for _, i := range p.Infos() {
		out = append(out, i.Color)
	}
	return out
}

// La placa de dos colores sale en dos partes con los colores de la plantilla,
// y las partes juntas son la misma geometría que el STL.
func TestNameplateTwoPartsMatchSTL(t *testing.T) {
	r := newRig(t)
	r.nameplate()
	ctx := context.Background()

	stl, err := Prepare(ctx, r.job("001-nameplate.scad"), FormatSTL)
	if err != nil {
		t.Fatal(err)
	}
	stlInfo, err := mesh.InfoFromFile(stl.meshPath)
	if err != nil {
		t.Fatal(err)
	}

	p, err := Prepare(ctx, r.job("001-nameplate.scad"), Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	infos := p.Infos()
	if len(infos) != 2 || infos[0].Color != "#FFFFFF" || infos[1].Color != "#D32F2F" {
		t.Fatalf("partes: %+v", infos)
	}
	if infos[0].Extruder != 1 || infos[1].Extruder != 2 || infos[0].Uncolored || infos[1].Uncolored {
		t.Errorf("extrusores/uncolored: %+v", infos)
	}
	if w := p.Warnings(); len(w) != 0 {
		t.Errorf("la A1 mini admite 4 colores, no hay aviso: %v", w)
	}
	total := 0
	for _, i := range infos {
		total += i.Triangles
	}
	if total != stlInfo.Triangles {
		t.Errorf("triángulos de las partes %d, del STL %d", total, stlInfo.Triangles)
	}

	dest := filepath.Join(t.TempDir(), "placa.3mf")
	if err := p.Write(dest); err != nil {
		t.Fatal(err)
	}
	// Mismo volumen que el STL: se relee el 3MF escrito y se vuelca a un STL.
	back, err := threemf.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var all threemf.Part
	all.Vertices = back.Vertices
	for _, tr := range back.Triangles {
		all.Triangles = append(all.Triangles, tr.V)
	}
	var buf bytes.Buffer
	if err := threemf.WriteSTL(&buf, all); err != nil {
		t.Fatal(err)
	}
	got, err := mesh.ParseSTL(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if rel := math.Abs(got.VolumeMM3-stlInfo.VolumeMM3) / stlInfo.VolumeMM3; rel > 1e-4 {
		t.Errorf("volumen del 3MF %.3f, del STL %.3f (dif. relativa %.2g)", got.VolumeMM3, stlInfo.VolumeMM3, rel)
	}
	for k := 0; k < 3; k++ {
		if math.Abs(got.Size[k]-stlInfo.Size[k]) > 1e-3 {
			t.Errorf("tamaño %v vs %v", got.Size, stlInfo.Size)
		}
	}
	checkPackage(t, dest, 2)

	// La segunda vez sale de la caché de exportaciones.
	res, err := r.eng.RenderArtifact(ctx, r.job("001-nameplate.scad").Req, r.job("001-nameplate.scad").Opts, render.ArtifactExt3MF, r.job("x").CacheDir)
	if err != nil || res.Status != render.StatusOK || !res.CacheHit {
		t.Errorf("segunda llamada: %+v err=%v", res, err)
	}
}

func TestModelWithoutColorIsOnePart(t *testing.T) {
	r := newRig(t)
	r.write("001-cubo.scad", "cube(10);\n")
	p, err := Prepare(context.Background(), r.job("001-cubo.scad"), Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	infos := p.Infos()
	if len(infos) != 1 || !infos[0].Uncolored || infos[0].Extruder != 1 || infos[0].Triangles != 12 {
		t.Fatalf("partes: %+v", infos)
	}
	if len(p.Warnings()) != 0 {
		t.Errorf("un modelo sin color no avisa: %v", p.Warnings())
	}
	dest := filepath.Join(t.TempDir(), "cubo.3mf")
	if err := p.Write(dest); err != nil {
		t.Fatal(err)
	}
	checkPackage(t, dest, 1)
}

func TestMoreColorsThanSlotsWarns(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	r := newRig(t)
	r.write("001-tres.scad", `color("red") cube(10);
translate([20,0,0]) color("green") cube(10);
translate([40,0,0]) color("blue") cube(10);
`)
	job := r.job("001-tres.scad")
	job.Printer = printers.Printer{ID: "sim", Name: "Impresora simulada", MulticolorSlots: 2, MulticolorSys: "AMS lite simulado"}
	p, err := Prepare(context.Background(), job, Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	if got := hexes(p); strings.Join(got, ",") != "#FF0000,#008000,#0000FF" {
		t.Fatalf("colores: %v", got)
	}
	w := p.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "3 colores") || !strings.Contains(w[0], "Impresora simulada") || !strings.Contains(w[0], "2 a la vez") {
		t.Fatalf("avisos: %q", w)
	}
	// Con la impresora real (4 slots) no hay aviso.
	if p2, _ := Prepare(context.Background(), r.job("001-tres.scad"), Format3MF); len(p2.Warnings()) != 0 {
		t.Errorf("avisos con 4 slots: %q", p2.Warnings())
	}
}

func TestUncoloredAndNearColorWarnings(t *testing.T) {
	r := newRig(t)
	r.write("001-mezcla.scad", `color("#ff0000") cube(10);
translate([20,0,0]) color("#fe0101") cube(10);
translate([40,0,0]) cube(10);
`)
	p, err := Prepare(context.Background(), r.job("001-mezcla.scad"), Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.Infos()); n != 3 {
		t.Fatalf("partes: %+v", p.Infos())
	}
	w := strings.Join(p.Warnings(), "\n")
	if !strings.Contains(w, "color()") || !strings.Contains(w, "#FF0000") || !strings.Contains(w, "#FE0101") {
		t.Errorf("avisos: %q", w)
	}
}

func TestBrokenModelFailsWithLine(t *testing.T) {
	r := newRig(t)
	r.write("001-roto.scad", "cube(10;\n")
	for _, f := range []string{Format3MF, FormatSTL, FormatSTLPerColor} {
		_, err := Prepare(context.Background(), r.job("001-roto.scad"), f)
		if err == nil || !strings.Contains(err.Error(), "line 1") {
			t.Errorf("%s: error = %v", f, err)
		}
	}
	if _, err := Prepare(context.Background(), r.job("001-roto.scad"), "obj"); err == nil {
		t.Error("un formato desconocido tiene que fallar")
	}
}

func TestSTLAndSTLPerColor(t *testing.T) {
	r := newRig(t)
	r.nameplate()
	ctx := context.Background()
	dir := t.TempDir()

	stl, err := Prepare(ctx, r.job("001-nameplate.scad"), FormatSTL)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "placa.stl")
	if err := stl.Write(dest); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(dest)
	b, _ := os.ReadFile(stl.meshPath)
	if len(a) == 0 || !bytes.Equal(a, b) {
		t.Error("el STL exportado tiene que ser la malla del render, byte a byte")
	}
	if len(stl.Infos()) != 0 {
		t.Errorf("el STL no se reparte en partes: %+v", stl.Infos())
	}

	pc, err := Prepare(ctx, r.job("001-nameplate.scad"), FormatSTLPerColor)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "Mi placa.zip")
	if err := pc.Write(zipPath); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		info, err := mesh.ParseSTL(bytes.NewReader(data))
		if err != nil || info.Triangles == 0 {
			t.Errorf("%s: %v %+v", f.Name, err, info)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "Mi placa-d32f2f.stl,Mi placa-ffffff.stl" {
		t.Errorf("entradas: %v", names)
	}
}

// Sin OpenSCAD: el contrato del Renderer y los mensajes de error.
type fakeRenderer struct{ res render.Result }

func (f fakeRenderer) Render(context.Context, render.Request, render.Options) (render.Result, error) {
	return f.res, nil
}
func (f fakeRenderer) RenderArtifact(context.Context, render.Request, render.Options, string, string) (render.Result, error) {
	return f.res, nil
}

func TestRenderFailureMessages(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	cases := []struct {
		name string
		res  render.Result
		want string
	}{
		{"missing", render.Result{Status: render.StatusMissing}, "OpenSCAD no está instalado"},
		{"cancelled", render.Result{Status: render.StatusCancelled}, "se canceló"},
		{"error con línea", render.Result{Status: render.StatusError, Diagnostics: []render.Diagnostic{
			{Severity: render.SeverityWarning, Message: "aviso"},
			{Severity: render.SeverityError, Line: 7, Message: "boom"},
		}}, "(línea 7): boom"},
		{"error sin diagnósticos", render.Result{Status: render.StatusError}, "no se puede renderizar"},
	}
	for _, c := range cases {
		for _, f := range []string{FormatSTL, Format3MF} {
			_, err := Prepare(context.Background(), Job{Engine: fakeRenderer{c.res}, Req: render.Request{FileName: "001-a.scad"}}, f)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s/%s: %v, quería %q", c.name, f, err, c.want)
			}
		}
	}
}

func TestNamesAndExtensions(t *testing.T) {
	if got := SuggestedName("mi-placa", "003-soporte.scad", Format3MF); got != "mi-placa-003-soporte.3mf" {
		t.Errorf("sugerido: %q", got)
	}
	if got := SuggestedName(`a:b/c`, "001-x.scad", FormatSTLPerColor); got != "a-b-c-001-x.zip" {
		t.Errorf("sugerido: %q", got)
	}
	if Ext(FormatSTL) != ".stl" || Ext(Format3MF) != ".3mf" || Ext(FormatSTLPerColor) != ".zip" {
		t.Error("extensiones")
	}
	if SafeName("  ") != "scadlab" || SafeName("x. ") != "x" {
		t.Error("SafeName")
	}
}

// checkPackage valida un 3MF escrito contra la estructura 3MF core. Es la misma
// comprobación que backend/threemf, sobre el archivo de punta a punta.
func checkPackage(t *testing.T, path string, nParts int) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	have := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		have[f.Name] = string(b)
	}
	for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "3D/3dmodel.model", "Metadata/model_settings.config"} {
		if have[n] == "" {
			t.Errorf("falta %s", n)
		}
	}
	m := have["3D/3dmodel.model"]
	if !strings.Contains(m, `unit="millimeter"`) || strings.Count(m, "<mesh>") != nParts || strings.Count(m, "<base ") != nParts {
		t.Errorf("modelo: unit/mallas/materiales no cuadran con %d partes", nParts)
	}
	if strings.Count(have["Metadata/model_settings.config"], "<part ") != nParts {
		t.Errorf("model_settings sin %d partes", nParts)
	}
}
