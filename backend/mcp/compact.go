package mcp

import (
	"context"
	"os"
	"sort"

	"scadlab/backend/estimate"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/parts"
	"scadlab/backend/render"
	"scadlab/backend/scad"
	"scadlab/backend/store"
)

// Respuestas compactas (S17, C). `render`, `lint`, `measure`, `update_checkpoint`,
// `edit_checkpoint` y `set_parameters` devuelven por defecto un resumen corto:
// estado, partes con color y tamaño, caja total, si cabe y hasta 5 errores con su
// línea. El detalle completo (la lista entera de diagnósticos, la estimación con
// sus supuestos…) sale con `verbose: true`. Un modelo de 8k de contexto no puede
// pagar 8 KB por cada render.
//
// Las claves que la interfaz ya lee (status, durationMs, engine, mesh.sizeMm,
// mesh.volumeCm3, mesh.triangles, mesh.watertight, fitsBuildVolume, overflowMm) no
// cambian de nombre.

// maxShownDiag es cuántos diagnósticos trae un resumen.
const maxShownDiag = 5

// diagMsgRunes y diagCodeRunes acotan el mensaje y la línea de código de un
// diagnóstico en un resumen.
const (
	diagMsgRunes  = 220
	diagCodeRunes = 120
)

func verboseArg(args map[string]any) bool { return boolArg(args, "verbose", false) }

// verboseProp es la propiedad `verbose` del esquema de las herramientas compactas.
var verboseProp = map[string]any{"type": "boolean", "description": "Full detail instead of the short summary. Default false."}

