package sysinfo

// SysInfo 静态系统信息。
type SysInfo struct {
	OS      OSInfo      `json:"os"`
	CPU     CPUInfo     `json:"cpu"`
	Memory  MemoryStat  `json:"memory"`
	GPUs    []GPUInfo   `json:"gpus"`
	Disks   []DiskInfo  `json:"disks"`
	Runtime RuntimeInfo `json:"runtime"`
}

type OSInfo struct {
	Name    string `json:"name"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

// Capability 单项能力标签。
type Capability struct {
	Key       string `json:"key"`       // 唯一键，如 "avx2", "cuda", "amx"
	Label     string `json:"label"`     // 显示名，如 "AVX2", "CUDA"
	Available bool   `json:"available"` // 是否可用
}

type CPUInfo struct {
	Name     string       `json:"name"`
	Vendor   string       `json:"vendor"`   // intel, amd, other
	Cores    int          `json:"cores"`
	Threads  int          `json:"threads"`
	Features []Capability `json:"features"` // avx, avx2, avx512, amx, onednn...
}

type MemoryStat struct {
	TotalGB float64 `json:"totalGB"`
}

type GPUInfo struct {
	Index      int          `json:"index"`      // 全局编号，调用时指定用
	Name       string       `json:"name"`
	Vendor     string       `json:"vendor"`     // nvidia, amd, intel, other
	Backend    string       `json:"backend"`    // cuda, rocm, directml, oneapi, none
	Usable     bool         `json:"usable"`     // 是否可用于推理（后端就绪）
	VRAMMB     int          `json:"vramMB"`     // 显存总量
	Driver     string       `json:"driver"`
	CudaVer    string       `json:"cudaVer"`    // NVIDIA CUDA 版本
	ComputeCap string       `json:"computeCap"` // NVIDIA 计算能力，如 "8.6"
	Features   []Capability `json:"features"`   // cuda, cudnn, tensorrt, nvenc...
}

type DiskInfo struct {
	Drive   string  `json:"drive"`
	TotalGB float64 `json:"totalGB"`
}

type RuntimeInfo struct {
	GoVersion string `json:"goVersion"`
	GoArch    string `json:"goArch"`
	NumCPU    int    `json:"numCPU"`
}

// DynamicInfo 动态系统指标。
type DynamicInfo struct {
	CPUPercent    int                   `json:"cpuPercent"`
	MemoryPercent int                   `json:"memoryPercent"`
	MemoryUsedGB  float64               `json:"memoryUsedGB"`
	MemoryTotalGB float64               `json:"memoryTotalGB"`
	GPUs          []GPUDynamic          `json:"gpus"`
	Disks         []DiskDynamic         `json:"disks"`
	PhysicalDisks []PhysicalDiskDynamic `json:"physicalDisks"` // grouped by physical disk
}

type GPUDynamic struct {
	Index       int `json:"index"`
	UtilPercent int `json:"utilPercent"`
	VRAMUsedMB  int `json:"vramUsedMB"`
	VRAMTotalMB int `json:"vramTotalMB"`
}

type DiskDynamic struct {
	Drive   string  `json:"drive"`
	FreeGB  float64 `json:"freeGB"`
	TotalGB float64 `json:"totalGB"`
	Percent int     `json:"percent"`
}

// PhysicalDiskDynamic groups volumes under their physical disk.
type PhysicalDiskDynamic struct {
	Name    string        `json:"name"`
	Model   string        `json:"model"`
	SizeGB  float64       `json:"sizeGB"`
	Volumes []DiskDynamic `json:"volumes"`
}

func round(v float64, decimals int) float64 {
	pow := 1.0
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	return float64(int(v*pow+0.5)) / pow
}
