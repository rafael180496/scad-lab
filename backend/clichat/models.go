package clichat

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"scadlab/backend/i18n"
)

// Los modelos de cada CLI. Es el backend/agentmodels de mini-tools portado.
//
// # Por qué no está escrita la lista
//
// Un campo de texto libre para el modelo no ayuda a elegir (hay que saberse los
// ids de memoria) y una lista fija envejece con cada versión de cada proveedor.
// La salida es sacar la lista de cada CLI en vez de escribirla acá:
//
//   - **Claude Code**: sus propios ALIAS (`opus`, `sonnet`, `haiku`, `fable`),
//     documentados en su `--model` como «un alias del último modelo». Un alias no
//     envejece —esa es su razón de existir—, así que es lo único que se escribe a
//     mano, y a propósito.
//   - **Codex**: `~/.codex/models_cache.json`, que el propio CLI mantiene, con
//     nombre y descripción.
//   - **Antigravity**: `agy models`, que los lista.
//   - **OpenCode**: su catálogo `~/.cache/opencode/models.json`, recortado a los
//     proveedores con credenciales en `~/.local/share/opencode/auth.json` (de ese
//     archivo solo se leen los NOMBRES de los proveedores, nunca sus valores).
//
// Si algo falla se devuelve una lista vacía: el turno corre igual con el modelo
// que el CLI tenga configurado, y la opción «Por defecto» (que no es parte de esta
// lista: la pone quien la muestra) siempre está.

// Model es una opción del selector de un CLI.
type Model struct {
	// ID es lo que se le pasa al CLI en `--model`.
	ID    string
	Label string
	// Description es la línea de ayuda: para qué sirve ese modelo.
	Description string
}

// claudeAliases son los alias documentados por Claude Code en su propio `--model`.
func claudeAliases() []Model {
	return []Model{
		{ID: "opus", Label: "Opus", Description: i18n.T(i18n.Msg{ES: "El más capaz para tareas complejas y largas", EN: "The most capable, for long and complex tasks"})},
		{ID: "sonnet", Label: "Sonnet", Description: i18n.T(i18n.Msg{ES: "Equilibrio entre capacidad y velocidad para el día a día", EN: "A balance of capability and speed for everyday work"})},
		{ID: "haiku", Label: "Haiku", Description: i18n.T(i18n.Msg{ES: "El más rápido, para respuestas cortas", EN: "The fastest, for short answers"})},
		{ID: "fable", Label: "Fable", Description: i18n.T(i18n.Msg{ES: "Para lo más difícil y de más aliento", EN: "For the hardest and longest-running work"})},
	}
}

// modelsTTL evita preguntarle al CLI en cada apertura del selector. Los modelos de
// un proveedor no cambian en una sesión de trabajo, y `agy models` sale a la red.
const modelsTTL = 10 * time.Minute

// agyModelsTimeout acota `agy models`: que el selector tarde en abrir por listar
// modelos sería peor que no listarlos.
const agyModelsTimeout = 15 * time.Second

var (
	modelsMu    sync.Mutex
	modelsCache = map[string]modelsEntry{}

	// homeDir es el directorio del usuario (variable para las pruebas, que nunca
	// leen el real).
	homeDir = os.UserHomeDir
	// agyModelsExec lista los modelos de Antigravity; variable para las pruebas.
	agyModelsExec = runAgyModels
)

type modelsEntry struct {
	models []Model
	at     time.Time
}

// Models devuelve los modelos de un CLI, sin la opción «Por defecto». refresh se
// salta la caché (el botón de refrescar del selector).
func Models(cliID string, refresh bool) []Model {
	key := cliID + "|" + i18n.Lang()
	modelsMu.Lock()
	if e, ok := modelsCache[key]; ok && !refresh && time.Since(e.at) < modelsTTL {
		modelsMu.Unlock()
		return append([]Model(nil), e.models...)
	}
	modelsMu.Unlock()

	list := buildModels(cliID)

	modelsMu.Lock()
	modelsCache[key] = modelsEntry{models: list, at: time.Now()}
	modelsMu.Unlock()
	return append([]Model(nil), list...)
}

// ResetModelsCache olvida las listas (las pruebas).
func ResetModelsCache() {
	modelsMu.Lock()
	modelsCache = map[string]modelsEntry{}
	modelsMu.Unlock()
}

func buildModels(cliID string) []Model {
	switch cliID {
	case "claude":
		return claudeAliases()
	case "codex":
		return codexModels()
	case "antigravity":
		// Sin niveles de esfuerzo aparte: sus modelos YA lo llevan en el nombre
		// (`gemini-3.7-flash-high`, `-medium`, `-low`).
		return agyModelsExec()
	case "opencode":
		return opencodeModels()
	}
	return nil
}

// codexModels lee el cache que el propio CLI mantiene.
func codexModels() []Model {
	home, err := homeDir()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(home, ".codex", "models_cache.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	out := make([]Model, 0, len(doc.Models))
	for _, m := range doc.Models {
		// Un modelo oculto (`hide`) es uno que el propio Codex no ofrece.
		if m.Slug == "" || !ValidModel(m.Slug) || m.Visibility == "hide" {
			continue
		}
		out = append(out, Model{ID: m.Slug, Label: firstNonEmpty(m.DisplayName, m.Slug), Description: m.Description})
	}
	return out
}

// runAgyModels le pregunta a Antigravity. Formato: «<slug>\t<nombre legible>»;
// una línea sin tabulador es ruido del CLI («Fetching available models…»).
func runAgyModels() []Model {
	exe := Resolve("antigravity")
	if exe == "" {
		return nil
	}
	argv := Launcher(exe)
	ctx, cancel := context.WithTimeout(context.Background(), agyModelsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], append(argv[1:], "models")...)
	cmd.Env = Env(exe)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseAgyModels(string(out))
}

func parseAgyModels(out string) []Model {
	var models []Model
	for _, line := range strings.Split(out, "\n") {
		slug, label, ok := strings.Cut(strings.TrimSpace(line), "\t")
		slug = strings.TrimSpace(slug)
		if !ok || slug == "" || !ValidModel(slug) {
			continue
		}
		models = append(models, Model{ID: slug, Label: firstNonEmpty(strings.TrimSpace(label), slug)})
	}
	return models
}

// opencodeModels lee el catálogo que el propio CLI descarga y lo recorta a los
// proveedores en los que hay una sesión iniciada: el catálogo trae cientos de
// proveedores, y ofrecer un modelo sin credenciales es ofrecer un turno que va a
// fallar. Los usados hace poco van primero, en ese orden, que es como el CLI los
// presenta.
func opencodeModels() []Model {
	home, err := homeDir()
	if err != nil {
		return nil
	}
	return opencodeModelsFrom(
		filepath.Join(home, ".cache", "opencode", "models.json"),
		filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		filepath.Join(home, ".local", "state", "opencode", "model.json"),
	)
}

func opencodeModelsFrom(catalogPath, authPath, statePath string) []Model {
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil
	}
	var providers map[string]struct {
		Name   string `json:"name"`
		Models map[string]struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil
	}

	// Proveedores habilitados: los que tienen credenciales guardadas. Solo se
	// leen las CLAVES del archivo; los valores son secretos y no se tocan (se
	// decodifican como RawMessage y se descartan sin mirarlos).
	enabled := map[string]bool{}
	if raw, err := os.ReadFile(authPath); err == nil {
		var auth map[string]json.RawMessage
		if json.Unmarshal(raw, &auth) == nil {
			for k := range auth {
				enabled[k] = true
			}
		}
	}
	// Los recientes también cuentan como habilitados: un proveedor gratuito
	// (OpenCode Zen) se usa sin credenciales guardadas.
	var recent []string
	if raw, err := os.ReadFile(statePath); err == nil {
		var st struct {
			Recent []struct {
				ProviderID string `json:"providerID"`
				ModelID    string `json:"modelID"`
			} `json:"recent"`
		}
		if json.Unmarshal(raw, &st) == nil {
			for _, r := range st.Recent {
				if r.ProviderID != "" && r.ModelID != "" {
					enabled[r.ProviderID] = true
					recent = append(recent, r.ProviderID+"/"+r.ModelID)
				}
			}
		}
	}
	if len(enabled) == 0 {
		return nil
	}

	byID := map[string]Model{}
	var ids []string
	for pid, p := range providers {
		if !enabled[pid] {
			continue
		}
		pname := firstNonEmpty(p.Name, pid)
		for mid, m := range p.Models {
			id := pid + "/" + mid
			if !ValidModel(id) {
				continue
			}
			byID[id] = Model{ID: id, Label: firstNonEmpty(m.Name, mid), Description: pname}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	out := make([]Model, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range append(recent, ids...) {
		m, ok := byID[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, m)
	}
	return out
}
