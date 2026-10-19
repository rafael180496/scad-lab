package scad

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// Parámetros del Customizer: lee los de un .scad (Parameters) y cambia sus
// valores sin tocar nada más del archivo (SetParameters). Lo usan las
// herramientas MCP get_parameters, set_parameters y create_from_template: el
// usuario que no sabe OpenSCAD pide «hazla 10 mm más ancha» y la IA cambia un
// número, no reescribe el código.
//
// Qué es un parámetro: una asignación del nivel superior, antes del primer
// module o function (igual que la regla SL009 y que el Customizer de OpenSCAD),
// cuyo valor es un literal — número, texto, true/false o un vector de números—.
// `$fn` y compañía (variables especiales) no cuentan. Las anotaciones son las del
// Customizer:
//
//	/* [Dimensiones] */            sección (Hidden las esconde)
//	// Ancho interior en mm        descripción: el comentario de la línea de arriba
//	ancho = 60;  // [20:1:180]     rango min:paso:max
//	tapa = "snap"; // [snap, slide] lista de opciones (valor o valor:etiqueta)

// Tipos de un parámetro.
const (
	ParamNumber = "number"
	ParamString = "string"
	ParamBool   = "bool"
	ParamVector = "vector"
)

// ParamOption es una opción de lista desplegable.
type ParamOption struct {
	Value any    `json:"value"`
	Label string `json:"label,omitempty"`
}

// Param es un parámetro del Customizer.
type Param struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Value       any           `json:"value"`   // float64 | string | bool | []float64
	Literal     string        `json:"literal"` // tal como está escrito en el código
	Group       string        `json:"group,omitempty"`
	Description string        `json:"description,omitempty"`
	Min         *float64      `json:"min,omitempty"`
	Max         *float64      `json:"max,omitempty"`
	Step        *float64      `json:"step,omitempty"`
	Options     []ParamOption `json:"options,omitempty"`
	Hidden      bool          `json:"hidden,omitempty"`
	Line        int           `json:"line"`

	valStart, valEnd int // bytes del valor en el código
}

// ParamChange es un valor que SetParameters cambió.
type ParamChange struct {
	Name string `json:"name"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

var (
	rangeCommentRe = regexp.MustCompile(`^//\s*\[(.*)\]`)
	numRe          = regexp.MustCompile(`^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$`)
)

// Parameters lee los parámetros del Customizer de src. El código con un string o
// un comentario sin cerrar no se puede leer: error. Sin parámetros, lista vacía.
func Parameters(src string) ([]Param, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{sig: significant(toks)}
	top := p.parseAll()

	// Líneas con código (para distinguir «comentario solo» de «comentario al
	// final de una sentencia»), secciones y comentarios de línea por línea.
	codeLine := map[int]bool{}
	for _, t := range p.sig {
		codeLine[t.Line] = true
	}
	type section struct {
		off  int
		name string
	}
	var sections []section
	lineComment := map[int]string{}
	for _, t := range toks {
		if t.Kind != TokComment {
			continue
		}
		if m := sectionRe.FindStringSubmatch(t.Text); m != nil {
			sections = append(sections, section{off: t.off, name: strings.TrimSpace(m[1])})
			continue
		}
		if strings.HasPrefix(t.Text, "//") {
			lineComment[t.Line] = t.Text
		}
	}

	out := []Param{}
	for _, s := range top {
		if s.kind == stmtModule || s.kind == stmtFunction {
			break
		}
		if s.kind != stmtAssign {
			continue
		}
		name := p.sig[s.name]
		if strings.HasPrefix(name.Text, "$") {
			continue
		}
		pr, ok := paramValue(p.sig, s.value)
		if !ok {
			continue
		}
		pr.Name = name.Text
		pr.Line = name.Line
		for _, sec := range sections {
			if sec.off < name.off {
				pr.Group = sec.name
			}
		}
		pr.Hidden = strings.EqualFold(pr.Group, "hidden")

		last := p.sig[s.end-1]
		if c, has := lineComment[last.Line]; has && codeLine[last.Line] {
			if m := rangeCommentRe.FindStringSubmatch(c); m != nil {
				applyAnnotation(&pr, m[1])
			}
		}
		// Descripción: el comentario de la línea de arriba si está solo en ella.
		if prev := name.Line - 1; prev >= 1 && !codeLine[prev] {
			if c, has := lineComment[prev]; has {
				pr.Description = strings.TrimSpace(strings.TrimPrefix(c, "//"))
			}
		}
		out = append(out, pr)
	}
	return out, nil
}

