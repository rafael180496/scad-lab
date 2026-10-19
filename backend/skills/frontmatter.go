package skills

import (
	"strings"
	"unicode/utf8"

	"scadlab/backend/i18n"
)

// DescriptionPlaceholder es la descripción con la que nace una skill nueva: no
// repite la de la plantilla («no es una skill operativa…»), que quedaba como
// descripción real de la skill y la dejaba sin cuándo-usarla. El editor la
// marca como pendiente hasta que se reescribe.
const DescriptionPlaceholder = "Describe aquí, en una o dos frases, cuándo debe usar el agente esta skill"

// templateDescriptionPrefix es el comienzo de la descripción de la plantilla:
// una skill creada con una versión anterior de ScadLab puede conservarla.
const templateDescriptionPrefix = "Plantilla para crear skills nuevas"

// MaxDescriptionLen es el tope de la descripción que aceptan los CLIs agénticos.
const MaxDescriptionLen = 1024

// fmField es un campo del frontmatter con su posición (línea 1-based).
type fmField struct {
	Present bool
	Value   string
	Line    int
	// Plain: escalar sin comillas ni bloque (el único que puede romper el YAML
	// con un «: » o un « #» dentro).
	Plain bool
}

// fmDoc es lo que se lee del bloque YAML inicial de un SKILL.md.
type fmDoc struct {
	Opened      bool // la primera línea es `---`
	Closed      bool
	CloseLine   int
	Name        fmField
	Description fmField
}

// parseFrontmatter lee name y description del bloque inicial. Basta con un
// lector de líneas `clave: valor` (más escalares de bloque `>` y `|`): el
// formato de SKILL.md no usa YAML anidado en esas dos claves. Solo cuentan las
// claves de primer nivel, y la primera aparición.
func parseFrontmatter(raw []byte) fmDoc {
	var d fmDoc
	text := strings.TrimPrefix(string(raw), "\ufeff")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return d
	}
	d.Opened = true
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimRight(line, " \t") == "---" {
			d.Closed, d.CloseLine = true, i+1
			return d
		}
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key != "name" && key != "description" {
			continue
		}
		f := &d.Name
		if key == "description" {
			f = &d.Description
		}
		if f.Present {
			continue
		}
		v = strings.TrimSpace(v)
		*f = fmField{Present: true, Line: i + 1}
		switch {
		case v != "" && (v[0] == '>' || v[0] == '|'):
			// Escalar de bloque: las líneas siguientes con sangría (o vacías).
			fold := v[0] == '>'
			var parts []string
			for i+1 < len(lines) {
				next := lines[i+1]
				if next != "" && next[0] != ' ' && next[0] != '\t' {
					break
				}
				parts = append(parts, strings.TrimSpace(next))
				i++
			}
			sep := "\n"
			if fold {
				sep = " "
			}
			f.Value = strings.TrimSpace(strings.Join(parts, sep))
		case len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0]:
			f.Value = v[1 : len(v)-1]
		default:
			f.Plain = true
			if j := strings.Index(v, " #"); j >= 0 {
				v = strings.TrimSpace(v[:j])
			}
			f.Value = strings.Trim(v, `"'`)
		}
	}
	return d
}

// frontmatter extrae name y description del SKILL.md (vacíos si no hay bloque).
func frontmatter(raw []byte) (name, desc string) {
	d := parseFrontmatter(raw)
	return d.Name.Value, d.Description.Value
}

// setFrontmatterField reemplaza el valor de una clave de primer nivel del
// frontmatter (o la agrega tras la apertura si falta). Sin bloque de
// frontmatter devuelve el texto tal cual: el cuerpo de la skill puede tener una
// línea `name:` que no es la del frontmatter. Conserva los fines de línea.
func setFrontmatterField(raw []byte, key, value string) []byte {
	text := string(raw)
	bom := ""
	if strings.HasPrefix(text, "\ufeff") {
		bom, text = "\ufeff", strings.TrimPrefix(text, "\ufeff")
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimRight(strings.TrimRight(lines[0], "\r"), " \t") != "---" {
		return raw
	}
	eol := ""
	if strings.HasSuffix(lines[0], "\r") {
		eol = "\r"
	}
	for i := 1; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r")
		if strings.TrimRight(l, " \t") == "---" {
			// Cerró sin la clave: se agrega justo antes del cierre.
			lines = append(lines[:i], append([]string{key + ": " + value + eol}, lines[i:]...)...)
			return []byte(bom + strings.Join(lines, "\n"))
		}
		if k, _, ok := strings.Cut(l, ":"); ok && l != "" && l[0] != ' ' && l[0] != '\t' && l[0] != '#' && strings.TrimSpace(k) == key {
			// Un escalar de bloque ocupa también las líneas con sangría que siguen.
			end := i + 1
			if _, v, _ := strings.Cut(l, ":"); strings.HasPrefix(strings.TrimSpace(v), ">") || strings.HasPrefix(strings.TrimSpace(v), "|") {
				for end < len(lines) && (strings.TrimRight(lines[end], "\r") == "" || lines[end][0] == ' ' || lines[end][0] == '\t') {
					end++
				}
			}
			lines = append(lines[:i], append([]string{key + ": " + value + eol}, lines[end:]...)...)
			return []byte(bom + strings.Join(lines, "\n"))
		}
	}
	return raw
}

