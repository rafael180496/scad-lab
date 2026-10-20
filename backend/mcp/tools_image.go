package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/imagetools"
	"scadlab/backend/raster"
	"scadlab/backend/refmodel"
	"scadlab/backend/render"
)

// Herramientas de imagen (S15): modelar a partir de una foto, un logo o un
// boceto. Procesan la imagen con backend/imagetools (Go puro) y la leen SOLO de
// refs/ o refs/images/, por refmodel.ResolveImage. Spec:
// .claude/specs/s15-modelo-desde-imagen.md, método en la skill
// `openscad-from-image`.

const imageHelp = "Image in the workspace's refs/ or refs/images/ (e.g. refs/images/photo-1a2b3c4d.jpg; see list_refs, kind \"image\"). PNG, JPEG, GIF or WebP."

// Lado mayor de las imágenes que devuelven las herramientas, en píxeles.
const (
	imageOutSide      = 800
	tracePreviewSide  = 600
	silhouetteSide    = 320 // lado de las siluetas del modelo que se comparan
	photoForegroundPx = 512 // lado al que se reduce la foto para separar su primer plano
)

func (e *env) imageTools() []Tool {
	numProp := func(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
	flag := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
	return []Tool{
		{
			Name: "image_info",
			Description: "Describe an image the user attached (refs/images/): size in pixels, aspect ratio (width/height), format, dominant colors, the detected background and the bounding box of the foreground object as fractions of the image [x, y, width, height]. " +
				"It also returns a picture: the whole image (reduced) or, with `region`, that part enlarged with its contrast stretched (`enhance`) so you can read small details such as a label, a hole or a dimension. " +
				"Use it first to know what you are modeling and the photo's proportions; ask for the real size of something in it (see the openscad-from-image skill).",
			InputSchema: obj(map[string]any{
				"image":   strProp(imageHelp),
				"region":  map[string]any{"type": "array", "description": "Part to look at closely, [x, y, width, height] as fractions of the image (0 to 1), e.g. [0.25, 0.25, 0.5, 0.5] is the center quarter. It is enlarged up to 4x.", "items": map[string]any{"type": "number"}, "minItems": 4, "maxItems": 4},
				"enhance": flag("Stretch the contrast of the picture. Default true when `region` is given, false otherwise."),
			}, "image"),
			Annotations: annotations(annReadOnly),
			handler:     e.imageInfo,
		},
		{
			Name: "compare_with_image",
			Description: "Render a checkpoint and put it NEXT TO the user's image (or the model's silhouette over it with mode=overlay) so you can compare shape and proportions. Returns one PNG, the `view` used, the width/height proportions of the model in that view and of the photo's foreground, and `silhouetteIoU` (0 to 1: how much the two silhouettes overlap once both are scaled to the same box; above 0.9 is a very close shape). " +
				"With view=auto (default) it picks, among the 8 views, the one whose silhouette matches the photo best, and lists the ranking. Look at it after render/preview and correct the model with update_checkpoint on the same file until proportions and details agree.",
			InputSchema: obj(map[string]any{
				"image":  strProp(imageHelp),
				"file":   strProp(fileHelp),
				"view":   enumProp("Model view. Default auto: the best match with the photo.", append([]string{"auto"}, viewNames...)...),
				"rotate": map[string]any{"type": "array", "description": "Free camera [rx, ry, rz] in degrees instead of `view` (as in preview).", "items": map[string]any{"type": "number"}, "minItems": 3, "maxItems": 3},
				"ortho":  flag("Orthogonal projection for a free `rotate`."),
				"mode":   enumProp("side (default): photo and model side by side; overlay: the model's silhouette semi-transparent over the photo, aligned by bounding box.", imagetools.ModeSide, imagetools.ModeOverlay),
			}, "image"),
			Annotations: annotations(annReadOnly),
			handler:     e.compareWithImage,
		},
		{
			Name: "trace_image_outline",
			Description: "Trace the outline of a flat shape in an image (a logo, a plaque, a silhouette, a template) into an SVG in millimeters saved in refs/, ready for `linear_extrude(h) import(\"refs/x.svg\");`. " +
				"Pipeline: gray levels -> Otsu threshold (or `threshold`) -> the shape is what differs from the image border (`invert` flips it) -> cleanup of specks -> contours WITH holes (even-odd fill) -> simplification -> millimeters, Y up, the shape's lower-left corner at the origin. " +
				"Give the scale with ONE of width_mm / height_mm (size of the traced SHAPE, not of the photo) or mm_per_px (millimeters per pixel of the original file, when the scale comes from a known object in the photo). " +
				"layers=2..6 splits the shape by color (k-means) and writes one SVG per color. Also returns the area, size and contour count, the import snippet and, when small, a polygon(points, paths) to paste. Needs no vision model. Never overwrites a file.",
			InputSchema: obj(map[string]any{
				"image":        strProp(imageHelp),
				"width_mm":     numProp("Width of the traced shape in mm."),
				"height_mm":    numProp("Height of the traced shape in mm (use it instead of width_mm)."),
				"mm_per_px":    numProp("Millimeters per pixel of the original image file."),
				"threshold":    map[string]any{"type": "integer", "description": "Gray level 1-255 that separates light from dark. Default: Otsu (automatic).", "minimum": 1, "maximum": 255},
				"invert":       flag("Trace the opposite: by default the shape is what differs from the border color."),
				"layers":       map[string]any{"type": "integer", "description": "1 (default): one silhouette. 2-6: separate the shape by color, one SVG per color.", "minimum": 1, "maximum": imagetools.MaxLayers},
				"simplify_mm":  numProp("Douglas-Peucker tolerance in mm. Default 0.1."),
				"min_area_mm2": numProp("Specks and holes smaller than this are dropped. Default 0.25."),
				"name":         strProp("File name stem in refs/ (default: the image name + -outline). An existing file is never overwritten: -2, -3 are added."),
			}, "image"),
			Annotations: annotations(annWrite),
			handler:     e.traceImageOutline,
		},
		{
			Name: "image_to_heightmap",
			Description: "Turn an image into a height map for OpenSCAD's `surface()` (refs/x.dat, values 0-100 from the luminance: light = high) and return the snippet that builds the solid: a base of `base_mm` plus a relief of up to `max_height_mm`, `width_mm` wide. " +
				"`invert` makes dark = thick (a lithophane: the light shines through the thin parts). Resolution is capped at 500 columns. Also returns a grayscale picture of the map. Never overwrites a file.",
			InputSchema: obj(map[string]any{
				"image":         strProp(imageHelp),
				"width_mm":      numProp("Width of the piece in mm."),
				"height_mm":     numProp("Height of the piece in mm. Default: from the image's proportions."),
				"max_height_mm": numProp("How much the relief rises above the base (value 100), in mm."),
				"base_mm":       numProp("Thickness where the value is 0, in mm. Default 0.6."),
				"invert":        flag("Lithophane: dark = thick, light = thin."),
				"resolution":    map[string]any{"type": "integer", "description": "Columns of the map, 2-500. Default 200 (the rows follow the proportions).", "minimum": 2, "maximum": imagetools.MaxHeightmapSide},
				"stretch":       flag("Stretch the contrast so the map uses the whole 0-100 range. Default false."),
				"name":          strProp("File name stem in refs/ (default: the image name + -heightmap)."),
			}, "image", "width_mm", "max_height_mm"),
			Annotations: annotations(annWrite),
			handler:     e.imageToHeightmap,
		},
	}
}

// imageCtx acota una operación de imagen a imagetools.OpTimeout.
func imageCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, imagetools.OpTimeout)
}

