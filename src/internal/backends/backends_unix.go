//go:build linux || darwin

package backends

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func loadSymbol(dllPath, symbol string) (bool, error) {
	// CGo dlopen check — fallback to nm
	_, err := exec.Command("nm", "-D", dllPath).Output()
	if err != nil {
		// try ldd
		_, err = exec.Command("ldd", dllPath).Output()
	}
	if err != nil {
		return false, err
	}
	// Rough check: file exists and can be read by nm/ldd
	if _, err := os.Stat(dllPath); err != nil {
		return false, err
	}
	return true, nil
}

func findInPath(pattern string) (string, error) {
	if strings.Contains(pattern, "*") {
		return "", fmt.Errorf("不支持通配")
	}
	cmd := exec.Command("which", pattern)
	out, _ := cmd.Output()
	p := strings.TrimSpace(string(out))
	if p != "" {
		return p, nil
	}
	return "", fmt.Errorf("未找到")
}

// ─── GPU Detection ───────────────────────────────────────────────

// detectGPU checks for an NVIDIA GPU on Linux/macOS.
// Uses nvidia-smi as primary, falls back to /proc/driver/nvidia/version.
func detectGPU() GPUInfo {
	// Primary: nvidia-smi
	if info := detectGPUFromSMI(); info.Found {
		return info
	}

	// Fallback: check for NVIDIA kernel module / proc entry
	if info := detectGPUFromProc(); info.Found {
		return info
	}

	// Last resort: check for libcuda.so
	if info := detectGPUFromLib(); info.Found {
		return info
	}

	return GPUInfo{}
}

// detectGPUFromSMI runs nvidia-smi to query GPU information.
func detectGPUFromSMI() GPUInfo {
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=name,driver_version",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return GPUInfo{}
	}

	line := strings.TrimSpace(string(out))
	parts := strings.SplitN(line, ",", 2)
	if len(parts) < 1 || strings.TrimSpace(parts[0]) == "" {
		return GPUInfo{}
	}

	name := strings.TrimSpace(parts[0])
	driverVer := ""
	if len(parts) >= 2 {
		driverVer = strings.TrimSpace(parts[1])
	}

	cudaVer := driverToCUDAVersion(driverVer)
	return GPUInfo{
		Found:       true,
		Name:        name,
		DriverVer:   driverVer,
		CUDAVersion: cudaVer,
	}
}

// detectGPUFromProc checks /proc/driver/nvidia/version for the NVIDIA driver.
func detectGPUFromProc() GPUInfo {
	data, err := os.ReadFile("/proc/driver/nvidia/version")
	if err != nil {
		return GPUInfo{}
	}

	content := string(data)
	// Example line: "NVRM version: NVIDIA UNIX x86_64 Kernel Module  560.94  ..."
	// Try to extract the driver version
	fields := strings.Fields(content)
	driverVer := ""
	for i, f := range fields {
		if f == "Module" && i+1 < len(fields) {
			driverVer = fields[i+1]
			break
		}
	}

	if driverVer == "" {
		// Try simpler: look for a version-like pattern
		for _, f := range fields {
			if strings.Contains(f, ".") && len(f) >= 5 {
				driverVer = f
				break
			}
		}
	}

	cudaVer := driverToCUDAVersion(driverVer)
	return GPUInfo{
		Found:       true,
		Name:        "NVIDIA GPU",
		DriverVer:   driverVer,
		CUDAVersion: cudaVer,
	}
}

// detectGPUFromLib checks for libcuda.so presence.
func detectGPUFromLib() GPUInfo {
	paths := []string{
		"/usr/lib/x86_64-linux-gnu/libcuda.so",
		"/usr/lib64/libcuda.so",
		"/usr/lib/libcuda.so",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return GPUInfo{
				Found:       true,
				Name:        "NVIDIA GPU",
				DriverVer:   "",
				CUDAVersion: "12.x",
			}
		}
	}
	return GPUInfo{}
}
