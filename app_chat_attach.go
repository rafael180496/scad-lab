package main

import (
	"context"
	"encoding/base64"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/refmodel"
	"scadlab/backend/store"
)

// Adjuntos del chat (S12): imágenes (pegadas, arrastradas o elegidas) y modelos
// 3D de referencia (STL, 3MF, OBJ). La clasificación, los límites y el reparto
// viven en backend/hermes/attachments.go; aquí se conecta con el workspace, el
// directorio de datos y el modelo elegido.
//
// Un adjunto vive en dos tiempos:
//
//	elegido    AddChatAttachment*: la imagen se guarda en <datos>/chat-attachments/
//	           staging/; un modelo se copia a <workspace>/refs/ y se analiza (el
//	           chip muestra su vista previa). Todavía no es parte de ningún mensaje.
//	enviado    SendChatMessage lo mueve a <datos>/chat-attachments/<chatId>/ y lo
//	           pasa al modelo; de ahí en adelante solo queda su ficha (nombre, tamaño,
//	           miniatura, informe, refPath) en los Parts del mensaje. Una imagen,
//	           además, se copia a <workspace>/refs/images/<nombre>-<hash8>.<ext>
//	           (S15; sin pisar, deduplicada por contenido): ahí la abren las
//	           herramientas de imagen, y esa ruta es la que ve el modelo.
//
// Quitar el chip antes de enviar borra lo que se creó (la copia en refs/ solo si
// la hizo este adjunto). Lo que quede de una sesión anterior se barre al
// primer uso y al cabo de un día.

const (
	chatAttachTTL = 24 * time.Hour
	// attachCapsTimeout acota la consulta de capacidades al proveedor: corre al
	// enviar, y un servidor remoto lento no debe dejar el envío colgado.
	attachCapsTimeout = 10 * time.Second
	// maxAttachDataBytes acota lo que se acepta por AddChatAttachmentData
	// (base64 de un modelo de 64 MB pesa ~86 MB).
	maxAttachDataBase64 = hermes.MaxModelBytes/3*4 + 4096
)

// chatRoot es la carpeta de los adjuntos del chat. "" sin carpeta de datos.
func (a *App) chatRoot() string {
	if a.dataDir == "" {
		return ""
	}
	return filepath.Join(a.dataDir, "chat-attachments")
}

// chatDir es la carpeta de los adjuntos y las imágenes de herramientas de un
// chat: <datos>/chat-attachments/<chatId>/.
func (a *App) chatDir(chatID int64) string {
	if root := a.chatRoot(); root != "" {
		return filepath.Join(root, strconv.FormatInt(chatID, 10))
	}
	return ""
}

// stagingDir es donde esperan las imágenes elegidas y todavía no enviadas.
func (a *App) stagingDir() string {
	if root := a.chatRoot(); root != "" {
		return filepath.Join(root, "staging")
	}
	return ""
}

// prepareStaging crea la carpeta de preparación y, la primera vez de la sesión,
// borra lo que quedó de antes: los adjuntos pendientes viven en memoria y se
// pierden al cerrar la app, así que sus archivos ya no son de nadie.
func (a *App) prepareStaging() (string, error) {
	dir := a.stagingDir()
	if dir == "" {
		return "", i18n.New(i18n.Msg{ES: "el directorio de datos no está disponible", EN: "the data directory is not available"})
	}
	svc := a.chatSvc()
	svc.pruneOnce.Do(func() { _ = os.RemoveAll(dir) })
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", i18n.Errorf(i18n.Msg{ES: "no se pudo preparar la carpeta de adjuntos: %w", EN: "could not prepare the attachments folder: %w"}, err)
	}
	// Lo pendiente que lleva más de un día sin enviarse se descarta.
	for _, p := range svc.attachments.Expired(chatAttachTTL) {
		p.Discard()
	}
	return dir, nil
}

// releaseAttachments quita del registro los adjuntos que ya viajaron en un
// mensaje (sus archivos pasaron a la carpeta del chat o a refs/).
func (s *chatService) releaseAttachments(ids []string) {
	for _, id := range ids {
		s.attachments.Take(id)
	}
}

