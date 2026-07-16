// Package httpclient provides a shared HTTP client factory with
// smart proxy detection: user config > env vars (Clash/V2Ray) >
// Windows system proxy (registry) > direct.
//
// The application calls SetUserProxy() at startup (from config)
// and on settings change. All HTTP clients created via New() /
// Transport() automatically pick up the proxy.
package httpclient

import (
	"net/http"
	"time"
)

// Transport returns a proxy-aware http.Transport.
func Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 proxyFunc(),
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// New returns a proxy-aware *http.Client with the given timeout.
func New(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: Transport(),
	}
}