// imageErr traduce el vencimiento del plazo a un mensaje que el agente entiende.
func imageErr(parent context.Context, err error) error {
	if err != nil && errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil {
		return i18n.Errorf(i18n.Msg{
			ES: "la operación con la imagen superó el plazo de %d s: prueba con una imagen más chica",
			EN: "the image operation exceeded its %d s limit: try a smaller image",
		}, int(imagetools.OpTimeout.Seconds()))
	}
	return err
}

// loadImage resuelve `image` (solo refs/ o refs/images/) y la decodifica con los
// límites del paquete.
func (e *env) loadImage(ctx context.Context, args map[string]any) (*imagetools.Image, string, error) {
	name := strings.TrimSpace(strArg(args, "image"))
	if name == "" {
		return nil, "", i18n.New(i18n.Msg{
			ES: "falta `image`: la imagen de refs/ (p. ej. refs/images/foto.jpg; list_refs las muestra)",
			EN: "`image` is required: an image in refs/ (e.g. refs/images/photo.jpg; list_refs shows them)",
		})
	}
	abs, rel, err := refmodel.ResolveImage(e.ws, name)
	if err != nil {
		return nil, "", err
	}
	im, err := imagetools.Load(ctx, abs)
	if err != nil {
		return nil, "", err
	}
	return im, rel, nil
}

