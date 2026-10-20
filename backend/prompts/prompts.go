// Package prompts es el contenido de texto que ScadLab le da a la IA: el
// prompt de sistema del chat de diseño, el aviso de auto-iteración que se
// escribe en la terminal del agente y la extracción de los bloques OpenSCAD de
// una respuesta. Contrato en .claude/specs/s5-agentes-ia.md.
//
// Las plantillas viven en assets/prompts/ (system.es.md, system.en.md) y
// resumen —no copian— las skills de assets/skills/. Si cambia una regla de
// modelado (checkpoints, color, FDM), se cambia en la skill, en
// assets/workspace/RULES.md.tmpl y aquí.
package prompts

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"

	"scadlab/assets"
)

// MaxSourceBytes es el tope del código actual que entra al prompt. Un modelo
// local con ventana chica no puede pagar más, y el final de un archivo largo
// rara vez es lo que se está cambiando.
const MaxSourceBytes = 12 * 1024

// maxListedCheckpoints limita la lista de checkpoints del prompt: en un
// workspace largo, los últimos son los que importan para ramificar.
const maxListedCheckpoints = 40

// maxPromptImages limita las imágenes que el prompt lista: más de las que un
// modelo puede mirar bien no le sirven y agrandan el prompt.
const maxPromptImages = 10

// defaultColorSlots es lo que se asume sin dato: los 4 colores con que nace un
// workspace de la impresora de respaldo del catálogo (la A1 mini, printers.ByID).
// Una impresora conocida trae sus propios colores en Context.ColorSlots.
const defaultColorSlots = 4

// Context es lo que el prompt de sistema sabe del workspace.
type Context struct {
	Lang          string // "es" | "en" (cualquier otro valor es inglés)
	WorkspaceName string
	PrinterName   string
	BuildVolume   [3]float64
	ColorSlots    int
	Checkpoints   []string // nombres existentes (para sugerir el siguiente)
	CurrentFile   string   // checkpoint seleccionado ("" si ninguno)
	CurrentSource string   // su código (se recorta a MaxSourceBytes)

	// S15: las imágenes que el usuario adjuntó, como rutas relativas al workspace
	// ("refs/images/foto-1a2b3c4d.jpg"). Con ellas, los prompts mandan a la skill
	// `openscad-from-image` y al flujo crear → render → compare_with_image.
	Images []string

	// S14: la impresora efectiva del workspace (ColorSlots son sus colores, no el
	// máximo de la máquina). Las dos plantillas nombran marca, sistema
	// multicolor, boquilla, filamento y slicer cuando vienen; vacíos se omiten.
	Brand              string  // "Prusa Research"
	MulticolorSystem   string  // "Prusa MMU3"
	SlicerName         string  // el slicer recomendado, "PrusaSlicer"
	NozzleMM           float64 // boquilla, mm
	FilamentDiameterMm float64 // 1,75 o 2,85
}

// tplData es lo que ven las plantillas. Todo son strings, números y bools ya
// resueltos: así una plantilla nunca imprime "<no value>".
type tplData struct {
	WorkspaceName    string
	PrinterName      string
	HasVolume        bool
	X, Y, Z          string
	Slots            int
	Brand            string // "Prusa Research" ("" = no se nombra)
	MulticolorSystem string // "Prusa MMU3 (opcional)"
	SlicerName       string
	Nozzle           string   // "0.4"; "" si no se sabe
	Filament         string   // "1.75" o "2.85"; "" si no se sabe
	Images           []string // rutas de las imágenes adjuntas, ya saneadas
	CheckpointList   string
	Next             string // siguiente número de checkpoint, "004"
	CurrentFile      string
	HasCurrent       bool
	Parent           string // parent que debe llevar el próximo checkpoint
	HasSource        bool
	Source           string
	Fence            string // cerco de código más largo que cualquier ``` del código
	Truncated        bool
	LinesShown       int
	LinesTotal       int
}

var templates = map[string]*template.Template{}

