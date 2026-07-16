package mcp

// lifecycle handles MCP initialization.

// HandleInitialize returns the server capabilities to the client.
func HandleInitialize(version string) *InitializeResult {
	return &InitializeResult{
		ProtocolVersion: "2025-06-18",
		Capabilities: ServerCaps{
			Tools:     &ToolsCaps{ListChanged: true},
			Resources: &ResourcesCaps{},
			Prompts:   &PromptsCaps{},
		},
		ServerInfo: ServerInfo{
			Name:    "EverEvo",
			Version: version,
		},
		Instructions: "EverEvo is a desktop AI model toolbox. Use tools to manage models, plugins, knowledge bases, and downloads. Use resources to query current state. Use prompts for guided workflows.",
	}
}
