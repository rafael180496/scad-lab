package render

import (
	"path/filepath"
	"reflect"
	"testing"
)

// Mensajes con la forma que imprimen OpenSCAD 2021.01 y los snapshots 2025.
// Verosímiles, no copiados de una instalación: si una versión real usa otra
// forma, se agrega acá como caso nuevo.
func TestParseOutput(t *testing.T) {
	ws := filepath.Join(string(filepath.Separator), "ws", "proj")
	main := "003-lid.scad"
	cases := []struct {
		name string
		in   string
		want []Diagnostic
	}{
		{
			name: "2021 parser error con archivo entre comillas y ruta absoluta",
			in:   `ERROR: Parser error in file "` + filepath.Join(ws, main) + `", line 3: syntax error`,
			want: []Diagnostic{{Severity: SeverityError, Line: 3, Message: "Parser error: syntax error"}},
		},
		{
			name: "2021 warning con archivo sin comillas y punto final",
			in:   "WARNING: Ignoring unknown variable 'wall', in file 003-lid.scad, line 12.",
			want: []Diagnostic{{Severity: SeverityWarning, Line: 12, Message: "Ignoring unknown variable 'wall'"}},
		},
		{
			name: "2025 warning con comillas dobles, include de lib",
			in:   `WARNING: Ignoring unknown module "hinge" in file "lib/parts.scad", line 7`,
			want: []Diagnostic{{Severity: SeverityWarning, Line: 7, File: "lib/parts.scad", Message: `Ignoring unknown module "hinge"`}},
		},
		{
			name: "2025 con columna",
			in:   `ERROR: Parser error in file "003-lid.scad", line 4, column 15: syntax error`,
			want: []Diagnostic{{Severity: SeverityError, Line: 4, Col: 15, Message: "Parser error: syntax error"}},
		},
		{
			name: "at line",
			in:   "ERROR: Assertion '(wall > 0)' failed at line 9",
			want: []Diagnostic{{Severity: SeverityError, Line: 9, Message: "Assertion '(wall > 0)' failed"}},
		},
		{
			name: "coma line sin archivo",
			in:   "DEPRECATED: Using ranges of the form [begin:end] with begin value greater than the end value is deprecated, line 21",
			want: []Diagnostic{{Severity: SeverityWarning, Line: 21, Message: "Using ranges of the form [begin:end] with begin value greater than the end value is deprecated"}},
		},
		{
			name: "error sin ubicación se reporta igual",
			in:   "WARNING: Can't open include file 'nope.scad'.\nERROR: CGAL error in CGAL_Nef_polyhedron3(): CGAL ERROR: assertion violation!",
			want: []Diagnostic{
				{Severity: SeverityWarning, Message: "Can't open include file 'nope.scad'"},
				{Severity: SeverityError, Message: "CGAL error in CGAL_Nef_polyhedron3(): CGAL ERROR: assertion violation!"},
			},
		},
		{
			name: "TRACE, ECHO y ruido se ignoran; CRLF tolerado",
			in:   "Compiling design (CSG Tree generation)...\r\nECHO: \"wall\", 2\r\nERROR: Assertion 'false' failed in file 003-lid.scad, line 2\r\nTRACE: called by 'box', in file 003-lid.scad, line 10.\r\nExecution aborted\r\n",
			want: []Diagnostic{{Severity: SeverityError, Line: 2, Message: "Assertion 'false' failed"}},
		},
		{
			name: "archivo fuera del workspace queda tal cual",
			in:   `WARNING: Ignoring unknown variable 'x' in file "/usr/share/openscad/libraries/MCAD/gears.scad", line 40`,
			want: []Diagnostic{{Severity: SeverityWarning, Line: 40, File: "/usr/share/openscad/libraries/MCAD/gears.scad", Message: "Ignoring unknown variable 'x'"}},
		},
		{
			name: "sin diagnósticos",
			in:   "Geometries in cache: 3\nTotal rendering time: 0:00:00.120\n",
			want: []Diagnostic{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseOutput(c.in, ws, main)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("\n got  %+v\n want %+v", got, c.want)
			}
		})
	}
}

func TestIsEmptyModel(t *testing.T) {
	for in, want := range map[string]bool{
		"Current top level object is empty.":           true,
		"WARNING: Current top-level object is empty.":  true,
		"WARNING: No top level geometry to render":     true,
		"Total rendering time: 0:00:01\n   Volumes: 2": false,
	} {
		if got := isEmptyModel(in); got != want {
			t.Errorf("isEmptyModel(%q) = %v", in, got)
		}
	}
}

func TestRejectsOption(t *testing.T) {
	if !rejectsOption("unrecognised option '--backend=manifold'\nUsage: ...", "backend") {
		t.Error("boost con 's' no reconocido")
	}
	if !rejectsOption("ERROR: unknown option --export-format", "export-format") {
		t.Error("unknown option no reconocido")
	}
	if rejectsOption("ERROR: unknown option --export-format", "backend") {
		t.Error("confundió la opción rechazada")
	}
}
