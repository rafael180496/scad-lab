package scad

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"scadlab/backend/i18n"
	"scadlab/backend/render"
)

// Severidades y fuentes de un diagnóstico.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"

	SourceOpenSCAD = "openscad"
	SourceScadLab  = "scadlab"
)

// Diagnostic es un marcador para el editor. Line/Col/EndLine/EndCol son
// 1-based, con columnas en unidades UTF-16 (las de Monaco). EndLine y EndCol
// en 0 = toda la línea.
type Diagnostic struct {
	Severity string `json:"severity"` // "error" | "warning" | "info"
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	EndLine  int    `json:"endLine"`
	EndCol   int    `json:"endCol"`
	Code     string `json:"code"`    // "syntax" | "SL001"…
	Source   string `json:"source"`  // "openscad" | "scadlab"
	Message  string `json:"message"` // en el idioma activo
}

// LintOptions dice contra qué se revisa el código.
type LintOptions struct {
	WorkspaceDir  string   // para resolver include/use y el parent
	FileName      string   // nombre del checkpoint (SL001/SL002); "" = sin reglas de checkpoint
	OpenSCADPath  string   // "" = sin chequeo de sintaxis de OpenSCAD (solo reglas propias)
	ExistingFiles []string // .scad del workspace (SL002); nil = mirar el disco
}

// lintTimeout es el plazo de OpenSCAD. Exportar a .ast evalúa el nivel
// superior (~50 ms), pero un for de un millón de vueltas no debe colgar el
// editor.
var lintTimeout = 5 * time.Second

// Lint revisa src con OpenSCAD (si hay ruta) y con las reglas propias. Nunca
// falla: lo que no se pudo revisar simplemente no aporta diagnósticos.
func Lint(ctx context.Context, src string, o LintOptions) []Diagnostic {
	var scadDiags []scadDiag
	parseError := false
	ran := false
	if o.OpenSCADPath != "" && o.WorkspaceDir != "" {
		scadDiags, parseError, ran = runOpenSCAD(ctx, src, o)
	}
	if !ran {
		o.OpenSCADPath = "" // sin salida de OpenSCAD, SL004 resuelve por su cuenta
	}

	a := &analysis{src: src, opts: o}
	toks, lexErr := lex(src)
	if lexErr != nil {
		// Con el archivo cortado a la mitad las reglas estructurales darían
		// falsos positivos: solo las de encabezado, que leen la línea 1.
		ruleHeader(a)
		if !parseError {
			var le *LexError
			if errors.As(lexErr, &le) {
				a.add(SeverityError, CodeSyntax, le.Line, le.Col, 0, 0, le.msg, le.Line, le.Col)
			}
		}
	} else {
		a.toks = toks
		a.p = &parser{sig: significant(toks)}
		a.top = a.p.parseAll()
		for _, r := range ownRules {
			r(a)
		}
	}
	diags := mergeOpenSCAD(a, scadDiags)
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].Line != diags[j].Line {
			return diags[i].Line < diags[j].Line
		}
		return diags[i].Col < diags[j].Col
	})
	return diags
}

// parseAll lee el archivo entero; un «}» sobrante se saltea.
func (p *parser) parseAll() []stmt {
	var out []stmt
	i := 0
	for i < len(p.sig) {
		list, next := p.parseList(i)
		out = append(out, list...)
		i = next
		if p.at(i, TokPunct, "}") {
			i++
		}
	}
	return out
}

var (
	includeMissingRe = regexp.MustCompile(`(?i)can't (find|open) (include file|library) '([^']*)'`)
	overwrittenRe    = regexp.MustCompile(`was assigned on line \d+ but was overwritten`)
)

