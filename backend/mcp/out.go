package mcp

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Salidas compactas (S17). Un modelo chico (Ollama de 8k) paga cada byte que una
// herramienta le devuelve, y un JSON con sangría de 8 KB puede ser un tercio de su
// ventana. Dos herramientas de este archivo:
//
//   - ordered: un objeto JSON con las claves en el orden en que se agregaron. Un
//     map de Go las serializa en orden alfabético, y `updated` quedaría detrás de
//     un `render` enorme: si el resultado se recorta al guardarlo en el chat, lo
//     que se pierde tiene que ser la cola, no la clave que decide qué hace la
//     interfaz (UpdatesInPlace).
//   - compactResult: JSON sin sangría.

type kv struct {
	K string
	V any
}

// ordered es un objeto JSON que conserva el orden de inserción.
type ordered []kv

// set agrega la clave, o reemplaza su valor si ya estaba.
func (o *ordered) set(k string, v any) {
	for i := range *o {
		if (*o)[i].K == k {
			(*o)[i].V = v
			return
		}
	}
	*o = append(*o, kv{k, v})
}

func (o ordered) get(k string) (any, bool) {
	for _, e := range o {
		if e.K == k {
			return e.V, true
		}
	}
	return nil, false
}

func (o ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshalNoEscape(e.K)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := marshalNoEscape(e.V)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalNoEscape es json.Marshal sin escapar <, > y & (un include <lib/x.scad>
// no tiene que llegar como <) y sin el salto final.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// compactResult serializa v en una sola línea.
func compactResult(v any) (*toolResult, error) {
	b, err := marshalNoEscape(v)
	if err != nil {
		return nil, err
	}
	return textResult(string(b)), nil
}

// arr3 redondea un vector a n decimales como lista de tres números, la forma más
// corta de decir un tamaño o una posición.
func arr3(a [3]float64, decimals int) []float64 {
	return []float64{round(a[0], decimals), round(a[1], decimals), round(a[2], decimals)}
}

// clipRunes recorta s a n runas con «…» (sin espacios en los extremos).
func clipRunes(s string, n int) string {
	return clipKeep(strings.TrimSpace(s), n)
}

// clipKeep recorta s a n runas con «…» sin tocar el espacio del comienzo: la
// sangría de una línea de código importa cuando el modelo la va a copiar.
func clipKeep(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return strings.TrimRight(string(r[:n-1]), " ") + "…"
}

// itoa abrevia strconv.Itoa en los mensajes.
func itoa(n int) string { return strconv.Itoa(n) }

// toolResultSoftCap es el tope de bytes con el que las herramientas que listan
// cortan su resultado (6 KB, S17): lo que no cabe se resume y se dice cuánto falta.
const toolResultSoftCap = 5 << 10
