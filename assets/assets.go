// Package assets embebe en el binario lo que ScadLab instala en cada
// workspace: las skills incluidas (skills/) y las plantillas de reglas para
// los agentes (workspace/).
//
// Vive en la raíz de assets/ porque go:embed no admite rutas con "..": el
// paquete que embebe tiene que estar por encima de lo embebido.
package assets

import "embed"

// FS contiene skills/** y workspace/**. El prefijo all: incluye los archivos
// que empiezan con "_" o ".", como la skill _skill-template.
//
//go:embed all:skills all:workspace
var FS embed.FS
