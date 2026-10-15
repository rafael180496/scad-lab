// Package workspace crea y lee las carpetas de proyecto de ScadLab.
//
// Un workspace es una carpeta normal del usuario con:
//
//	NNN-slug.scad    checkpoints inmutables (la fuente de verdad del modelo)
//	CLAUDE.md        reglas para Claude Code
//	AGENTS.md        las mismas reglas para opencode, Codex y Antigravity
//	printer.json     impresora destino y su volumen de impresión
//	lib/  exports/   librerías OpenSCAD y salidas generadas
//	.claude/skills … skills sincronizadas (ver backend/skills)
package workspace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"scadlab/assets"
	"scadlab/backend/i18n"
	"scadlab/backend/printers"
)

var (
	errNameRequired = i18n.New(i18n.Msg{ES: "el nombre del workspace es obligatorio", EN: "the workspace name is required"})
	errDirNotEmpty  = i18n.New(i18n.Msg{ES: "la carpeta ya existe y no está vacía", EN: "the folder already exists and is not empty"})
)

// Slug convierte un nombre en un nombre de carpeta seguro en Windows y macOS:
// sin acentos, minúsculas, guiones. Windows prohíbe <>:"/\|?* y nombres como
// CON o NUL; quedarse en [a-z0-9-] evita los dos problemas de raíz.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(name)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue // marca diacrítica: "é" → "e"
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s
}

// Create arma la carpeta del workspace bajo root y escribe sus archivos base.
func Create(root, name, printerID string) (string, error) {
	slug := Slug(name)
	if slug == "" {
		return "", errNameRequired
	}
	dir := filepath.Join(root, slug)
	if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
		return "", errDirNotEmpty
	}
	for _, sub := range []string{"lib", "exports"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", err
		}
	}
	if err := WriteRules(dir, name, printerID); err != nil {
		return "", err
	}
	gi, err := fs.ReadFile(assets.FS, "workspace/gitignore.tmpl")
	if err != nil {
		return "", err
	}
	if err := writeIfMissing(filepath.Join(dir, ".gitignore"), gi); err != nil {
		return "", err
	}
	return dir, nil
}

// WriteRules (re)escribe CLAUDE.md, AGENTS.md y printer.json. Se vuelve a
// llamar cuando cambia la impresora del workspace.
func WriteRules(dir, name, printerID string) error {
	p, _ := printers.ByID(printerID)
	tplRaw, err := fs.ReadFile(assets.FS, "workspace/RULES.md.tmpl")
	if err != nil {
		return err
	}
	tpl, err := template.New("rules").Parse(string(tplRaw))
	if err != nil {
		return err
	}
	slots := p.MulticolorSlots
	if slots == 0 {
		slots = 1
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, map[string]any{
		"Name": name, "PrinterName": p.Name,
		"BuildX": p.BuildVolume.X, "BuildY": p.BuildVolume.Y, "BuildZ": p.BuildVolume.Z,
		"ColorSlots": slots,
	}); err != nil {
		return err
	}
	for _, f := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	pj, err := json.MarshalIndent(map[string]any{
		"printer": p.ID, "name": p.Name, "build_volume": p.BuildVolume,
		"nozzle_mm": p.NozzleMM, "multicolor_slots": p.MulticolorSlots,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "printer.json"), append(pj, '\n'), 0o644)
}

func writeIfMissing(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

// CheckpointFile es un .scad leído del disco.
type CheckpointFile struct {
	FileName    string `json:"fileName"`
	ParentFile  string `json:"parentFile"`
	Description string `json:"description"`
	Mtime       int64  `json:"mtime"`
}

// headerRe lee `// @checkpoint parent=X desc="..."`. desc admite \" escapado.
var (
	headerRe = regexp.MustCompile(`^//\s*@checkpoint\b(.*)$`)
	parentRe = regexp.MustCompile(`\bparent=(\S*)`)
	descRe   = regexp.MustCompile(`\bdesc="((?:[^"\\]|\\.)*)"`)
	numRe    = regexp.MustCompile(`^(\d{3,})-`)
)

// Scan lee los .scad de la raíz del workspace con su encabezado.
func Scan(dir string) ([]CheckpointFile, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []CheckpointFile{}
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".scad") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cf := CheckpointFile{FileName: e.Name(), Mtime: info.ModTime().Unix()}
		if line, err := firstLine(filepath.Join(dir, e.Name())); err == nil {
			if m := headerRe.FindStringSubmatch(line); m != nil {
				if p := parentRe.FindStringSubmatch(m[1]); p != nil {
					cf.ParentFile = p[1]
				}
				if d := descRe.FindStringSubmatch(m[1]); d != nil {
					cf.Description = strings.ReplaceAll(d[1], `\"`, `"`)
				}
			}
		}
		out = append(out, cf)
	}
	return out, nil
}

func firstLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 64*1024)
	if sc.Scan() {
		return strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF")), nil
	}
	return "", sc.Err()
}

// NextFileName devuelve el siguiente NNN-slug.scad libre.
func NextFileName(dir, label string) (string, error) {
	files, err := Scan(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	max := 0
	for _, f := range files {
		if m := numRe.FindStringSubmatch(f.FileName); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > max {
				max = n
			}
		}
	}
	slug := Slug(label)
	if slug == "" {
		slug = "model"
	}
	return fmt.Sprintf("%03d-%s.scad", max+1, slug), nil
}
