// Package skills gestiona las skills que ScadLab instala en cada workspace.
//
// Dos orígenes:
//   - builtin: embebidas en el binario (assets/skills). Se actualizan con la app.
//   - user: creadas por el usuario en <datos>/skills/<nombre>, a partir de la
//     plantilla _skill-template. Una skill de usuario con el mismo nombre que
//     una incluida la reemplaza: así se puede ajustar una sin recompilar.
//
// Sync copia las habilitadas a las carpetas que lee cada CLI agéntico.
package skills

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"scadlab/assets"
	"scadlab/backend/i18n"
)

// TemplateName es la skill plantilla: no se instala en los workspaces, solo
// se copia para crear skills nuevas.
const TemplateName = "_skill-template"

// AgentDirs son las carpetas de skills de cada CLI, relativas al workspace.
// Claude Code lee .claude/skills; Codex .agents/skills; opencode
// .opencode/skills (y también .claude/skills); Antigravity .agent/skills.
var AgentDirs = []string{
	filepath.Join(".claude", "skills"),
	filepath.Join(".agents", "skills"),
	filepath.Join(".opencode", "skills"),
	filepath.Join(".agent", "skills"),
}

// Skill es una skill descubierta.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "builtin" | "user"
	Dir         string `json:"dir"`    // ruta en disco (user) o en el embed (builtin)
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

var (
	errBadName = i18n.New(i18n.Msg{
		ES: "nombre de skill inválido: usá minúsculas, números y guiones (kebab-case)",
		EN: "invalid skill name: use lowercase letters, digits and hyphens (kebab-case)",
	})
	errExists = i18n.New(i18n.Msg{ES: "ya existe una skill con ese nombre", EN: "a skill with that name already exists"})
)

// UserDir es <datos>/skills.
func UserDir(dataDir string) string { return filepath.Join(dataDir, "skills") }

// Discover lista las skills incluidas y las del usuario (sin la plantilla).
func Discover(dataDir string) ([]Skill, error) {
	byName := map[string]Skill{}
	entries, err := fs.ReadDir(assets.FS, "skills")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == TemplateName {
			continue
		}
		raw, err := fs.ReadFile(assets.FS, path.Join("skills", e.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		_, desc := frontmatter(raw)
		byName[e.Name()] = Skill{Name: e.Name(), Description: desc, Source: "builtin", Dir: path.Join("skills", e.Name())}
	}

	udir := UserDir(dataDir)
	if ents, err := os.ReadDir(udir); err == nil {
		for _, e := range ents {
			if !e.IsDir() || e.Name() == TemplateName {
				continue
			}
			dir := filepath.Join(udir, e.Name())
			raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if err != nil {
				continue
			}
			_, desc := frontmatter(raw)
			byName[e.Name()] = Skill{Name: e.Name(), Description: desc, Source: "user", Dir: dir}
		}
	}

	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateFromTemplate copia _skill-template a <datos>/skills/<name> y ajusta
// el `name:` del frontmatter. Devuelve la carpeta creada.
func CreateFromTemplate(dataDir, name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", errBadName
	}
	dst := filepath.Join(UserDir(dataDir), name)
	if _, err := os.Stat(dst); err == nil {
		return "", errExists
	}
	src := path.Join("skills", TemplateName)
	err := fs.WalkDir(assets.FS, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, src), "/")
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := fs.ReadFile(assets.FS, p)
		if err != nil {
			return err
		}
		if rel == "SKILL.md" {
			raw = setFrontmatterName(raw, name)
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	return dst, nil
}

// Sync instala las skills habilitadas en las carpetas de cada CLI del
// workspace. Reescribe solo las skills que gestiona ScadLab (las que están en
// `managed`); una skill que el usuario agregó a mano en el workspace no se toca.
func Sync(workspaceDir string, list []Skill, enabled func(name string) bool) error {
	var errs []error
	for _, rel := range AgentDirs {
		base := filepath.Join(workspaceDir, rel)
		if err := os.MkdirAll(base, 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		for _, s := range list {
			target := filepath.Join(base, s.Name)
			if !enabled(s.Name) {
				// Apagada: se retira la copia gestionada, si la había.
				if isManaged(target) {
					_ = os.RemoveAll(target)
				}
				continue
			}
			if err := os.RemoveAll(target); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := copySkill(s, target); err != nil {
				errs = append(errs, err)
				continue
			}
			_ = os.WriteFile(filepath.Join(target, managedMarker), []byte(s.Source+"\n"), 0o644)
		}
	}
	return errors.Join(errs...)
}

// managedMarker marca una carpeta de skill como instalada por ScadLab.
const managedMarker = ".scadlab-managed"

func isManaged(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, managedMarker))
	return err == nil
}

func copySkill(s Skill, target string) error {
	if s.Source == "builtin" {
		return fs.WalkDir(assets.FS, s.Dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(p, s.Dir), "/")
			dst := filepath.Join(target, filepath.FromSlash(rel))
			if d.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			raw, err := fs.ReadFile(assets.FS, p)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, raw, 0o644)
		})
	}
	return filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Dir, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, raw, 0o644)
	})
}

// frontmatter extrae name y description del bloque YAML inicial. Basta con un
// lector de líneas `clave: valor`: el formato de SKILL.md no usa YAML anidado
// en esas dos claves.
func frontmatter(raw []byte) (name, desc string) {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	inside := false
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "---" {
			if inside {
				break
			}
			inside = true
			continue
		}
		if !inside {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			desc = v
		}
	}
	return name, desc
}

var nameLineRe = regexp.MustCompile(`(?m)^name:.*$`)

func setFrontmatterName(raw []byte, name string) []byte {
	// Solo la primera: una línea "name:" en el cuerpo de la skill no es la del
	// frontmatter.
	loc := nameLineRe.FindIndex(raw)
	if loc == nil {
		return raw
	}
	out := append([]byte{}, raw[:loc[0]]...)
	out = append(out, "name: "+name...)
	return append(out, raw[loc[1]:]...)
}
