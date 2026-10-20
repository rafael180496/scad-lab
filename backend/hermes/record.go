package hermes

import (
	"encoding/json"
	"regexp"
	"strings"

	"scadlab/backend/chat"
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/mcp"
)

// Registro de lo que pasó en un turno, para guardarlo, y la operación inversa:
// armar el contexto de un modelo desde lo guardado.
//
// En ScadLab no hay un historial en memoria que sobreviva entre turnos (como el
// `llmHistory` de mini-tools): cada turno arma el contexto desde las filas de
// chat_messages. Por eso retomar una conversación tras reiniciar la app y
// seguirla en la misma sesión son el mismo camino —SeedHistory— y no hay un
// caso especial que pueda desincronizarse.

// Parts es el JSON que se guarda en chat_messages.parts. En un mensaje del
// usuario trae Attachments; en uno del asistente, Steps (y Model/StepLimit).
// Un mensaje anterior a Hermes tiene parts vacío.
type Parts struct {
	Attachments []AttachmentMeta `json:"attachments,omitempty"`
	Steps       []Step           `json:"steps,omitempty"`
	Model       string           `json:"model,omitempty"`
	StepLimit   bool             `json:"stepLimit,omitempty"`
	// Usage es el uso del turno con su tiempo y la ocupación del contexto (S17):
	// el pie del turno lo lee de acá. Un mensaje anterior no lo trae.
	Usage *Usage `json:"usage,omitempty"`
}

// AttachmentMeta es lo que se guarda de un adjunto de un mensaje: nunca los
// bytes. Thumb (miniatura de ~96 px) hace que el historial siga mostrando la
// imagen que se mandó; Summary es el informe de un modelo de referencia (el
// mismo texto que viajó al modelo), para que un turno posterior sepa qué había
// en refs/.
type AttachmentMeta struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Mime    string `json:"mime,omitempty"`
	Size    int64  `json:"size"`
	RefPath string `json:"refPath,omitempty"`
	Summary string `json:"summary,omitempty"`
	Thumb   string `json:"thumb,omitempty"`
}

// EncodeParts serializa los Parts; vacíos dan "".
func EncodeParts(p Parts) string {
	if len(p.Attachments) == 0 && len(p.Steps) == 0 && p.Model == "" && !p.StepLimit && p.Usage == nil {
		return ""
	}
	// Un slice nil viaja como null y la interfaz lo lee como lista: los pasos
	// sin imagen se guardan con [] (la interfaz además normaliza al leer).
	steps := make([]Step, len(p.Steps))
	for i, st := range p.Steps {
		if st.Images == nil {
			st.Images = []string{}
		}
		steps[i] = st
	}
	p.Steps = steps
	raw, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(raw)
}

// ParseParts lee los Parts de un mensaje; un texto vacío o corrupto es "sin
// partes" (un mensaje viejo no debe romper la conversación).
func ParseParts(s string) Parts {
	var p Parts
	if strings.TrimSpace(s) == "" {
		return p
	}
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return Parts{}
	}
	return p
}

// StepsForStorage devuelve los pasos listos para guardar: sus imágenes pasan de
// PNG completo a JPEG reducido (StepImagePx) y las que no se pueden leer se
// descartan. No toca los pasos recibidos.
func StepsForStorage(steps []Step) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		s.Images = nil
		for _, u := range steps[i].Images {
			if small := shrinkDataURL(u); small != "" {
				s.Images = append(s.Images, small)
			}
		}
		out[i] = s
	}
	return out
}

// StoredMessage es una fila de chat_messages en lo que le importa a SeedHistory.
type StoredMessage struct {
	Role    string
	Content string
	Parts   string
}

