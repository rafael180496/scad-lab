package mcp

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"scadlab/backend/estimate"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/raster"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

// Impresión: estimar el material con otros parámetros, ver los materiales y
// sugerir cómo poner la pieza en la placa (o cómo partirla si no cabe).

func (e *env) printTools() []Tool {
	return []Tool{
		{
			Name: "estimate_print",
			Description: "Estimate the weight, filament length and cost of printing a checkpoint, with parameters you choose: material (id from list_materials), infill percent and wall loops. Without them it uses the app settings, like measure. " +
				"Per part (one per color) and in total, with the assumptions listed (no supports, brim or color-change purge; the slicer gives the exact figure). Use it to answer «how much does it weigh in PETG at 30 % infill?».",
			InputSchema: obj(map[string]any{
				"file":           strProp(fileHelp),
				"material":       strProp("Material id from list_materials (e.g. pla, petg). Default: the app's default material."),
				"infill_percent": map[string]any{"type": "number", "description": "Infill 0-100. Default: the app setting.", "minimum": 0, "maximum": 100},
				"wall_loops":     map[string]any{"type": "integer", "description": "Perimeters of the shell, 1-20. Default: the app setting.", "minimum": 1, "maximum": 20},
			}),
			Annotations: annotations(annWrite),
			handler:     e.estimatePrint,
		},
		{
			Name:        "list_materials",
			Description: "List the filament materials the app knows (PLA, PETG, ABS, ASA, TPU, PLA-CF…) with density (g/cm³) and cost per kg, and which one is the default. The `id` is what estimate_print takes.",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.listMaterials,
		},
		{
			Name: "suggest_orientation",
			Description: "Find how to lay a checkpoint on the printer plate. Renders it and tries each of the 6 faces down (and the XY swap for non-square plates): for each, the resulting size, whether it fits the build volume, its height, the area that overhangs more than 45 degrees (a measure of support needed) and the flat contact area on the plate. " +
				"Orientations are ordered: fits first, then least overhang, then lowest height. Each comes with the edit_mesh `operations` that apply it. If nothing fits it gives the scale that would fit and where to cut (a first edit_mesh `cut`). " +
				"It does not judge strength, layer direction or appearance: look at preview and use judgement. Works on the checkpoint's whole mesh.",
			InputSchema: obj(map[string]any{"file": strProp(fileHelp)}),
			Annotations: annotations(annWrite),
			handler:     e.suggestOrientation,
		},
	}
}

// estimateOverrides son los parámetros de estimate_print que cambian los de la
// configuración de la app.
type estimateOverrides struct {
	material string
	infill   *float64
	walls    int
}

type printEstimate struct {
	parts []parts.Part
	est   estimate.Result
	opts  estimate.Options
	mat   store.Material
	warns []string
}

// estimateModel estima un modelo ya renderizado: reparte en partes por color (las
// del visor) y aplica estimate.Estimate con la configuración de la app más los
// cambios de ov. Si las partes no se pueden repartir, estima el modelo entero
// como una sola y avisa: estimar no debe fallar porque el 3MF no salió.
func (e *env) estimateModel(ctx context.Context, s store.Settings, name, meshPath string, info mesh.Info, ov estimateOverrides) (printEstimate, error) {
	var pe printEstimate
	pe.mat = e.material(s)
	partList, perr := e.modelParts(ctx, s, name, meshPath, info)
	if perr != nil {
		pe.warns = append(pe.warns, msgf(i18n.Msg{
			ES: "No se pudieron separar las partes por color (%v): la estimación trata el modelo como una sola parte.",
			EN: "The parts by color could not be separated (%v): the estimate treats the model as a single part.",
		}, perr))
		partList = []parts.Part{{Index: 0, Name: "Default", Color: "#F9D72C", Uncolored: true, Info: info}}
	}
	pe.parts = partList

	opts := estimate.OptionsFromSettings(s)
	mats := e.estimateMaterials()
	if ov.material != "" {
		if !hasMaterial(mats, ov.material) {
			ids := make([]string, 0, len(mats))
			for _, m := range mats {
				ids = append(ids, m.ID)
			}
			return pe, i18n.Errorf(i18n.Msg{ES: "material desconocido: %q (disponibles: %s)", EN: "unknown material: %q (available: %s)"}, ov.material, strings.Join(ids, ", "))
		}
		opts.DefaultMaterialID = ov.material
		for _, m := range mats {
			if m.ID == ov.material {
				pe.mat = store.Material{ID: m.ID, Name: m.Name, DensityGCm3: m.DensityGCm3, CostPerKg: m.CostPerKg}
			}
		}
	}
	if ov.infill != nil {
		opts.InfillPercent = *ov.infill
	}
	if ov.walls > 0 {
		opts.WallLoops = ov.walls
	}
	if !hasMaterial(mats, opts.DefaultMaterialID) {
		// El material por defecto ya no existe (se borró de la base): se estima
		// con el mismo respaldo de siempre (material()) y se avisa.
		mats = append(mats, estimate.Material{ID: pe.mat.ID, Name: pe.mat.Name, DensityGCm3: pe.mat.DensityGCm3})
		pe.warns = append(pe.warns, msgf(i18n.Msg{
			ES: "El material por defecto %q no existe: la estimación usa %s.",
			EN: "The default material %q does not exist: the estimate uses %s.",
		}, opts.DefaultMaterialID, pe.mat.Name))
		opts.DefaultMaterialID = pe.mat.ID
	}
	est, err := estimate.Estimate(estimate.ClosedAsWhole(estimateParts(partList), info.Watertight), mats, opts)
	if err != nil {
		return pe, err
	}
	pe.est, pe.opts = est, opts
	return pe, nil
}

