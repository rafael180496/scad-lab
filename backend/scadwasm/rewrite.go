package scadwasm

import (
	"path/filepath"
	"sort"
	"strings"
)

// rewritePaths cambia en la salida las rutas virtuales de los montajes por las
// del disco. OpenSCAD escribe «in file /work/lib/x.scad, line 3» con lo que ve,
// y quien parsea la salida (render.ParseOutput, el linter) compara contra rutas
// del workspace real: con esta traducción los diagnósticos quedan igual que con
// el OpenSCAD nativo.
//
// Una ruta virtual solo se reemplaza si empieza en un límite (inicio, espacio,
// comilla, paréntesis…) y termina en «/» o en un carácter que no es de ruta:
// «/workshop» no es «/work».
func rewritePaths(out string, mounts []Mount) string {
	if out == "" || len(mounts) == 0 {
		return out
	}
	ms := make([]Mount, 0, len(mounts))
	for _, m := range mounts {
		if v := cleanVirtual(m.Virtual); v != "" && v != "/" {
			m.Virtual = v
			ms = append(ms, m)
		}
	}
	// El más largo primero: /lib/10 antes que /lib/1.
	sort.SliceStable(ms, func(i, j int) bool { return len(ms[i].Virtual) > len(ms[j].Virtual) })

	var b strings.Builder
	b.Grow(len(out))
	for i := 0; i < len(out); {
		if out[i] == '/' && (i == 0 || isPathBoundary(out[i-1])) {
			if host, n, ok := matchMount(out[i:], ms); ok {
				b.WriteString(host)
				i += n
				continue
			}
		}
		b.WriteByte(out[i])
		i++
	}
	return b.String()
}

// matchMount intenta reconocer una ruta virtual al principio de s. Devuelve la
// ruta del disco equivalente y cuántos bytes de s consumió.
func matchMount(s string, ms []Mount) (host string, n int, ok bool) {
	for _, m := range ms {
		if !strings.HasPrefix(s, m.Virtual) {
			continue
		}
		rest := s[len(m.Virtual):]
		if rest != "" && rest[0] != '/' && !isPathEnd(rest[0]) {
			continue
		}
		// El resto de la ruta: hasta un carácter que no puede ser parte de ella.
		end := 0
		for end < len(rest) && isPathChar(rest[end]) {
			end++
		}
		// Un punto final es de la frase («… /work/a.scad.»), no de la ruta.
		sub := strings.TrimRight(rest[:end], ".")
		host = filepath.Join(m.Host, filepath.FromSlash(strings.TrimPrefix(sub, "/")))
		return host, len(m.Virtual) + len(sub), true
	}
	return "", 0, false
}

func isPathBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', '(', '[', '<', '=', ',', ':':
		return true
	}
	return false
}

// isPathEnd son los caracteres que cierran una ruta justo después del montaje
// («/work» sin nada más).
func isPathEnd(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', ')', ']', '>', ',', ':', ';', '.':
		return true
	}
	return false
}

func isPathChar(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', '<', '>', '|', '*', '?', ',', ':', ';', '(', ')', '[', ']':
		return false
	}
	return true
}