// --- image_info -----------------------------------------------------------------

type imageInfoOut struct {
	Image string `json:"image"`
	imagetools.Info
	Shown imageShown `json:"shown"`
	Notes []string   `json:"notes,omitempty"`
}

type imageShown struct {
	Region   *[4]float64 `json:"region"`
	Enhanced bool        `json:"enhanced"`
	WidthPx  int         `json:"widthPx"`
	HeightPx int         `json:"heightPx"`
}

func (e *env) imageInfo(ctx context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	octx, cancel := imageCtx(ctx)
	defer cancel()
	im, rel, err := e.loadImage(octx, args)
	if err != nil {
		return nil, imageErr(ctx, err)
	}
	info, err := im.Describe(octx)
	if err != nil {
		return nil, imageErr(ctx, err)
	}

	out := imageInfoOut{Image: rel, Info: info}
	view := im
	var region *[4]float64
	if raw, has := args["region"]; has && raw != nil {
		list, ok := listArg(args, "region")
		if !ok || len(list) != 4 {
			return nil, i18n.New(i18n.Msg{
				ES: "`region` lleva cuatro números [x, y, ancho, alto] en fracciones de la imagen (0 a 1)",
				EN: "`region` takes four numbers [x, y, width, height] as fractions of the image (0 to 1)",
			})
		}
		var r [4]float64
		for i, it := range list {
			n, ok := asNum(it)
			if !ok {
				return nil, i18n.New(i18n.Msg{ES: "`region` lleva solo números", EN: "`region` takes numbers only"})
			}
			r[i] = n
		}
		if view, err = im.Crop(r); err != nil {
			return nil, err
		}
		region = &r
	}
	enhance := boolArg(args, "enhance", region != nil)
	if enhance {
		view = view.Enhance()
	}
	if region != nil {
		if view, err = view.FitSide(octx, imageOutSide); err != nil {
			return nil, imageErr(ctx, err)
		}
	} else if view, err = view.Reduced(octx, imageOutSide); err != nil {
		return nil, imageErr(ctx, err)
	}
	png, err := view.PNG()
	if err != nil {
		return nil, err
	}
	out.Shown = imageShown{Region: region, Enhanced: enhance, WidthPx: view.W, HeightPx: view.H}

	if info.HasAlpha {
		out.Notes = append(out.Notes, msgf(i18n.Msg{
			ES: "La imagen tiene fondo transparente: el primer plano es lo opaco. En el dibujo se ve sobre blanco.",
			EN: "The image has a transparent background: the foreground is the opaque part. The picture shows it over white.",
		}))
	} else if !info.Background.Uniform {
		out.Notes = append(out.Notes, msgf(i18n.Msg{
			ES: "El borde de la foto no es de un color uniforme: la caja del primer plano no es fiable. Una pieza sobre un fondo liso se separa mejor.",
			EN: "The photo's border is not one uniform color: the foreground box is unreliable. A part on a plain background separates much better.",
		}))
	}
	if info.Foreground == nil {
		out.Notes = append(out.Notes, msgf(i18n.Msg{
			ES: "No se distingue un objeto del fondo.",
			EN: "No object can be told apart from the background.",
		}))
	}
	if info.WorkWidth != info.WidthPx {
		out.Notes = append(out.Notes, msgf(i18n.Msg{
			ES: "La imagen mide %d×%d px y se trabaja reducida a %d×%d (tope de %d px de lado).",
			EN: "The image is %d×%d px and is processed reduced to %d×%d (cap of %d px per side).",
		}, info.WidthPx, info.HeightPx, info.WorkWidth, info.WorkHeight, imagetools.MaxWork))
	}
	res, err := jsonResult(out)
	if err != nil {
		return nil, err
	}
	res.Content = append([]map[string]any{imageContent(png, "image/png")}, res.Content...)
	return res, nil
}

