// Package web is the shared plain-HTTP fetcher. Always sends the real-browser
// User-Agent (pitfall P2: Cloudflare 403s default Go/curl agents).
package web

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

var Client = &http.Client{Timeout: 30 * time.Second}

// Get returns body and status code. A non-2xx status is not an error here —
// callers like the checklist check care about the code itself.
func Get(url, userAgent string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en,pl;q=0.8")
	resp, err := Client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading %s: %w", url, err)
	}
	return body, resp.StatusCode, nil
}
