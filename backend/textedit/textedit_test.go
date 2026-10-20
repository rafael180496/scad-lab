package textedit

import (
	"strings"
	"testing"

	"scadlab/backend/i18n"
)

func init() { i18n.SetLang("es") }

func old(o, n string) Edit { return Edit{Old: o, New: n, OldSet: true} }
func lines(from, to int, n string) Edit {
	return Edit{FromLine: from, ToLine: to, New: n, LinesSet: true}
}

func mustApply(t *testing.T, src string, opts Options, edits ...Edit) Result {
	t.Helper()
	r, err := Apply(src, edits, opts)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return r
}

func mustFail(t *testing.T, src string, opts Options, want string, edits ...Edit) {
	t.Helper()
	r, err := Apply(src, edits, opts)
	if err == nil {
		t.Fatalf("esperaba error con %q, obtuve el texto %q", want, r.Text)
	}
	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q no contiene %q", err.Error(), want)
	}
}

func TestLinesAndCount(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\n", []string{"a"}},
		{"a\n\n", []string{"a", ""}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"\uFEFFa\nb", []string{"a", "b"}},
		{"\n", []string{""}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"ñandú ✓\n😀", []string{"ñandú ✓", "😀"}},
	}
	for _, c := range cases {
		got := Lines(c.src)
		if strings.Join(got, "|") != strings.Join(c.want, "|") || len(got) != len(c.want) {
			t.Errorf("Lines(%q) = %q, quería %q", c.src, got, c.want)
		}
		if CountLines(c.src) != len(c.want) {
			t.Errorf("CountLines(%q) = %d", c.src, CountLines(c.src))
		}
	}
}

func TestTextEditExactlyOnce(t *testing.T) {
	src := "// @checkpoint parent= desc=\"x\"\nwidth = 20;\ndepth = 10;\ncube([width, depth, 5]);\n"
	r := mustApply(t, src, Options{ProtectHeader: true}, old("width = 20;", "width = 35;"))
	want := "// @checkpoint parent= desc=\"x\"\nwidth = 35;\ndepth = 10;\ncube([width, depth, 5]);\n"
	if r.Text != want || r.Unchanged {
		t.Fatalf("texto: %q", r.Text)
	}
	if len(r.Changes) != 1 {
		t.Fatalf("cambios: %+v", r.Changes)
	}
	c := r.Changes[0]
	if c.Index != 1 || c.Kind != "text" || c.From != 2 || c.To != 2 || c.Old != "width = 20;" || c.NewFrom != 2 || c.NewTo != 2 || c.New != "width = 35;" {
		t.Errorf("cambio: %+v", c)
	}
}

func TestTextEditAcrossLines(t *testing.T) {
	src := "a\nb\nc\nd\n"
	r := mustApply(t, src, Options{}, old("b\nc", "X"))
	if r.Text != "a\nX\nd\n" {
		t.Fatalf("texto: %q", r.Text)
	}
	c := r.Changes[0]
	if c.From != 2 || c.To != 3 || c.Old != "b\nc" || c.NewFrom != 2 || c.NewTo != 2 {
		t.Errorf("cambio: %+v", c)
	}
}

func TestTextEditAmbiguousAndNotFound(t *testing.T) {
	src := "cube(1);\nsphere(2);\ncube(1);\n  translate([0,0,1]) cube(3);\n"
	mustFail(t, src, Options{}, "aparece 2 veces, en las líneas 1, 3", old("cube(1);", "cube(2);"))
	// Las apariciones que se solapan también son ambiguas.
	mustFail(t, "aaa", Options{}, "aparece 2 veces", old("aa", "b"))
	// No encontrado: sugiere la línea más parecida con su número.
	mustFail(t, src, Options{}, "la línea más parecida es la 4: `translate([0,0,1]) cube(3);`", old("translate([0, 0, 1]) cube(3);", "x"))
	mustFail(t, src, Options{}, "no hay ninguna línea parecida", old("zzzzzzzz qqqq", "x"))
	// Un `old` muy largo se recorta en el mensaje.
	long := strings.Repeat("abcdefghij ", 30)
	_, err := Apply(long+"\n", []Edit{old(long+"X", "y")}, Options{})
	if err == nil || strings.Count(err.Error(), "abcdefghij") > 13 || !strings.Contains(err.Error(), "…") {
		t.Errorf("la línea sugerida no se recortó: %v", err)
	}
}

