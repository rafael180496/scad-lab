package refmodel

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/raster"
	"scadlab/backend/threemf"
)

// Editor de STL (S13): dos operaciones sobre refs/ que el editor necesita y que
// no son de adjuntar ni de analizar.
//
//   - ConvertToSTL: `import()` de un 3MF no existe en todas las versiones de
//     OpenSCAD y el de un OBJ solo en las recientes, así que lo que se edita es
//     siempre un STL. El original se conserva al lado.
//   - FreezeMesh: la base de una edición es la malla que ya renderizó el
//     checkpoint, guardada en refs/ para que el hijo pueda importarla.

// ConvertToSTL convierte un 3MF o un OBJ de refs/ en un STL binario, también en
// refs/ (refs/<nombre>.stl; -2, -3… si ese nombre ya existe) y devuelve su ruta
// relativa. El original queda intacto. Un STL se devuelve tal cual. rel es lo
// que acepta ResolveRef (con o sin «refs/»).
//
// Un 3MF se aplana en UNA malla: se pierden los colores y las partes. Es la
// misma limitación que tiene importarlo con `import()`.
func ConvertToSTL(workspaceDir, rel string) (string, error) {
	abs, relOK, err := ResolveRef(workspaceDir, rel)
	if err != nil {
		return "", err
	}
	format := FormatOf(relOK)
	if format == FormatSTL {
		return relOK, nil
	}
	if _, err := check(abs); err != nil {
		return "", err
	}
	var part threemf.Part
	switch format {
	case Format3MF:
		m, _, err := read3MF(abs)
		if err != nil {
			return "", err
		}
		tris := make([][3]int32, len(m.Triangles))
		for i, t := range m.Triangles {
			tris[i] = t.V
		}
		part = threemf.Part{Vertices: m.Vertices, Triangles: tris}
	case FormatOBJ:
		m, err := readOBJ(abs)
		if err != nil {
			return "", err
		}
		part = threemf.Part{Vertices: m.verts, Triangles: m.tris}
	default:
		return "", errNotRef
	}
	if len(part.Triangles) == 0 {
		return "", errEmpty
	}
	// WriteSTL indexa los vértices sin comprobar: un índice fuera de rango sería
	// un pánico, no un error.
	for _, t := range part.Triangles {
		for _, i := range t {
			if i < 0 || int(i) >= len(part.Vertices) {
				return "", errBadIndex
			}
		}
	}

	dir, err := refsDir(workspaceDir, false)
	if err != nil {
		return "", err
	}
	stem := safeStem(strings.TrimSuffix(filepath.Base(relOK), filepath.Ext(relOK)))
	for n := 1; n < 1000; n++ {
		name := stem + ".stl"
		if n > 1 {
			name = stem + "-" + strconv.Itoa(n) + ".stl"
		}
		path := filepath.Join(dir, name)
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		werr := threemf.WriteSTL(out, part)
		cerr := out.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(path) // el archivo es nuestro y quedó a medias
			if werr != nil {
				return "", werr
			}
			return "", cerr
		}
		return RefsDir + "/" + name, nil
	}
	return "", i18n.New(i18n.Msg{ES: "ya hay demasiadas referencias con ese nombre", EN: "there are too many references with that name already"})
}

