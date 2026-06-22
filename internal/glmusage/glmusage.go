// Package glmusage queries the GLM/Z.ai Coding Plan quota-limit endpoint to
// read each usage window's percentage and next-reset time. It performs a single
// read-only HTTPS GET using the provider's auth token; it never writes anywhere
// and never logs the token.
package glmusage

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type rawLimit struct {
	Type        string `json:"type"`
	Unit        int    `json:"unit"`
	Number      int    `json:"number"`
	Percentage  int    `json:"percentage"`
	NextResetMS int64  `json:"nextResetTime"`
}

type rawResponse struct {
	Code int `json:"code"`
	Data struct {
		Limits []rawLimit `json:"limits"`
	} `json:"data"`
	Success bool `json:"success"`
}

// Limit is one plan usage window (e.g. "Token · 5小时" at 86%, resets at NextResetMS).
type Limit struct {
	Window      string `json:"window"`      // human window, e.g. "5小时"/"1天"/"1月"
	Kind        string `json:"kind"`        // "Token" / "MCP"
	Percent     int    `json:"percent"`
	NextResetMS int64  `json:"nextResetMs"` // unix milliseconds
}

// Fetch queries the plan quota endpoint. authToken is the raw
// ANTHROPIC_AUTH_TOKEN (sent verbatim as the Authorization header); baseURL is
// ANTHROPIC_BASE_URL (e.g. https://open.bigmodel.cn/api/anthropic).
// Returns nil (no error) when inputs are missing so callers can no-op.
func Fetch(baseURL, authToken string) ([]Limit, error) {
	if authToken == "" || baseURL == "" {
		return nil, nil
	}
	domain := stripToDomain(baseURL)
	if domain == "" {
		return nil, nil
	}
	url := domain + "/api/monitor/usage/quota/limit"
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US,en")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("glm quota: HTTP %d", resp.StatusCode)
	}
	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make([]Limit, 0, len(raw.Data.Limits))
	for _, l := range raw.Data.Limits {
		kind, window := describe(l)
		out = append(out, Limit{Window: window, Kind: kind, Percent: l.Percentage, NextResetMS: l.NextResetMS})
	}
	return out, nil
}

func describe(l rawLimit) (kind, window string) {
	switch l.Type {
	case "TOKENS_LIMIT":
		kind = "Token"
		switch l.Unit {
		case 3:
			window = "5小时" // GLM's 5-hour rolling token window (matches cc switch label)
		case 6:
			window = "7天" // GLM's 7-day token window (cc switch labels it "7天")
		default:
			window = fmt.Sprintf("unit%d·%d", l.Unit, l.Number)
		}
	case "TIME_LIMIT":
		kind = "MCP"
		window = "月度"
	default:
		kind = l.Type
		window = fmt.Sprintf("unit%d·%d", l.Unit, l.Number)
	}
	return kind, window
}

// stripToDomain turns https://open.bigmodel.cn/api/anthropic -> https://open.bigmodel.cn
func stripToDomain(baseURL string) string {
	u := strings.TrimSpace(baseURL)
	i := strings.Index(u, "://")
	if i < 0 {
		return ""
	}
	rest := u[i+3:]
	if j := strings.Index(rest, "/"); j >= 0 {
		return u[:i+3+j]
	}
	return u
}
