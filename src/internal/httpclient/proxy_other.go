//go:build !windows

package httpclient

// systemProxy returns "" on non-Windows platforms.
// macOS/Linux users typically use env vars (HTTP_PROXY) set by proxy tools.
func systemProxy() string { return "" }
