package clichat

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"scadlab/backend/i18n"
)

// maxLineBytes acota una línea del stream. Un evento puede traer el contenido de
// un archivo adentro, así que el tope es generoso; una línea de 16 MB significa
// que esto no es lo que creemos que es.
const maxLineBytes = 16 << 20

// killGrace es cuánto se espera, tras matar el árbol de procesos, a que se
// cierren los pipes antes de abandonarlos (un nieto que heredó stdout y
// sobrevivió dejaría la lectura colgada).
const killGrace = 3 * time.Second

// Turn es lo que hace falta para un turno. Un turno = un proceso: los CLIs
// headless terminan cuando contestan, así que no hay un proceso vivo entre una
// pregunta y la siguiente; la continuidad la da el id de conversación que el CLI
// devuelve (Session), que quien llama guarda y pasa en el turno siguiente.
type Turn struct {
	// CLI es el id corto (`claude`, `codex`, `antigravity`, `opencode`).
	CLI string
	// Exe es el ejecutable ya resuelto a ruta absoluta (ver Resolve): lanzarlo
	// por nombre dependería del PATH que heredó la ventana, que una app abierta
	// desde el Finder tiene recortado.
	Exe string
	// Cwd es la carpeta del workspace: lo que el CLI puede leer sin que nadie se
	// lo pase.
	Cwd string
	// Env es el entorno del hijo (ver Env). Vacío = el del proceso, sin ampliar.
	Env    []string
	Prompt string
	Mode   Mode
	// Model es el modelo de este turno; vacío usa el que el CLI tenga
	// configurado. No se valida contra una lista fija —los ids cambian con cada
	// versión de cada proveedor—: solo que no pueda leerse como bandera.
	Model string
	// Session es el id de conversación del turno anterior ("" = conversación nueva).
	Session string
	// Images son rutas absolutas de imágenes adjuntas. Codex las recibe por su
	// bandera `-i`; a los demás se les nombran en el prompt y las abre su
	// herramienta de lectura.
	Images []string
	// MCP son los flags que dejan conectado el servidor MCP de ScadLab (los que
	// arma la app para la terminal: mcpLaunchArgs). AddDirs, los `--add-dir` de
	// los otros proyectos abiertos. Los dos se colocan tal cual: la línea de
	// comandos del servidor se define en un solo lugar (backend/mcp).
	MCP     []string
	AddDirs []string
	// ReadDirs son carpetas fuera del workspace que el CLI tiene que poder leer
	// (la de las imágenes adjuntas): Claude Code y Antigravity piden permiso para
	// leer fuera de su carpeta, y en headless el permiso se deniega solo.
	ReadDirs []string
	// MCPEnabled dice que el servidor MCP está encendido (el interruptor de
	// Configuración → IA): con él, el modo decide qué herramientas de ScadLab
	// quedan permitidas.
	MCPEnabled bool
	// Lang es el idioma de la interfaz («es» | «en»): Claude Code contesta en él.
	Lang string
	// StdinPrompt manda el prompt por stdin en vez de por argv. Se activa solo
	// con un shim de Windows (ver usesStdin); la prueba lo fuerza.
	StdinPrompt bool
}

// Result es cómo terminó un turno.
type Result struct {
	// Session es el id de conversación que informó el CLI ("" si no dijo ninguno).
	Session string
	// Model es el modelo que contestó, si el CLI lo informa.
	Model string
	// Usage es el uso del turno; nil si el CLI no lo informó.
	Usage *Usage
	// Cancelled: el contexto se canceló y el árbol de procesos se mató. Lo
	// escrito hasta ahí se conserva y no es un error.
	Cancelled bool
}

// cliError es un fallo que explicó el propio CLI: su texto se muestra tal cual.
type cliError struct{ msg string }

func (e *cliError) Error() string { return e.msg }

