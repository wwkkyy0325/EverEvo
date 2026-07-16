//go:build windows

package onnx

import (
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// ONNX Runtime 的全局初始化状态。yalue 的 InitializeEnvironment 是进程级全局，
// 只能成功调用一次；这里用互斥 + 标志保证 Init 幂等、且缓存首次结果。
var (
	initMu  sync.Mutex
	inited  bool
	initErr error
)

// Init 加载指定路径的 onnxruntime 共享库并初始化全局 ORT 环境。
// 幂等：多次调用只有首次生效，后续调用直接返回首次的结果。
// 失败时返回错误（不 panic），保持 app.go startup 里的非致命语义。
func Init(dllPath string) error {
	initMu.Lock()
	defer initMu.Unlock()
	if inited || initErr != nil {
		return initErr
	}
	ort.SetSharedLibraryPath(dllPath)
	if err := ort.InitializeEnvironment(); err != nil {
		initErr = err
		return err
	}
	inited = true
	return nil
}

// Initialized 报告 ORT 环境是否已成功初始化。
func Initialized() bool {
	initMu.Lock()
	defer initMu.Unlock()
	return inited
}

// Close 销毁全局 ORT 环境。应在应用退出、所有会话关闭之后调用。
func Close() error {
	initMu.Lock()
	defer initMu.Unlock()
	if !inited {
		return nil
	}
	err := ort.DestroyEnvironment()
	inited = false
	initErr = nil
	return err
}
