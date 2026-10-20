// Package workspace crea y lee las carpetas de proyecto de ScadLab.
//
// Un workspace es una carpeta normal del usuario con:
//
//	NNN-slug.scad    checkpoints (la fuente de verdad del modelo; se reescriben solo con respaldo en .scadlab/history)
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
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"scadlab/assets"
	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/slicers"
)

var (
	errNameRequired = i18n.New(i18n.Msg{ES: "el nombre del workspace es obligatorio", EN: "the workspace name is required"})
	// ErrParentNotAbs: la ubicación de un workspace nuevo tiene que ser una ruta
	// absoluta. Una relativa se resolvería contra el directorio de trabajo del
	// proceso (en una app gráfica, algo impredecible) y «~» no lo expande nadie.
	ErrParentNotAbs = i18n.New(i18n.Msg{
		ES: "la ubicación debe ser una ruta absoluta (no relativa ni con «~»)",
		EN: "the location must be an absolute path (not relative, no \"~\")",
	})
)

// errDirNotEmpty nombra la carpeta: «ya existe» sin decir cuál obliga a adivinar
// qué nombre y qué ubicación produjeron el choque.
func errDirNotEmpty(dir string) error {
	return i18n.Errorf(i18n.Msg{
		ES: "la carpeta %q ya existe y no está vacía: elige otro nombre u otra ubicación",
		EN: "the folder %q already exists and is not empty: pick another name or location",
	}, dir)
}

func errParentNotDir(parent string) error {
	return i18n.Errorf(i18n.Msg{
		ES: "la ubicación %q existe pero no es una carpeta",
		EN: "the location %q exists but is not a folder",
	}, parent)
}

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

// TargetDir es la carpeta final de un workspace: <parent>/<slug del nombre>. Es la
// misma cuenta que hace Create, así la vista previa del diálogo «Nuevo
// workspace» nunca difiere de lo que se crea. Devuelve "" si el nombre no
// produce un slug o si parent no es una ruta absoluta.
func TargetDir(parent, name string) string {
	slug := Slug(name)
	if slug == "" || !filepath.IsAbs(parent) {
		return ""
	}
	return filepath.Join(parent, slug)
}

