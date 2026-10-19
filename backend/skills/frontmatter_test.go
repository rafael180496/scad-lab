package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codes(ds []Diagnostic) map[string]string {
	m := map[string]string{}
	for _, d := range ds {
		m[d.Code] = d.Severity
	}
	return m
}

func TestParseFrontmatterForms(t *testing.T) {
	cases := []struct {
		name, in, wantName, wantDesc string
	}{
		{"plano", "---\nname: a-b\ndescription: Hace cosas\n---\ncuerpo", "a-b", "Hace cosas"},
		{"comillas", "---\nname: \"a-b\"\ndescription: 'Dice: algo'\n---\n", "a-b", "Dice: algo"},
		{"comentarios", "---\n# name: otro\nname: a-b # nota\ndescription: x\n---\n", "a-b", "x"},
		{"bloque plegado", "---\nname: a-b\ndescription: >\n  uno\n  dos\n---\n", "a-b", "uno dos"},
		{"bloque literal", "---\nname: a-b\ndescription: |\n  uno\n  dos\n---\n", "a-b", "uno\ndos"},
		{"CRLF", "---\r\nname: a-b\r\ndescription: x\r\n---\r\n", "a-b", "x"},
		{"BOM", "\ufeff---\nname: a-b\ndescription: x\n---\n", "a-b", "x"},
		{"anidado no cuenta", "---\nmetadata:\n  name: falso\nname: a-b\ndescription: x\n---\n", "a-b", "x"},
		{"name del cuerpo no cuenta", "---\ndescription: x\n---\nname: del-cuerpo\n", "", "x"},
		{"sin frontmatter", "name: a-b\n", "", ""},
	}
	for _, c := range cases {
		n, d := frontmatter([]byte(c.in))
		if n != c.wantName || d != c.wantDesc {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, n, d, c.wantName, c.wantDesc)
		}
	}
}

func TestValidateSkillMD(t *testing.T) {
	ok := "---\nname: gear-helper\ndescription: Úsala cuando el usuario pida engranajes\n---\n# Cuerpo\n"
	if ds := ValidateSkillMD("gear-helper", []byte(ok)); len(ds) != 0 {
		t.Fatalf("una skill correcta no debe dar diagnósticos: %+v", ds)
	}
	cases := []struct {
		name, folder, in, code, sev string
		line                        int
	}{
		{"sin frontmatter", "x-y", "# Hola\n", "SK001", "error", 1},
		{"sin cierre", "x-y", "---\nname: x-y\ndescription: d\n", "SK002", "error", 1},
		{"falta name", "x-y", "---\ndescription: d\n---\n", "SK003", "error", 1},
		{"name vacío", "x-y", "---\nname:\ndescription: d\n---\n", "SK003", "error", 2},
		{"name no kebab", "x-y", "---\nname: Gear Helper\ndescription: d\n---\n", "SK004", "error", 2},
		{"name distinto de la carpeta", "x-y", "---\ndescription: d\nname: otro-nombre\n---\n", "SK005", "error", 3},
		{"falta description", "x-y", "---\nname: x-y\n---\n", "SK006", "error", 1},
		{"description vacía", "x-y", "---\nname: x-y\ndescription:\n---\n", "SK006", "error", 3},
		{"descripción de ejemplo", "x-y", "---\nname: x-y\ndescription: " + DescriptionPlaceholder + "\n---\n", "SK008", "warning", 3},
		{"descripción de la plantilla", "x-y", "---\nname: x-y\ndescription: Plantilla para crear skills nuevas de ScadLab\n---\n", "SK008", "warning", 3},
		{"description larga", "x-y", "---\nname: x-y\ndescription: " + strings.Repeat("a", MaxDescriptionLen+1) + "\n---\n", "SK007", "warning", 3},
		{"YAML dudoso", "x-y", "---\nname: x-y\ndescription: Úsala así: cuando pida\n---\n", "SK009", "warning", 3},
	}
	for _, c := range cases {
		ds := ValidateSkillMD(c.folder, []byte(c.in))
		var got *Diagnostic
		for i := range ds {
			if ds[i].Code == c.code {
				got = &ds[i]
			}
		}
		if got == nil {
			t.Errorf("%s: falta %s en %+v", c.name, c.code, ds)
			continue
		}
		if got.Severity != c.sev || got.Line != c.line || got.Message == "" {
			t.Errorf("%s: %+v, quería severidad %s en la línea %d", c.name, *got, c.sev, c.line)
		}
	}
	// Con comillas el «: » ya no avisa.
	if _, has := codes(ValidateSkillMD("x-y", []byte("---\nname: x-y\ndescription: \"Úsala así: cuando pida\"\n---\n")))["SK009"]; has {
		t.Error("una descripción entre comillas no debería avisar de YAML")
	}
	// Los errores impiden guardar; los avisos no.
	if !HasErrors(ValidateSkillMD("x-y", []byte("# sin frontmatter"))) {
		t.Error("HasErrors debería ser true")
	}
	if HasErrors(ValidateSkillMD("x-y", []byte("---\nname: x-y\ndescription: "+DescriptionPlaceholder+"\n---\n"))) {
		t.Error("un aviso no es un error")
	}
}

