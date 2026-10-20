package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"scadlab/backend/i18n"
	"scadlab/backend/mcp"
)

// runMCP es el modo `ScadLab --mcp --workspace <dir>` (S6): un servidor MCP por
// stdio para los agentes. Corre ANTES de wails.Run y nunca abre la ventana ni
// toca nada de la GUI: es un proceso aparte, lanzado por el agente, que lee la
// configuración de scadlab.db y trabaja sobre los archivos del workspace.
//
// stdout es del protocolo: solo JSON-RPC. Todo lo demás —el uso, los errores,
// los logs— va a stderr. El idioma de los mensajes lo fija el propio servidor
// desde settings.language de la base (mcp.Serve lo relee en cada llamada, así
// un cambio en la app se nota sin reiniciar el agente).
//
// En Windows el .exe es de subsistema gráfico, pero un proceso hijo hereda los
// pipes que le abre quien lo lanza (Claude Code, opencode…), así que stdin y
// stdout funcionan; es lo mismo que hace mini-tools.
func runMCP(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scadlab --mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", "", i18n.T(i18n.Msg{
		ES: "carpeta del workspace (por defecto, el directorio actual)",
		EN: "workspace folder (defaults to the current directory)",
	}))
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// La carpeta: --workspace, si no SCADLAB_WORKSPACE (el chat la pone en los turnos
	// de Antigravity, cuya entrada global no lleva carpeta) y, si no, el directorio
	// actual. Se deja dicho en stderr cuál valió: es lo primero que se mira cuando una
	// herramienta trabaja en una carpeta que no era.
	dir, origin := *workspace, "--workspace"
	if dir == "" {
		if env := strings.TrimSpace(os.Getenv(mcp.WorkspaceEnv)); env != "" {
			dir, origin = env, mcp.WorkspaceEnv
		} else {
			dir, origin = "", "cwd"
			if wd, err := os.Getwd(); err == nil {
				dir = wd
			}
		}
	}
	fmt.Fprintf(stderr, "scadlab --mcp: workspace %s (%s)\n", dir, origin)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := mcp.Serve(ctx, stdin, stdout, mcp.Options{
		WorkspaceDir: dir,
		Version:      appVersion,
		Log:          stderr,
	})
	if err != nil && ctx.Err() == nil {
		fmt.Fprintf(stderr, "scadlab --mcp: %v\n", err)
		return 1
	}
	return 0
}