func (e *env) estimatePrint(ctx context.Context, args map[string]any) (*toolResult, error) {
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	var ov estimateOverrides
	ov.material = strings.ToLower(strings.TrimSpace(strArg(args, "material")))
	if _, has := args["infill_percent"]; has {
		v, ok := numArg(args, "infill_percent")
		if !ok || v < 0 || v > 100 {
			return nil, i18n.New(i18n.Msg{ES: "`infill_percent` tiene que ser un número entre 0 y 100", EN: "`infill_percent` must be a number between 0 and 100"})
		}
		ov.infill = &v
	}
	if _, has := args["wall_loops"]; has {
		n := intArg(args, "wall_loops", 0)
		if n < 1 || n > 20 {
			return nil, i18n.New(i18n.Msg{ES: "`wall_loops` tiene que estar entre 1 y 20", EN: "`wall_loops` must be between 1 and 20"})
		}
		ov.walls = n
	}
	res, s, err := e.render(ctx, name)
	if err != nil {
		return nil, err
	}
	if res.Status != render.StatusOK {
		return e.failedRender(name, res)
	}
	info, err := mesh.InfoFromFile(res.MeshPath)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la malla renderizada: %w", EN: "could not read the rendered mesh: %w"}, err)
	}
	pe, err := e.estimateModel(ctx, s, name, res.MeshPath, info, ov)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"file":             name,
		"material":         map[string]any{"id": pe.mat.ID, "name": pe.mat.Name, "densityGCm3": pe.mat.DensityGCm3, "costPerKg": pe.mat.CostPerKg},
		"estimatedWeightG": round(pe.est.Total.MassG, 2),
		"filamentM":        round(pe.est.Total.LengthM, 2),
		"cost":             round(pe.est.Total.Cost, 3),
		"currency":         pe.est.Currency,
		"parts":            partsOut(pe.parts),
		"estimate":         estimateOut(pe.est, pe.opts.Normalized()),
	}
	warns := append(pe.warns, meshWarnings(info)...)
	if len(warns) > 0 {
		out["warnings"] = warns
	}
	return jsonResult(out)
}

// numArg saca un número de un argumento (número o texto numérico).
func numArg(args map[string]any, key string) (float64, bool) {
	switch v := args[key].(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f, !math.IsNaN(f) && !math.IsInf(f, 0)
		}
	}
	return 0, false
}

func (e *env) listMaterials(_ context.Context, _ map[string]any) (*toolResult, error) {
	s := e.settings()
	def := e.material(s)
	type matOut struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		DensityGCm3 float64 `json:"densityGCm3"`
		CostPerKg   float64 `json:"costPerKg"`
		Default     bool    `json:"default"`
	}
	opts := estimate.OptionsFromSettings(s)
	out := []matOut{}
	for _, m := range e.estimateMaterials() {
		out = append(out, matOut{ID: m.ID, Name: m.Name, DensityGCm3: m.DensityGCm3, CostPerKg: m.CostPerKg, Default: m.ID == def.ID})
	}
	return jsonResult(map[string]any{"materials": out, "defaultMaterial": def.ID, "currency": opts.Normalized().Currency})
}

// --- suggest_orientation ------------------------------------------------------------

// orientCandidate es una orientación probada.
type orientCandidate struct {
	Down       string           `json:"down"`
	SizeMM     vec3             `json:"sizeMm"`
	Fits       bool             `json:"fits"`
	NeedsZSpin bool             `json:"needsZRotation,omitempty"`
	HeightMM   float64          `json:"heightMm"`
	OverhangMM float64          `json:"overhangAreaMm2"`
	ContactMM  float64          `json:"contactAreaMm2"`
	Operations []map[string]any `json:"operations"`

	overhang float64
}

// faceOrder es el orden fijo de las seis caras.
var faceOrder = []string{"-z", "+z", "-x", "+x", "-y", "+y"}

