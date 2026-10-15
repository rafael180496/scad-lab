package scad

import (
	"context"
	"strings"
	"testing"
)

var formatCases = []struct {
	name   string
	in     string
	want   string
	indent int
}{
	{name: "espacios", in: "x=1+2*3;", want: "x = 1 + 2 * 3;\n"},
	{name: "unarios", in: "y=-x;z=!a;w=[-1,+2];v=a- -b;n=~m;",
		want: "y = -x; z = !a; w = [-1, +2]; v = a - -b; n = ~m;\n"},
	{name: "ternario", in: "r=a>b?a:-b;", want: "r = a > b ? a : -b;\n"},
	{name: "comparaciones", in: "if(a<=b&&c!=d||!e) cube();", want: "if (a <= b && c != d || !e) cube();\n"},
	{name: "numeros", in: "x=1e-3+.5-2.;", want: "x = 1e-3 + .5 - 2.;\n"},
	{name: "modificadores",
		in:   "#cube(1);\ntranslate([1,0,0]) %sphere(2);\n*cylinder(h=1);\n!cube();\nz = a*b%c;",
		want: "#cube(1);\ntranslate([1, 0, 0]) %sphere(2);\n*cylinder(h = 1);\n!cube();\nz = a * b % c;\n"},
	{name: "bloques",
		in:   "module m(){\ncube(1);\nif(a){\nsphere(1);\n}\n}",
		want: "module m() {\n    cube(1);\n    if (a) {\n        sphere(1);\n    }\n}\n"},
	{name: "for e if sin llaves",
		in:   "for(i=[0:2])\ntranslate([i,0,0])\ncube(1);\nif (a)\ncube();\nelse\nsphere();",
		want: "for (i = [0 : 2])\n    translate([i, 0, 0])\n        cube(1);\nif (a)\n    cube();\nelse\n    sphere();\n"},
	{name: "difference",
		in:   "difference(){\ncube(10);\ntranslate([0,0,-1])\ncylinder(r=2,h=12);\n}",
		want: "difference() {\n    cube(10);\n    translate([0, 0, -1])\n        cylinder(r = 2, h = 12);\n}\n"},
	{name: "comentarios y strings con llaves",
		in:   "/* bloque { no cuenta\n   segunda línea */\nx = \"{ [ (\";   // comentario con {   \n  // comentario mal indentado\nmodule m() {\n        /* uno */ cube(1);\n}",
		want: "/* bloque { no cuenta\n   segunda línea */\nx = \"{ [ (\";   // comentario con {\n// comentario mal indentado\nmodule m() {\n    /* uno */ cube(1);\n}\n"},
	{name: "comentario pegado", in: "x=1;//c", want: "x = 1; //c\n"},
	{name: "comentario de bloque multilínea se corre entero",
		in:   "module m() {\n/*\n * doc\n */\ncube(1);\n}",
		want: "module m() {\n    /*\n     * doc\n     */\n    cube(1);\n}\n"},
	{name: "comentario de bloque que no se puede correr a la izquierda",
		in:   "        /* a\nb */\ncube();",
		want: "/* a\nb */\ncube();\n"},
	{name: "listas multilínea",
		in:   "pts = [\n[0,0],\n[1,0],\n  [1,1]\n];\npts2 = [[0, 0],\n[1, 1]];",
		want: "pts = [\n    [0, 0],\n    [1, 0],\n    [1, 1]\n];\npts2 = [[0, 0],\n        [1, 1]];\n"},
	{name: "argumentos alineados",
		in:   "rrect([a, b],\n   max(c, 0));",
		want: "rrect([a, b],\n      max(c, 0));\n"},
	{name: "let y comprensiones",
		in:   "function f(n) =\nlet(s=n*2)\n[for(i=[0:n]) if(i%2==0) i*s];\nv=[each [1,2], for (i=[0:1]) [i,i]];\nw = [for (i = [0 : 2])\nlet(t = i)\nt];",
		want: "function f(n) =\n    let(s = n * 2)\n    [for (i = [0 : n]) if (i % 2 == 0) i * s];\nv = [each [1, 2], for (i = [0 : 1]) [i, i]];\nw = [for (i = [0 : 2])\n    let(t = i)\n    t];\n"},
	{name: "continuación de expresión", in: "total = a +\nb +\n    c;", want: "total = a +\n    b +\n    c;\n"},
	{name: "líneas en blanco", in: "\n\n\ncube(1);   \n\n\n\nsphere(1);\n\n\n", want: "cube(1);\n\nsphere(1);\n"},
	{name: "crlf", in: "x=1;\r\n\r\n\r\ny=2; // c\r\n", want: "x = 1;\r\n\r\ny = 2; // c\r\n"},
	{name: "indentación 2", in: "module m() {\ncube();\n}", want: "module m() {\n  cube();\n}\n", indent: 2},
	{name: "include", in: "include<lib/BOSL2/std.scad>\nuse <MCAD/gears.scad>\ncube();",
		want: "include <lib/BOSL2/std.scad>\nuse <MCAD/gears.scad>\ncube();\n"},
	{name: "miembros e índices", in: "a=p.x+q[0][1]+f(x)[2]+\"ab\"[0];", want: "a = p.x + q[0][1] + f(x)[2] + \"ab\"[0];\n"},
	{name: "allman", in: "module m()\n{\ncube();\n}", want: "module m()\n{\n    cube();\n}\n"},
	{name: "función literal", in: "f=function(x) x*2;", want: "f = function(x) x * 2;\n"},
	{name: "argumentos con nombre", in: "module m(a=1,b=[1,2]) cylinder(h=a,r=b[0]);",
		want: "module m(a = 1, b = [1, 2]) cylinder(h = a, r = b[0]);\n"},
	{name: "una línea con else", in: "if(a){cube();}else if(b){sphere();}else{cylinder();}",
		want: "if (a) { cube(); } else if (b) { sphere(); } else { cylinder(); }\n"},
	{name: "llaves vacías", in: "module m(){}", want: "module m() {}\n"},
	{name: "string multilínea intacto", in: "s = \"a\n   b\";\n   x=1;", want: "s = \"a\n   b\";\nx = 1;\n"},
	{name: "vacío", in: "", want: ""},
	{name: "solo blancos", in: "\n  \n\t\n", want: ""},
}

