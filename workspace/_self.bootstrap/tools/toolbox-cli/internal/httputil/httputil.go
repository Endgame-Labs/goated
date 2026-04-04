package httputil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var client = &http.Client{Timeout: 30 * time.Second}

func DoJSON(method, url string, body interface{}, headers map[string]string, result interface{}) (int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	if result != nil {
		if len(bytes.TrimSpace(data)) == 0 {
			return resp.StatusCode, nil
		}
		if err := json.Unmarshal(data, result); err != nil {
			return resp.StatusCode, fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(data), 200))
		}
	}
	return resp.StatusCode, nil
}

func GetJSON(url string, headers map[string]string, result interface{}) (int, error) {
	return DoJSON(http.MethodGet, url, nil, headers, result)
}

func PostJSON(url string, body interface{}, headers map[string]string, result interface{}) (int, error) {
	return DoJSON(http.MethodPost, url, body, headers, result)
}

func BearerAuth(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