// cdiag es un diagnóstico de un resumen.
type cdiag struct {
	Severity string `json:"severity"`
	Line     int    `json:"line,omitempty"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
}

// pickDiags elige hasta n diagnósticos —los errores primero— y dice cuántos
// quedaron fuera. El código de la línea solo se incluye en los errores.
func pickDiags(ds []diagOut, n int) (shown []cdiag, more int) {
	shown = []cdiag{}
	sorted := make([]diagOut, len(ds))
	copy(sorted, ds)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Severity == render.SeverityError && sorted[j].Severity != render.SeverityError
	})
	for i, d := range sorted {
		if i >= n {
			more = len(sorted) - n
			break
		}
		c := cdiag{Severity: d.Severity, Line: d.Line, File: d.File, Message: clipRunes(d.Message, diagMsgRunes)}
		if d.Severity == render.SeverityError {
			c.Code = clipRunes(d.Code, diagCodeRunes)
		}
		shown = append(shown, c)
	}
	return shown, more
}

// cachedParts devuelve las partes por color de un render SI ya están en la caché
// (las generó el visor, un preview o un measure). No las genera: eso cuesta otro
// render con color.
func (e *env) cachedParts(meshPath string) []parts.Part {
	if e.parts == nil || meshPath == "" {
		return nil
	}
	key, err := parts.KeyFromMeshPath(meshPath)
	if err != nil {
		return nil
	}
	m, ok := e.parts.Lookup(key)
	if !ok {
		return nil
	}
	return m.Parts
}

// compactMesh es la malla de un resumen.
func compactMesh(info mesh.Info) ordered {
	return ordered{
		{"sizeMm", vecOf(info.Size)},
		{"minMm", vecOf(info.Min)},
		{"maxMm", vecOf(info.Max)},
		{"volumeCm3", round(info.VolumeMM3/1000, 3)},
		{"triangles", info.Triangles},
		{"watertight", info.Watertight},
	}
}

// compactParts son las partes de color de un resumen: color, tamaño y volumen.
// Con una sola parte no dicen nada que la malla no diga.
func compactParts(ps []parts.Part, grams map[int]float64) []ordered {
	if len(ps) < 2 {
		return nil
	}
	out := make([]ordered, 0, len(ps))
	for _, p := range ps {
		o := ordered{{"color", p.Color}}
		if p.Uncolored {
			o.set("uncolored", true)
		}
		o.set("sizeMm", arr3(p.Info.Size, 2))
		o.set("cm3", round(p.Info.VolumeMM3/1000, 3))
		if g, ok := grams[p.Index]; ok {
			o.set("g", round(g, 2))
		}
		out = append(out, o)
	}
	return out
}

// compactRenderOK es el resumen de un render que salió bien.
func (e *env) compactRenderOK(name string, res render.Result, s store.Settings) (ordered, error) {
	info, err := meshInfoOf(res)
	if err != nil {
		return nil, err
	}
	pr := e.printer(s)
	ok, over := fits(info.Size, pr.BuildVolume)
	out := ordered{{"file", name}, {"status", res.Status}, {"durationMs", res.DurationMs}}
	if res.Engine == render.EngineWASM {
		out.set("engine", render.EngineWASM)
	}
	out.set("mesh", compactMesh(info))
	out.set("fitsBuildVolume", ok)
	if !ok {
		out.set("overflowMm", over)
	}
	if ps := compactParts(e.cachedParts(res.MeshPath), nil); len(ps) > 0 {
		out.set("parts", ps)
	}
	// Avisos de OpenSCAD (un render que salió bien solo trae warnings).
	if ds, more := pickDiags(e.diagnosticsOut(name, res.Diagnostics), maxShownDiag); len(ds) > 0 {
		out.set("diagnostics", ds)
		if more > 0 {
			out.set("moreDiagnostics", more)
		}
	}
	if w := meshWarnings(info); len(w) > 0 {
		out.set("warnings", w)
	}
	return out, nil
}

// meshInfoOf lee la malla de un render.
func meshInfoOf(res render.Result) (mesh.Info, error) {
	info, err := mesh.InfoFromFile(res.MeshPath)
	if err != nil {
		return mesh.Info{}, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la malla renderizada: %w", EN: "could not read the rendered mesh: %w"}, err)
	}
	return info, nil
}

// compactRenderFailure es el resumen de un render que no produjo malla: hasta 5
// errores con su línea y el código, y qué hacer.
func (e *env) compactRenderFailure(name string, res render.Result) ordered {
	out := ordered{{"file", name}, {"status", res.Status}, {"durationMs", res.DurationMs}}
	all := e.diagnosticsOut(name, res.Diagnostics)
	shown, more := pickDiags(all, maxShownDiag)
	out.set("diagnostics", shown)
	if more > 0 {
		out.set("moreDiagnostics", more)
	}
	switch res.Status {
	case render.StatusMissing:
		out.set("message", msgf(i18n.Msg{
			ES: "OpenSCAD no está instalado o no se encontró. Pídele al usuario que lo instale o indique su ruta en ScadLab → Configuración → OpenSCAD.",
			EN: "OpenSCAD is not installed or was not found. Ask the user to install it or set its path in ScadLab → Settings → OpenSCAD.",
		}))
	case render.StatusCancelled:
		out.set("message", msgf(i18n.Msg{
			ES: "El render se canceló (otro render del mismo archivo lo reemplazó, o se canceló la petición).",
			EN: "The render was cancelled (a newer render of the same file replaced it, or the request was cancelled).",
		}))
	default:
		located := false
		for _, d := range res.Diagnostics {
			if d.Severity == render.SeverityError && d.Line > 0 {
				located = true
			}
		}
		if !located && res.Log != "" {
			out.set("logTail", clipRunes(tailLines(res.Log, 8), 800))
		}
		out.set("message", msgf(i18n.Msg{
			ES: "El modelo no renderiza. Corrige las líneas marcadas con edit_checkpoint sobre este archivo (%s); la versión rota queda en su historial.",
			EN: "The model does not render. Fix the marked lines with edit_checkpoint on this file (%s); the broken version stays in its history.",
		}, name))
	}
	return out
}

// renderBlock renderiza name y devuelve el bloque `render` que llevan los
// resultados de las herramientas que escriben: ok primero, después el resumen (o
// el fallo). Un fallo de verificar no es un fallo de escribir: el archivo ya
// cambió.
func (e *env) renderBlock(ctx context.Context, name string, verbose bool) ordered {
	res, s, err := e.render(ctx, name)
	switch {
	case err != nil:
		return ordered{{"ok", false}, {"message", err.Error()}}
	case res.Status != render.StatusOK:
		if verbose {
			fail := e.renderFailure(name, res)
			out := ordered{{"ok", false}}
			for _, k := range sortedKeys(fail) {
				out.set(k, fail[k])
			}
			return out
		}
		out := ordered{{"ok", false}}
		for _, kv := range e.compactRenderFailure(name, res) {
			out.set(kv.K, kv.V)
		}
		return out
	}
	if verbose {
		sum, serr := e.renderSummary(name, res, s)
		if serr != nil {
			return ordered{{"ok", false}, {"message", serr.Error()}}
		}
		out := ordered{{"ok", true}}
		for _, k := range sortedKeys(sum) {
			out.set(k, sum[k])
		}
		return out
	}
	sum, serr := e.compactRenderOK(name, res, s)
	if serr != nil {
		return ordered{{"ok", false}, {"message", serr.Error()}}
	}
	out := ordered{{"ok", true}}
	for _, kv := range sum {
		out.set(kv.K, kv.V)
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// lintBlock corre el linter sobre lo que quedó en disco y devuelve los
// diagnósticos de un resumen (errores y avisos, hasta 5). Para un archivo de lib/
// no corre: las reglas de cabecera no le aplican y un módulo suelto no siempre
// compila solo; se verifica renderizando el checkpoint que lo incluye.
func (e *env) lintBlock(ctx context.Context, ref fileRef, s store.Settings, verbose bool) any {
	if ref.Lib {
		return []any{}
	}
	raw, err := os.ReadFile(ref.Path)
	if err != nil {
		return []any{}
	}
	ds, _, lerr := e.lintSource(ctx, s, string(raw), ref.Key)
	if lerr != nil {
		return []any{}
	}
	if verbose {
		if len(ds) > maxDiagnostics {
			ds = ds[:maxDiagnostics]
		}
		return ds
	}
	return lintShown(ds)
}

// ldiag es un diagnóstico del linter en un resumen: el código de la regla (SL003…
// o el de OpenSCAD) y la línea.
type ldiag struct {
	Severity string `json:"severity"`
	Line     int    `json:"line,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
}

