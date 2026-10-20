package clichat

import (
	"slices"
	"strings"
	"testing"
)

func TestModeToolSets(t *testing.T) {
	ask, edit := scadlabTools(ModeAsk), scadlabTools(ModeEdit)
	for _, w := range WriteTools() {
		if slices.Contains(ask, w) {
			t.Errorf("«Solo consulta» no incluye %s", w)
		}
		if !slices.Contains(edit, w) {
			t.Errorf("«Agente» incluye %s", w)
		}
	}
	for _, r := range ReadTools() {
		if !slices.Contains(ask, r) || !slices.Contains(edit, r) {
			t.Errorf("la herramienta de lectura %s va en los dos modos", r)
		}
	}
	// Las listas no se pisan.
	for _, w := range WriteTools() {
		if slices.Contains(ReadTools(), w) {
			t.Errorf("%s no puede ser de lectura y de escritura", w)
		}
	}
	// ReadTools devuelve una copia.
	r := ReadTools()
	r[0] = "otra"
	if ReadTools()[0] == "otra" {
		t.Error("copia")
	}
}

func TestClaudeAndCodexRules(t *testing.T) {
	if got := claudeAllowed(ModeAsk); strings.Contains(got, "create_checkpoint") || !strings.Contains(got, "mcp__scadlab__render") {
		t.Errorf("%q", got)
	}
	if got := claudeDenied(ModeEdit); got != "Bash,PowerShell" {
		t.Errorf("la shell se niega siempre: %q", got)
	}
	// «Agente» nombra TODAS las herramientas, las de escritura incluidas.
	if got := codexEnabledTools(ModeEdit); !strings.Contains(got, `"create_checkpoint"`) || !strings.Contains(got, `"import_mesh"`) || !strings.Contains(got, `"render"`) {
		t.Errorf("%q", got)
	}
	if got := codexEnabledTools(ModeAsk); !strings.HasPrefix(got, `["list_checkpoints"`) || strings.Contains(got, "export") {
		t.Errorf("%q", got)
	}
}

// update_checkpoint (S16) es de escritura: solo «Agente» la permite en los CLIs que
// aprueban por lista (Claude `--allowedTools`, Codex `enabled_tools`).
func TestUpdateCheckpointIsAnAgentOnlyTool(t *testing.T) {
	if !slices.Contains(WriteTools(), "update_checkpoint") || slices.Contains(ReadTools(), "update_checkpoint") {
		t.Fatal("update_checkpoint va en writeTools y no en readTools")
	}
	if got := claudeAllowed(ModeEdit); !strings.Contains(","+got+",", ",mcp__scadlab__update_checkpoint,") {
		t.Errorf("Claude en «Agente»: %q", got)
	}
	if got := claudeAllowed(ModeAsk); strings.Contains(got, "update_checkpoint") {
		t.Errorf("Claude en «Solo consulta»: %q", got)
	}
	if got := codexEnabledTools(ModeEdit); !strings.Contains(got, `"update_checkpoint"`) {
		t.Errorf("Codex en «Agente»: %q", got)
	}
	if got := codexEnabledTools(ModeAsk); strings.Contains(got, "update_checkpoint") {
		t.Errorf("Codex en «Solo consulta»: %q", got)
	}
	// Y no puede reemplazar a la shell: la edición de archivos del CLI sigue
	// denegada en «Solo consulta» y la shell en los dos modos.
	if got := claudeDenied(ModeAsk); !strings.Contains(got, "Edit") || !strings.Contains(got, "Bash") {
		t.Errorf("denegadas en consulta: %q", got)
	}
}
