package checkpoint

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"scadlab/backend/i18n"
)

// reparentMu serializa las reescrituras de cabecera dentro del proceso (la app y
// el MCP corren cada uno en el suyo; entre procesos la escritura atómica basta:
// un lector ve la cabecera vieja o la nueva).
var reparentMu sync.Mutex

// parentTokenRe es el `parent=X` de la cabecera. Es el mismo patrón con el que
// workspace.Scan lee el padre, y se busca sobre lo que sigue a `@checkpoint`,
// así que escribir aquí y leer allá siempre coinciden.
var parentTokenRe = regexp.MustCompile(`\bparent=(\S*)`)

// SetParent cambia el `parent=` de la cabecera `@checkpoint` de file. "" lo deja
// como raíz.
//
// Es una de las excepciones a «un checkpoint no se reescribe sin respaldo»
// (regla 3 de technical.md; las otras reescrituras, update_checkpoint y
// RestoreVersion, guardan antes la versión anterior en el historial): el padre
// es metadato del árbol, no el modelo, y sin poder
// moverlo borrar un checkpoint dejaría a sus hijos apuntando a un archivo que ya
// no está. Por eso solo toca la primera línea:
//
//   - el resto del archivo se conserva byte a byte (código, finales de línea,
//     BOM) y dentro de la cabecera solo cambia el valor de `parent=`: la
//     descripción y los espacios quedan como estaban;
//   - la escritura es atómica (temporal en la misma carpeta + rename), y el
//     archivo conserva sus permisos y su fecha de modificación: cambiar de rama
//     no es «haberlo editado» y la fecha que muestra el árbol es la de cuando
//     se creó;
//   - un enlace simbólico no se reescribe (el rename lo reemplazaría por un
//     archivo normal y el destino quedaría sin tocar).
//
// Si el padre ya es newParent no escribe nada. El padre nuevo no tiene que
// existir (restaurar puede traer de vuelta a un padre ausente), pero sí ser un
// nombre válido sin espacios, porque `parent=` termina en el primer espacio.
func SetParent(wsDir, file, newParent string) error {
	if err := ValidName(file); err != nil {
		return err
	}
	if newParent != "" {
		if err := ValidName(newParent); err != nil || strings.ContainsFunc(newParent, unicode.IsSpace) {
			return i18n.New(i18n.Msg{ES: "nombre de checkpoint padre inválido", EN: "invalid parent checkpoint name"})
		}
		if newParent == file {
			return i18n.New(i18n.Msg{ES: "un checkpoint no puede ser padre de sí mismo", EN: "a checkpoint cannot be its own parent"})
		}
	}
	reparentMu.Lock()
	defer reparentMu.Unlock()

	path := filepath.Join(wsDir, file)
	st, err := os.Lstat(path)
	if err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se encontró el checkpoint %s", EN: "checkpoint %s was not found"}, file)
	}
	if !st.Mode().IsRegular() {
		return i18n.Errorf(i18n.Msg{
			ES: "%s no es un archivo normal (¿es un enlace?): no cambio su padre por seguridad",
			EN: "%s is not a plain file (is it a link?): not changing its parent for safety",
		}, file)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, changed, err := withParent(raw, newParent)
	if err != nil {
		return i18n.Errorf(i18n.Msg{ES: "%s: %w", EN: "%s: %w"}, file, err)
	}
	if !changed {
		return nil
	}
	return replaceKeepingStat(path, out, st)
}

// withParent devuelve raw con el valor de `parent=` de la primera línea
// reemplazado. changed=false si ya era ese. Solo cambia esos bytes.
func withParent(raw []byte, newParent string) (out []byte, changed bool, err error) {
	start := 0
	if bytes.HasPrefix(raw, []byte("\xEF\xBB\xBF")) {
		start = 3
	}
	end := bytes.IndexByte(raw[start:], '\n')
	if end < 0 {
		end = len(raw)
	} else {
		end += start
	}
	line := raw[start:end]
	loc := headerLineRe.FindIndex(line)
	if loc == nil {
		return nil, false, i18n.New(i18n.Msg{ES: "no tiene el encabezado @checkpoint en la primera línea", EN: "it has no @checkpoint header on the first line"})
	}
	rest := line[loc[1]:]
	m := parentTokenRe.FindSubmatchIndex(rest)
	if m == nil {
		if newParent == "" {
			return raw, false, nil // sin parent= ya es raíz
		}
		return nil, false, i18n.New(i18n.Msg{ES: "su encabezado no tiene parent=", EN: "its header has no parent="})
	}
	vs, ve := start+loc[1]+m[2], start+loc[1]+m[3]
	if string(raw[vs:ve]) == newParent {
		return raw, false, nil
	}
	out = make([]byte, 0, len(raw)+len(newParent))
	out = append(out, raw[:vs]...)
	out = append(out, newParent...)
	out = append(out, raw[ve:]...)
	return out, true, nil
}

// replaceKeepingStat reemplaza path por data de forma atómica y le devuelve los
// permisos y la fecha de modificación que tenía (st).
func replaceKeepingStat(path string, data []byte, st os.FileInfo) error {
	dir, name := filepath.Split(path)
	tmp, err := os.CreateTemp(dir, "."+name+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = os.Remove(tmpName) // el temporal es nuestro, no un checkpoint
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(tmpName, st.Mode().Perm()); err != nil {
		return fail(err)
	}
	if err := os.Chtimes(tmpName, st.ModTime(), st.ModTime()); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fail(err)
	}
	return nil
}
