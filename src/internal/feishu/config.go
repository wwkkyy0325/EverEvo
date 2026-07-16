package feishu

// Config holds the Feishu (Lark) self-built-app credentials for the WebSocket
// long-connection bot. Persisted as part of config.LLMConfig.
type Config struct {
	Enabled           bool   `json:"enabled"`
	AppID             string `json:"appId"`
	AppSecret         string `json:"appSecret"`
	VerificationToken string `json:"verificationToken"`
}

// Status is the runtime status reported to the frontend.
type Status struct {
	Running bool   `json:"running"`
	AppID   string `json:"appId"` // masked, last 4 chars
}
