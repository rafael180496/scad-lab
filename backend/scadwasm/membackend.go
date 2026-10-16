package scadwasm

import (
	"io"
	"sort"
	"strings"
	"time"
)

// memBackend es un directorio en memoria, escribible y sin relación con el
// disco: el motor lo usa para lo que necesita escribir sin que sea del
// usuario (la caché de fuentes de fontconfig). Vive lo que dura una ejecución.
type memBackend struct {
	nodes map[string]*memNode // ruta relativa → nodo; "" es la raíz
}

type memNode struct {
	dir   bool
	data  []byte
	mtime time.Time
}

func newMemBackend() *memBackend {
	return &memBackend{nodes: map[string]*memNode{"": {dir: true, mtime: time.Now()}}}
}

func (m *memBackend) readOnly() bool { return false }

func parentOf(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return ""
}

func (m *memBackend) stat(rel string) (statInfo, errno) {
	n, ok := m.nodes[rel]
	if !ok {
		return statInfo{}, errnoNoEnt
	}
	si := statInfo{mtime: n.mtime, ino: inoOf("mem:" + rel)}
	if n.dir {
		si.mode, si.size = sIFDir|permDRW, 4096
	} else {
		si.mode, si.size = sIFReg|permRW, int64(len(n.data))
	}
	return si, errnoOK
}

func (m *memBackend) open(rel string, flags int) (backFile, errno) {
	n, ok := m.nodes[rel]
	if !ok {
		if flags&oCreat == 0 {
			return nil, errnoNoEnt
		}
		if p, pok := m.nodes[parentOf(rel)]; !pok || !p.dir {
			return nil, errnoNoEnt
		}
		n = &memNode{mtime: time.Now()}
		m.nodes[rel] = n
	} else if n.dir {
		return nil, errnoIsDir
	}
	if flags&oTrunc != 0 {
		n.data = nil
	}
	return &memHandle{n: n}, errnoOK
}

func (m *memBackend) readdir(rel string) ([]string, errno) {
	if n, ok := m.nodes[rel]; !ok || !n.dir {
		return nil, errnoNotDir
	}
	prefix := rel + "/"
	if rel == "" {
		prefix = ""
	}
	var names []string
	for k := range m.nodes {
		if k == "" || !strings.HasPrefix(k, prefix) {
			continue
		}
		if rest := k[len(prefix):]; rest != "" && !strings.Contains(rest, "/") {
			names = append(names, rest)
		}
	}
	sort.Strings(names)
	return names, errnoOK
}

func (m *memBackend) mkdir(rel string) errno {
	if _, ok := m.nodes[rel]; ok {
		return errnoExist
	}
	if p, ok := m.nodes[parentOf(rel)]; !ok || !p.dir {
		return errnoNoEnt
	}
	m.nodes[rel] = &memNode{dir: true, mtime: time.Now()}
	return errnoOK
}

func (m *memBackend) rmdir(rel string) errno {
	n, ok := m.nodes[rel]
	if !ok {
		return errnoNoEnt
	}
	if !n.dir {
		return errnoNotDir
	}
	if names, _ := m.readdir(rel); len(names) > 0 {
		return errnoNotEmpty
	}
	delete(m.nodes, rel)
	return errnoOK
}

func (m *memBackend) unlink(rel string) errno {
	n, ok := m.nodes[rel]
	if !ok {
		return errnoNoEnt
	}
	if n.dir {
		return errnoIsDir
	}
	delete(m.nodes, rel)
	return errnoOK
}

func (m *memBackend) rename(from, to string) errno {
	n, ok := m.nodes[from]
	if !ok {
		return errnoNoEnt
	}
	if p, pok := m.nodes[parentOf(to)]; !pok || !p.dir {
		return errnoNoEnt
	}
	if n.dir {
		return errnoNoSys // renombrar un directorio con contenido no hace falta aquí
	}
	delete(m.nodes, from)
	m.nodes[to] = n
	return errnoOK
}

type memHandle struct{ n *memNode }

func (h *memHandle) readAt(p []byte, off int64) (int, error) {
	if off >= int64(len(h.n.data)) {
		return 0, io.EOF
	}
	n := copy(p, h.n.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (h *memHandle) writeAt(p []byte, off int64) (int, error) {
	end := off + int64(len(p))
	if end > int64(len(h.n.data)) {
		h.n.data = append(h.n.data, make([]byte, end-int64(len(h.n.data)))...)
	}
	copy(h.n.data[off:], p)
	h.n.mtime = time.Now()
	return len(p), nil
}

func (h *memHandle) size() (int64, error) { return int64(len(h.n.data)), nil }
func (h *memHandle) truncate(n int64) error {
	if n < int64(len(h.n.data)) {
		h.n.data = h.n.data[:n]
	} else {
		h.n.data = append(h.n.data, make([]byte, n-int64(len(h.n.data)))...)
	}
	return nil
}
func (h *memHandle) close() error { return nil }