// paramValue reconoce un valor literal en sig[r[0]:r[1]].
func paramValue(sig []tok, r [2]int) (Param, bool) {
	if r[1] <= r[0] || r[1] > len(sig) {
		return Param{}, false
	}
	toks := sig[r[0]:r[1]]
	pr := Param{valStart: toks[0].off, valEnd: toks[len(toks)-1].end}
	pr.Literal = joinLiteral(toks)
	switch {
	case len(toks) == 1 && toks[0].Kind == TokString:
		pr.Type = ParamString
		pr.Value = unquoteScad(toks[0].Text)
		return pr, true
	case len(toks) == 1 && toks[0].Kind == TokKeyword && (toks[0].Text == "true" || toks[0].Text == "false"):
		pr.Type = ParamBool
		pr.Value = toks[0].Text == "true"
		return pr, true
	}
	if n, used, ok := signedNumber(toks); ok && used == len(toks) {
		pr.Type = ParamNumber
		pr.Value = n
		return pr, true
	}
	// Vector de números: [1, -2, 3.5]
	if len(toks) >= 2 && toks[0].is(TokPunct, "[") && toks[len(toks)-1].is(TokPunct, "]") {
		inner := toks[1 : len(toks)-1]
		vec := []float64{}
		for i := 0; i < len(inner); {
			n, used, ok := signedNumber(inner[i:])
			if !ok {
				return Param{}, false
			}
			vec = append(vec, n)
			i += used
			if i < len(inner) {
				if !inner[i].is(TokPunct, ",") {
					return Param{}, false
				}
				i++
				if i == len(inner) { // coma final: [1, 2,]
					break
				}
			}
		}
		if len(vec) == 0 {
			return Param{}, false
		}
		pr.Type = ParamVector
		pr.Value = vec
		return pr, true
	}
	return Param{}, false
}

// signedNumber lee un número con signo opcional al principio de toks y dice
// cuántos tokens usó.
func signedNumber(toks []tok) (float64, int, bool) {
	i, sign := 0, 1.0
	if i < len(toks) && toks[i].Kind == TokOp && (toks[i].Text == "-" || toks[i].Text == "+") {
		if toks[i].Text == "-" {
			sign = -1
		}
		i++
	}
	if i >= len(toks) || toks[i].Kind != TokNumber {
		return 0, 0, false
	}
	n, err := parseScadNumber(toks[i].Text)
	if err != nil {
		return 0, 0, false
	}
	return sign * n, i + 1, true
}

func parseScadNumber(s string) (float64, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		v, err := strconv.ParseInt(s[2:], 16, 64)
		return float64(v), err
	}
	return strconv.ParseFloat(s, 64)
}

// joinLiteral reconstruye el literal sin los espacios de adentro.
func joinLiteral(toks []tok) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 && t.Kind != TokPunct && toks[i-1].is(TokPunct, ",") {
			b.WriteByte(' ')
		}
		b.WriteString(t.Text)
	}
	return b.String()
}

