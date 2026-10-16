// Package checkpoint crea y nombra checkpoints: la lógica que comparten el
// binding SaveAsCheckpoint (editor) y la herramienta create_checkpoint del
// servidor MCP (S6). Vive en un paquete aparte para que las dos puntas escriban
// exactamente el mismo formato y respeten la misma regla: un checkpoint nunca se
// sobrescribe.
package checkpoint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/watch"
	"scadlab/backend/workspace"
)

// maxAttempts es cuántos números se prueban si otro proceso (el agente) toma el
// nombre entre que se calcula y se crea.
const maxAttempts = 50

// BeforeCreate es un gancho para los tests: simula que otro proceso crea el
// archivo justo antes que nosotros. Nunca se asigna fuera de un test.
var BeforeCreate func(path string)

var numRe = regexp.MustCompile(`^(\d{3,})-(.*)$`)

// ValidName valida igual que ReadCheckpoint: un nombre suelto, sin carpetas,
// terminado en .scad. Así ni la UI ni un agente pueden hacer correr OpenSCAD
// —ni escribir RENDER_ERRORS.md— fuera del workspace. Rechaza también los
// caracteres de control (NUL incluido): ni un nombre ni el archivo de padre
// activo deben poder esconder nada.
func ValidName(fileName string) error {
	if fileName == "" || strings.EqualFold(fileName, ".scad") || fileName != filepath.Base(fileName) || !strings.EqualFold(filepath.Ext(fileName), ".scad") || strings.ContainsFunc(fileName, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return i18n.New(i18n.Msg{ES: "nombre de checkpoint inválido", EN: "invalid checkpoint name"})
	}
	return nil
}

// Latest es el .scad de número más alto del workspace ("" si no hay).
func Latest(dir string) string {
	files, err := workspace.Scan(dir)
	if err != nil {
		return ""
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.FileName
	}
	return watch.Newest(names)
}

// Create guarda source como un checkpoint NUEVO hijo de parentFile (los
// checkpoints son inmutables: nunca sobrescribe). Escribe el encabezado
// `// @checkpoint parent=… desc="…"` —reemplaza la primera línea si el código ya
// trae uno— y devuelve el nombre creado.
func Create(dir, parentFile, label, description, source string) (string, error) {
	if err := ValidName(parentFile); err != nil {
		return "", err
	}
	if st, err := os.Stat(filepath.Join(dir, parentFile)); err != nil || st.IsDir() {
		return "", i18n.Errorf(i18n.Msg{ES: "el checkpoint padre %s no existe", EN: "the parent checkpoint %s does not exist"}, parentFile)
	}
	name, err := create(dir, parentFile, label, description, source)
	if err != nil {
		return "", err
	}
	// El padre activo avanza solo: si el usuario estaba parado en parentFile, el
	// hijo que acaba de nacer es ahora donde está. Es una preferencia, no parte
	// del checkpoint: si no se puede guardar (un .scadlab roto, disco lleno) el
	// checkpoint ya existe y no se le niega al llamador por eso.
	_ = Advance(dir, parentFile, name)
	return name, nil
}

// CreateRoot crea el primer checkpoint de un workspace (`parent=` vacío). Falla
// si el workspace ya tiene alguno: una raíz nueva en un árbol que existe es casi
// siempre un olvido del padre.
func CreateRoot(dir, label, description, source string) (string, error) {
	if Latest(dir) != "" {
		return "", i18n.New(i18n.Msg{
			ES: "el workspace ya tiene checkpoints: indica el padre del nuevo",
			EN: "the workspace already has checkpoints: give the parent of the new one",
		})
	}
	return create(dir, "", label, description, source)
}

func create(dir, parentFile, label, description, source string) (string, error) {
	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = strings.TrimSpace(label)
	}
	content := WithHeader(source, parentFile, desc)

	name, err := workspace.NextFileName(dir, label)
	if err != nil {
		return "", err
	}
	m := numRe.FindStringSubmatch(name)
	if m == nil { // NextFileName siempre da NNN-slug.scad; por las dudas
		return "", i18n.Errorf(i18n.Msg{ES: "nombre de checkpoint inesperado: %s", EN: "unexpected checkpoint name: %s"}, name)
	}
	n, _ := strconv.Atoi(m[1])
	rest := m[2]
	for attempt := 0; attempt < maxAttempts; attempt++ {
		name = fmt.Sprintf("%03d-%s", n+attempt, rest)
		path := filepath.Join(dir, name)
		if BeforeCreate != nil {
			BeforeCreate(path)
		}
		// O_EXCL: si el agente creó ese mismo nombre en el medio, falla en vez
		// de pisarlo, y se prueba el número siguiente.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(content)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			// El archivo es nuestro y quedó a medias: un checkpoint truncado
			// sería peor que ninguno.
			_ = os.Remove(path)
			if werr != nil {
				return "", werr
			}
			return "", cerr
		}
		return name, nil
	}
	return "", i18n.New(i18n.Msg{ES: "no se encontró un nombre libre para el checkpoint", EN: "could not find a free name for the checkpoint"})
}

var headerLineRe = regexp.MustCompile(`^\s*//\s*@checkpoint\b`)

// WithHeader pone el encabezado en la primera línea: reemplaza el que haya o lo
// antepone. Respeta los finales de línea \r\n del código.
func WithHeader(source, parent, desc string) string {
	source = strings.TrimPrefix(source, "\uFEFF")
	nl := "\n"
	if strings.Contains(source, "\r\n") {
		nl = "\r\n"
	}
	header := Header(parent, desc)
	first, rest, found := strings.Cut(source, "\n")
	if headerLineRe.MatchString(strings.TrimRight(first, "\r")) {
		if !found {
			return header + nl
		}
		return header + nl + rest
	}
	return header + nl + source
}

// Header arma `// @checkpoint parent=X desc="…"`. La descripción va en una sola
// línea y con \ y " escapados: un " suelto cortaría desc antes de tiempo para
// workspace.Scan.
func Header(parent, desc string) string {
	desc = strings.Join(strings.Fields(desc), " ")
	desc = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(desc)
	return fmt.Sprintf(`// @checkpoint parent=%s desc="%s"`, parent, desc)
}