func TestSetFrontmatterFieldOnlyTouchesFrontmatter(t *testing.T) {
	in := "---\nname: viejo\ndescription: d\n---\nname: del cuerpo\n"
	got := string(setFrontmatterName([]byte(in), "nuevo"))
	if got != "---\nname: nuevo\ndescription: d\n---\nname: del cuerpo\n" {
		t.Errorf("reemplazo: %q", got)
	}
	// Sin la clave se agrega antes del cierre.
	got = string(setFrontmatterName([]byte("---\ndescription: d\n---\ncuerpo\n"), "nuevo"))
	if got != "---\ndescription: d\nname: nuevo\n---\ncuerpo\n" {
		t.Errorf("alta: %q", got)
	}
	// CRLF y BOM se conservan.
	got = string(setFrontmatterName([]byte("\ufeff---\r\nname: viejo\r\n---\r\n"), "nuevo"))
	if got != "\ufeff---\r\nname: nuevo\r\n---\r\n" {
		t.Errorf("CRLF/BOM: %q", got)
	}
	// Un escalar de bloque se reemplaza entero.
	got = string(setFrontmatterField([]byte("---\nname: a-b\ndescription: >\n  uno\n  dos\nx: y\n---\n"), "description", "nueva"))
	if got != "---\nname: a-b\ndescription: nueva\nx: y\n---\n" {
		t.Errorf("bloque: %q", got)
	}
	// Sin frontmatter no se inventa uno.
	if string(setFrontmatterName([]byte("name: x\n"), "n")) != "name: x\n" {
		t.Error("sin frontmatter debe quedar igual")
	}
}

// Una skill recién creada: name puesto, descripción pendiente (no la de la
// plantilla) y sin errores de frontmatter.
func TestCreateFromTemplateLeavesAValidPendingSkill(t *testing.T) {
	dir, err := CreateFromTemplate(t.TempDir(), "gear-helper")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, MainFile))
	if err != nil {
		t.Fatal(err)
	}
	n, d := frontmatter(raw)
	if n != "gear-helper" || d != DescriptionPlaceholder {
		t.Fatalf("name %q, description %q", n, d)
	}
	if strings.Contains(string(raw), "no es una skill operativa") {
		t.Error("quedó el texto de la plantilla")
	}
	ds := ValidateSkillMD("gear-helper", raw)
	if HasErrors(ds) || codes(ds)["SK008"] != "warning" {
		t.Errorf("diagnósticos: %+v", ds)
	}
}

func TestCheckNameRejectsReservedAndBad(t *testing.T) {
	for _, n := range []string{"", "a", "Mayus", "con espacio", "nul", "con", "_skill-template", "../x", "a/b"} {
		if CheckName(n) == nil {
			t.Errorf("%q debería rechazarse", n)
		}
	}
	for _, n := range []string{"ab", "gear-helper", "m3-tools"} {
		if err := CheckName(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
}