var (
	modelRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@#\[\]+-]{0,199}$`)
	sessionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`) // uuid (Claude, Codex, Antigravity) o ses_… (OpenCode)
)

// ValidModel dice si s sirve como id de modelo para `--model`: el id acaba como
// argumento del CLI, así que no puede empezar con «-» (se leería como bandera) ni
// llevar espacios o metacaracteres.
func ValidModel(s string) bool { return s == "" || modelRe.MatchString(s) }

func validSession(s string) bool { return sessionRe.MatchString(s) }

// usesStdin dice si el prompt viaja por stdin. Con un shim de Windows (`.cmd`,
// `.ps1`) el argv pasa por cmd.exe o PowerShell, que reinterpretan comillas,
// `%`, `&` y cortan en el primer salto de línea: un prompt con texto del usuario
// no puede ir por ahí. Los tres CLIs que leen stdin (Claude Code `-p`, Codex sin
// prompt, OpenCode `run`) lo reciben así; Antigravity no tiene esa vía y su
// prompt se limpia (ver shimSafe).
func (t Turn) usesStdin() bool {
	if t.StdinPrompt {
		return true
	}
	return isShim(t.Exe) && t.CLI != "antigravity"
}

// promptArg es el prompt tal como va en argv: con las imágenes nombradas para
// los CLIs que no tienen bandera de imagen.
func (t Turn) promptArg() string {
	p := t.Prompt
	if t.CLI != "codex" {
		p = withImagePaths(p, t.Images)
	}
	if isShim(t.Exe) && t.CLI == "antigravity" {
		p = shimSafe(p)
	}
	return p
}

// withImagePaths nombra las imágenes adjuntas dentro del prompt, para los CLIs
// que no tienen una bandera propia.
func withImagePaths(prompt string, images []string) string {
	if len(images) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\n" + i18n.T(i18n.Msg{ES: "Imágenes adjuntas (ábrelas para verlas):", EN: "Attached images (open them to see them):"}))
	for _, p := range images {
		b.WriteString("\n- ")
		b.WriteString(p)
	}
	return b.String()
}

// shimSafe deja un texto en una sola línea sin los caracteres que cmd.exe o
// PowerShell interpretan. Pierde fidelidad a propósito: es el único camino para
// el CLI que no lee stdin.
func shimSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case strings.ContainsRune("&|<>^%\"`$;(){}!", r), r < 0x20:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// replyLanguage le pide al CLI que conteste en el idioma de la interfaz.
func replyLanguage(lang string) string {
	if lang == "es" {
		return "Responde siempre en español (de tú), salvo que el usuario te escriba en otro idioma."
	}
	return "Always reply in English, unless the user writes to you in another language."
}

// BuildArgs arma la línea de comandos del turno SIN el ejecutable (ver Command).
//
// **Las banderas que saltean todos los permisos no se pasan nunca**, ni en
// «Agente». Cubren también la ejecución de comandos, no solo la edición de
// archivos: es la diferencia entre «que cree checkpoints en el workspace» y
// «que haga cualquier cosa en la máquina».
//
// El orden importa. En Claude Code, `--mcp-config`, `--add-dir`,
// `--allowedTools` y `--disallowedTools` son variádicos: se comen cualquier
// argumento que no empiece con «-», así que cada grupo termina en otro flag o en
// el final de la lista y el prompt va antes de todos. En Codex, `resume` es un
// subcomando que no repite las banderas de `exec` (`--sandbox`, `--add-dir`): van
// antes de él.
func BuildArgs(t Turn) ([]string, error) {
	if !ValidModel(t.Model) {
		return nil, i18n.Errorf(i18n.Msg{ES: "el modelo %q no es un id válido", EN: "the model %q is not a valid id"}, t.Model)
	}
	if t.Session != "" && !validSession(t.Session) {
		return nil, i18n.New(i18n.Msg{ES: "el id de la conversación del CLI no es válido", EN: "the CLI conversation id is not valid"})
	}
	edit := t.Mode == ModeEdit
	stdin := t.usesStdin()

	switch t.CLI {
	case "claude":
		// Claude Code no tiene bandera de imagen: se le nombran los archivos en el
		// prompt y los abre con su herramienta de lectura, que soporta imágenes.
		args := []string{"-p"}
		if !stdin {
			args = append(args, t.promptArg())
		}
		args = append(args, "--output-format", "stream-json", "--verbose")
		if t.Session != "" {
			args = append(args, "--resume", t.Session)
		}
		if t.Model != "" {
			args = append(args, "--model", t.Model)
		}
		// Que conteste en el idioma de la interfaz, como system prompt AGREGADO y
		// no dentro del mensaje: --append-system-prompt no pisa ninguna
		// instrucción propia del usuario.
		args = append(args, "--append-system-prompt", replyLanguage(t.Lang))
		if edit {
			// Aplica las ediciones sin preguntar; los comandos siguen denegados.
			args = append(args, "--permission-mode", "acceptEdits")
		}
		args = append(args, t.MCP...)
		args = append(args, t.AddDirs...)
		for _, d := range t.ReadDirs {
			args = append(args, "--add-dir", d)
		}
		args = append(args, "--disallowedTools", claudeDenied(t.Mode))
		if t.MCPEnabled {
			args = append(args, "--allowedTools", claudeAllowed(t.Mode))
		}
		return args, nil

	case "codex":
		// `exec --json` es su modo headless; --skip-git-repo-check porque la
		// carpeta ya la eligió el usuario al abrir el workspace.
		args := []string{"exec"}
		args = append(args, t.MCP...)
		args = append(args, t.AddDirs...)
		args = append(args, "--json", "--skip-git-repo-check")
		// Su sandbox acota lo que puede hacer la shell que Codex usa para leer:
		// solo lectura en «Solo consulta» (explícito, no el default: el
		// config.toml del usuario puede tener otro) y escritura acotada al
		// workspace en «Agente». Codex no tiene forma de quitar su shell; el
		// sandbox es su límite.
		sandbox := "read-only"
		if edit {
			sandbox = "workspace-write"
		}
		args = append(args, "--sandbox", sandbox)
		if enabled := codexEnabledTools(t.Mode); enabled != "" && t.MCPEnabled {
			args = append(args, "-c", "mcp_servers."+mcpServerName+".enabled_tools="+enabled)
		}
		if t.Model != "" {
			args = append(args, "--model", t.Model)
		}
		if t.Session != "" {
			args = append(args, "resume", t.Session)
		}
		// Codex SÍ tiene bandera propia (`-i`), así que las imágenes van por ahí.
		// Es variádica: el `--` evita que se coma el prompt.
		for _, img := range t.Images {
			args = append(args, "-i", img)
		}
		if !stdin {
			args = append(args, "--", t.promptArg())
		}
		return args, nil

	case "antigravity":
		// Sin bandera de imagen documentada: se nombran las rutas en el prompt.
		args := []string{"--print", t.promptArg(), "--output-format", "stream-json"}
		if t.Session != "" {
			args = append(args, "--conversation", t.Session)
		}
		if edit {
			args = append(args, "--mode", "accept-edits")
		}
		if t.Model != "" {
			args = append(args, "--model", t.Model)
		}
		args = append(args, t.AddDirs...)
		for _, d := range t.ReadDirs {
			args = append(args, "--add-dir", d)
		}
		return args, nil

	case "opencode":
		// `run --format json` es su modo headless. `--standalone` levanta un
		// servidor propio para el turno en vez de usar el servicio en segundo
		// plano: ese ya arrancó con su entorno e ignoraría la configuración de
		// permisos que va por variable (opencodeEnv).
		args := []string{"run", "--format", "json", "--standalone"}
		if t.Session != "" {
			args = append(args, "--session", t.Session)
		}
		// Sin bandera de modo: por defecto (su agente `build`) edita sin
		// preguntar, así que lo que se elige es el agente. «Solo consulta» usa
		// `plan`, que no escribe archivos; «Agente», `build`. Se nombra SIEMPRE:
		// una sesión retomada conserva el agente con que se usó la última vez si
		// no se le dice otro. `--auto` no se pasa nunca: aprueba todo lo que no
		// esté denegado.
		agent := "plan"
		if edit {
			agent = "build"
		}
		args = append(args, "--agent", agent)
		if t.Model != "" {
			args = append(args, "--model", t.Model)
		}
		// El `--` va antes del prompt para que un mensaje que empieza con un guion
		// no se lea como bandera.
		if !stdin {
			args = append(args, "--", t.promptArg())
		}
		return args, nil
	}
	return nil, i18n.Errorf(i18n.Msg{ES: "el CLI %q no es compatible con el chat", EN: "the CLI %q is not supported by the chat"}, t.CLI)
}

// Command es el argv completo del turno: el lanzador del ejecutable más los
// argumentos de BuildArgs.
func Command(t Turn) ([]string, error) {
	if t.Exe == "" {
		return nil, i18n.New(i18n.Msg{ES: "el CLI no está instalado en este equipo", EN: "the CLI is not installed on this computer"})
	}
	args, err := BuildArgs(t)
	if err != nil {
		return nil, err
	}
	return append(Launcher(t.Exe), args...), nil
}

// environ es el entorno base del hijo: el que armó quien llama o, sin él, el del
// proceso con el PATH ampliado (ver Env). Siempre una copia.
func (t Turn) environ() []string {
	if len(t.Env) > 0 {
		return append([]string{}, t.Env...)
	}
	return Env(t.Exe)
}

// turnEnv son las variables que el turno agrega según el CLI, encima del entorno
// que armó quien llama.
func turnEnv(t Turn) []string {
	switch t.CLI {
	case "opencode":
		return opencodeEnv(t)
	}
	return nil
}

// stderrTail guarda los últimos bytes de stderr: ahí explica el CLI por qué no
// arrancó (sesión vencida, modelo inexistente), y sin eso el error sería un
// código de salida pelado.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

const stderrMax = 4096

func (s *stderrTail) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > stderrMax {
		s.buf = s.buf[len(s.buf)-stderrMax:]
	}
	return len(p), nil
}

