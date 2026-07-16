package sysinfo

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	prevIdleTime   uint64
	prevKernelTime uint64
	prevUserTime   uint64
	cpuMu          sync.Mutex

	smiMu       sync.Mutex
	smiCache    *smiResult
	smiCacheT   time.Time
)

type smiResult struct {
	statics  []GPUInfo
	dynamics []GPUDynamic
	ok       bool
}

// CollectDynamic 采集动态指标。CPU/内存/磁盘走 syscall (微秒级)，
// GPU 走 nvidia-smi 或 Get-Counter，2 秒缓存。
func CollectDynamic() *DynamicInfo {
	info := &DynamicInfo{}
	info.CPUPercent = sampleCPU()
	info.MemoryUsedGB, info.MemoryTotalGB, info.MemoryPercent = sampleMemory()
	info.Disks = sampleAllDisks()
	info.PhysicalDisks = samplePhysicalDisks()
	info.GPUs = sampleGPUDynamic()
	return info
}

func sampleCPU() int {
	var idle, kernel, user syscall.Filetime
	k32 := syscall.NewLazyDLL("kernel32.dll")
	ret, _, _ := k32.NewProc("GetSystemTimes").Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ret == 0 {
		return 0
	}
	nowIdle := ftToUint64(idle)
	nowKernel := ftToUint64(kernel)
	nowUser := ftToUint64(user)
	nowTotal := nowKernel + nowUser

	cpuMu.Lock()
	pIdle := prevIdleTime
	pTotal := prevKernelTime + prevUserTime
	prevIdleTime = nowIdle
	prevKernelTime = nowKernel
	prevUserTime = nowUser
	cpuMu.Unlock()

	if pTotal == 0 {
		return 0
	}
	idleDelta := nowIdle - pIdle
	totalDelta := nowTotal - pTotal
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

func sampleMemory() (float64, float64, int) {
	type memStatEx struct {
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
	var m memStatEx
	m.length = uint32(unsafe.Sizeof(m))
	syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx").Call(
		uintptr(unsafe.Pointer(&m)),
	)
	totalGB := float64(m.totalPhys) / (1024 * 1024 * 1024)
	usedGB := float64(m.totalPhys-m.availPhys) / (1024 * 1024 * 1024)
	return round(usedGB, 1), round(totalGB, 1), int(m.memoryLoad)
}

func sampleAllDisks() []DiskDynamic {
	var result []DiskDynamic
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + ":"
		root, _ := syscall.UTF16PtrFromString(drive + `\`)
		ret, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW").Call(uintptr(unsafe.Pointer(root)))
		if ret != 3 && ret != 4 { // 3=FIXED 4=REMOTE
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
		totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
		freeGB := float64(freeBytes) / (1024 * 1024 * 1024)
		result = append(result, DiskDynamic{
			Drive:   drive,
			FreeGB:  round(freeGB, 1),
			TotalGB: round(totalGB, 1),
			Percent: int((totalBytes - freeBytes) * 100 / totalBytes),
		})
	}
	return result
}

// samplePhysicalDisks groups drive letters under their physical disk.
func samplePhysicalDisks() []PhysicalDiskDynamic {
	ps := `
Get-PhysicalDisk | ForEach-Object {
  $pd = $_
  $diskNum = $pd.DeviceID
  $vols = @(Get-Partition -DiskNumber $diskNum -ErrorAction SilentlyContinue |
    Get-Volume -ErrorAction SilentlyContinue |
    Where-Object { $_.DriveLetter } |
    ForEach-Object {
      $size = $_.Size
      $free = $_.SizeRemaining
      if ($size -gt 0) {
        $pct = [int](($size - $free) * 100 / $size)
      } else { $pct = 0 }
      "$($_.DriveLetter):|$([math]::Round($free/1GB,1))|$([math]::Round($size/1GB,1))|$pct"
    })
  if ($vols.Count -gt 0) {
    $sizeGB = [math]::Round($pd.Size/1GB, 1)
    $model = $pd.FriendlyName -replace '\|','/'
    "DISK|$model|$sizeGB"
    $vols | ForEach-Object { $_ }
    "ENDDISK"
  }
}
`
	out, _ := powershellHidden(ps)
	var result []PhysicalDiskDynamic
	var cur *PhysicalDiskDynamic

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "DISK|") {
			parts := strings.SplitN(line, "|", 3)
			if len(parts) >= 3 {
				result = append(result, PhysicalDiskDynamic{
					Model: strings.TrimSpace(parts[1]),
					Name:  strings.TrimSpace(parts[1]),
				})
				cur = &result[len(result)-1]
				cur.SizeGB = parseFloat(strings.TrimSpace(parts[2]))
			}
			continue
		}
		if line == "ENDDISK" {
			cur = nil
			continue
		}
		if cur != nil {
			// Volume line: "C:|234.5|476.9|51"
			parts := strings.SplitN(line, "|", 4)
			if len(parts) >= 4 {
				cur.Volumes = append(cur.Volumes, DiskDynamic{
					Drive:   strings.TrimSpace(parts[0]),
					FreeGB:  parseFloat(strings.TrimSpace(parts[1])),
					TotalGB: parseFloat(strings.TrimSpace(parts[2])),
					Percent: atoiSafe(strings.TrimSpace(parts[3])),
				})
			}
		}
	}
	return result
}

func parseFloat(s string) float64 {
	n := 0.0
	fmt.Sscanf(strings.TrimSpace(s), "%f", &n)
	return n
}

// ─── GPU ─────────────────────────────────────────────────────

// sampleGPUDynamic 返回 GPU 实时指标。优先 nvidia-smi，回退 Get-Counter。
func sampleGPUDynamic() []GPUDynamic {
	if r := querySmiCached(2 * time.Second); r.ok {
		return r.dynamics
	}
	return sampleGPUViaCounter()
}

// listGPUsStatic 返回 GPU 静态信息。优先 nvidia-smi（显存准确），回退 WMI。
func listGPUsStatic() []GPUInfo {
	if r := querySmiCached(10 * time.Second); r.ok {
		return r.statics
	}
	return listGPUsViaWMI()
}

func querySmiCached(maxAge time.Duration) *smiResult {
	smiMu.Lock()
	defer smiMu.Unlock()
	if smiCache != nil && smiCache.ok && time.Since(smiCacheT) < maxAge {
		return smiCache
	}
	r := queryNvidiaSMI()
	if r.ok {
		smiCache = &r
		smiCacheT = time.Now()
	}
	return &r
}

// queryNvidiaSMI 调用 nvidia-smi 获取全部 GPU 的静态+动态数据。
// 多级降级覆盖不同驱动版本：名词单位(nounits)、compute_cap 都是较新参数。
func queryNvidiaSMI() smiResult {
	queries := [][]string{
		// ① 最新驱动：完整字段 + nounits
		{"--query-gpu=name,memory.total,memory.used,utilization.gpu,driver_version,cuda_version,compute_cap",
			"--format=csv,noheader,nounits"},
		// ② 旧驱动：无 compute_cap，仍有 nounits
		{"--query-gpu=name,memory.total,memory.used,utilization.gpu,driver_version,cuda_version",
			"--format=csv,noheader,nounits"},
		// ③ 更旧驱动：无 nounits（值带单位后缀），atoiSafe 能容忍
		{"--query-gpu=name,memory.total,memory.used,utilization.gpu,driver_version,cuda_version",
			"--format=csv,noheader"},
		// ④ 最旧驱动：最小字段集，无任何 format 选项
		{"--query-gpu=name,memory.total,memory.used,utilization.gpu",
			"--format=csv,noheader"},
	}
	var out string
	var ok bool
	for _, args := range queries {
		out, ok = tryNvidiaSMI(args...)
		if ok {
			break
		}
	}
	if !ok {
		return smiResult{}
	}
	var statics []GPUInfo
	var dynamics []GPUDynamic
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		for j := range fields {
			fields[j] = strings.TrimSpace(fields[j])
		}
		if len(fields) < 4 {
			continue
		}
		name := fields[0]
		totalMB := atoiSafe(fields[1])
		usedMB := atoiSafe(fields[2])
		util := atoiSafe(fields[3])
		driver := ""
		cudaVer := ""
		computeCap := ""
		if len(fields) > 4 {
			driver = fields[4]
		}
		if len(fields) > 5 {
			cudaVer = fields[5]
		}
		if len(fields) > 6 {
			computeCap = fields[6]
		}

		feats := nvidiaFeatures(computeCap)
		statics = append(statics, GPUInfo{
			Index:      i,
			Name:       name,
			Vendor:     "nvidia",
			Backend:    "cuda",
			Usable:     true, // nvidia-smi 成功即可用
			VRAMMB:     totalMB,
			Driver:     driver,
			CudaVer:    cudaVer,
			ComputeCap: computeCap,
			Features:   feats,
		})
		dynamics = append(dynamics, GPUDynamic{Index: i, UtilPercent: util, VRAMUsedMB: usedMB, VRAMTotalMB: totalMB})
	}
	if len(statics) == 0 {
		return smiResult{}
	}
	return smiResult{statics: statics, dynamics: dynamics, ok: true}
}

// nvidiaFeatures 生成 NVIDIA 能力标签。compute_cap>=6 有 Pascal+, >=7 Tensor Cores。
func nvidiaFeatures(computeCap string) []Capability {
	caps := []Capability{
		{Key: "cuda", Label: "CUDA", Available: true},
	}
	// Tensor Cores: compute capability >= 7.0 (Volta+)
	if maj := majorCompute(computeCap); maj >= 7 {
		caps = append(caps, Capability{Key: "tensor_cores", Label: "Tensor Cores", Available: true})
	}
	// 检测库文件
	caps = append(caps, Capability{Key: "cudnn", Label: "cuDNN", Available: dllExists("cudnn64_")})
	caps = append(caps, Capability{Key: "tensorrt", Label: "TensorRT", Available: dllExists("nvinfer")})
	caps = append(caps, Capability{Key: "nvenc", Label: "NVENC", Available: true})  // NVIDIA 卡普遍支持
	caps = append(caps, Capability{Key: "nvdec", Label: "NVDEC", Available: true})
	return caps
}

func majorCompute(cc string) int {
	// "8.6" -> 8
	for _, ch := range cc {
		if ch >= '0' && ch <= '9' {
			return int(ch - '0')
		}
		if ch == '.' {
			break
		}
	}
	return 0
}

// dllExists 检查 System32 下是否存在匹配前缀的 DLL。
func dllExists(prefix string) bool {
	entries, err := os.ReadDir(`C:\Windows\System32`)
	if err != nil {
		return false
	}
	lower := strings.ToLower(prefix)
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if strings.HasPrefix(name, lower) && strings.HasSuffix(name, ".dll") {
			return true
		}
	}
	return false
}

// tryNvidiaSMI 尝试多个路径调用 nvidia-smi。
func tryNvidiaSMI(args ...string) (string, bool) {
	paths := []string{
		"nvidia-smi",
		`C:\Windows\System32\nvidia-smi.exe`,
		`C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`,
		`C:\Program Files (x86)\NVIDIA Corporation\NVSMI\nvidia-smi.exe`,
	}
	for _, p := range paths {
		cmd := exec.Command(p, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			return string(out), true
		}
	}
	return "", false
}

// sampleGPUViaCounter 回退方案：Get-Counter 性能计数器，5 秒缓存。
func sampleGPUViaCounter() []GPUDynamic {
	statics := listGPUsViaWMI()
	util, used := cachedGPUMetrics()
	result := make([]GPUDynamic, len(statics))
	for i, g := range statics {
		u := used
		if i > 0 {
			u = 0
		}
		result[i] = GPUDynamic{Index: i, UtilPercent: util, VRAMUsedMB: u, VRAMTotalMB: g.VRAMMB}
	}
	return result
}

var (
	cntMu     sync.Mutex
	cntCacheT time.Time
	cntUtil   int
	cntVRAM   int
)

func cachedGPUMetrics() (int, int) {
	cntMu.Lock()
	defer cntMu.Unlock()
	if !cntCacheT.IsZero() && time.Since(cntCacheT) < 5*time.Second {
		return cntUtil, cntVRAM
	}
	util, vram := queryGPUCounter()
	cntUtil, cntVRAM = util, vram
	cntCacheT = time.Now()
	return util, vram
}

func queryGPUCounter() (int, int) {
	ps := `
$util = 0; $mem = 0
try {
  $u = (Get-Counter '\GPU Engine(*)\Utilization Percentage' -ErrorAction Stop).CounterSamples |
       Where-Object { $_.CookedValue -gt 0 -and $_.InstanceName -notmatch '^pid_' } |
       Measure-Object -Property CookedValue -Maximum
  if ($u.Maximum) { $util = [int]$u.Maximum }
} catch {}
try {
  $d = (Get-Counter '\GPU Adapter Memory(*)\Dedicated Usage' -ErrorAction Stop).CounterSamples |
       Where-Object { $_.InstanceName -notmatch '^pid_' }
  $mem = [int](($d | Measure-Object -Property CookedValue -Sum).Sum / 1MB)
} catch {}
"$util|$mem"
`
	out, _ := powershellHidden(ps)
	out = strings.TrimSpace(out)
	parts := strings.SplitN(out, "|", 2)
	util, vram := 0, 0
	if len(parts) > 0 {
		util = clamp0to100(atoiSafe(parts[0]))
	}
	if len(parts) > 1 {
		vram = atoiSafe(parts[1])
		if vram < 0 {
			vram = 0
		}
	}
	return util, vram
}

// listGPUsViaWMI WMI 回退。AdapterRAM 是 uint32，显存 >4GB 会溢出。
// NVIDIA 卡额外尝试注册表获取准确值；AMD/Intel 卡 AdapterRAM 溢出风险较低（消费级显存通常 ≤4GB 或用其他 API）。
func listGPUsViaWMI() []GPUInfo {
	ps := `$g = Get-CimInstance Win32_VideoController; foreach ($x in $g) { "$($x.Name)|$($x.AdapterRAM)|$($x.DriverVersion)|$($x.PNPDeviceID)" }`
	out, _ := powershellHidden(ps)
	var gpus []GPUInfo
	idx := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		g := GPUInfo{}
		if len(parts) > 0 {
			g.Name = strings.TrimSpace(parts[0])
		}
		if len(parts) > 1 && parts[1] != "" {
			g.VRAMMB = atoiSafe(parts[1]) / (1024 * 1024)
		}
		if len(parts) > 2 {
			g.Driver = strings.TrimSpace(parts[2])
		}
		g.Vendor, g.Backend = detectGPUVendor(g.Name)
		// 排除非主流厂商(虚拟显卡、Microsoft Basic Render Driver 等)
		if g.Vendor == "other" {
			continue
		}
		// NVIDIA: try registry for accurate VRAM (WMI AdapterRAM overflows at 4GB)
		if g.Vendor == "nvidia" {
			if regMB := queryNvidiaRegistryVRAM(); regMB > 0 {
				g.VRAMMB = regMB
			}
		}
		g.Usable = gpuUsable(g.Vendor)
		g.Features = gpuFeaturesFor(g.Vendor)
		g.Index = idx
		idx++
		gpus = append(gpus, g)
	}
	return gpus
}

// queryNvidiaRegistryVRAM 从注册表读取 NVIDIA GPU 显存（qwMemorySize，uint64，无 4GB 溢出问题）。
// 返回 MB 值；失败返回 0。
func queryNvidiaRegistryVRAM() int {
	ps := `
$key = 'HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}'
$max = 0
Get-ChildItem $key -ErrorAction SilentlyContinue | ForEach-Object {
  $v = Get-ItemProperty $_.PSPath -Name 'HardwareInformation.qwMemorySize' -ErrorAction SilentlyContinue
  $mb = [int]($v.'HardwareInformation.qwMemorySize' / 1MB)
  if ($mb -gt $max) { $max = $mb }
}
"$max"
`
	out, _ := powershellHidden(ps)
	return atoiSafe(strings.TrimSpace(out))
}

// detectGPUVendor 按名称识别 GPU 厂商和推荐后端。
func detectGPUVendor(name string) (vendor, backend string) {
	low := strings.ToLower(name)
	switch {
	case containsAny(low, "nvidia", "geforce", "rtx", "gtx", "quadro", "tesla"):
		return "nvidia", "cuda"
	case containsAny(low, "amd", "radeon", "rx ", "vega", "instinct"):
		return "amd", "directml"
	case containsAny(low, "intel", "arc", "iris", "uhd graphics", "hd graphics"):
		return "intel", "directml"
	}
	return "other", "none"
}

// gpuFeaturesFor 按厂商生成能力标签。NVIDIA 即使走 WMI 回退也用完整标签。
func gpuFeaturesFor(vendor string) []Capability {
	switch vendor {
	case "nvidia":
		return nvidiaFeatures("") // WMI 回退无 compute_cap
	case "amd":
		return []Capability{
			{Key: "directml", Label: "DirectML", Available: true},
			{Key: "rocm", Label: "ROCm", Available: dllExists("amd_comdata") || dllExists("amdocl")},
			{Key: "amf", Label: "AMF", Available: true},
		}
	case "intel":
		return []Capability{
			{Key: "directml", Label: "DirectML", Available: true},
			{Key: "openvino", Label: "OpenVINO", Available: dllExists("openvino")},
			{Key: "oneapi", Label: "oneAPI", Available: dllExists("sycl")},
			{Key: "qsv", Label: "QuickSync", Available: true},
		}
	}
	return []Capability{{Key: "directml", Label: "DirectML", Available: true}}
}

// gpuUsable 判断 GPU 是否可用于推理。
func gpuUsable(vendor string) bool {
	switch vendor {
	case "nvidia":
		// nvidia-smi 不可用时，只要 CUDA 相关 DLL 在也算可用
		return dllExists("nvcuda") || dllExists("cudart64")
	case "amd", "intel":
		return true // DirectML 通用可用
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func clamp0to100(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func atoiSafe(s string) int {
	n := 0
	neg := false
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		} else {
			break
		}
	}
	if neg {
		return -n
	}
	return n
}

func ftToUint64(ft syscall.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}
