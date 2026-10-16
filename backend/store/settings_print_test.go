package store

import (
	"math"
	"testing"
)

// Los parámetros de la estimación (S7) tienen valor de fábrica, también en una
// base anterior a S7 que no trae las claves.
func TestPrintSettingsDefaults(t *testing.T) {
	s := openTemp(t)
	got, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.WallLoops != 2 || got.LineWidthMm != 0.42 || got.FilamentDiameterMm != 1.75 || got.Currency != "USD" {
		t.Errorf("defaults: %d %v %v %q", got.WallLoops, got.LineWidthMm, got.FilamentDiameterMm, got.Currency)
	}
}

// Un valor fuera de rango no es un error: vuelve al de fábrica (misma política
// que el editor y la apariencia). Un cliente que no conoce los campos los manda
// en cero y tampoco pisa nada con un cero.
func TestPrintSettingsNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   Settings
		want Settings
	}{
		{"cero", Settings{}, Settings{WallLoops: 2, LineWidthMm: 0.42, FilamentDiameterMm: 1.75, Currency: "USD"}},
		{"válidos", Settings{WallLoops: 3, LineWidthMm: 0.6, FilamentDiameterMm: 2.85, Currency: "eur"}, Settings{WallLoops: 3, LineWidthMm: 0.6, FilamentDiameterMm: 2.85, Currency: "EUR"}},
		{"bordes", Settings{WallLoops: 10, LineWidthMm: 0.2, FilamentDiameterMm: 3.5, Currency: " gtq "}, Settings{WallLoops: 10, LineWidthMm: 0.2, FilamentDiameterMm: 3.5, Currency: "GTQ"}},
		{"fuera", Settings{WallLoops: 11, LineWidthMm: 2, FilamentDiameterMm: 0.5, Currency: "DOLAR"}, Settings{WallLoops: 2, LineWidthMm: 0.42, FilamentDiameterMm: 1.75, Currency: "USD"}},
		{"negativos", Settings{WallLoops: -1, LineWidthMm: -0.4, FilamentDiameterMm: -1.75, Currency: "U$D"}, Settings{WallLoops: 2, LineWidthMm: 0.42, FilamentDiameterMm: 1.75, Currency: "USD"}},
		{"NaN", Settings{WallLoops: 2, LineWidthMm: math.NaN(), FilamentDiameterMm: math.Inf(1), Currency: "MXN"}, Settings{WallLoops: 2, LineWidthMm: 0.42, FilamentDiameterMm: 1.75, Currency: "MXN"}},
	}
	for _, c := range cases {
		in := c.in
		in.normalizePrint()
		if in.WallLoops != c.want.WallLoops || in.LineWidthMm != c.want.LineWidthMm || in.FilamentDiameterMm != c.want.FilamentDiameterMm || in.Currency != c.want.Currency {
			t.Errorf("%s: %d %v %v %q, quería %d %v %v %q", c.name, in.WallLoops, in.LineWidthMm, in.FilamentDiameterMm, in.Currency,
				c.want.WallLoops, c.want.LineWidthMm, c.want.FilamentDiameterMm, c.want.Currency)
		}
	}
}

// SaveSettings guarda los valores válidos, normaliza los inválidos y GetSettings
// los devuelve igual.
func TestPrintSettingsRoundTrip(t *testing.T) {
	s := openTemp(t)
	cur, _ := s.GetSettings()
	cur.WallLoops, cur.LineWidthMm, cur.FilamentDiameterMm, cur.Currency = 4, 0.5, 2.85, "gtq"
	if err := s.SaveSettings(cur); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.WallLoops != 4 || got.LineWidthMm != 0.5 || got.FilamentDiameterMm != 2.85 || got.Currency != "GTQ" {
		t.Errorf("guardado: %d %v %v %q", got.WallLoops, got.LineWidthMm, got.FilamentDiameterMm, got.Currency)
	}

	cur.WallLoops, cur.LineWidthMm = 99, 9
	if err := s.SaveSettings(cur); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSettings()
	if got.WallLoops != 2 || got.LineWidthMm != 0.42 {
		t.Errorf("fuera de rango: %d %v", got.WallLoops, got.LineWidthMm)
	}
}