// Create arma la carpeta del workspace bajo root (que se crea si no existe) y
// escribe sus archivos base. root tiene que ser absoluta; el destino
// <root>/<slug> puede existir solo si está vacío. p es la impresora EFECTIVA del
// workspace (wsconfig.EffectivePrinter): con sus colores ya elegidos.
func Create(root, name string, p printers.Printer) (string, error) {
	slug := Slug(name)
	if slug == "" {
		return "", errNameRequired
	}
	if !filepath.IsAbs(root) {
		return "", ErrParentNotAbs
	}
	if st, err := os.Stat(root); err == nil && !st.IsDir() {
		return "", errParentNotDir(root)
	}
	dir := filepath.Join(root, slug)
	if st, err := os.Stat(dir); err == nil {
		if !st.IsDir() {
			return "", errDirNotEmpty(dir)
		}
		if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
			return "", errDirNotEmpty(dir)
		}
	}
	for _, sub := range []string{"lib", "exports"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", err
		}
	}
	if err := WriteRules(dir, name, p); err != nil {
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

// Marcas del bloque de reglas que gestiona ScadLab dentro de CLAUDE.md y
// AGENTS.md. Solo se reescribe lo que está entre ellas: lo que el usuario
// agregue antes o después queda intacto. Sin esto, actualizar las reglas (MCP,
// ramas, papelera…) en un workspace viejo obligaba a elegir entre dejar al
// agente con reglas desactualizadas o pisar lo que el usuario hubiera escrito.
const (
	rulesBegin = "<!-- scadlab:rules:begin — ScadLab actualiza este bloque solo; escribe lo tuyo fuera de él -->"
	rulesEnd   = "<!-- scadlab:rules:end -->"
	// legacyTitleSuffix identifica un archivo generado por ScadLab antes de
	// las marcas: su primera línea era «# <nombre> — workspace de ScadLab».
	legacyTitleSuffix = "— workspace de ScadLab"
)

// mergeRules escribe las reglas en dir/name respetando el contenido ajeno:
//   - sin archivo: lo crea con el bloque;
//   - con marcas: reemplaza solo el bloque (sin tocar el archivo si no cambió);
//   - generado por una versión anterior (sin marcas, con nuestro título): lo
//     copia a .scadlab/backup/ por si el usuario lo editó, y lo reemplaza;
//   - un archivo del usuario sin marcas (p. ej. una carpeta importada): agrega
//     el bloque arriba y conserva todo su contenido debajo.
func mergeRules(dir, name, rules string) error {
	path := filepath.Join(dir, name)
	block := rulesBegin + "\n" + strings.TrimRight(rules, "\n") + "\n" + rulesEnd + "\n"
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return writeFileAtomic(path, []byte(block))
	}
	if err != nil {
		return err
	}
	cur := string(raw)
	var next string
	switch b, e := strings.Index(cur, rulesBegin), strings.Index(cur, rulesEnd); {
	case b >= 0 && e > b:
		next = cur[:b] + strings.TrimRight(block, "\n") + cur[e+len(rulesEnd):]
	case strings.Contains(firstLineOf(cur), legacyTitleSuffix):
		if err := backupRules(dir, name, raw); err != nil {
			return err
		}
		next = block
	default:
		next = block + "\n" + cur
	}
	if next == cur {
		return nil
	}
	return writeFileAtomic(path, []byte(next))
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// backupRules guarda la versión anterior en .scadlab/backup/<nombre>.<fecha>.
func backupRules(dir, name string, raw []byte) error {
	bdir := filepath.Join(dir, ".scadlab", "backup")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	return os.WriteFile(filepath.Join(bdir, name+"."+stamp), raw, 0o644)
}

// writeFileAtomic escribe con temporal + rename en la misma carpeta, para que
// un agente que lee el archivo nunca vea uno a medio escribir.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// WriteRules (re)escribe CLAUDE.md, AGENTS.md y printer.json. Se vuelve a
// llamar cuando cambia la impresora o la configuración del workspace. p es la
// impresora EFECTIVA (wsconfig.EffectivePrinter): su MulticolorSlots son los
// colores que eligió ese workspace y MaxColors el tope de la máquina.
func WriteRules(dir, name string, p printers.Printer) error {
	tplRaw, err := fs.ReadFile(assets.FS, "workspace/RULES.md.tmpl")
	if err != nil {
		return err
	}
	tpl, err := template.New("rules").Parse(string(tplRaw))
	if err != nil {
		return err
	}
	data := rulesData(name, p)
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return err
	}
	for _, f := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := mergeRules(dir, f, buf.String()); err != nil {
			return err
		}
	}
	// multicolor_slots conserva su clave de siempre con los colores del workspace;
	// colors es lo mismo con el nombre que lee la IA, y max_colors el tope.
	pj, err := json.MarshalIndent(map[string]any{
		"printer": p.ID, "name": p.Name, "brand": p.BrandName, "model": p.Model,
		"build_volume": p.BuildVolume, "nozzle_mm": p.NozzleMM,
		"multicolor_slots": data["ColorSlots"], "colors": data["ColorSlots"], "max_colors": data["MaxColors"],
		"multicolor_system":    p.MulticolorSys,
		"filament_diameter_mm": data["FilamentDiameter"], "slicer": data["Slicer"],
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "printer.json"), append(pj, '\n'), 0o644)
}

// rulesData son las variables de RULES.md.tmpl. Las que no vienen del catálogo
// (colores, slicer) se resuelven aquí una sola vez para que las reglas y
// printer.json digan lo mismo. Una plantilla que no nombra alguna no cambia.
func rulesData(name string, p printers.Printer) map[string]any {
	colors := max(p.MulticolorSlots, 1)
	slicer := ""
	if d, ok := slicers.Recommended(p.Slicers); ok {
		slicer = d.Name
	}
	filament := p.FilamentDiameterMM
	if filament <= 0 {
		filament = 1.75
	}
	return map[string]any{
		"Name": name, "PrinterName": p.Name,
		"BuildX": p.BuildVolume.X, "BuildY": p.BuildVolume.Y, "BuildZ": p.BuildVolume.Z,
		"ColorSlots": colors,
		"Brand":      p.BrandName, "PrinterModel": p.Model, "MulticolorSystem": p.MulticolorSys,
		"MaxColors": max(p.MaxColors, colors), "NozzleMM": p.NozzleMM, "FilamentDiameter": filament,
		"Slicer": slicer, "BrandSkill": p.Skill,
	}
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

// descUnescape deshace el escapado que escriben SaveAsCheckpoint y los
// agentes: \" y \\ (un NewReplacer recorre una sola vez, así \\" no se
// desescapa dos veces).
var descUnescape = strings.NewReplacer(`\"`, `"`, `\\`, `\`)

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
					cf.Description = descUnescape.Replace(d[1])
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