// discardAllAttachments borra lo que dejaron los adjuntos pendientes al cerrar
// la app.
func (s *chatService) discardAllAttachments() {
	for _, p := range s.attachments.Drain() {
		p.Discard()
	}
}

// pendingAttachments resuelve los ids que llegan del compositor. Un id
// desconocido (venció, o se quitó) o de otro workspace es un error claro.
func (a *App) pendingAttachments(ids []string, workspaceID int64) ([]*hermes.Pending, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > hermes.MaxAttachments {
		return nil, i18n.Errorf(i18n.Msg{ES: "se pueden adjuntar hasta %d archivos por mensaje (llegaron %d)", EN: "up to %d files can be attached per message (got %d)"}, hermes.MaxAttachments, len(ids))
	}
	out := make([]*hermes.Pending, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		p, ok := a.chatSvc().attachments.Get(id)
		if !ok {
			return nil, i18n.New(i18n.Msg{ES: "un adjunto ya no está (los adjuntos se descartan al cabo de un día); vuelve a adjuntarlo", EN: "an attachment is gone (attachments are discarded after a day); attach it again"})
		}
		if p.WorkspaceID != workspaceID {
			return nil, i18n.Errorf(i18n.Msg{ES: "el adjunto %q es de otro proyecto", EN: "the attachment %q belongs to another project"}, p.Name)
		}
		out = append(out, p)
	}
	return out, nil
}

// --- Capacidades del modelo --------------------------------------------------------

// modelCaps es lo que se sabe del modelo del chat: si ve imágenes, si llama
// herramientas y cuánto cobra. Cada capacidad trae su «Known»: «no se sabe» no
// es «no», y solo un «no» informado por el proveedor bloquea algo.
type modelCaps struct {
	Vision, VisionKnown bool
	Tools, ToolsKnown   bool
	// Precios en dólares por millón de tokens; 0 = desconocido.
	PromptPricePerM, CompletionPricePerM float64
}

// assumeVision dice si se le mandan imágenes: ve, o no se sabe (se manda igual y
// contesta el servidor; ver llm.AttachCaps).
func (c modelCaps) assumeVision() bool { return c.Vision || !c.VisionKnown }

// modelCapabilities lee las capacidades del modelo de la lista guardada del
// servidor. Si no está y full es true (hay herramientas o adjuntos de por
// medio), pide la lista y, si aun así no sale, le pregunta al cliente por la
// visión. Sin full nunca sale a la red: un chat de texto no paga una consulta
// por mensaje.
func (a *App) modelCapabilities(srv store.ModelServer, client llm.Client, model string, full bool) modelCaps {
	fromInfo := func(m ModelInfo) modelCaps {
		return modelCaps{
			Vision: m.Vision, VisionKnown: m.VisionKnown, Tools: m.Tools, ToolsKnown: m.ToolsKnown,
			PromptPricePerM: m.PromptPricePerM, CompletionPricePerM: m.CompletionPricePerM,
		}
	}
	if m, ok := a.serverModelInfo(srv.ID, model); ok {
		return fromInfo(m)
	}
	if !full {
		return modelCaps{}
	}
	if _, err := a.ListServerModels(srv.ID, false); err == nil {
		if m, ok := a.serverModelInfo(srv.ID, model); ok {
			return fromInfo(m)
		}
	}
	ctx, cancel := context.WithTimeout(a.appCtx(), attachCapsTimeout)
	defer cancel()
	ac := llm.AttachCapsOf(ctx, client, model)
	return modelCaps{Vision: ac.Images, VisionKnown: ac.Known}
}

// --- Bindings ----------------------------------------------------------------------

