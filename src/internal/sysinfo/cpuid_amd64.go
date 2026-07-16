//go:build amd64

package sysinfo

// cpuidImpl(leaf, subleaf) -> (eax, ebx, ecx, edx)
func cpuidImpl(leaf, subleaf uint32) (uint32, uint32, uint32, uint32)
