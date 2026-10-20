package prompts_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"scadlab/backend/appdata"
	"scadlab/backend/mcp"
	"scadlab/backend/printers"
	"scadlab/backend/prompts"
	"scadlab/backend/workspace"
)

// Los tokens se estiman como caracteres/4 (la misma regla que hermes.EstimateTokens).
func estTokens(s string) int { return (utf8.RuneCountInString(s) + 3) / 4 }

// typicalAgentContext es un workspace real de trabajo: una maceta de 596 líneas
// abierta, varios checkpoints, skills sincronizadas y una referencia 3D.
func typicalAgentContext(t *testing.T, lang string, compact bool) prompts.AgentContext {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "mcp", "testdata", "maceta.scad"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := 1; i <= 12; i++ {
		names = append(names, fmt.Sprintf("%03d-paso-%d.scad", i, i))
	}
	return prompts.AgentContext{
		Context: prompts.Context{
			Lang: lang, WorkspaceName: "Macetas", PrinterName: "Bambu Lab A1 mini", Brand: "Bambu Lab",
			MulticolorSystem: "AMS lite", SlicerName: "Bambu Studio", NozzleMM: 0.4, FilamentDiameterMm: 1.75,
			BuildVolume: [3]float64{180, 180, 180}, ColorSlots: 4, Checkpoints: names, CurrentFile: names[len(names)-1],
			CurrentSource: string(raw), Compact: compact,
		},
		Tools: true, Vision: true, MaxSteps: 12,
		Skills: []prompts.SkillInfo{
			{Name: "openscad-core", Description: "Reglas base para escribir cualquier .scad: checkpoints, color, parámetros, convenciones de código y errores comunes"},
			{Name: "bambu-lab-printers", Description: "Perfiles de impresoras Bambu Lab (A1, A1 mini, P1S, X1C, H2D), volúmenes, AMS y reglas FDM de la marca"},
			{Name: "openscad-3mf-multicolor", Description: "Cómo exportar un 3MF multicolor para cada slicer: una parte por color con su extrusor"},
			{Name: "openscad-templates", Description: "Plantillas paramétricas listas: caja con tapa, soporte, engranaje, bisagra"},
			{Name: "openscad-libraries", Description: "BOSL2, MCAD y NopSCADlib: cuándo usarlas y cómo instalarlas"},
			{Name: "openscad-from-image", Description: "Modelar a partir de una foto, un logo o un boceto"},
		},
		References: []string{"refs/soporte.stl"},
	}
}

// El prompt compacto: ≤ 1 200 tokens con un contexto típico, en los dos idiomas,
// y mucho más corto que el completo (que además embebe el código abierto).
func TestCompactPromptFitsTheBudget(t *testing.T) {
	for _, lang := range []string{"es", "en"} {
		full := prompts.AgentSystemPrompt(typicalAgentContext(t, lang, false))
		compact := prompts.AgentSystemPrompt(typicalAgentContext(t, lang, true))
		ft, ct := estTokens(full), estTokens(compact)
		t.Logf("%s: prompt completo ≈ %d tokens (%d bytes), compacto ≈ %d tokens (%d bytes)", lang, ft, len(full), ct, len(compact))
		if ct > 1200 {
			t.Errorf("%s: el prompt compacto pesa ~%d tokens (máximo 1200)", lang, ct)
		}
		if ct*3 > ft {
			t.Errorf("%s: el compacto (%d) debería pesar menos de un tercio del completo (%d)", lang, ct, ft)
		}
		// Un contexto grande tampoco lo agranda: no embebe el código y limita la lista.
		big := typicalAgentContext(t, lang, true)
		for i := 13; i <= 200; i++ {
			big.Checkpoints = append(big.Checkpoints, fmt.Sprintf("%03d-otro-paso-con-un-nombre-largo.scad", i))
		}
		if bt := estTokens(prompts.AgentSystemPrompt(big)); bt > 1200 {
			t.Errorf("%s: con 200 checkpoints el compacto pesa ~%d tokens", lang, bt)
		}
	}
	// Sin imágenes es todavía más corto, y el vacío también renderiza.
	for _, lang := range []string{"es", "en", "", "fr"} {
		for name, c := range map[string]prompts.AgentContext{
			"vacío": {Context: prompts.Context{Lang: lang, Compact: true}, Tools: true},
			"visión": func() prompts.AgentContext {
				x := typicalAgentContext(t, lang, true)
				x.Vision = false
				return x
			}(),
		} {
			got := prompts.AgentSystemPrompt(c)
			if strings.Contains(got, "<no value>") || strings.Contains(got, "%!") || strings.Contains(got, "{{") {
				t.Errorf("%s/%q: plantilla sin resolver:\n%s", name, lang, got)
			}
			if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
				t.Errorf("%s/%q: debe terminar en exactamente un salto", name, lang)
			}
		}
	}
}

