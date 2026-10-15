package render

// ParseOutput expone el parser de la salida de OpenSCAD para el linter del
// editor (backend/scad), que corre `openscad -o x.ast` sobre un temporal y
// necesita los mismos diagnósticos que un render. Vive en un archivo aparte
// para no tocar el contrato de types.go.
func ParseOutput(output, workspaceDir, mainFile string) []Diagnostic {
	return parseOutput(output, workspaceDir, mainFile)
}
