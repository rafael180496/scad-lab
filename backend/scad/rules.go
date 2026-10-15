package scad

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// Reglas propias (Source "scadlab"). Cada una es una función que recibe el
// análisis ya hecho y agrega diagnósticos; agregar una regla es escribir la
// función, sumarla a ownRules y documentarla en claude/10-editor.md.

// analysis es lo que comparten las reglas: los tokens, el esqueleto y las
// opciones del lint.
type analysis struct {
	src   string
	toks  []tok // todos, con comentarios y saltos
	p     *parser
	top   []stmt
	opts  LintOptions
	diags []Diagnostic
	// coloredModules: módulos definidos en el archivo que aplican color()
	// adentro (directo o llamando a otro que lo hace).
	coloredModules map[string]bool
}

type rule func(*analysis)

var ownRules = []rule{ruleHeader, ruleColor, ruleIncludes, ruleFnTooHigh, ruleNestedMinkowski, ruleDuplicateAssign, ruleCustomizerRange}

// Códigos de las reglas.
const (
	CodeSyntax          = "syntax"
	CodeHeader          = "SL001"
	CodeParentMissing   = "SL002"
	CodeNoColor         = "SL003"
	CodeIncludeMissing  = "SL004"
	CodeIncludesCkpt    = "SL005"
	CodeFnTooHigh       = "SL006"
	CodeNestedMinkowski = "SL007"
	CodeDuplicateAssign = "SL008"
	CodeCustomizerRange = "SL009"
)

func (a *analysis) add(sev, code string, line, col, endLine, endCol int, m i18n.Msg, args ...any) {
	a.diags = append(a.diags, Diagnostic{
		Severity: sev, Line: line, Col: col, EndLine: endLine, EndCol: endCol,
		Code: code, Source: SourceScadLab, Message: i18n.T(m, args...),
	})
}

// addTok marca el token entero.
func (a *analysis) addTok(sev, code string, t tok, m i18n.Msg, args ...any) {
	if strings.Contains(t.Text, "\n") {
		a.add(sev, code, t.Line, t.Col, 0, 0, m, args...)
		return
	}
	a.add(sev, code, t.Line, t.Col, t.Line, t.endCol(), m, args...)
}

// ---------------------------------------------------------------- SL001/SL002

var (
	headerStartRe = regexp.MustCompile(`^\s*//\s*@checkpoint\b`)
	headerFullRe  = regexp.MustCompile(`^\s*//\s*@checkpoint\s+parent=(\S*)\s+desc="(?:[^"\\]|\\.)*"\s*$`)
	checkpointRe  = regexp.MustCompile(`^\d{3,}-.*\.scad$`)
)

func firstSourceLine(src string) string {
	src = strings.TrimPrefix(src, "\uFEFF")
	if i := strings.IndexByte(src, '\n'); i >= 0 {
		src = src[:i]
	}
	return strings.TrimRight(src, "\r")
}

func ruleHeader(a *analysis) {
	if a.opts.FileName == "" {
		return
	}
	line := firstSourceLine(a.src)
	if !headerStartRe.MatchString(line) {
		a.add(SeverityWarning, CodeHeader, 1, 1, 0, 0, i18n.Msg{
			ES: `falta el encabezado del checkpoint en la línea 1: // @checkpoint parent=<archivo> desc="<qué cambió>"`,
			EN: `missing checkpoint header on line 1: // @checkpoint parent=<file> desc="<what changed>"`,
		})
		return
	}
	m := headerFullRe.FindStringSubmatch(line)
	if m == nil {
		a.add(SeverityWarning, CodeHeader, 1, 1, 0, 0, i18n.Msg{
			ES: `encabezado del checkpoint mal formado: tiene que ser // @checkpoint parent=<archivo> desc="<qué cambió>"`,
			EN: `malformed checkpoint header: it must be // @checkpoint parent=<file> desc="<what changed>"`,
		})
		return
	}
	parent := m[1]
	if parent == "" {
		return
	}
	switch {
	case parent == a.opts.FileName:
		a.add(SeverityWarning, CodeParentMissing, 1, 1, 0, 0, i18n.Msg{
			ES: "parent=%s apunta al propio checkpoint", EN: "parent=%s points to this same checkpoint",
		}, parent)
	case !a.fileExists(parent):
		a.add(SeverityWarning, CodeParentMissing, 1, 1, 0, 0, i18n.Msg{
			ES: "parent=%s no existe en el workspace", EN: "parent=%s does not exist in the workspace",
		}, parent)
	}
}

