package parts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"scadlab/assets"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/meshserve"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/threemf"
	"scadlab/backend/threemf/macetatest"
)

const nameplateTemplate = "skills/openscad-templates/templates/nameplate-two-color.scad"

// --- con OpenSCAD real --------------------------------------------------------

type rig struct {
	t      *testing.T
	dir    string // workspace
	data   string // <datos>
	eng    *render.Engine
	bin    string
	store  *Store
	counts *countingRenderer
}

// countingRenderer envuelve el motor real y cuenta los renders 3MF.
type countingRenderer struct {
	Renderer
	artifacts atomic.Int32
}

func (c *countingRenderer) RenderArtifact(ctx context.Context, req render.Request, opts render.Options, ext, dir string) (render.Result, error) {
	c.artifacts.Add(1)
	return c.Renderer.RenderArtifact(ctx, req, opts, ext, dir)
}

func newRig(t *testing.T) *rig {
	t.Helper()
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	data := t.TempDir()
	eng, err := render.NewEngine(filepath.Join(data, "cache", "meshes"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	return &rig{
		t: t, dir: t.TempDir(), data: data, eng: eng, bin: bin,
		store:  NewStore(filepath.Join(data, "cache", "parts")),
		counts: &countingRenderer{Renderer: eng},
	}
}

func (r *rig) write(name, body string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) nameplate() {
	r.t.Helper()
	b, err := assets.FS.ReadFile(nameplateTemplate)
	if err != nil {
		r.t.Fatal(err)
	}
	r.write("001-nameplate.scad", string(b))
}

// source hace el render normal (el que dejaría el visor) y arma el Source.
func (r *rig) source(name string) Source {
	r.t.Helper()
	opts := render.Options{OpenSCADPath: r.bin, Backend: "manifold", TimeoutSeconds: 120}
	res, err := r.eng.Render(context.Background(), render.Request{WorkspaceKey: "test:view", WorkspaceDir: r.dir, FileName: name}, opts)
	if err != nil || res.Status != render.StatusOK {
		r.t.Fatalf("render: %+v err=%v", res, err)
	}
	return Source{
		Engine:      r.counts,
		Req:         render.Request{WorkspaceKey: "test:parts", WorkspaceDir: r.dir, FileName: name},
		Opts:        opts,
		ArtifactDir: filepath.Join(r.data, "cache", "exports"),
		MeshPath:    res.MeshPath,
	}
}

// La placa de dos colores sale en dos partes con los colores de la plantilla,
// los volúmenes suman el total y la segunda vez sale de la caché.
func TestNameplateTwoParts(t *testing.T) {
	r := newRig(t)
	r.nameplate()
	src := r.source("001-nameplate.scad")
	m, err := r.store.Ensure(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Parts) != 2 || m.Parts[0].Color != "#FFFFFF" || m.Parts[1].Color != "#D32F2F" {
		t.Fatalf("partes: %+v", m.Parts)
	}
	key := strings.TrimSuffix(filepath.Base(src.MeshPath), ".stl")
	if m.Key != key {
		t.Errorf("clave %q, quería %q", m.Key, key)
	}
	var sum float64
	for i, p := range m.Parts {
		if p.Index != i || p.URL != "/parts/"+key+"/"+FileName(i) || p.Uncolored || p.Name == "" {
			t.Errorf("parte %d: %+v", i, p)
		}
		sum += p.Info.VolumeMM3
		if p.Info.VolumeMM3 <= 0 || p.Info.AreaMM2 <= 0 {
			t.Errorf("parte %d sin volumen o área: %+v", i, p.Info)
		}
	}
	if rel := math.Abs(sum-m.Total.VolumeMM3) / m.Total.VolumeMM3; rel > 1e-3 {
		t.Errorf("volumen de las partes %.3f, del total %.3f (dif. relativa %.2g)", sum, m.Total.VolumeMM3, rel)
	}
	// Cada parte es un sólido cerrado (conserva la cara por la que toca a la otra): su
	// cantidad de triángulos no tiene por qué coincidir con la del sólido fundido.
	for i, p := range m.Parts {
		if !p.Info.Watertight {
			t.Errorf("parte %d: no es un sólido cerrado: %+v", i, p.Info)
		}
	}
	// El total es la malla del render normal.
	want, err := mesh.InfoFromFile(src.MeshPath)
	if err != nil || m.Total != want {
		t.Errorf("total %+v, quería %+v (err %v)", m.Total, want, err)
	}

	// En disco: un STL por parte y el manifest, sin temporales.
	dir := filepath.Join(r.store.Dir(), key+DirSuffix)
	entries, _ := os.ReadDir(r.store.Dir())
	if len(entries) != 1 {
		t.Errorf("la caché tiene que tener solo %s.parts: %v", key, entries)
	}
	for _, f := range []string{"00.stl", "01.stl", "manifest.json"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Size() == 0 {
			t.Errorf("falta %s", f)
		}
	}

	// Segunda vez: caché, sin otro render 3MF; Lookup la ve.
	n := r.counts.artifacts.Load()
	again, err := r.store.Ensure(context.Background(), src)
	if err != nil || r.counts.artifacts.Load() != n {
		t.Errorf("la segunda llamada tenía que salir de la caché (renders 3MF %d → %d, err %v)", n, r.counts.artifacts.Load(), err)
	}
	if again.Key != m.Key || len(again.Parts) != 2 {
		t.Errorf("manifest de la caché: %+v", again)
	}
	if got, ok := r.store.Lookup(key); !ok || got.Total != m.Total {
		t.Errorf("Lookup: %v %+v", ok, got)
	}
	// Otro Store sobre la misma carpeta (la app y el MCP comparten la caché).
	other := NewStore(r.store.Dir())
	if _, ok := other.Lookup(key); !ok {
		t.Error("otro Store no ve las partes en disco")
	}

	// Cada parte se sirve por el middleware byte a byte.
	srv := httptest.NewServer(meshserve.MiddlewareWithParts(filepath.Join(r.data, "cache", "meshes"), r.store.Dir())(http.NotFoundHandler()))
	defer srv.Close()
	for i, p := range m.Parts {
		resp, err := http.Get(srv.URL + p.URL)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		onDisk, err := os.ReadFile(filepath.Join(dir, FileName(i)))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != meshserve.ContentType || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("parte %d: código %d tipo %q", i, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if !bytes.Equal(got, onDisk) || len(got) != 84+50*p.Info.Triangles {
			t.Errorf("parte %d: servidos %d bytes, en disco %d, quería %d", i, len(got), len(onDisk), 84+50*p.Info.Triangles)
		}
		// El STL servido se analiza igual que lo que dice el manifest.
		info, err := mesh.ParseSTL(bytes.NewReader(got))
		if err != nil || info != p.Info {
			t.Errorf("parte %d: info servida %+v, del manifest %+v (err %v)", i, info, p.Info, err)
		}
	}
}

// Un modelo sin color() es una sola parte: la malla del render normal.
func TestUncoloredModelIsOnePart(t *testing.T) {
	r := newRig(t)
	r.write("001-cubo.scad", "cube(10);\n")
	src := r.source("001-cubo.scad")
	m, err := r.store.Ensure(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Parts) != 1 || !m.Parts[0].Uncolored || m.Parts[0].Color != "#F9D72C" || m.Parts[0].Info.Triangles != 12 {
		t.Fatalf("partes: %+v", m.Parts)
	}
	if math.Abs(m.Parts[0].Info.VolumeMM3-1000) > 1e-6 || m.Parts[0].Info != m.Total {
		t.Errorf("volumen %v, total %+v", m.Parts[0].Info, m.Total)
	}
	want, _ := os.ReadFile(src.MeshPath)
	got, err := os.ReadFile(filepath.Join(r.store.Dir(), m.Key+DirSuffix, "00.stl"))
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("la parte única tiene que ser la malla del render normal (err %v)", err)
	}
}

// Un solo color() es una parte con ese color (no «sin color»).
func TestSingleColorModel(t *testing.T) {
	r := newRig(t)
	r.write("001-rojo.scad", "color(\"red\") cube(10);\n")
	m, err := r.store.Ensure(context.Background(), r.source("001-rojo.scad"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Parts) != 1 || m.Parts[0].Uncolored || m.Parts[0].Color != "#FF0000" {
		t.Fatalf("partes: %+v", m.Parts)
	}
}

// Dos pedidos simultáneos de la misma clave generan una sola vez.
func TestConcurrentEnsureGeneratesOnce(t *testing.T) {
	r := newRig(t)
	r.nameplate()
	src := r.source("001-nameplate.scad")
	var wg sync.WaitGroup
	errs := make([]error, 6)
	ms := make([]Manifest, 6)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms[i], errs[i] = r.store.Ensure(context.Background(), src)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || len(ms[i].Parts) != 2 {
			t.Fatalf("llamada %d: %+v err=%v", i, ms[i], err)
		}
	}
	// Una generación = dos renders 3MF: el normal (sus partes salen abiertas porque los dos
	// colores se tocan) y el de lazy-union. Seis pedidos no los repiten.
	if n := r.counts.artifacts.Load(); n != 2 {
		t.Errorf("render 3MF %d veces, quería 2 (una sola generación)", n)
	}
}

// --- sin OpenSCAD: motor falso -----------------------------------------------

// cubeAt arma una parte cúbica de lado s con esquina en (x,0,0).
func cubeAt(x, s float64, c threemf.RGB, name string, n int) threemf.Part {
	v := func(a, b, cc float64) [3]float64 { return [3]float64{x + a*s, b * s, cc * s} }
	return threemf.Part{
		Name: name, Color: c, Extruder: n,
		Vertices: [][3]float64{v(0, 0, 0), v(1, 0, 0), v(1, 1, 0), v(0, 1, 0), v(0, 0, 1), v(1, 0, 1), v(1, 1, 1), v(0, 1, 1)},
		Triangles: [][3]int32{
			{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4},
			{1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7},
		},
	}
}

// fakeEngine devuelve un 3MF armado a mano y un render normal ya hecho.
type fakeEngine struct {
	dir       string
	parts     []threemf.Part
	status    string
	artifacts atomic.Int32
	gate      chan struct{} // si no es nil, RenderArtifact espera
}

func (f *fakeEngine) Render(context.Context, render.Request, render.Options) (render.Result, error) {
	return render.Result{Status: render.StatusOK}, nil
}

func (f *fakeEngine) RenderArtifact(ctx context.Context, _ render.Request, _ render.Options, _, _ string) (render.Result, error) {
	f.artifacts.Add(1)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return render.Result{Status: render.StatusCancelled}, nil
		}
	}
	if f.status != "" && f.status != render.StatusOK {
		return render.Result{Status: f.status, Diagnostics: []render.Diagnostic{{Severity: render.SeverityError, Line: 3, Message: "boom"}}}, nil
	}
	p := filepath.Join(f.dir, "fake.3mf")
	if err := threemf.WriteFile(p, f.parts, threemf.WriteOptions{Title: "x"}); err != nil {
		return render.Result{}, err
	}
	return render.Result{Status: render.StatusOK, MeshPath: p}, nil
}

