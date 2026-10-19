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

// winReserved son los nombres que Windows no deja usar como carpeta.
var winReserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true, "com1": true, "com2": true, "com3": true, "com4": true, "lpt1": true, "lpt2": true, "lpt3": true}

// CheckName valida el nombre de una skill propia: kebab-case, sin nombres
// reservados de Windows ni el de la plantilla.
func CheckName(name string) error {
	if !nameRe.MatchString(name) || winReserved[name] {
		return errBadName
	}
	return nil
}

var (
	errBadName = i18n.New(i18n.Msg{
		ES: "nombre de skill inválido: usa minúsculas, números y guiones (kebab-case)",
		EN: "invalid skill name: use lowercase letters, digits and hyphens (kebab-case)",
	})
	errExists = i18n.New(i18n.Msg{ES: "ya existe una skill con ese nombre", EN: "a skill with that name already exists"})
)

// UserDir es la carpeta de skills propias por defecto: <datos>/skills. El
// usuario puede apuntar a otra (settings.skillsDir); quien llama a Discover o a
// CreateFromTemplate pasa la vigente.
func UserDir(dataDir string) string { return filepath.Join(dataDir, "skills") }

// Discover lista las skills incluidas y las del usuario (sin la plantilla).
// userDir es la carpeta de skills propias; si no existe, solo hay incluidas.
func Discover(userDir string) ([]Skill, error) {
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

	udir := userDir
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

// CreateFromTemplate copia _skill-template a <userDir>/<name>, ajusta el
// `name:` del frontmatter y deja la descripción como pendiente (la de la
// plantilla decía «no es una skill operativa»). Devuelve la carpeta creada.
func CreateFromTemplate(userDir, name string) (string, error) {
	if err := CheckName(name); err != nil {
		return "", err
	}
	dst := filepath.Join(userDir, name)
	if _, err := os.Lstat(dst); err == nil {
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
			raw = setFrontmatterField(raw, "description", DescriptionPlaceholder)
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
			writeMarker(target, s.Source)
		}
	}
	return errors.Join(errs...)
}

// managedMarker marca una carpeta de skill como instalada por ScadLab. Guarda
// el origen (builtin | user) y, desde que se puede borrar una skill propia, el
// hash del contenido tal como se instaló: con él se sabe si alguien editó la
// copia a mano.
const managedMarker = ".scadlab-managed"

func isManaged(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, managedMarker))
	return err == nil
}

func writeMarker(dir, source string) {
	body := source + "\n"
	if h, err := treeHash(dir); err == nil {
		body += "sha256:" + h + "\n"
	}
	_ = os.WriteFile(filepath.Join(dir, managedMarker), []byte(body), 0o644)
}

// readMarker devuelve el origen y el hash de instalación de una copia
// gestionada ("" si no hay marca o es de una versión anterior, sin hash).
func readMarker(dir string) (source, hash string) {
	raw, err := os.ReadFile(filepath.Join(dir, managedMarker))
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	source = strings.TrimSpace(lines[0])
	for _, l := range lines[1:] {
		if h, ok := strings.CutPrefix(strings.TrimSpace(l), "sha256:"); ok {
			hash = h
		}
	}
	return source, hash
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
