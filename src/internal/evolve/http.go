package evolve

import (
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 2 * time.Second}

func httpGetInternal(url string) (bool, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode < 500, nil
}