const fakeKey = "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12"

// fakeSource escribe el STL del «render normal» (los dos cubos juntos) y arma
// el Source con el motor falso.
func fakeSource(t *testing.T, eng *fakeEngine) Source {
	t.Helper()
	eng.dir = t.TempDir()
	if eng.parts == nil {
		eng.parts = []threemf.Part{
			cubeAt(0, 10, threemf.RGB{255, 0, 0}, "Color 1 (#FF0000)", 1),
			cubeAt(20, 10, threemf.RGB{0, 0, 255}, "Color 2 (#0000FF)", 2),
		}
	}
	var all threemf.Part
	for _, p := range eng.parts {
		base := int32(len(all.Vertices))
		all.Vertices = append(all.Vertices, p.Vertices...)
		for _, tr := range p.Triangles {
			all.Triangles = append(all.Triangles, [3]int32{tr[0] + base, tr[1] + base, tr[2] + base})
		}
	}
	meshPath := filepath.Join(eng.dir, fakeKey+".stl")
	f, err := os.Create(meshPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := threemf.WriteSTL(f, all); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return Source{Engine: eng, Req: render.Request{FileName: "001-x.scad"}, ArtifactDir: eng.dir, MeshPath: meshPath}
}

func TestFakeEnginePartsAndFit(t *testing.T) {
	eng := &fakeEngine{}
	src := fakeSource(t, eng)
	st := NewStore(t.TempDir())
	m, err := st.Ensure(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Parts) != 2 || m.Parts[0].Color != "#FF0000" || m.Parts[1].Color != "#0000FF" {
		t.Fatalf("partes: %+v", m.Parts)
	}
	for _, p := range m.Parts {
		if math.Abs(p.Info.VolumeMM3-1000) > 1e-6 || !p.Info.Watertight || p.Info.Triangles != 12 {
			t.Errorf("parte %d: %+v", p.Index, p.Info)
		}
	}
	if m.Total.Size != [3]float64{30, 10, 10} || math.Abs(m.Total.VolumeMM3-2000) > 1e-6 {
		t.Errorf("total: %+v", m.Total)
	}

	// Model: cabe en una A1 mini (180) pero no en una placa de 25 mm.
	mini, _ := printers.ByID("bambu-a1-mini")
	mod := m.Model(mini)
	if !mod.Fits || mod.Overflow != (printers.Volume{}) || mod.Plate != mini.BuildVolume || mod.Key != fakeKey || len(mod.Parts) != 2 {
		t.Errorf("modelo: %+v", mod)
	}
	small := printers.Printer{BuildVolume: printers.Volume{X: 25, Y: 25, Z: 5}}
	mod = m.Model(small)
	if mod.Fits || mod.Overflow.X != 5 || mod.Overflow.Y != 0 || mod.Overflow.Z != 5 {
		t.Errorf("sobrante: %+v", mod.Overflow)
	}
	// Segunda vez: caché.
	if _, err := st.Ensure(context.Background(), src); err != nil || eng.artifacts.Load() != 1 {
		t.Errorf("caché: renders 3MF %d err %v", eng.artifacts.Load(), err)
	}
}

func TestFit(t *testing.T) {
	v := printers.Volume{X: 180, Y: 180, Z: 180}
	for _, c := range []struct {
		size [3]float64
		fits bool
		over printers.Volume
	}{
		{[3]float64{180, 180, 180}, true, printers.Volume{}},
		{[3]float64{180.04, 10, 10}, true, printers.Volume{}}, // dentro de la tolerancia
		{[3]float64{200, 10, 10}, false, printers.Volume{X: 20}},
		{[3]float64{180.06, 181.234, 300}, false, printers.Volume{X: 0.06, Y: 1.23, Z: 120}},
	} {
		fits, over := Fit(c.size, v)
		if fits != c.fits || over != c.over {
			t.Errorf("%v: cabe=%v sobra=%+v, quería %v %+v", c.size, fits, over, c.fits, c.over)
		}
	}
	// Un eje sin límite no cuenta.
	if fits, _ := Fit([3]float64{999, 1, 1}, printers.Volume{X: 0, Y: 10, Z: 10}); !fits {
		t.Error("eje sin dato no debe fallar")
	}
}

// Cien pedidos a la vez con un render 3MF lento: una sola generación.
func TestFakeEngineSingleFlight(t *testing.T) {
	gate := make(chan struct{})
	eng := &fakeEngine{gate: gate}
	src := fakeSource(t, eng)
	st := NewStore(t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := st.Ensure(context.Background(), src)
			if err == nil && len(m.Parts) != 2 {
				err = errors.New("partes inesperadas")
			}
			errs <- err
		}()
	}
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if n := eng.artifacts.Load(); n != 1 {
		t.Errorf("render 3MF %d veces, quería 1", n)
	}
}