// lintShown es la lista corta de diagnósticos del linter (errores primero).
func lintShown(ds []scad.Diagnostic) []ldiag {
	sorted := make([]scad.Diagnostic, len(ds))
	copy(sorted, ds)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Severity == scad.SeverityError && sorted[j].Severity != scad.SeverityError
	})
	out := []ldiag{}
	for i, d := range sorted {
		if i >= maxShownDiag {
			break
		}
		out = append(out, ldiag{Severity: d.Severity, Line: d.Line, Code: d.Code, Message: clipRunes(d.Message, diagMsgRunes)})
	}
	return out
}

// verifyWritten agrega a out lo que se verifica tras escribir un archivo: el
// linter y el render. Un archivo de lib/ se verifica con el checkpoint más nuevo
// que lo incluye (`checked`); si ninguno lo incluye, lo dice.
func (e *env) verifyWritten(ctx context.Context, out *ordered, ref fileRef, verbose bool) {
	s := e.settings()
	out.set("lint", e.lintBlock(ctx, ref, s, verbose))
	target := ref.Key
	if ref.Lib {
		target = e.dependentCheckpoint(ref.Key)
		if target == "" {
			out.set("render", ordered{{"ok", nil}, {"message", msgf(i18n.Msg{
				ES: "Ningún checkpoint incluye %s todavía: no hay nada que renderizar para verificarlo.",
				EN: "No checkpoint includes %s yet: there is nothing to render to verify it.",
			}, ref.Key)}})
			return
		}
		out.set("checked", target)
	}
	out.set("render", e.renderBlock(ctx, target, verbose))
}

// --- measure ----------------------------------------------------------------

// compactMeasure es el resumen de measure: caja, volumen, si cabe, peso y las
// partes con su color. La estimación detallada y sus supuestos van con verbose.
func compactMeasure(name string, info mesh.Info, fitsOK bool, over map[string]float64, matName string, est estimate.Result, ps []parts.Part, warns []string) ordered {
	grams := map[int]float64{}
	for _, p := range est.Parts {
		grams[p.Index] = p.MassG
	}
	out := ordered{{"file", name}}
	m := compactMesh(info)
	m.set("areaCm2", round(info.AreaMM2/100, 2))
	out.set("mesh", m)
	out.set("fitsBuildVolume", fitsOK)
	if !fitsOK {
		out.set("overflowMm", over)
	}
	out.set("estimatedWeightG", round(est.Total.MassG, 2))
	out.set("material", matName)
	if cp := compactParts(ps, grams); len(cp) > 0 {
		out.set("parts", cp)
	}
	if len(warns) > 0 {
		out.set("warnings", warns)
	}
	return out
}

// lintSummary es la salida corta de lint: los conteos y hasta 5 diagnósticos.
func lintSummary(file string, ds []scad.Diagnostic, counts map[string]int, note string) ordered {
	out := ordered{
		{"file", file},
		{"errors", counts[scad.SeverityError]},
		{"warnings", counts[scad.SeverityWarning]},
		{"infos", counts[scad.SeverityInfo]},
		{"diagnostics", lintShown(ds)},
	}
	if more := len(ds) - maxShownDiag; more > 0 {
		out.set("moreDiagnostics", more)
	}
	if note != "" {
		out.set("note", note)
	}
	return out
}