// mergeOpenSCAD junta los diagnósticos de OpenSCAD con los propios sin
// repetir lo mismo dos veces: un include que OpenSCAD no encuentra pasa a ser
// SL004 (error, con la ruta marcada) y la reasignación que OpenSCAD avisa se
// deja a SL008.
func mergeOpenSCAD(a *analysis, scadDiags []scadDiag) []Diagnostic {
	out := append([]Diagnostic{}, a.diags...)
	type codeLine struct {
		code string
		line int
	}
	ownAt := map[codeLine]bool{}
	for _, d := range a.diags {
		ownAt[codeLine{d.Code, d.Line}] = true
	}
	for _, sd := range scadDiags {
		d := sd.Diagnostic
		if sd.file != "" {
			d.Line = a.includeLine(sd.file)
		}
		if d.Line <= 0 {
			d.Line = 1
		}
		if m := includeMissingRe.FindStringSubmatch(d.Message); m != nil && sd.file == "" {
			out = append(out, a.includeMissingAt(d.Line, m[3]))
			continue
		}
		if overwrittenRe.MatchString(d.Message) && ownAt[codeLine{CodeDuplicateAssign, d.Line}] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// includeLine es la línea del include/use que trae file (por nombre de
// archivo), o 1 si no se encuentra.
func (a *analysis) includeLine(file string) int {
	base := filepath.Base(filepath.FromSlash(file))
	for _, t := range a.toks {
		if t.Kind == TokPath && filepath.Base(filepath.FromSlash(strings.Trim(t.Text, "<> "))) == base {
			return t.Line
		}
	}
	return 1
}

// includeMissingAt arma el SL004 de un include que OpenSCAD no encontró,
// marcando la ruta si está en esa línea.
func (a *analysis) includeMissingAt(line int, target string) Diagnostic {
	d := Diagnostic{Severity: SeverityError, Line: line, Col: 1, Code: CodeIncludeMissing, Source: SourceScadLab,
		Message: i18n.T(msgIncludeMissing, target)}
	for _, t := range a.toks {
		if t.Kind == TokPath && t.Line == line {
			d.Col, d.EndLine, d.EndCol = t.Col, t.Line, t.endCol()
			break
		}
	}
	return d
}

// scadDiag es un diagnóstico de OpenSCAD con el include al que apunta ("" =
// el propio archivo).
type scadDiag struct {
	Diagnostic
	file string
}

// runOpenSCAD escribe src a <ws>/.scadlab/lint/ (el watcher no es recursivo:
// no lo ve), corre `openscad -o x.ast x.scad` y traduce la salida. ran = false
// si OpenSCAD no llegó a correr (no se pudo escribir, plazo vencido…).
func runOpenSCAD(ctx context.Context, src string, o LintOptions) (diags []scadDiag, parseError, ran bool) {
	dir := filepath.Join(o.WorkspaceDir, ".scadlab", "lint")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, false
	}
	f, err := os.CreateTemp(dir, "lint-*.scad")
	if err != nil {
		return nil, false, false
	}
	tmp := f.Name()
	ast := strings.TrimSuffix(tmp, ".scad") + ".ast"
	defer os.Remove(tmp)
	defer os.Remove(ast)
	_, werr := f.WriteString(src)
	if cerr := f.Close(); werr != nil || cerr != nil {
		return nil, false, false
	}

	ctx, cancel := context.WithTimeout(ctx, lintTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, o.OpenSCADPath, "-o", ast, tmp)
	cmd.Dir = o.WorkspaceDir
	// El temporal vive en una subcarpeta: sin esto, un include relativo al
	// workspace («include <piezas.scad>») no se encontraría.
	path := o.WorkspaceDir + string(os.PathListSeparator) + filepath.Join(o.WorkspaceDir, "lib")
	if prev := os.Getenv("OPENSCADPATH"); prev != "" {
		path += string(os.PathListSeparator) + prev
	}
	cmd.Env = append(os.Environ(), "OPENSCADPATH="+path)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	hideWindow(cmd)
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, false, false
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return nil, false, false // no arrancó
	}

	rel, err := filepath.Rel(o.WorkspaceDir, tmp)
	if err != nil {
		rel = tmp
	}
	base := filepath.Base(tmp)
	shown := o.FileName
	if shown == "" {
		shown = "<editor>"
	}
	for _, rd := range render.ParseOutput(out.String(), o.WorkspaceDir, rel) {
		d := Diagnostic{Severity: rd.Severity, Line: rd.Line, Col: rd.Col, Code: CodeSyntax, Source: SourceOpenSCAD, Message: rd.Message}
		if d.Severity != SeverityError {
			d.Severity = SeverityWarning
		}
		if strings.Contains(rd.Message, "Parser error") {
			parseError = true
		}
		// Un mensaje sobre el temporal es sobre el archivo editado: es el
		// mismo texto, así que las líneas coinciden. Uno sobre un include se
		// marca en la línea del include (mergeOpenSCAD), con el archivo y la
		// línea en el texto.
		file := ""
		if rd.File != "" && filepath.Base(filepath.FromSlash(rd.File)) != base {
			file = rd.File
			loc := rd.File
			if rd.Line > 0 {
				loc += ":" + strconv.Itoa(rd.Line)
			}
			d.Message = loc + ": " + rd.Message
			d.Line, d.Col = 0, 0
		}
		if d.Col <= 0 {
			d.Col = 1
		}
		d.Message = strings.ReplaceAll(d.Message, base, shown)
		diags = append(diags, scadDiag{Diagnostic: d, file: file})
	}
	return diags, parseError, true
}