// AddChatAttachment adjunta un archivo del disco (elegido con el diálogo del
// sistema). El tipo se decide por el contenido: imagen PNG/JPG/GIF/WebP de hasta
// 5 MB o modelo STL/3MF/OBJ de hasta 64 MB. Un modelo se copia a refs/ del
// workspace y se analiza; el chip muestra su vista previa.
func (a *App) AddChatAttachment(workspaceID int64, path string) (hermes.Attachment, error) {
	if err := a.ready(); err != nil {
		return hermes.Attachment{}, err
	}
	w, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		return hermes.Attachment{}, err
	}
	path = filepath.Clean(strings.TrimSpace(path))
	name := hermes.CleanName(filepath.Base(path))
	st, err := os.Stat(path)
	if err != nil {
		return hermes.Attachment{}, i18n.Errorf(i18n.Msg{ES: "no se pudo abrir %q: %w", EN: "could not open %q: %w"}, name, err)
	}
	if !st.Mode().IsRegular() {
		return hermes.Attachment{}, i18n.Errorf(i18n.Msg{ES: "%q no es un archivo", EN: "%q is not a file"}, name)
	}
	if st.Size() == 0 {
		return hermes.Attachment{}, i18n.Errorf(i18n.Msg{ES: "%q está vacío", EN: "%q is empty"}, name)
	}
	f, err := os.Open(path)
	if err != nil {
		return hermes.Attachment{}, err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	kind, mime, err := hermes.Classify(name, head[:n])
	if err != nil {
		return hermes.Attachment{}, err
	}
	if st.Size() > hermes.LimitFor(kind) {
		return hermes.Attachment{}, hermes.ErrTooBig(name, kind, hermes.LimitFor(kind))
	}
	if kind == hermes.KindImage {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return hermes.Attachment{}, err
		}
		raw, err := io.ReadAll(io.LimitReader(f, hermes.MaxImageInputBytes+1))
		if err != nil {
			return hermes.Attachment{}, err
		}
		return a.stageImage(w, name, mime, raw)
	}
	return a.attachModel(w, path, name, mime, st.Size())
}

// AddChatAttachmentData adjunta un archivo que el navegador entrega en memoria
// (pegado del portapapeles o arrastrado): name solo aporta el nombre a mostrar y
// la extensión; el archivo lo nombra este código, así que un `../` en name no
// escribe fuera de la carpeta de adjuntos. dataBase64 puede venir como data URL.
func (a *App) AddChatAttachmentData(workspaceID int64, name, dataBase64 string) (hermes.Attachment, error) {
	if err := a.ready(); err != nil {
		return hermes.Attachment{}, err
	}
	w, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		return hermes.Attachment{}, err
	}
	display := hermes.CleanName(name)
	if i := strings.Index(dataBase64, ","); strings.HasPrefix(dataBase64, "data:") && i > 0 {
		dataBase64 = dataBase64[i+1:]
	}
	if len(dataBase64) > maxAttachDataBase64 {
		return hermes.Attachment{}, hermes.ErrTooBig(display, hermes.KindModel, hermes.MaxModelBytes)
	}
	raw, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return hermes.Attachment{}, i18n.Errorf(i18n.Msg{ES: "el archivo %q no se pudo decodificar: %w", EN: "the file %q could not be decoded: %w"}, display, err)
	}
	if len(raw) == 0 {
		return hermes.Attachment{}, i18n.Errorf(i18n.Msg{ES: "%q está vacío", EN: "%q is empty"}, display)
	}
	head := raw
	if len(head) > 512 {
		head = head[:512]
	}
	kind, mime, err := hermes.Classify(display, head)
	if err != nil {
		return hermes.Attachment{}, err
	}
	if int64(len(raw)) > hermes.LimitFor(kind) {
		return hermes.Attachment{}, hermes.ErrTooBig(display, kind, hermes.LimitFor(kind))
	}
	if kind == hermes.KindImage {
		return a.stageImage(w, display, mime, raw)
	}
	// Un modelo se analiza y se copia a refs/ desde un archivo: se escribe uno
	// temporal con el nombre que se quiere conservar.
	dir, err := a.prepareStaging()
	if err != nil {
		return hermes.Attachment{}, err
	}
	tmpDir, err := os.MkdirTemp(dir, "tmp-")
	if err != nil {
		return hermes.Attachment{}, err
	}
	defer os.RemoveAll(tmpDir)
	tmp := filepath.Join(tmpDir, hermes.FSName(display))
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return hermes.Attachment{}, err
	}
	return a.attachModel(w, tmp, display, mime, int64(len(raw)))
}

