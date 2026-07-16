//go:build windows

package evolve

import (
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
)

const (
	waitTimeout  = 0x00000102
	stillActive  = 259
	processQueryLimitedInfo = 0x1000
	synhronize = 0x00100000
)

// isPIDAlive checks whether a Windows process is still running.
// Uses WaitForSingleObject with zero timeout — returns immediately.
func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInfo|synhronize, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)

	ret, _, _ := procWaitForSingleObject.Call(uintptr(h), 0)
	return ret == waitTimeout
}

// ensure we don't import unsafe unnecessarily
var _ = unsafe.Sizeof(0)
