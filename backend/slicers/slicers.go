// Package slicers es el registro de los laminadores (slicers) a los que ScadLab
// sabe abrir un 3MF/STL, y cómo encontrarlos en esta máquina. No lanza nada:
// arma la ruta y el comando; quien abre el slicer es la app, con un lanzador
// que los tests reemplazan (regla 9 de .claude/rules/technical.md).
//
// # Cómo editar la tabla
//
// Registry es la única lista. Una marca de slicer nueva es una entrada nueva;
// un nombre de instalación que cambia es una línea nueva en Mac o Win de su
// entrada (las líneas viejas se dejan: sirven a quien no actualizó).
//
// Mac: nombres de paquete .app.
//   - Absoluto ("/Applications/X.app") se usa tal cual.
//   - "~/..." cuelga de la carpeta personal.
//   - Relativo ("X.app") se busca en /Applications y en ~/Applications.
//
// Win: rutas relativas a las carpetas de programas (%ProgramFiles%,
// %ProgramFiles(x86)% y %LocalAppData%\Programs), siempre con "/" como
// separador (se convierte al del sistema). Una carpeta con versión en el nombre
// ("UltiMaker Cura 5.9.0") se escribe con comodín ("UltiMaker Cura */...").
//
// Un comodín (filepath.Glob) puede dar varias coincidencias: se elige la última
// en orden natural, es decir la versión más nueva ("5.10" va después de "5.9").
// Glob distingue mayúsculas también en Windows, así que si un fabricante usó
// dos capitalizaciones a lo largo de las versiones va una línea por cada una.
package slicers

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"scadlab/backend/i18n"
)

// Def es un slicer del registro.
type Def struct {
	ID   string
	Name string
	Mac  []string // ver «Cómo editar la tabla»
	Win  []string
}

// Registry son los slicers conocidos, en el orden en que se ofrecen en
// Configuración. Los ids son contrato con el campo `slicers` de cada impresora
// del catálogo (backend/printers).
//
// Cada línea sale del código o del instalador del propio fabricante (S14,
// investigación de octubre de 2026). Las dos que NO se pudieron verificar —la
// ruta de Windows de Bambu Studio y todo Anycubic Slicer Next— están marcadas
// «sin verificar»: son el mejor candidato conocido y la ruta configurable por
// slicer (settings.slicerPaths) cubre el resto.
var Registry = []Def{
	{
		ID: "bambu-studio", Name: "Bambu Studio",
		Mac: []string{"BambuStudio.app", "Bambu Studio.app"}, // el build crea BambuStudio.app; el bundle se llama «Bambu Studio»
		Win: []string{"Bambu Studio/bambu-studio.exe"},       // sin verificar: convención conocida, sin fuente oficial
	},
	{
		ID: "orcaslicer", Name: "OrcaSlicer",
		Mac: []string{"OrcaSlicer.app"},
		Win: []string{"OrcaSlicer/orca-slicer.exe"},
	},
	{
		ID: "prusaslicer", Name: "PrusaSlicer",
		Mac: []string{"PrusaSlicer.app"},
		Win: []string{"Prusa3D/PrusaSlicer/prusa-slicer.exe"},
	},
	{
		ID: "creality-print", Name: "Creality Print",
		Mac: []string{"CrealityPrint.app"},
		Win: []string{"Creality/Creality Print */CrealityPrint.exe"}, // la carpeta lleva MAJOR.MINOR
	},
	{
		ID: "elegoo-slicer", Name: "ElegooSlicer",
		Mac: []string{"ElegooSlicer.app"},
		Win: []string{"ElegooSlicer/elegoo-slicer.exe"},
	},
	{
		// Sin verificar: el repositorio público de Anycubic Slicer Next conserva los
		// nombres de OrcaSlicer y el producto distribuido puede diferir. Es el mejor
		// candidato; la ruta configurable lo cubre.
		ID: "anycubic-slicer-next", Name: "Anycubic Slicer Next",
		Mac: []string{"AnycubicSlicerNext.app", "Anycubic Slicer Next.app"},
		Win: []string{"AnycubicSlicerNext/AnycubicSlicerNext.exe", "Anycubic Slicer Next/AnycubicSlicerNext.exe"},
	},
	{
		ID: "snapmaker-orca", Name: "Snapmaker Orca",
		Mac: []string{"Snapmaker Orca.app", "Snapmaker_Orca.app"}, // el script de empaquetado admite las dos
		Win: []string{"Snapmaker_Orca/snapmaker-orca.exe"},
	},
	{
		ID: "ultimaker-cura", Name: "UltiMaker Cura",
		Mac: []string{"UltiMaker Cura.app"},
		Win: []string{"UltiMaker Cura */UltiMaker-Cura.exe"}, // la carpeta lleva la versión completa
	},
}

// ByID busca un slicer del registro.
func ByID(id string) (Def, bool) {
	for _, d := range Registry {
		if d.ID == id {
			return d, true
		}
	}
	return Def{}, false
}

// Recommended es el slicer recomendado de una impresora: el primero de su lista
// que el registro conoce (un id desconocido se salta en vez de dejar a la
// impresora sin recomendación). ids es el campo `slicers` del catálogo.
func Recommended(ids []string) (Def, bool) {
	for _, id := range ids {
		if d, ok := ByID(id); ok {
			return d, true
		}
	}
	return Def{}, false
}

