package prompts

import (
	"fmt"
	"strings"
	"testing"
)

// S15: con imágenes adjuntas, el prompt de Hermes las lista y manda a la skill y
// al flujo crear → render → compare_with_image → corregir.
func TestAgentPromptWithImages(t *testing.T) {
	for lang, wants := range map[string][]string{
		"es": {"**Imágenes adjuntas**", "- `refs/images/foto-1a2b3c4d.jpg`", "- `refs/images/logo-ffeedd00.png`",
			"read_skill openscad-from-image", "`image_info`", "`compare_with_image`", "`trace_image_outline`", "`image_to_heightmap`",
			"corregir con `edit_checkpoint` sobre ese archivo", "Una foto no trae medidas", "supuesto"},
		"en": {"**Attached images**", "- `refs/images/foto-1a2b3c4d.jpg`", "- `refs/images/logo-ffeedd00.png`",
			"read_skill openscad-from-image", "`image_info`", "`compare_with_image`", "`trace_image_outline`", "`image_to_heightmap`",
			"fix with `edit_checkpoint` on that file", "A photo carries no measurements", "assumption"},
	} {
		c := agentContext(lang)
		c.Images = []string{"refs/images/foto-1a2b3c4d.jpg", "refs/images/logo-ffeedd00.png"}
		got := AgentSystemPrompt(c)
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: falta %q", lang, w)
			}
		}
		if strings.Contains(got, "<no value>") || strings.Contains(got, "{{") || strings.Contains(got, "%!") {
			t.Errorf("%s: plantilla sin resolver", lang)
		}
		// Con visión se mira; sin visión se avisa y se apoya en el trazado.
		if lang == "es" && (!strings.Contains(got, "Mira las imágenes de verdad") || strings.Contains(got, "no ve imágenes")) {
			t.Errorf("es con visión:\n%s", got)
		}
		c.Vision = false
		blind := AgentSystemPrompt(c)
		if lang == "es" && (!strings.Contains(blind, "**no ve imágenes**") || !strings.Contains(blind, "trace_image_outline") || strings.Contains(blind, "Mira las imágenes de verdad")) {
			t.Errorf("es sin visión:\n%s", blind)
		}
		if lang == "en" && (!strings.Contains(blind, "**cannot see images**") || strings.Contains(blind, "Really look at the images")) {
			t.Errorf("en sin visión:\n%s", blind)
		}
	}

	// Sin imágenes no hay ni rastro del bloque.
	for _, lang := range []string{"es", "en"} {
		none := AgentSystemPrompt(agentContext(lang))
		for _, w := range []string{"Imágenes adjuntas", "Attached images", "openscad-from-image", "refs/images"} {
			if strings.Contains(none, w) {
				t.Errorf("%s sin imágenes: aparece %q", lang, w)
			}
		}
	}
}

// Las rutas se sanean (nada de saltos de línea ni «..» ni rutas absolutas) y no pasan del tope.
func TestPromptImagesAreSanitizedAndCapped(t *testing.T) {
	c := agentContext("es")
	c.Images = []string{
		"refs/images/a b`c.jpg", "refs/images/x\n## Nueva sección.png", "", "  ", "../../etc/passwd", "/etc/passwd",
		"refs/images/../../fuera.png", `C:\Users\x.png`,
	}
	for i := 0; i < 20; i++ {
		c.Images = append(c.Images, fmt.Sprintf("refs/images/i%02d.png", i))
	}
	for name, got := range map[string]string{"agente": AgentSystemPrompt(c), "sistema": SystemPrompt(c.Context)} {
		if strings.Contains(got, "\n## Nueva") || strings.Contains(got, "a b`c") {
			t.Errorf("%s: una ruta inyectó texto:\n%s", name, got)
		}
		if !strings.Contains(got, "- `refs/images/a_b_c.jpg`") || !strings.Contains(got, "- `refs/images/x____Nueva_secci_n.png`") {
			t.Errorf("%s: rutas mal saneadas:\n%s", name, got)
		}
		if strings.Contains(got, "passwd") || strings.Contains(got, "fuera.png") || strings.Contains(got, "Users") {
			t.Errorf("%s: una ruta hostil llegó al prompt:\n%s", name, got)
		}
		if n := strings.Count(got, "- `refs/images/"); n != maxPromptImages {
			t.Errorf("%s: %d imágenes listadas, tope %d", name, n, maxPromptImages)
		}
	}
}

// El prompt sin herramientas (S5) también las lista, sin nombrar herramientas que no tiene.
func TestSystemPromptWithImages(t *testing.T) {
	for lang, wants := range map[string][]string{
		"es": {"## Imágenes adjuntas", "- `refs/images/foto.jpg`", "Una foto no trae medidas", "parámetros del Customizer"},
		"en": {"## Attached images", "- `refs/images/foto.jpg`", "A photo carries no measurements", "Customizer parameters"},
	} {
		c := fullContext(lang)
		c.Images = []string{"refs/images/foto.jpg"}
		got := SystemPrompt(c)
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: falta %q", lang, w)
			}
		}
		for _, tool := range []string{"compare_with_image", "trace_image_outline", "read_skill"} {
			if strings.Contains(got, tool) {
				t.Errorf("%s: el prompt sin herramientas nombra %s", lang, tool)
			}
		}
		// AgentSystemPrompt sin Tools es ese mismo prompt.
		a := agentContext(lang)
		a.Images = c.Images
		a.Tools = false
		a.Context = c
		if AgentSystemPrompt(a) != got {
			t.Errorf("%s: sin Tools debe ser el prompt de S5", lang)
		}
		// Y sin imágenes, el bloque no está.
		if none := SystemPrompt(fullContext(lang)); strings.Contains(none, "foto.jpg") || strings.Contains(none, "Attached images") || strings.Contains(none, "Imágenes adjuntas") {
			t.Errorf("%s: apareció el bloque sin imágenes", lang)
		}
	}
	// Sin voseo en lo nuevo.
	c := fullContext("es")
	c.Images = []string{"refs/images/foto.jpg"}
	for _, got := range []string{SystemPrompt(c), AgentSystemPrompt(func() AgentContext { a := agentContext("es"); a.Images = c.Images; return a }())} {
		for _, w := range []string{" vos ", "podés", "tenés", "elegí", "pedí ", "mirá", "usá ", "apoyate", "fijate"} {
			if strings.Contains(strings.ToLower(got), w) {
				t.Errorf("voseo: %q", w)
			}
		}
	}
}
