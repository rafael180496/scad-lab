package threemf

import (
	"archive/zip"
	"testing"
)

// Atajos para las pruebas externas (package threemf_test) de este directorio.

// CheckPackage es la revisión estructural del paquete 3MF de threemf_test.go.
func CheckPackage(t *testing.T, path string, nParts int) { checkPackage(t, path, nParts) }

// ReadEntry devuelve el texto de una entrada del zip.
func ReadEntry(t *testing.T, zr *zip.Reader, name string) string { return readEntry(t, zr, name) }