// Si el que genera se cancela, el que esperaba no hereda esa cancelación.
func TestWaiterRetriesAfterLeaderCancelled(t *testing.T) {
	gate := make(chan struct{})
	eng := &fakeEngine{gate: gate}
	src := fakeSource(t, eng)
	st := NewStore(t.TempDir())

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() { _, err := st.Ensure(leaderCtx, src); leaderErr <- err }()
	for eng.artifacts.Load() == 0 { // el líder ya está dentro del render
		runtimeGosched()
	}
	waiter := make(chan error, 1)
	go func() { _, err := st.Ensure(context.Background(), src); waiter <- err }()
	cancelLeader()
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Errorf("el líder tiene que volver cancelado: %v", err)
	}
	close(gate) // el reintento del que esperaba ya puede terminar
	if err := <-waiter; err != nil {
		t.Errorf("el que esperaba tiene que reintentar y terminar bien: %v", err)
	}
}

func TestFakeEngineFallbacksAndErrors(t *testing.T) {
	// Falla el 3MF pero el STL estaba bien: una sola parte, sin error.
	eng := &fakeEngine{status: render.StatusError}
	src := fakeSource(t, eng)
	m, err := NewStore(t.TempDir()).Ensure(context.Background(), src)
	if err != nil || len(m.Parts) != 1 || !m.Parts[0].Uncolored || m.Parts[0].Info != m.Total {
		t.Fatalf("3MF con error: %+v err=%v", m.Parts, err)
	}
	// OpenSCAD ausente o cancelado: error.
	for _, status := range []string{render.StatusMissing, render.StatusCancelled} {
		eng := &fakeEngine{status: status}
		src := fakeSource(t, eng)
		if _, err := NewStore(t.TempDir()).Ensure(context.Background(), src); err == nil {
			t.Errorf("%s: quería error", status)
		}
	}
	// Una malla que no es de la caché.
	if _, err := NewStore(t.TempDir()).Ensure(context.Background(), Source{Engine: eng, MeshPath: "/tmp/x.stl"}); err == nil {
		t.Error("ruta que no es una clave: quería error")
	}
}