// --- compare_with_image -----------------------------------------------------------

type namedView struct {
	name string
	v    view
}

type compareOut struct {
	Image         string            `json:"image"`
	File          string            `json:"file"`
	View          string            `json:"view"`
	AutoSelected  bool              `json:"autoSelected"`
	Mode          string            `json:"mode"`
	SilhouetteIoU *float64          `json:"silhouetteIoU"`
	Proportions   compareProp       `json:"proportions"`
	Ranking       []compareRank     `json:"viewRanking,omitempty"`
	Photo         comparePhotoOut   `json:"photo"`
	Advice        string            `json:"advice"`
	Notes         []string          `json:"notes,omitempty"`
	Layout        map[string]string `json:"layout"`
}

type compareProp struct {
	Photo             float64  `json:"photo"` // ancho / alto del primer plano de la foto
	Model             float64  `json:"model"` // ancho / alto del modelo en esa vista
	DifferencePercent *float64 `json:"differencePercent"`
}

type compareRank struct {
	View string  `json:"view"`
	IoU  float64 `json:"iou"`
}

type comparePhotoOut struct {
	WidthPx           int         `json:"widthPx"`
	HeightPx          int         `json:"heightPx"`
	ForegroundBox     *[4]float64 `json:"foregroundBox"`
	BackgroundUniform bool        `json:"backgroundUniform"`
}