func TestEmptyAndIdenticalOld(t *testing.T) {
	mustFail(t, "a\n", Options{}, "`old` está vacío", old("", "x"))
	mustFail(t, "a\n", Options{}, "idénticos", old("a", "a"))
	mustFail(t, "a\n", Options{}, "no hay ediciones")
	mustFail(t, "a\n", Options{}, "usa una sola forma", Edit{Old: "a", New: "b", OldSet: true, FromLine: 1, ToLine: 1, LinesSet: true})
	mustFail(t, "a\n", Options{}, "falta `old`", Edit{})
}

func TestLineRangeReplaceInsertDelete(t *testing.T) {
	src := "l1\nl2\nl3\nl4\nl5\n"
	// Reemplazar l2-l3 por tres líneas.
	r := mustApply(t, src, Options{}, lines(2, 3, "A\nB\nC"))
	if r.Text != "l1\nA\nB\nC\nl4\nl5\n" {
		t.Fatalf("reemplazar: %q", r.Text)
	}
	if c := r.Changes[0]; c.From != 2 || c.To != 3 || c.Old != "l2\nl3" || c.NewFrom != 2 || c.NewTo != 4 || c.New != "A\nB\nC" {
		t.Errorf("cambio: %+v", c)
	}
	// Una línea final \n en new es opcional.
	if got := mustApply(t, src, Options{}, lines(2, 2, "A\n")).Text; got != "l1\nA\nl3\nl4\nl5\n" {
		t.Errorf("con salto final: %q", got)
	}
	// Insertar antes de la 3 (to = from-1).
	r = mustApply(t, src, Options{}, lines(3, 2, "NEW"))
	if r.Text != "l1\nl2\nNEW\nl3\nl4\nl5\n" {
		t.Fatalf("insertar: %q", r.Text)
	}
	if c := r.Changes[0]; c.From != 3 || c.To != 2 || c.Old != "" || c.NewFrom != 3 || c.NewTo != 3 {
		t.Errorf("cambio de inserción: %+v", c)
	}
	// Borrar (new vacío).
	r = mustApply(t, src, Options{}, lines(2, 4, ""))
	if r.Text != "l1\nl5\n" {
		t.Fatalf("borrar: %q", r.Text)
	}
	if c := r.Changes[0]; c.Old != "l2\nl3\nl4" || c.NewFrom != 2 || c.NewTo != 1 {
		t.Errorf("cambio de borrado: %+v", c)
	}
	// Una línea en blanco explícita no es borrar.
	if got := mustApply(t, src, Options{}, lines(2, 2, "\n")).Text; got != "l1\n\nl3\nl4\nl5\n" {
		t.Errorf("línea en blanco: %q", got)
	}
	// Reemplazar todo el archivo.
	if got := mustApply(t, src, Options{}, lines(1, 5, "solo")).Text; got != "solo\n" {
		t.Errorf("todo: %q", got)
	}
}

func TestAppendAtEnd(t *testing.T) {
	// Con salto final.
	r := mustApply(t, "a\nb\n", Options{}, lines(3, 2, "c"))
	if r.Text != "a\nb\nc\n" {
		t.Fatalf("con salto final: %q", r.Text)
	}
	if c := r.Changes[0]; c.From != 3 || c.NewFrom != 3 || c.NewTo != 3 || c.New != "c" {
		t.Errorf("cambio: %+v", c)
	}
	// Sin salto final: se conserva así y la última línea no pierde nada.
	r = mustApply(t, "a\nb", Options{}, lines(3, 2, "c"))
	if r.Text != "a\nb\nc" {
		t.Fatalf("sin salto final: %q", r.Text)
	}
	if c := r.Changes[0]; c.From != 3 || c.NewFrom != 3 || c.NewTo != 3 {
		t.Errorf("cambio sin salto final: %+v", c)
	}
	// Archivo vacío.
	if got := mustApply(t, "", Options{}, lines(1, 0, "x")).Text; got != "x\n" {
		t.Errorf("vacío: %q", got)
	}
	// Reemplazar la última línea de un archivo sin salto final mantiene que no lo tenga.
	if got := mustApply(t, "a\nb", Options{}, lines(2, 2, "B")).Text; got != "a\nB" {
		t.Errorf("última sin salto: %q", got)
	}
	// Borrar la última línea de un archivo sin salto final.
	if got := mustApply(t, "a\nb", Options{}, lines(2, 2, "")).Text; got != "a\n" {
		t.Errorf("borrar última: %q", got)
	}
}