// unquoteScad quita las comillas de un string de OpenSCAD y resuelve sus escapes.
func unquoteScad(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// quoteScad escribe s como literal de OpenSCAD.
func quoteScad(s string) (string, error) {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			return "", i18n.New(i18n.Msg{ES: "el texto lleva caracteres de control", EN: "the text contains control characters"})
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

// applyAnnotation interpreta lo que hay entre corchetes del comentario final:
// un rango (min:max o min:paso:max, solo números) o una lista de opciones.
func applyAnnotation(p *Param, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	if !strings.Contains(body, ",") && strings.Contains(body, ":") {
		parts := strings.Split(body, ":")
		nums := make([]float64, 0, 3)
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if !numRe.MatchString(s) {
				nums = nil
				break
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				nums = nil
				break
			}
			nums = append(nums, v)
		}
		switch len(nums) {
		case 2:
			p.Min, p.Max = &nums[0], &nums[1]
			return
		case 3:
			p.Min, p.Step, p.Max = &nums[0], &nums[1], &nums[2]
			return
		}
		// «valor:etiqueta» suelto: una lista de una opción; sigue abajo.
	}
	for _, item := range splitTop(body, ',') {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		val, label := item, ""
		if i := strings.Index(item, ":"); i >= 0 {
			val, label = strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
		}
		opt := ParamOption{Label: label}
		if p.Type == ParamNumber && numRe.MatchString(val) {
			v, _ := strconv.ParseFloat(val, 64)
			opt.Value = v
		} else {
			opt.Value = strings.Trim(val, `"`)
		}
		p.Options = append(p.Options, opt)
	}
	// Una lista de opciones solo tiene sentido para un número o un texto.
	if p.Type != ParamNumber && p.Type != ParamString {
		p.Options = nil
	}
}

// splitTop parte por sep ignorando lo que va entre comillas.
func splitTop(s string, sep byte) []string {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			quoted = !quoted
		case s[i] == sep && !quoted:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// SetParameters cambia el valor de los parámetros nombrados en values y devuelve
// el código nuevo, qué cambió y avisos (valor fuera de rango o de la lista de
// opciones; se aplica igual: OpenSCAD no lo impide). Todo lo demás del archivo
// queda byte a byte: comentarios, anotaciones, orden.
//
// Es todo o nada: un nombre que no es un parámetro visible del Customizer (ni
// uno de la sección Hidden), o un valor del tipo equivocado, devuelve error y no
// cambia nada.
func SetParameters(src string, values map[string]any) (string, []ParamChange, []string, error) {
	return setParameters(src, values, false)
}

// SetHiddenParameter es SetParameters para una constante de la sección Hidden
// (no editable por el usuario en el Customizer). La usa ScadLab para ajustar lo
// que las plantillas derivan de la impresora (PLATE); no es una puerta para que
// la IA cambie constantes: set_parameters no la expone.
func SetHiddenParameter(src, name string, value any) (string, []ParamChange, error) {
	out, changes, _, err := setParameters(src, map[string]any{name: value}, true)
	return out, changes, err
}

func setParameters(src string, values map[string]any, allowHidden bool) (string, []ParamChange, []string, error) {
	if len(values) == 0 {
		return "", nil, nil, i18n.New(i18n.Msg{ES: "no se indicó ningún valor para cambiar", EN: "no value to change was given"})
	}
	params, err := Parameters(src)
	if err != nil {
		return "", nil, nil, err
	}
	byName := make(map[string]*Param, len(params))
	var visible []string
	for i := range params {
		byName[params[i].Name] = &params[i]
		if !params[i].Hidden {
			visible = append(visible, params[i].Name)
		}
	}
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)

	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	var changes []ParamChange
	var warnings []string
	for _, name := range names {
		p, ok := byName[name]
		switch {
		case !ok:
			return "", nil, nil, i18n.Errorf(i18n.Msg{
				ES: "%q no es un parámetro del Customizer (disponibles: %s)",
				EN: "%q is not a Customizer parameter (available: %s)",
			}, name, strings.Join(visible, ", "))
		case p.Hidden && !allowHidden:
			return "", nil, nil, i18n.Errorf(i18n.Msg{
				ES: "%q está en la sección Hidden: no es un parámetro editable (cámbialo con un checkpoint nuevo escrito a mano)",
				EN: "%q is in the Hidden section: it is not an editable parameter (change it with a hand-written new checkpoint)",
			}, name)
		}
		lit, warn, err := formatParam(p, values[name])
		if err != nil {
			return "", nil, nil, err
		}
		warnings = append(warnings, warn...)
		if lit == p.Literal {
			continue
		}
		edits = append(edits, edit{start: p.valStart, end: p.valEnd, text: lit})
		changes = append(changes, ParamChange{Name: name, Old: p.Literal, New: lit})
	}
	// De atrás hacia adelante para no mover los desplazamientos de los demás.
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := src
	for _, e := range edits {
		out = out[:e.start] + e.text + out[e.end:]
	}
	return out, changes, warnings, nil
}

// formatParam convierte el valor que llegó (JSON) al literal de OpenSCAD según
// el tipo del parámetro y devuelve los avisos de rango/opciones.
func formatParam(p *Param, v any) (string, []string, error) {
	wrong := func() error {
		return i18n.Errorf(i18n.Msg{
			ES: "%s es de tipo %s (valor actual %s): el valor recibido no sirve",
			EN: "%s is of type %s (current value %s): the value received does not fit",
		}, p.Name, p.Type, p.Literal)
	}
	var warns []string
	switch p.Type {
	case ParamNumber:
		n, ok := asNumber(v)
		if !ok {
			return "", nil, wrong()
		}
		warns = append(warns, checkNumber(p, n)...)
		return formatNumber(n), warns, nil
	case ParamBool:
		switch b := v.(type) {
		case bool:
			return strconv.FormatBool(b), nil, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(b)) {
			case "true", "yes", "1":
				return "true", nil, nil
			case "false", "no", "0":
				return "false", nil, nil
			}
		}
		return "", nil, wrong()
	case ParamString:
		s, ok := v.(string)
		if !ok {
			return "", nil, wrong()
		}
		lit, err := quoteScad(s)
		if err != nil {
			return "", nil, err
		}
		if len(p.Options) > 0 {
			found := false
			for _, o := range p.Options {
				if os, _ := o.Value.(string); os == s {
					found = true
				}
			}
			if !found {
				warns = append(warns, i18n.T(i18n.Msg{
					ES: "%s: %q no está en la lista de opciones (%s)",
					EN: "%s: %q is not in the options list (%s)",
				}, p.Name, s, optionList(p.Options)))
			}
		}
		return lit, warns, nil
	case ParamVector:
		cur, _ := p.Value.([]float64)
		list, ok := v.([]any)
		if fl, isF := v.([]float64); isF {
			list, ok = make([]any, len(fl)), true
			for i, x := range fl {
				list[i] = x
			}
		}
		if !ok || len(list) != len(cur) {
			return "", nil, i18n.Errorf(i18n.Msg{
				ES: "%s es un vector de %d números (valor actual %s): manda una lista del mismo largo",
				EN: "%s is a vector of %d numbers (current value %s): send a list of the same length",
			}, p.Name, len(cur), p.Literal)
		}
		parts := make([]string, len(list))
		for i, it := range list {
			n, ok := asNumber(it)
			if !ok {
				return "", nil, wrong()
			}
			warns = append(warns, checkNumber(p, n)...)
			parts[i] = formatNumber(n)
		}
		return "[" + strings.Join(parts, ", ") + "]", warns, nil
	}
	return "", nil, wrong()
}

