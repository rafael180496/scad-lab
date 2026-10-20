package textedit

import (
	"sort"
	"strings"
)

// Match es una línea del texto con su número (1-based).
type Match struct {
	Line  int
	Text  string
	Score float64
}

// Closest devuelve las n líneas de src más parecidas a query (mejor primero),
// ignorando mayúsculas y diferencias de espacios. Lo usan los errores («la línea
// más parecida es la 123») y la búsqueda sin resultados. Una línea sin nada en
// común no se devuelve.
func Closest(src, query string, n int) []Match {
	_, body := stripBOM(src)
	return closest(newIndex(body), query, n)
}

// minSimilarity: por debajo, una línea no se parece lo suficiente como para
// sugerirla.
const minSimilarity = 0.2

func closest(ix index, query string, n int) []Match {
	q := normalizeLine(query)
	if q == "" || n <= 0 {
		return nil
	}
	qb := bigrams(q)
	var out []Match
	for i := 1; i <= ix.count(); i++ {
		raw := ix.line(i)
		l := normalizeLine(raw)
		if l == "" {
			continue
		}
		score := dice(qb, bigrams(l))
		// Una línea que contiene la consulta entera es lo más parecido que hay.
		if strings.Contains(l, q) {
			score = max(score, 0.9)
		}
		if score < minSimilarity {
			continue
		}
		out = append(out, Match{Line: i, Text: strings.TrimSpace(raw), Score: score})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Score > out[b].Score })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// normalizeLine pasa a minúsculas y colapsa los espacios.
func normalizeLine(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// bigrams cuenta los pares de runas consecutivas. Una cadena de una runa cuenta
// como un par consigo misma.
func bigrams(s string) map[[2]rune]int {
	r := []rune(s)
	out := map[[2]rune]int{}
	if len(r) == 1 {
		out[[2]rune{r[0], 0}]++
		return out
	}
	for i := 0; i+1 < len(r); i++ {
		out[[2]rune{r[i], r[i+1]}]++
	}
	return out
}

// dice es el coeficiente de Dice entre dos multiconjuntos de pares.
func dice(a, b map[[2]rune]int) float64 {
	var na, nb, common int
	for k, v := range a {
		na += v
		if w, ok := b[k]; ok {
			common += min(v, w)
		}
	}
	for _, v := range b {
		nb += v
	}
	if na+nb == 0 {
		return 0
	}
	return 2 * float64(common) / float64(na+nb)
}
