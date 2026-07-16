//go:build linux || darwin

package sysinfo

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	prevCPUTotal uint64
	prevCPUIdle  uint64
	mu           sync.Mutex
)

func CollectDynamic() *DynamicInfo {
	info := &DynamicInfo{}

	// CPU — 读 /proc/stat (Linux) 或 top -l (macOS)
	info.CPUPercent = sampleCPU()

	// 内存
	info.MemoryUsedGB, info.MemoryTotalGB, info.MemoryPercent = sampleMemory()

	// 磁盘
	info.DiskFreeGB, info.DiskTotalGB, info.DiskPercent = sampleDisk()

	return info
}

func sampleCPU() int {
	switch runtime.GOOS {
	case "linux":
		return sampleCPULinux()
	case "darwin":
		return sampleCPUDarwin()
	}
	return 0
}

func sampleCPULinux() int {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0
	}
	fields := strings.Fields(lines[0]) // cpu  user nice system idle ...
	if len(fields) < 5 {
		return 0
	}

	// user + nice + system + idle + iowait + irq + softirq
	nums := make([]uint64, len(fields)-1)
	total := uint64(0)
	for i, f := range fields[1:] {
		nums[i], _ = strconv.ParseUint(f, 10, 64)
		total += nums[i]
	}
	idle := nums[3] + nums[4] // idle + iowait

	mu.Lock()
	pt := prevCPUTotal
	pi := prevCPUIdle
	prevCPUTotal = total
	prevCPUIdle = idle
	mu.Unlock()

	if pt == 0 {
		return 0
	}

	totalDelta := total - pt
	idleDelta := idle - pi
	if totalDelta == 0 {
		return 0
	}
	cpu := 100 - int(idleDelta*100/totalDelta)
	if cpu < 0 {
		cpu = 0
	}
	if cpu > 100 {
		cpu = 100
	}
	return cpu
}

func sampleCPUDarwin() int {
	out, err := exec.Command("top", "-l", "1", "-n", "0").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "CPU usage") {
			// "CPU usage: 5.12% user, 3.33% sys, 91.54% idle"
			for _, part := range strings.Split(line, ",") {
				if strings.Contains(part, "idle") {
					f := strings.TrimSpace(strings.Split(part, "%")[0])
					idle, _ := strconv.ParseFloat(f, 64)
					return int(100 - idle)
				}
			}
		}
	}
	return 0
}

func sampleMemory() (float64, float64, int) {
	switch runtime.GOOS {
	case "linux":
		return sampleMemLinux()
	case "darwin":
		return sampleMemDarwin()
	}
	return 0, 0, 0
}

func sampleDisk() (float64, float64, int) {
	wd, _ := os.Getwd()
	out, err := exec.Command("df", "-k", wd).Output()
	if err != nil {
		return 0, 0, 0
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) >= 2 {
		fields := strings.Fields(lines[1])
		if len(fields) >= 4 {
			totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
			usedKB, _ := strconv.ParseUint(fields[2], 10, 64)
			availKB, _ := strconv.ParseUint(fields[3], 10, 64)
			totalGB := round(float64(totalKB)/(1024*1024), 1)
			freeGB := round(float64(availKB)/(1024*1024), 1)
			pct := 0
			if totalKB > 0 {
				pct = int(usedKB * 100 / totalKB)
			}
			return freeGB, totalGB, pct
		}
	}
	return 0, 0, 0
}

func init() {
	// 第一次采样预热（避免返回 0）
	CollectDynamic()
	time.Sleep(500 * time.Millisecond)
}
