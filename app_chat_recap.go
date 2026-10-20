package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
)

// Resumen de un turno que terminó sin escribir nada (S17, G.3). Un modelo que usa
// herramientas y se calla —Antigravity termina el turno al primer rechazo, un modelo
// chico se queda sin contexto— deja una burbuja vacía o un «terminó sin responder
// nada» que no dice si algo quedó hecho. Este resumen cuenta qué se ejecutó, qué
// archivos cambiaron y qué devolvió la última herramienta.

var (
	msgRecapIntro = i18n.Msg{ES: "Esto es lo que hizo antes de terminar:", EN: "This is what it did before finishing:"}
	msgRecapTools = i18n.Msg{ES: "- Herramientas: %s", EN: "- Tools: %s"}
	msgRecapFiles = i18n.Msg{ES: "- Quedó hecho: se creó o se cambió %s", EN: "- Done: created or changed %s"}
	msgRecapNone  = i18n.Msg{ES: "- No quedó ningún checkpoint creado ni cambiado.", EN: "- No checkpoint was created or changed."}
	msgRecapLast  = i18n.Msg{ES: "- Último resultado (`%s`): %s", EN: "- Last result (`%s`): %s"}
	// La shell: Antigravity la bloquea en el chat (--print) y el turno termina.
	msgCLIAgyShell = i18n.Msg{
		ES: "Antigravity intentó ejecutar un comando de shell (%s) y lo bloqueó: en el chat de ScadLab la shell no se permite y, cuando se intenta, Antigravity termina el turno. No se ejecutó nada. Vuelve a pedirlo indicando que use las herramientas de ScadLab (`edit_checkpoint`, `set_parameters`, `render`…), o elige otro proveedor en la barra del chat.",
		EN: "Antigravity tried to run a shell command (%s) and blocked it: the shell is not allowed in ScadLab's chat and, when it is attempted, Antigravity ends the turn. Nothing was run. Ask again telling it to use ScadLab's tools (`edit_checkpoint`, `set_parameters`, `render`…), or pick another provider in the chat bar.",
	}
)

// turnRecap devuelve el resumen en Markdown de los pasos de un turno, o "" si no
// hubo ninguno (no hay nada que contar). skipResult (puede ser nil) descarta los
// pasos cuyo resultado ya explica otra nota del mensaje —un rechazo de permisos— para
// no repetirlo.
func turnRecap(steps []hermes.Step, skipResult func(hermes.Step) bool) string {
	if len(steps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(i18n.T(msgRecapIntro))
	b.WriteString("\n" + i18n.T(msgRecapTools, recapTools(steps)))
	if files := hermes.TouchedFiles(steps); len(files) > 0 {
		quoted := make([]string, len(files))
		for i, f := range files {
			quoted[i] = "`" + f + "`"
		}
		b.WriteString("\n" + i18n.T(msgRecapFiles, strings.Join(quoted, ", ")))
	} else {
		b.WriteString("\n" + i18n.T(msgRecapNone))
	}
	for i := len(steps) - 1; i >= 0; i-- {
		st := steps[i]
		if strings.TrimSpace(st.Text) == "" || (skipResult != nil && skipResult(st)) {
			continue
		}
		b.WriteString("\n" + i18n.T(msgRecapLast, st.Tool, summarizeToolResult(st.Text)))
		break
	}
	return b.String()
}

// recapTools lista las herramientas del turno en orden, agrupando las repetidas
// seguidas («render ×2») y marcando las que fallaron.
func recapTools(steps []hermes.Step) string {
	type run struct {
		name   string
		n      int
		failed bool
	}
	var runs []run
	for _, st := range steps {
		failed := st.Status == hermes.StepError
		if n := len(runs); n > 0 && runs[n-1].name == st.Tool && runs[n-1].failed == failed {
			runs[n-1].n++
			continue
		}
		runs = append(runs, run{name: st.Tool, n: 1, failed: failed})
	}
	const maxRuns = 10
	parts := make([]string, 0, len(runs))
	for i, r := range runs {
		if i == maxRuns {
			parts = append(parts, fmt.Sprintf("… (+%d)", len(runs)-maxRuns))
			break
		}
		s := "`" + r.name + "`"
		if r.n > 1 {
			s += fmt.Sprintf(" ×%d", r.n)
		}
		if r.failed {
			s += " ✗"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " → ")
}

// summaryKeys son, en orden, los campos de un resultado JSON que se muestran en el
// resumen: lo que dice si salió bien, de qué archivo habla y la medida más útil.
var summaryKeys = []string{"error", "status", "file", "updated", "created", "changes", "fitsBuildVolume", "watertight", "triangles", "lines", "bytes", "message"}

// summarizeToolResult resume el resultado de una herramienta en una línea: sus
// campos más útiles si es un objeto JSON y, si no, el comienzo del texto.
func summarizeToolResult(text string) string {
	text = strings.TrimSpace(text)
	var obj map[string]any
	if strings.HasPrefix(text, "{") && json.Unmarshal([]byte(text), &obj) == nil {
		var parts []string
		for _, k := range summaryKeys {
			v, ok := obj[k]
			if !ok {
				continue
			}
			switch x := v.(type) {
			case string:
				parts = append(parts, k+": "+clipText(oneLine(x), 120))
			case bool, float64:
				parts = append(parts, fmt.Sprintf("%s: %v", k, x))
			}
			if len(parts) == 5 {
				break
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 6 {
			keys = keys[:6]
		}
		return "{" + strings.Join(keys, ", ") + "}"
	}
	return clipText(oneLine(text), 240)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// agyShellRe lee el comando que Antigravity se negó a ejecutar en su rechazo
// («user denied permission to run command:\n<comando>»).
var agyShellRe = regexp.MustCompile(`(?s)user denied permission to run command:\s*\n?(.*?)(?:\nDo not attempt|$)`)

// agyShellDenial devuelve el primer comando de shell que Antigravity bloqueó en el
// turno (run_command rechazado por permisos). ok=false si ninguno.
func agyShellDenial(steps []hermes.Step) (command string, ok bool) {
	for _, st := range steps {
		if st.Status != hermes.StepError || !isShellTool(st.Tool) {
			continue
		}
		// Solo el rechazo de permisos de Antigravity (el texto real de su --print): otro
		// fallo de la shell no es «la bloqueó».
		m := agyShellRe.FindStringSubmatch(st.Text)
		if m == nil && !strings.Contains(st.Text, "permission check failed") {
			continue
		}
		if m != nil {
			if c := strings.TrimSpace(m[1]); c != "" {
				return clipText(oneLine(c), 120), true
			}
		}
		if c, _ := st.Args["CommandLine"].(string); c != "" {
			return clipText(oneLine(c), 120), true
		}
		return "?", true
	}
	return "", false
}

// isShellTool: las herramientas con que un CLI corre comandos.
func isShellTool(name string) bool {
	switch strings.ToLower(name) {
	case "run_command", "runcommand", "bash", "shell", "command_execution":
		return true
	}
	return false
}
