package agentsessions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Codex guarda cada sesión en
//
//	<~/.codex>/sessions/AAAA/MM/DD/rollout-<fecha>-<uuid>.jsonl
//
// Cada línea es {"timestamp","type","payload"}. La primera es "session_meta"
// con payload.id (el uuid que acepta `codex resume`), payload.cwd y
// payload.source ("exec" = ejecución no interactiva, que `codex resume` no
// ofrece por defecto). El primer mensaje del usuario es el primer
// "event_msg" con payload.type = "user_message" (payload.message).

type codexLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Cwd     string `json:"cwd"`
		Source  string `json:"source"`
		Message string `json:"message"`
	} `json:"payload"`
}

func listCodex(ctx context.Context, dirs []string, o Options) ([]Session, error) {
	root := filepath.Join(homeDir(o, "CODEX_HOME", ".codex"), "sessions")
	// Año/mes/día/archivo, de lo más nuevo a lo más viejo: el nombre lleva la
	// fecha, así que ordenar por nombre descendente es ordenar por tiempo y
	// el listado se corta en cuanto junta suficientes del workspace.
	var out []Session
	scanned := 0
	for _, y := range sortedDesc(root) {
		for _, m := range sortedDesc(filepath.Join(root, y)) {
			for _, d := range sortedDesc(filepath.Join(root, y, m)) {
				day := filepath.Join(root, y, m, d)
				for _, name := range sortedDesc(day) {
					if ctx.Err() != nil {
						return out, ctx.Err()
					}
					if !strings.HasSuffix(name, ".jsonl") {
						continue
					}
					if scanned++; scanned > maxScan {
						return sortByRecent(out), nil
					}
					if s, ok := codexSession(filepath.Join(day, name), dirs); ok {
						out = append(out, s)
					}
				}
				if len(out) >= o.limit() {
					return sortByRecent(out)[:o.limit()], nil
				}
			}
		}
	}
	return sortByRecent(out), nil
}

func sortByRecent(s []Session) []Session {
	sort.SliceStable(s, func(i, j int) bool { return s[i].UpdatedAt > s[j].UpdatedAt })
	return s
}

// sortedDesc lista los nombres de una carpeta de mayor a menor ([] si no existe).
func sortedDesc(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

func codexSession(path string, dirs []string) (Session, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, false
	}
	defer f.Close()
	lr := newLineReader(f, maxHeadBytes)
	first, ok := lr.next()
	if !ok {
		return Session{}, false
	}
	var meta codexLine
	if json.Unmarshal(first, &meta) != nil || meta.Type != "session_meta" {
		return Session{}, false
	}
	if meta.Payload.Source == "exec" || !samePath(meta.Payload.Cwd, dirs) || !ValidID(Codex, meta.Payload.ID) {
		return Session{}, false
	}
	title := ""
	for title == "" {
		raw, more := lr.next()
		if !more {
			break
		}
		var l codexLine
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		if l.Type == "event_msg" && l.Payload.Type == "user_message" {
			title = humanText(l.Payload.Message)
		}
	}
	info, err := f.Stat()
	if err != nil {
		return Session{}, false
	}
	return Session{ID: meta.Payload.ID, Title: title, UpdatedAt: info.ModTime().UnixMilli()}, true
}
