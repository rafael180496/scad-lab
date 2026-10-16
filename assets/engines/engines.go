// Package engines embebe los motores de render que viajan dentro del binario:
// hoy, el OpenSCAD oficial compilado a WebAssembly (backend/scadwasm) y las
// fuentes que necesita para text(). Los orígenes, las versiones, los SHA-256
// y las licencias están en THIRD_PARTY_NOTICES.md, en la raíz del repo.
//
// Vive en su propio paquete y no en assets.FS porque son 3 MB comprimidos que
// solo hacen falta cuando no hay un OpenSCAD instalado, y porque go:embed no
// admite rutas con "..": el paquete que embebe tiene que estar por encima de
// lo embebido.
package engines

import (
	"embed"
	"io/fs"
)

// OpenSCADWasmGz es openscad.wasm comprimido con gzip, tal como lo publica
// openscad.org (sin modificar; solo se comprime). scadwasm lo descomprime y
// verifica su SHA-256 antes de usarlo.
//
//go:embed openscad-wasm/openscad.wasm.gz
var OpenSCADWasmGz []byte

//go:embed all:openscad-wasm/fonts
var fontsFS embed.FS

// Fonts devuelve las fuentes embebidas (Liberation Sans, SIL OFL 1.1) con la
// raíz en la carpeta de las fuentes.
func Fonts() fs.FS {
	sub, err := fs.Sub(fontsFS, "openscad-wasm/fonts")
	if err != nil {
		// Solo si cambia la estructura del embed: se vería en el primer test.
		panic(err)
	}
	return sub
}
