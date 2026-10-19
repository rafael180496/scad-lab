package mcp

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"scadlab/backend/i18n"
	"scadlab/backend/prompts"
)

// Las skills que lee Hermes son las que la app ya sincronizó en
// <ws>/.claude/skills/<nombre>/SKILL.md (las incluidas y las propias del
// usuario). Este archivo solo LEE: no depende de backend/skills (que gestiona
// la sincronización y cambia aparte) ni escribe nada.

const (
	// skillsRel es la carpeta de skills dentro del workspace.
	skillsRel = ".claude/skills"
	// maxSkillFileBytes es el tope de un archivo de skill que se le entrega al
	// modelo: una skill es texto de instrucciones, y un volcado de 5 MB solo le
	// gastaría el contexto.
	maxSkillFileBytes = 256 << 10
	// maxSkillDescRunes acota la descripción que se le muestra al modelo.
	maxSkillDescRunes = 1024
	// maxSkillFilesListed acota la lista de archivos de una skill.
	maxSkillFilesListed = 60
)

// skillEntry es una skill encontrada: su carpeta y lo que dice su encabezado.
type skillEntry struct {
	dir         string // nombre de la carpeta
	name        string // `name` del encabezado (o la carpeta si falta)
	description string
	realDir     string // ruta absoluta sin enlaces, comprobada dentro del workspace
}

// ListWorkspaceSkills devuelve nombre y descripción de cada skill sincronizada
// en el workspace, ordenadas por nombre. Lo usa el prompt de Hermes para decir
// qué skills existen sin cargar su contenido. Un workspace sin skills (o una
// carpeta ilegible) da una lista vacía.
func ListWorkspaceSkills(workspaceDir string) []prompts.SkillInfo {
	root, err := filepath.EvalSymlinks(workspaceDir)
	if err != nil {
		return nil
	}
	entries := scanSkills(root)
	out := make([]prompts.SkillInfo, len(entries))
	for i, s := range entries {
		out[i] = prompts.SkillInfo{Name: s.name, Description: s.description}
	}
	return out
}

// scanSkills lee <root>/.claude/skills/*/SKILL.md. root ya viene sin enlaces
// simbólicos. Se salta lo que no es una carpeta con un SKILL.md regular dentro
// del workspace: un enlace que apunta fuera no es una skill de este proyecto.
func scanSkills(root string) []skillEntry {
	base := filepath.Join(root, filepath.FromSlash(skillsRel))
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []skillEntry
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(base, e.Name()))
		if err != nil || !within(root, real) {
			continue
		}
		if st, err := os.Stat(real); err != nil || !st.IsDir() {
			continue
		}
		md := filepath.Join(real, "SKILL.md")
		if st, err := os.Stat(md); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if r, err := filepath.EvalSymlinks(md); err != nil || !within(real, r) {
			continue
		}
		raw, err := readLimited(md, maxSkillFileBytes)
		if err != nil {
			continue
		}
		name, desc := skillHeader(raw)
		if name == "" {
			name = e.Name()
		}
		out = append(out, skillEntry{dir: e.Name(), name: name, description: desc, realDir: real})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// readLimited lee hasta max bytes de un archivo (el resto se descarta): para el
// encabezado alcanza con el principio.
func readLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

// skillHeader saca `name` y `description` del bloque YAML inicial de un
// SKILL.md. Basta un lector de `clave: valor` con escalares entre comillas y de
// bloque (`>` y `|`) y continuación por sangría: es lo que usan las skills.
func skillHeader(raw []byte) (name, desc string) {
	text := strings.TrimPrefix(string(raw), "\ufeff")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", ""
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimRight(line, " \t") == "---" {
			break
		}
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !ok || (key != "name" && key != "description") {
			continue
		}
		val = strings.TrimSpace(val)
		// Continuación: líneas con sangría (o vacías dentro de un bloque).
		var cont []string
		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			if l != "" && l[0] != ' ' && l[0] != '\t' {
				break
			}
			cont = append(cont, strings.TrimSpace(l))
			i = j
		}
		switch {
		case strings.HasPrefix(val, ">") || strings.HasPrefix(val, "|"):
			val = strings.TrimSpace(strings.Join(cont, " "))
		case len(cont) > 0:
			val = strings.TrimSpace(val + " " + strings.Join(cont, " "))
		}
		val = strings.Join(strings.Fields(unquoteYAML(val)), " ")
		if key == "name" && name == "" {
			name = val
		}
		if key == "description" && desc == "" {
			desc = val
		}
	}
	if utf8.RuneCountInString(desc) > maxSkillDescRunes {
		desc = string([]rune(desc)[:maxSkillDescRunes])
	}
	return name, desc
}

