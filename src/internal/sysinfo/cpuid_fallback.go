//go:build !amd64

package sysinfo

func cpuidImpl(leaf, subleaf uint32) (uint32, uint32, uint32, uint32) {
	return 0, 0, 0, 0
}
