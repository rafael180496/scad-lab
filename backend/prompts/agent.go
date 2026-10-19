package prompts

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"unicode"

	"scadlab/assets"
)

// SkillInfo es lo que el prompt de Hermes dice de cada skill del workspace:
// nombre y descripción. El contenido se lee bajo demanda con `read_skill`.
type SkillInfo struct {
	Name        string
	Description string
}

// AgentContext es lo que el prompt de Hermes sabe: el contexto del chat de S5
// más lo propio del modo con herramientas.
type AgentContext struct {
	Context
	// Skills son las skills sincronizadas en el workspace (nombre y descripción).
	Skills []SkillInfo
	// Tools dice si el modelo puede llamar herramientas. Falso = el modo de
	// respaldo de S5: bloques de código y «Guardar como checkpoint».
	Tools bool
	// Vision dice si el modelo ve imágenes: con ella el preview se mira; sin
	// ella, solo se mide.
	Vision bool
	// MaxSteps es el tope de llamadas a herramientas del turno (<= 0: 12).
	MaxSteps int
	// References son las referencias 3D adjuntas, como rutas del workspace
	// ("refs/pieza.stl").
	References []string
}

// DefaultMaxSteps es el tope de pasos que el prompt anuncia sin un valor.
const DefaultMaxSteps = 12

// Topes del prompt: lo que el usuario o el workspace aportan no puede agrandarlo
// sin límite.
const (
	maxPromptSkills       = 30
	maxSkillDescRunes     = 240
	maxSkillNameRunes     = 64
	maxPromptReferences   = 10
	maxReferencePathRunes = 120
)

type skillView struct {
	Name        string
	Description string
}

// agentData es tplData más lo propio del modo agente.
type agentData struct {
	tplData
	Skills     []skillView
	Vision     bool
	MaxSteps   int
	References []string
}

var agentTemplates = map[string]*template.Template{}

func init() {
	for _, lang := range []string{"es", "en"} {
		raw, err := assets.FS.ReadFile("prompts/agent." + lang + ".md")
		if err != nil {
			panic("prompts: falta la plantilla embebida del agente " + lang + ": " + err.Error())
		}
		agentTemplates[lang] = template.Must(template.New("agent-" + lang).Option("missingkey=error").Parse(string(raw)))
	}
}

// AgentSystemPrompt arma el prompt de sistema de Hermes. Con Tools describe el
// bucle de herramientas (crear checkpoint → render → preview → measure →
// corregir en un checkpoint nuevo), las skills disponibles y cómo tratar las
// referencias y fotos adjuntas. Sin Tools devuelve el prompt de S5
// (SystemPrompt): el modelo no tiene herramientas, así que entrega bloques de
// código que el usuario guarda como checkpoint.
func AgentSystemPrompt(c AgentContext) string {
	if !c.Tools {
		return SystemPrompt(c.Context)
	}
	lang := normLang(c.Lang)
	d := agentData{
		tplData:  buildData(c.Context),
		Vision:   c.Vision,
		MaxSteps: c.MaxSteps,
	}
	if d.MaxSteps <= 0 {
		d.MaxSteps = DefaultMaxSteps
	}
	for _, s := range c.Skills {
		name := oneLine(s.Name, maxSkillNameRunes)
		if name == "" {
			continue
		}
		if len(d.Skills) >= maxPromptSkills {
			break
		}
		d.Skills = append(d.Skills, skillView{Name: strings.ReplaceAll(name, "`", "'"), Description: shorten(s.Description, maxSkillDescRunes)})
	}
	for _, r := range c.References {
		if p := SafeRefPath(r); p != "" && len(d.References) < maxPromptReferences {
			d.References = append(d.References, p)
		}
	}
	var b bytes.Buffer
	if err := agentTemplates[lang].Execute(&b, d); err != nil {
		panic(fmt.Sprintf("prompts: render agente %s: %v", lang, err))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// shorten deja el texto en una línea de a lo más max runas, con «…» si se cortó.
func shorten(s string, max int) string {
	s = strings.ReplaceAll(oneLine(s, 4*max), "`", "'")
	if r := []rune(s); len(r) > max {
		return strings.TrimRight(string(r[:max-1]), " ") + "…"
	}
	return s
}

// SafeRefPath deja la ruta de una referencia apta para ir dentro del prompt:
// solo [A-Za-z0-9._/-], el resto pasa a "_", tope de largo. Vacío si no queda
// nada o si la ruta es absoluta o sube con «..». Una ruta normal ("refs/pieza.stl") queda igual.
func SafeRefPath(p string) string {
	p = strings.TrimSpace(p)
	// Una referencia es siempre relativa y de adentro del workspace: una ruta
	// absoluta o con «..» no es una y no se le muestra al modelo como tal.
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || strings.Contains(p, "..") {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range p {
		if n >= maxReferencePathRunes {
			break
		}
		switch {
		case r < unicode.MaxASCII && (r == '.' || r == '-' || r == '_' || r == '/' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		n++
	}
	return b.String()
}
