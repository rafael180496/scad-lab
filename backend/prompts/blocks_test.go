package prompts

import "testing"

func TestScadBlocks(t *testing.T) {
	tests := []struct {
		name string
		md   string
		want []Block // Start/End se comprueban aparte cuando importan
	}{
		{
			name: "openscad etiquetado",
			md:   "Hola\n```openscad\ncube(10);\n```\nChau",
			want: []Block{{Code: "cube(10);", Lang: "openscad"}},
		},
		{
			name: "scad etiquetado y mayúsculas",
			md:   "```SCAD\nsphere(3);\n```",
			want: []Block{{Code: "sphere(3);", Lang: "scad"}},
		},
		{
			name: "sin lenguaje que parece openscad",
			md:   "```\nmodule box(w) { cube([w, w, w]); }\nbox(5);\n```",
			want: []Block{{Code: "module box(w) { cube([w, w, w]); }\nbox(5);", Lang: ""}},
		},
		{
			name: "sin lenguaje con $fn",
			md:   "```\n$fn = 64;\ncircle(r = 2);\n```",
			want: []Block{{Code: "$fn = 64;\ncircle(r = 2);", Lang: ""}},
		},
		{
			name: "sin lenguaje con dos indicios débiles y punto y coma",
			md:   "```\ncolor(\"red\") square(4);\n```",
			want: []Block{{Code: "color(\"red\") square(4);", Lang: ""}},
		},
		{
			name: "sin lenguaje que no es openscad",
			md:   "```\nnpm install\nls -la;\n```\n```\nSELECT * FROM t;\n```",
			want: nil,
		},
		{
			name: "css con translate no es openscad",
			md:   "```\n.a { transform: translate(10px, 5px); color: red; }\n```",
			want: nil,
		},
		{
			name: "otro lenguaje etiquetado se ignora aunque parezca openscad",
			md:   "```python\ncube(10)\ntranslate([1,2,3])\n```\n```cpp\ncube(1);\n```",
			want: nil,
		},
		{
			name: "mezcla: solo los openscad, en orden",
			md:   "```bash\nopenscad -o x.stl a.scad\n```\ntexto\n```openscad\na();\n```\n~~~scad\nb();\n~~~\n```json\n{}\n```",
			want: []Block{{Code: "a();", Lang: "openscad"}, {Code: "b();", Lang: "scad"}},
		},
		{
			name: "fence de cuatro backticks que contiene tres",
			md:   "````openscad\n// ```\ncube(1);\n````\nfin",
			want: []Block{{Code: "// ```\ncube(1);", Lang: "openscad"}},
		},
		{
			name: "tres backticks no cierran una tilde",
			md:   "~~~openscad\ncube(1);\n```\ncube(2);\n~~~",
			want: []Block{{Code: "cube(1);\n```\ncube(2);", Lang: "openscad"}},
		},
		{
			name: "cierre más largo y con espacios",
			md:   "```openscad\ncube(1);\n`````  \nfin",
			want: []Block{{Code: "cube(1);", Lang: "openscad"}},
		},
		{
			name: "cierre más corto no cierra",
			md:   "````openscad\ncube(1);\n```\n",
			want: []Block{{Code: "cube(1);\n```", Lang: "openscad", Partial: true}},
		},
		{
			name: "sin cerrar (streaming)",
			md:   "Voy a crear:\n```openscad\n// @checkpoint parent= desc=\"x\"\ncube(",
			want: []Block{{Code: "// @checkpoint parent= desc=\"x\"\ncube(", Lang: "openscad", Partial: true}},
		},
		{
			name: "sin cerrar con cierre a medias",
			md:   "```openscad\ncube(1);\n``",
			want: []Block{{Code: "cube(1);", Lang: "openscad", Partial: true}},
		},
		{
			name: "recién abierto",
			md:   "```openscad\n",
			want: []Block{{Code: "", Lang: "openscad", Partial: true}},
		},
		{
			name: "sin cerrar y sin lenguaje que no parece scad",
			md:   "```\nhola",
			want: nil,
		},
		{
			name: "dentro de una lista, con sangría",
			md:   "1. Paso:\n   ```openscad\n   cube(1);\n     translate([1,0,0]) cube(1);\n   ```\n",
			want: []Block{{Code: "cube(1);\n  translate([1,0,0]) cube(1);", Lang: "openscad"}},
		},
		{
			name: "CRLF",
			md:   "```openscad\r\ncube(1);\r\nsphere(2);\r\n```\r\nfin",
			want: []Block{{Code: "cube(1);\nsphere(2);", Lang: "openscad"}},
		},
		{
			name: "etiqueta con atributos",
			md:   "```openscad title=\"x\"\ncube(1);\n```\n```{.scad}\nsphere(1);\n```",
			want: []Block{{Code: "cube(1);", Lang: "openscad"}, {Code: "sphere(1);", Lang: "scad"}},
		},
		{
			name: "código en línea con tres backticks no abre bloque",
			md:   "usa ```cube(1)``` así\ncube(2);\n",
			want: nil,
		},
		{
			name: "sin bloques",
			md:   "solo texto\n\notra línea",
			want: nil,
		},
		{name: "vacío", md: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScadBlocks(tt.md)
			if len(got) != len(tt.want) {
				t.Fatalf("%d bloques, quería %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				g := got[i]
				if g.Code != w.Code || g.Lang != w.Lang || g.Partial != w.Partial {
					t.Errorf("bloque %d = %+v, quería %+v", i, g, w)
				}
				if g.Start < 0 || g.End > len(tt.md) || g.Start >= g.End {
					t.Errorf("bloque %d: offsets fuera de rango %d..%d de %d", i, g.Start, g.End, len(tt.md))
				}
			}
		})
	}
}

func TestScadBlocksOffsets(t *testing.T) {
	md := "Antes\n```openscad\ncube(1);\n```\nDespués"
	got := ScadBlocks(md)
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if s := md[got[0].Start:got[0].End]; s != "```openscad\ncube(1);\n```" {
		t.Errorf("md[Start:End] = %q", s)
	}
	crlf := "A\r\n```scad\r\nx();\r\n```\r\nB"
	b := ScadBlocks(crlf)
	if len(b) != 1 || crlf[b[0].Start:b[0].End] != "```scad\r\nx();\r\n```" {
		t.Errorf("CRLF: %+v", b)
	}
	partial := "x\n```openscad\ncube("
	p := ScadBlocks(partial)
	if len(p) != 1 || !p[0].Partial || p[0].End != len(partial) || p[0].Start != 2 {
		t.Errorf("parcial: %+v", p)
	}
}

func TestScadBlocksStreamingPrefixes(t *testing.T) {
	// Cada prefijo de una respuesta válida: nunca entra en pánico, a lo sumo un
	// bloque, y solo el último prefijo completo lo devuelve sin Partial.
	full := "Texto.\n\n```openscad\n// @checkpoint parent= desc=\"c\"\ncube(10);\n```\n\nFin.\n"
	for i := 0; i <= len(full); i++ {
		bs := ScadBlocks(full[:i])
		if len(bs) > 1 {
			t.Fatalf("prefijo %d: %d bloques", i, len(bs))
		}
		for _, b := range bs {
			if !b.Partial && i < len("Texto.\n\n```openscad\n// @checkpoint parent= desc=\"c\"\ncube(10);\n```") {
				t.Fatalf("prefijo %d: bloque completo antes de cerrar", i)
			}
		}
	}
}