func TestLineRangeErrors(t *testing.T) {
	src := "a\nb\nc\n"
	mustFail(t, src, Options{}, "from_line debe ser 1 o más", lines(0, 1, "x"))
	mustFail(t, src, Options{}, "from_line 9 está fuera del archivo, que tiene 3 líneas (para agregar al final usa from_line = 4)", lines(9, 9, "x"))
	mustFail(t, src, Options{}, "to_line 7 está fuera del archivo", lines(2, 7, "x"))
	mustFail(t, src, Options{}, "to_line (1) no puede ser menor", lines(3, 1, "x"))
	mustFail(t, src, Options{}, "no hay nada que insertar", lines(2, 1, ""))
	mustFail(t, "", Options{}, "from_line 2 está fuera del archivo, que tiene 0 líneas", lines(2, 2, "x"))
}

func TestCRLF(t *testing.T) {
	src := "a\r\nb\r\nc\r\n"
	// old con \n sobre un archivo CRLF.
	r := mustApply(t, src, Options{}, old("a\nb", "A\nB"))
	if r.Text != "A\r\nB\r\nc\r\n" {
		t.Fatalf("old: %q", r.Text)
	}
	// Líneas: lo insertado toma CRLF, el terminador de la última reemplazada se conserva.
	r = mustApply(t, src, Options{}, lines(2, 2, "X\nY"))
	if r.Text != "a\r\nX\r\nY\r\nc\r\n" {
		t.Fatalf("lines: %q", r.Text)
	}
	r = mustApply(t, src, Options{}, lines(2, 1, "X"))
	if r.Text != "a\r\nX\r\nb\r\nc\r\n" {
		t.Fatalf("insertar: %q", r.Text)
	}
	r = mustApply(t, src, Options{}, lines(4, 3, "d"))
	if r.Text != "a\r\nb\r\nc\r\nd\r\n" {
		t.Fatalf("agregar: %q", r.Text)
	}
	// CRLF sin salto final.
	if got := mustApply(t, "a\r\nb", Options{}, lines(3, 2, "c")).Text; got != "a\r\nb\r\nc" {
		t.Errorf("sin final: %q", got)
	}
	// Los números de línea no cuentan el \r.
	if r = mustApply(t, src, Options{}, old("c", "C")); r.Changes[0].From != 3 || r.Changes[0].Old != "c" {
		t.Errorf("cambio: %+v", r.Changes[0])
	}
}

func TestMixedEndingsKeepEveryLinesTerminator(t *testing.T) {
	src := "a\r\nb\nc\r\nd\n"
	r := mustApply(t, src, Options{}, lines(3, 3, "C"))
	// Dominante: empate → LF. La línea reemplazada conserva su \r\n.
	if r.Text != "a\r\nb\nC\r\nd\n" {
		t.Fatalf("texto: %q", r.Text)
	}
}

func TestTabsAndUnicode(t *testing.T) {
	src := "module m() {\n\tcube(1); // ñandú ✓\n\tsphere(2);\n}\n"
	r := mustApply(t, src, Options{}, old("\tcube(1); // ñandú ✓", "\tcube(2); // 😀"))
	if r.Text != "module m() {\n\tcube(2); // 😀\n\tsphere(2);\n}\n" {
		t.Fatalf("texto: %q", r.Text)
	}
	// Las diferencias de sangría no se «arreglan» en old: es un error con pista.
	mustFail(t, src, Options{}, "la línea más parecida es la 2", old("    cube(1); // ñandú ✓", "x"))
	// Tabuladores en new se conservan.
	r = mustApply(t, src, Options{}, lines(3, 3, "\t\tsphere(3);"))
	if !strings.Contains(r.Text, "\t\tsphere(3);\n}") {
		t.Errorf("tabs: %q", r.Text)
	}
	// Los desplazamientos son de bytes, no de runas: una edición tras un emoji cae bien.
	r = mustApply(t, "😀 a\nb 😀\n", Options{}, old("b 😀", "c"))
	if r.Text != "😀 a\nc\n" || r.Changes[0].From != 2 {
		t.Errorf("emoji: %q %+v", r.Text, r.Changes)
	}
}

