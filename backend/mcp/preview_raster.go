package mcp

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/parts"
	"scadlab/backend/raster"
	"scadlab/backend/render"
)

// defaultPartColor es el amarillo por defecto de OpenSCAD (#F9D72C): el de lo
// que no pasó por color().
var defaultPartColor = [3]uint8{0xF9, 0xD7, 0x2C}

// previewRaster es `preview` cuando no hay un OpenSCAD instalado. El PNG de
// OpenSCAD necesita OpenGL fuera de pantalla, que el motor integrado
// (WebAssembly) no tiene: se renderiza la malla con el motor integrado —con la
// misma caché que `render`— y se dibuja con backend/raster (z-buffer por
// software, sombreado plano), una parte por color, con las mismas vistas y el
// mismo tamaño que el preview nativo.
func (e *env) previewRaster(ctx context.Context, name, viewName string, v view, size int) (*toolResult, error) {
	res, s, err := e.render(ctx, name)
	if err != nil {
		return nil, err
	}
	switch res.Status {
	case render.StatusOK:
	case render.StatusCancelled:
		return nil, i18n.New(i18n.Msg{ES: "el preview se canceló", EN: "the preview was cancelled"})
	default:
		return e.previewFailure(name, res.Log, nil)
	}

	var rparts []raster.Part
	info, ierr := mesh.InfoFromFile(res.MeshPath)
	if ierr == nil {
		if list, perr := e.modelParts(ctx, s, name, res.MeshPath, info); perr == nil && len(list) > 0 {
			rparts = e.rasterParts(res.MeshPath, list)
		}
	}
	if len(rparts) == 0 {
		// Sin partes (no se pudo repartir por color): la malla entera, sin color.
		tris, err := raster.ReadSTL(res.MeshPath)
		if err != nil {
			return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la malla renderizada: %w", EN: "could not read the rendered mesh: %w"}, err)
		}
		rparts = []raster.Part{{Tris: tris, Color: defaultPartColor}}
	}

	img := raster.Render(rparts, raster.View{
		RotX: float64(v.rot[0]), RotY: float64(v.rot[1]), RotZ: float64(v.rot[2]), Ortho: v.ortho,
	}, size)
	png, err := raster.EncodePNG(img)
	if err != nil {
		return nil, err
	}
	return &toolResult{Content: []map[string]any{
		imageContent(png, "image/png"),
		textContent(msgf(i18n.Msg{
			ES: "Preview de %s, vista %s, %d×%d px. Dibujado por ScadLab con el motor integrado (OpenSCAD %s en WebAssembly, sin OpenGL): sombreado plano y el color de cada parte.",
			EN: "Preview of %s, %s view, %d×%d px. Drawn by ScadLab with the built-in engine (OpenSCAD %s on WebAssembly, no OpenGL): flat shading and the color of each part.",
		}, name, viewName, size, size, res.Version)),
	}}, nil
}

// rasterParts lee los STL de cada parte desde la caché de partes (la misma que
// usa la app). Las que no se puedan leer se omiten.
func (e *env) rasterParts(meshPath string, list []parts.Part) []raster.Part {
	key, err := parts.KeyFromMeshPath(meshPath)
	if err != nil || e.parts == nil {
		return nil
	}
	var out []raster.Part
	for _, p := range list {
		tris, err := raster.ReadSTL(filepath.Join(e.parts.Dir(), key+parts.DirSuffix, parts.FileName(p.Index)))
		if err != nil {
			e.log.Printf("preview: parte %d: %v", p.Index, err)
			continue
		}
		out = append(out, raster.Part{Tris: tris, Color: parseHexColor(p.Color)})
	}
	return out
}

// parseHexColor lee «#RRGGBB»; algo ilegible es el color por defecto.
func parseHexColor(s string) [3]uint8 {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) < 6 {
		return defaultPartColor
	}
	v, err := strconv.ParseUint(s[:6], 16, 32)
	if err != nil {
		return defaultPartColor
	}
	return [3]uint8{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}
