package render

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Salidas capturadas de OpenSCAD 2026.10.11 real (snapshot), para que el
// parser no dependa solo de formatos supuestos.
func TestParseRealOpenSCAD2026(t *testing.T) {
	cases := []struct {
		name, out string
		want      []Diagnostic
	}{
		{"syntax", `ERROR: Parser error: syntax error in file syn.scad, line 1`,
			[]Diagnostic{{Severity: SeverityError, Line: 1}}},
		{"recursion", "ERROR: Recursion detected calling module 'a' in file rec.scad, line 1\nTRACE: called by 'a' in file rec.scad, line 2",
			[]Diagnostic{{Severity: SeverityError, Line: 1}}},
		{"assert", "ERROR: Assertion '(1 > 2)' failed: \"demasiado grande\" in file asrt.scad, line 1\nTRACE: called by 'assert' in file asrt.scad, line 1\nCurrent top level object is empty.",
			[]Diagnostic{{Severity: SeverityError, Line: 1}}},
		{"warnings", "WARNING: Ignoring unknown function 'undefined_fn' in file warn.scad, line 3\nWARNING: Ignoring unknown variable \"foo\" in file warn.scad, line 1\nECHO: \"hola\"",
			[]Diagnostic{{Severity: SeverityWarning, Line: 3}, {Severity: SeverityWarning, Line: 1}}},
		{"include", `WARNING: Can't find include file 'missing.scad'. in file inc.scad, line 1`,
			[]Diagnostic{{Severity: SeverityWarning, Line: 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseOutput(c.out, "/ws", "x.scad")
			if len(got) != len(c.want) {
				t.Fatalf("diagnósticos: %+v", got)
			}
			for i, w := range c.want {
				if got[i].Severity != w.Severity || got[i].Line != w.Line || got[i].Message == "" {
					t.Errorf("[%d] = %+v, quería %+v", i, got[i], w)
				}
				if strings.Contains(got[i].Message, "in file") {
					t.Errorf("[%d] el mensaje conserva la ubicación: %q", i, got[i].Message)
				}
			}
		})
	}
	if !isEmptyModel("Current top level object is empty.") || !isNot3D("Current top level object is not a 3D object.") {
		t.Error("no reconoce los avisos de modelo vacío / 2D")
	}
}

// TestRenderWithRealOpenSCAD corre el motor contra el OpenSCAD instalado, si
// lo hay. Es la única prueba que confirma que los flags y mensajes son reales.
func TestRenderWithRealOpenSCAD(t *testing.T) {
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	ws := t.TempDir()
	files := map[string]string{
		"001-ok.scad":     "color(\"red\") cube(10);\n",
		"002-syntax.scad": "cube(10;\n",
		"003-assert.scad": "assert(1 > 2, \"grande\");\ncube(1);\n",
		"004-2d.scad":     "square(10);\n",
		"005-empty.scad":  "translate([1,0,0]);\n",
	}
	for n, body := range files {
		if err := os.WriteFile(filepath.Join(ws, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	eng, err := NewEngine(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	opts := Options{OpenSCADPath: bin, Backend: "manifold", TimeoutSeconds: 120}
	render := func(f string) Result {
		r, err := eng.Render(context.Background(), Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: f}, opts)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		return r
	}

	ok := render("001-ok.scad")
	if ok.Status != StatusOK || ok.MeshPath == "" || ok.Version == "" {
		t.Fatalf("ok: %+v", ok)
	}
	if again := render("001-ok.scad"); !again.CacheHit {
		t.Error("el segundo render debería salir de la caché")
	}
	if r := render("002-syntax.scad"); r.Status != StatusError || len(r.Diagnostics) == 0 || r.Diagnostics[0].Line != 1 {
		t.Errorf("syntax: %+v", r.Diagnostics)
	}
	r := render("003-assert.scad")
	if r.Status != StatusError || len(r.Diagnostics) != 1 || !strings.Contains(r.Diagnostics[0].Message, "Assertion") {
		t.Errorf("assert: debería quedar solo el error real, hay %+v", r.Diagnostics)
	}
	if r := render("004-2d.scad"); r.Status != StatusError || !strings.Contains(r.Diagnostics[len(r.Diagnostics)-1].Message, "linear_extrude") {
		t.Errorf("2d: %+v", r.Diagnostics)
	}
	if r := render("005-empty.scad"); r.Status != StatusError || len(r.Diagnostics) == 0 {
		t.Errorf("empty: %+v", r)
	}
}