func TestBOMIsKept(t *testing.T) {
	src := "\uFEFFa\nb\n"
	r := mustApply(t, src, Options{}, lines(2, 2, "B"))
	if r.Text != "\uFEFFa\nB\n" {
		t.Fatalf("texto: %q", r.Text)
	}
	if r.Changes[0].From != 2 {
		t.Errorf("el BOM no cuenta como línea: %+v", r.Changes[0])
	}
}

func TestOverlapsAreErrors(t *testing.T) {
	src := "a\nb\nc\nd\ne\n"
	mustFail(t, src, Options{}, "las ediciones 1 y 2 se solapan", lines(2, 3, "X"), lines(3, 4, "Y"))
	mustFail(t, src, Options{}, "se solapan", old("b\nc", "X"), old("c\nd", "Y"))
	mustFail(t, src, Options{}, "se solapan", lines(2, 4, "X"), old("c", "Y"))
	// Insertar dentro de un rango reemplazado también se solapa.
	mustFail(t, src, Options{}, "se solapan", lines(2, 4, "X"), lines(3, 2, "Y"))
	// El mismo rango dos veces.
	mustFail(t, src, Options{}, "se solapan", lines(2, 2, "X"), lines(2, 2, "Y"))
}

func TestAdjacentEditsAndOrderIndependence(t *testing.T) {
	src := "a\nb\nc\nd\ne\n"
	// Contiguos pero sin solape: válido, y el orden pedido no importa.
	want := "a\nB\nC\nD\ne\n"
	for _, es := range [][]Edit{
		{lines(2, 2, "B"), lines(3, 3, "C"), lines(4, 4, "D")},
		{lines(4, 4, "D"), lines(2, 2, "B"), lines(3, 3, "C")},
		{old("d", "D"), lines(2, 2, "B"), old("c", "C")},
	} {
		r := mustApply(t, src, Options{}, es...)
		if r.Text != want {
			t.Errorf("texto: %q", r.Text)
		}
	}
	// Los números son los del archivo ORIGINAL aunque una edición anterior agregue líneas.
	r := mustApply(t, src, Options{}, lines(1, 1, "A1\nA2\nA3"), lines(5, 5, "E"))
	if r.Text != "A1\nA2\nA3\nb\nc\nd\nE\n" {
		t.Errorf("originales: %q", r.Text)
	}
	if len(r.Changes) != 2 || r.Changes[0].Index != 1 || r.Changes[1].Index != 2 || r.Changes[1].From != 5 || r.Changes[1].NewFrom != 7 {
		t.Errorf("cambios: %+v", r.Changes)
	}
	// Una inserción justo después de un rango reemplazado no se solapa.
	r = mustApply(t, src, Options{}, lines(2, 3, "X"), lines(4, 3, "Y"))
	if r.Text != "a\nX\nY\nd\ne\n" {
		t.Errorf("inserción contigua: %q", r.Text)
	}
	// Dos inserciones en el mismo punto conservan el orden pedido.
	r = mustApply(t, src, Options{}, lines(3, 2, "P"), lines(3, 2, "Q"))
	if r.Text != "a\nb\nP\nQ\nc\nd\ne\n" {
		t.Errorf("dos inserciones: %q", r.Text)
	}
}

func TestAllOrNothing(t *testing.T) {
	src := "a\nb\nc\n"
	r, err := Apply(src, []Edit{lines(1, 1, "A"), old("no existe", "x")}, Options{})
	if err == nil {
		t.Fatal("la segunda edición falla: no puede aplicarse ninguna")
	}
	if r.Text != "" || len(r.Changes) != 0 {
		t.Errorf("un fallo no devuelve texto parcial: %+v", r)
	}
	if !strings.Contains(err.Error(), "edición 2") {
		t.Errorf("el error nombra la edición: %v", err)
	}
}

