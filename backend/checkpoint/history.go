package checkpoint

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"scadlab/backend/i18n"
)

// Historial de un checkpoint (S16). Los checkpoints se pueden reescribir en el
// lugar —lo hace `update_checkpoint` (la IA, solo en «Agente») y «Restaurar
// versión» / «Deshacer» (el usuario)—, pero SIEMPRE con un respaldo previo de lo
// que había:
//
//	<ws>/.scadlab/history/<archivo.scad>/<id>.scad   la versión anterior, entera
//	<ws>/.scadlab/history/<archivo.scad>/<id>.json   {savedAt, reason, tool, note, size}
//
// El orden es respaldo → escritura atómica: si el respaldo falla no se toca el
// archivo. Los respaldos pasan el tope (Settings.HistoryMaxVersions): al pasarlo
// se borran los MÁS VIEJOS de ese archivo. Son copias de seguridad, no
// checkpoints; ninguna herramienta borra un checkpoint (regla 3 de technical.md).

const (
	// ReasonAI: el respaldo lo guardó update_checkpoint antes de que la IA
	// reescribiera el archivo.
	ReasonAI = "ai"
	// ReasonRestore: lo guardó un «Restaurar versión» / «Deshacer» antes de
	// reemplazar el contenido.
	ReasonRestore = "restore"
	// DefaultHistoryMax es cuántas versiones se conservan por archivo cuando nadie
	// dice otra cosa (maxVersions <= 0).
	DefaultHistoryMax = 20

	historyDirName = "history"
	// updateTool es el nombre de la herramienta MCP, anotado en cada respaldo.
	updateTool = "update_checkpoint"
	// maxVersionBytes acota la lectura de una versión (un checkpoint real no se
	// acerca; 32 MB es un volcado).
	maxVersionBytes = 32 << 20
	// maxMetaBytes acota la lectura del .json de una versión.
	maxMetaBytes = 64 << 10
	// maxNoteRunes acota la nota de un respaldo.
	maxNoteRunes = 300
)

// Version es un respaldo del historial de un archivo.
type Version struct {
	ID      string    `json:"id"`      // "20261019-221500" o "20261019-221500-2"
	SavedAt time.Time `json:"savedAt"` // cuándo se guardó el respaldo
	Reason  string    `json:"reason"`  // ReasonAI | ReasonRestore
	Tool    string    `json:"tool"`    // "update_checkpoint" | ""
	Note    string    `json:"note"`
	Size    int64     `json:"size"`
}

// UpdateResult es lo que devuelve Update.
type UpdateResult struct {
	Unchanged   bool
	Version     Version // respaldo guardado (cero si Unchanged)
	Description string  // desc final de la cabecera
	Parent      string
}

// versionMeta es el contenido del <id>.json.
type versionMeta struct {
	SavedAt string `json:"savedAt"`
	Reason  string `json:"reason"`
	Tool    string `json:"tool"`
	Note    string `json:"note"`
	Size    int64  `json:"size"`
}

// versionIDRe: AAAAMMDD-HHMMSS con un -N opcional (N >= 2) si chocó.
var versionIDRe = regexp.MustCompile(`^(\d{8}-\d{6})(?:-(\d{1,6}))?$`)

// ValidVersionID dice si id es un identificador de versión que ScadLab pudo
// haber generado. Es la única puerta de un id que llega de fuera (un binding, un
// argumento): con él se arma un nombre de archivo, así que nada más pasa.
func ValidVersionID(id string) error {
	if m := versionIDRe.FindStringSubmatch(id); m == nil {
		return i18n.New(i18n.Msg{ES: "identificador de versión inválido", EN: "invalid version id"})
	}
	return nil
}

// splitID separa un id válido en su marca y su número de orden (1 si no lleva sufijo).
func splitID(id string) (stamp string, seq int, ok bool) {
	m := versionIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	seq = 1
	if m[2] != "" {
		seq, _ = strconv.Atoi(m[2])
		if seq < 1 {
			seq = 1
		}
	}
	return m[1], seq, true
}

func joinID(stamp string, seq int) string {
	if seq <= 1 {
		return stamp
	}
	return stamp + "-" + strconv.Itoa(seq)
}

