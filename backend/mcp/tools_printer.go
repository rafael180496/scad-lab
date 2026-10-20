package mcp

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/skills"
	"scadlab/backend/slicers"
	"scadlab/backend/store"
	"scadlab/backend/wsconfig"
)

// Impresoras (S14): el catálogo de todas las marcas y el cambio de impresora de un
// workspace. printer_info vive en tools.go; la fuente de la impresora efectiva es
// wsconfig, la misma que usa la interfaz.

func (e *env) printerTools() []Tool {
	return []Tool{
		{
			Name: "list_printers",
			Description: "List the printers in ScadLab's catalog (Bambu Lab, Prusa, Creality, Elegoo, Anycubic, Snapmaker, UltiMaker): id, brand, model, build volume in mm (X×Y×Z), maximum colors, multicolor system, recommended slicer and filament diameter, " +
				"and which one is this workspace's (`current`). Filter by `brand` (id or name, e.g. prusa) and/or `query` (text in the id, name or model). The `id` is what set_workspace_printer takes; printer_info gives the full detail of the current one.",
			InputSchema: obj(map[string]any{
				"brand": strProp("Brand id or name (bambu, prusa, creality, elegoo, anycubic, snapmaker, ultimaker). Omit for all."),
				"query": strProp("Case-insensitive text to look for in the printer id, name, model or brand (e.g. mk4, kobra, centauri)."),
			}),
			Annotations: annotations(annReadOnly),
			handler:     e.listPrinters,
		},
		{
			Name: "set_workspace_printer",
			Description: "Change the printer (and optionally the number of colors) of THIS workspace, as the user's «Workspace settings» does. It rewrites CLAUDE.md / AGENTS.md and printer.json with the new printer and, by default, swaps the brand skill (the old brand's skill leaves and the new brand's enters; a copy the user edited by hand is kept). " +
				"Only do it when the user asks to change printer; read the id with list_printers first. Returns the new printer, colors and skills. Existing checkpoints are not touched: re-run measure afterwards, because the build volume may be different.",
			InputSchema: obj(map[string]any{
				"printer_id":       strProp("Printer id from list_printers (e.g. prusa-mk4s)."),
				"colors":           map[string]any{"type": "integer", "description": "Colors this workspace uses, 1 up to the printer's maximum. Omit to keep the current number when the printer does not change, or to start with the new printer's default when it does.", "minimum": 1},
				"swap_brand_skill": map[string]any{"type": "boolean", "description": "When the brand changes, swap the brand skill (old out, new in). Default true; false keeps the skills as they are."},
			}, "printer_id"),
			Annotations: annotations(annWrite),
			handler:     e.setWorkspacePrinter,
		},
	}
}

// slicerOut es el slicer recomendado de una impresora: el primero de su lista que
// existe en el registro.
func slicerOut(p printers.Printer) map[string]any {
	d, ok := slicers.Recommended(p.Slicers)
	if !ok {
		return nil
	}
	return map[string]any{"id": d.ID, "name": d.Name}
}

// slicersOut son todos los slicers posibles de la impresora, en su orden (el
// primero es el recomendado).
func slicersOut(p printers.Printer) []map[string]any {
	out := []map[string]any{}
	for _, id := range p.Slicers {
		if d, ok := slicers.ByID(id); ok {
			out = append(out, map[string]any{"id": d.ID, "name": d.Name})
		}
	}
	return out
}

// matchBrand resuelve el argumento `brand` (id o nombre, sin importar mayúsculas ni
// «Research»/«Lab» de más) al id de una marca. "" = ninguna.
func matchBrand(in string) (string, bool) {
	in = strings.ToLower(strings.TrimSpace(in))
	brands, _ := printers.Brands()
	for _, b := range brands {
		if in == b.ID || in == strings.ToLower(b.Name) || in == strings.ToLower(b.Skill) {
			return b.ID, true
		}
	}
	for _, b := range brands { // «prusa research», «bambu lab», «ultimaker»…
		if in != "" && (strings.HasPrefix(strings.ToLower(b.Name), in) || strings.HasPrefix(in, b.ID)) {
			return b.ID, true
		}
	}
	return "", false
}