func init() {
	for _, lang := range []string{"es", "en"} {
		raw, err := assets.FS.ReadFile("prompts/system." + lang + ".md")
		if err != nil {
			panic("prompts: falta la plantilla embebida " + lang + ": " + err.Error())
		}
		templates[lang] = template.Must(template.New(lang).Option("missingkey=error").Parse(string(raw)))
	}
}

func normLang(lang string) string {
	if lang == "es" {
		return "es"
	}
	return "en"
}

// SystemPrompt arma el prompt de sistema del chat: experto en OpenSCAD y en
// FDM para la impresora del workspace (cualquier marca del catálogo), contrato de checkpoints, color, formato de respuesta, y
// el estado del workspace (lista de checkpoints y código actual recortado).
func SystemPrompt(c Context) string {
	lang := normLang(c.Lang)
	d := buildData(c)
	var b bytes.Buffer
	if err := templates[lang].Execute(&b, d); err != nil {
		// Las plantillas son estáticas y los datos tipados: un fallo aquí es un
		// error de programación que el test de plantillas atrapa.
		panic(fmt.Sprintf("prompts: render %s: %v", lang, err))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func buildData(c Context) tplData {
	d := tplData{
		WorkspaceName: oneLine(c.WorkspaceName, 80),
		PrinterName:   oneLine(c.PrinterName, 80),
		Slots:         c.ColorSlots,
		Brand:         oneLine(c.Brand, 80),
		SlicerName:    oneLine(c.SlicerName, 80),
	}
	d.MulticolorSystem = oneLine(c.MulticolorSystem, 160)
	if c.NozzleMM > 0 {
		d.Nozzle = fmtMM(c.NozzleMM)
	}
	if c.FilamentDiameterMm > 0 {
		d.Filament = fmtMM(c.FilamentDiameterMm)
	}
	if d.WorkspaceName == "" {
		d.WorkspaceName = "—"
	}
	if d.Slots <= 0 {
		d.Slots = defaultColorSlots
	}
	if v := c.BuildVolume; v[0] > 0 && v[1] > 0 && v[2] > 0 {
		d.HasVolume = true
		d.X, d.Y, d.Z = fmtMM(v[0]), fmtMM(v[1]), fmtMM(v[2])
	}

	for _, r := range c.Images {
		if p := SafeRefPath(r); p != "" && len(d.Images) < maxPromptImages {
			d.Images = append(d.Images, p)
		}
	}

	names := make([]string, 0, len(c.Checkpoints))
	for _, n := range c.Checkpoints {
		if s := SafeFileName(n); s != "" {
			names = append(names, s)
		}
	}
	d.CurrentFile = SafeFileName(c.CurrentFile)
	d.HasCurrent = d.CurrentFile != ""
	d.Next = nextNumber(append(append([]string{}, names...), d.CurrentFile))
	listed := names
	prefix := ""
	if len(listed) > maxListedCheckpoints {
		listed = listed[len(listed)-maxListedCheckpoints:]
		prefix = "… "
	}
	if len(listed) > 0 {
		d.CheckpointList = prefix + strings.Join(listed, ", ")
	}

	switch {
	case d.HasCurrent:
		d.Parent = d.CurrentFile
	case len(names) > 0:
		d.Parent = latest(names)
	}

	if src := strings.TrimSpace(strings.ReplaceAll(c.CurrentSource, "\r\n", "\n")); src != "" {
		shown, truncated := truncateLines(src, MaxSourceBytes)
		d.HasSource = true
		d.Source = shown
		d.Truncated = truncated
		d.LinesShown = strings.Count(shown, "\n") + 1
		d.LinesTotal = strings.Count(src, "\n") + 1
		d.Fence = fenceFor(shown)
	}
	return d
}

// truncateLines recorta s a max bytes en un límite de línea (nunca a mitad de
// línea ni de runa). Si ni la primera línea cabe, corta en un límite de runa.
func truncateLines(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := s[:max]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		return cut[:i], true
	}
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut, true
}

// fenceFor devuelve un cerco de backticks más largo que cualquier racha de
// backticks dentro del código, para que este no pueda cerrar el bloque.
func fenceFor(code string) string {
	longest, run := 0, 0
	for _, r := range code {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := 3
	if longest >= n {
		n = longest + 1
	}
	return strings.Repeat("`", n)
}

func fmtMM(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

var numRe = regexp.MustCompile(`^(\d{3,})-`)

// nextNumber es el siguiente número de checkpoint: el mayor que exista + 1, a
// tres dígitos como mínimo (la convención de workspace.Scan).
func nextNumber(names []string) string {
	max := 0
	for _, n := range names {
		if m := numRe.FindStringSubmatch(n); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > max {
				max = v
			}
		}
	}
	return fmt.Sprintf("%03d", max+1)
}

// latest devuelve el checkpoint de mayor número; si ninguno trae número, el
// último de la lista.
func latest(names []string) string {
	best, bestN := names[len(names)-1], -1
	for _, n := range names {
		if m := numRe.FindStringSubmatch(n); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v >= bestN {
				best, bestN = n, v
			}
		}
	}
	return best
}

// SafeFileName deja un nombre de archivo apto para ir dentro de un prompt o de
// una línea de terminal: solo [A-Za-z0-9._-], el resto pasa a "_", tope 80
// caracteres. Un nombre con forma de checkpoint (NNN-slug.scad) queda igual.
func SafeFileName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	n := 0
	for _, r := range name {
		if n >= 80 {
			break
		}
		switch {
		case r < unicode.MaxASCII && (r == '.' || r == '-' || r == '_' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		n++
	}
	return b.String()
}

// oneLine colapsa saltos de línea y caracteres de control a espacios y recorta:
// el nombre del workspace y de la impresora los escribe el usuario.
func oneLine(s string, maxRunes int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxRunes {
		s = string(r[:maxRunes])
	}
	return s
}

// Nudge es el texto que se escribe en la terminal del agente cuando falla el
// render del checkpoint más nuevo (auto-iteración): una sola línea, en el
// idioma pedido, sin saltos de línea ni caracteres de control, que no termina
// en salto (la terminal decide si manda Enter). Pide leer RENDER_ERRORS.md y
// corregir en un checkpoint NUEVO con parent=fileName.
//
// fileName se sanea con SafeFileName: un nombre con caracteres raros nunca
// rompe la línea ni inyecta nada. Empieza por "ScadLab:" a propósito: un
// primer carácter "/", "!" o "#" lo interpretarían algunos CLIs como comando.
func Nudge(fileName string, errorCount int, lang string) string {
	name := SafeFileName(fileName)
	es := normLang(lang) == "es"
	var b strings.Builder
	b.WriteString("ScadLab: ")
	switch {
	case es && name == "":
		b.WriteString("el render del último checkpoint falló")
	case es:
		fmt.Fprintf(&b, "el render de \"%s\" falló", name)
	case name == "":
		b.WriteString("the render of the latest checkpoint failed")
	default:
		fmt.Fprintf(&b, "the render of \"%s\" failed", name)
	}
	if errorCount > 0 {
		switch {
		case es && errorCount == 1:
			b.WriteString(" con 1 error")
		case es:
			fmt.Fprintf(&b, " con %d errores", errorCount)
		case errorCount == 1:
			b.WriteString(" with 1 error")
		default:
			fmt.Fprintf(&b, " with %d errors", errorCount)
		}
	}
	parent := name
	if parent == "" {
		parent = "<archivo>"
		if !es {
			parent = "<file>"
		}
	}
	if es {
		fmt.Fprintf(&b, ". Lee RENDER_ERRORS.md y corrígelo en un checkpoint NUEVO con parent=%s; no edites el archivo que falló.", parent)
	} else {
		fmt.Fprintf(&b, ". Read RENDER_ERRORS.md and fix it in a NEW checkpoint with parent=%s; do not edit the file that failed.", parent)
	}
	return b.String()
}