// SeedHistory arma los mensajes previos de un modelo a partir de las filas
// guardadas de un chat.
//
//   - Solo van el usuario y el asistente: los pasos de herramienta no se
//     reenvían (el modelo ve su resultado en el texto final y en los
//     checkpoints, que lista el prompt de sistema). Reenviarlos exigiría
//     guardar los bloques firmados de cada proveedor.
//   - Los adjuntos de un mensaje NO se reenvían: queda una línea con sus
//     nombres y, para un modelo de referencia, el informe que se le dio (dice
//     dónde quedó en refs/). Las imágenes se borran al día.
//   - Los mensajes consecutivos del mismo rol se funden (un turno que falló sin
//     respuesta deja dos del usuario seguidos y varias plantillas de chat
//     locales exigen que alternen) y se conserva la cola que entra en maxChars,
//     empezando por un mensaje del usuario.
//   - S17: lo que no entra no se pierde del todo. Los mensajes viejos que se
//     quedan fuera se reducen a un resumen EXTRACTIVO (qué se pidió, qué se
//     contestó y qué archivos se tocaron; sin llamar al modelo) que encabeza el
//     primer mensaje conservado. Los últimos mensajes viajan enteros.
func SeedHistory(stored []StoredMessage, maxChars int) []llm.Message {
	entries := historyEntries(stored)
	msgs := entriesToMessages(entries)
	kept := chat.TrimHistory(msgs, maxChars)
	if len(kept) == len(msgs) {
		return kept
	}
	// Algo quedó fuera: se reserva sitio para el resumen y se vuelve a recortar.
	room := summaryRoom(maxChars)
	if maxChars > room*2 {
		kept = chat.TrimHistory(msgs, maxChars-room)
	}
	dropped := entries[:len(msgs)-len(kept)]
	summary := extractiveSummary(dropped, room)
	if summary == "" || len(kept) == 0 {
		return kept
	}
	out := append([]llm.Message(nil), kept...)
	if out[0].Role == llm.RoleUser {
		out[0].Content = summary + "\n\n" + out[0].Content
		return out
	}
	return append([]llm.Message{{Role: llm.RoleUser, Content: summary}}, out...)
}

// ExtractiveSummary resume TODA la conversación guardada sin llamar al modelo (qué
// se pidió, qué se contestó, qué archivos se tocaron), en a lo sumo maxChars. Es el
// respaldo de «Nueva conversación con resumen» cuando el modelo no puede
// resumir, y lo mismo que SeedHistory antepone a lo que ya no cabe.
func ExtractiveSummary(stored []StoredMessage, maxChars int) string {
	return extractiveSummary(historyEntries(stored), maxChars)
}

// historyEntry es un mensaje del historial con lo que el resumen necesita y el
// mensaje del modelo ya no trae: los archivos que sus pasos tocaron.
type historyEntry struct {
	role    string
	content string
	files   []string
}

// historyEntries arma las entradas del historial: solo usuario y asistente, con la
// nota de los adjuntos, los consecutivos del mismo rol fundidos (como
// chat.MergeRoles) y los vacíos descartados.
func historyEntries(stored []StoredMessage) []historyEntry {
	var out []historyEntry
	for _, m := range stored {
		var e historyEntry
		switch m.Role {
		case "user":
			p := ParseParts(m.Parts)
			content := m.Content
			if strings.TrimSpace(content) == "" && len(p.Attachments) > 0 {
				content = DefaultAttachmentPrompt()
			}
			if note := AttachmentNote(p.Attachments); note != "" {
				content = withText(content, note)
			}
			e = historyEntry{role: llm.RoleUser, content: content}
		case "assistant":
			e = historyEntry{role: llm.RoleAssistant, content: m.Content, files: touchedFiles(ParseParts(m.Parts).Steps)}
		default:
			continue
		}
		if strings.TrimSpace(e.content) == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].role == e.role {
			out[n-1].content += "\n\n" + e.content
			out[n-1].files = append(out[n-1].files, e.files...)
			continue
		}
		out = append(out, e)
	}
	return out
}

func entriesToMessages(entries []historyEntry) []llm.Message {
	msgs := make([]llm.Message, len(entries))
	for i, e := range entries {
		msgs[i] = llm.Message{Role: e.role, Content: e.content}
	}
	return msgs
}

// TouchedFiles son los checkpoints que los pasos terminados bien de un turno
// crearon o cambiaron (para el resumen de un turno que no dijo nada).
func TouchedFiles(steps []Step) []string { return touchedFiles(steps) }

// touchedFiles son los checkpoints que los pasos terminados bien de un turno
// crearon o cambiaron: el `file` del resultado de una herramienta con efecto, o su
// argumento `file` cuando el resultado no lo trae.
func touchedFiles(steps []Step) []string {
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, st := range steps {
		if st.Status != StepOK || mcp.EffectOf(st.Tool) != mcp.EffectCheckpoint {
			continue
		}
		if m := resultFileRE.FindStringSubmatch(st.Text); m != nil {
			add(m[1])
		} else if f, ok := st.Args["file"].(string); ok {
			add(f)
		}
	}
	return out
}

var resultFileRE = regexp.MustCompile(`"file"\s*:\s*"([^"]+\.scad)"`)

// summaryRoom es cuántos caracteres puede ocupar el resumen dentro de un
// presupuesto de historial: la cuarta parte, entre 600 y 2400 (unos 150–600
// tokens). Un resumen que ocupa lo que ahorra no ahorra nada.
func summaryRoom(maxChars int) int {
	room := maxChars / 4
	if room < 600 {
		room = 600
	}
	if room > 2400 {
		room = 2400
	}
	return room
}