func (e *env) listPrinters(_ context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	current := e.printer(s)
	all, err := printers.All()
	if err != nil {
		return nil, err
	}
	brand := ""
	if raw := strArg(args, "brand"); strings.TrimSpace(raw) != "" {
		id, ok := matchBrand(raw)
		if !ok {
			ids := []string{}
			bs, _ := printers.Brands()
			for _, b := range bs {
				ids = append(ids, b.ID)
			}
			return nil, i18n.Errorf(i18n.Msg{ES: "marca desconocida: %q (disponibles: %s)", EN: "unknown brand: %q (available: %s)"}, raw, strings.Join(ids, ", "))
		}
		brand = id
	}
	query := strings.ToLower(strings.TrimSpace(strArg(args, "query")))
	type row struct {
		ID                 string         `json:"id"`
		Name               string         `json:"name"`
		Brand              string         `json:"brand"`
		BrandID            string         `json:"brandId"`
		Model              string         `json:"model"`
		BuildVolumeMM      vec3           `json:"buildVolumeMm"`
		MaxColors          int            `json:"maxColors"`
		DefaultColors      int            `json:"defaultColors"`
		MulticolorKind     string         `json:"multicolorKind"`
		MulticolorSystem   string         `json:"multicolorSystem"`
		Slicer             map[string]any `json:"slicer,omitempty"`
		FilamentDiameterMM float64        `json:"filamentDiameterMm"`
		BrandSkill         string         `json:"brandSkill"`
		Current            bool           `json:"current,omitempty"`
		Unconfirmed        []string       `json:"unconfirmedFields,omitempty"`
	}
	out := []row{}
	for _, p := range all {
		if brand != "" && p.BrandID != brand {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(p.ID+" "+p.Name+" "+p.Model+" "+p.BrandName), query) {
			continue
		}
		out = append(out, row{
			ID: p.ID, Name: p.Name, Brand: p.BrandName, BrandID: p.BrandID, Model: p.Model,
			BuildVolumeMM: vec3{p.BuildVolume.X, p.BuildVolume.Y, p.BuildVolume.Z},
			MaxColors:     p.MaxColors, DefaultColors: p.DefaultColors,
			MulticolorKind: p.MulticolorKind, MulticolorSystem: p.MulticolorSys,
			Slicer: slicerOut(p), FilamentDiameterMM: p.FilamentDiameterMM, BrandSkill: p.Skill,
			Current: p.ID == current.ID, Unconfirmed: p.Verify,
		})
	}
	res := map[string]any{"count": len(out), "printers": out, "currentPrinter": current.ID}
	if len(out) == 0 {
		res["note"] = msgf(i18n.Msg{
			ES: "Ninguna impresora coincide. Prueba sin `query` o con otra marca.",
			EN: "No printer matches. Try without `query` or with another brand.",
		})
	}
	return jsonResult(res)
}

// userSkillsDir es la carpeta de las skills propias: settings.skillsDir si es una
// ruta absoluta y, si no, <datos>/skills (la misma cuenta de app_skills.go).
func (e *env) userSkillsDir(s store.Settings) string {
	if d := strings.TrimSpace(s.SkillsDir); d != "" && filepath.IsAbs(d) {
		return filepath.Clean(d)
	}
	return skills.UserDir(e.dataDir)
}

