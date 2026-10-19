package hermes

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/refmodel"
)

// Adjuntos del chat: imágenes (PNG, JPG, GIF, WebP) y modelos 3D de referencia
// (STL, 3MF, OBJ). Es el attachments.go de mini-tools sin PDF ni texto pegado,
// con los modelos de referencia en su lugar.
//
// # Cómo llega cada tipo al modelo
//
//	imagen   base64 en el request (llm.Message.Images son rutas: se guardan en
//	         <datos>/chat-attachments/<chatId>/). Si el modelo no ve, se rechaza.
//	modelo   se copia a <workspace>/refs/ (para `import("refs/x.stl")`), se
//	         analiza en Go (medidas, volumen, partes) y el informe entra al
//	         mensaje como texto. Su vista previa se manda como imagen solo si el
//	         modelo ve; si no, basta el informe.
//
// # El tipo se decide por el contenido
//
// Una imagen se reconoce mirando sus primeros bytes, no el nombre: un `.stl`
// que en realidad es un PNG es una imagen. Un modelo se reconoce por su
// extensión (el STL no tiene firma fiable) más una comprobación de la firma del
// 3MF (un zip), y lo valida a fondo refmodel.Analyze.

// Límites. El de imagen es el de la API de Anthropic; el de modelo, el de
// refmodel.
const (
	MaxImageBytes  = 5 << 20
	MaxModelBytes  = refmodel.MaxBytes
	MaxAttachments = 10
)

// Tipos de adjunto.
const (
	KindImage = "image"
	KindModel = "model"
)

// Attachment es un adjunto ya guardado, tal como lo ve la interfaz. Los nombres
// JSON son contrato con frontend/src/lib/chatAttachments.ts.
type Attachment struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	// ThumbDataURL es la miniatura del chip: la imagen reducida, o la vista
	// previa de un modelo. "" si no hay (una imagen WebP).
	ThumbDataURL string `json:"thumbDataUrl"`
	// RefPath es la ruta relativa al workspace de un modelo ("refs/pieza.stl").
	RefPath string `json:"refPath"`
	// Summary es el informe del modelo (medidas, partes), el que entra al mensaje.
	Summary string `json:"summary"`
}

// ImageExt es la extensión de una imagen por su tipo MIME; "" si no se admite.
func ImageExt(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}

var modelMime = map[string]string{".stl": "model/stl", ".3mf": "model/3mf", ".obj": "model/obj"}

// Classify decide qué es el archivo mirando su comienzo (hasta 512 bytes, lo que
// mira http.DetectContentType) y su nombre.
func Classify(name string, head []byte) (kind, mime string, err error) {
	ct, _, _ := strings.Cut(http.DetectContentType(head), ";")
	switch {
	case ImageExt(ct) != "":
		return KindImage, ct, nil
	case strings.HasPrefix(ct, "image/"):
		return "", "", i18n.Errorf(i18n.Msg{
			ES: "%q: formato de imagen no soportado (%s); usa PNG, JPG, GIF o WebP",
			EN: "%q: unsupported image format (%s); use PNG, JPG, GIF or WebP",
		}, name, ct)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if m, ok := modelMime[ext]; ok {
		if ext == ".3mf" && !(len(head) >= 4 && string(head[:4]) == "PK\x03\x04") {
			return "", "", i18n.Errorf(i18n.Msg{ES: "%q no es un 3MF válido (un 3MF es un archivo comprimido)", EN: "%q is not a valid 3MF (a 3MF is a compressed file)"}, name)
		}
		return KindModel, m, nil
	}
	return "", "", i18n.Errorf(i18n.Msg{
		ES: "%q no se puede adjuntar: solo imágenes PNG/JPG/GIF/WebP y modelos 3D STL, 3MF u OBJ",
		EN: "%q cannot be attached: only PNG/JPG/GIF/WebP images and STL, 3MF or OBJ 3D models",
	}, name)
}

// LimitFor es el tope de tamaño de un tipo.
func LimitFor(kind string) int64 {
	if kind == KindModel {
		return MaxModelBytes
	}
	return MaxImageBytes
}

// ErrTooBig es el error de un adjunto que pasa el tope de su tipo.
func ErrTooBig(name, kind string, limit int64) error {
	what := i18n.Msg{ES: "imagen", EN: "image"}
	if kind == KindModel {
		what = i18n.Msg{ES: "modelo 3D", EN: "3D model"}
	}
	return i18n.Errorf(i18n.Msg{
		ES: "%q supera el tamaño máximo de un adjunto de tipo %s (%s)",
		EN: "%q exceeds the maximum size of a %s attachment (%s)",
	}, name, i18n.T(what), FormatBytes(limit))
}

// FormatBytes escribe un tamaño para un mensaje.
func FormatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.4g MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.4g KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// CleanName deja de un nombre solo lo que se puede mostrar y poner en un nombre
// de archivo: sin directorios (se toma lo que sigue al último separador, de
// cualquiera de los dos sistemas), sin caracteres de control, acotado.
func CleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == utf8.RuneError {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(strings.Trim(b.String(), ". "))
	if r := []rune(out); len(r) > 100 {
		out = string(r[:100])
	}
	if out == "" {
		return "adjunto"
	}
	return out
}

// FSName es el nombre apto para el disco: letras, números y . _ - y espacio.
func FSName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r), r == '.', r == '-', r == ' ':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// NewID es un identificador de adjunto.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "att-" + hex.EncodeToString(b[:])
}

