package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/render"
	"scadlab/backend/threemf"
	"scadlab/backend/threemf/macetatest"
)

// La maceta verde con relieve naranja curvado (S17, tarea 17.2): la que Bambu Studio abría con
// el hueco relleno y muchos soportes. Aquí, por la ruta de ScadLab con OpenSCAD real:
//
//   - el 3MF por defecto de OpenSCAD reparte en dos cáscaras abiertas (la causa);
//   - Prepare entrega dos sólidos cerrados, con el volumen y la caja del STL de cada color;
//   - lo que se escribe (3MF de Bambu, de PrusaSlicer y STL por color) conserva eso.
func TestPotPartsAreClosedSolids(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	r := newRig(t)
	r.write("001-maceta.scad", macetatest.Scad)
	ctx := context.Background()
	job := r.job("001-maceta.scad")

	// La causa: sin lazy-union el reparto por color deja las dos partes abiertas.
	res, err := r.eng.RenderArtifact(ctx, job.Req, job.Opts, render.ArtifactExt3MF, job.CacheDir)
	if err != nil || res.Status != render.StatusOK {
		t.Fatalf("render 3MF: %+v err=%v", res, err)
	}
	plain, err := threemf.ReadFile(res.MeshPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := threemf.OpenEdgeCount(threemf.Split(plain)); n == 0 {
		t.Fatal("el 3MF por defecto de OpenSCAD deberia dejar partes abiertas")
	}

	p, err := Prepare(ctx, job, Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	macetatest.AssertSolids(t, p.parts, 1e-3)
	for _, w := range p.Warnings() {
		if strings.Contains(w, "sólido cerrado") {
			t.Errorf("aviso de parte abierta con partes cerradas: %s", w)
		}
	}

	// Cada parte contra el STL que da el MISMO motor para ese color solo.
	for i, color := range []string{"verde", "naranja"} {
		r.write("002-"+color+".scad", macetatest.Only(color))
		ref, err := r.eng.Render(ctx, render.Request{WorkspaceKey: "test:ref", WorkspaceDir: r.dir, FileName: "002-" + color + ".scad"}, job.Opts)
		if err != nil || ref.Status != render.StatusOK {
			t.Fatalf("render de %s: %+v err=%v", color, ref, err)
		}
		info, err := mesh.InfoFromFile(ref.MeshPath)
		if err != nil || !info.Watertight {
			t.Fatalf("STL de %s: %+v err=%v", color, info, err)
		}
		c := threemf.Inspect(p.parts[i])
		if math.Abs(c.SignedVolume-info.VolumeMM3)/info.VolumeMM3 > 1e-4 {
			t.Errorf("%s: volumen de la parte %.3f, del STL %.3f", color, c.SignedVolume, info.VolumeMM3)
		}
		if !macetatest.Near(c.Min, info.Min, 1e-3) || !macetatest.Near(c.Max, info.Max, 1e-3) {
			t.Errorf("%s: caja de la parte %v–%v, del STL %v–%v", color, c.Min, c.Max, info.Min, info.Max)
		}
	}

	// Lo escrito: el 3MF de Bambu, el de PrusaSlicer y el STL por color.
	dir := t.TempDir()
	bambu := filepath.Join(dir, "maceta.3mf")
	if err := p.Write(bambu); err != nil {
		t.Fatal(err)
	}
	checkPackage(t, bambu, 2)
	back, err := threemf.ReadFile(bambu)
	if err != nil {
		t.Fatal(err)
	}
	reread := threemf.Split(back)
	for i, c := range func() []threemf.PartCheck { cs, _ := threemf.InspectAll(reread); return cs }() {
		if !c.Closed() {
			t.Errorf("parte %d del 3MF escrito, releída: %+v", i, c)
		}
	}
	if mn, mx, _ := threemf.Bounds(reread); math.Abs(mn[2]) > 1e-9 ||
		math.Abs((mn[0]+mx[0])/2-r.printer.BuildVolume.X/2) > 1e-6 || math.Abs((mn[1]+mx[1])/2-r.printer.BuildVolume.Y/2) > 1e-6 {
		t.Errorf("la pieza tiene que apoyarse en Z=0 y estar centrada en la cama: %v–%v", mn, mx)
	}

	pp, err := Prepare(ctx, job, Format3MFPrusa)
	if err != nil {
		t.Fatal(err)
	}
	if err := pp.Write(filepath.Join(dir, "maceta-prusa.3mf")); err != nil {
		t.Fatal(err)
	}

	pc, err := Prepare(ctx, job, FormatSTLPerColor)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "maceta.zip")
	if err := pc.Write(zipPath); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 2 {
		t.Fatalf("STL por color: %d entradas", len(zr.File))
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		info, err := mesh.ParseSTL(bytes.NewReader(data))
		if err != nil || !info.Watertight || info.VolumeMM3 <= 0 {
			t.Errorf("%s: %+v err=%v", f.Name, info, err)
		}
	}
}

// Un modelo cuyos colores no se tocan ya sale cerrado con el render normal: no paga un
// segundo render y su reparto no cambia.
func TestSeparatedColorsKeepTheUnionRender(t *testing.T) {
	r := newRig(t)
	r.write("001-dos.scad", "color(\"red\") cube(10);\ncolor(\"blue\") translate([20, 0, 0]) cube(10);\n")
	p, err := Prepare(context.Background(), r.job("001-dos.scad"), Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	if n := threemf.OpenEdgeCount(p.parts); n != 0 || len(p.parts) != 2 {
		t.Fatalf("partes %d, aristas abiertas %d", len(p.parts), n)
	}
}

// Dos colores que se funden dentro de un union() explícito no se pueden separar sin perder la
// cara de contacto: el aviso lo dice en vez de entregar el archivo en silencio.
func TestWarnsWhenPartsStayOpen(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	r := newRig(t)
	r.write("001-union.scad", "union() { color(\"red\") cube(10); color(\"blue\") translate([0, 0, 10]) cube(10); }\n")
	p, err := Prepare(context.Background(), r.job("001-union.scad"), Format3MF)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range p.Warnings() {
		if strings.Contains(w, "no es un sólido cerrado") && strings.Contains(w, "union()") {
			found = true
		}
	}
	if !found {
		t.Errorf("sin aviso de partes abiertas: %v", p.Warnings())
	}
}

// Sin OpenSCAD: el aviso sale de las partes que dejó el 3MF por defecto, y no sale con las
// cerradas.
func TestOpenPartsWarningFromFixtures(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	read := func(b []byte) []threemf.Part {
		m, err := threemf.Read(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		return threemf.Split(m)
	}
	var pr = (&rig{}).printer // impresora vacía: sin límite de colores
	open := 0
	for _, w := range warnings(read(macetatest.Union), pr) {
		if strings.Contains(w, "no es un sólido cerrado") {
			open++
		}
	}
	if open != 2 {
		t.Errorf("avisos de parte abierta: %d, quería 2 (verde y naranja)", open)
	}
	for _, w := range warnings(read(macetatest.Lazy), pr) {
		if strings.Contains(w, "sólido cerrado") {
			t.Errorf("aviso inesperado con partes cerradas: %s", w)
		}
	}
}