// footprint es (ancho, fondo, alto) del cuerpo con la cara `down` hacia la placa.
func footprint(size [3]float64, down string) [3]float64 {
	switch down[1] {
	case 'x':
		return [3]float64{size[2], size[1], size[0]}
	case 'y':
		return [3]float64{size[0], size[2], size[1]}
	}
	return size
}

// rankOrientations prueba las seis caras contra la placa y las ordena: las que
// caben primero, luego la menor área en voladizo (lo que pide soportes) y luego
// la menor altura. overhang/contact salen de la malla (nil = sin datos).
func rankOrientations(size [3]float64, plate printers.Volume, overhang, contact map[string]float64) []orientCandidate {
	out := make([]orientCandidate, 0, len(faceOrder))
	for _, down := range faceOrder {
		fp := footprint(size, down)
		ok, _ := parts.Fit(fp, plate)
		spin := false
		if !ok {
			// En una placa que no es cuadrada, girar 90° en Z puede hacerla caber.
			if ok2, _ := parts.Fit([3]float64{fp[1], fp[0], fp[2]}, plate); ok2 {
				ok, spin = true, true
				fp = [3]float64{fp[1], fp[0], fp[2]}
			}
		}
		ops := []map[string]any{{"op": "orient", "down": down}}
		if spin {
			ops = append(ops, map[string]any{"op": "rotate", "degrees": []float64{0, 0, 90}})
		}
		ops = append(ops, map[string]any{"op": "center"}, map[string]any{"op": "to_bed"})
		out = append(out, orientCandidate{
			Down: down, SizeMM: vecOf(fp), Fits: ok, NeedsZSpin: spin, HeightMM: round(fp[2], 2),
			OverhangMM: round(overhang[down], 1), ContactMM: round(contact[down], 1),
			Operations: ops, overhang: overhang[down],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Fits != b.Fits {
			return a.Fits
		}
		// Menos de 1 mm² de diferencia es empate: ruido de la malla.
		if math.Abs(a.overhang-b.overhang) > 1 {
			return a.overhang < b.overhang
		}
		if a.HeightMM != b.HeightMM {
			return a.HeightMM < b.HeightMM
		}
		return a.ContactMM > b.ContactMM
	})
	return out
}

// faceAreas mide, para cada cara que podría quedar abajo, el área de la malla que
// queda en voladizo (más de 45° de la vertical, sin contar lo apoyado en la
// placa) y el área plana que toca la placa.
func faceAreas(tris []raster.Tri, min, max [3]float64) (overhang, contact map[string]float64) {
	overhang, contact = map[string]float64{}, map[string]float64{}
	const flatTol = 0.2 // mm: lo que está a menos de esto del plano más bajo toca la placa
	const sin45 = 0.70710678
	for _, face := range faceOrder {
		axis := int(face[1] - 'x')
		sign := 1.0
		if face[0] == '-' {
			sign = -1
		}
		// La cara que baja es la del extremo de la caja en la dirección «abajo»
		// (sign*e_axis): ahí está la placa.
		plane := min[axis]
		if sign > 0 {
			plane = max[axis]
		}
		for _, t := range tris {
			var p [3][3]float64
			for k := 0; k < 3; k++ {
				for a := 0; a < 3; a++ {
					p[k][a] = float64(t[k*3+a])
				}
			}
			u := [3]float64{p[1][0] - p[0][0], p[1][1] - p[0][1], p[1][2] - p[0][2]}
			v := [3]float64{p[2][0] - p[0][0], p[2][1] - p[0][1], p[2][2] - p[0][2]}
			n := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
			l := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])
			if l < 1e-12 {
				continue
			}
			area := l / 2
			d := sign * n[axis] / l // cuánto mira hacia abajo (1 = directo)
			if d <= sin45 {
				continue
			}
			// Altura sobre el plano más bajo de sus tres vértices.
			onBed := true
			for k := 0; k < 3; k++ {
				if math.Abs(p[k][axis]-plane) > flatTol {
					onBed = false
				}
			}
			if onBed {
				if d > 0.999 {
					contact[face] += area
				}
				continue
			}
			overhang[face] += area
		}
	}
	return overhang, contact
}

