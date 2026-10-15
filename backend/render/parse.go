package render

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// El parser convierte la salida de OpenSCAD en diagnósticos con línea para los
// marcadores de Monaco y para RENDER_ERRORS.md.
//
// OpenSCAD no tiene un formato de mensaje estable: cada versión mueve la
// ubicación de lugar. Los que hay que entender (vistos en 2021.01 y en los
// snapshots 2025):
//
//	ERROR: Parser error in file "/ruta/x.scad", line 3: syntax error
//	WARNING: Ignoring unknown variable 'r', in file x.scad, line 5.
//	WARNING: Ignoring unknown module "foo" in file "x.scad", line 7
//	DEPRECATED: The import() function for STL is deprecated, line 4
//	ERROR: Assertion '(x > 0)' failed at line 9
//	WARNING: Can't open include file 'nope.scad'.        ← sin ubicación
//
// Por eso la ubicación se busca con varias expresiones en orden de
// especificidad, en vez de con una sola que asuma dónde va.

var (
	// prefixRe reconoce el tipo de línea. TRACE (la pila de llamadas que 2025
	// imprime debajo de un error) no se reporta: es contexto del error
	// anterior, y como diagnóstico propio llenaría el editor de marcadores
	// sobre líneas que no tienen la culpa. Queda en el log.
	prefixRe = regexp.MustCompile(`^\s*(ERROR|WARNING|DEPRECATED):\s*(.*)$`)

	// Ubicaciones, de la más específica a la menos.
	locQuotedRe = regexp.MustCompile(`,?\s*in file "([^"]+)",\s*line (\d+)(?:,\s*column (\d+))?`)
	locBareRe   = regexp.MustCompile(`,?\s*in file ([^,"]+?),\s*line (\d+)(?:,\s*column (\d+))?`)
	locAtRe     = regexp.MustCompile(`,?\s*at line (\d+)(?:,\s*column (\d+))?`)
	locCommaRe  = regexp.MustCompile(`,\s*line (\d+)(?:,\s*column (\d+))?`)

	// emptyRe: el modelo no generó geometría. 2021.01 dice "Current top level
	// object is empty."; los snapshots 2025, "top-level object is empty" o
	// "No top level geometry to render". En los tres casos OpenSCAD puede salir
	// con código 0 y sin escribir el STL: si no se detecta, el render parece
	// bueno y el visor queda en blanco sin explicación.
	emptyRe = regexp.MustCompile(`(?i)(top[ -]level object is empty|no top[ -]level geometry)`)
)

// parseOutput extrae los diagnósticos de la salida de OpenSCAD. workspaceDir y
// mainFile sirven para decidir si un mensaje apunta al propio checkpoint
// (File "") o a un include (File relativo al workspace).
func parseOutput(output, workspaceDir, mainFile string) []Diagnostic {
	diags := []Diagnostic{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		m := prefixRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sev := SeverityWarning
		if m[1] == "ERROR" {
			sev = SeverityError
		}
		d := Diagnostic{Severity: sev}
		d.Message, d.File, d.Line, d.Col = splitLocation(m[2])
		if d.Message == "" {
			continue
		}
		d.File = relFile(d.File, workspaceDir, mainFile)
		diags = append(diags, d)
	}
	return diags
}

// splitLocation separa el texto del mensaje de su ubicación. El mensaje queda
// sin la ubicación ("Parser error: syntax error"), porque la línea ya la
// muestra el marcador y repetirla ensucia la lista de diagnósticos.
func splitLocation(text string) (msg, file string, line, col int) {
	text = strings.TrimSpace(text)
	var loc []int
	switch {
	case locQuotedRe.MatchString(text):
		loc = locQuotedRe.FindStringSubmatchIndex(text)
		file = text[loc[2]:loc[3]]
		line, col = atoi(text, loc[4], loc[5]), atoi(text, loc[6], loc[7])
	case locBareRe.MatchString(text):
		loc = locBareRe.FindStringSubmatchIndex(text)
		file = strings.TrimSpace(text[loc[2]:loc[3]])
		line, col = atoi(text, loc[4], loc[5]), atoi(text, loc[6], loc[7])
	case locAtRe.MatchString(text):
		loc = locAtRe.FindStringSubmatchIndex(text)
		line, col = atoi(text, loc[2], loc[3]), atoi(text, loc[4], loc[5])
	case locCommaRe.MatchString(text):
		loc = locCommaRe.FindStringSubmatchIndex(text)
		line, col = atoi(text, loc[2], loc[3]), atoi(text, loc[4], loc[5])
	default:
		return cleanMessage(text), "", 0, 0
	}
	msg = text[:loc[0]] + text[loc[1]:]
	return cleanMessage(msg), file, line, col
}

// cleanMessage arregla lo que deja quitar la ubicación del medio de la frase:
// "Parser error : syntax error", comas sueltas, el punto final.
func cleanMessage(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, " :", ":")
	s = strings.ReplaceAll(s, " ,", ",")
	s = strings.TrimRight(s, " .,:;")
	s = strings.TrimLeft(s, " ,:")
	return s
}

func atoi(s string, from, to int) int {
	if from < 0 || to < 0 {
		return 0
	}
	n, _ := strconv.Atoi(s[from:to])
	return n
}

// relFile normaliza el archivo de un diagnóstico: "" si es el checkpoint que
// se renderizó (OpenSCAD lo nombra a veces con ruta absoluta, a veces como se
// lo pasamos), relativo al workspace si es un include de adentro, y tal cual
// si está afuera (una librería del sistema).
func relFile(file, workspaceDir, mainFile string) string {
	if file == "" {
		return ""
	}
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workspaceDir, file)
	}
	abs = filepath.Clean(abs)
	if abs == filepath.Clean(filepath.Join(workspaceDir, mainFile)) {
		return ""
	}
	if rel, err := filepath.Rel(workspaceDir, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return file
}

// isEmptyModel dice si OpenSCAD avisó que el nivel superior no tiene geometría.
func isEmptyModel(output string) bool { return emptyRe.MatchString(output) }

// not3DRe: el nivel superior es 2D. OpenSCAD 2026 dice "Current top level
// object is not a 3D object." y sale con 1, sin escribir el STL.
var not3DRe = regexp.MustCompile(`(?i)top[ -]level object is not a 3D object`)

func isNot3D(output string) bool { return not3DRe.MatchString(output) }

// rejectsOption dice si OpenSCAD terminó porque no conoce la opción `flag`.
// boost::program_options escribe "unrecognised option '--backend=manifold'"
// (con s) y algunas compilaciones "unknown option"; se aceptan las dos.
func rejectsOption(output, flag string) bool {
	low := strings.ToLower(output)
	for _, l := range strings.Split(low, "\n") {
		if (strings.Contains(l, "unknown option") || strings.Contains(l, "unrecognised option") ||
			strings.Contains(l, "unrecognized option")) && strings.Contains(l, flag) {
			return true
		}
	}
	return false
}
