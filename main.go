//go:build windows

package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	app "everevo/internal/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	windowsOpts "github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

//go:embed all:docs/llmwiki
var llmwikiFS embed.FS

func main() {
	// --zone flag for multi-instance runtime isolation.
	// Production instance: everevo.exe (defaults to "production")
	// Experiment instance: everevo.exe --zone=experiment-fix-auth
	var zoneFlag string
	var sandboxFlag bool
	var mcpPortFlag int
	var a2aPortFlag int
	flag.StringVar(&zoneFlag, "zone", "production", "runtime zone name (production | alpha | beta)")
	flag.BoolVar(&sandboxFlag, "sandbox", false, "run as sandbox instance")
	flag.IntVar(&mcpPortFlag, "mcp-port", 0, "MCP server port override")
	flag.IntVar(&a2aPortFlag, "a2a-port", 0, "A2A server port override")
	flag.Parse()

	os.Setenv("EVEREVO_ZONE", zoneFlag)
	if sandboxFlag {
		os.Setenv("EVEREVO_SANDBOX", "1")
	}
	if mcpPortFlag > 0 {
		os.Setenv("EVEREVO_MCP_PORT", fmt.Sprintf("%d", mcpPortFlag))
	}
	if a2aPortFlag > 0 {
		os.Setenv("EVEREVO_A2A_PORT", fmt.Sprintf("%d", a2aPortFlag))
	}

	a := app.New()
	app.LlmwikiFS = llmwikiFS

	err := wails.Run(&options.App{
		Title:     "EverEvo",
		MinWidth:  640,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		// semi-transparent dark tint — the acrylic blur shows through this
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 20, A: 200},
		OnStartup:        a.Startup,
		OnShutdown:       a.Shutdown,
		Windows: &windowsOpts.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windowsOpts.Acrylic,
		},
		Bind: []interface{}{
			a,
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