func (e *env) compareWithImage(ctx context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	mode := strings.ToLower(strings.TrimSpace(strArg(args, "mode")))
	if mode == "" {
		mode = imagetools.ModeSide
	}
	if mode != imagetools.ModeSide && mode != imagetools.ModeOverlay {
		return nil, i18n.Errorf(i18n.Msg{ES: "modo desconocido: %q (usa side u overlay)", EN: "unknown mode: %q (use side or overlay)"}, mode)
	}
	name, _, err := e.checkpointFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}

	// La foto y su primer plano (a un tamaño chico: solo hace falta la silueta).
	octx, cancel := imageCtx(ctx)
	photo, rel, err := e.loadImage(octx, args)
	if err != nil {
		cancel()
		return nil, imageErr(ctx, err)
	}
	small, err := photo.Reduced(octx, photoForegroundPx)
	var pfg *imagetools.Foreground
	if err == nil {
		pfg, err = small.Foreground(octx)
	}
	cancel()
	if err != nil {
		return nil, imageErr(ctx, err)
	}

	// Qué vista(s) se miran.
	wanted, auto, err := compareViews(args)
	if err != nil {
		return nil, err
	}

	// La malla del modelo (con la caché de render) y su silueta desde cada vista.
	tris, err := e.modelTriangles(ctx, name)
	if err != nil {
		return nil, err
	}
	masks := make(map[string]*imagetools.Mask, len(wanted))
	for _, nv := range wanted {
		img := raster.Render([]raster.Part{{Tris: tris, Color: [3]uint8{0, 0, 0}}}, raster.View{
			RotX: float64(nv.v.rot[0]), RotY: float64(nv.v.rot[1]), RotZ: float64(nv.v.rot[2]), Ortho: nv.v.ortho,
		}, silhouetteSide)
		masks[nv.name] = imagetools.SilhouetteFromBlack(img)
	}
	out := compareOut{Image: rel, File: name, Mode: mode, AutoSelected: auto, Layout: map[string]string{}}
	out.Photo = comparePhotoOut{WidthPx: photo.OrigW, HeightPx: photo.OrigH, BackgroundUniform: pfg.Uniform}
	if pfg.Box.Valid {
		b := round3s(pfg.BoxFractions())
		out.Photo.ForegroundBox = &b
		out.Proportions.Photo = round(pfg.Aspect(), 3)
	}

	// La elegida: la de mayor IoU con la foto (la primera en el orden de las
	// vistas si empatan, para que el resultado sea estable).
	chosen := wanted[0]
	var iou *float64
	if pfg.Box.Valid {
		best := -1.0
		for _, nv := range wanted {
			v := imagetools.SilhouetteIoU(pfg.Mask, masks[nv.name])
			out.Ranking = append(out.Ranking, compareRank{View: nv.name, IoU: round(v, 3)})
			if v > best+1e-9 {
				best, chosen = v, nv
			}
		}
		x := round(best, 3)
		iou = &x
		sort.SliceStable(out.Ranking, func(i, j int) bool { return out.Ranking[i].IoU > out.Ranking[j].IoU })
		if len(out.Ranking) > 3 {
			out.Ranking = out.Ranking[:3]
		}
		if !auto {
			out.Ranking = nil // con una vista pedida el ranking no aporta
		}
	} else {
		out.Notes = append(out.Notes, msgf(i18n.Msg{
			ES: "No se pudo separar un objeto del fondo de la foto, así que no hay IoU ni se pudo elegir la vista: se usó %s. Compara a ojo.",
			EN: "No object could be told apart from the photo's background, so there is no IoU and the view could not be chosen: %s was used. Compare by eye.",
		}, chosen.name))
	}
	out.View, out.SilhouetteIoU = chosen.name, iou
	if mb := masks[chosen.name].BBox(); mb.Valid {
		out.Proportions.Model = round(float64(mb.W())/float64(mb.H()), 3)
	}
	if out.Proportions.Photo > 0 && out.Proportions.Model > 0 {
		d := round((out.Proportions.Model/out.Proportions.Photo-1)*100, 1)
		out.Proportions.DifferencePercent = &d
	}
	if !pfg.Uniform && !photo.HasAlpha {
		out.Notes = append(out.Notes, msgf(i18n.Msg{
			ES: "El borde de la foto no es de un color uniforme: su silueta (y el IoU) pueden ser poco fiables.",
			EN: "The photo's border is not one uniform color: its silhouette (and the IoU) may be unreliable.",
		}))
	}

	// La imagen del modelo desde esa vista, con la misma ruta que `preview`.
	sh, err := e.shoot(ctx, name, chosen.name, chosen.v, defaultPreviewSize)
	if err != nil {
		return nil, err
	}
	octx, cancel = imageCtx(ctx)
	defer cancel()
	model, err := imagetools.Decode(octx, sh.png)
	if err != nil {
		return nil, imageErr(ctx, err)
	}
	composed, err := imagetools.Compose(octx, photo, model, imagetools.ComposeOptions{
		Mode:       mode,
		PhotoLabel: msgf(i18n.Msg{ES: "FOTO", EN: "PHOTO"}),
		ModelLabel: msgf(i18n.Msg{ES: "MODELO", EN: "MODEL"}) + " " + chosen.name,
	})
	if err != nil {
		return nil, imageErr(ctx, err)
	}
	png, err := raster.EncodePNG(composed)
	if err != nil {
		return nil, err
	}
	if mode == imagetools.ModeOverlay {
		out.Layout["image"] = msgf(i18n.Msg{
			ES: "la foto con la silueta del modelo encima (relleno semitransparente y borde magenta), alineadas por caja",
			EN: "the photo with the model's silhouette on top (semi-transparent fill, magenta outline), aligned by box",
		})
	} else {
		out.Layout["left"] = msgf(i18n.Msg{ES: "la foto, recortada alrededor del objeto", EN: "the photo, cropped around the object"})
		out.Layout["right"] = msgf(i18n.Msg{ES: "el modelo, vista %s, recortado igual", EN: "the model, %s view, cropped the same way"}, chosen.name)
	}
	out.Advice = compareAdvice(iou, out.Proportions.DifferencePercent)

	res, err := jsonResult(out)
	if err != nil {
		return nil, err
	}
	res.Content = append([]map[string]any{imageContent(png, "image/png")}, res.Content...)
	return res, nil
}

