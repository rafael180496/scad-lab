package workspace

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"scadlab/assets"
)

// WriteStarter deja un primer checkpoint válido en un workspace que no tiene
// ninguno: 001-<slug del nombre>.scad, con cabecera, parámetros de Customizer,
// color() y una pieza simple que renderiza. Así un proyecto nuevo (o una
// carpeta importada sin modelos) abre con algo que ver y de lo que partir, en
// vez de un visor vacío.
//
// Solo escribe si la raíz de dir no tiene ningún .scad: si ya hay alguno
// devuelve "" sin error y no toca nada. Nunca pisa un archivo: se crea con
// O_EXCL, así que una carrera con otro proceso que cree el 001 da error y no
// una sobreescritura (los checkpoints son inmutables, regla 3 de
// .claude/rules/technical.md). lang es "es"; cualquier otro valor, inglés.
func WriteStarter(dir, workspaceName, lang string) (fileName string, err error) {
	files, err := Scan(dir)
	if err != nil {
		return "", err
	}
	if len(files) > 0 {
		return "", nil
	}
	tplRaw, err := fs.ReadFile(assets.FS, "workspace/starter.scad."+starterLang(lang)+".tmpl")
	if err != nil {
		return "", err
	}
	tpl, err := template.New("starter").Option("missingkey=error").Parse(string(tplRaw))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, map[string]string{"Desc": starterDesc(workspaceName)}); err != nil {
		return "", err
	}
	name, err := NextFileName(dir, workspaceName)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return name, nil
}

func starterLang(lang string) string {
	if lang == "es" {
		return "es"
	}
	return "en"
}

// starterDesc es el nombre del workspace listo para ir entre comillas dobles en
// la cabecera `desc="…"`: una sola línea, con \ y " escapados (es lo que lee
// workspace.Scan) y sin pasar de una longitud razonable.
func starterDesc(name string) string {
	name = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, name)), " ")
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80])
	}
	if name == "" {
		name = "ScadLab"
	}
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name)
}