func unquoteYAML(v string) string {
	if len(v) >= 2 {
		switch {
		case v[0] == '"' && v[len(v)-1] == '"':
			return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(v[1 : len(v)-1])
		case v[0] == '\'' && v[len(v)-1] == '\'':
			return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
	}
	return v
}

// --- herramientas -----------------------------------------------------------

func (e *env) listSkills(_ context.Context, _ map[string]any) (*toolResult, error) {
	e.settings()
	type skillOut struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Folder      string `json:"folder"`
	}
	list := []skillOut{}
	for _, s := range scanSkills(e.ws) {
		list = append(list, skillOut{Name: s.name, Description: s.description, Folder: s.dir})
	}
	out := map[string]any{"skills": list}
	if len(list) == 0 {
		out["note"] = msgf(i18n.Msg{
			ES: "Este workspace no tiene skills sincronizadas en .claude/skills/.",
			EN: "This workspace has no skills synced in .claude/skills/.",
		})
	}
	return jsonResult(out)
}

func (e *env) readSkill(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	want := strings.TrimSpace(strArg(args, "name"))
	if want == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `name`: el nombre de la skill (ver list_skills)", EN: "`name` is required: the skill name (see list_skills)"})
	}
	// La skill se busca en la lista y no se arma una ruta con lo que llega: un
	// nombre con «..» o barras no puede salirse de .claude/skills/.
	var skill *skillEntry
	found := scanSkills(e.ws)
	names := make([]string, 0, len(found))
	for i := range found {
		names = append(names, found[i].name)
		if skill == nil && (found[i].dir == want || found[i].name == want) {
			skill = &found[i]
		}
	}
	if skill == nil {
		return nil, i18n.Errorf(i18n.Msg{
			ES: "skill desconocida: %q (disponibles: %s)",
			EN: "unknown skill: %q (available: %s)",
		}, want, strings.Join(names, ", "))
	}

	rel := strings.TrimSpace(strArg(args, "file"))
	if rel == "" {
		rel = "SKILL.md"
	}
	path, err := resolveSkillFile(skill.realDir, rel)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(raw[:min(len(raw), 8192)], 0) >= 0 {
		return nil, i18n.Errorf(i18n.Msg{ES: "%s no es un archivo de texto", EN: "%s is not a text file"}, rel)
	}
	text := string(raw)
	if strings.EqualFold(rel, "SKILL.md") {
		if list := skillFiles(skill.realDir); len(list) > 0 {
			text += "\n\n---\n" + msgf(i18n.Msg{
				ES: "Archivos de esta skill (léelos con read_skill name=%s file=<ruta>):\n",
				EN: "Files in this skill (read them with read_skill name=%s file=<path>):\n",
			}, skill.dir) + "- " + strings.Join(list, "\n- ")
		}
	}
	return textResult(text), nil
}

// resolveSkillFile convierte el `file` que pidió el modelo en la ruta real de un
// archivo regular DENTRO de la carpeta de la skill. Rechaza rutas absolutas,
// `..`, barras invertidas y unidades de Windows, y —con los enlaces simbólicos
// resueltos— cualquier destino que quede fuera de la skill. Tope de 256 KB.
func resolveSkillFile(skillDir, rel string) (string, error) {
	bad := func() error {
		return i18n.Errorf(i18n.Msg{
			ES: "ruta de skill inválida: %q (usa una ruta relativa dentro de la skill, p. ej. references/x.md)",
			EN: "invalid skill path: %q (use a relative path inside the skill, e.g. references/x.md)",
		}, rel)
	}
	if strings.ContainsAny(rel, "\\:\x00") || strings.HasPrefix(rel, "/") {
		return "", bad()
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", bad()
		}
	}
	path := filepath.Join(skillDir, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", i18n.Errorf(i18n.Msg{ES: "no se encontró %s en la skill", EN: "%s was not found in the skill"}, rel)
	}
	if !within(skillDir, real) || real == skillDir {
		return "", i18n.Errorf(i18n.Msg{ES: "%s apunta fuera de la skill", EN: "%s points outside the skill"}, rel)
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return "", i18n.Errorf(i18n.Msg{ES: "%s no es un archivo", EN: "%s is not a file"}, rel)
	}
	if st.Size() > maxSkillFileBytes {
		return "", i18n.Errorf(i18n.Msg{
			ES: "%s es demasiado grande (%d KB; máximo %d KB)",
			EN: "%s is too large (%d KB; maximum %d KB)",
		}, rel, st.Size()>>10, maxSkillFileBytes>>10)
	}
	return real, nil
}

// skillFiles lista los archivos de la skill (rutas relativas con «/») salvo su
// SKILL.md, sin seguir enlaces simbólicos. Tope de maxSkillFilesListed.
func skillFiles(skillDir string) []string {
	var out []string
	_ = filepath.WalkDir(skillDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if p != skillDir && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(skillDir, p)
		if err != nil || rel == "SKILL.md" {
			return nil
		}
		if len(out) >= maxSkillFilesListed {
			return fs.SkipAll
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}