// compareViews resuelve qué vistas se evalúan: las ocho con `view=auto` (por
// omisión), o la que se pidió (`view` con nombre o `rotate`).
func compareViews(args map[string]any) (list []namedView, auto bool, err error) {
	if raw, has := args["rotate"]; has && raw != nil {
		label, v, err := viewFromArgs(args)
		if err != nil {
			return nil, false, err
		}
		return []namedView{{label, v}}, false, nil
	}
	name := strings.ToLower(strings.TrimSpace(strArg(args, "view")))
	if name == "" || name == "auto" {
		for _, n := range viewNames {
			list = append(list, namedView{n, views[n]})
		}
		return list, true, nil
	}
	label, v, err := viewFromArgs(args)
	if err != nil {
		return nil, false, err
	}
	return []namedView{{label, v}}, false, nil
}

// modelTriangles renderiza el checkpoint (con la caché del motor) y lee su malla.
func (e *env) modelTriangles(ctx context.Context, name string) ([]raster.Tri, error) {
	res, _, err := e.render(ctx, name)
	if err != nil {
		return nil, err
	}
	switch res.Status {
	case render.StatusOK:
	case render.StatusCancelled:
		return nil, i18n.New(i18n.Msg{ES: "la comparación se canceló", EN: "the comparison was cancelled"})
	default:
		_, ferr := e.previewFailure(name, res.Log, nil)
		return nil, ferr
	}
	tris, err := raster.ReadSTL(res.MeshPath)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la malla renderizada: %w", EN: "could not read the rendered mesh: %w"}, err)
	}
	if len(tris) == 0 {
		return nil, i18n.New(i18n.Msg{ES: "el modelo no tiene geometría 3D que comparar", EN: "the model has no 3D geometry to compare"})
	}
	return tris, nil
}

// compareAdvice resume en una frase qué hacer con el resultado.
func compareAdvice(iou *float64, diff *float64) string {
	var parts []string
	switch {
	case iou == nil:
		parts = append(parts, msgf(i18n.Msg{ES: "Sin IoU: compara las dos mitades a ojo.", EN: "No IoU: compare the two halves by eye."}))
	case *iou >= 0.9:
		parts = append(parts, msgf(i18n.Msg{ES: "La silueta coincide casi por completo: revisa ahora los detalles (agujeros, redondeos, relieves).", EN: "The silhouette matches almost entirely: check the details now (holes, fillets, reliefs)."}))
	case *iou >= 0.75:
		parts = append(parts, msgf(i18n.Msg{ES: "La silueta se parece pero no coincide: busca qué parte sobra o falta y corrige con update_checkpoint.", EN: "The silhouette is similar but not equal: find which part is extra or missing and fix it with update_checkpoint."}))
	default:
		parts = append(parts, msgf(i18n.Msg{ES: "La silueta es distinta: revisa la forma base y las proporciones antes de afinar detalles.", EN: "The silhouette is different: review the base shape and the proportions before refining details."}))
	}
	if diff != nil && math.Abs(*diff) >= 8 {
		if *diff > 0 {
			parts = append(parts, msgf(i18n.Msg{ES: "El modelo es un %.0f %% más ancho (o menos alto) que la foto.", EN: "The model is %.0f %% wider (or less tall) than the photo."}, *diff))
		} else {
			parts = append(parts, msgf(i18n.Msg{ES: "El modelo es un %.0f %% más angosto (o más alto) que la foto.", EN: "The model is %.0f %% narrower (or taller) than the photo."}, -*diff))
		}
	}
	return strings.Join(parts, " ")
}

// --- trace_image_outline -------------------------------------------------------------

type traceLayerOut struct {
	Index    int     `json:"index"`
	File     string  `json:"file"`
	Color    string  `json:"color,omitempty"`
	Percent  float64 `json:"percentOfForeground,omitempty"`
	AreaMM2  float64 `json:"areaMm2"`
	Contours int     `json:"contours"`
	Holes    int     `json:"holes"`
	Points   int     `json:"points"`
	Snippet  string  `json:"snippet"`
	Polygon  string  `json:"polygon,omitempty"`
}

