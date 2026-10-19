package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/zalando/go-keyring"

	"scadlab/backend/appdata"
	"scadlab/backend/i18n"
	"scadlab/backend/meshserve"
	"scadlab/backend/store"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

// appVersion la fija el empaquetado con -ldflags "-X main.appVersion=X.Y.Z"
// (ver scripts/package-*.sh). En `wails dev` queda "dev".
var appVersion = "dev"

func main() {
	// Modo servidor MCP (S6): corre antes de crear la App y de wails.Run, sin
	// ventana. Ver main_mcp.go.
	if len(os.Args) > 1 && os.Args[1] == "--mcp" {
		os.Exit(runMCP(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}

	// Pruebas de humo: con SCADLAB_FAKE_KEYRING=1 el llavero del sistema se
	// reemplaza por uno en memoria, para probar el llavero propio (S12) sin leer
	// ni escribir el Keychain / Administrador de credenciales real. Se usa junto
	// con SCADLAB_DATA_DIR temporal; nunca en uso normal.
	if os.Getenv("SCADLAB_FAKE_KEYRING") == "1" {
		keyring.MockInit()
	}

	app := NewApp()
	// El «Acerca de» nativo de macOS se arma antes de que la App lea sus
	// ajustes, así que el idioma guardado se consulta aquí.
	i18n.SetLang(savedLanguage())

	// Las mallas del render se sirven por HTTP (GET /mesh/<clave>.stl) desde
	// la misma caché que usa app.go, y las partes por color del modelo (S7)
	// en GET /parts/<clave>/NN.stl (app_parts.go). Sin directorio de datos no hay caché que
	// servir: la app abre igual, sin el middleware.
	var meshMiddleware assetserver.Middleware
	if dir, err := appdata.Dir(); err == nil {
		meshMiddleware = meshserve.MiddlewareWithParts(filepath.Join(dir, "cache", "meshes"), partsCacheDir(dir))
	} else {
		log.Printf("scadlab: servidor de mallas desactivado: %v", err)
	}

	err := wails.Run(&options.App{
		Title:     "ScadLab",
		Menu:      appMenu(),
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets:     frontendAssets,
			Middleware: meshMiddleware,
		},
		BackgroundColour: &options.RGBA{R: 13, G: 15, B: 20, A: 1},
		// En macOS la barra de título nativa queda oculta y la cabecera de la
		// app hace de barra (con los semáforos encima). En Windows se conserva
		// el marco nativo: sin él se pierden el snap de ventanas y el
		// redimensionado por los bordes que Windows da gratis.
		Frameless:        false,
		WindowStartState: options.Maximised,
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "ScadLab",
				Message: i18n.T(i18n.Msg{ES: "Diseño 3D con OpenSCAD e IA", EN: "3D design with OpenSCAD and AI"}),
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Dark,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			CSSDropProperty:    "--wails-drop-target",
			CSSDropValue:       "drop",
			DisableWebViewDrop: true,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

// appMenu es el menú nativo. En macOS hace falta aunque la app no tenga
// comandos propios: sin un menú Edición, WKWebView no le entrega ⌘C, ⌘V, ⌘X,
// ⌘Z ni ⌘A al editor (Monaco) ni a ningún campo de texto. El AppMenu da
// además Ocultar/Salir con sus atajos estándar. Ese menú reserva ⌘H para
// «Ocultar»: por eso reemplazar en el editor es ⌥⌘F en macOS.
//
// En Windows no se arma: WebView2 maneja esos atajos solo, y una barra de
// menú vacía de comandos quedaría fuera de lugar sobre la cabecera de la app.
func appMenu() *menu.Menu {
	if goruntime.GOOS != "darwin" {
		return nil
	}
	return menu.NewMenuFromItems(menu.AppMenu(), menu.EditMenu())
}

// savedLanguage lee settings.language sin dejar la base abierta: el diálogo
// «Acerca de» de macOS no se puede traducir después de wails.Run. Sin base o
// sin idioma elegido devuelve "" (inglés, el default).
func savedLanguage() string {
	path, err := appdata.DBPath()
	if err != nil {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	st, err := store.Open(path)
	if err != nil {
		return ""
	}
	defer st.Close()
	s, err := st.GetSettings()
	if err != nil {
		return ""
	}
	return s.Language
}