// idLess ordena por marca y, a igualdad, por número: el id más grande es el más nuevo.
func idLess(a, b string) bool {
	sa, na, _ := splitID(a)
	sb, nb, _ := splitID(b)
	if sa != sb {
		return sa < sb
	}
	return na < nb
}

// historyLocks da un candado por workspace y archivo: dos actualizaciones del
// mismo archivo dentro del proceso no se pisan (entre procesos —la app y el
// servidor MCP— lo cubren O_EXCL al reservar el id y el rename atómico).
var historyLocks sync.Map // string → *sync.Mutex

func lockHistory(ws, file string) func() {
	v, _ := historyLocks.LoadOrStore(ws+"\x00"+file, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// historyDir devuelve <ws>/.scadlab/history/<file>. metaDir rechaza que
// .scadlab, history o la carpeta del archivo sean enlaces.
func historyDir(ws, file string, create bool) (string, error) {
	return metaDir(ws, create, historyDirName, file)
}

// cleanNote deja la nota en una línea y acotada.
func cleanNote(note string) string {
	note = strings.Join(strings.Fields(note), " ")
	if r := []rune(note); len(r) > maxNoteRunes {
		note = string(r[:maxNoteRunes])
	}
	return note
}

// headerInfo es lo que Update y RestoreVersion necesitan de la primera línea.
type headerInfo struct {
	bom    bool
	has    bool   // la primera línea es una cabecera @checkpoint
	line   string // la línea tal cual, sin su fin de línea ni el BOM
	parent string
	desc   string // sin escapar
	body   string // lo que sigue a la primera línea (con su fin de línea inicial ya quitado)
	nl     string // fin de línea de la primera línea ("\n", "\r\n" o "" si es la única)
}

func parseHeader(raw string) headerInfo {
	var h headerInfo
	if strings.HasPrefix(raw, "\uFEFF") {
		h.bom = true
		raw = strings.TrimPrefix(raw, "\uFEFF")
	}
	first, rest, found := strings.Cut(raw, "\n")
	h.nl = ""
	if found {
		h.nl = "\n"
		if strings.HasSuffix(first, "\r") {
			first = strings.TrimSuffix(first, "\r")
			h.nl = "\r\n"
		}
	}
	h.line, h.body = first, rest
	if !found {
		h.body = ""
	}
	if loc := headerLineRe.FindStringIndex(first); loc != nil {
		h.has = true
		tail := first[loc[1]:]
		if m := parentTokenRe.FindStringSubmatch(tail); m != nil {
			h.parent = m[1]
		}
		if m := descValueRe.FindStringSubmatch(tail); m != nil {
			h.desc = descUnescape.Replace(m[1])
		}
	}
	if !h.has {
		h.line, h.body = "", raw
		h.nl = ""
	}
	return h
}

// descValueRe lee el desc="…" de la cabecera (admite \" y \\), igual que
// workspace.Scan.
var (
	descValueRe  = regexp.MustCompile(`\bdesc="((?:[^"\\]|\\.)*)"`)
	descUnescape = strings.NewReplacer(`\"`, `"`, `\\`, `\`)
)

// notInWorkspace arma el error de «el archivo no está»: si hay una copia en la
// papelera lo dice y sugiere restore_checkpoint.
func notInWorkspace(ws, file string) error {
	if item, ok := trashedCopy(ws, file); ok {
		return i18n.Errorf(i18n.Msg{
			ES: "%s no está en el workspace: está en la papelera como %s. Restáuralo con restore_checkpoint (trashed_name=%s) y vuelve a intentarlo",
			EN: "%s is not in the workspace: it is in the trash as %s. Restore it with restore_checkpoint (trashed_name=%s) and try again",
		}, file, item, item)
	}
	return i18n.Errorf(i18n.Msg{ES: "no se encontró el checkpoint %s", EN: "checkpoint %s was not found"}, file)
}

// trashedCopy busca en la papelera la copia más reciente de file.
func trashedCopy(ws, file string) (string, bool) {
	items, err := ListTrash(ws)
	if err != nil {
		return "", false
	}
	for _, it := range items { // lo más reciente primero
		if it.Original == file {
			return it.Name, true
		}
	}
	return "", false
}

// checkEditable valida que file sea un archivo regular de la raíz del workspace
// y devuelve su estado.
func checkEditable(ws, file string) (string, os.FileInfo, error) {
	if err := ValidName(file); err != nil {
		return "", nil, err
	}
	path := filepath.Join(ws, file)
	st, err := os.Lstat(path)
	if err != nil || st.IsDir() {
		return "", nil, notInWorkspace(ws, file)
	}
	if !st.Mode().IsRegular() {
		return "", nil, i18n.Errorf(i18n.Msg{
			ES: "%s no es un archivo normal (¿es un enlace?): no lo reescribo por seguridad",
			EN: "%s is not a plain file (is it a link?): not rewriting it for safety",
		}, file)
	}
	return path, st, nil
}

// Update reescribe en el lugar el checkpoint file con source: es lo que hace
// update_checkpoint cuando la IA edita el archivo que el usuario nombró.
//
//   - Conserva la cabecera `// @checkpoint parent=… desc="…"` del archivo (la
//     primera línea, byte a byte). Si source trae la suya se descarta; un note no
//     vacío reemplaza el desc. Un archivo sin cabecera sigue sin ella (con note se
//     le pone una raíz con ese desc).
//   - Si el resultado es idéntico al archivo no escribe ni respalda (Unchanged).
//   - Si no: guarda la versión anterior en el historial (ReasonAI, tool
//     update_checkpoint) y SOLO ENTONCES escribe, de forma atómica (temporal en la
//     carpeta + rename, conservando los permisos), y poda el historial al tope.
//
// maxVersions <= 0 es DefaultHistoryMax. No toca el padre activo ni los demás
// archivos.
func Update(ws, file, source, note string, maxVersions int) (UpdateResult, error) {
	path, st, err := checkEditable(ws, file)
	if err != nil {
		return UpdateResult{}, err
	}
	unlock := lockHistory(ws, file)
	defer unlock()

	curRaw, err := os.ReadFile(path)
	if err != nil {
		return UpdateResult{}, err
	}
	cur := parseHeader(string(curRaw))

	src := strings.TrimPrefix(source, "\uFEFF")
	body := src
	if first, rest, found := strings.Cut(src, "\n"); headerLineRe.MatchString(strings.TrimRight(first, "\r")) {
		body = ""
		if found {
			body = rest
		}
	}
	if strings.TrimSpace(body) == "" {
		return UpdateResult{}, i18n.New(i18n.Msg{
			ES: "el código nuevo está vacío: no reescribo el archivo",
			EN: "the new source is empty: not rewriting the file",
		})
	}
	// Fin de línea del archivo resultante: el del código nuevo si trae saltos y, si
	// es de una sola línea, el que ya tenía el archivo.
	nl := "\n"
	switch {
	case strings.Contains(src, "\r\n"):
		nl = "\r\n"
	case strings.Contains(src, "\n"):
	case cur.nl == "\r\n":
		nl = "\r\n"
	}

	note = cleanNote(note)
	res := UpdateResult{Parent: cur.parent, Description: cur.desc}
	var headerLine string
	switch {
	case note != "":
		headerLine = Header(cur.parent, note)
		res.Description = strings.Join(strings.Fields(note), " ")
	case cur.has:
		headerLine = cur.line // la cabecera tal cual estaba
	}
	var out strings.Builder
	if cur.bom {
		out.WriteString("\uFEFF")
	}
	if headerLine != "" {
		out.WriteString(headerLine)
		out.WriteString(nl)
	}
	out.WriteString(body)
	newRaw := []byte(out.String())

	if bytes.Equal(newRaw, curRaw) {
		res.Unchanged = true
		return res, nil
	}

	// 1) Respaldo de lo que hay. Sin él no se escribe.
	ver, err := saveVersion(ws, file, curRaw, ReasonAI, updateTool, note)
	if err != nil {
		return UpdateResult{}, err
	}
	// 2) Escritura atómica.
	if err := replaceKeepingMode(path, newRaw, st); err != nil {
		removeVersion(ws, file, ver.ID) // el respaldo es una copia del archivo intacto: no cuenta como edición
		return UpdateResult{}, err
	}
	res.Version = ver
	pruneHistory(ws, file, maxVersions)
	return res, nil
}

// replaceKeepingMode reemplaza path por data de forma atómica conservando los
// permisos (pero NO la fecha: es una edición y el árbol debe verla).
func replaceKeepingMode(path string, data []byte, st os.FileInfo) error {
	dir, name := filepath.Split(path)
	tmp, err := os.CreateTemp(dir, "."+name+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = os.Remove(tmpName) // el temporal es nuestro, no un checkpoint
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(tmpName, st.Mode().Perm()); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fail(err)
	}
	return nil
}

// versionIDs lista los ids que tienen reservado un .json en dir, del más viejo
// al más nuevo.
func versionIDs(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || e.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if ValidVersionID(id) == nil {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return idLess(ids[i], ids[j]) })
	return ids
}

// maxIDAttempts son los ids que se prueban si otro proceso se adelanta.
const maxIDAttempts = 50

// saveVersion guarda content como una versión nueva del historial de file y
// devuelve su ficha. El id sale de la hora local; si choca con uno existente —o
// el reloj retrocedió, p. ej. al terminar el horario de verano— toma el
// siguiente número de la última marca (-2, -3…), así el id más grande es siempre
// el más reciente. El id se reserva con O_EXCL sobre su .json, y el .scad se
// escribe de forma atómica antes de completar el .json: un lector solo ve
// versiones completas (las que tienen un .json válido).
func saveVersion(ws, file string, content []byte, reason, tool, note string) (Version, error) {
	dir, err := historyDir(ws, file, true)
	if err != nil {
		return Version{}, err
	}
	t := now()
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		stamp, seq := t.Format(trashStamp), 1
		if ids := versionIDs(dir); len(ids) > 0 {
			lastStamp, lastSeq, _ := splitID(ids[len(ids)-1])
			switch {
			case stamp < lastStamp:
				stamp, seq = lastStamp, lastSeq+1
			case stamp == lastStamp:
				seq = lastSeq + 1
			}
		}
		id := joinID(stamp, seq)
		f, err := os.OpenFile(filepath.Join(dir, id+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return Version{}, err
		}
		_ = f.Close()
		ver := Version{ID: id, SavedAt: t, Reason: reason, Tool: tool, Note: cleanNote(note), Size: int64(len(content))}
		meta, merr := json.Marshal(versionMeta{SavedAt: t.Format(time.RFC3339), Reason: reason, Tool: tool, Note: ver.Note, Size: ver.Size})
		if merr == nil {
			merr = writeFileAtomic(dir, id+".scad", content)
		}
		if merr == nil {
			merr = writeFileAtomic(dir, id+".json", meta)
		}
		if merr != nil {
			_ = os.Remove(filepath.Join(dir, id+".scad"))
			_ = os.Remove(filepath.Join(dir, id+".json"))
			return Version{}, merr
		}
		return ver, nil
	}
	return Version{}, i18n.New(i18n.Msg{ES: "no se encontró un identificador libre para el respaldo", EN: "could not find a free id for the backup"})
}

// removeVersion borra los dos archivos de una versión (mejor esfuerzo).
func removeVersion(ws, file, id string) {
	dir, err := historyDir(ws, file, false)
	if err != nil || ValidVersionID(id) != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, id+".scad"))
	_ = os.Remove(filepath.Join(dir, id+".json"))
}

// pruneHistory deja a lo más maxVersions versiones: borra las más viejas (el
// .scad y su .json). Son respaldos, no checkpoints. Mejor esfuerzo.
func pruneHistory(ws, file string, maxVersions int) {
	if maxVersions <= 0 {
		maxVersions = DefaultHistoryMax
	}
	dir, err := historyDir(ws, file, false)
	if err != nil {
		return
	}
	ids := versionIDs(dir)
	for len(ids) > maxVersions {
		_ = os.Remove(filepath.Join(dir, ids[0]+".scad"))
		_ = os.Remove(filepath.Join(dir, ids[0]+".json"))
		ids = ids[1:]
	}
}

// readMeta lee el .json de una versión. ok=false si no existe, está a medias o
// no se entiende.
func readMeta(dir, id string) (versionMeta, bool) {
	path := filepath.Join(dir, id+".json")
	if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
		return versionMeta{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return versionMeta{}, false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxMetaBytes))
	if err != nil {
		return versionMeta{}, false
	}
	var m versionMeta
	if json.Unmarshal(raw, &m) != nil {
		return versionMeta{}, false
	}
	return m, true
}