// Las mismas reglas que el completo, y solo las herramientas del perfil esencial.
func TestCompactPromptKeepsTheRulesAndOnlyNamesEssentialTools(t *testing.T) {
	t.Setenv(appdata.OverrideEnv, t.TempDir())
	ws, err := workspace.Create(t.TempDir(), "Prueba", mustPrinter(t))
	if err != nil {
		t.Fatal(err)
	}
	tb, err := mcp.NewToolbox(mcp.ToolboxOptions{WorkspaceDir: ws})
	if err != nil {
		t.Fatal(err)
	}
	defer tb.Close()
	catalog := map[string]bool{}
	for _, s := range tb.Tools() {
		catalog[s.Name] = true
	}
	essential := map[string]bool{}
	for _, n := range mcp.EssentialTools() {
		essential[n] = true
	}
	word := regexp.MustCompile(`[a-z][a-z0-9_]+`)

	for lang, wants := range map[string][]string{
		"es": {"`create_checkpoint`", "`set_parameters`", "`outline_checkpoint`", "`find_in_checkpoint`", "`read_checkpoint`", "`edit_checkpoint`", "from_line, to_line, new", "`variant=true`", "`color(", "`measure`", "`preview`", "`list_checkpoints`", "Nunca pidas permiso", "ninguna", "eps = 0.01"},
		"en": {"`create_checkpoint`", "`set_parameters`", "`outline_checkpoint`", "`find_in_checkpoint`", "`read_checkpoint`", "`edit_checkpoint`", "from_line, to_line, new", "`variant=true`", "`color(", "`measure`", "`preview`", "`list_checkpoints`", "Never ask permission", "none", "eps = 0.01"},
	} {
		got := prompts.AgentSystemPrompt(typicalAgentContext(t, lang, true))
		for _, w := range wants {
			if w == "ninguna" || w == "none" {
				continue
			}
			if !strings.Contains(got, w) {
				t.Errorf("%s: el prompt compacto no dice %q", lang, w)
			}
		}
		for _, w := range word.FindAllString(got, -1) {
			if catalog[w] && !essential[w] {
				t.Errorf("%s: el prompt compacto nombra %q, que no está en el perfil esencial", lang, w)
			}
		}
		// No embebe el código ni habla de skills, referencias o imágenes.
		for _, bad := range []string{"module perfil_cuerpo", "```openscad", "read_skill", "list_skills", "inspect_reference", "import_mesh", "refs/"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: el prompt compacto no debe incluir %q", lang, bad)
			}
		}
		if !strings.Contains(got, "596 ") {
			t.Errorf("%s: debe decir cuántas líneas tiene el checkpoint abierto: %s", lang, got[len(got)-300:])
		}
		if strings.Contains(got, "update_checkpoint") {
			t.Errorf("%s: update_checkpoint no está en el perfil esencial", lang)
		}
		// El estado: lista acotada a los últimos y el siguiente número.
		if !strings.Contains(got, "… 007-paso-7.scad, 008-paso-8.scad") || !strings.Contains(got, "012-paso-12.scad") || strings.Contains(got, "006-paso-6") || !strings.Contains(got, "013") {
			t.Errorf("%s: estado del workspace: %s", lang, got[len(got)-400:])
		}
		if lang == "es" {
			for _, voseo := range []string{"Corregí", "Pedí", "Creá", "podés", "tenés", "usá", "mirá"} {
				if strings.Contains(got, voseo) {
					t.Errorf("voseo: %q", voseo)
				}
			}
		}
	}

	// Compact=false no cambia el prompt de siempre.
	a := typicalAgentContext(t, "es", false)
	if got := prompts.AgentSystemPrompt(a); !strings.Contains(got, "## Cómo trabajas (el bucle)") || !strings.Contains(got, "module perfil_cuerpo") {
		t.Error("el prompt completo debe seguir siendo el de siempre")
	}
}

// Sin visión, el compacto lo dice; con visión manda mirar el preview.
func TestCompactPromptVisionStep(t *testing.T) {
	for lang, wants := range map[string][2]string{"es": {"mírala", "no ve imágenes"}, "en": {"look at it", "cannot see images"}} {
		c := typicalAgentContext(t, lang, true)
		if got := prompts.AgentSystemPrompt(c); !strings.Contains(got, wants[0]) || strings.Contains(got, wants[1]) {
			t.Errorf("%s con visión", lang)
		}
		c.Vision = false
		if got := prompts.AgentSystemPrompt(c); strings.Contains(got, wants[0]) || !strings.Contains(got, wants[1]) {
			t.Errorf("%s sin visión", lang)
		}
	}
}

func mustPrinter(t *testing.T) printers.Printer {
	t.Helper()
	p, ok := printers.ByID("bambu-a1-mini")
	if !ok {
		t.Fatal("falta la A1 mini")
	}
	return p
}