func (a *analysis) fileExists(name string) bool {
	if a.opts.ExistingFiles != nil {
		for _, f := range a.opts.ExistingFiles {
			if f == name {
				return true
			}
		}
		return false
	}
	if a.opts.WorkspaceDir == "" || name != filepath.Base(name) {
		return false
	}
	st, err := os.Stat(filepath.Join(a.opts.WorkspaceDir, name))
	return err == nil && !st.IsDir()
}

// ---------------------------------------------------------------- SL003

// Cabezas que no generan un objeto propio: lo que importa es su hijo. for y
// union juntan varios objetos, pero cada uno conserva su color.
var transparentHeads = map[string]bool{
	"if": true, "for": true, "let": true, "assert": true, "echo": true,
	"translate": true, "rotate": true, "scale": true, "mirror": true, "multmatrix": true,
	"resize": true, "union": true, "render": true, "group": true,
}

// Llamadas que nunca son geometría.
var nonObjectHeads = map[string]bool{"echo": true, "assert": true, "children": true, "let": true}

func ruleColor(a *analysis) {
	a.computeColoredModules()
	for i := range a.top {
		a.checkColored(&a.top[i])
	}
}

// computeColoredModules marca los módulos del archivo que aplican color()
// adentro, por punto fijo: un módulo que llama a uno coloreado también lo está.
func (a *analysis) computeColoredModules() {
	a.coloredModules = map[string]bool{}
	var mods []*stmt
	for i := range a.top {
		walk(&a.top[i], func(s *stmt) {
			if s.kind == stmtModule && s.name >= 0 {
				mods = append(mods, s)
			}
		})
	}
	for changed := true; changed; {
		changed = false
		for _, m := range mods {
			name := a.p.sig[m.name].Text
			if !a.coloredModules[name] && a.subtreeColored(m.child) {
				a.coloredModules[name] = true
				changed = true
			}
		}
	}
}

// subtreeColored dice si algo debajo de s aplica color(): directo o con un
// módulo coloreado.
func (a *analysis) subtreeColored(s *stmt) bool {
	if s == nil {
		return false
	}
	found := false
	walk(s, func(n *stmt) {
		if h := a.p.headName(n); h == "color" || a.coloredModules[h] {
			found = true
		}
	})
	return found
}

var msgNoColor = i18n.Msg{
	ES: "objeto de primer nivel sin color(): envolvelo en color(\"…\") para que el 3MF multicolor lo asigne a un filamento",
	EN: "top-level object without color(): wrap it in color(\"…\") so the multicolor 3MF can map it to a filament",
}

// checkColored revisa una sentencia del nivel superior (o de un grupo
// transparente debajo de él).
func (a *analysis) checkColored(s *stmt) {
	switch s.kind {
	case stmtBlock:
		for i := range s.body {
			a.checkColored(&s.body[i])
		}
		return
	case stmtInst:
	default:
		return
	}
	// * desactiva y % deja el objeto de fondo: ninguno sale en el render.
	if strings.ContainsAny(s.modifier, "*%") || s.name < 0 {
		return
	}
	head := a.p.sig[s.name].Text
	switch {
	case head == "color" || a.coloredModules[head]:
		return
	case head == "if":
		for _, c := range []*stmt{s.child, s.elseChild} {
			if c != nil {
				a.checkColored(c)
			}
		}
		return
	case transparentHeads[head] || nonObjectHeads[head]:
		if s.child != nil {
			a.checkColored(s.child)
		}
		return
	}
	// Una operación (difference, hull, linear_extrude…) o un módulo: es un
	// objeto. Basta con que algo adentro tenga color: difference() toma el
	// del primer hijo, y avisar por cada hijo restado sería ruido.
	if a.subtreeColored(s.child) {
		return
	}
	a.addTok(SeverityWarning, CodeNoColor, a.p.sig[s.name], msgNoColor)
}

