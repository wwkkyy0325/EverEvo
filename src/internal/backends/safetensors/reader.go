package safetensors

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Header safetensors 文件头（JSON 描述张量元数据）。
type Header map[string]TensorInfo

// TensorInfo 单个张量的元数据。
type TensorInfo struct {
	Dtype       string   `json:"dtype"`       // "F16", "F32", "I32" 等
	Shape       []int64  `json:"shape"`
	DataOffsets [2]int64 `json:"data_offsets"` // [起始偏移, 结束偏移]
}

// Reader 封装一个已打开的 safetensors 文件。
type Reader struct {
	header   Header
	dataStart int64
	file     *os.File
}

// Open 打开 safetensors 文件，解析头部。
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// 前 8 字节：header size（little-endian uint64）
	var headerSize uint64
	if err := binary.Read(f, binary.LittleEndian, &headerSize); err != nil {
		f.Close()
		return nil, fmt.Errorf("读取 header size 失败: %w", err)
	}
	// 读取 JSON header
	buf := make([]byte, headerSize)
	if _, err := io.ReadFull(f, buf); err != nil {
		f.Close()
		return nil, fmt.Errorf("读取 header 失败: %w", err)
	}
	var header Header
	if err := json.Unmarshal(buf, &header); err != nil {
		f.Close()
		return nil, fmt.Errorf("解析 header JSON 失败: %w", err)
	}
	return &Reader{header: header, dataStart: 8 + int64(headerSize), file: f}, nil
}

// TensorNames 返回所有张量名称。
func (r *Reader) TensorNames() []string {
	names := make([]string, 0, len(r.header))
	for name := range r.header {
		names = append(names, name)
	}
	return names
}

// Header 返回原始 header。
func (r *Reader) Header() Header {
	return r.header
}

// ReadTensor 读取指定张量的原始数据。
func (r *Reader) ReadTensor(name string) ([]byte, error) {
	info, ok := r.header[name]
	if !ok {
		return nil, fmt.Errorf("张量 %s 不存在", name)
	}
	start := r.dataStart + info.DataOffsets[0]
	size := info.DataOffsets[1] - info.DataOffsets[0]
	buf := make([]byte, size)
	if _, err := r.file.ReadAt(buf, start); err != nil {
		return nil, err
	}
	return buf, nil
}

// Close 关闭文件。
func (r *Reader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}