// skillsAfterSwap calcula la lista explícita de skills del workspace tras cambiar de
// impresora. Parte de las efectivas de ahora; con swap y marca distinta, saca la
// skill de la marca anterior y mete la de la nueva.
func skillsAfterSwap(current []string, oldSkill, newSkill string, swap bool) []string {
	set := map[string]bool{}
	for _, n := range current {
		set[n] = true
	}
	if swap && oldSkill != newSkill {
		delete(set, oldSkill)
		if newSkill != "" {
			set[newSkill] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (e *env) setWorkspacePrinter(_ context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	w, ok := e.storedWorkspace()
	if !ok || e.store == nil {
		return nil, i18n.New(i18n.Msg{
			ES: "este workspace no está registrado en ScadLab, así que no se puede cambiar su impresora desde aquí: el usuario tiene que abrirlo en la app (Configuración del workspace)",
			EN: "this workspace is not registered in ScadLab, so its printer cannot be changed from here: the user has to open it in the app (Workspace settings)",
		})
	}
	id := strings.TrimSpace(strArg(args, "printer_id"))
	if id == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `printer_id`: el id de list_printers", EN: "`printer_id` is required: an id from list_printers"})
	}
	newPrinter, found := printers.ByID(id)
	if !found || newPrinter.ID != id {
		return nil, i18n.Errorf(i18n.Msg{ES: "no existe la impresora %q: usa list_printers para ver los ids", EN: "there is no printer %q: use list_printers to see the ids"}, id)
	}
	old := wsconfig.EffectivePrinter(w)

	pr := w.Print
	changing := id != w.PrinterID
	switch {
	case hasArg(args, "colors"):
		n := intArg(args, "colors", 0)
		if n < 1 {
			return nil, i18n.New(i18n.Msg{ES: "`colors` tiene que ser un entero de 1 en adelante", EN: "`colors` must be an integer of 1 or more"})
		}
		pr.Colors = n
	case changing:
		pr.Colors = 0 // la impresora nueva arranca con sus colores por defecto
	}

	all, err := skills.Discover(e.userSkillsDir(s))
	if err != nil {
		return nil, err
	}
	enabled, err := wsconfig.GlobalEnabled(e.store)
	if err != nil {
		return nil, err
	}
	names := skillsAfterSwap(wsconfig.EffectiveSkills(w, all, enabled, old), old.Skill, newPrinter.Skill, boolArg(args, "swap_brand_skill", true))

	saved, res, err := wsconfig.Apply(e.store, e.userSkillsDir(s), w, id, names, pr)
	if err != nil {
		return nil, err
	}
	eff := wsconfig.EffectivePrinter(saved)
	out := map[string]any{
		"workspace": saved.Name,
		"printer": map[string]any{
			"id": eff.ID, "name": eff.Name, "brand": eff.BrandName, "model": eff.Model,
			"buildVolumeMm":    vec3{eff.BuildVolume.X, eff.BuildVolume.Y, eff.BuildVolume.Z},
			"multicolorSystem": eff.MulticolorSys, "multicolorKind": eff.MulticolorKind,
			"colors": eff.MulticolorSlots, "maxColors": eff.MaxColors,
			"filamentDiameterMm": eff.FilamentDiameterMM, "slicer": slicerOut(eff), "brandSkill": eff.Skill,
		},
		"previousPrinter": map[string]any{"id": old.ID, "name": old.Name},
		"skills":          saved.Skills,
		"removedSkills":   res.Removed,
		"keptSkills":      res.Kept,
		"note": msgf(i18n.Msg{
			ES: "Las reglas del workspace (CLAUDE.md, AGENTS.md y printer.json) ya describen la impresora nueva. Los checkpoints existentes no cambian: vuelve a medirlos (measure) con el volumen nuevo.",
			EN: "The workspace rules (CLAUDE.md, AGENTS.md and printer.json) now describe the new printer. Existing checkpoints are unchanged: measure them again against the new build volume.",
		}),
	}
	if res.SyncError != "" {
		out["syncError"] = res.SyncError
	}
	return jsonResult(out)
}

// hasArg dice si el argumento vino (aunque sea con un valor vacío o cero).
func hasArg(args map[string]any, key string) bool {
	v, ok := args[key]
	return ok && v != nil
}
