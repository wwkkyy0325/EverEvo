package main

import (
	"fmt"
	ort "github.com/yalue/onnxruntime_go"
)

func main() {
	dllPath := `F:\workspace-new\wwkkyy0325\EverEvo\build\bin\onnxruntime.dll`
	ort.SetSharedLibraryPath(dllPath)
	fmt.Println("SetSharedLibraryPath:", dllPath)
	err := ort.InitializeEnvironment()
	if err != nil {
		fmt.Println("❌ InitializeEnvironment 失败:", err)
	} else {
		fmt.Println("✓ ONNX Runtime 初始化成功!")
		ort.DestroyEnvironment()
	}
}