type traceOut struct {
	Image     string          `json:"image"`
	Files     []string        `json:"files"`
	Layers    int             `json:"layers"`
	Contours  int             `json:"contours"` // exteriores y huecos, de todas las capas
	Holes     int             `json:"holes"`
	AreaMM2   float64         `json:"areaMm2"` // área neta de todas las capas
	SizeMM    map[string]any  `json:"sizeMm"`
	MMPerPx   float64         `json:"mmPerPx"`
	Threshold *int            `json:"threshold"`
	Shape     string          `json:"shape,omitempty"`
	Inverted  bool            `json:"inverted"`
	Origin    string          `json:"origin"`
	Snippet   string          `json:"snippet"`
	PerLayer  []traceLayerOut `json:"perLayer"`
	Notes     []string        `json:"notes,omitempty"`
	Next      string          `json:"next"`
}

// maxPolygonPoints es el tope de puntos para devolver el `polygon()` en el texto.
const maxPolygonPoints = 120

func (e *env) traceImageOutline(ctx context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	opts := imagetools.TraceOptions{
		Threshold: intArg(args, "threshold", 0),
		Invert:    boolArg(args, "invert", false),
		Layers:    intArg(args, "layers", 1),
	}
	opts.WidthMM, _ = numArg(args, "width_mm")
	opts.HeightMM, _ = numArg(args, "height_mm")
	opts.MMPerPx, _ = numArg(args, "mm_per_px")
	opts.SimplifyMM, _ = numArg(args, "simplify_mm")
	opts.MinAreaMM2, _ = numArg(args, "min_area_mm2")

	octx, cancel := imageCtx(ctx)
	defer cancel()
	im, rel, err := e.loadImage(octx, args)
	if err != nil {
		return nil, imageErr(ctx, err)
	}
	res, err := imagetools.Trace(octx, im, opts)
	if err != nil {
		return nil, imageErr(ctx, err)
	}
	png, err := res.PreviewPNG(tracePreviewSide)
	if err != nil {
		return nil, err
	}

	stem := strings.TrimSpace(strArg(args, "name"))
	if stem == "" {
		stem = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)) + "-outline"
	}
	out := traceOut{
		Image:   rel,
		SizeMM:  map[string]any{"width": res.WidthMM, "height": res.HeightMM},
		MMPerPx: round(res.MMPerPx, 5), Shape: res.ShapeIs, Inverted: res.Inverted,
		Origin: msgf(i18n.Msg{
			ES: "El SVG se importa con la esquina inferior izquierda de la forma en el origen (X→derecha, Y→arriba), en mm. Para centrarla: translate([-%s/2, -%s/2]).",
			EN: "The SVG imports with the shape's lower-left corner at the origin (X right, Y up), in mm. To center it: translate([-%s/2, -%s/2]).",
		}, fmtNum(res.WidthMM), fmtNum(res.HeightMM)),
		Notes: res.Notes,
	}
	if res.Threshold >= 0 {
		t := res.Threshold
		out.Threshold = &t
	}
	for i, l := range res.Layers {
		layerStem := stem
		if len(res.Layers) > 1 {
			layerStem = fmt.Sprintf("%s-%d", stem, l.Index)
		}
		path, err := e.writeRef(layerStem, "svg", res.SVG(i))
		if err != nil {
			return nil, err
		}
		holes := 0
		for _, c := range l.Contours {
			if c.Hole {
				holes++
			}
		}
		col := `"` + strings.ToLower(l.Color) + `"`
		if l.Color == "" {
			col = `"gray"`
		}
		out.PerLayer = append(out.PerLayer, traceLayerOut{
			Index: l.Index, File: path, Color: l.Color, Percent: l.Percent, AreaMM2: l.AreaMM2,
			Contours: len(l.Contours), Holes: holes, Points: l.PointCount(),
			Snippet: fmt.Sprintf("color(%s) linear_extrude(height = 2) import(%q);", col, path),
			Polygon: res.Polygon(i, maxPolygonPoints),
		})
		out.Files = append(out.Files, path)
		out.Contours += len(l.Contours)
		out.Holes += holes
		out.AreaMM2 += l.AreaMM2
	}
	out.AreaMM2 = round(out.AreaMM2, 3)
	out.Layers = len(out.PerLayer)
	snippets := make([]string, len(out.PerLayer))
	for i, l := range out.PerLayer {
		snippets[i] = l.Snippet
	}
	out.Snippet = strings.Join(snippets, "\n")
	out.Next = msgf(i18n.Msg{
		ES: "Usa el snippet en un create_checkpoint (cambia la altura y el color); la forma está en mm. Si el contorno no se parece a la imagen, prueba `threshold`, `invert` o `layers`.",
		EN: "Use the snippet in a create_checkpoint (change the height and the color); the shape is in mm. If the outline does not look like the image, try `threshold`, `invert` or `layers`.",
	})
	r, err := jsonResult(out)
	if err != nil {
		return nil, err
	}
	r.Content = append([]map[string]any{imageContent(png, "image/png")}, r.Content...)
	return r, nil
}

