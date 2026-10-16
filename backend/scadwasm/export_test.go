package scadwasm

import "context"

// UnimplementedImports expone, para los tests externos, las importaciones del
// wasm que el host no implementa.
func (r *Runner) UnimplementedImports(ctx context.Context) ([]string, error) {
	c, err := r.get(ctx)
	if err != nil {
		return nil, err
	}
	return unimplementedImports(c.mod), nil
}

// RewritePaths expone la reescritura de la salida.
var RewritePaths = rewritePaths
