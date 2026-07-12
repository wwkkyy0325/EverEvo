package guides

import (
	"io/fs"
	"testing"
)

// TestEmbeddedUserGuidesPresent guards the //go:embed directive: the bundled
// usage guides must compile into the binary so the default "everevo" source can
// materialize them with no network. If a file is renamed or moved outside the
// embed glob, this fails loudly instead of shipping an empty Guide Center.
func TestEmbeddedUserGuidesPresent(t *testing.T) {
	efs := embeddedUserGuides()
	var names []string
	err := fs.WalkDir(efs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		names = append(names, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded guides: %v", err)
	}
	if len(names) < 6 {
		t.Errorf("embedded guides: want >=6 files, got %d (%v)", len(names), names)
	}
	has := func(target string) bool {
		for _, n := range names {
			if n == target {
				return true
			}
		}
		return false
	}
	for _, must := range []string{"getting-started.md", "toolbox.md"} {
		if !has(must) {
			t.Errorf("embedded guides missing %q; have %v", must, names)
		}
	}
}

// TestDefaultEverEvoSource pins the seeded source shape (local type, enabled).
func TestDefaultEverEvoSource(t *testing.T) {
	s := defaultEverEvoSource()
	if s.Name != "everevo" || s.Type != "local" || !s.Enabled {
		t.Errorf("default source = %+v, want {Name:everevo Type:local Enabled:true}", s)
	}
}