func (s *stderrTail) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(string(s.buf))
}

// Run corre un turno y reparte sus eventos por emit, en el orden en que llegan.
// Bloquea hasta que el proceso termina (quien llama lo corre en su goroutine).
//
//   - Cancelar ctx mata el grupo de procesos (macOS) o el árbol (Windows) y
//     devuelve Result.Cancelled sin error: lo escrito hasta ahí se conserva.
//   - Un fallo que el CLI explicó con un evento propio se devuelve como error con
//     su mensaje; si murió sin explicarse, el error lleva lo que dijo por stderr.
//   - emit recibe KindStart, KindText, KindTool, KindToolResult y KindDone /
//     KindError. El razonamiento no se reparte.
func Run(ctx context.Context, t Turn, emit func(Event)) (Result, error) {
	var res Result
	if strings.TrimSpace(t.Prompt) == "" {
		return res, i18n.New(i18n.Msg{ES: "el mensaje está vacío", EN: "the message is empty"})
	}
	adapt, finish, ok := adapterFor(t.CLI)
	if !ok {
		return res, i18n.Errorf(i18n.Msg{ES: "el CLI %q no es compatible con el chat", EN: "the CLI %q is not supported by the chat"}, t.CLI)
	}
	argv, err := Command(t)
	if err != nil {
		return res, err
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = t.Cwd
	cmd.Env = append(t.environ(), turnEnv(t)...)
	prepareProc(cmd)
	// Cancelar mata TODO el árbol, no solo al CLI: un CLI de node deja vivos sus
	// servidores MCP, y matar solo el proceso directo los dejaría huérfanos.
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = killGrace

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return res, err
	}
	stderr := &stderrTail{}
	cmd.Stderr = stderr
	if t.usesStdin() {
		cmd.Stdin = strings.NewReader(t.Prompt + imageNote(t))
	}
	if err := cmd.Start(); err != nil {
		return res, i18n.Errorf(i18n.Msg{ES: "no se pudo ejecutar %q: %v", EN: "could not run %q: %v"}, argv[0], err)
	}

	// El texto llega en trozos y un trozo puede cortar un carácter por la mitad:
	// acá se vuelve a pegar (ver rawtext.go).
	var textFix utf8Stitcher
	failure := ""
	handle := func(ev Event) {
		switch ev.Kind {
		case KindThinking:
			return
		case KindText:
			// Un trozo que quedó entero en el retén no se emite: mandarlo vacío
			// haría parpadear el «escribiendo» sin texto nuevo.
			if ev.Text = textFix.Feed(ev.Text); ev.Text == "" {
				return
			}
		case KindStart:
			if ev.Model != "" {
				res.Model = ev.Model
			}
		case KindDone:
			res.Usage = ev.Usage
			if ev.Usage != nil && ev.Usage.Model != "" {
				res.Model = ev.Usage.Model
			}
		case KindError:
			failure = ev.Error
			if ev.Usage != nil {
				res.Usage = ev.Usage
			}
		}
		// El id de conversación se retiene apenas aparece, no al final: si el
		// turno se corta a la mitad, el siguiente igual encadena con lo que ya
		// se dijo en vez de empezar de cero.
		if ev.ConversationID != "" {
			res.Session = ev.ConversationID
		}
		emit(ev)
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		for _, ev := range adapt([]byte(line)) {
			handle(ev)
		}
	}
	// Lo que haya quedado a medias sale igual: un carácter incompleto es señal de
	// que el flujo se cortó, y comérselo escondería el truncado.
	if rest := textFix.Flush(); rest != "" {
		emit(Event{Kind: KindText, Text: rest})
	}
	scanErr := sc.Err()
	// Si el CLI sigue vivo pero ya no lee nadie, Wait lo espera: se vacía.
	if scanErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		res.Cancelled = true
		return res, nil
	}
	switch {
	case failure != "":
		return res, &cliError{msg: failure}
	case waitErr != nil:
		msg := stderr.String()
		if msg == "" {
			msg = waitErr.Error()
		}
		return res, &cliError{msg: msg}
	case scanErr != nil:
		return res, i18n.Errorf(i18n.Msg{ES: "se cortó la lectura de la respuesta: %v", EN: "reading the response was interrupted: %v"}, scanErr)
	}
	if finish != nil {
		for _, ev := range finish(t) {
			handle(ev)
		}
	}
	return res, nil
}

// imageNote nombra las imágenes en el prompt que viaja por stdin (el de argv ya
// las lleva en promptArg). Codex las recibe por -i.
func imageNote(t Turn) string {
	if t.CLI == "codex" {
		return ""
	}
	return withImagePaths("", t.Images)
}
