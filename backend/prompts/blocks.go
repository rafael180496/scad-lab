package prompts

import (
	"regexp"
	"strings"
)

// Block es un bloque de código OpenSCAD extraído de una respuesta markdown.
type Block struct {
	// Code es el contenido del bloque, sin las líneas de cerco, con "\n" como
	// separador y sin el salto final.
	Code string
	// Lang es la etiqueta tal como la escribió el modelo, en minúsculas:
	// "openscad", "scad" o "" (bloque sin lenguaje cuyo contenido parece
	// OpenSCAD).
	Lang string
	// Start y End son offsets en bytes sobre el markdown original: el bloque
	// completo, desde el inicio de su cerco de apertura hasta el final de su
	// cerco de cierre (sin el salto de línea posterior). Reemplazar
	// markdown[Start:End] sustituye el bloque entero.
	Start, End int
	// Partial marca un bloque que se abrió y no llegó a cerrarse: la respuesta
	// sigue llegando por streaming. Code trae lo recibido hasta ahora y End es
	// len(markdown). Quien guarda o ejecuta código debe ignorar los parciales.
	//
	// DESVÍO del contrato de la spec (que define Block{Code, Lang, Start,
	// End}): se agrega Partial para poder mostrar el bloque que se está
	// escribiendo sin ofrecer «Guardar como checkpoint» antes de tiempo.
	Partial bool
}

var fenceRe = regexp.MustCompile("^([ \\t]*)(`{3,}|~{3,})(.*)$")

// ScadBlocks extrae de una respuesta markdown los bloques ```openscad y
// ```scad, y los ``` sin lenguaje cuyo contenido parezca OpenSCAD. Ignora los
// bloques de otros lenguajes (aunque su contenido lo parezca: el modelo dijo
// qué eran). Los cercos pueden ser de 3 o más backticks o tildes, y el cierre
// debe usar el mismo carácter y al menos la misma longitud; un cerco de
// apertura tolera sangría (un bloque dentro de una lista). Un bloque sin
// cierre al final del texto se devuelve con Partial = true.
func ScadBlocks(markdown string) []Block {
	var blocks []Block
	var (
		open     bool
		fenceCh  byte
		fenceLen int
		indent   int
		lang     string
		start    int
		code     []string
	)
	emit := func(end int, partial bool) {
		if partial && len(code) > 0 && isFragmentOfFence(code[len(code)-1], fenceCh) {
			// El cierre llegó a medias por streaming ("``" de "```"): no es código.
			code = code[:len(code)-1]
		}
		body := strings.Join(code, "\n")
		l := strings.ToLower(lang)
		ok := l == "openscad" || l == "scad" || (l == "" && looksLikeOpenSCAD(body))
		if ok {
			blocks = append(blocks, Block{Code: body, Lang: l, Start: start, End: end, Partial: partial})
		}
	}

	pos := 0
	for pos < len(markdown) {
		nl := strings.IndexByte(markdown[pos:], '\n')
		lineEnd, next := len(markdown), len(markdown)+1
		if nl >= 0 {
			lineEnd, next = pos+nl, pos+nl+1
		}
		line := strings.TrimSuffix(markdown[pos:lineEnd], "\r")

		if !open {
			if m := fenceRe.FindStringSubmatch(line); m != nil {
				info := strings.TrimSpace(m[3])
				// Un cerco de backticks no admite backticks en la etiqueta
				// (sería código en línea: ```x``` dentro de un párrafo).
				if m[2][0] != '`' || !strings.Contains(info, "`") {
					open, fenceCh, fenceLen = true, m[2][0], len(m[2])
					indent = len(m[1])
					lang = firstWord(info)
					start = pos
					code = code[:0]
				}
			}
		} else {
			if closesFence(line, fenceCh, fenceLen) {
				open = false
				emit(pos+len(strings.TrimRight(line, " \t")), false)
			} else {
				code = append(code, stripIndent(line, indent))
			}
		}
		pos = next
	}
	if open {
		emit(len(markdown), true)
	}
	return blocks
}

// isFragmentOfFence dice si la línea es solo 1 o 2 caracteres de cerco.
func isFragmentOfFence(line string, ch byte) bool {
	t := strings.TrimSpace(line)
	return t != "" && len(t) < 3 && strings.Trim(t, string(ch)) == ""
}

func closesFence(line string, ch byte, minLen int) bool {
	t := strings.TrimLeft(line, " \t")
	n := 0
	for n < len(t) && t[n] == ch {
		n++
	}
	return n >= minLen && strings.TrimSpace(t[n:]) == ""
}

func stripIndent(line string, n int) string {
	i := 0
	for i < n && i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[i:]
}

// firstWord devuelve la primera palabra de la etiqueta, sin llaves ni punto
// (```{.openscad} o ```openscad title="x").
func firstWord(info string) string {
	info = strings.TrimLeft(info, "{. ")
	if i := strings.IndexAny(info, " \t{}"); i >= 0 {
		info = info[:i]
	}
	return info
}

var (
	// Una sola de estas basta: son construcciones que casi no existen fuera de
	// OpenSCAD. translate/rotate/scale/mirror/resize exigen el vector literal
	// para no confundirse con translate(10px) de CSS.
	strongScadRe = regexp.MustCompile(`(?m)` +
		`\b(cube|sphere|cylinder|polyhedron|linear_extrude|rotate_extrude|minkowski|difference|intersection)\s*\(` +
		`|\bmodule\s+\w+\s*\(` +
		`|\$f[nas]\s*=` +
		`|@checkpoint\b` +
		`|\b(translate|rotate|scale|mirror|resize)\s*\(\s*\[` +
		`|\b(hull|union)\s*\(\s*\)`)
	// Indicios débiles: hacen falta al menos dos distintos y un ";".
	weakScadRe = regexp.MustCompile(`\b(color|circle|square|polygon|offset|text|projection|children|render)\s*\(`)
)

// looksLikeOpenSCAD decide si un bloque sin lenguaje es OpenSCAD. Es una
// heurística: un falso negativo solo hace que el bloque no ofrezca «Guardar
// como checkpoint»; el modelo suele etiquetar bien.
func looksLikeOpenSCAD(code string) bool {
	if strongScadRe.MatchString(code) {
		return true
	}
	if !strings.Contains(code, ";") {
		return false
	}
	seen := map[string]bool{}
	for _, m := range weakScadRe.FindAllStringSubmatch(code, -1) {
		seen[m[1]] = true
	}
	return len(seen) >= 2
}
