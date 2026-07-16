//go:build windows

package httpclient

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const regInternetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// systemProxy reads the Windows system proxy from the registry
// (IE / Edge proxy settings). Returns "" if not configured.
func systemProxy() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, regInternetSettings, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return ""
	}

	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || server == "" {
		return ""
	}

	// Add http:// prefix if the proxy is just host:port
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	return server
}
