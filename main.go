//go:build windows

// cc-enhance targets Windows (WebView2, Win32 taskbar/toast). The whole main
// package is Windows-only so non-Windows builds fail fast at the constraints
// stage instead of on undefined platform symbols.

package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// trayIconBytes is embedded for the system tray.
//
//go:embed build/windows/icon.ico
var trayIconBytes []byte

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "cc-enhance",
		Width:            300,
		Height:           360,
		MinWidth:         32,
		MinHeight:        24,
		MaxWidth:         560,
		MaxHeight:        640,
		Frameless:        true,
		AlwaysOnTop:      true,
		HideWindowOnClose: true,
		StartHidden:      false,
		BackgroundColour: &options.RGBA{R: 22, G: 22, B: 28, A: 255},
		CSSDragProperty:  "--wails-draggable",
		CSSDragValue:     "drag",
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:     app.startup,
		OnDomReady:    app.domReady,
		OnShutdown:    app.shutdown,
		OnBeforeClose: func(ctx context.Context) bool { return app.beforeClose(ctx) },
		Bind: []interface{}{
			app,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "cc-enhance-7f3a1c2e-9b4d-4e8a-b61c-2d5e9f0a1b3c",
		},
		Windows: &windows.Options{
			Theme: windows.Dark,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
