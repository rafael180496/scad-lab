package mcp

import (
	"encoding/base64"
	"strconv"
	"strings"
)

func encodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// strArg saca un argumento de texto; falta o tipo equivocado = "".
func strArg(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case string:
		return v
	case float64: // un modelo chico manda 3 donde va "3"
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// intArg saca un entero. Acepta número y texto numérico: los modelos mandan
// "640" con frecuencia.
func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// obj arma un JSON Schema de objeto. Las propiedades se escriben a mano por
// herramienta: son pocas y generarlas por reflexión costaría más de lo que
// ahorra.
func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func enumProp(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}
