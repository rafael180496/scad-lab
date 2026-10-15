package agentsessions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Claude Code guarda cada sesión en
//
//	<~/.claude>/projects/<cwd con todo lo no alfanumérico → "-">/<uuid>.jsonl
//
// (en Windows C:\Users\x → C--Users-x). Cada línea es un objeto JSON con "type":
// "user" y "assistant" llevan "message", "cwd", "sessionId", "isSidechain" y
// "isMeta"; "ai-title" lleva el título que le puso la IA. El id de la sesión
// es el nombre del archivo.

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// claudeDirName es el nombre de la carpeta de proyecto de Claude Code.
func claudeDirName(cwd string) string { return nonAlnum.ReplaceAllString(cwd, "-") }

type claudeLine struct {
	Type        string `json:"type"`
	Cwd         string `json:"cwd"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	AITitle     string `json:"aiTitle"`
	Summary     string `json:"summary"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// claudeText saca el texto del contenido de un mensaje de usuario: un string
// suelto o una lista de bloques {type:"text", text}. Devuelve el primer texto
// que escribió la persona: los bloques que empiezan por "<" son contexto que
// el IDE o Claude agregan (<ide_opened_file>, <system-reminder>,
// <command-name>…) y no sirven para reconocer la sesión.
func claudeText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return humanText(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" {
			if t := humanText(b.Text); t != "" {
				return t
			}
		}
	}
	return ""
}

// humanText descarta el texto que no escribió la persona.
func humanText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "<") || strings.HasPrefix(s, "Caveat:") || strings.HasPrefix(s, "[Request interrupted") {
		return ""
	}
	return cleanTitle(s)
}

func listClaude(ctx context.Context, dirs []string, o Options) ([]Session, error) {
	base := filepath.Join(homeDir(o, "CLAUDE_CONFIG_DIR", ".claude"), "projects")
	type file struct {
		path string
		id   string
		mod  int64
	}
	var files []file
	seen := map[string]bool{}
	for _, d := range dirs {
		proj := filepath.Join(base, claudeDirName(d))
		if seen[proj] {
			continue
		}
		seen[proj] = true
		entries, err := os.ReadDir(proj)
		if err != nil {
			continue // sin carpeta = sin sesiones
		}
		for _, e := range entries {
			id, ok := strings.CutSuffix(e.Name(), ".jsonl")
			// Los «agent-….jsonl» son subagentes, no conversaciones que se
			// puedan retomar; no pasan el formato UUID.
			if e.IsDir() || !ok || !ValidID(Claude, id) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, file{filepath.Join(proj, e.Name()), id, info.ModTime().UnixMilli()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod > files[j].mod })

	var out []Session
	for i, f := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if i >= maxScan || len(out) >= o.limit() {
			break
		}
		title, ok := claudeHead(f.path, dirs)
		if !ok {
			continue
		}
		out = append(out, Session{ID: f.id, Title: title, UpdatedAt: f.mod})
	}
	return out, nil
}

// claudeHead lee el principio del archivo. ok=false si no es una conversación
// de este workspace o no tiene ningún mensaje (claude --resume no la abre).
func claudeHead(path string, dirs []string) (title string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	lr := newLineReader(f, maxHeadBytes)
	hasUser, aiTitle, cwdChecked := false, "", false
	for {
		raw, more := lr.next()
		if !more {
			break
		}
		var l claudeLine
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		switch l.Type {
		case "ai-title":
			if aiTitle == "" {
				aiTitle = cleanTitle(l.AITitle)
			}
		case "summary":
			if aiTitle == "" {
				aiTitle = cleanTitle(l.Summary)
			}
		case "user":
			if l.IsSidechain {
				continue
			}
			hasUser = true
			// Dos carpetas distintas pueden dar el mismo nombre codificado
			// (/a/b-c y /a/b/c): el cwd guardado en la sesión desempata.
			if !cwdChecked && l.Cwd != "" {
				cwdChecked = true
				if !samePath(l.Cwd, dirs) {
					return "", false
				}
			}
			if l.IsMeta || title != "" {
				continue
			}
			title = claudeText(l.Message.Content)
		}
		if title != "" && cwdChecked {
			break
		}
	}
	if !hasUser {
		return "", false
	}
	if title == "" {
		title = aiTitle
	}
	return title, true
}
