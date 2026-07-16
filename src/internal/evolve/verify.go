package evolve

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

)

// VerificationTask describes a self-evolution verification request.
// The orchestrator (old instance) sends this to the sandbox (new instance)
// via A2A to confirm that the code change actually fixes the problem.
type VerificationTask struct {
	OriginalProblem string   `json:"originalProblem"`
	TestSteps       []string `json:"testSteps"`
	ExpectedResult  string   `json:"expectedResult"`
}

// VerificationReport is returned by the sandbox after executing tests.
type VerificationReport struct {
	Passed   bool     `json:"passed"`
	Results  []string `json:"results"`
	Evidence string   `json:"evidence"`
}

// SandboxState tracks the current evolution cycle.
type SandboxState struct {
	Active  Name `json:"active"`  // currently "alpha" or "beta"
	Pending Name `json:"pending"` // the one being verified
}

// CurrentSandbox returns the currently active sandbox name, or "" if none.
// In dev mode (production zone), sandbox is determined by the .sandbox_state file.
func CurrentSandbox(projectRoot string) Name {
	data, err := os.ReadFile(filepath.Join(projectRoot, "data", ".sandbox_state"))
	if err != nil {
		return "" // no sandbox active yet
	}
	name := strings.TrimSpace(string(data))
	if name == "alpha" || name == "beta" {
		return Name(name)
	}
	return ""
}

// SaveSandboxState persists the active sandbox name.
func SaveSandboxState(projectRoot string, active Name) error {
	dir := filepath.Join(projectRoot, "data")
	os.MkdirAll(dir, 0755)
	return os.WriteFile(filepath.Join(dir, ".sandbox_state"), []byte(active), 0644)
}

// RunEvolutionCycle executes a complete self-evolution cycle:
//   1. Build the current source (caller must already have done this)
//   2. Copy EXE to the pending sandbox
//   3. Launch the new instance
//   4. Return the instance for A2A verification (caller handles the A2A chat)
//   5. On Accept: save state, stop old sandbox
//   6. On Reject: kill new instance, report failure
func RunEvolutionCycle(projectRoot, buildExePath string) (*Instance, error) {
	// Determine target 
	current := CurrentSandbox(projectRoot)
	var target Name
	if current == "" || current == Beta {
		target = Alpha
	} else {
		target = Beta
	}

	log.Printf("[evolve] 进化周期启动: current=%s target=%s", current, target)

	// Stop the target sandbox if it's somehow still running from a previous attempt.
	if inst, err := Prepare(projectRoot, target, buildExePath); err == nil {
		_ = inst.Stop() // best-effort cleanup
	}

	// Prepare the sandbox with the new EXE.
	inst, err := Prepare(projectRoot, target, buildExePath)
	if err != nil {
		return nil, fmt.Errorf("prepare sandbox %s: %w", target, err)
	}

	// Launch the new instance.
	if err := inst.Launch(); err != nil {
		return nil, fmt.Errorf("launch sandbox %s: %w", target, err)
	}

	// Wait for it to be ready (A2A endpoint responding).
	log.Printf("[evolve] 等待 sandbox:%s 就绪 (最多 30s)...", target)
	if err := inst.WaitReady(30 * time.Second); err != nil {
		_ = inst.Stop()
		return nil, fmt.Errorf("sandbox %s 启动超时: %w", target, err)
	}

	return inst, nil
}

// AcceptEvolution promotes the verified sandbox instance as the active one,
// saves the state, and returns instructions for the old instance to exit.
func AcceptEvolution(projectRoot string, inst *Instance) error {
	log.Printf("[evolve] ✅ 验证通过！切换至 sandbox:%s", inst.Name)
	if err := SaveSandboxState(projectRoot, inst.Name); err != nil {
		return fmt.Errorf("save sandbox state: %w", err)
	}
	log.Printf("[evolve] sandbox:%s 已成为主实例，旧实例可退出", inst.Name)
	return nil
}

// RejectEvolution stops the failed sandbox instance and reports the reason.
func RejectEvolution(inst *Instance, reason string) error {
	log.Printf("[evolve] ❌ 验证失败: %s", reason)
	if err := inst.Stop(); err != nil {
		log.Printf("[evolve] 停止 sandbox:%s 失败: %v", inst.Name, err)
	}
	return fmt.Errorf("evolution rejected: %s", reason)
}