func TestHeaderIsProtected(t *testing.T) {
	src := "// @checkpoint parent=001-a.scad desc=\"hola\"\nx = 1;\ny = 2;\n"
	p := Options{ProtectHeader: true}
	for name, e := range map[string]Edit{
		"reemplazar la 1":     lines(1, 1, "// otra"),
		"insertar antes":      lines(1, 0, "// otra"),
		"old en la cabecera":  old("desc=\"hola\"", "desc=\"chau\""),
		"rango desde la 1":    lines(1, 2, "z"),
		"old que la cruza":    old("hola\"\nx", "x"),
		"old con parent=":     old("parent=001-a.scad", "parent="),
		"borrar la cabecera":  lines(1, 1, ""),
		"insertar sobre todo": lines(1, 0, "// @checkpoint parent= desc=\"nueva\""),
	} {
		mustFail(t, src, p, "cabecera", e)
		_ = name
	}
	// Desde la línea 2 sí; insertar antes de la 2 (justo debajo de la cabecera) también.
	if got := mustApply(t, src, p, lines(2, 1, "// nota")).Text; got != "// @checkpoint parent=001-a.scad desc=\"hola\"\n// nota\nx = 1;\ny = 2;\n" {
		t.Errorf("justo debajo: %q", got)
	}
	if got := mustApply(t, src, p, old("x = 1;", "x = 9;")).Text; !strings.Contains(got, "x = 9;") {
		t.Errorf("edición normal: %q", got)
	}
	// Sin protección (un archivo de lib/, o ProtectHeader apagado) la línea 1 se edita.
	if got := mustApply(t, src, Options{}, lines(1, 1, "// otra")).Text; !strings.HasPrefix(got, "// otra\n") {
		t.Errorf("sin protección: %q", got)
	}
	// Una primera línea que NO es cabecera no está protegida.
	if got := mustApply(t, "x = 1;\ny = 2;\n", p, lines(1, 1, "x = 5;")).Text; got != "x = 5;\ny = 2;\n" {
		t.Errorf("sin cabecera: %q", got)
	}
	// Con BOM la cabecera sigue siendo la línea 1.
	mustFail(t, "\uFEFF"+src, p, "cabecera", lines(1, 1, "z"))
}

func TestUnchanged(t *testing.T) {
	r := mustApply(t, "a\nb\n", Options{}, lines(2, 2, "b"))
	if !r.Unchanged || r.Text != "a\nb\n" {
		t.Errorf("reemplazar por lo mismo: %+v", r)
	}
}

func TestClosest(t *testing.T) {
	src := "color(\"red\") cube(1);\nwidth = 20;\n\nmodule caja(w = 3) {\n}\n"
	got := Closest(src, "WIDTH  =  25;", 3)
	if len(got) == 0 || got[0].Line != 2 || got[0].Text != "width = 20;" {
		t.Errorf("closest: %+v", got)
	}
	if got := Closest(src, "zzzzqqqq", 3); len(got) != 0 {
		t.Errorf("sin parecidos: %+v", got)
	}
	if got := Closest(src, "", 3); got != nil {
		t.Errorf("consulta vacía: %+v", got)
	}
	if got := Closest(src, "module caja", 1); len(got) != 1 || got[0].Line != 4 {
		t.Errorf("contiene: %+v", got)
	}
}

func TestApplyKeepsUntouchedBytesExactly(t *testing.T) {
	// Un archivo con de todo: BOM, CRLF y LF mezclados, tabs, unicode, sin salto final.
	src := "\uFEFF// @checkpoint parent= desc=\"ñ\"\r\n\tx = 1;\nñ = 2;\r\n\t\tz = 3;"
	r := mustApply(t, src, Options{ProtectHeader: true}, old("ñ = 2;", "ñ = 5;"))
	if r.Text != "\uFEFF// @checkpoint parent= desc=\"ñ\"\r\n\tx = 1;\nñ = 5;\r\n\t\tz = 3;" {
		t.Errorf("texto: %q", r.Text)
	}
}

func TestTooManyEdits(t *testing.T) {
	es := make([]Edit, maxEdits+1)
	for i := range es {
		es[i] = lines(1, 0, "x")
	}
	mustFail(t, "a\n", Options{}, "demasiadas ediciones", es...)
}
