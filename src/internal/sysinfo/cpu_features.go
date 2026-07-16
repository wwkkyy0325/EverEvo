package sysinfo

import "unsafe"

// cpuVendor 返回 CPUID leaf 0 的厂商字符串（GenuineIntel / AuthenticAMD 等）。
// 字节顺序：EBX:EDX:ECX。
func cpuVendor() string {
	_, ebx, ecx, edx := cpuidImpl(0, 0)
	var buf [12]byte
	put := func(off int, v uint32) {
		buf[off+0] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	put(0, ebx)
	put(4, edx)
	put(8, ecx)
	// 去尾部 0
	n := 12
	for n > 0 && buf[n-1] == 0 {
		n--
	}
	return string(buf[:n])
}

// detectCPUFeatures 返回 CPU 能力标签。
func detectCPUFeatures(vendor string) []Capability {
	caps := []Capability{}

	// leaf 1: AVX (ECX bit28)
	_, _, cx1, _ := cpuidImpl(1, 0)
	avx := cx1&(1<<28) != 0
	caps = append(caps, Capability{Key: "avx", Label: "AVX", Available: avx})

	// leaf 7, subleaf 0: AVX2 (EBX bit5), AVX-512F (EBX bit16), AMX (EDX bit22/23/24)
	_, bx7, _, dx7 := cpuidImpl(7, 0)
	avx2 := bx7&(1<<5) != 0
	avx512 := bx7&(1<<16) != 0
	amxTile := dx7&(1<<22) != 0 || dx7&(1<<23) != 0 || dx7&(1<<24) != 0

	caps = append(caps, Capability{Key: "avx2", Label: "AVX2", Available: avx2})
	caps = append(caps, Capability{Key: "avx512", Label: "AVX-512", Available: avx512})

	switch vendor {
	case "intel":
		caps = append(caps, Capability{Key: "amx", Label: "AMX", Available: amxTile})
		caps = append(caps, Capability{Key: "onednn", Label: "oneDNN", Available: true}) // Intel 推理加速库可用
	case "amd":
		caps = append(caps, Capability{Key: "zen", Label: "Zen 架构", Available: true})
	default:
		caps = append(caps, Capability{Key: "amx", Label: "AMX", Available: amxTile})
	}

	return caps
}

// 保留 unsafe 引用避免某些构建组合下未使用告警
var _ = unsafe.Sizeof(0)
