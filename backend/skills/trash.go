package skills

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"scadlab/backend/i18n"
)

// La papelera de skills vive en los datos de la app (<datos>/trash/…), igual que
// la de checkpoints vive en .scadlab/trash de su workspace: «borrar» mueve, con
// la fecha delante, y no hay botón que vacíe. Sacar algo de ahí es mover la
// carpeta de vuelta a mano.

// trashStamp es el prefijo de fecha (AAAAMMDD-HHMMSS, hora local), el mismo de
// la papelera de checkpoints.
const trashStamp = "20060102-150405"

// now se reemplaza en los tests para fijar la hora.
var now = time.Now

// TrashDir es <datos>/trash/<kind>.
func TrashDir(dataDir, kind string) string { return filepath.Join(dataDir, "trash", kind) }

var errTrashFull = i18n.New(i18n.Msg{ES: "no se encontró un nombre libre en la papelera", EN: "could not find a free name in the trash"})

// MoveToTrash mueve src (carpeta o archivo) a <datos>/trash/<kind>/<fecha>-<label>
// y devuelve el destino. Si ese nombre ya existe corre la marca de a un segundo.
func MoveToTrash(dataDir, kind, label, src string) (string, error) {
	dir := TrashDir(dataDir, kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	t := now()
	for i := 0; i < 100; i++ {
		dst := filepath.Join(dir, t.Add(time.Duration(i)*time.Second).Format(trashStamp)+"-"+label)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		return dst, MovePath(src, dst)
	}
	return "", errTrashFull
}

var (
	errDstExists = i18n.New(i18n.Msg{ES: "el destino ya existe", EN: "the destination already exists"})
	errSymlink   = i18n.New(i18n.Msg{
		ES: "la skill contiene un enlace simbólico y no se puede mover a otro disco",
		EN: "the skill contains a symbolic link and cannot be moved to another disk",
	})
)

// MovePath mueve src a dst sin pisar nada: falla si dst existe. Renombra; si no
// se puede (otro disco, otra unidad), copia y después borra el origen. Una
// copia incompleta se deshace y el origen queda intacto.
func MovePath(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return errDstExists
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// copyTree copia un archivo o una carpeta. Un enlace simbólico es un error: se
// borraría el original sin que la copia lo conserve.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyFile(p, target)
		default:
			return errSymlink
		}
	})
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	_, err = io.Copy(out, in)
	return err
}
