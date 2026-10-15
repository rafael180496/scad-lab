package main

import (
	"embed"
	"log"
	"path/filepath"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"scadlab/backend/appdata"
	"scadlab/backend/meshserve"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

// appVersion la fija el empaquetado con -ldflags "-X main.appVersion=X.Y.Z"
// (ver scripts/package-*.sh). En `wails dev` queda "dev".
var appVersion = "dev"

func main() {
	app := NewApp()

	// Las mallas del render se sirven por HTTP (GET /mesh/<clave>.stl) desde
	// la misma caché que usa app.go. Sin directorio de datos no hay caché que
	// servir: la app abre igual, sin el middleware.
	var meshMiddleware assetserver.Middleware
	if dir, err := appdata.Dir(); err == nil {
		meshMiddleware = meshserve.Middleware(filepath.Join(dir, "cache", "meshes"))
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
				Message: "Diseño 3D con OpenSCAD e IA",
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