// writeRef crea refs/<stem>.<ext> sin pisar nada y le escribe los datos.
func (e *env) writeRef(stem, ext string, data []byte) (string, error) {
	f, rel, err := refmodel.ReserveRef(e.ws, stem, ext)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(e.ws, filepath.FromSlash(rel)))
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Join(e.ws, filepath.FromSlash(rel)))
		return "", err
	}
	return rel, nil
}

// --- image_to_heightmap ----------------------------------------------------------------

func (e *env) imageToHeightmap(ctx context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	opts := imagetools.HeightmapOptions{
		Invert:     boolArg(args, "invert", false),
		Stretch:    boolArg(args, "stretch", false),
		Resolution: intArg(args, "resolution", 0),
	}
	opts.WidthMM, _ = numArg(args, "width_mm")
	opts.HeightMM, _ = numArg(args, "height_mm")
	opts.MaxHeightMM, _ = numArg(args, "max_height_mm")
	opts.BaseMM, _ = numArg(args, "base_mm")

	octx, cancel := imageCtx(ctx)
	defer cancel()
	im, rel, err := e.loadImage(octx, args)
	if err != nil {
		return nil, imageErr(ctx, err)
	}
	h, err := imagetools.BuildHeightmap(octx, im, opts)
	if err != nil {
		return nil, imageErr(ctx, err)
	}
	png, err := h.PreviewPNG(400)
	if err != nil {
		return nil, err
	}
	stem := strings.TrimSpace(strArg(args, "name"))
	if stem == "" {
		stem = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)) + "-heightmap"
	}
	path, err := e.writeRef(stem, "dat", h.DAT())
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"image":   rel,
		"file":    path,
		"columns": h.Cols, "rows": h.Rows,
		"sizeMm": map[string]any{
			"width": h.WidthMM, "height": h.HeightMM, "base": h.BaseMM, "relief": h.MaxHeightMM,
			"maxThickness": round(h.BaseMM+h.MaxHeightMM, 3),
		},
		"scale":      []float64{round(h.WidthMM/float64(h.Cols-1), 6), round(h.HeightMM/float64(h.Rows-1), 6), round(h.MaxHeightMM/100, 6)},
		"valueRange": map[string]any{"min": h.Min, "max": h.Max},
		"invert":     h.Invert,
		"snippet":    h.Snippet(path),
		"next": msgf(i18n.Msg{
			ES: "Pega el snippet en un create_checkpoint dentro de color(). El mapa está centrado en X/Y; en Z la parte más fina mide %s mm. Si se ve poco relieve, usa `stretch`.",
			EN: "Paste the snippet into a create_checkpoint inside color(). The map is centered in X/Y; the thinnest part is %s mm thick. If the relief looks flat, use `stretch`.",
		}, fmtNum(h.BaseMM)),
	}
	if h.Max-h.Min < 25 {
		out["notes"] = []string{msgf(i18n.Msg{
			ES: "La imagen usa poco rango de grises (%.0f–%.0f de 100): el relieve será suave. `stretch` lo amplía.",
			EN: "The image uses a narrow gray range (%.0f–%.0f of 100): the relief will be shallow. `stretch` widens it.",
		}, h.Min, h.Max)}
	}
	r, err := jsonResult(out)
	if err != nil {
		return nil, err
	}
	r.Content = append([]map[string]any{imageContent(png, "image/png")}, r.Content...)
	return r, nil
}

func round3s(a [4]float64) [4]float64 {
	for i := range a {
		a[i] = round(a[i], 3)
	}
	return a
}

func fmtNum(v float64) string { return strconv.FormatFloat(round(v, 3), 'f', -1, 64) }
