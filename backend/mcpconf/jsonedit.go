package mcpconf

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"

	"scadlab/backend/i18n"
)

// Edición quirúrgica de JSON.
//
// Un config MCP es un archivo del usuario: lo importante no es que el JSON
// resultante «signifique lo mismo», es que **el archivo se vea igual** salvo
// por nuestra clave. Decodificar a un mapa y volver a escribir reordena las
// claves (Go ordena los mapas alfabéticamente) y reformatea todo; aquí se
// ubican los bytes de cada miembro del objeto y se empalma solo el tramo que
// cambia. Lo que no se toca queda byte a byte: orden, sangría, saltos de línea.
//
// El escáner da por hecho que el texto ya pasó json.Valid (quien llama lo
// comprueba): no valida, solo mide.

// pair es un campo con su valor ya serializable; los objetos que escribimos
// van ordenados (command antes que args) porque así se leen mejor.
type pair struct {
	Key string
	Val any
}

// ordered es un objeto JSON con el orden de sus campos.
type ordered []pair

func (o ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshalPlain(p.Key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := marshalPlain(p.Val)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalPlain serializa sin escapar <, > ni &: una ruta con «&» tiene que
// verse como la escribió el sistema de archivos.
func marshalPlain(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// renderIn es render con el estilo del archivo (sangría y salto de línea).
func renderIn(v any, prefix string, st style) ([]byte, error) {
	b, err := render(v, prefix, st.unit)
	if err != nil || st.nl == "\n" {
		return b, err
	}
	return bytes.ReplaceAll(b, []byte("\n"), []byte(st.nl)), nil
}

// render serializa v con sangría; prefix va delante de cada línea salvo la
// primera (la que ya está colocada por quien empalma).
func render(v any, prefix, unit string) ([]byte, error) {
	compact, err := marshalPlain(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, prefix, unit); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// member es un miembro de un objeto: «"clave": valor».
type member struct {
	key      string
	start    int // la comilla de apertura de la clave
	valStart int
	valEnd   int
}

// object es un objeto ya medido: posición de su { y su }, y sus miembros.
type object struct {
	open, close int
	members     []member
}

var errNotObject = i18n.New(i18n.Msg{ES: "no es un objeto JSON", EN: "not a JSON object"})

func skipWS(d []byte, i int) int {
	for i < len(d) && (d[i] == ' ' || d[i] == '\t' || d[i] == '\n' || d[i] == '\r') {
		i++
	}
	return i
}

func scanString(d []byte, i int) int {
	i++ // comilla de apertura
	for i < len(d) {
		switch d[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1
		default:
			i++
		}
	}
	return i
}

// skipValue devuelve la posición justo después del valor que empieza en i.
func skipValue(d []byte, i int) int {
	switch d[i] {
	case '"':
		return scanString(d, i)
	case '{', '[':
		depth := 0
		for i < len(d) {
			switch d[i] {
			case '"':
				i = scanString(d, i)
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return i
	default: // número, true, false, null
		for i < len(d) && !strings.ContainsRune(",}] \t\r\n", rune(d[i])) {
			i++
		}
		return i
	}
}

// scanObject mide el objeto que empieza en pos (que apunta a su «{»).
func scanObject(d []byte, pos int) (object, error) {
	if pos >= len(d) || d[pos] != '{' {
		return object{}, errNotObject
	}
	o := object{open: pos}
	i := skipWS(d, pos+1)
	for i < len(d) && d[i] != '}' {
		ks := i
		ke := scanString(d, ks)
		var key string
		if err := json.Unmarshal(d[ks:ke], &key); err != nil {
			return object{}, err
		}
		i = skipWS(d, ke)
		i = skipWS(d, i+1) // los dos puntos
		vs := i
		ve := skipValue(d, vs)
		o.members = append(o.members, member{key: key, start: ks, valStart: vs, valEnd: ve})
		i = skipWS(d, ve)
		if i < len(d) && d[i] == ',' {
			i = skipWS(d, i+1)
		}
	}
	o.close = i
	return o, nil
}

func (o object) find(key string) (int, bool) {
	for i, m := range o.members {
		if m.key == key {
			return i, true
		}
	}
	return -1, false
}

// lineIndent es la sangría de la línea donde cae pos.
func lineIndent(d []byte, pos int) string {
	ls := bytes.LastIndexByte(d[:pos], '\n') + 1
	e := ls
	for e < len(d) && (d[e] == ' ' || d[e] == '\t') {
		e++
	}
	return string(d[ls:e])
}

// style es el estilo del archivo, para que lo que se agrega se vea igual.
type style struct {
	nl   string // salto de línea
	unit string // una sangría
}

// detectStyle lee el salto de línea y la sangría del texto; sin pistas, LF y
// dos espacios.
func detectStyle(d []byte) style {
	s := style{nl: "\n", unit: "  "}
	if bytes.Contains(d, []byte("\r\n")) {
		s.nl = "\r\n"
	}
	// La primera línea indentada de la raíz marca la unidad.
	for _, line := range bytes.Split(d, []byte("\n")) {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			j := 0
			for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
				j++
			}
			s.unit = string(line[:j])
			break
		}
	}
	return s
}

// multiline dice si el objeto tiene sus miembros en líneas propias.
func multiline(d []byte, o object) bool {
	return bytes.IndexByte(d[o.open:o.close], '\n') >= 0
}

// insertMember agrega «"key": value» al final del objeto o.
func insertMember(d []byte, o object, key string, value any, st style) ([]byte, error) {
	k, err := marshalPlain(key)
	if err != nil {
		return nil, err
	}
	closeIndent := lineIndent(d, o.open)
	switch {
	case len(o.members) == 0:
		mi := closeIndent + st.unit
		v, err := renderIn(value, mi, st)
		if err != nil {
			return nil, err
		}
		text := st.nl + mi + string(k) + ": " + string(v) + st.nl + closeIndent
		return splice(d, o.open+1, o.close, []byte(text)), nil
	case multiline(d, o):
		last := o.members[len(o.members)-1]
		mi := lineIndent(d, o.members[0].start)
		v, err := renderIn(value, mi, st)
		if err != nil {
			return nil, err
		}
		text := "," + st.nl + mi + string(k) + ": " + string(v)
		return splice(d, last.valEnd, last.valEnd, []byte(text)), nil
	default: // «{"a":1}» en una línea: se agrega en la misma línea
		last := o.members[len(o.members)-1]
		v, err := marshalPlain(value)
		if err != nil {
			return nil, err
		}
		text := ", " + string(k) + ": " + string(v)
		return splice(d, last.valEnd, last.valEnd, []byte(text)), nil
	}
}

// removeMember quita el miembro i con su coma.
func removeMember(d []byte, o object, i int) []byte {
	switch {
	case len(o.members) == 1:
		return splice(d, o.open+1, o.close, nil) // «{}»
	case i < len(o.members)-1:
		return splice(d, o.members[i].start, o.members[i+1].start, nil)
	default:
		return splice(d, o.members[i-1].valEnd, o.members[i].valEnd, nil)
	}
}

func splice(d []byte, from, to int, with []byte) []byte {
	out := make([]byte, 0, len(d)-(to-from)+len(with))
	out = append(out, d[:from]...)
	out = append(out, with...)
	return append(out, d[to:]...)
}

// mergeOwned devuelve el objeto «existing» con los campos de owned puestos: los
// que ya estaban se actualizan en su lugar, los que faltan se agregan al final
// y los que el usuario agregó por su cuenta (timeout, env…) se conservan.
// changed dice si hubo alguna diferencia real.
func mergeOwned(existing []byte, owned ordered) (merged ordered, changed bool, err error) {
	o, err := scanObject(existing, skipWS(existing, 0))
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	for _, m := range o.members {
		var raw any
		if err := json.Unmarshal(existing[m.valStart:m.valEnd], &raw); err != nil {
			return nil, false, err
		}
		var val any = json.RawMessage(existing[m.valStart:m.valEnd]) // los campos ajenos pasan tal cual
		for _, p := range owned {
			if p.Key != m.key {
				continue
			}
			seen[p.Key] = true
			if !sameJSON(raw, p.Val) {
				changed = true
			}
			val = p.Val
		}
		merged = append(merged, pair{Key: m.key, Val: val})
	}
	for _, p := range owned {
		if !seen[p.Key] {
			merged = append(merged, p)
			changed = true
		}
	}
	return merged, changed, nil
}

// sameJSON compara un valor decodificado con uno que vamos a escribir.
func sameJSON(decoded, want any) bool {
	b, err := marshalPlain(want)
	if err != nil {
		return false
	}
	var w any
	if json.Unmarshal(b, &w) != nil {
		return false
	}
	return reflect.DeepEqual(decoded, w)
}
