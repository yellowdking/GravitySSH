package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/yellowdking/GravitySSH/internal/dpi"
)

// AppVersion can be overridden at build time via -ldflags "-X main.AppVersion=..."
var AppVersion = "1.5.0"

//go:embed all:frontend/dist
var assets embed.FS

func resolveWebviewUserDataPath() string {
	cfgDir, err := os.UserConfigDir()
	if err != nil || cfgDir == "" {
		return ""
	}
	dir := filepath.Join(cfgDir, "GravitySSH")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return dir
}

func main() {
	dpi.EnablePerMonitorV2()
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "GravitySSH",
		Width:            1380,
		Height:           860,
		MinWidth:         860,
		MinHeight:        540,
		BackgroundColour: &options.RGBA{R: 11, G: 13, B: 18, A: 255},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
		},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []any{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisablePinchZoom:     true,
			WebviewUserDataPath:  resolveWebviewUserDataPath(),
			Theme:                windows.Dark,
			CustomTheme: &windows.ThemeSettings{
				DarkModeTitleBar:   windows.RGB(16, 19, 26),
				DarkModeTitleText:  windows.RGB(230, 234, 245),
				DarkModeBorder:     windows.RGB(35, 41, 58),
				LightModeTitleBar:  windows.RGB(16, 19, 26),
				LightModeTitleText: windows.RGB(230, 234, 245),
				LightModeBorder:    windows.RGB(35, 41, 58),
			},
		},
	})
	if err != nil {
		log.Fatalf("GravitySSH startup error: %v", err)
	}
}