var sentenceEndRE = regexp.MustCompile(`[.!?]( |$)`)

var fencedCodeRE = regexp.MustCompile("(?s)```.*?(```|$)")

// extractiveSummary resume las entradas en a lo sumo room caracteres: para cada
// par, lo primero que se pidió y lo primero que se contestó (el código de los
// bloques se omite: está en el checkpoint) y los archivos tocados. Se llena de lo
// más reciente a lo más viejo —lo último es lo que más probablemente sigue
// importando— y se muestra en orden cronológico.
func extractiveSummary(entries []historyEntry, room int) string {
	if len(entries) == 0 || room <= 0 {
		return ""
	}
	header := i18n.T(msgSummaryHeader)
	used := len(header)
	var lines []string
	var isAnswer []bool // por línea: ¿es una respuesta?
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		var line string
		switch e.role {
		case llm.RoleUser:
			line = i18n.T(msgSummaryAsked, firstSentence(e.content, 200))
		default:
			line = i18n.T(msgSummaryAnswered, firstSentence(e.content, 200))
			if len(e.files) > 0 {
				line += " " + i18n.T(msgSummaryFiles, strings.Join(e.files, ", "))
			}
		}
		if used+len(line)+1 > room {
			if len(lines) == 0 {
				line = clipRunes(line, room-used-1)
			} else {
				break
			}
		}
		used += len(line) + 1
		lines = append(lines, line)
		isAnswer = append(isAnswer, e.role != llm.RoleUser)
	}
	if len(lines) == 0 {
		return ""
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
		isAnswer[i], isAnswer[j] = isAnswer[j], isAnswer[i]
	}
	// Una respuesta sin el pedido que la explica no dice nada: si el recorte dejó
	// una al principio, se descarta.
	if len(lines) > 1 && isAnswer[0] {
		lines = lines[1:]
	}
	return header + "\n" + strings.Join(lines, "\n")
}

// firstSentence deja la primera frase útil de un texto: sin bloques de código, en
// una línea y recortada a max caracteres.
func firstSentence(s string, max int) string {
	s = fencedCodeRE.ReplaceAllString(s, " [código] ")
	s = strings.Join(strings.Fields(s), " ")
	// El primer punto seguido de espacio: un punto dentro de «002-maceta.scad» no termina la frase.
	if loc := sentenceEndRE.FindStringIndex(s); loc != nil && loc[0] > 20 && loc[0] < max {
		s = s[:loc[0]+1]
	}
	return clipRunes(s, max)
}

var (
	msgSummaryHeader   = i18n.Msg{ES: "[Resumen automático de los mensajes anteriores de esta conversación, que ya no caben:]", EN: "[Automatic summary of the earlier messages of this conversation, which no longer fit:]"}
	msgSummaryAsked    = i18n.Msg{ES: "- Se pidió: %s", EN: "- Asked: %s"}
	msgSummaryAnswered = i18n.Msg{ES: "- Se respondió: %s", EN: "- Answered: %s"}
	msgSummaryFiles    = i18n.Msg{ES: "(archivos: %s)", EN: "(files: %s)"}
)

// DefaultAttachmentPrompt es el mensaje cuando la persona adjuntó archivos y no
// escribió nada: un turno no puede ir vacío.
func DefaultAttachmentPrompt() string {
	return i18n.T(i18n.Msg{ES: "Revisa los archivos adjuntos.", EN: "Take a look at the attached files."})
}

// AttachmentNote es lo que reemplaza a los adjuntos de un mensaje que ya no se
// reenvían: los nombres y, de un modelo de referencia, su informe.
func AttachmentNote(metas []AttachmentMeta) string {
	if len(metas) == 0 {
		return ""
	}
	var names, reports []string
	for _, m := range metas {
		name := m.Name
		// Una foto copiada al workspace (S15) dice dónde quedó: el turno siguiente
		// todavía tiene que poder pasarla a las herramientas de imagen.
		if m.Kind == KindImage && m.RefPath != "" {
			name += " (" + m.RefPath + ")"
		}
		names = append(names, name)
		if m.Kind == KindModel && m.Summary != "" {
			reports = append(reports, m.Summary)
		}
	}
	note := i18n.T(i18n.Msg{
		ES: "[Adjuntos de este mensaje, que ya no se reenvían: %s]",
		EN: "[Attachments of this message, which are no longer resent: %s]",
	}, strings.Join(names, ", "))
	if len(reports) > 0 {
		note += "\n\n" + strings.Join(reports, "\n\n")
	}
	return note
}

func withText(prompt, extra string) string {
	if extra == "" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return extra
	}
	return prompt + "\n\n" + extra
}
