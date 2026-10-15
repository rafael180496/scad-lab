// Package agentsessions lista las sesiones previas de cada CLI agéntico
// (Claude Code, Codex, opencode) para la carpeta de un workspace, y arma los
// argumentos con que se retoma una.
//
// Solo LEE lo que cada CLI dejó en disco (o lo que su propio comando de
// listado imprime): nunca escribe, mueve ni copia una sesión. Formatos
// verificados contra los CLIs instalados, ver claude/09-agents-ai.md.
//
// Todo es tolerante: una línea rota, un archivo que no se puede leer o un
// formato que cambió en una versión nueva se saltan, nunca rompen el listado.
// Una lista vacía es un resultado válido (el usuario todavía no trabajó ahí).
package agentsessions

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"

	"scadlab/backend/i18n"
)

// Session es una conversación previa de un CLI.
type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`     // primer mensaje del usuario ("" si no se pudo leer)
	UpdatedAt int64  `json:"updatedAt"` // unix ms de la última actividad
}

// Modos de arranque de una terminal de agente.
const (
	ModeNew      = "new"
	ModeResume   = "resume"
	ModeContinue = "continue"
)

// IDs de los CLIs (los mismos que ai_providers.id).
const (
	Claude      = "claude"
	Codex       = "codex"
	OpenCode    = "opencode"
	Antigravity = "antigravity"
)

const (
	// DefaultLimit es cuántas sesiones se devuelven como máximo: la lista es
	// para elegir una reciente, no para auditar el historial.
	DefaultLimit = 40
	// maxScan acota cuántos archivos se abren buscando sesiones del
	// workspace (la carpeta de Codex mezcla todos los proyectos).
	maxScan = 4000
	// maxHeadBytes es cuánto de cada archivo se lee buscando el título: un
	// .jsonl de Claude pesa decenas de MB, pero el primer mensaje está arriba.
	maxHeadBytes = 2 << 20
	maxLineBytes = 8 << 20
	maxTitleRune = 120
)

// Options ajusta List. Todo es opcional; existe para las pruebas y para el
// ejecutable de opencode.
type Options struct {
	// Home reemplaza al directorio del usuario (y a CLAUDE_CONFIG_DIR /
	// CODEX_HOME). Vacío = el real.
	Home string
	// Exe es la ruta del ejecutable de opencode (el único CLI que se consulta
	// con su propio comando en vez de leer archivos).
	Exe string
	// Limit tope de resultados (0 = DefaultLimit).
	Limit int
	// Run ejecuta un comando y devuelve su salida estándar. nil = os/exec.
	Run Runner
}

// Runner ejecuta exe con args dentro de dir.
type Runner func(ctx context.Context, dir, exe string, args ...string) ([]byte, error)

func (o Options) limit() int {
	if o.Limit > 0 {
		return o.Limit
	}
	return DefaultLimit
}

// CanList dice si el CLI tiene un listado de sesiones verificable. Antigravity
// no: solo ofrece «nueva» y «continuar la última».
func CanList(agentID string) bool {
	switch agentID {
	case Claude, Codex, OpenCode:
		return true
	}
	return false
}

// Known dice si agentID es uno de los CLIs que ScadLab sabe lanzar.
func Known(agentID string) bool {
	return CanList(agentID) || agentID == Antigravity
}

var (
	uuidRe     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	opencodeRe = regexp.MustCompile(`^ses_[A-Za-z0-9]{8,64}$`)
)

// ValidID valida un id de sesión con el charset del CLI. Va antes de usarlo
// como argumento: un id que empezara por «-» sería una opción del CLI, y uno
// con espacios o comillas no es un id.
func ValidID(agentID, id string) bool {
	switch agentID {
	case Claude, Codex:
		return uuidRe.MatchString(id)
	case OpenCode:
		return opencodeRe.MatchString(id)
	}
	return false
}

func unsupported(agentID string) error {
	return i18n.Errorf(i18n.Msg{ES: "%q no tiene sesiones que retomar por ID", EN: "%q has no sessions that can be resumed by ID"}, agentID)
}

// Args devuelve los argumentos (sin el ejecutable) para arrancar agentID en el
// modo pedido. Se pasan como argv, nunca por una shell.
//
//	Claude:      claude | claude --resume <id> | claude -c
//	Codex:       codex  | codex resume <id>    | codex resume --last
//	opencode:    opencode | opencode --session <id> | opencode -c
//	Antigravity: agy    | (no hay por id)      | agy --continue
func Args(agentID, mode, sessionID string) ([]string, error) {
	if !Known(agentID) {
		return nil, i18n.Errorf(i18n.Msg{ES: "agente desconocido: %q", EN: "unknown agent: %q"}, agentID)
	}
	switch mode {
	case ModeNew:
		return nil, nil
	case ModeContinue:
		switch agentID {
		case Claude:
			return []string{"-c"}, nil
		case Codex:
			return []string{"resume", "--last"}, nil
		case OpenCode:
			return []string{"-c"}, nil
		case Antigravity:
			return []string{"--continue"}, nil
		}
	case ModeResume:
		if !CanList(agentID) {
			return nil, unsupported(agentID)
		}
		if !ValidID(agentID, sessionID) {
			return nil, i18n.Errorf(i18n.Msg{ES: "ID de sesión inválido: %q", EN: "invalid session ID: %q"}, sessionID)
		}
		switch agentID {
		case Claude:
			return []string{"--resume", sessionID}, nil
		case Codex:
			return []string{"resume", sessionID}, nil
		case OpenCode:
			return []string{"--session", sessionID}, nil
		}
	}
	return nil, i18n.Errorf(i18n.Msg{ES: "modo de arranque inválido: %q", EN: "invalid start mode: %q"}, mode)
}

// List devuelve las sesiones de agentID cuya carpeta de trabajo es
// workspaceDir, la más reciente primero.
func List(ctx context.Context, agentID, workspaceDir string, o Options) ([]Session, error) {
	if !CanList(agentID) {
		return nil, unsupported(agentID)
	}
	dirs := dirForms(workspaceDir)
	if len(dirs) == 0 {
		return []Session{}, nil
	}
	var out []Session
	var err error
	switch agentID {
	case Claude:
		out, err = listClaude(ctx, dirs, o)
	case Codex:
		out, err = listCodex(ctx, dirs, o)
	case OpenCode:
		out, err = listOpenCode(ctx, dirs, workspaceDir, o)
	}
	if out == nil {
		out = []Session{}
	}
	return out, err
}

// dirForms son las formas equivalentes de la carpeta del workspace: la que
// está registrada y la real (sin enlaces simbólicos). Los CLIs guardan el
// cwd del proceso, que en macOS ya viene resuelto (/tmp → /private/tmp).
func dirForms(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = filepath.Clean(dir)
	}
	forms := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		forms = append(forms, real)
	}
	return forms
}

// samePath compara dos rutas tal como las guardó un CLI. Windows (y macOS por
// defecto) no distinguen mayúsculas en el sistema de archivos.
func samePath(a string, forms []string) bool {
	a = filepath.Clean(a)
	for _, f := range forms {
		if a == f {
			return true
		}
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			if strings.EqualFold(a, f) {
				return true
			}
		}
	}
	return false
}

// homeDir es la base de los datos de un CLI: Options.Home (pruebas), la
// variable de entorno que el propio CLI respeta, o ~/<def>.
func homeDir(o Options, env, def string) string {
	if o.Home != "" {
		return filepath.Join(o.Home, def)
	}
	if v := os.Getenv(env); v != "" {
		return v
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, def)
}

// cleanTitle deja el texto en una línea corta: las sesiones se listan para
// reconocerlas, no para leerlas.
func cleanTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxTitleRune {
		r := []rune(s)
		s = string(r[:maxTitleRune-1]) + "…"
	}
	return s
}

// lineReader lee un .jsonl línea a línea con tope de bytes totales. Un solo
// registro puede pesar megabytes (un snapshot de archivos), por eso no es un
// bufio.Scanner con el tamaño por defecto.
type lineReader struct {
	r     *bufio.Reader
	read  int
	limit int
}

func newLineReader(r io.Reader, limit int) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 64<<10), limit: limit}
}

// next devuelve la línea siguiente, o false al terminar el archivo o llegar al
// tope. Una línea que supera maxLineBytes se descarta entera.
func (l *lineReader) next() ([]byte, bool) {
	var line []byte
	tooLong := false
	for {
		if l.read >= l.limit {
			return nil, false
		}
		chunk, err := l.r.ReadSlice('\n')
		l.read += len(chunk)
		if !tooLong {
			line = append(line, chunk...)
			if len(line) > maxLineBytes {
				tooLong, line = true, nil
			}
		}
		switch err {
		case nil:
			if tooLong {
				tooLong = false
				continue
			}
			return line, true
		case bufio.ErrBufferFull:
			continue
		default:
			if len(line) > 0 && !tooLong {
				return line, true
			}
			return nil, false
		}
	}
}
