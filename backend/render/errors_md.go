package render

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"scadlab/backend/i18n"
)

// RENDER_ERRORS.md es el canal por el que un agente que trabaja en el
// workspace (Claude Code, opencode…) se entera de que su último checkpoint no
// renderiza. Las reglas del workspace (assets/workspace/RULES.md.tmpl, regla 6)
// le dicen que lo lea. Por eso no es un volcado del log: lleva la línea, el
// código alrededor y la instrucción de cómo corregir sin romper la
// inmutabilidad de los checkpoints.

const renderErrorsFile = "RENDER_ERRORS.md"

// contextLines: líneas de código antes y después de cada error.
const contextLines = 3

// logTailLines: cuánto del final del log va al archivo cuando ningún error
// trae ubicación (el agente necesita algo concreto que leer).
const logTailLines = 30

func writeRenderErrors(dir string, r Result) error {
	var b strings.Builder
	t := func(m i18n.Msg, args ...any) string { return i18n.T(m, args...) }

	fmt.Fprintf(&b, "# %s — %s\n\n", t(i18n.Msg{ES: "Errores de render", EN: "Render errors"}), r.FileName)
	fmt.Fprintf(&b, "- %s: `%s`\n", t(i18n.Msg{ES: "Archivo", EN: "File"}), r.FileName)
	fmt.Fprintf(&b, "- %s: %s\n", t(i18n.Msg{ES: "Fecha", EN: "Date"}), time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- %s: %s\n", t(i18n.Msg{ES: "Estado", EN: "Status"}), r.Status)
	if r.Version != "" || r.Backend != "" {
		fmt.Fprintf(&b, "- OpenSCAD: %s (%s)\n", orDash(r.Version), orDash(r.Backend))
	}
	b.WriteString("\n")

	var errs, warns []Diagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			errs = append(errs, d)
		} else {
			warns = append(warns, d)
		}
	}

	sources := map[string][]string{} // archivo → líneas, leído una vez
	lines := func(file string) []string {
		if file == "" {
			file = r.FileName
		}
		if l, ok := sources[file]; ok {
			return l
		}
		var l []string
		if !filepath.IsAbs(file) {
			if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file))); err == nil {
				l = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
			}
		}
		sources[file] = l
		return l
	}

	fmt.Fprintf(&b, "## %s (%d)\n\n", t(i18n.Msg{ES: "Errores", EN: "Errors"}), len(errs))
	if len(errs) == 0 {
		b.WriteString(t(i18n.Msg{ES: "OpenSCAD no informó errores con ubicación.", EN: "OpenSCAD reported no errors with a location."}) + "\n\n")
	}
	located := false
	for i, d := range errs {
		fmt.Fprintf(&b, "### %d. %s\n\n", i+1, location(d, r.FileName))
		fmt.Fprintf(&b, "%s\n\n", d.Message)
		if d.Line > 0 {
			if snippet := codeSnippet(lines(d.File), d.Line); snippet != "" {
				located = true
				b.WriteString("```openscad\n" + snippet + "```\n\n")
			}
		}
	}

	if len(warns) > 0 {
		fmt.Fprintf(&b, "## %s (%d)\n\n", t(i18n.Msg{ES: "Warnings", EN: "Warnings"}), len(warns))
		for _, d := range warns {
			fmt.Fprintf(&b, "- %s: %s\n", location(d, r.FileName), d.Message)
		}
		b.WriteString("\n")
	}

	if !located && strings.TrimSpace(r.Log) != "" {
		fmt.Fprintf(&b, "## %s\n\n```text\n%s\n```\n\n", t(i18n.Msg{ES: "Final de la salida de OpenSCAD", EN: "End of the OpenSCAD output"}), tailLines(r.Log, logTailLines))
	}

	fmt.Fprintf(&b, "## %s\n\n", t(i18n.Msg{ES: "Cómo corregir", EN: "How to fix"}))
	b.WriteString(t(i18n.Msg{
		ES: "Corregir en un checkpoint NUEVO cuyo parent sea `%s`: crea el siguiente `NNN-slug.scad` con la primera línea `// @checkpoint parent=%s desc=\"<qué corregiste>\"`. No edites `%s` ni ningún otro checkpoint: son inmutables. Si el error está en un include de `lib/`, corrígelo ahí y crea igual el checkpoint nuevo. ScadLab borra este archivo cuando un render sale bien.",
		EN: "Fix it in a NEW checkpoint whose parent is `%s`: create the next `NNN-slug.scad` with the first line `// @checkpoint parent=%s desc=\"<what you fixed>\"`. Do not edit `%s` or any other checkpoint: they are immutable. If the error is in an include under `lib/`, fix it there and still create the new checkpoint. ScadLab deletes this file when a render succeeds.",
	}, r.FileName, r.FileName, r.FileName))
	b.WriteString("\n")

	// Escritura atómica: el agente puede estar leyendo el archivo justo ahora.
	path := filepath.Join(dir, renderErrorsFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo escribir %s: %w", EN: "could not write %s: %w"}, renderErrorsFile, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return i18n.Errorf(i18n.Msg{ES: "no se pudo escribir %s: %w", EN: "could not write %s: %w"}, renderErrorsFile, err)
	}
	return nil
}

func clearRenderErrors(dir string) error {
	err := os.Remove(filepath.Join(dir, renderErrorsFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo borrar %s: %w", EN: "could not delete %s: %w"}, renderErrorsFile, err)
	}
	return nil
}

// location rotula un diagnóstico: "x.scad, línea 12" o "sin ubicación".
func location(d Diagnostic, mainFile string) string {
	file := d.File
	if file == "" {
		file = mainFile
	}
	if d.Line <= 0 {
		return i18n.T(i18n.Msg{ES: "`%s`, sin línea", EN: "`%s`, no line"}, file)
	}
	return i18n.T(i18n.Msg{ES: "`%s`, línea %d", EN: "`%s`, line %d"}, file, d.Line)
}

// codeSnippet devuelve las líneas line±contextLines numeradas, con «>» en la
// que falló. "" si la línea no existe en el archivo (el include no se pudo
// leer o la numeración no coincide).
func codeSnippet(src []string, line int) string {
	if line < 1 || line > len(src) {
		return ""
	}
	from := max(1, line-contextLines)
	to := min(len(src), line+contextLines)
	width := len(fmt.Sprint(to))
	var b strings.Builder
	for n := from; n <= to; n++ {
		mark := " "
		if n == line {
			mark = ">"
		}
		fmt.Fprintf(&b, "%s %*d | %s\n", mark, width, n, src[n-1])
	}
	return b.String()
}

func tailLines(s string, n int) string {
	l := strings.Split(strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
