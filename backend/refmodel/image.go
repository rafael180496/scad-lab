package refmodel

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// ImagesDir es la subcarpeta de refs/ donde ScadLab guarda las imágenes que el
// usuario adjunta en el chat (refs/images/<nombre>-<hash>.<ext>). Las imágenes
// también pueden estar sueltas en refs/.
const ImagesDir = "images"

// Tipos de referencia (Ref.Kind).
const (
	KindModel = "model" // STL, 3MF u OBJ
	KindImage = "image" // PNG, JPEG, GIF o WebP
)

// Formatos de imagen que se reconocen, por extensión (el contenido lo confirma
// backend/imagetools al decodificar).
const (
	ImagePNG  = "png"
	ImageJPEG = "jpeg"
	ImageGIF  = "gif"
	ImageWebP = "webp"
)

// ImageFormatOf dice qué formato de imagen es un archivo según su extensión ("" si
// no es una imagen).
func ImageFormatOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return ImagePNG
	case ".jpg", ".jpeg":
		return ImageJPEG
	case ".gif":
		return ImageGIF
	case ".webp":
		return ImageWebP
	}
	return ""
}

func errNotImage(name string) error {
	return i18n.Errorf(i18n.Msg{
		ES: "%q no es una imagen: usa un archivo PNG, JPEG, GIF o WebP de refs/ o refs/images/",
		EN: "%q is not an image: use a PNG, JPEG, GIF or WebP file in refs/ or refs/images/",
	}, name)
}

// validRefName dice si es el nombre de un archivo suelto, seguro para unirlo a una
// carpeta: sin separadores, sin «..», sin unidad de Windows ni nombres ocultos.
func validRefName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\:\x00") && !strings.HasPrefix(name, ".")
}

// ResolveImage resuelve la imagen que nombró un modelo —`refs/images/foto.png`,
// `refs/foto.png`, o solo `foto.png`— a su ruta absoluta y a su ruta relativa
// al workspace. Es la puerta de las herramientas de imagen, con el mismo
// confinamiento que ResolveRef: ni «..», ni rutas absolutas, ni unidades de
// Windows, y si el archivo (o refs/images) es un enlace simbólico su destino
// tiene que seguir dentro de refs/.
func ResolveImage(workspaceDir, file string) (abs, rel string, err error) {
	name := strings.TrimSpace(file)
	name = strings.TrimPrefix(name, RefsDir+"/")
	bad := func() error {
		return i18n.Errorf(i18n.Msg{
			ES: "nombre de imagen inválido: %q (usa un archivo de refs/images/, p. ej. refs/images/foto.png)",
			EN: "invalid image name: %q (use a file in refs/images/, e.g. refs/images/photo.png)",
		}, file)
	}
	var candidates [][]string // cada uno son los componentes bajo refs/
	switch parts := strings.Split(name, "/"); {
	case len(parts) == 1 && validRefName(parts[0]):
		candidates = [][]string{{parts[0]}, {ImagesDir, parts[0]}}
	case len(parts) == 2 && parts[0] == ImagesDir && validRefName(parts[1]):
		candidates = [][]string{{ImagesDir, parts[1]}}
	default:
		return "", "", bad()
	}
	base := candidates[0][len(candidates[0])-1]
	if ImageFormatOf(base) == "" {
		return "", "", errNotImage(base)
	}
	dir, err := refsDir(workspaceDir, false)
	if err != nil {
		return "", "", err
	}
	for _, c := range candidates {
		p := filepath.Join(append([]string{dir}, c...)...)
		st, err := os.Lstat(p)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil || !within(dir, real) {
			return "", "", i18n.Errorf(i18n.Msg{ES: "la imagen %s apunta fuera de refs/", EN: "image %s points outside refs/"}, RefsDir+"/"+strings.Join(c, "/"))
		}
		if st.IsDir() || (st.Mode()&os.ModeSymlink != 0 && !isRegular(real)) {
			continue
		}
		return real, RefsDir + "/" + strings.Join(c, "/"), nil
	}
	return "", "", i18n.Errorf(i18n.Msg{ES: "no se encontró la imagen %s", EN: "image %s was not found"}, file)
}

func isRegular(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// SafeStem deja el nombre de un archivo que se va a crear en refs/ en [a-z0-9-],
// sin acentos ni espacios y sin nombres reservados de Windows.
func SafeStem(name string) string { return safeStem(name) }

// ReserveRef crea, sin pisar nunca, el archivo refs/<stem>.<ext> y lo devuelve
// abierto para escribir junto con su ruta relativa. Si el nombre ya existe agrega
// -2, -3… (la creación es exclusiva: no sigue un enlace simbólico puesto ahí).
// Crea refs/ si falta. Quien lo recibe lo cierra y, si falla, lo borra.
func ReserveRef(workspaceDir, stem, ext string) (*os.File, string, error) {
	dir, err := refsDir(workspaceDir, true)
	if err != nil {
		return nil, "", err
	}
	ext = "." + strings.ToLower(strings.TrimPrefix(ext, "."))
	stem = safeStem(stem)
	for n := 1; n < 1000; n++ {
		name := stem + ext
		if n > 1 {
			name = stem + "-" + strconv.Itoa(n) + ext
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return f, RefsDir + "/" + name, nil
	}
	return nil, "", i18n.New(i18n.Msg{ES: "ya hay demasiados archivos con ese nombre en refs/", EN: "there are too many files with that name in refs/ already"})
}