// --- Registro de adjuntos pendientes ---------------------------------------------

// Pending es un adjunto elegido pero todavía no enviado.
type Pending struct {
	Attachment
	WorkspaceID int64
	// Staged es el archivo de una imagen en la carpeta de preparación; "" en un
	// modelo (ya está en refs/).
	Staged string
	// RefAbs es la ruta absoluta de la copia en refs/ de un modelo, y Owned dice
	// que la creó este adjunto (quitarlo antes de enviar la borra; nunca se borra
	// una referencia que ya existía).
	RefAbs string
	Owned  bool
	// Preview es el PNG de la vista previa de un modelo (para mandarlo si el
	// modelo ve).
	Preview []byte
	At      time.Time
}

// Registry guarda los adjuntos pendientes por id. Es seguro entre goroutines.
type Registry struct {
	mu sync.Mutex
	m  map[string]*Pending
}

// NewRegistry crea un registro vacío.
func NewRegistry() *Registry { return &Registry{m: map[string]*Pending{}} }

// Add registra un adjunto.
func (r *Registry) Add(p *Pending) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p.At = time.Now()
	r.m[p.ID] = p
}

// Get devuelve uno sin quitarlo.
func (r *Registry) Get(id string) (*Pending, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.m[id]
	return p, ok
}

// Take lo quita del registro y lo devuelve.
func (r *Registry) Take(id string) (*Pending, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.m[id]
	delete(r.m, id)
	return p, ok
}

// Expired quita y devuelve los pendientes más viejos que ttl.
func (r *Registry) Expired(ttl time.Duration) []*Pending {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Pending
	for id, p := range r.m {
		if time.Since(p.At) > ttl {
			out = append(out, p)
			delete(r.m, id)
		}
	}
	return out
}

// Drain quita y devuelve todos los pendientes (al cerrar la app).
func (r *Registry) Drain() []*Pending {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Pending, 0, len(r.m))
	for id, p := range r.m {
		out = append(out, p)
		delete(r.m, id)
	}
	return out
}

// Discard borra lo que un adjunto pendiente dejó en el disco: la imagen en
// preparación y la copia en refs/ si la creó él.
func (p *Pending) Discard() {
	if p.Staged != "" {
		_ = os.Remove(p.Staged)
	}
	if p.Owned && p.RefAbs != "" {
		_ = os.Remove(p.RefAbs)
	}
}

// --- Preparar el envío --------------------------------------------------------

