package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	logFile := setupLogging()
	if logFile != nil {
		defer logFile.Close()
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "AlwaysOnTop",
		Width:  750,
		Height: 300,

		// Fixed-size window matching the reference design; no native
		// title bar - frontend/index.html supplies the custom one.
		DisableResize: true,
		Frameless:     true,

		// A normal opaque window - no rounded corners on the frame
		// itself, so no transparency trick is needed here.
		BackgroundColour: &options.RGBA{R: 0x18, G: 0x18, B: 0x18, A: 255},

		AssetServer: &assetserver.Options{
			Assets: assets,
		},

		OnStartup:  app.OnStartup,
		OnShutdown: app.OnShutdown,

		Bind: []interface{}{
			app,
		},

		// AlwaysOnTop refers to *this* window, not the target windows
		// the app manages - keep it false per spec ("do NOT make the
		// AlwaysOnTop application itself always-on-top").
		AlwaysOnTop: false,

		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