// ChooseChatAttachment abre el diálogo de archivos del sistema y adjunta lo que
// se elija. Si alguno falla, no queda ninguno adjunto de esa tanda y el error
// nombra el archivo: así lo que se ve en el compositor siempre coincide con lo
// que se eligió. Lista vacía si se cancela.
func (a *App) ChooseChatAttachment(workspaceID int64) ([]hermes.Attachment, error) {
	return a.chooseAttachments(workspaceID, false)
}

// ChooseChatImages es ChooseChatAttachment solo para fotos (S15, «Crear desde una
// imagen»): el diálogo del sistema ofrece únicamente imágenes y un modelo 3D que se
// cuele (se puede cambiar el filtro) se rechaza, igual que si no se hubiera elegido.
func (a *App) ChooseChatImages(workspaceID int64) ([]hermes.Attachment, error) {
	return a.chooseAttachments(workspaceID, true)
}

func (a *App) chooseAttachments(workspaceID int64, imagesOnly bool) ([]hermes.Attachment, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	opts := wruntime.OpenDialogOptions{
		Title: i18n.T(i18n.Msg{ES: "Adjuntar imágenes o modelos 3D", EN: "Attach images or 3D models"}),
		Filters: []wruntime.FileFilter{
			{DisplayName: i18n.T(i18n.Msg{ES: "Imágenes y modelos 3D", EN: "Images and 3D models"}), Pattern: "*.png;*.jpg;*.jpeg;*.gif;*.webp;*.stl;*.3mf;*.obj"},
		},
	}
	if imagesOnly {
		opts.Title = i18n.T(i18n.Msg{ES: "Elegir fotos del objeto", EN: "Choose photos of the object"})
		opts.Filters = []wruntime.FileFilter{
			{DisplayName: i18n.T(i18n.Msg{ES: "Imágenes", EN: "Images"}), Pattern: "*.png;*.jpg;*.jpeg;*.gif;*.webp"},
		}
	}
	paths, err := wruntime.OpenMultipleFilesDialog(a.appCtx(), opts)
	if err != nil {
		return nil, err
	}
	out := []hermes.Attachment{}
	undo := func() {
		for _, done := range out {
			_ = a.RemoveChatAttachment(done.ID)
		}
	}
	for _, p := range paths {
		att, err := a.AddChatAttachment(workspaceID, p)
		if err != nil {
			undo()
			return nil, err
		}
		out = append(out, att)
		if imagesOnly && att.Kind != hermes.KindImage {
			undo()
			return nil, i18n.Errorf(i18n.Msg{ES: "%q no es una imagen: elige fotos PNG, JPG, GIF o WebP", EN: "%q is not an image: choose PNG, JPG, GIF or WebP photos"}, att.Name)
		}
	}
	return out, nil
}

// RemoveChatAttachment quita un adjunto que todavía no se envió y borra lo que
// se creó para él (la imagen en preparación y la copia en refs/ de un modelo).
// Quitar uno que ya no está no es un error.
func (a *App) RemoveChatAttachment(id string) error {
	if p, ok := a.chatSvc().attachments.Take(id); ok {
		p.Discard()
	}
	return nil
}