// History devuelve las versiones guardadas de file, la más nueva primero. Sin
// historial devuelve nil, nil. Solo cuentan las versiones completas: las dos
// piezas presentes y el .json legible.
func History(ws, file string) ([]Version, error) {
	if err := ValidName(file); err != nil {
		return nil, err
	}
	dir, err := historyDir(ws, file, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids := versionIDs(dir)
	var out []Version
	for i := len(ids) - 1; i >= 0; i-- {
		id := ids[i]
		m, ok := readMeta(dir, id)
		if !ok {
			continue
		}
		st, err := os.Lstat(filepath.Join(dir, id+".scad"))
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		at, _ := time.Parse(time.RFC3339, m.SavedAt)
		out = append(out, Version{ID: id, SavedAt: at, Reason: m.Reason, Tool: m.Tool, Note: m.Note, Size: st.Size()})
	}
	return out, nil
}

// ReadVersion devuelve el código de una versión del historial.
func ReadVersion(ws, file, id string) (string, error) {
	if err := ValidName(file); err != nil {
		return "", err
	}
	if err := ValidVersionID(id); err != nil {
		return "", err
	}
	dir, err := historyDir(ws, file, false)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", noVersion(file, id)
		}
		return "", err
	}
	path := filepath.Join(dir, id+".scad")
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", noVersion(file, id)
	}
	if st.Size() > maxVersionBytes {
		return "", i18n.New(i18n.Msg{ES: "la versión es demasiado grande", EN: "the version is too large"})
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func noVersion(file, id string) error {
	return i18n.Errorf(i18n.Msg{ES: "no existe la versión %s de %s", EN: "version %s of %s does not exist"}, id, file)
}

// RestoreVersion devuelve file a la versión id del historial. Antes guarda lo que
// hay ahora como una versión más (ReasonRestore), así que restaurar también se
// puede deshacer. Toma el cuerpo y el desc de la versión y conserva el `parent=`
// ACTUAL del archivo (es metadato del árbol, no del modelo). Devuelve la versión
// que guardó (la del contenido anterior).
func RestoreVersion(ws, file, id string, maxVersions int) (Version, error) {
	path, st, err := checkEditable(ws, file)
	if err != nil {
		return Version{}, err
	}
	if err := ValidVersionID(id); err != nil {
		return Version{}, err
	}
	unlock := lockHistory(ws, file)
	defer unlock()

	old, err := ReadVersion(ws, file, id)
	if err != nil {
		return Version{}, err
	}
	curRaw, err := os.ReadFile(path)
	if err != nil {
		return Version{}, err
	}
	cur := parseHeader(string(curRaw))
	vh := parseHeader(old)

	var newRaw string
	switch {
	case vh.has:
		raw := []byte(old)
		out, _, werr := withParent(raw, cur.parent)
		if werr != nil { // la cabecera de la versión no tiene parent=: se rearma
			nl := vh.nl
			if nl == "" {
				nl = "\n"
			}
			out = []byte(boolStr(vh.bom, "\uFEFF") + Header(cur.parent, vh.desc) + nl + vh.body)
		}
		newRaw = string(out)
	case cur.has: // la versión no tenía cabecera: se conserva la actual
		nl := cur.nl
		if nl == "" {
			nl = "\n"
		}
		newRaw = boolStr(cur.bom, "\uFEFF") + cur.line + nl + vh.body
	default:
		newRaw = old
	}

	ver, err := saveVersion(ws, file, curRaw, ReasonRestore, "", "")
	if err != nil {
		return Version{}, err
	}
	if err := replaceKeepingMode(path, []byte(newRaw), st); err != nil {
		removeVersion(ws, file, ver.ID)
		return Version{}, err
	}
	pruneHistory(ws, file, maxVersions)
	return ver, nil
}

func boolStr(b bool, s string) string {
	if b {
		return s
	}
	return ""
}

// EditedByAI dice si la versión más nueva del historial de file la guardó la IA
// (reason "ai"): el último cambio del archivo es suyo y «Deshacer» lo revierte.
// Restaurar guarda la versión actual con reason "restore", así que el indicador
// se apaga solo. Es barato: ordena por nombre y lee un único .json, sin tocar el
// código de ninguna versión.
func EditedByAI(ws, file string) bool {
	if ValidName(file) != nil {
		return false
	}
	dir, err := historyDir(ws, file, false)
	if err != nil {
		return false
	}
	ids := versionIDs(dir)
	for i := len(ids) - 1; i >= 0; i-- {
		// El .json de la más nueva puede estar a medias (se está escribiendo): se
		// pasa a la siguiente, que es la última completa.
		if m, ok := readMeta(dir, ids[i]); ok {
			return m.Reason == ReasonAI
		}
	}
	return false
}

// --- historial y papelera -----------------------------------------------------

// historySuffix es lo que se agrega al nombre de un elemento de la papelera para
// nombrar la carpeta con su historial: <AAAAMMDD-HHMMSS>-<archivo>.history. No
// termina en .scad: ListTrash y workspace.Scan no la ven.
const historySuffix = ".history"

// trashHistory mueve el historial de file a la papelera, junto al archivo ya
// movido (trashedName). Sin historial no hace nada.
func trashHistory(wsDir, trashPath, file, trashedName string) error {
	src, err := historyDir(wsDir, file, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	unlock := lockHistory(wsDir, file)
	defer unlock()
	return moveHistory(src, filepath.Join(trashPath, trashedName+historySuffix))
}

// restoreHistory devuelve a .scadlab/history/<restored>/ el historial que se fue
// a la papelera con trashedName. Sin historial no hace nada.
func restoreHistory(wsDir, trashedName, restored string) error {
	tdir, err := metaDir(wsDir, false, trashDirName)
	if err != nil {
		return nil // sin papelera no hay historial
	}
	src := filepath.Join(tdir, trashedName+historySuffix)
	st, err := os.Lstat(src)
	if err != nil {
		return nil
	}
	if !st.IsDir() || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return i18n.Errorf(i18n.Msg{
			ES: "%s no es una carpeta normal (¿es un enlace?): no la uso por seguridad",
			EN: "%s is not a plain folder (is it a link?): not using it for safety",
		}, src)
	}
	if _, err := metaDir(wsDir, true, historyDirName); err != nil {
		return err
	}
	unlock := lockHistory(wsDir, restored)
	defer unlock()
	dst := filepath.Join(wsDir, metaDirName, historyDirName, restored)
	return moveHistory(src, dst)
}

// moveHistory mueve la carpeta de un historial. Si el destino no existe la
// renombra entera; si existe (el archivo ya tiene historial nuevo) mueve versión
// por versión sin pisar nada: una versión cuyo id ya está ocupado toma el
// siguiente libre de su misma marca.
func moveHistory(src, dst string) error {
	if _, err := os.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
		rerr := os.Rename(src, dst)
		if rerr == nil {
			return nil
		}
		// Otro proceso pudo crear el destino en el medio: entonces se fusiona.
		if _, err := os.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
			return rerr
		}
	}
	st, err := os.Lstat(dst)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return i18n.Errorf(i18n.Msg{
			ES: "%s no es una carpeta normal (¿es un enlace?): no la uso por seguridad",
			EN: "%s is not a plain folder (is it a link?): not using it for safety",
		}, dst)
	}
	var firstErr error
	for _, id := range versionIDs(src) {
		to := id
		for n := 0; fileExists(filepath.Join(dst, to+".json")) || fileExists(filepath.Join(dst, to+".scad")); n++ {
			stamp, seq, _ := splitID(to)
			to = joinID(stamp, seq+1)
			if n > 1000 {
				break
			}
		}
		// El .scad primero y el .json al final: un lector no ve la versión hasta
		// que está completa.
		for _, ext := range []string{".scad", ".json"} {
			from := filepath.Join(src, id+ext)
			if _, err := os.Lstat(from); err != nil {
				continue
			}
			if err := moveNoReplace(from, filepath.Join(dst, to+ext)); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr == nil {
		_ = os.Remove(src) // vacía: se va; si quedó algo ajeno, queda
	}
	return firstErr
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// removeTree borra path sin seguir enlaces: un enlace (simbólico o punto de
// unión) se quita a él, nunca a su destino.
func removeTree(path string) error {
	st, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !st.IsDir() {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}
