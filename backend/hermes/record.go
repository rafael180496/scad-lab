package hermes

import (
	"encoding/json"
	"strings"

	"scadlab/backend/chat"
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
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
	if len(p.Attachments) == 0 && len(p.Steps) == 0 && p.Model == "" && !p.StepLimit {
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
func SeedHistory(stored []StoredMessage, maxChars int) []llm.Message {
	var msgs []llm.Message
	for _, m := range stored {
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
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: content})
		case "assistant":
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: m.Content})
		}
	}
	return chat.TrimHistory(chat.MergeRoles(msgs), maxChars)
}

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