func (e *env) suggestOrientation(ctx context.Context, args map[string]any) (*toolResult, error) {
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	res, s, err := e.render(ctx, name)
	if err != nil {
		return nil, err
	}
	if res.Status != render.StatusOK {
		return e.failedRender(name, res)
	}
	info, err := mesh.InfoFromFile(res.MeshPath)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la malla renderizada: %w", EN: "could not read the rendered mesh: %w"}, err)
	}
	pr := e.printer(s)
	plate := pr.BuildVolume
	var overhang, contact map[string]float64
	if tris, terr := raster.ReadSTL(res.MeshPath); terr == nil {
		overhang, contact = faceAreas(tris, info.Min, info.Max)
	} else {
		e.log.Printf("suggest_orientation: %v", terr)
	}
	cands := rankOrientations(info.Size, plate, overhang, contact)

	out := map[string]any{
		"file":         name,
		"printer":      printerOf(pr),
		"currentSize":  vecOf(info.Size),
		"currentFits":  func() bool { ok, _ := fits(info.Size, plate); return ok }(),
		"orientations": cands,
		"note": msgf(i18n.Msg{
			ES: "«down» es la cara del modelo (tal como está ahora) que baja a la placa. Para aplicarlo: edit_mesh con las `operations` de esa orientación sobre este checkpoint (crea un hijo; en un checkpoint que no es un import, la malla se congela en una sola pieza de un solo color). Voladizo = área que se imprime a más de 45° de la vertical; es una medida de soportes, no un juicio de resistencia.",
			EN: "«down» is the face of the model (as it is now) that goes down onto the plate. To apply it: edit_mesh with that orientation's `operations` on this checkpoint (it creates a child; for a checkpoint that is not an import, the mesh is frozen into a single single-color piece). Overhang = area printed more than 45° from vertical; it measures supports, not strength.",
		}),
	}
	anyFits := false
	for _, c := range cands {
		anyFits = anyFits || c.Fits
	}
	out["fitsInSomeOrientation"] = anyFits
	if anyFits {
		out["recommended"] = cands[0].Down
	} else {
		out["split"] = splitAdvice(info.Size, plate, cands)
	}
	return jsonResult(out)
}

// splitAdvice dice qué hacer cuando ninguna orientación cabe: la escala que sí
// cabría y por dónde cortar. Elige la orientación que menos se pasa.
func splitAdvice(size [3]float64, plate printers.Volume, cands []orientCandidate) map[string]any {
	limits := [3]float64{plate.X, plate.Y, plate.Z}
	bestScale, bestDown := 0.0, ""
	var bestFP [3]float64
	for _, c := range cands {
		fp := [3]float64{c.SizeMM.X, c.SizeMM.Y, c.SizeMM.Z}
		f := math.Inf(1)
		for a := 0; a < 3; a++ {
			if limits[a] > 0 && fp[a] > 0 {
				f = math.Min(f, limits[a]/fp[a])
			}
		}
		if f > bestScale {
			bestScale, bestDown, bestFP = f, c.Down, fp
		}
	}
	// Eje que más se pasa en esa orientación, y en cuántas piezas hay que partirlo.
	axis, pieces, worst := 0, 1, 0.0
	for a := 0; a < 3; a++ {
		if limits[a] > 0 && bestFP[a] > limits[a] {
			if r := bestFP[a] / limits[a]; r > worst {
				axis, worst = a, r
				pieces = int(math.Ceil(r))
			}
		}
	}
	axisName := string(rune('x' + axis))
	adv := map[string]any{
		"bestOrientation":  bestDown,
		"maxUniformScale":  round(bestScale, 3),
		"overflowAxis":     axisName,
		"piecesAlongAxis":  pieces,
		"orientOperations": []map[string]any{{"op": "orient", "down": bestDown}, {"op": "center"}, {"op": "to_bed"}},
	}
	if pieces == 2 {
		adv["firstCutOperation"] = map[string]any{"op": "cut", "axis": axisName, "at_rel": 0.5, "keep": "both", "split": "objects"}
		adv["advice"] = msgf(i18n.Msg{
			ES: "No cabe en ninguna orientación. Opciones: (1) escalarla a ×%.3f como máximo (edit_mesh con la operación scale o resize); (2) partirla por la mitad a lo largo de %s: edit_mesh con las orientOperations y el corte firstCutOperation (cada mitad cabe sola, aunque juntas no entren en una placa: el laminador las reparte); (3) si es un diseño tuyo, rediséñalo en piezas que encajen.",
			EN: "It does not fit in any orientation. Options: (1) scale it to ×%.3f at most (edit_mesh with the scale or resize operation); (2) split it in half along %s: edit_mesh with the orientOperations and the firstCutOperation cut (each half fits alone, although together they do not fit one plate: the slicer spreads them out); (3) if it is your own design, redesign it as parts that join.",
		}, bestScale, strings.ToUpper(axisName))
	} else {
		adv["advice"] = msgf(i18n.Msg{
			ES: "No cabe en ninguna orientación y haría falta partirla en %d piezas a lo largo de %s. Un corte por llamada no alcanza para tantas: escálala a ×%.3f como máximo (edit_mesh con scale o resize), o rediseña el modelo en piezas que encajen.",
			EN: "It does not fit in any orientation and would need %d pieces along %s. One cut per call is not enough for that: scale it to ×%.3f at most (edit_mesh with scale or resize), or redesign the model as parts that join.",
		}, pieces, strings.ToUpper(axisName), bestScale)
	}
	return adv
}