// FreezeMesh copia la malla STL meshPath (la que dejó el render de un
// checkpoint en la caché) a refs/<nombre>.stl y devuelve su ruta relativa. name
// es el checkpoint sin «.scad».
//
// Si refs/<nombre>.stl ya existe con el MISMO contenido (mismo SHA-256) lo
// reutiliza y reused es true: abrir el editor dos veces sobre el mismo
// checkpoint no llena refs/ de copias. Con contenido distinto —el render cambió
// porque se actualizó OpenSCAD o el backend— prueba -2, -3… Nunca pisa.
func FreezeMesh(workspaceDir, meshPath, name string) (rel string, reused bool, err error) {
	st, err := os.Stat(meshPath)
	if err != nil {
		return "", false, err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return "", false, i18n.Errorf(i18n.Msg{ES: "%s no es una malla válida", EN: "%s is not a valid mesh"}, filepath.Base(meshPath))
	}
	if st.Size() > MaxBytes {
		return "", false, errTooBig(st.Size())
	}
	want, err := fileHash(meshPath)
	if err != nil {
		return "", false, err
	}
	dir, err := refsDir(workspaceDir, true)
	if err != nil {
		return "", false, err
	}
	in, err := os.Open(meshPath)
	if err != nil {
		return "", false, err
	}
	defer in.Close()

	stem := safeStem(name)
	for n := 1; n < 1000; n++ {
		file := stem + ".stl"
		if n > 1 {
			file = stem + "-" + strconv.Itoa(n) + ".stl"
		}
		path := filepath.Join(dir, file)
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			if lst, lerr := os.Lstat(path); lerr == nil && lst.Mode().IsRegular() {
				if got, herr := fileHash(path); herr == nil && got == want {
					return RefsDir + "/" + file, true, nil
				}
			}
			continue
		}
		if err != nil {
			return "", false, err
		}
		written, cerr := io.Copy(out, io.LimitReader(in, MaxBytes+1))
		if closeErr := out.Close(); cerr == nil {
			cerr = closeErr
		}
		if cerr == nil && written > MaxBytes {
			cerr = errTooBig(written)
		}
		if cerr != nil {
			_ = os.Remove(path)
			return "", false, cerr
		}
		return RefsDir + "/" + file, false, nil
	}
	return "", false, i18n.New(i18n.Msg{ES: "ya hay demasiadas referencias con ese nombre", EN: "there are too many references with that name already"})
}

// fileHash es el SHA-256 de un archivo, en hexadecimal.
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FileHash expone el SHA-256 de un archivo a quien necesita nombrar una copia
// por su contenido (la copia de la malla cruda en la caché del visor).
func FileHash(path string) (string, error) { return fileHash(path) }

// WriteBinarySTL escribe en dst una copia de src (un STL binario o ASCII) en STL
// binario, que es lo único que sabe leer el visor. dst no se pisa si existe (el
// llamador lo nombra por contenido: lo que ya está es lo mismo).
func WriteBinarySTL(src, dst string) error {
	tris, err := raster.ReadSTL(src)
	if err != nil {
		return err
	}
	if len(tris) == 0 {
		return errEmpty
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(out, 1<<16)
	var head [84]byte
	copy(head[:], "ScadLab STL")
	binary.LittleEndian.PutUint32(head[80:], uint32(len(tris)))
	_, werr := bw.Write(head[:])
	var rec [50]byte
	for _, t := range tris {
		if werr != nil {
			break
		}
		// La normal se recalcula: los visores la ignoran y los laminadores también.
		ux, uy, uz := t[3]-t[0], t[4]-t[1], t[5]-t[2]
		vx, vy, vz := t[6]-t[0], t[7]-t[1], t[8]-t[2]
		n := [3]float32{uy*vz - uz*vy, uz*vx - ux*vz, ux*vy - uy*vx}
		if l := float32(math.Sqrt(float64(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]))); l > 0 {
			n = [3]float32{n[0] / l, n[1] / l, n[2] / l}
		}
		for k := 0; k < 3; k++ {
			binary.LittleEndian.PutUint32(rec[4*k:], math.Float32bits(n[k]))
		}
		for k := 0; k < 9; k++ {
			binary.LittleEndian.PutUint32(rec[12+4*k:], math.Float32bits(t[k]))
		}
		rec[48], rec[49] = 0, 0
		_, werr = bw.Write(rec[:])
	}
	if werr == nil {
		werr = bw.Flush()
	}
	cerr := out.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(dst)
		if werr != nil {
			return werr
		}
		return cerr
	}
	return nil
}
