package hermes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/workspace"
)

// Las fotos del chat viven también en el workspace (S15): al enviar, cada imagen
// se copia a <ws>/refs/images/<nombre>-<hash8>.<ext>. Así las herramientas de
// imagen (image_info, compare_with_image, trace_image_outline…) la pueden abrir
// por una ruta relativa, y una foto no se pierde entre turnos ni entre
// proveedores. Lo que ya existe en <datos>/chat-attachments/ sigue igual: es la
// miniatura y lo que viaja al modelo con visión.

// ImagesRefDir es la carpeta del workspace donde quedan las fotos del chat.
const ImagesRefDir = "refs/images"

// maxImageSlug acota la parte legible del nombre: el hash ya hace único al
// archivo, y un nombre largo estorba en una ruta que el modelo tiene que repetir.
const maxImageSlug = 40

// imageSlug es la parte legible del nombre de la foto: sin extensión, sin
// acentos, en [a-z0-9-]. Una foto «IMG 0042.JPG» queda «img-0042».
func imageSlug(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	s := workspace.Slug(stem)
	if len(s) > maxImageSlug {
		s = strings.Trim(s[:maxImageSlug], "-")
	}
	if s == "" {
		s = "foto"
	}
	return s
}

// imagesDir resuelve (y crea) <ws>/refs/images comprobando que, con los enlaces
// resueltos, siga dentro del workspace: refs/ puede ser un enlace que apunte a
// otro lado y la copia no debe escribir fuera.
func imagesDir(wsDir string) (string, error) {
	root, err := filepath.EvalSymlinks(wsDir)
	if err != nil {
		return "", err
	}
	outside := func() error {
		return i18n.New(i18n.Msg{ES: "la carpeta refs apunta fuera del workspace", EN: "the refs folder points outside the workspace"})
	}
	within := func(p string) bool {
		rel, err := filepath.Rel(root, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	// refs/ primero y antes de crear nada debajo: si es un enlace hacia afuera,
	// ni siquiera se crea la carpeta images allá.
	refs := filepath.Join(root, "refs")
	if err := os.MkdirAll(refs, 0o755); err != nil {
		return "", err
	}
	realRefs, err := filepath.EvalSymlinks(refs)
	if err != nil {
		return "", err
	}
	if !within(realRefs) {
		return "", outside()
	}
	dir := filepath.Join(realRefs, "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if !within(real) {
		return "", outside()
	}
	return real, nil
}

// CopyImageToRefs copia una imagen a <ws>/refs/images/ y devuelve su ruta
// relativa, con «/», lista para pasar a una herramienta ("refs/images/x-ab12cd34.png").
//
//   - El nombre es <slug del nombre>-<hash8>.<ext>; el hash es el SHA-256 del
//     contenido, así que la misma foto enviada dos veces es el mismo archivo
//     (se reutiliza) y dos fotos distintas con el mismo nombre no se pisan.
//   - Nunca pisa: si el nombre existe con otro contenido (una colisión de 8
//     dígitos, que no pasa en la práctica) se alarga el hash.
//   - La escritura es atómica: un archivo temporal en la misma carpeta y luego
//     un enlace al nombre final, que falla si ya existe. Nadie ve una foto a medias.
func CopyImageToRefs(wsDir, name, mime string, raw []byte) (string, error) {
	ext := ImageExt(mime)
	if ext == "" {
		return "", i18n.Errorf(i18n.Msg{ES: "%q no es una imagen que se pueda guardar", EN: "%q is not an image that can be saved"}, name)
	}
	if len(raw) == 0 {
		return "", i18n.Errorf(i18n.Msg{ES: "%q está vacío", EN: "%q is empty"}, name)
	}
	dir, err := imagesDir(wsDir)
	if err != nil {
		return "", i18n.Errorf(i18n.Msg{ES: "no se pudo preparar la carpeta refs/images: %w", EN: "could not prepare the refs/images folder: %w"}, err)
	}
	sum := sha256.Sum256(raw)
	full := hex.EncodeToString(sum[:])
	stem := imageSlug(name)
	for _, n := range []int{8, 16, len(full)} {
		file := stem + "-" + full[:n] + ext
		dst := filepath.Join(dir, file)
		created, err := writeNew(dir, dst, raw)
		if err != nil {
			return "", i18n.Errorf(i18n.Msg{ES: "no se pudo guardar %q en refs/images: %w", EN: "could not save %q in refs/images: %w"}, name, err)
		}
		if created {
			return ImagesRefDir + "/" + file, nil
		}
		// Ya existía: es la misma foto si el contenido coincide.
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, raw) {
			return ImagesRefDir + "/" + file, nil
		}
	}
	return "", i18n.Errorf(i18n.Msg{ES: "no se pudo guardar %q en refs/images", EN: "could not save %q in refs/images"}, name)
}

// writeNew escribe raw en dst sin pisar nada: devuelve false si dst ya existe.
func writeNew(dir, dst string, raw []byte) (created bool, err error) {
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	_ = os.Chmod(tmpName, 0o644)
	// Link falla con ErrExist si el destino ya está: ni un rename ni una copia
	// pisarían nada, pero tampoco avisarían.
	err = os.Link(tmpName, dst)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrExist):
		return false, nil
	}
	// Un sistema de archivos sin enlaces duros (exFAT, algunos recursos de red):
	// se comprueba y se renombra. La ventana entre las dos llamadas es de
	// microsegundos y el peor caso es reemplazar la misma foto por sí misma.
	if _, serr := os.Lstat(dst); serr == nil {
		return false, nil
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return false, err
	}
	return true, nil
}

// ImagePathsNote es la línea que cierra el mensaje del turno cuando trae fotos:
// las rutas relativas al workspace que el modelo pasa a las herramientas de
// imagen. Una sola línea, entre corchetes, para que se distinga de lo que
// escribió la persona. "" si no hay fotos.
func ImagePathsNote(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	return i18n.T(i18n.Msg{
		ES: "[Fotos de este mensaje, en el proyecto (pasa estas rutas a las herramientas de imagen): %s]",
		EN: "[Photos of this message, in the project (pass these paths to the image tools): %s]",
	}, strings.Join(refs, ", "))
}
