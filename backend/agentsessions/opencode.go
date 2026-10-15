package agentsessions

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"time"

	"scadlab/backend/i18n"
)

// opencode no guarda sesiones en archivos que convenga leer (usa una base
// SQLite propia con esquema interno): se le pregunta con su comando,
//
//	opencode session list --format json -n <N>
//
// que imprime un arreglo de {id, title?, updated, created, projectId,
// directory} con las fechas en milisegundos. El comando lista las del
// «proyecto» actual (la raíz git, que puede contener varias carpetas), por eso
// además se filtra por directory == carpeta del workspace.

type opencodeSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Updated   int64  `json:"updated"`
	Created   int64  `json:"created"`
	Directory string `json:"directory"`
}

const opencodeTimeout = 20 * time.Second

func listOpenCode(ctx context.Context, dirs []string, workspaceDir string, o Options) ([]Session, error) {
	if o.Exe == "" {
		return nil, i18n.New(i18n.Msg{ES: "no se encontró el ejecutable de opencode", EN: "the opencode executable was not found"})
	}
	run := o.Run
	if run == nil {
		run = runCommand
	}
	ctx, cancel := context.WithTimeout(ctx, opencodeTimeout)
	defer cancel()
	// Se pide más de lo que se muestra: el comando trae sesiones de todo el
	// proyecto y el filtro por carpeta descarta parte.
	raw, err := run(ctx, workspaceDir, o.Exe, "session", "list", "--format", "json", "-n", strconv.Itoa(o.limit()*3))
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "opencode no pudo listar las sesiones: %v", EN: "opencode could not list the sessions: %v"}, err)
	}
	return parseOpenCode(raw, dirs, o.limit())
}

// parseOpenCode interpreta la salida del comando. Tolera texto antes del
// arreglo (avisos del servicio de fondo) buscando el primer «[».
func parseOpenCode(raw []byte, dirs []string, limit int) ([]Session, error) {
	if i := bytes.IndexByte(raw, '['); i > 0 {
		raw = raw[i:]
	}
	var list []opencodeSession
	if err := json.Unmarshal(bytes.TrimSpace(raw), &list); err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "respuesta de opencode ilegible: %v", EN: "unreadable opencode response: %v"}, err)
	}
	out := []Session{}
	for _, s := range list {
		if !ValidID(OpenCode, s.ID) || !samePath(s.Directory, dirs) {
			continue
		}
		at := s.Updated
		if at == 0 {
			at = s.Created
		}
		out = append(out, Session{ID: s.ID, Title: cleanTitle(s.Title), UpdatedAt: at})
	}
	out = sortByRecent(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func runCommand(ctx context.Context, dir, exe string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	hideWindow(cmd)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}
