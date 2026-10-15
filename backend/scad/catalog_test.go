package scad

import (
	"strings"
	"testing"
)

// El catálogo embebido carga y cada entrada está completa en los dos idiomas.
func TestCatalog(t *testing.T) {
	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if c.Version == "" || len(c.Entries) < 80 {
		t.Fatalf("catálogo incompleto: versión %q, %d entradas", c.Version, len(c.Entries))
	}
	kinds := map[string]bool{"module": true, "function": true, "keyword": true, "special": true}
	seen := map[string]bool{}
	for _, e := range c.Entries {
		if e.Name == "" || !kinds[e.Kind] || strings.TrimSpace(e.Signature) == "" {
			t.Errorf("entrada mal formada: %+v", e)
		}
		if strings.TrimSpace(e.Doc.ES) == "" || strings.TrimSpace(e.Doc.EN) == "" {
			t.Errorf("%s: falta doc en algún idioma", e.Name)
		}
		if seen[e.Name] {
			t.Errorf("%s: repetida", e.Name)
		}
		seen[e.Name] = true
		if (e.Kind == "module" || e.Kind == "function") && !strings.HasPrefix(e.Signature, e.Name+"(") {
			t.Errorf("%s: la firma no empieza con el nombre: %q", e.Name, e.Signature)
		}
		for _, p := range e.Params {
			if p.Name == "" || strings.TrimSpace(p.Doc.ES) == "" || strings.TrimSpace(p.Doc.EN) == "" {
				t.Errorf("%s: parámetro incompleto %+v", e.Name, p)
			}
			// Cada parámetro documentado aparece en la firma (salvo las
			// variables especiales que se aceptan como argumento).
			if !strings.HasPrefix(p.Name, "$") && !strings.Contains(e.Signature, p.Name) {
				t.Errorf("%s: el parámetro %s no está en la firma %q", e.Name, p.Name, e.Signature)
			}
		}
	}
	// Lo que pide el spec, como mínimo.
	required := strings.Fields(`cube sphere cylinder polyhedron square circle polygon text import surface
		translate rotate scale resize mirror multmatrix color offset hull minkowski union difference
		intersection linear_extrude rotate_extrude projection render children echo assert let for
		intersection_for if each function module include use $fn $fa $fs $t $vpr $vpt $vpd $vpf
		$children $preview abs sin cos atan2 sqrt pow min max norm cross len concat lookup str chr ord search
		is_undef is_num is_list is_string is_function rands version version_num`)
	for _, name := range required {
		if !seen[name] {
			t.Errorf("falta %s", name)
		}
	}
	// El catálogo conoce todas las palabras clave del lexer.
	for kw := range keywords {
		if !seen[kw] {
			t.Errorf("palabra clave del lexer sin entrada: %s", kw)
		}
	}
}
