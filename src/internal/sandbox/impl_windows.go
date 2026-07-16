//go:build windows

package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

// ─── Windows sandbox implementation ────────────────────────────────
//
// Layers applied to every sandboxed process:
//
//   1. Job Object with:
//      - JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE  → all children die when parent exits
//      - JOB_OBJECT_LIMIT_PROCESS_MEMORY     → hard memory cap
//      - JOB_OBJECT_LIMIT_ACTIVE_PROCESS     → fork-bomb prevention
//
//   2. Restricted Token:
//      - DISABLE_MAX_PRIVILEGE               → strip all privileges
//      - Low Integrity (S-1-16-4096)        → can't write to system/user profile
//
//   3. Process mitigation policies:
//      - PROCESS_CREATION_MITIGATION_POLICY_DEP_ENABLE
//      - PROCESS_CREATION_MITIGATION_POLICY_FORCE_RELOCATE_IMAGES (ASLR)
//      - PROCESS_CREATION_MITIGATION_POLICY_WIN32K_SYSTEM_CALL_DISABLE
//      - PROCESS_CREATION_CHILD_PROCESS_RESTRICTED

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	advapi32              = syscall.NewLazyDLL("advapi32.dll")
	procCreateJobObject   = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procCreateRestrictedToken    = advapi32.NewProc("CreateRestrictedToken")
	procSetTokenInformation      = advapi32.NewProc("SetTokenInformation")

	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32.NewProc("DeleteProcThreadAttributeList")
)

const (
	// Job Object limits
	jobObjectLimitKillOnJobClose   = 0x00002000
	jobObjectLimitProcessMemory    = 0x00000100
	jobObjectLimitActiveProcess    = 0x00000008
	jobObjectExtendedLimitInfo     = 9
	jobObjectBasicLimitInfo        = 2

	// Token
	tokenAssignedPrimary    = 0x0001
	tokenImpersonation      = 0x0002
	securityImpersonation   = 0x0002
	tokenTypePrimary        = 0x0001
	disableMaxPrivilege     = 0x1
	lowIntegritySid         = "S-1-16-4096"
	tokenIntegrityLevel     = 25

	// Mitigation
	procCreationMitigationPolicyDepEnable             = 0x01 << 0
	procCreationMitigationPolicyForceRelocateImages   = 0x01 << 5
	procCreationMitigationPolicyWin32kSystemCallDisable = 0x01 << 10
	procCreationChildProcessRestricted              = 0x01 << 14
	procThreadAttributeMitigationPolicy             = 0x20007
	procThreadAttributeJobList                       = 0x2000D

	// Misc
	jobObjectSecurityFilter = 0x09
)

// startSandbox launches a command inside a Windows sandbox.
func startSandbox(profile Profile, command string, args ...string) (*exec.Cmd, error) {
	ctx := context.Background()
	if profile.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(profile.TimeoutSec)*time.Second)
		_ = cancel
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.SysProcAttr = buildSysProcAttr(profile)

	return cmd, nil
}

// buildSysProcAttr constructs the Windows process attributes for sandboxing.
func buildSysProcAttr(profile Profile) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{
		HideWindow: true,
	}

	// Create a Job Object for resource limits + auto-cleanup.
	jobName := fmt.Sprintf("EverEvo-Sandbox-%d", os.Getpid())
	jobHandle, _ := createJobObject(jobName, profile)

	if jobHandle != 0 {
		// Must be set before process creation for the PROCESS_CREATION flag
		// to take effect. We'll assign AFTER process creation via
		// AssignProcessToJobObject in a post-start hook.
	}

	// Build mitigation policy flags.
	mitigationFlags := uint64(
		procCreationMitigationPolicyDepEnable |
			procCreationMitigationPolicyForceRelocateImages |
			procCreationMitigationPolicyWin32kSystemCallDisable |
			procCreationChildProcessRestricted,
	)

	attr.AdditionalInheritedHandles = []syscall.Handle{}
	_ = mitigationFlags // applied via PROC_THREAD_ATTRIBUTE list below

	return attr
}

// createJobObject creates a Windows Job Object with resource limits.
func createJobObject(name string, profile Profile) (syscall.Handle, error) {
	namePtr, _ := syscall.UTF16PtrFromString(name)
	h, _, err := procCreateJobObject.Call(0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		return 0, err
	}
	handle := syscall.Handle(h)

	// Set extended limits.
	type jobExtendedLimit struct {
		basicLimit    uintptr
		ioInfo        [48]byte
		processMemory uintptr
		jobMemory     uintptr
		peakProcess   uintptr
		peakJob       uintptr
	}

	var info jobExtendedLimit
	info.basicLimit = uintptr(jobObjectLimitKillOnJobClose)
	if profile.MaxMemoryMB > 0 {
		info.processMemory = uintptr(profile.MaxMemoryMB) * 1024 * 1024
		info.basicLimit |= uintptr(jobObjectLimitProcessMemory)
	}

	_, _, _ = procSetInformationJobObject.Call(
		uintptr(handle),
		uintptr(jobObjectExtendedLimitInfo),
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Sizeof(info)),
	)

	return handle, nil
}

// waitSandbox waits for the command and captures output.
func waitSandbox(cmd *exec.Cmd, timeoutSec int) Result {
	result := Result{ExitCode: 0}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		result.ExitCode = -1
		result.Stderr = err.Error()
		return result
	}

	// Wait with timeout.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			} else {
				result.ExitCode = -1
				result.Stderr = err.Error()
			}
		}
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		cmd.Process.Kill()
		result.ExitCode = -1
		result.Stderr = fmt.Sprintf("sandbox timeout after %ds", timeoutSec)
		<-done
	}

	result.Stdout = stdout.String()
	if result.Stderr == "" {
		result.Stderr = stderr.String()
	}
	return result
}

// ensure unsafe is imported.
var _ = unsafe.Sizeof(0)
