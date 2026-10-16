package main

import (
	"bytes"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/estimate"
	"scadlab/backend/meshserve"
)

// renderedRig es exportRig (dos cubos, rojo y azul, de 10 mm) con el checkpoint
// ya renderizado, que es lo que GetModelParts y EstimatePrint exigen.
func renderedRig(t *testing.T) (*App, int64, string) {
	t.Helper()
	a, id, name := exportRig(t)
	if res, err := a.RenderCheckpoint(id, name); err != nil || res.Status != "ok" {
		t.Fatalf("render: %+v err=%v", res, err)
	}
	return a, id, name
}

func TestGetModelParts(t *testing.T) {
	a, id, name := renderedRig(t)
	m, err := a.GetModelParts(id, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Parts) != 2 || m.Parts[0].Color != "#FF0000" || m.Parts[1].Color != "#0000FF" {
		t.Fatalf("partes: %+v", m.Parts)
	}
	if len(m.Key) != 64 || m.Parts[0].URL != "/parts/"+m.Key+"/00.stl" || m.Parts[1].URL != "/parts/"+m.Key+"/01.stl" {
		t.Errorf("urls: %+v", m.Parts)
	}
	// Dos cubos de 10 mm separados 10 mm: 30×10×10 en una A1 mini (180).
	if m.Total.Size != [3]float64{30, 10, 10} || math.Abs(m.Total.VolumeMM3-2000) > 1e-6 {
		t.Errorf("total: %+v", m.Total)
	}
	if !m.Fits || m.Plate.X != 180 || m.Plate.Y != 180 || m.Plate.Z != 180 || m.Overflow.X != 0 {
		t.Errorf("placa: fits=%v plate=%+v overflow=%+v", m.Fits, m.Plate, m.Overflow)
	}
	// El middleware sirve cada parte desde la caché que usa el binding.
	h := meshserve.MiddlewareWithParts(filepath.Join(a.dataDir, "cache", "meshes"), partsCacheDir(a.dataDir))(http.NotFoundHandler())
	for _, p := range m.Parts {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p.URL, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "model/stl" {
			t.Fatalf("%s: código %d", p.URL, rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		if len(body) != 84+50*p.Info.Triangles {
			t.Errorf("%s: %d bytes", p.URL, len(body))
		}
	}
	// Segunda vez: el mismo modelo.
	again, err := a.GetModelParts(id, name)
	if err != nil || again.Key != m.Key || len(again.Parts) != 2 {
		t.Errorf("segunda vez: %+v err=%v", again, err)
	}
}

func TestGetModelPartsOverflow(t *testing.T) {
	a, id, _ := exportRig(t)
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	name := "002-enorme.scad"
	body := "// @checkpoint parent=001-dos.scad desc=\"no cabe\"\ncolor(\"green\") cube([200, 50, 20]);\n"
	if err := os.WriteFile(filepath.Join(w.Path, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RenderCheckpoint(id, name); err != nil {
		t.Fatal(err)
	}
	m, err := a.GetModelParts(id, name)
	if err != nil {
		t.Fatal(err)
	}
	if m.Fits || m.Overflow.X != 20 || m.Overflow.Y != 0 || m.Overflow.Z != 0 {
		t.Errorf("200 mm en 180: fits=%v overflow=%+v", m.Fits, m.Overflow)
	}
	if len(m.Parts) != 1 || m.Parts[0].Color != "#008000" || m.Parts[0].Uncolored {
		t.Errorf("partes: %+v", m.Parts)
	}
}

func TestPartsRequireOKRender(t *testing.T) {
	a, id, name := exportRig(t) // sin RenderCheckpoint
	if _, err := a.GetModelParts(id, name); err == nil || !strings.Contains(err.Error(), name) {
		t.Errorf("sin render: quería un error que nombre el checkpoint, vino %v", err)
	}
	if _, err := a.EstimatePrint(id, name, estimate.Options{}); err == nil {
		t.Error("EstimatePrint sin render: quería error")
	}
	if _, err := a.GetModelParts(id, "../x.scad"); err == nil {
		t.Error("nombre con carpeta")
	}
	if _, err := a.GetModelParts(999, name); err == nil {
		t.Error("workspace inexistente")
	}
}

func TestEstimatePrint(t *testing.T) {
	a, id, name := renderedRig(t)

	// Opciones vacías: los ajustes (15 % de relleno, 2 × 0,42, PLA, USD).
	r, err := a.EstimatePrint(id, name, estimate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Cubo de 10 mm: cáscara min(1000, 600×0,84) = 504; efectivo 504 + 496×0,15 = 578,4.
	if len(r.Parts) != 2 || math.Abs(r.Parts[0].EffectiveMm3-578.4) > 1e-6 || r.Parts[0].MaterialID != "pla" || r.Currency != "USD" {
		t.Fatalf("estimación: %+v", r)
	}
	if math.Abs(r.Total.MassG-2*578.4/1000*1.24) > 1e-9 || len(r.Assumptions) < 4 {
		t.Errorf("total: %+v", r.Total)
	}

	// Cambiar el relleno y el material de una parte (la 1) en vivo.
	r, err = a.EstimatePrint(id, name, estimate.Options{
		InfillPercent: 100, WallLoops: 3, LineWidthMm: 0.4, FilamentDiameterMm: 1.75,
		DefaultMaterialID: "pla", PartMaterials: map[int]string{1: "abs"}, Currency: "EUR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Parts[0].MaterialID != "pla" || r.Parts[1].MaterialID != "abs" || r.Currency != "EUR" {
		t.Fatalf("materiales: %+v", r)
	}
	if math.Abs(r.Parts[0].EffectiveMm3-1000) > 1e-6 || math.Abs(r.Parts[1].MassG-1.04) > 1e-9 {
		t.Errorf("relleno 100 %%: %+v", r.Parts)
	}
	if math.Abs(r.Parts[1].Cost-1.04/1000*22) > 1e-12 {
		t.Errorf("costo de la parte 1: %v", r.Parts[1].Cost)
	}

	// Relleno 0 con el conjunto completo: solo cáscara (3 × 0,4 = 1,2 mm → 720 mm³).
	r, err = a.EstimatePrint(id, name, estimate.Options{InfillPercent: 0, WallLoops: 3, LineWidthMm: 0.4, FilamentDiameterMm: 1.75})
	if err != nil || math.Abs(r.Parts[0].EffectiveMm3-600*1.2) > 1e-6 {
		t.Errorf("relleno 0: %+v err=%v", r.Parts, err)
	}

	// Un material que no existe: error que lo nombra.
	_, err = a.EstimatePrint(id, name, estimate.Options{DefaultMaterialID: "unobtanium"})
	if err == nil || !strings.Contains(err.Error(), "unobtanium") {
		t.Errorf("material inexistente: %v", err)
	}
}

// EstimatePrint genera el manifest si todavía no está (sin pasar por
// GetModelParts) y después no vuelve a generar.
func TestEstimatePrintGeneratesMissingParts(t *testing.T) {
	a, id, name := renderedRig(t)
	if _, err := a.EstimatePrint(id, name, estimate.Options{}); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(partsCacheDir(a.dataDir))
	if err != nil || len(ents) != 1 || !strings.HasSuffix(ents[0].Name(), ".parts") {
		t.Fatalf("caché de partes: %v err=%v", ents, err)
	}
	// El STL de la parte coincide con lo que dice el modelo.
	m, _ := a.GetModelParts(id, name)
	data, err := os.ReadFile(filepath.Join(partsCacheDir(a.dataDir), ents[0].Name(), "00.stl"))
	if err != nil || len(data) != 84+50*m.Parts[0].Info.Triangles || !bytes.HasPrefix(data, []byte("ScadLab STL")) {
		t.Errorf("00.stl: %d bytes err=%v", len(data), err)
	}
}