// setFrontmatterName pone `name:` en el frontmatter.
func setFrontmatterName(raw []byte, name string) []byte {
	return setFrontmatterField(raw, "name", name)
}

// Diagnostic es un problema del frontmatter de un SKILL.md, para marcarlo en
// el editor.
type Diagnostic struct {
	Line     int    `json:"line"`     // 1-based
	Severity string `json:"severity"` // "error" | "warning"
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func diag(line int, sev, code string, m i18n.Msg, args ...any) Diagnostic {
	if line < 1 {
		line = 1
	}
	return Diagnostic{Line: line, Severity: sev, Code: code, Message: i18n.T(m, args...)}
}

// HasErrors dice si alguno de los diagnósticos impide guardar.
func HasErrors(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

// ValidateSkillMD revisa el frontmatter de un SKILL.md: `name` y `description`
// obligatorios, `name` en kebab-case e igual al nombre de la carpeta. folder es
// el nombre de la carpeta de la skill. Los errores impiden guardar; los avisos
// (descripción sin reescribir, demasiado larga, YAML dudoso) no.
func ValidateSkillMD(folder string, raw []byte) []Diagnostic {
	out := []Diagnostic{}
	d := parseFrontmatter(raw)
	if !d.Opened {
		return append(out, diag(1, "error", "SK001", i18n.Msg{
			ES: "SKILL.md debe empezar con un bloque de frontmatter: una primera línea «---», name y description, y otra «---»",
			EN: "SKILL.md must start with a frontmatter block: a first «---» line, name and description, and another «---»",
		}))
	}
	if !d.Closed {
		out = append(out, diag(1, "error", "SK002", i18n.Msg{
			ES: "el frontmatter no se cierra: falta la segunda línea «---»",
			EN: "the frontmatter is not closed: the second «---» line is missing",
		}))
	}

	switch {
	case !d.Name.Present:
		out = append(out, diag(1, "error", "SK003", i18n.Msg{ES: "falta el campo obligatorio name", EN: "the required field name is missing"}))
	case strings.TrimSpace(d.Name.Value) == "":
		out = append(out, diag(d.Name.Line, "error", "SK003", i18n.Msg{ES: "name está vacío", EN: "name is empty"}))
	case !nameRe.MatchString(d.Name.Value):
		out = append(out, diag(d.Name.Line, "error", "SK004", i18n.Msg{
			ES: "name inválido: usa minúsculas, números y guiones (kebab-case)",
			EN: "invalid name: use lowercase letters, digits and hyphens (kebab-case)",
		}))
	case d.Name.Value != folder:
		out = append(out, diag(d.Name.Line, "error", "SK005", i18n.Msg{
			ES: "name tiene que ser igual al nombre de la carpeta de la skill («%s»)",
			EN: "name must match the skill folder name («%s»)",
		}, folder))
	}

	desc := strings.TrimSpace(d.Description.Value)
	switch {
	case !d.Description.Present:
		out = append(out, diag(1, "error", "SK006", i18n.Msg{ES: "falta el campo obligatorio description", EN: "the required field description is missing"}))
	case desc == "":
		out = append(out, diag(d.Description.Line, "error", "SK006", i18n.Msg{ES: "description está vacía", EN: "description is empty"}))
	default:
		if desc == DescriptionPlaceholder || strings.HasPrefix(desc, templateDescriptionPrefix) {
			out = append(out, diag(d.Description.Line, "warning", "SK008", i18n.Msg{
				ES: "description sigue siendo el texto de ejemplo: di cuándo debe usar el agente esta skill",
				EN: "description is still the sample text: say when the agent should use this skill",
			}))
		}
		if n := utf8.RuneCountInString(desc); n > MaxDescriptionLen {
			out = append(out, diag(d.Description.Line, "warning", "SK007", i18n.Msg{
				ES: "description tiene %d caracteres; los agentes aceptan hasta %d",
				EN: "description is %d characters long; agents accept up to %d",
			}, n, MaxDescriptionLen))
		}
		if d.Description.Plain && (strings.Contains(d.Description.Value, ": ") || strings.HasSuffix(d.Description.Value, ":")) {
			out = append(out, diag(d.Description.Line, "warning", "SK009", i18n.Msg{
				ES: "description lleva «: » sin comillas: el YAML puede no leerse. Enciérrala entre comillas",
				EN: "description contains «: » without quotes: the YAML may not parse. Wrap it in quotes",
			}))
		}
	}
	return out
}