// ---------------------------------------------------------------- SL004/SL005

func ruleIncludes(a *analysis) {
	for i := range a.top {
		walk(&a.top[i], func(s *stmt) {
			if s.kind != stmtInclude || s.name < 0 {
				return
			}
			t := a.p.sig[s.name]
			target := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t.Text, "<"), ">"))
			if checkpointRe.MatchString(filepath.Base(filepath.FromSlash(target))) {
				a.addTok(SeverityWarning, CodeIncludesCkpt, t, i18n.Msg{
					ES: "un checkpoint no debe incluir a otro (%s): cada checkpoint es autocontenido; copiá lo que necesites",
					EN: "a checkpoint must not include another one (%s): each checkpoint is self-contained; copy what you need",
				}, target)
			}
			// Con OpenSCAD disponible, el que decide si se encuentra es
			// OpenSCAD (conoce sus propias librerías, como MCAD): ver
			// mergeOpenSCAD.
			if a.opts.OpenSCADPath != "" || target == "" {
				return
			}
			if !a.resolveInclude(target) {
				a.addTok(SeverityError, CodeIncludeMissing, t, msgIncludeMissing, target)
			}
		})
	}
}

var msgIncludeMissing = i18n.Msg{
	ES: "no se encuentra %s (se busca junto al archivo, en el workspace, en lib/ y en las librerías de OpenSCAD)",
	EN: "%s not found (searched next to the file, in the workspace, in lib/ and in the OpenSCAD libraries)",
}

// includeDirs son los lugares donde OpenSCAD busca un include, en su orden:
// junto al archivo (el workspace: los checkpoints viven en la raíz), lib/,
// OPENSCADPATH, la carpeta de librerías del usuario y la de la instalación.
func (a *analysis) includeDirs() []string {
	var dirs []string
	if a.opts.WorkspaceDir != "" {
		dirs = append(dirs, a.opts.WorkspaceDir, filepath.Join(a.opts.WorkspaceDir, "lib"))
	}
	for _, d := range filepath.SplitList(os.Getenv("OPENSCADPATH")) {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		switch runtime.GOOS {
		case "darwin", "windows":
			dirs = append(dirs, filepath.Join(home, "Documents", "OpenSCAD", "libraries"))
		default:
			dirs = append(dirs, filepath.Join(home, ".local", "share", "OpenSCAD", "libraries"))
		}
	}
	return dirs
}