// Prepared es el reparto de los adjuntos de un mensaje.
type Prepared struct {
	// Text es el mensaje que lee el modelo: lo que escribió la persona más el
	// informe de cada modelo de referencia.
	Text string
	// Images son las rutas de las imágenes que viajan (las adjuntas y la vista
	// previa de cada modelo, esta última solo si el modelo ve).
	Images []string
	// Metas es lo que se guarda en los Parts del mensaje.
	Metas []AttachmentMeta
}

// CheckVision rechaza el envío de imágenes a un modelo que informó que no las
// ve. Con visionKnown en false (un servidor genérico que no lo dice) nunca
// bloquea: es mejor dejar que el servidor conteste que negarle a la persona algo
// que quizá funcione. who nombra al destino («Ollama · llama3.2»).
func CheckVision(atts []*Pending, vision, visionKnown bool, who string) error {
	if vision || !visionKnown {
		return nil
	}
	for _, a := range atts {
		if a.Kind == KindImage {
			return i18n.Errorf(i18n.Msg{
				ES: "%s no ve imágenes (%q): elige un modelo con visión o quita el adjunto",
				EN: "%s cannot see images (%q): choose a model with vision or remove the attachment",
			}, who, a.Name)
		}
	}
	return nil
}

// Prepare arma el mensaje con los adjuntos: mueve las imágenes en preparación a
// dir (la carpeta del chat), escribe la vista previa de los modelos si el modelo
// ve, y reúne el texto. Llamarla consume los archivos en preparación.
func Prepare(text string, atts []*Pending, vision bool, dir string) (Prepared, error) {
	var p Prepared
	if len(atts) > MaxAttachments {
		return p, i18n.Errorf(i18n.Msg{ES: "se pueden adjuntar hasta %d archivos por mensaje (llegaron %d)", EN: "up to %d files can be attached per message (got %d)"}, MaxAttachments, len(atts))
	}
	if len(atts) > 0 {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return p, i18n.Errorf(i18n.Msg{ES: "no se pudo preparar la carpeta de adjuntos: %w", EN: "could not prepare the attachments folder: %w"}, err)
		}
	}
	var reports []string
	for _, a := range atts {
		meta := AttachmentMeta{Kind: a.Kind, Name: a.Name, Mime: a.Mime, Size: a.Size, RefPath: a.RefPath, Summary: a.Summary, Thumb: a.ThumbDataURL}
		switch a.Kind {
		case KindImage:
			dst := filepath.Join(dir, filepath.Base(a.Staged))
			if err := moveFile(a.Staged, dst); err != nil {
				return p, i18n.Errorf(i18n.Msg{ES: "el adjunto %q ya no está (se borra al cabo de un día); vuelve a adjuntarlo", EN: "the attachment %q is gone (they are deleted after a day); attach it again"}, a.Name)
			}
			p.Images = append(p.Images, dst)
		case KindModel:
			if a.Summary != "" {
				reports = append(reports, a.Summary)
			}
			if vision && len(a.Preview) > 0 {
				path := filepath.Join(dir, "ref-"+a.ID+".png")
				if err := os.WriteFile(path, a.Preview, 0o600); err == nil {
					p.Images = append(p.Images, path)
				}
			}
		}
		p.Metas = append(p.Metas, meta)
	}
	text = strings.TrimSpace(text)
	if text == "" && len(atts) > 0 {
		text = DefaultAttachmentPrompt()
	}
	p.Text = withText(text, strings.Join(reports, "\n\n"))
	return p, nil
}

// moveFile mueve un archivo; si rename falla (otro volumen) lo copia y borra el
// original.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return err
	}
	return os.Remove(src)
}

// PruneDir borra de dir los archivos con más de ttl. Se llama al guardar uno
// nuevo en vez de con una tarea de fondo: es el único momento en que esa carpeta
// crece, y un recolector aparte sería una pieza más que puede fallar sola.
func PruneDir(dir string, ttl time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-ttl)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// UserMessage arma el mensaje de usuario del turno con lo que preparó Prepare.
func UserMessage(p Prepared) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: p.Text, Images: p.Images}
}
