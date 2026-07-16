package backends

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// loadSymbol tries LoadLibrary + GetProcAddress to check DLL availability.
func loadSymbol(dllPath, symbol string) (bool, error) {
	handle, err := syscall.LoadLibrary(dllPath)
	if err != nil {
		return false, err
	}
	defer syscall.FreeLibrary(handle)

	proc, err := syscall.GetProcAddress(handle, symbol)
	if err != nil || proc == 0 {
		return false, fmt.Errorf("symbol %s 不存在", symbol)
	}
	return true, nil
}

// findInPath fallback: uses where command to locate DLL in PATH.
func findInPath(pattern string) (string, error) {
	if strings.Contains(pattern, "*") {
		return "", fmt.Errorf("不支持 PATH 通配搜索")
	}
	cmd := exec.Command("where", pattern)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		return strings.TrimSpace(lines[0]), nil
	}
	return "", fmt.Errorf("未找到")
}

// ─── GPU Detection ───────────────────────────────────────────────

// displayAdapterGUID is the device class GUID for display adapters.
const displayAdapterGUID = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`

// detectGPU checks for an NVIDIA GPU on Windows.
// Uses registry to enumerate display adapters and find NVIDIA vendor.
func detectGPU() GPUInfo {
	// Try registry first (fast, no external process)
	if info := detectGPUFromRegistry(); info.Found {
		return info
	}

	// Fallback: try nvidia-smi
	if info := detectGPUFromSMI(); info.Found {
		return info
	}

	// Last resort: check if nvcuda.dll exists (NVIDIA driver DLL)
	if info := detectGPUFromDLL(); info.Found {
		return info
	}

	return GPUInfo{}
}

// detectGPUFromRegistry enumerates the display adapter class in the registry,
// looking for an NVIDIA adapter. Returns GPU name and driver version.
func detectGPUFromRegistry() GPUInfo {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, displayAdapterGUID, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return GPUInfo{}
	}
	defer k.Close()

	subkeys, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return GPUInfo{}
	}

	for _, sk := range subkeys {
		// Only check numbered subkeys (0000, 0001, ...)
		if _, err := strconv.Atoi(sk); err != nil {
			continue
		}

		adapter, err := registry.OpenKey(k, sk, registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		provider, _, _ := adapter.GetStringValue("ProviderName")
		if !strings.Contains(strings.ToLower(provider), "nvidia") {
			adapter.Close()
			continue
		}

		desc, _, _ := adapter.GetStringValue("DriverDesc")
		driverVer, _, _ := adapter.GetStringValue("DriverVersion")
		adapter.Close()

		if desc == "" {
			continue
		}

		nvVer, cudaVer := parseNVIDIAVersion(driverVer)
		return GPUInfo{
			Found:       true,
			Name:        desc,
			DriverVer:   nvVer,
			CUDAVersion: cudaVer,
		}
	}

	return GPUInfo{}
}

// detectGPUFromSMI runs nvidia-smi to query GPU information.
func detectGPUFromSMI() GPUInfo {
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=name,driver_version",
		"--format=csv,noheader,nounits")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
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

	nvVer, cudaVer := parseNVIDIAVersionFromDriver(driverVer)
	return GPUInfo{
		Found:       true,
		Name:        name,
		DriverVer:   nvVer,
		CUDAVersion: cudaVer,
	}
}

// detectGPUFromDLL checks for nvcuda.dll in System32 as proof of NVIDIA driver.
func detectGPUFromDLL() GPUInfo {
	sys32 := filepath.Join(os.Getenv("SystemRoot"), "System32", "nvcuda.dll")
	if _, err := os.Stat(sys32); os.IsNotExist(err) {
		return GPUInfo{}
	}

	verStr := getFileVersion(sys32)
	nvVer, cudaVer := parseNVIDIAVersion(verStr)
	return GPUInfo{
		Found:       true,
		Name:        "NVIDIA GPU",
		DriverVer:   nvVer,
		CUDAVersion: cudaVer,
	}
}

// ─── Version Parsing ─────────────────────────────────────────────

// parseNVIDIAVersion converts a Windows 4-part driver version string
// (e.g., "32.0.15.6094") to NVIDIA driver version ("560.94") and
// the max supported CUDA version ("12.4").
func parseNVIDIAVersion(winVer string) (nvVer string, cudaVer string) {
	if winVer == "" {
		return "", ""
	}

	parts := strings.Split(winVer, ".")
	if len(parts) != 4 {
		// Maybe it's already a driver version like "560.94"
		if len(parts) == 2 {
			return winVer, driverToCUDAVersion(winVer)
		}
		return "", ""
	}

	c, err1 := strconv.Atoi(parts[2])
	d, err2 := strconv.Atoi(parts[3])
	if err1 != nil || err2 != nil {
		return "", ""
	}

	// NVIDIA driver version from Windows version:
	//   (C % 10) * 100 + D / 100 gives the major version
	//   D % 100 gives the minor version
	// e.g., 32.0.15.6094 → major=560, minor=94 → "560.94"
	//       31.0.15.3598 → major=535, minor=98 → "535.98"
	major := (c%10)*100 + d/100
	minor := d % 100
	nvVer = fmt.Sprintf("%d.%02d", major, minor)
	cudaVer = driverToCUDAVersion(fmt.Sprintf("%d", major))
	return
}

// parseNVIDIAVersionFromDriver handles a direct driver version like "560.94".
func parseNVIDIAVersionFromDriver(driverVer string) (nvVer string, cudaVer string) {
	if driverVer == "" {
		return "", ""
	}
	cudaVer = driverToCUDAVersion(driverVer)
	return driverVer, cudaVer
}

// driverToCUDAVersion maps an NVIDIA driver version to max supported CUDA version.
// Reference: https://docs.nvidia.com/cuda/cuda-toolkit-release-notes/
func driverToCUDAVersion(nvVer string) string {
	// Parse the major.minor or just major from the driver version
	nvVer = strings.TrimSpace(nvVer)
	dotIdx := strings.Index(nvVer, ".")
	var major int
	if dotIdx > 0 {
		major, _ = strconv.Atoi(nvVer[:dotIdx])
	} else {
		major, _ = strconv.Atoi(nvVer)
	}

	switch {
	case major >= 575:
		return "12.8"
	case major >= 570:
		return "12.7"
	case major >= 560:
		return "12.6"
	case major >= 555:
		return "12.5"
	case major >= 550:
		return "12.4"
	case major >= 545:
		return "12.3"
	case major >= 535:
		return "12.2"
	case major >= 530:
		return "12.1"
	case major >= 525:
		return "12.0"
	case major >= 520:
		return "11.8"
	case major >= 515:
		return "11.7"
	case major >= 510:
		return "11.6"
	case major >= 470:
		return "11.4"
	case major >= 450:
		return "11.0"
	default:
		return "11.x"
	}
}

// getFileVersion reads the ProductVersion or FileVersion from a PE/DLL file.
func getFileVersion(path string) string {
	var handle windows.Handle
	size, err := windows.GetFileVersionInfoSize(path, &handle)
	if err != nil || size == 0 {
		return ""
	}

	data := make([]byte, size)
	err = windows.GetFileVersionInfo(path, uint32(handle), size, unsafe.Pointer(&data[0]))
	if err != nil {
		return ""
	}

	// Query \StringFileInfo\040904B0\ProductVersion (US English code page)
	subBlock := `\StringFileInfo\040904B0\ProductVersion`
	var bufPtr unsafe.Pointer
	var bufLen uint32
	err = windows.VerQueryValue(unsafe.Pointer(&data[0]), subBlock, unsafe.Pointer(&bufPtr), &bufLen)
	if err != nil || bufLen == 0 {
		// Fallback: try FileVersion
		subBlock = `\StringFileInfo\040904B0\FileVersion`
		err = windows.VerQueryValue(unsafe.Pointer(&data[0]), subBlock, unsafe.Pointer(&bufPtr), &bufLen)
		if err != nil || bufLen == 0 {
			return ""
		}
	}

	// bufPtr now points to a NUL-terminated UTF-16 string
	return windows.UTF16PtrToString((*uint16)(bufPtr))
}