func optionList(opts []ParamOption) string {
	parts := make([]string, len(opts))
	for i, o := range opts {
		parts[i] = fmt.Sprint(o.Value)
	}
	return strings.Join(parts, ", ")
}

// checkNumber avisa de un valor fuera del rango o de la lista de opciones.
func checkNumber(p *Param, n float64) []string {
	var w []string
	if (p.Min != nil && n < *p.Min) || (p.Max != nil && n > *p.Max) {
		lo, hi := "…", "…"
		if p.Min != nil {
			lo = formatNumber(*p.Min)
		}
		if p.Max != nil {
			hi = formatNumber(*p.Max)
		}
		w = append(w, i18n.T(i18n.Msg{
			ES: "%s: %s está fuera del rango del Customizer [%s … %s]",
			EN: "%s: %s is outside the Customizer range [%s … %s]",
		}, p.Name, formatNumber(n), lo, hi))
	}
	if len(p.Options) > 0 {
		found := false
		for _, o := range p.Options {
			if ov, ok := o.Value.(float64); ok && ov == n {
				found = true
			}
		}
		if !found {
			w = append(w, i18n.T(i18n.Msg{
				ES: "%s: %s no está en la lista de opciones (%s)",
				EN: "%s: %s is not in the options list (%s)",
			}, p.Name, formatNumber(n), optionList(p.Options)))
		}
	}
	return w
}

// asNumber acepta un número, un json.Number o un texto numérico (los modelos
// chicos mandan "40").
func asNumber(v any) (float64, bool) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		n = f
	case string:
		s := strings.TrimSpace(x)
		if !numRe.MatchString(s) {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		n = f
	default:
		return 0, false
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// formatNumber escribe un número sin ceros de más (40, 2.5).
func formatNumber(n float64) string {
	if n == math.Trunc(n) && math.Abs(n) < 1e15 {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}