// Una carpeta de una versión vieja o rota se regenera; los temporales viejos se
// limpian al podar.
func TestStaleOrBrokenCacheIsRegenerated(t *testing.T) {
	eng := &fakeEngine{}
	src := fakeSource(t, eng)
	dir := t.TempDir()
	st := NewStore(dir)
	if _, err := st.Ensure(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, fakeKey+DirSuffix)
	// Se rompe una parte.
	if err := os.Remove(filepath.Join(final, "01.stl")); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Lookup(fakeKey); ok {
		t.Fatal("una caché con una parte menos no es válida")
	}
	m, err := st.Ensure(context.Background(), src)
	if err != nil || len(m.Parts) != 2 {
		t.Fatalf("regenerar: %+v err=%v", m, err)
	}
	if _, ok := st.Lookup(fakeKey); !ok {
		t.Error("tras regenerar tiene que ser válida")
	}
	// Un manifest ilegible.
	if err := os.WriteFile(filepath.Join(final, "manifest.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Lookup(fakeKey); ok {
		t.Error("manifest roto")
	}
	if _, err := st.Ensure(context.Background(), src); err != nil {
		t.Fatalf("regenerar manifest roto: %v", err)
	}
	// Ninguna carpeta temporal queda.
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("quedó un temporal: %s", e.Name())
		}
	}
	// Lookup rechaza claves que no lo son.
	if _, ok := st.Lookup("../" + fakeKey); ok {
		t.Error("Lookup aceptó una clave inválida")
	}
}

