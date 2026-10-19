package refmodel

import (
	"os"
	"path/filepath"
	"testing"

	"scadlab/backend/mesh"
)

func TestConvertToSTL(t *testing.T) {
	ws := t.TempDir()
	refs := filepath.Join(ws, "refs")
	if err := os.MkdirAll(refs, 0o755); err != nil {
		t.Fatal(err)
	}
	v, tr := boxMesh(0, 0, 0, 10, 20, 30)
	writeFile(t, refs, "caja.stl", binarySTL(v, tr))
	if err := os.WriteFile(filepath.Join(refs, "caja.obj"), []byte("v 0 0 0\nv 10 0 0\nv 0 20 0\nv 0 0 30\nf 1 3 2\nf 1 2 4\nf 2 3 4\nf 1 4 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Un STL se devuelve tal cual.
	if got, err := ConvertToSTL(ws, "caja.stl"); err != nil || got != "refs/caja.stl" {
		t.Fatalf("STL: %q %v", got, err)
	}
	// Un OBJ se convierte a refs/caja-2.stl (caja.stl ya existe) sin tocar nada.
	before, _ := os.ReadFile(filepath.Join(refs, "caja.stl"))
	got, err := ConvertToSTL(ws, "refs/caja.obj")
	if err != nil || got != "refs/caja-2.stl" {
		t.Fatalf("OBJ: %q %v", got, err)
	}
	after, _ := os.ReadFile(filepath.Join(refs, "caja.stl"))
	if string(before) != string(after) {
		t.Error("pisó un STL existente")
	}
	info, err := mesh.InfoFromFile(filepath.Join(ws, "refs", "caja-2.stl"))
	if err != nil || info.Triangles != 4 || !info.Watertight || !nearly(info.VolumeMM3, 10*20*30/6.0) {
		t.Errorf("tetraedro convertido: %+v %v", info, err)
	}
	// Convertir otra vez no pisa la conversión anterior.
	if again, err := ConvertToSTL(ws, "caja.obj"); err != nil || again != "refs/caja-3.stl" {
		t.Errorf("segunda conversión: %q %v", again, err)
	}
	for _, bad := range []string{"", "../caja.obj", "refs/../caja.obj", "/etc/passwd", "nada.obj", "sub/x.obj"} {
		if got, err := ConvertToSTL(ws, bad); err == nil {
			t.Errorf("%q debía rechazarse: %q", bad, got)
		}
	}
}

func nearly(a, b float64) bool { d := a - b; return d < 0.01 && d > -0.01 }

func TestFreezeMeshReusesByHash(t *testing.T) {
	ws, cache := t.TempDir(), t.TempDir()
	v, tr := boxMesh(0, 0, 0, 10, 20, 30)
	mesh1 := writeFile(t, cache, "a.stl", binarySTL(v, tr))

	rel, reused, err := FreezeMesh(ws, mesh1, "003-agregar-bisagra")
	if err != nil || reused || rel != "refs/003-agregar-bisagra.stl" {
		t.Fatalf("primera vez: %q reused=%v %v", rel, reused, err)
	}
	// Mismo contenido, otro archivo de la caché: se reutiliza.
	same := writeFile(t, cache, "b.stl", binarySTL(v, tr))
	if rel2, reused, err := FreezeMesh(ws, same, "003-agregar-bisagra"); err != nil || !reused || rel2 != rel {
		t.Fatalf("mismo hash: %q reused=%v %v", rel2, reused, err)
	}
	// Contenido distinto (el render cambió): archivo nuevo, el anterior intacto.
	v2, tr2 := boxMesh(0, 0, 0, 11, 20, 30)
	other := writeFile(t, cache, "c.stl", binarySTL(v2, tr2))
	rel3, reused, err := FreezeMesh(ws, other, "003-agregar-bisagra")
	if err != nil || reused || rel3 != "refs/003-agregar-bisagra-2.stl" {
		t.Fatalf("otro contenido: %q reused=%v %v", rel3, reused, err)
	}
	if got, _ := os.ReadFile(filepath.Join(ws, rel)); string(got) != string(binarySTL(v, tr)) {
		t.Error("la primera congelación cambió")
	}
	// Y la tercera vez con el segundo contenido reutiliza -2.
	if rel4, reused, err := FreezeMesh(ws, other, "003-agregar-bisagra"); err != nil || !reused || rel4 != rel3 {
		t.Fatalf("reutilizar -2: %q reused=%v %v", rel4, reused, err)
	}
	// Un nombre con caracteres raros se normaliza a un archivo dentro de refs/.
	if rel5, _, err := FreezeMesh(ws, mesh1, "../../Ñandú con espacio"); err != nil || filepath.Dir(rel5) != "refs" {
		t.Errorf("nombre raro: %q %v", rel5, err)
	}
	// Entradas inválidas.
	empty := writeFile(t, cache, "vacio.stl", nil)
	if _, _, err := FreezeMesh(ws, empty, "x"); err == nil {
		t.Error("una malla vacía debía rechazarse")
	}
	if _, _, err := FreezeMesh(ws, filepath.Join(cache, "no-existe.stl"), "x"); err == nil {
		t.Error("una malla inexistente debía rechazarse")
	}
	if _, _, err := FreezeMesh(ws, cache, "x"); err == nil {
		t.Error("una carpeta debía rechazarse")
	}
}

func TestWriteBinarySTLFromASCII(t *testing.T) {
	dir := t.TempDir()
	v, tr := boxMesh(0, 0, 0, 10, 20, 30)
	ascii := writeFile(t, dir, "a.stl", asciiSTL(v, tr))
	dst := filepath.Join(dir, "b.stl")
	if err := WriteBinarySTL(ascii, dst); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dst)
	if st.Size() != 84+50*12 {
		t.Errorf("no es un STL binario de 12 triángulos: %d bytes", st.Size())
	}
	info, err := mesh.InfoFromFile(dst)
	if err != nil || !info.Watertight || !nearly(info.VolumeMM3, 6000) {
		t.Errorf("copia: %+v %v", info, err)
	}
	if err := WriteBinarySTL(ascii, dst); err == nil {
		t.Error("no debe pisar el destino")
	}
	if err := WriteBinarySTL(filepath.Join(dir, "nada.stl"), filepath.Join(dir, "c.stl")); err == nil {
		t.Error("origen inexistente")
	}
}
