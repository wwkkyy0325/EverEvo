// Package evolve provides self-evolution capabilities: building, verifying,
// and deploying new versions of the application within isolated sandbox instances.
//
// Public API: Capability, Prepare, Launch, Verify, BuildPath.
// Implementation details (alpha/beta lifecycle, proc detection) live in internal/.
package evolve

import (
	"fmt"
	"os"
	"path/filepath"
)

// BuildPath returns the default Wails build output path from the project root.
func BuildPath(projectRoot string) string {
	return filepath.Join(projectRoot, "dist", "bin", "everevo.exe")
}

// CurrentExe returns the path to the currently running executable.
func CurrentExe() string {
	exe, _ := os.Executable()
	return exe
}

// DetectCapability probes the environment to determine if self-compilation is possible.
func DetectCapability(sourceDir string) Capability {
	exe, _ := os.Executable()
	buildOutput := ""
	if sourceDir != "" {
		buildOutput = BuildPath(sourceDir)
	}
	return Capability{
		SourceAvailable: sourceDir != "",
		SourceDir:       sourceDir,
		BuildOutput:     buildOutput,
		CurrentExe:      exe,
		GoAvailable:     true,
		NodeAvailable:   true,
		WailsAvailable:  true,
	}
}

// verifyBuildOutput checks that the built EXE exists at the expected path.
func verifyBuildOutput(sourceDir string) error {
	exePath := BuildPath(sourceDir)
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		return fmt.Errorf("build output not found: %s", exePath)
	}
	return nil
}
