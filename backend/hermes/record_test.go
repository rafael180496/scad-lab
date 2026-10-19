package hermes

import (
	"encoding/base64"
	"strings"
	"testing"

	"scadlab/backend/llm"
)

func TestPartsRoundTrip(t *testing.T) {
	if EncodeParts(Parts{}) != "" {
		t.Error("sin partes se guarda vacío")
	}
	p := Parts{
		Attachments: []AttachmentMeta{{Kind: KindModel, Name: "soporte.stl", Size: 1234, RefPath: "refs/soporte.stl", Summary: "Referencia: refs/soporte.stl"}},
		Steps:       []Step{{ID: "st-1", Index: 1, Tool: "render", Args: map[string]any{"file": "001-a.scad"}, Status: StepOK, Text: "ok", Images: []string{}, TextAt: 12}},
		Model:       "gpt-x", StepLimit: true,
	}
	got := ParseParts(EncodeParts(p))
	if len(got.Attachments) != 1 || got.Attachments[0].RefPath != "refs/soporte.stl" || len(got.Steps) != 1 || got.Steps[0].TextAt != 12 || !got.StepLimit || got.Model != "gpt-x" {
		t.Errorf("ida y vuelta: %+v", got)
	}
	// Un mensaje viejo (vacío) o corrupto no rompe la conversación.
	for _, bad := range []string{"", "   ", "{no es json", "[1,2]"} {
		if p := ParseParts(bad); len(p.Steps) != 0 || len(p.Attachments) != 0 {
			t.Errorf("%q: %+v", bad, p)
		}
	}
}

func TestStepsForStorageShrinksImages(t *testing.T) {
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString(mustPNG(t, 1000, 800))
	steps := []Step{
		{ID: "a", Tool: "preview", Images: []string{png, "data:image/png;base64,@@@"}},
		{ID: "b", Tool: "render", Images: []string{}},
	}
	out := StepsForStorage(steps)
	if len(out[0].Images) != 1 || !strings.HasPrefix(out[0].Images[0], "data:image/jpeg;base64,") {
		t.Fatalf("la imagen válida pasa a JPEG y la ilegible se descarta: %d", len(out[0].Images))
	}
	if len(out[0].Images[0]) >= len(png) {
		t.Error("lo guardado debía ser más chico")
	}
	if steps[0].Images[0] != png {
		t.Error("no debe tocar los pasos recibidos")
	}
	if len(out[1].Images) != 0 {
		t.Errorf("sin imágenes: %v", out[1].Images)
	}
}

// SeedHistory: lo que un turno posterior (o tras reiniciar la app) le manda al
// modelo del historial guardado.
func TestSeedHistory(t *testing.T) {
	model := AttachmentMeta{Kind: KindModel, Name: "soporte.stl", RefPath: "refs/soporte.stl", Summary: "Referencia: refs/soporte.stl (STL, 12 triángulos)."}
	img := AttachmentMeta{Kind: KindImage, Name: "foto.png"}
	stored := []StoredMessage{
		{Role: "user", Content: "haz una caja", Parts: EncodeParts(Parts{Attachments: []AttachmentMeta{img}})},
		{Role: "assistant", Content: "Listo, 001-caja.scad.", Parts: EncodeParts(Parts{Steps: []Step{{Tool: "render"}}})},
		{Role: "user", Content: "", Parts: EncodeParts(Parts{Attachments: []AttachmentMeta{model}})},
		{Role: "user", Content: "y que encaje con eso"}, // el turno anterior falló sin respuesta
		{Role: "assistant", Content: "Hecho."},
	}
	msgs := SeedHistory(stored, 100_000)
	if len(msgs) != 4 {
		t.Fatalf("mensajes: %+v", msgs)
	}
	want := []string{llm.RoleUser, llm.RoleAssistant, llm.RoleUser, llm.RoleAssistant}
	for i, m := range msgs {
		if m.Role != want[i] {
			t.Errorf("rol %d: %s", i, m.Role)
		}
		if len(m.Images) != 0 || len(m.ToolCalls) != 0 {
			t.Errorf("el historial no reenvía imágenes ni herramientas: %+v", m)
		}
	}
	// La imagen vieja queda como una línea con su nombre.
	if !strings.Contains(msgs[0].Content, "haz una caja") || !strings.Contains(msgs[0].Content, "foto.png") {
		t.Errorf("mensaje 0: %q", msgs[0].Content)
	}
	// El modelo de referencia conserva su informe (dónde quedó en refs/), y los
	// dos mensajes de usuario seguidos se funden.
	if !strings.Contains(msgs[2].Content, "refs/soporte.stl") || !strings.Contains(msgs[2].Content, "y que encaje con eso") {
		t.Errorf("mensaje 2: %q", msgs[2].Content)
	}
	if !strings.Contains(msgs[2].Content, "Take a look at the attached files") {
		t.Errorf("sin texto propio se usa el mensaje por defecto: %q", msgs[2].Content)
	}
}

func TestSeedHistoryKeepsTheTailWithinBudget(t *testing.T) {
	var stored []StoredMessage
	for i := 0; i < 20; i++ {
		stored = append(stored, StoredMessage{Role: "user", Content: strings.Repeat("a", 1000)}, StoredMessage{Role: "assistant", Content: strings.Repeat("b", 1000)})
	}
	msgs := SeedHistory(stored, 5000)
	total := 0
	for _, m := range msgs {
		total += len(m.Content)
	}
	if len(msgs) == 0 || total > 6000 || msgs[0].Role != llm.RoleUser || msgs[len(msgs)-1].Role != llm.RoleAssistant {
		t.Errorf("%d mensajes, %d caracteres, primero %s", len(msgs), total, msgs[0].Role)
	}
	if SeedHistory(nil, 1000) != nil {
		t.Error("sin historial no hay mensajes")
	}
}

// Un paso sin imágenes se guarda con "images":[] y no null: la interfaz lo lee
// como lista y un null tumbaba la conversación al reabrirla (prueba de humo S12).
func TestEncodePartsStepWithoutImagesIsEmptyList(t *testing.T) {
	raw := EncodeParts(Parts{Steps: []Step{{ID: "s1", Tool: "render", Status: "ok"}}})
	if !strings.Contains(raw, `"images":[]`) || strings.Contains(raw, `"images":null`) {
		t.Fatalf("parts = %s", raw)
	}
}
