package scadwasm

import "scadlab/backend/i18n"

// hostSlots devuelve, para cada posición de la tabla indirecta que guarda una
// función IMPORTADA (del host), el índice de esa importación.
//
// Hace falta porque el módulo guarda en la tabla algunos punteros a funciones
// del host (exit, __cxa_throw…) y un invoke_* tiene que poder llamarlos como
// cualquier otro; pero wazero solo sabe buscar en la tabla funciones del propio
// módulo y revienta con las importadas. El lector es mínimo a propósito: solo
// recorre las secciones de importación y de elementos.
func hostSlots(wasm []byte) (map[uint32]uint32, error) {
	r := &wasmReader{b: wasm}
	if len(wasm) < 8 || string(wasm[:4]) != "\x00asm" {
		return nil, i18n.New(i18n.Msg{ES: "no es un módulo de WebAssembly", EN: "not a WebAssembly module"})
	}
	r.p = 8
	var importFuncs uint32
	slots := map[uint32]uint32{}
	for r.p < len(wasm) {
		id := r.byte()
		size := r.uleb()
		end := r.p + int(size)
		if r.err != nil || end > len(wasm) {
			return nil, i18n.New(i18n.Msg{ES: "módulo mal formado", EN: "malformed module"})
		}
		switch id {
		case 2: // importaciones
			n := r.uleb()
			for i := uint32(0); i < n && r.err == nil; i++ {
				r.skipName()
				r.skipName()
				switch kind := r.byte(); kind {
				case 0: // función
					r.uleb()
					importFuncs++
				case 1: // tabla
					r.byte()
					r.limits()
				case 2: // memoria
					r.limits()
				case 3: // global
					r.byte()
					r.byte()
				default:
					return nil, i18n.Errorf(i18n.Msg{ES: "importación desconocida %d", EN: "unknown import %d"}, kind)
				}
			}
		case 9: // elementos
			n := r.uleb()
			for i := uint32(0); i < n && r.err == nil; i++ {
				if flag := r.uleb(); flag != 0 {
					return nil, i18n.Errorf(i18n.Msg{ES: "segmento de elementos con flag %d no soportado", EN: "unsupported element segment flag %d"}, flag)
				}
				if op := r.byte(); op != 0x41 { // i32.const
					return nil, i18n.New(i18n.Msg{ES: "desplazamiento de elementos no constante", EN: "non-constant element offset"})
				}
				off := uint32(r.sleb())
				r.byte() // end
				cnt := r.uleb()
				for j := uint32(0); j < cnt && r.err == nil; j++ {
					f := r.uleb()
					if f < importFuncs {
						slots[off+j] = f
					}
				}
			}
		}
		if r.err != nil {
			return nil, r.err
		}
		r.p = end
	}
	return slots, nil
}

type wasmReader struct {
	b   []byte
	p   int
	err error
}

func (r *wasmReader) byte() byte {
	if r.p >= len(r.b) {
		r.err = i18n.New(i18n.Msg{ES: "fin inesperado del módulo", EN: "unexpected end of module"})
		return 0
	}
	v := r.b[r.p]
	r.p++
	return v
}

func (r *wasmReader) uleb() uint32 {
	var v uint64
	var shift uint
	for {
		x := r.byte()
		v |= uint64(x&0x7f) << shift
		shift += 7
		if x&0x80 == 0 || r.err != nil {
			break
		}
	}
	return uint32(v)
}

func (r *wasmReader) sleb() int32 {
	var v int64
	var shift uint
	for {
		x := r.byte()
		v |= int64(x&0x7f) << shift
		shift += 7
		if x&0x80 == 0 || r.err != nil {
			if x&0x40 != 0 && shift < 64 {
				v |= -1 << shift
			}
			break
		}
	}
	return int32(v)
}

func (r *wasmReader) skipName() {
	n := r.uleb()
	r.p += int(n)
}

func (r *wasmReader) limits() {
	flags := r.byte()
	r.uleb()
	if flags&1 != 0 {
		r.uleb()
	}
}
