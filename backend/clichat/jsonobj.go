package clichat

import (
	"bytes"
	"encoding/json"
	"io"

	"scadlab/backend/i18n"
)

// Un objeto JSON que conserva el ORDEN de sus claves y el texto de cada valor tal
// cual. Se usa para editar archivos que no son de ScadLab (el settings.json de
// Antigravity, un hooks.json) cambiando solo lo que toca: con un map[string]any las
// claves saldrían en otro orden y los números y el resto del archivo se reescribirían.

type member struct {
	key string
	val json.RawMessage
}

type orderedObj []member

var errNotObject = i18n.New(i18n.Msg{ES: "no es un objeto JSON", EN: "not a JSON object"})

// parseObject lee raw como UN objeto JSON (con nada más que espacios alrededor).
func parseObject(raw []byte) (orderedObj, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errNotObject
	}
	obj := orderedObj{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, errNotObject
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		obj = append(obj, member{key: key, val: v})
	}
	if _, err := dec.Token(); err != nil { // el '}'
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errNotObject // basura después del objeto
	}
	return obj, nil
}

// index devuelve la posición de la ÚLTIMA aparición de key (la que gana en la
// mayoría de los intérpretes de JSON con claves repetidas) o -1.
func (o orderedObj) index(key string) int {
	for i := len(o) - 1; i >= 0; i-- {
		if o[i].key == key {
			return i
		}
	}
	return -1
}

// without devuelve una copia sin la posición i.
func (o orderedObj) without(i int) orderedObj {
	out := make(orderedObj, 0, len(o)-1)
	out = append(out, o[:i]...)
	return append(out, o[i+1:]...)
}

// marshal escribe el objeto con sangría de dos espacios, las claves en su orden y un
// salto de línea final.
func (o orderedObj) marshal() ([]byte, error) {
	b, err := o.marshalCompact()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// marshalCompact escribe el objeto en una línea, con las claves en su orden y cada
// valor tal cual (el que lo contiene lo sangra al final).
func (o orderedObj) marshalCompact() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := marshalNoEscape(m.key)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(m.val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// isNull dice si un valor crudo es el literal null.
func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }
