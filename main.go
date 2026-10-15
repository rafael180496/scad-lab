package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

// appVersion la fija el empaquetado con -ldflags "-X main.appVersion=X.Y.Z"
// (ver scripts/package-*.sh). En `wails dev` queda "dev".
var appVersion = "dev"

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "ScadLab",
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
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