// stageImage guarda una imagen en la carpeta de preparación y la registra.
func (a *App) stageImage(w store.Workspace, name, mime string, raw []byte) (hermes.Attachment, error) {
	dir, err := a.prepareStaging()
	if err != nil {
		return hermes.Attachment{}, err
	}
	// Una foto grande no se rechaza: al modelo va una copia reducida (lado ≤ 2048 px,
	// ≤ 5 MB) y el original queda al lado, para guardarlo en refs/images/ al enviar.
	send, sendMime, reduced, err := hermes.ReduceImage(raw, mime)
	if err != nil {
		return hermes.Attachment{}, i18n.Errorf(i18n.Msg{ES: "no se pudo preparar la imagen %q: %w", EN: "could not prepare the image %q: %w"}, name, err)
	}
	id := hermes.NewID()
	saveErr := func(err error) error {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo guardar el adjunto: %w", EN: "could not save the attachment: %w"}, err)
	}
	path := filepath.Join(dir, id+hermes.ImageExt(sendMime))
	if err := os.WriteFile(path, send, 0o600); err != nil {
		return hermes.Attachment{}, saveErr(err)
	}
	pending := &hermes.Pending{WorkspaceID: w.ID, Staged: path}
	if reduced {
		orig := filepath.Join(dir, id+"-original"+hermes.ImageExt(mime))
		if err := os.WriteFile(orig, raw, 0o600); err != nil {
			_ = os.Remove(path)
			return hermes.Attachment{}, saveErr(err)
		}
		pending.Original = orig
	}
	// Size y Mime son los del archivo del usuario (el chip dice lo que adjuntó);
	// la miniatura sale de la copia reducida, que es barata de decodificar.
	att := hermes.Attachment{ID: id, Kind: hermes.KindImage, Name: name, Mime: mime, Size: int64(len(raw)), ThumbDataURL: hermes.ThumbDataURL(send)}
	pending.Attachment = att
	a.chatSvc().attachments.Add(pending)
	return att, nil
}

// attachModel copia un modelo de referencia a refs/, lo analiza y lo registra.
// Si el análisis falla (archivo corrupto, sin triángulos) la copia se borra: no
// se deja en el proyecto una referencia que no sirve.
func (a *App) attachModel(w store.Workspace, src, name, mime string, size int64) (hermes.Attachment, error) {
	if _, err := a.prepareStaging(); err != nil {
		return hermes.Attachment{}, err
	}
	// Un modelo que ya está en refs/ del workspace (lo adjunta «Modificar con IA»
	// tras PrepareMeshEdit) se usa tal cual: copiarlo dejaría un refs/x-2.stl
	// duplicado, y quitar el chip no debe borrar un archivo que no creó el chat.
	rel, owned := existingRef(w.Path, src), false
	if rel == "" {
		var err error
		if rel, err = refmodel.CopyToRefs(w.Path, src); err != nil {
			return hermes.Attachment{}, err
		}
		owned = true
	}
	abs := filepath.Join(w.Path, filepath.FromSlash(rel))
	report, err := refmodel.Analyze(abs)
	if err != nil {
		if owned {
			_ = os.Remove(abs)
		}
		return hermes.Attachment{}, i18n.Errorf(i18n.Msg{ES: "no se pudo leer %q como modelo 3D: %w", EN: "could not read %q as a 3D model: %w"}, name, err)
	}
	att := hermes.Attachment{
		ID: hermes.NewID(), Kind: hermes.KindModel, Name: name, Mime: mime, Size: size,
		RefPath: rel, Summary: report.PromptText(rel, i18n.Lang()),
	}
	// La vista previa es del chip y, si el modelo ve, también viaja al modelo.
	// Que no se pueda dibujar no impide adjuntar: queda el informe.
	var preview []byte
	if png, perr := refmodel.Preview(abs, "iso", 640); perr == nil {
		preview = png
		att.ThumbDataURL = hermes.ThumbDataURL(png)
	} else {
		log.Printf("scadlab: vista previa de %s: %v", rel, perr)
	}
	a.chatSvc().attachments.Add(&hermes.Pending{Attachment: att, WorkspaceID: w.ID, RefAbs: abs, Owned: owned, Preview: preview})
	return att, nil
}

// existingRef devuelve "refs/<nombre>" si src es un archivo que ya vive
// directamente en <ws>/refs/ (comparando rutas reales, sin enlaces), o "".
func existingRef(wsDir, src string) string {
	refs, err := filepath.EvalSymlinks(filepath.Join(wsDir, "refs"))
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return ""
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() || filepath.Dir(real) != refs {
		return ""
	}
	return "refs/" + filepath.Base(real)
}
