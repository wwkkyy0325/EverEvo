//go:build windows

package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	windowsOpts "github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	// --zone flag for multi-instance runtime isolation.
	// Production instance: everevo.exe (defaults to "production")
	// Experiment instance: everevo.exe --zone=experiment-fix-auth
	var zoneFlag string
	flag.StringVar(&zoneFlag, "zone", "production", "runtime zone name (production | experiment-*)")
	flag.Parse()

	// Set early so storage.AppDataDir() and all subsystems pick up the zone.
	os.Setenv("EVEREVO_ZONE", zoneFlag)

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "EverEvo",
		MinWidth:  640,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		// semi-transparent dark tint — the acrylic blur shows through this
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 20, A: 200},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Windows: &windowsOpts.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windowsOpts.Acrylic,
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		msg := fmt.Sprintf("启动失败\n\n%v", err)
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
}

// ExeDir returns the directory containing the running executable.
func ExeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}
