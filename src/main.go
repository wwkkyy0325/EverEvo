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
	// Wails on Windows may launch with a truncated environment (missing
	// HOME/USERPROFILE). This causes conda/python to fail with "Could not
	// determine home directory" and pop up a terminal error. Ensure the
	// essential home-directory env vars are set before anything else.
	fixHomeEnv()

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

// fixHomeEnv ensures HOME and USERPROFILE are set in the process environment.
// Wails on Windows sometimes starts with a truncated environment that omits
// these variables, which causes conda/python to fail with "Could not determine
// home directory" and pop up an error terminal on every shell_exec call.
func fixHomeEnv() {
	if os.Getenv("USERPROFILE") == "" && os.Getenv("HOME") == "" {
		// Derive from HOMEDRIVE + HOMEPATH (Windows legacy pair).
		drive := os.Getenv("HOMEDRIVE")
		path := os.Getenv("HOMEPATH")
		if drive != "" && path != "" {
			home := drive + path
			os.Setenv("USERPROFILE", home)
			os.Setenv("HOME", home)
			return
		}
		// Last resort: use the known user profile path format.
		userprofile := os.Getenv("USERNAME")
		if userprofile != "" {
			home := `C:\Users\` + userprofile
			if _, err := os.Stat(home); err == nil {
				os.Setenv("USERPROFILE", home)
				os.Setenv("HOME", home)
			}
		}
	}
	// Ensure HOME mirrors USERPROFILE if only one is set.
	if os.Getenv("HOME") == "" {
		os.Setenv("HOME", os.Getenv("USERPROFILE"))
	}
	if os.Getenv("USERPROFILE") == "" {
		os.Setenv("USERPROFILE", os.Getenv("HOME"))
	}
}
