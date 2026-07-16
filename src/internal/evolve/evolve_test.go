package evolve

import (
	"testing"
)

func TestBuildPath(t *testing.T) {
	path := BuildPath("F:\\EverEvo")
	expected := "F:\\EverEvo\\dist\\bin\\everevo.exe"
	if path != expected {
		t.Errorf("BuildPath = %s, want %s", path, expected)
	}
}

func TestDetectCapability_NoSource(t *testing.T) {
	c := DetectCapability("")
	if c.SourceAvailable {
		t.Error("DetectCapability('').SourceAvailable should be false")
	}
}

func TestDetectCapability_WithSource(t *testing.T) {
	c := DetectCapability("F:\\EverEvo")
	if !c.SourceAvailable {
		t.Error("DetectCapability with source dir should have SourceAvailable=true")
	}
	if c.BuildOutput != "F:\\EverEvo\\dist\\bin\\everevo.exe" {
		t.Errorf("BuildOutput = %s, want F:\\EverEvo\\dist\\bin\\everevo.exe", c.BuildOutput)
	}
}

func TestCurrentExe(t *testing.T) {
	exe := CurrentExe()
	if exe == "" {
		t.Error("CurrentExe should not be empty")
	}
}

func TestNameConstants(t *testing.T) {
	if Alpha == "" || string(Alpha) != "alpha" {
		t.Errorf("Alpha = %s, want 'alpha'", Alpha)
	}
	if Beta == "" || string(Beta) != "beta" {
		t.Errorf("Beta = %s, want 'beta'", Beta)
	}
}

func TestOther(t *testing.T) {
	if Other(Alpha) != Beta {
		t.Error("Other(Alpha) should be Beta")
	}
	if Other(Beta) != Alpha {
		t.Error("Other(Beta) should be Alpha")
	}
}

func TestMCPPortFor(t *testing.T) {
	if MCPPortFor(Alpha) == 0 {
		t.Error("MCPPortFor(Alpha) should not be 0")
	}
	if MCPPortFor(Alpha) == MCPPortFor(Beta) {
		t.Error("Alpha and Beta MCP ports should differ")
	}
}

func TestA2APortFor(t *testing.T) {
	if A2APortFor(Alpha) == 0 {
		t.Error("A2APortFor(Alpha) should not be 0")
	}
	if A2APortFor(Alpha) == A2APortFor(Beta) {
		t.Error("Alpha and Beta A2A ports should differ")
	}
}
