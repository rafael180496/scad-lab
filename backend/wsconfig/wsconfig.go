// Package wsconfig resuelve y aplica la configuración PROPIA de un workspace
// (S14): su impresora y los colores que usa, las skills que tiene activas y sus
// ajustes de impresión. Es la única definición de «qué vale en este workspace»:
// la app (bindings, exportación, estimación) y el servidor MCP (printer_info,
// measure) preguntan aquí, así la IA y la interfaz no pueden discrepar.
//
// Lo que el workspace no fija lo hereda, en este orden:
//
//	colores            ajuste del workspace > default_colors de la impresora
//	diámetro filamento ajuste del workspace > el de la impresora > Configuración
//	material, relleno,
//	perímetros, ancho  ajuste del workspace > Configuración → Impresión
//	skills             lista del workspace > (las encendidas en Configuración
//	                   que no son de marca) + la skill de la marca de su impresora
//
// Spec: .claude/specs/s14-impresoras-multimarca.md.
package wsconfig

import (
	"errors"
	"os"
	"sort"

	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/skills"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// EffectivePrinter es la impresora del workspace tal como se usa: la del
// catálogo con MulticolorSlots = los colores que ese workspace eligió (MaxColors
// conserva el tope de la máquina). Un id que ya no existe cae en la A1 mini, como
// printers.ByID.
func EffectivePrinter(w store.Workspace) printers.Printer {
	p, _ := printers.ByID(w.PrinterID)
	p.MulticolorSlots = p.Colors(w.Print.Colors)
	return p
}

// EffectiveSettings devuelve la configuración con los ajustes del workspace
// aplicados encima. p es su impresora (EffectivePrinter): el diámetro del
// filamento es una propiedad de la máquina, así que gana la de la impresora sobre
// la global (una Ultimaker usa 2,85) y el ajuste del workspace sobre las dos. Un
// ajuste del workspace fuera de rango se ignora y rige el heredado: lo que se
// guarda ya pasó por ValidatePrint, pero una base editada a mano no debe romper
// la estimación.
func EffectiveSettings(s store.Settings, w store.Workspace, p printers.Printer) store.Settings {
	pr := w.Print
	if pr.Material != "" {
		s.DefaultMaterial = pr.Material
	}
	if v := pr.InfillPercent; v != nil && inRange(*v, 0, 100) {
		s.InfillPercent = *v
	}
	if v := pr.WallLoops; v != nil && *v >= store.MinWallLoops && *v <= store.MaxWallLoops {
		s.WallLoops = *v
	}
	if v := pr.LineWidthMm; v != nil && inRange(*v, store.MinLineWidthMm, store.MaxLineWidthMm) {
		s.LineWidthMm = *v
	}
	switch v := pr.FilamentDiameterMm; {
	case v != nil && inRange(*v, store.MinFilamentDiameter, store.MaxFilamentDiameter):
		s.FilamentDiameterMm = *v
	case inRange(p.FilamentDiameterMM, store.MinFilamentDiameter, store.MaxFilamentDiameter):
		s.FilamentDiameterMm = p.FilamentDiameterMM
	}
	return s
}

// inRange también rechaza NaN.
func inRange(v, lo, hi float64) bool { return v >= lo && v <= hi }

// ValidatePrint comprueba los ajustes de impresión de un workspace contra su
// impresora. Los rangos son los de Settings.normalizePrint, pero acá un valor
// fuera de rango es un error claro y no un valor de fábrica en silencio: quien lo
// pide (la interfaz o la IA) tiene que enterarse de que no se aplicó.
func ValidatePrint(pr printers.Printer, p store.WorkspacePrint) error {
	top := max(pr.MaxColors, 1)
	if p.Colors < 0 || p.Colors > top {
		return i18n.Errorf(i18n.Msg{
			ES: "los colores tienen que estar entre 1 y %d (el máximo de %s)",
			EN: "colors must be between 1 and %d (the maximum for %s)",
		}, top, pr.Name)
	}
	if v := p.InfillPercent; v != nil && !inRange(*v, 0, 100) {
		return i18n.New(i18n.Msg{ES: "el relleno tiene que estar entre 0 y 100 %", EN: "infill must be between 0 and 100%"})
	}
	if v := p.WallLoops; v != nil && (*v < store.MinWallLoops || *v > store.MaxWallLoops) {
		return i18n.Errorf(i18n.Msg{ES: "los perímetros tienen que estar entre %d y %d", EN: "wall loops must be between %d and %d"}, store.MinWallLoops, store.MaxWallLoops)
	}
	if v := p.LineWidthMm; v != nil && !inRange(*v, store.MinLineWidthMm, store.MaxLineWidthMm) {
		return i18n.Errorf(i18n.Msg{ES: "el ancho de línea tiene que estar entre %.1f y %.1f mm", EN: "line width must be between %.1f and %.1f mm"}, store.MinLineWidthMm, store.MaxLineWidthMm)
	}
	if v := p.FilamentDiameterMm; v != nil && !inRange(*v, store.MinFilamentDiameter, store.MaxFilamentDiameter) {
		return i18n.Errorf(i18n.Msg{ES: "el diámetro del filamento tiene que estar entre %.1f y %.1f mm", EN: "filament diameter must be between %.1f and %.1f mm"}, store.MinFilamentDiameter, store.MaxFilamentDiameter)
	}
	return nil
}

// DefaultSkills son las skills con que nace un workspace de la impresora p: las
// encendidas en Configuración → Skills que NO son de marca, más la skill de la
// marca de p. Las skills de marca no se heredan del global porque se eligen por
// la impresora: un workspace Prusa no lleva la guía de Bambu aunque esté
// encendida. Solo cuentan las que existen en all. Orden alfabético.
func DefaultSkills(all []skills.Skill, globalEnabled map[string]bool, p printers.Printer) []string {
	brand := printers.BrandSkills()
	out := []string{}
	for _, s := range all {
		switch {
		case s.Name == p.Skill:
			out = append(out, s.Name)
		case globalEnabled[s.Name] && !brand[s.Name]:
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}

// EffectiveSkills son las skills que debe tener instaladas el workspace: su
// lista, si la tiene, o las de DefaultSkills. De su lista se descartan las que ya
// no existen (una skill propia borrada) y los repetidos.
func EffectiveSkills(w store.Workspace, all []skills.Skill, globalEnabled map[string]bool, p printers.Printer) []string {
	if w.Skills == nil {
		return DefaultSkills(all, globalEnabled, p)
	}
	exists := make(map[string]bool, len(all))
	for _, s := range all {
		exists[s.Name] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, n := range w.Skills {
		if exists[n] && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// GlobalEnabled lee de la base qué skills están encendidas en Configuración.
func GlobalEnabled(st *store.Store) (map[string]bool, error) {
	rows, err := st.ListSkills()
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.Name] = r.Enabled
	}
	return m, nil
}

// Install sincroniza las skills de UN workspace con las que le corresponden: copia
// las efectivas y retira las demás que ScadLab instaló y nadie editó (las editadas
// quedan y van en Kept). names acota qué skills se miran (vacío = todas): ver una
// sola no pisa las copias de las demás. Una carpeta de workspace que ya no existe
// es un error: Sync la crearía de la nada.
func Install(st *store.Store, userSkillsDir string, w store.Workspace, names ...string) (skills.SyncResult, error) {
	if fi, err := os.Stat(w.Path); err != nil || !fi.IsDir() {
		return skills.SyncResult{}, i18n.Errorf(i18n.Msg{ES: "la carpeta del workspace no existe: %s", EN: "the workspace folder does not exist: %s"}, w.Path)
	}
	all, err := skills.Discover(userSkillsDir)
	if err != nil {
		return skills.SyncResult{}, err
	}
	enabled, err := GlobalEnabled(st)
	if err != nil {
		return skills.SyncResult{}, err
	}
	want := map[string]bool{}
	for _, n := range EffectiveSkills(w, all, enabled, EffectivePrinter(w)) {
		want[n] = true
	}
	list := all
	if len(names) > 0 {
		only := map[string]bool{}
		for _, n := range names {
			only[n] = true
		}
		list = list[:0:0]
		for _, s := range all {
			if only[s.Name] {
				list = append(list, s)
			}
		}
	}
	return skills.Sync(w.Path, list, func(n string) bool { return want[n] })
}

// ResolveSkills valida la lista de skills que pide quien configura un workspace
// y la deja lista para guardar: ordenada y sin repetidos. nil pide las de
// DefaultSkills para esa impresora (se guardan como lista explícita); una lista
// vacía apaga todas. Una skill que no existe es un error con su nombre.
func ResolveSkills(all []skills.Skill, globalEnabled map[string]bool, pr printers.Printer, skillNames []string) ([]string, error) {
	if skillNames == nil {
		return DefaultSkills(all, globalEnabled, pr), nil
	}
	exists := make(map[string]bool, len(all))
	for _, s := range all {
		exists[s.Name] = true
	}
	seen := map[string]bool{}
	names := []string{}
	for _, n := range skillNames {
		if !exists[n] {
			return nil, i18n.Errorf(i18n.Msg{ES: "no existe la skill %q", EN: "there is no skill %q"}, n)
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// Validate comprueba los ajustes de impresión (ValidatePrint) y que el material,
// si se fija uno, exista en la base.
func Validate(st *store.Store, pr printers.Printer, p store.WorkspacePrint) error {
	if err := ValidatePrint(pr, p); err != nil {
		return err
	}
	if p.Material == "" {
		return nil
	}
	mats, err := st.ListMaterials()
	if err != nil {
		return err
	}
	for _, m := range mats {
		if m.ID == p.Material {
			return nil
		}
	}
	return i18n.Errorf(i18n.Msg{ES: "no existe el material %q", EN: "there is no material %q"}, p.Material)
}

// ApplyResult es lo que pasó al aplicar la configuración de un workspace.
type ApplyResult struct {
	Removed   []string `json:"removed"`   // skills retiradas del workspace
	Kept      []string `json:"kept"`      // skills que se dejaron porque alguien editó su copia
	SyncError string   `json:"syncError"` // reglas o skills que no se pudieron escribir; la configuración sí quedó guardada
}

// Apply valida y guarda la configuración de un workspace, reescribe sus reglas
// (CLAUDE.md, AGENTS.md y printer.json) con la impresora efectiva y sincroniza
// SUS skills.
//
//   - printerID tiene que estar en el catálogo (nada de caer en la A1 mini en
//     silencio: elegir una impresora que no existe es un error).
//   - skillNames son skills que existen; nil pide las de DefaultSkills para esa
//     impresora, y se guarda como lista explícita. Una lista vacía apaga todas.
//   - p se valida con ValidatePrint (colores 1..máximo de la impresora, rangos de
//     impresión) y el material tiene que existir.
//
// Si la validación falla no se toca nada. Una vez guardada, un fallo al escribir
// las reglas o al copiar las skills NO deshace la configuración: vuelve en
// ApplyResult.SyncError y el workspace devuelto ya trae lo guardado. Una skill que
// sale de la lista se retira solo si su copia no está editada (skills.Sync).
func Apply(st *store.Store, userSkillsDir string, w store.Workspace, printerID string, skillNames []string, p store.WorkspacePrint) (store.Workspace, ApplyResult, error) {
	res := ApplyResult{Removed: []string{}, Kept: []string{}}
	pr, ok := printers.ByID(printerID)
	if !ok {
		return w, res, i18n.Errorf(i18n.Msg{ES: "no existe la impresora %q", EN: "there is no printer %q"}, printerID)
	}
	all, err := skills.Discover(userSkillsDir)
	if err != nil {
		return w, res, err
	}
	enabled, err := GlobalEnabled(st)
	if err != nil {
		return w, res, err
	}
	names, err := ResolveSkills(all, enabled, pr, skillNames)
	if err != nil {
		return w, res, err
	}
	if err := Validate(st, pr, p); err != nil {
		return w, res, err
	}
	if err := st.SetWorkspaceConfig(w.ID, printerID, names, p); err != nil {
		return w, res, err
	}
	saved, err := st.GetWorkspace(w.ID)
	if err != nil {
		return w, res, err
	}

	var errs []error
	if fi, err := os.Stat(saved.Path); err == nil && fi.IsDir() {
		if err := workspace.WriteRules(saved.Path, saved.Name, EffectivePrinter(saved)); err != nil {
			errs = append(errs, err)
		}
		sync, err := Install(st, userSkillsDir, saved)
		if err != nil {
			errs = append(errs, err)
		}
		res.Removed, res.Kept = nonNil(sync.Removed), nonNil(sync.Kept)
	} else {
		errs = append(errs, i18n.Errorf(i18n.Msg{ES: "la carpeta del workspace no existe: %s", EN: "the workspace folder does not exist: %s"}, saved.Path))
	}
	if err := errors.Join(errs...); err != nil {
		res.SyncError = err.Error()
	}
	return saved, res, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
