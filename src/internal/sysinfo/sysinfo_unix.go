//go:build linux || darwin

package sysinfo

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func Collect() (*SysInfo, error) {
	info := &SysInfo{}
	info.OS = getOSInfo()
	info.CPU = getCPUInfo()
	info.Memory = getMemoryStat()
	info.GPUs = listGPUsUnix()
	info.Disks = listDisksUnix()
	info.Runtime = getRuntimeInfo()
	return info, nil
}

func getMemoryStat() MemoryStat {
	mi := getMemoryInfo()
	return MemoryStat{TotalGB: mi.TotalGB}
}

func listGPUsUnix() []GPUInfo {
	name := getGPUInfo().Name
	vendor, backend := detectGPUVendor(name)
	return []GPUInfo{{
		Index: 0, Name: name, Vendor: vendor, Backend: backend,
		Features: gpuFeaturesFor(vendor),
	}}
}

// detectGPUVendor / gpuFeaturesFor / containsAny 在 sysinfo_windows.go 同名定义。
// Unix 版本需要自己的实现（因为 Windows 版本在 windows build tag 下）。
func detectGPUVendor(name string) (vendor, backend string) {
	low := strings.ToLower(name)
	switch {
	case strings.Contains(low, "nvidia") || strings.Contains(low, "geforce") || strings.Contains(low, "rtx"):
		return "nvidia", "cuda"
	case strings.Contains(low, "amd") || strings.Contains(low, "radeon") || strings.Contains(low, "vega"):
		return "amd", "rocm"
	case strings.Contains(low, "intel") || strings.Contains(low, "arc") || strings.Contains(low, "iris"):
		return "intel", "oneapi"
	}
	return "other", "none"
}

func gpuFeaturesFor(vendor string) []Capability {
	switch vendor {
	case "nvidia":
		return []Capability{{Key: "cuda", Label: "CUDA", Available: true}}
	case "amd":
		return []Capability{{Key: "rocm", Label: "ROCm", Available: true}}
	case "intel":
		return []Capability{{Key: "openvino", Label: "OpenVINO", Available: true}}
	}
	return []Capability{{Key: "cpu", Label: "CPU", Available: true}}
}

func listDisksUnix() []DiskInfo {
	d := getDiskInfo()
	return []DiskInfo{{Drive: d.Drive, TotalGB: d.TotalGB}}
}

func getOSInfo() OSInfo {
	return OSInfo{
		Name:    runtime.GOOS,
		Arch:    runtime.GOARCH,
		Version: getOSVersion(),
	}
}

func getRuntimeInfo() RuntimeInfo {
	return RuntimeInfo{
		GoVersion: runtime.Version(),
		GoArch:    runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
	}
}

func getOSVersion() string {
	switch runtime.GOOS {
	case "linux":
		return readFirstLine("/etc/os-release")
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err == nil {
			return "macOS " + strings.TrimSpace(string(out))
		}
		return "macOS"
	}
	return runtime.GOOS
}

func getCPUInfo() CPUInfo {
	info := CPUInfo{}
	info.Cores = runtime.NumCPU()
	info.Threads = runtime.NumCPU()

	vendorStr := cpuVendor()
	switch {
	case strings.HasPrefix(strings.ToLower(vendorStr), "genuineintel"):
		info.Vendor = "intel"
	case strings.HasPrefix(strings.ToLower(vendorStr), "authenticamd"):
		info.Vendor = "amd"
	default:
		info.Vendor = "other"
	}
	info.Features = detectCPUFeatures(info.Vendor)

	switch runtime.GOOS {
	case "linux":
		info.Name = readCPUNameLinux()
		info.Cores = readPhysicalCoresLinux()
	case "darwin":
		info.Name = readCPUNameDarwin()
		info.Cores = readPhysicalCoresDarwin()
	}
	if info.Name == "" {
		info.Name = "Unknown CPU"
	}
	// AVX detection on Unix
	info.AVX, info.AVX2 = checkAVX()
	return info
}

func checkAVX() (bool, bool) {
	// Use CPUID — works on x86 Linux and macOS too
	maxLeaf, _, _, _ := cpuidImpl(0, 0)
	if maxLeaf < 1 {
		return false, false
	}
	_, _, cx, _ := cpuidImpl(1, 0)
	avx := cx&(1<<28) != 0
	avx2 := false
	if maxLeaf >= 7 {
		_, ebx, _, _ := cpuidImpl(7, 0)
		avx2 = ebx&(1<<5) != 0
	}
	return avx, avx2
}

func getMemoryInfo() MemoryStat {
	switch runtime.GOOS {
	case "linux":
		return readMemLinux()
	case "darwin":
		return readMemDarwin()
	}
	return MemoryStat{}
}

func getGPUInfo() GPUInfo {
	info := GPUInfo{}
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("lspci", "-mm").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "VGA") || strings.Contains(line, "3D") || strings.Contains(line, "Display") {
					info.Name = strings.TrimSpace(line)
					break
				}
			}
		}
	case "darwin":
		out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Chipset Model:") {
					info.Name = strings.TrimSpace(strings.TrimPrefix(line, "Chipset Model:"))
					break
				}
			}
		}
	}
	return info
}

func getDiskInfo() DiskInfo {
	info := DiskInfo{}
	wd, _ := os.Getwd()
	info.Drive = wd
	out, err := exec.Command("df", "-k", wd).Output()
	if err != nil {
		return info
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) >= 2 {
		fields := strings.Fields(lines[1])
		if len(fields) >= 2 {
			totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
			info.TotalGB = round(float64(totalKB)/(1024*1024), 1)
		}
	}
	return info
}

func readFirstLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
}

func readCPUNameLinux() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func readCPUNameDarwin() string {
	out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func readPhysicalCoresLinux() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return runtime.NumCPU()
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "physical id") || strings.HasPrefix(line, "core id") {
			seen[strings.TrimSpace(line)] = true
		}
	}
	if len(seen) > 0 {
		return len(seen)/2 + 1
	}
	return runtime.NumCPU()
}

func readPhysicalCoresDarwin() int {
	out, err := exec.Command("sysctl", "-n", "hw.physicalcpu").Output()
	if err == nil {
		n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		if n > 0 {
			return n
		}
	}
	return runtime.NumCPU()
}

func readMemLinux() MemoryStat {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemoryStat{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
				return MemoryStat{TotalGB: round(float64(totalKB)/(1024*1024), 1)}
			}
		}
	}
	return MemoryStat{}
}

func readMemDarwin() MemoryStat {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return MemoryStat{}
	}
	totalBytes, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	return MemoryStat{TotalGB: round(float64(totalBytes)/(1024*1024*1024), 1)}
}
