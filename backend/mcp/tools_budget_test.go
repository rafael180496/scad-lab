package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// S17, C: tope duro de bytes por herramienta. Corre CADA herramienta del catálogo
// sobre la maceta de prueba (≈600 líneas) y mide lo que le devuelve al modelo:
// ningún resultado de texto pasa de 6 KB sin `verbose`/`full`. Imprime la tabla
// antes/después (antes = el detalle completo con verbose o la lectura entera, que
// es lo que devolvían estas herramientas antes de S17).

const toolResultCap = 6 << 10

// budgetExempt son las herramientas cuyo resultado ES el contenido que se pidió y
// tiene su propio tope por diseño: read_checkpoint (12 KB sin rango, 400 líneas con
// rango), read_skill y get_template (un documento), format (el código formateado).
var budgetExempt = map[string]string{
	"read_checkpoint": "tope propio: 12 KB sin rango / 400 líneas con rango",
	"read_skill":      "devuelve el documento pedido",
	"get_template":    "devuelve la plantilla pedida",
	"format":          "devuelve el código formateado",
}

type budgetRow struct {
	tool, call    string
	before, after int
	imgBefore     int // base64 de las imágenes, antes
	imgAfter      int
	isErr         bool
}

func b64len(imgs []ToolImage) int {
	n := 0
	for _, im := range imgs {
		n += (len(im.Data) + 2) / 3 * 4
	}
	return n
}

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if (x-48)*(x-48)+(y-48)*(y-48) < 30*30 {
				c = color.RGBA{200, 40, 40, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestToolOutputBudgetTable(t *testing.T) {
	needOpenSCAD(t)
	tb, ws, _, _ := registeredToolbox(t)
	src := macetaSource(t)
	pot := tb.create(t, "maceta", src, nil)
	mini := tb.create(t, "cubo", srcCube, map[string]any{"parent": pot})
	spare := tb.create(t, "sobrante", srcCube, map[string]any{"parent": mini})
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "soporte.stl"))
	writeTestPNG(t, filepath.Join(ws, "refs", "images", "logo.png"))
	disk := string(mustRead(t, filepath.Join(ws, pot)))

	var rows []budgetRow
	seen := map[string]bool{}
	ctx := context.Background()
	type callSpec struct {
		tool string
		args map[string]any
	}
	// run llama a la herramienta y anota su tamaño. before es la llamada que daba
	// antes de S17 lo mismo (o lo más parecido): el detalle completo con verbose, o
	// leer el archivo entero para orientarse en vez de outline/find.
	run := func(tool string, args map[string]any, call string, before *callSpec) ToolOutput {
		t.Helper()
		out, err := tb.Call(ctx, tool, args)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		r := budgetRow{tool: tool, call: call, after: len(out.Text), imgAfter: b64len(out.Images), isErr: out.IsError}
		r.before, r.imgBefore = r.after, r.imgAfter
		if before != nil {
			v, err := tb.Call(ctx, before.tool, before.args)
			if err != nil {
				t.Fatalf("%s (antes): %v", tool, err)
			}
			r.before, r.imgBefore = len(v.Text), b64len(v.Images)
		}
		rows = append(rows, r)
		seen[tool] = true
		return out
	}
	with := func(base map[string]any, extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	run("list_checkpoints", nil, "", nil)
	// read_checkpoint: antes = el archivo entero (full); ahora, sin rango (recortado) y por rango.
	r := run("read_checkpoint", map[string]any{"file": pot}, "sin rango (recortado)", &callSpec{"read_checkpoint", map[string]any{"file": pot, "full": true}})
	_ = r
	run("read_checkpoint", map[string]any{"file": pot, "from": 440, "to": 480}, "from=440 to=480", &callSpec{"read_checkpoint", map[string]any{"file": pot, "full": true}})
	run("outline_checkpoint", map[string]any{"file": pot}, "", &callSpec{"read_checkpoint", map[string]any{"file": pot, "full": true}}) // «antes» = leer el archivo para orientarse
	run("find_in_checkpoint", map[string]any{"file": pot, "query": "cascaron_relieve"}, "cascaron_relieve", &callSpec{"read_checkpoint", map[string]any{"file": pot, "full": true}})
	run("get_parameters", map[string]any{"file": pot}, "", &callSpec{"get_parameters", map[string]any{"file": pot, "verbose": true}})
	run("printer_info", nil, "", nil)
	run("lint", map[string]any{"file": pot}, "", &callSpec{"lint", map[string]any{"file": pot, "verbose": true}})
	run("render", map[string]any{"file": pot}, "", &callSpec{"render", map[string]any{"file": pot, "verbose": true}})
	run("measure", map[string]any{"file": pot}, "", &callSpec{"measure", map[string]any{"file": pot, "verbose": true}})
	run("preview", map[string]any{"file": pot}, "iso 384 px", &callSpec{"preview", map[string]any{"file": pot, "size": 640}})
	run("set_parameters", map[string]any{"file": pot, "values": map[string]any{"altura": 95}}, "altura 85→95", nil)
	// edit_checkpoint / update_checkpoint: se mide el resultado; la petición, aparte.
	editArgs := map[string]any{"file": pot, "edits": []any{map[string]any{"old": "altura = 95;", "new": "altura = 90;"}}}
	run("edit_checkpoint", editArgs, "1 edición", nil)
	newSrc := strings.Replace(mustTextOf(t, tb, pot), "altura = 90;", "altura = 85;", 1)
	run("update_checkpoint", map[string]any{"file": pot, "source": newSrc}, "archivo completo", &callSpec{"update_checkpoint", map[string]any{"file": pot, "source": strings.Replace(newSrc, "altura = 85;", "altura = 86;", 1), "verbose": true}})
	run("extract_module", map[string]any{"file": pot, "modules": []any{"plato"}, "lib": "plato"}, "plato", nil)
	run("create_checkpoint", map[string]any{"label": "nuevo", "source": srcCube, "parent": spare}, "", nil)
	run("format", map[string]any{"source": srcCube}, "", nil)
	run("export", map[string]any{"file": mini, "format": "stl"}, "stl", nil)
	run("list_templates", nil, "", nil)
	run("get_template", map[string]any{"name": "knob"}, "", nil)
	run("create_from_template", map[string]any{"name": "knob", "parent": spare}, "", nil)
	run("set_active_parent", map[string]any{"file": mini}, "", nil)
	run("rename_checkpoint", map[string]any{"file": mini, "label": "Un cubo"}, "", nil)
	trashed := run("trash_checkpoint", map[string]any{"file": spare}, "", nil)
	_ = trashed
	lt := run("list_trash", nil, "", nil)
	trashName := ""
	if i := strings.Index(lt.Text, "-"+spare); i > 0 {
		trashName = lt.Text[strings.LastIndex(lt.Text[:i], "\"")+1 : i+1+len(spare)]
	}
	run("restore_checkpoint", map[string]any{"trashed_name": trashName}, "", nil)
	run("list_refs", nil, "", nil)
	run("list_libraries", nil, "", nil)
	run("install_library", map[string]any{"name": "no-existe"}, "nombre inválido (sin red)", nil)
	run("import_mesh", map[string]any{"file": "refs/soporte.stl"}, "", nil)
	run("edit_mesh", map[string]any{"file": mini, "operations": []any{map[string]any{"op": "center"}}}, "", nil)
	run("estimate_print", map[string]any{"file": pot}, "", nil)
	run("list_materials", nil, "", nil)
	run("suggest_orientation", map[string]any{"file": pot}, "", nil)
	run("list_printers", nil, "", &callSpec{"list_printers", map[string]any{"verbose": true}})
	run("set_workspace_printer", with(map[string]any{"printer_id": "bambu-a1-mini"}, nil), "", nil)
	run("list_skills", nil, "", nil)
	run("read_skill", map[string]any{"name": "openscad-core"}, "", nil)
	run("inspect_reference", map[string]any{"file": "refs/soporte.stl"}, "", nil)
	run("image_info", map[string]any{"image": "refs/images/logo.png"}, "", nil)
	run("compare_with_image", map[string]any{"image": "refs/images/logo.png", "file": mini}, "", nil)
	run("trace_image_outline", map[string]any{"image": "refs/images/logo.png", "width_mm": 40}, "", nil)
	run("image_to_heightmap", map[string]any{"image": "refs/images/logo.png", "width_mm": 40}, "", nil)

	// Cada herramienta del catálogo se midió.
	for _, spec := range tb.Tools() {
		if !seen[spec.Name] {
			t.Errorf("la tabla no mide la herramienta %s", spec.Name)
		}
	}

	// La tabla.
	var b strings.Builder
	fmt.Fprintf(&b, "\nresultado de cada herramienta sobre la maceta de prueba (%d líneas, %d bytes)\n", strings.Count(disk, "\n"), len(disk))
	fmt.Fprintf(&b, "%-22s %-28s %9s %9s %7s   %s\n", "herramienta", "llamada", "antes(B)", "ahora(B)", "ahorro", "imagen b64 antes→ahora")
	sorted := append([]budgetRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return false })
	for _, r := range sorted {
		saving := ""
		if r.before > 0 && r.before != r.after {
			saving = fmt.Sprintf("%.0f%%", 100*(1-float64(r.after)/float64(r.before)))
		}
		img := ""
		if r.imgBefore > 0 || r.imgAfter > 0 {
			img = fmt.Sprintf("%d → %d", r.imgBefore, r.imgAfter)
		}
		mark := ""
		if r.isErr {
			mark = " (error)"
		}
		fmt.Fprintf(&b, "%-22s %-28s %9d %9d %7s   %s%s\n", r.tool, clipRunes(r.call, 28), r.before, r.after, saving, img, mark)
	}
	// Lo que el MODELO escribe para el mismo cambio («hazla 10 mm más alta»): la
	// petición pesa tanto como el resultado, y es lo que tarda en generar.
	reqBytes := func(args map[string]any) int { j, _ := json.Marshal(args); return len(j) }
	fmt.Fprintf(&b, "\npetición del modelo para el mismo cambio: update_checkpoint %d B (el archivo entero) · edit_checkpoint %d B · set_parameters %d B\n",
		reqBytes(map[string]any{"file": pot, "source": newSrc}), reqBytes(editArgs),
		reqBytes(map[string]any{"file": pot, "values": map[string]any{"altura": 95}}))
	t.Log(b.String())

	// El tope duro.
	for _, r := range rows {
		if _, ok := budgetExempt[r.tool]; ok {
			continue
		}
		if r.after > toolResultCap {
			t.Errorf("%s (%s): %d bytes pasan el tope de %d", r.tool, r.call, r.after, toolResultCap)
		}
	}
	// Las exentas tienen su tope propio.
	for _, r := range rows {
		if r.tool == "read_checkpoint" && r.after > readMaxBytes+500 {
			t.Errorf("read_checkpoint (%s): %d bytes pasan su tope de %d", r.call, r.after, readMaxBytes)
		}
	}
}

func mustTextOf(t *testing.T, tb *Toolbox, file string) string {
	t.Helper()
	return tb.must(t, "read_checkpoint", map[string]any{"file": file, "full": true}).Text
}
