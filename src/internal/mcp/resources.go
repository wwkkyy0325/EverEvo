package mcp

import "encoding/json"

// ResourceProvider is the interface the MCP server needs to serve resources.
type ResourceProvider interface {
	ListModelsJSON() (json.RawMessage, error)
	ListDownloadedModelsJSON() (json.RawMessage, error)
	ListToolModelsJSON() (json.RawMessage, error)
	ListPluginsJSON() (json.RawMessage, error)
	GetPluginStatusJSON(name string) (json.RawMessage, error)
	ListKBsJSON() (json.RawMessage, error)
	GetSysInfoJSON() (json.RawMessage, error)
	GetDynamicInfoJSON() (json.RawMessage, error)
	GetBackendsJSON() (json.RawMessage, error)
	ListGuidesJSON() (json.RawMessage, error)
	ReadGuideJSON(id string) (json.RawMessage, error)
}

// builtinResources defines all static resources.
var builtinResources = []ResourceDef{
	{URI: "everevo://models/list", Name: "Loaded Models", Title: "已加载的模型", Description: "当前已加载到内存中的模型列表", MimeType: "application/json"},
	{URI: "everevo://models/downloaded", Name: "Downloaded Models", Title: "已下载的模型", Description: "已下载到本地的模型文件列表", MimeType: "application/json"},
	{URI: "everevo://models/tool", Name: "Tool Models", Title: "工具箱模型", Description: "工具箱中已探测到类型的可用模型", MimeType: "application/json"},
	{URI: "everevo://plugins/list", Name: "Installed Plugins", Title: "已安装的插件", Description: "所有已安装的插件及其元数据", MimeType: "application/json"},
	{URI: "everevo://kb/list", Name: "Knowledge Bases", Title: "知识库列表", Description: "所有已创建的知识库", MimeType: "application/json"},
	{URI: "everevo://system/info", Name: "System Info", Title: "系统信息", Description: "静态硬件信息（CPU/GPU/内存/OS）", MimeType: "application/json"},
	{URI: "everevo://system/dynamic", Name: "System Dynamic", Title: "系统实时指标", Description: "实时 CPU/GPU/内存使用率", MimeType: "application/json"},
	{URI: "everevo://system/backends", Name: "Inference Backends", Title: "推理后端", Description: "ONNX Runtime / llama.cpp 可用状态", MimeType: "application/json"},
	{URI: "everevo://guides/list", Name: "Guides List", Title: "攻略列表", Description: "所有已同步的攻略文档列表", MimeType: "application/json"},
	{URI: "everevo://guides/content", Name: "Guide Content", Title: "攻略内容", Description: "读取指定攻略的完整内容（需 URI 参数 ?id=...）", MimeType: "text/markdown"},
}

// HandleResourcesList returns all available resources.
func HandleResourcesList() (*ListResourcesResult, error) {
	return &ListResourcesResult{Resources: builtinResources}, nil
}

// HandleResourcesRead reads a specific resource by URI.
func HandleResourcesRead(provider ResourceProvider, uri string) (*ReadResourceResult, error) {
	var data json.RawMessage
	var err error

	switch uri {
	case "everevo://models/list":
		data, err = provider.ListModelsJSON()
	case "everevo://models/downloaded":
		data, err = provider.ListDownloadedModelsJSON()
	case "everevo://models/tool":
		data, err = provider.ListToolModelsJSON()
	case "everevo://plugins/list":
		data, err = provider.ListPluginsJSON()
	case "everevo://kb/list":
		data, err = provider.ListKBsJSON()
	case "everevo://system/info":
		data, err = provider.GetSysInfoJSON()
	case "everevo://system/dynamic":
		data, err = provider.GetDynamicInfoJSON()
	case "everevo://system/backends":
		data, err = provider.GetBackendsJSON()
	case "everevo://guides/list":
		data, err = provider.ListGuidesJSON()
	default:
		if len(uri) > 28 && uri[:28] == "everevo://guides/content?id=" {
			id := uri[28:]
			data, err = provider.ReadGuideJSON(id)
		} else if len(uri) > 27 && uri[:27] == "everevo://guides/content/" {
			id := uri[27:]
			data, err = provider.ReadGuideJSON(id)
		} else if len(uri) > 23 && uri[:23] == "everevo://plugins/status/" {
			name := uri[23:]
			data, err = provider.GetPluginStatusJSON(name)
		} else {
			return &ReadResourceResult{
				Contents: []ContentBlock{TextContent("Unknown resource: " + uri)},
			}, nil
		}
	}

	if err != nil {
		return &ReadResourceResult{
			Contents: []ContentBlock{TextContent("Error: " + err.Error())},
		}, nil
	}

	text := string(data)
	return &ReadResourceResult{
		Contents: []ContentBlock{TextContent(text)},
	}, nil
}
