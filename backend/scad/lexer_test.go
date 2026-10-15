package scad

import (
	"errors"
	"testing"
)

func kinds(ts []Token) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Kind.String() + ":" + t.Text
	}
	return out
}

func TestLexBasic(t *testing.T) {
	ts, err := Lex("include <lib/BOSL2/std.scad>\nx=-1.5e-3; // fin\r\nmodule 3d(){ cube($fn=0x10); } /* a\nb */ s=\"a\\\"{\";")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"keyword:include", "path:<lib/BOSL2/std.scad>", "newline:\n",
		"ident:x", "op:=", "op:-", "number:1.5e-3", "punct:;", "comment:// fin", "newline:\n",
		"keyword:module", "ident:3d", "punct:(", "punct:)", "punct:{", "ident:cube", "punct:(", "ident:$fn", "op:=", "number:0x10", "punct:)", "punct:;", "punct:}",
		"comment:/* a\nb */", "ident:s", "op:=", `string:"a\"{"`, "punct:;",
	}
	got := kinds(ts)
	if len(got) != len(want) {
		t.Fatalf("tokens:\n%q\nwant\n%q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, got[i], want[i])
		}
	}
	// Posiciones: «s» va después del comentario de dos líneas.
	last := ts[len(ts)-4]
	if last.Text != "s" || last.Line != 4 || last.Col != 6 {
		t.Errorf("posición de s: %+v", last)
	}
}

func TestLexNumbersVsIdents(t *testing.T) {
	cases := map[string]string{"1e5": "number", "3d": "ident", ".5": "number", "2.": "number", "10mm": "ident", "0x1F": "number", "$preview": "ident"}
	for src, kind := range cases {
		ts, err := Lex(src)
		if err != nil || len(ts) != 1 || ts[0].Kind.String() != kind {
			t.Errorf("%s: %v %v", src, kinds(ts), err)
		}
	}
}

// Las columnas cuentan unidades UTF-16, como Monaco: «é» suma 1 y un emoji 2.
func TestLexColumnsUTF16(t *testing.T) {
	ts, err := Lex("s=\"é😀\"; x=1;")
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range ts {
		if tk.Text == "x" && tk.Col != 10 {
			t.Errorf("columna de x = %d, quiero 10", tk.Col)
		}
	}
}

func TestLexOperators(t *testing.T) {
	ts, _ := Lex("a<=b>=c==d!=e&&f||!g<<1>>2")
	ops := 0
	for _, tk := range ts {
		if tk.Kind == TokOp {
			ops++
		}
	}
	if ops != 9 {
		t.Errorf("operadores: %v", kinds(ts))
	}
}

func TestLexUseWithoutPathIsNotPath(t *testing.T) {
	ts, _ := Lex("a = b < c; use\n<x.scad>")
	for _, tk := range ts {
		if tk.Kind == TokPath {
			t.Errorf("no debería haber ruta: %v", kinds(ts))
		}
	}
}

func TestLexErrors(t *testing.T) {
	for _, src := range []string{`x = "abc`, "x = 1; /* sin cerrar", `s = "fin\`} {
		_, err := Lex(src)
		var le *LexError
		if !errors.As(err, &le) || le.Line != 1 {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestLexBOM(t *testing.T) {
	ts, err := Lex("\uFEFFcube(1);")
	if err != nil || ts[0].Text != "cube" || ts[0].Col != 1 {
		t.Errorf("%v %v", kinds(ts), err)
	}
}