func TestKeyFromMeshPath(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{"/x/cache/meshes/" + fakeKey + ".stl", true},
		{`C:\d\meshes\` + fakeKey + ".stl", true},
		{fakeKey + ".stl", true},
		{"/x/" + fakeKey + ".json", false},
		{"/x/" + strings.ToUpper(fakeKey) + ".stl", false},
		{"/x/short.stl", false},
		{"", false},
	} {
		k, err := KeyFromMeshPath(c.in)
		if (err == nil) != c.ok || (c.ok && k != fakeKey) {
			t.Errorf("%q: %q %v", c.in, k, err)
		}
	}
}

func TestTooManyParts(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	var ps []threemf.Part
	for i := range MaxParts + 1 {
		ps = append(ps, cubeAt(float64(i)*2, 1, threemf.RGB{uint8(i), uint8(i >> 8), 7}, "c", i+1))
	}
	eng := &fakeEngine{parts: ps}
	src := fakeSource(t, eng)
	_, err := NewStore(t.TempDir()).Ensure(context.Background(), src)
	if err == nil || !strings.Contains(err.Error(), "colores") {
		t.Errorf("demasiados colores: %v", err)
	}
}

func runtimeGosched() { runtime.Gosched() }

// --- maceta con relieve: partes cerradas (S17, tarea 17.2) -------------------

// La maceta verde con relieve naranja: las dos partes del visor son sólidos cerrados (en el
// STL de cada una, aristas compartidas por exactamente dos caras) con el volumen de ese color.
// Sin lazy-union salían como cáscaras abiertas con volúmenes sin sentido (21 951 y 1 314 mm³
// en vez de 23 132 y 164).
func TestPotPartsAreWatertight(t *testing.T) {
	r := newRig(t)
	r.write("001-maceta.scad", macetatest.Scad)
	m, err := r.store.Ensure(context.Background(), r.source("001-maceta.scad"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Parts) != 2 {
		t.Fatalf("partes: %+v", m.Parts)
	}
	want := []float64{macetatest.Ref.GreenVol, macetatest.Ref.OrangeVol}
	for i, p := range m.Parts {
		if !p.Info.Watertight {
			t.Errorf("parte %d no es un sólido cerrado: %+v", i, p.Info)
		}
		if math.Abs(p.Info.VolumeMM3-want[i])/want[i] > 1e-3 {
			t.Errorf("parte %d: volumen %.3f, el del STL de ese color %.3f", i, p.Info.VolumeMM3, want[i])
		}
	}
	if n := r.counts.artifacts.Load(); n != 2 {
		t.Errorf("render 3MF %d veces, quería 2 (normal + lazy-union)", n)
	}

	// Un manifest de la versión anterior (partes abiertas) no se reutiliza.
	path := filepath.Join(r.store.Dir(), m.Key+DirSuffix, manifestName)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(string(b), `"version": 2`, `"version": 1`, 1)
	if old == string(b) {
		t.Fatalf("el manifest no declara version 2:\n%s", b)
	}
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.store.Lookup(m.Key); ok {
		t.Error("un manifest de la versión 1 (partes abiertas) no se tiene que servir")
	}
}

// lazyEngine sirve el 3MF por defecto de OpenSCAD (partes abiertas) y, si se le pide
// LazyUnion, el de lazy-union (partes cerradas).
type lazyEngine struct {
	dir       string
	calls     []bool // LazyUnion de cada llamada
	lazyFails bool
}

func (l *lazyEngine) Render(context.Context, render.Request, render.Options) (render.Result, error) {
	return render.Result{Status: render.StatusOK}, nil
}

func (l *lazyEngine) RenderArtifact(_ context.Context, _ render.Request, opts render.Options, _, _ string) (render.Result, error) {
	l.calls = append(l.calls, opts.LazyUnion)
	body := macetatest.Union
	if opts.LazyUnion {
		if l.lazyFails {
			return render.Result{Status: render.StatusError, Diagnostics: []render.Diagnostic{{Severity: render.SeverityError, Message: "boom"}}}, nil
		}
		body = macetatest.Lazy
	}
	p := filepath.Join(l.dir, "m.3mf")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		return render.Result{}, err
	}
	return render.Result{Status: render.StatusOK, MeshPath: p}, nil
}

func TestLoad3MFRendersAgainWithLazyUnionWhenPartsAreOpen(t *testing.T) {
	eng := &lazyEngine{dir: t.TempDir()}
	split, _, err := Load3MF(context.Background(), eng, render.Request{}, render.Options{}, "")
	if err != nil {
		t.Fatal(err)
	}
	macetatest.AssertSolids(t, split, 1e-3)
	if len(eng.calls) != 2 || eng.calls[0] || !eng.calls[1] {
		t.Errorf("llamadas (LazyUnion): %v, quería [false true]", eng.calls)
	}

	// Pedir ya LazyUnion no encadena otro render.
	eng = &lazyEngine{dir: t.TempDir()}
	split, _, err = Load3MF(context.Background(), eng, render.Request{}, render.Options{LazyUnion: true}, "")
	if err != nil || len(eng.calls) != 1 || len(split) != 2 {
		t.Errorf("LazyUnion explícito: llamadas %v, partes %d, err %v", eng.calls, len(split), err)
	}

	// Si el segundo render falla se queda con lo que ya tenía (la exportación avisa).
	eng = &lazyEngine{dir: t.TempDir(), lazyFails: true}
	split, _, err = Load3MF(context.Background(), eng, render.Request{}, render.Options{}, "")
	if err != nil || len(split) != 2 || threemf.OpenEdgeCount(split) == 0 {
		t.Errorf("sin lazy-union: partes %d, aristas abiertas %d, err %v", len(split), threemf.OpenEdgeCount(split), err)
	}
}

// Partes que ya vienen cerradas (colores separados) no pagan el segundo render.
func TestLoad3MFSkipsLazyUnionWhenPartsAreClosed(t *testing.T) {
	eng := &fakeEngine{}
	src := fakeSource(t, eng)
	if _, _, err := Load3MF(context.Background(), eng, src.Req, src.Opts, ""); err != nil {
		t.Fatal(err)
	}
	if n := eng.artifacts.Load(); n != 1 {
		t.Errorf("render 3MF %d veces, quería 1", n)
	}
}