// Env es lo del sistema que cambia dónde se instala un slicer. Es un valor y no
// lecturas directas de os.Getenv para poder probar los dos sistemas desde uno.
type Env struct {
	GOOS            string
	Home            string // carpeta personal
	Applications    string // carpeta de aplicaciones de macOS ("" = /Applications)
	ProgramFiles    string // %ProgramFiles%
	ProgramFilesX86 string // %ProgramFiles(x86)%
	LocalAppData    string // %LocalAppData%
}

// CurrentEnv lee el entorno de este proceso.
func CurrentEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{
		GOOS: runtime.GOOS, Home: home,
		ProgramFiles:    os.Getenv("ProgramFiles"),
		ProgramFilesX86: os.Getenv("ProgramFiles(x86)"),
		LocalAppData:    os.Getenv("LocalAppData"),
	}
}

// Candidates son las rutas (o patrones con comodín) donde puede estar el slicer
// en este sistema, en orden de preferencia. Vacío en un sistema sin soporte.
func (d Def) Candidates(env Env) []string {
	var out []string
	switch env.GOOS {
	case "darwin":
		apps := env.Applications
		if apps == "" {
			apps = "/Applications"
		}
		for _, e := range d.Mac {
			switch {
			case filepath.IsAbs(e):
				out = append(out, e)
			case strings.HasPrefix(e, "~/"):
				if env.Home != "" {
					out = append(out, filepath.Join(env.Home, filepath.FromSlash(e[2:])))
				}
			default:
				out = append(out, filepath.Join(apps, e))
				if env.Home != "" {
					out = append(out, filepath.Join(env.Home, "Applications", e))
				}
			}
		}
	case "windows":
		pf := env.ProgramFiles
		if pf == "" {
			pf = `C:\Program Files`
		}
		roots := []string{pf}
		if env.ProgramFilesX86 != "" {
			roots = append(roots, env.ProgramFilesX86)
		}
		if env.LocalAppData != "" {
			roots = append(roots, filepath.Join(env.LocalAppData, "Programs"))
		}
		for _, e := range d.Win {
			rel := filepath.FromSlash(e)
			for _, root := range roots {
				out = append(out, filepath.Join(root, rel))
			}
		}
	}
	return out
}

// Find devuelve la ruta del slicer: la configurada si existe y, si no, la
// primera de los candidatos que exista (con comodín, la versión más nueva).
// "" si no está. En macOS una ruta configurada que apunta dentro del .app (el
// ejecutable de Contents/MacOS) se lleva al propio .app, que es lo que
// `open -a` espera.
func Find(d Def, env Env, configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		if env.GOOS == "darwin" {
			configured = AppBundlePath(configured)
		}
		if _, err := os.Stat(configured); err == nil {
			return configured
		}
	}
	for _, c := range d.Candidates(env) {
		if !strings.ContainsAny(c, "*?[") {
			if _, err := os.Stat(c); err == nil {
				return c
			}
			continue
		}
		matches, err := filepath.Glob(c)
		if err != nil || len(matches) == 0 {
			continue
		}
		sort.Slice(matches, func(i, j int) bool { return naturalLess(matches[i], matches[j]) })
		return matches[len(matches)-1]
	}
	return ""
}

// AppBundlePath lleva una ruta dentro de un .app (p. ej. el ejecutable de
// Contents/MacOS) al propio .app.
func AppBundlePath(p string) string {
	if i := strings.Index(strings.ToLower(p), ".app"+string(filepath.Separator)); i >= 0 {
		return p[:i+len(".app")]
	}
	return p
}

// LaunchCommand arma el comando que abre `file` con la app del slicer, según el
// sistema: se abre la APLICACIÓN con el archivo, sin shell y sin ninguna opción
// de la CLI del slicer (--slice, --export-3mf… son las que hacen caer a Bambu
// Studio y abren el diálogo de «se cerró inesperadamente»).
func LaunchCommand(goos, appPath, file string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{"-a", appPath, file}, nil
	case "windows":
		return appPath, []string{file}, nil
	}
	return "", nil, i18n.New(i18n.Msg{
		ES: "abrir en un slicer solo está disponible en macOS y Windows",
		EN: "opening in a slicer is only available on macOS and Windows",
	})
}

// naturalLess ordena "5.9" antes de "5.10": los tramos de dígitos se comparan
// como números y el resto como texto sin distinguir mayúsculas.
func naturalLess(a, b string) bool {
	ca, cb := chunks(a), chunks(b)
	for i := 0; i < len(ca) && i < len(cb); i++ {
		x, y := ca[i], cb[i]
		if x == y {
			continue
		}
		if isDigits(x) && isDigits(y) {
			// Sin ceros a la izquierda, más corto = menor; si no, orden de texto.
			x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if len(x) != len(y) {
				return len(x) < len(y)
			}
		} else {
			x, y = strings.ToLower(x), strings.ToLower(y)
		}
		if x != y {
			return x < y
		}
	}
	return len(ca) < len(cb)
}

func isDigits(s string) bool { return s != "" && unicode.IsDigit(rune(s[0])) }

// chunks parte s en tramos alternados de dígitos y no dígitos.
func chunks(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || unicode.IsDigit(rune(s[i])) != unicode.IsDigit(rune(s[i-1])) {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
}