func (a *analysis) resolveInclude(target string) bool {
	rel := filepath.FromSlash(target)
	if filepath.IsAbs(rel) {
		_, err := os.Stat(rel)
		return err == nil
	}
	for _, d := range a.includeDirs() {
		if st, err := os.Stat(filepath.Join(d, rel)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- SL006

func ruleFnTooHigh(a *analysis) {
	for _, s := range a.top {
		if s.kind != stmtAssign || a.p.sig[s.name].Text != "$fn" {
			continue
		}
		for i := s.value[0]; i < s.value[1]; i++ {
			t := a.p.sig[i]
			if t.Kind != TokNumber {
				continue
			}
			if v, err := strconv.ParseFloat(t.Text, 64); err == nil && v > 256 {
				a.addTok(SeverityInfo, CodeFnTooHigh, a.p.sig[s.name], i18n.Msg{
					ES: "$fn = %s en el nivel superior: cada círculo y esfera del modelo tendrá esa cantidad de lados y el render será lento; preferí $fa/$fs o un $fn alto solo en la pieza que lo necesite",
					EN: "$fn = %s at the top level: every circle and sphere in the model gets that many sides and rendering will be slow; prefer $fa/$fs or a high $fn only on the part that needs it",
				}, t.Text)
				break
			}
		}
	}
}

// ---------------------------------------------------------------- SL007

func ruleNestedMinkowski(a *analysis) {
	var visit func(s *stmt, inside string)
	visit = func(s *stmt, inside string) {
		if s == nil {
			return
		}
		head := a.p.headName(s)
		if head == "minkowski" && inside != "" {
			a.addTok(SeverityInfo, CodeNestedMinkowski, a.p.sig[s.name], i18n.Msg{
				ES: "minkowski() anidado dentro de %s(): el costo se multiplica y el render puede tardar minutos u horas",
				EN: "minkowski() nested inside %s(): the cost multiplies and rendering can take minutes or hours",
			}, inside)
		}
		next := inside
		if head == "minkowski" || head == "hull" {
			next = head
		}
		if s.kind == stmtModule { // el cuerpo de un módulo no está «dentro» de nada
			next = ""
		}
		visit(s.child, next)
		visit(s.elseChild, next)
		for i := range s.body {
			visit(&s.body[i], next)
		}
	}
	for i := range a.top {
		visit(&a.top[i], "")
	}
}

// ---------------------------------------------------------------- SL008

func ruleDuplicateAssign(a *analysis) {
	a.checkScope(a.top)
	for i := range a.top {
		walk(&a.top[i], func(s *stmt) {
			if s.kind == stmtBlock {
				a.checkScope(s.body)
			}
		})
	}
}

// checkScope avisa las asignaciones repetidas entre las sentencias de un
// mismo ámbito (no mira adentro de bloques: cada bloque es su ámbito).
func (a *analysis) checkScope(list []stmt) {
	seen := map[string]int{}
	for _, s := range list {
		if s.kind != stmtAssign {
			continue
		}
		t := a.p.sig[s.name]
		if first, ok := seen[t.Text]; ok {
			a.addTok(SeverityWarning, CodeDuplicateAssign, t, i18n.Msg{
				ES: "«%s» ya se asignó en la línea %d: OpenSCAD usa solo la última asignación del ámbito (y en la posición de la primera)",
				EN: "'%s' was already assigned on line %d: OpenSCAD only uses the last assignment in the scope (at the position of the first one)",
			}, t.Text, first)
			continue
		}
		seen[t.Text] = t.Line
	}
}

// ---------------------------------------------------------------- SL009

var (
	sectionRe = regexp.MustCompile(`^/\*\s*\[([^\]]*)\]\s*\*/$`)
	rangeRe   = regexp.MustCompile(`^//\s*\[.*\]`)
)

func ruleCustomizerRange(a *analysis) {
	// Secciones y comentarios finales de línea, por posición en bytes.
	type section struct {
		off    int
		hidden bool
	}
	var sections []section
	trailing := map[int]string{} // línea → comentario de línea que la termina
	for _, t := range a.toks {
		if t.Kind != TokComment {
			continue
		}
		if m := sectionRe.FindStringSubmatch(t.Text); m != nil {
			sections = append(sections, section{off: t.off, hidden: strings.EqualFold(strings.TrimSpace(m[1]), "hidden")})
		}
		if strings.HasPrefix(t.Text, "//") {
			trailing[t.Line] = t.Text
		}
	}
	if len(sections) == 0 {
		return
	}
	for _, s := range a.top {
		// El Customizer solo toma lo que está antes del primer module/function.
		if s.kind == stmtModule || s.kind == stmtFunction {
			return
		}
		if s.kind != stmtAssign {
			continue
		}
		name := a.p.sig[s.name]
		if strings.HasPrefix(name.Text, "$") || !a.isNumberLiteral(s.value) {
			continue
		}
		hidden := false
		for _, sec := range sections {
			if sec.off < name.off {
				hidden = sec.hidden
			}
		}
		if hidden {
			continue
		}
		last := a.p.sig[s.end-1]
		if c, ok := trailing[last.Line]; ok && rangeRe.MatchString(c) {
			continue
		}
		a.addTok(SeverityInfo, CodeCustomizerRange, name, i18n.Msg{
			ES: "parámetro «%s» del Customizer sin rango: agregá // [min:paso:max] al final de la línea",
			EN: "Customizer parameter '%s' has no range: add // [min:step:max] at the end of the line",
		}, name.Text)
	}
}

// isNumberLiteral: la expresión es un número, con o sin signo.
func (a *analysis) isNumberLiteral(r [2]int) bool {
	i := r[0]
	if i < r[1] && a.p.sig[i].Kind == TokOp && (a.p.sig[i].Text == "-" || a.p.sig[i].Text == "+") {
		i++
	}
	return i == r[1]-1 && a.p.sig[i].Kind == TokNumber
}
