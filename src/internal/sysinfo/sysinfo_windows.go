package sysinfo

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// Collect 采集全部系统信息。
func Collect() (*SysInfo, error) {
	info := &SysInfo{}
	info.OS = getOSInfo()
	info.CPU = getCPUInfo()
	info.Memory = getMemoryStat()
	info.GPUs = listGPUsStatic()
	info.Disks = listDisksStatic()
	info.Runtime = getRuntimeInfo()
	return info, nil
}

func getOSInfo() OSInfo {
	return OSInfo{
		Name:    "Windows",
		Arch:    runtime.GOARCH,
		Version: getWindowsVersion(),
	}
}

func getRuntimeInfo() RuntimeInfo {
	return RuntimeInfo{
		GoVersion: runtime.Version(),
		GoArch:    runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
	}
}

// ─── Windows 版本 ────────────────────────────────────────────

func getWindowsVersion() string {
	key, _ := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`)
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, key, 0, syscall.KEY_READ, &h); err != nil {
		return "Unknown"
	}
	defer syscall.RegCloseKey(h)

	getStr := func(name string) string {
		var buf [256]uint16
		n := uint32(len(buf))
		p, _ := syscall.UTF16PtrFromString(name)
		if err := syscall.RegQueryValueEx(h, p, nil, nil, (*byte)(unsafe.Pointer(&buf[0])), &n); err != nil {
			return ""
		}
		return syscall.UTF16ToString(buf[:n/2-1])
	}

	product := getStr("ProductName")
	build := getStr("CurrentBuildNumber")
	ubr := getStr("UBR")

	if product == "" {
		major, minor := getWindowsMajorMinor()
		product = fmt.Sprintf("Windows %d.%d", major, minor)
	}
	if build != "" {
		product += " Build " + build
	}
	if ubr != "" {
		product += "." + ubr
	}
	return product
}

func getWindowsMajorMinor() (uint32, uint32) {
	type osVersionInfoEx struct {
		osVersionInfoSize uint32
		majorVersion      uint32
		minorVersion      uint32
		buildNumber       uint32
		platformId        uint32
		csdVersion        [128]uint16
		servicePackMajor  uint16
		servicePackMinor  uint16
		suiteMask         uint16
		productType       byte
		reserved          byte
	}
	var ovi osVersionInfoEx
	ovi.osVersionInfoSize = uint32(unsafe.Sizeof(ovi))
	syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(
		uintptr(unsafe.Pointer(&ovi)),
	)
	return ovi.majorVersion, ovi.minorVersion
}

// ─── CPU ────────────────────────────────────────────────────

func getCPUInfo() CPUInfo {
	info := CPUInfo{}
	info.Name = getCPUName()
	info.Cores, info.Threads = getCPUCores()

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

	if info.Name == "" {
		info.Name = "Unknown CPU"
	}
	if info.Cores == 0 {
		info.Cores = runtime.NumCPU()
	}
	if info.Threads == 0 {
		info.Threads = runtime.NumCPU()
	}
	return info
}

func getCPUName() string {
	key, _ := syscall.UTF16PtrFromString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`)
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, key, 0, syscall.KEY_READ, &h); err != nil {
		return ""
	}
	defer syscall.RegCloseKey(h)

	var buf [256]uint16
	n := uint32(len(buf))
	pn, _ := syscall.UTF16PtrFromString("ProcessorNameString")
	if err := syscall.RegQueryValueEx(h, pn, nil, nil, (*byte)(unsafe.Pointer(&buf[0])), &n); err != nil {
		return ""
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf[:n/2-1]))
}

func getCPUCores() (int, int) {
	type systemInfo struct {
		processorArchitecture uint16
		reserved              uint16
		pageSize              uint32
		minAppAddress         uintptr
		maxAppAddress         uintptr
		activeProcessorMask   uintptr
		numberOfProcessors    uint32
		processorType         uint32
		allocationGranularity uint32
		processorLevel        uint16
		processorRevision     uint16
	}
	var si systemInfo
	syscall.NewLazyDLL("kernel32.dll").NewProc("GetNativeSystemInfo").Call(
		uintptr(unsafe.Pointer(&si)),
	)
	logicalCores := int(si.numberOfProcessors)

	// GetLogicalProcessorInformation for physical cores
	type slpi struct {
		processorMask uintptr
		relationship  uint32
		_             [20]byte // union placeholder
	}
	physCores := logicalCores
	var buf []byte
	bufLen := uint32(0)
	k32 := syscall.NewLazyDLL("kernel32.dll")
	glpi := k32.NewProc("GetLogicalProcessorInformation")
	glpi.Call(0, 0, uintptr(unsafe.Pointer(&bufLen)))
	if bufLen > 0 {
		buf = make([]byte, bufLen)
		ret, _, _ := glpi.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bufLen)))
		if ret != 0 {
			physCores = 0
			entrySize := uint32(unsafe.Sizeof(slpi{}))
			for i := uint32(0); i < bufLen; {
				entry := (*slpi)(unsafe.Pointer(&buf[i]))
				if entry.relationship == 0 { // RelationProcessorCore
					physCores++
				}
				i += entrySize
			}
		}
	}
	if physCores == 0 {
		physCores = logicalCores
	}
	return physCores, logicalCores
}

// ─── 内存 ──────────────────────────────────────────────────

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

func getMemoryStat() MemoryStat {
	var m memoryStatusEx
	m.length = uint32(unsafe.Sizeof(m))
	syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx").Call(
		uintptr(unsafe.Pointer(&m)),
	)
	totalGB := float64(m.totalPhys) / (1024 * 1024 * 1024)
	return MemoryStat{TotalGB: round(totalGB, 1)}
}

// listDisksStatic 返回所有固定/远程磁盘的总量信息。
func listDisksStatic() []DiskInfo {
	var result []DiskInfo
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + ":"
		root, _ := syscall.UTF16PtrFromString(drive + `\`)
		ret, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW").Call(uintptr(unsafe.Pointer(root)))
		// 3=FIXED 4=REMOTE
		if ret != 3 && ret != 4 {
			continue
		}
		var freeBytes, totalBytes, availBytes uint64
		ok, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW").Call(
			uintptr(unsafe.Pointer(root)),
			uintptr(unsafe.Pointer(&freeBytes)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&availBytes)),
		)
		if ok == 0 || totalBytes == 0 {
			continue
		}
		result = append(result, DiskInfo{
			Drive:   drive,
			TotalGB: round(float64(totalBytes)/(1024*1024*1024), 1),
		})
	}
	return result
}

// powershellHidden 静默执行 PowerShell 脚本并返回 stdout。
func powershellHidden(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	return string(out), err
}
