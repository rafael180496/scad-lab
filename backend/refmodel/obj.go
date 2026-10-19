package refmodel

import (
	"bufio"
	"bytes"
	"os"
	"strconv"

	"scadlab/backend/i18n"
)

// objMesh es un OBJ ya triangulado: vértices e índices (base 0).
type objMesh struct {
	verts [][3]float64
	tris  [][3]int32
}

// readOBJ lee un Wavefront OBJ. Solo importa la geometría: `v` y `f`. Se
// ignoran `vt`, `vn`, `vp`, `mtl`/`usemtl`, grupos, objetos y suavizado, y lo
// que sigue a la posición en `v` (el color por vértice de algunos exportadores).
//
//   - Una cara de 3 o más vértices se triangula en abanico desde el primero: es
//     correcta para polígonos convexos, que es lo que escribe casi todo.
//   - Los índices de una cara son `v`, `v/vt`, `v//vn` o `v/vt/vn`; solo cuenta
//     el primero.
//   - Un índice negativo cuenta desde el último vértice leído hasta ese punto.
//   - Una línea que termina en `\` sigue en la siguiente.
func readOBJ(path string) (objMesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return objMesh{}, err
	}
	defer f.Close()

	var m objMesh
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 8<<20)
	var pending []byte
	lineNo := 0
	handle := func(line []byte, n int) error {
		fs := bytes.Fields(line)
		if len(fs) == 0 {
			return nil
		}
		switch string(fs[0]) {
		case "v":
			if len(fs) < 4 {
				return objError(n)
			}
			var p [3]float64
			for k := 0; k < 3; k++ {
				v, err := strconv.ParseFloat(string(fs[1+k]), 64)
				if err != nil {
					return objError(n)
				}
				p[k] = v
			}
			m.verts = append(m.verts, p)
		case "f":
			if len(fs) < 4 {
				return objError(n)
			}
			idx := make([]int32, 0, len(fs)-1)
			for _, tok := range fs[1:] {
				i, err := objIndex(tok, len(m.verts))
				if err != nil {
					return objError(n)
				}
				idx = append(idx, i)
			}
			for k := 1; k+1 < len(idx); k++ {
				m.tris = append(m.tris, [3]int32{idx[0], idx[k], idx[k+1]})
			}
		}
		return nil
	}
	for sc.Scan() {
		lineNo++
		raw := bytes.TrimRight(sc.Bytes(), "\r")
		if bytes.HasSuffix(raw, []byte("\\")) {
			pending = append(pending, bytes.TrimSuffix(raw, []byte("\\"))...)
			pending = append(pending, ' ')
			continue
		}
		line := raw
		if len(pending) > 0 {
			line = append(pending, raw...)
			pending = pending[:0]
		}
		if i := bytes.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if err := handle(line, lineNo); err != nil {
			return objMesh{}, err
		}
	}
	if err := sc.Err(); err != nil {
		return objMesh{}, err
	}
	if len(m.tris) == 0 {
		return objMesh{}, i18n.New(i18n.Msg{ES: "el OBJ no contiene ninguna cara", EN: "the OBJ does not contain any face"})
	}
	// Un índice positivo puede apuntar a un vértice que se declara más abajo:
	// recién ahora se sabe cuántos hay.
	for _, t := range m.tris {
		for _, i := range t {
			if int(i) >= len(m.verts) {
				return objMesh{}, errBadIndex
			}
		}
	}
	return m, nil
}

func objError(line int) error {
	return i18n.Errorf(i18n.Msg{ES: "OBJ inválido en la línea %d", EN: "invalid OBJ at line %d"}, line)
}

// objIndex lee el índice de vértice de «v», «v/vt», «v//vn» o «v/vt/vn» y lo
// pasa a base 0. n es la cantidad de vértices leídos hasta ahora: un índice
// negativo es relativo a ella. Un positivo se valida al final (puede apuntar
// a un vértice declarado después).
func objIndex(tok []byte, n int) (int32, error) {
	if i := bytes.IndexByte(tok, '/'); i >= 0 {
		tok = tok[:i]
	}
	v, err := strconv.Atoi(string(tok))
	if err != nil || v == 0 {
		return 0, errBadIndex
	}
	if v < 0 {
		v = n + v + 1
	}
	if v < 1 {
		return 0, errBadIndex
	}
	return int32(v - 1), nil
}

var errBadIndex = i18n.New(i18n.Msg{ES: "índice de vértice fuera de rango", EN: "vertex index out of range"})