func TestFormatTable(t *testing.T) {
	for _, c := range formatCases {
		t.Run(c.name, func(t *testing.T) {
			indent := c.indent
			if indent == 0 {
				indent = 4
			}
			got, err := Format(c.in, FormatOptions{IndentSize: indent})
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("Format:\n%s\n--- quiero ---\n%s", got, c.want)
			}
			again, err := Format(got, FormatOptions{IndentSize: indent})
			if err != nil || again != got {
				t.Fatalf("no es idempotente:\n%s\n--- segunda pasada ---\n%s", got, again)
			}
			assertSameTokens(t, c.in, got)
		})
	}
}

func TestFormatErrors(t *testing.T) {
	for _, src := range []string{"x = \"abc", "/* sin cerrar"} {
		if _, err := Format(src, FormatOptions{}); err == nil {
			t.Errorf("%q: quiero error", src)
		}
	}
}

// assertSameTokens: formatear no cambia el código, solo los blancos. Compara
// los tokens sin saltos de línea; en los comentarios ignora los blancos (los
// de línea pierden los finales y los de bloque se corren enteros).
func assertSameTokens(t *testing.T, a, b string) {
	t.Helper()
	ta, err := Lex(a)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := Lex(b)
	if err != nil {
		t.Fatal(err)
	}
	norm := func(ts []Token) []string {
		var out []string
		for _, tk := range ts {
			switch tk.Kind {
			case TokNewline:
				continue
			case TokComment:
				out = append(out, "comment:"+strings.Join(strings.Fields(tk.Text), ""))
			default:
				out = append(out, tk.Kind.String()+":"+tk.Text)
			}
		}
		return out
	}
	na, nb := norm(ta), norm(tb)
	if strings.Join(na, "\x00") != strings.Join(nb, "\x00") {
		t.Fatalf("el formateo cambió los tokens:\n%q\n%q", na, nb)
	}
}

// FuzzFormat: con cualquier entrada que el lexer acepte, Format es
// idempotente y conserva los tokens; Lint no entra en pánico.
func FuzzFormat(f *testing.F) {
	for _, c := range formatCases {
		f.Add(c.in)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_ = Lint(context.Background(), src, LintOptions{FileName: "001-x.scad"})
		once, err := Format(src, FormatOptions{IndentSize: 4})
		if err != nil {
			return
		}
		if twice, _ := Format(once, FormatOptions{IndentSize: 4}); twice != once {
			t.Fatalf("no es idempotente:\n%q\n%q", once, twice)
		}
		assertSameTokens(t, src, once)
	})
}
