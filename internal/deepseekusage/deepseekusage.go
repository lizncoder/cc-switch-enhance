// Package deepseekusage queries the DeepSeek account balance API
// (GET /user/balance) to show available funds when a DeepSeek provider
// is configured. It performs a single read-only HTTPS GET using the
// user's API key; the token is never logged.
package deepseekusage

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const deepseekBalanceURL = "https://api.deepseek.com/user/balance"

// BalanceInfo is the parsed balance payload pushed to the frontend.
type BalanceInfo struct {
	IsAvailable     bool   `json:"isAvailable"`
	TotalBalance    string `json:"totalBalance"`
	GrantedBalance  string `json:"grantedBalance"`
	ToppedUpBalance string `json:"toppedUpBalance"`
	Currency        string `json:"currency"`
}

type rawBalanceResp struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}

// Fetch queries the DeepSeek balance endpoint. authToken is the raw API
// key (sent as a Bearer token). Returns nil (no error) when the token is
// empty so callers can no-op.
func Fetch(authToken string) (*BalanceInfo, error) {
	if authToken == "" {
		return nil, nil
	}
	return fetchBalance(authToken, deepseekBalanceURL)
}

// fetchBalance is the testable core: hits the given URL with the bearer token.
func fetchBalance(authToken, url string) (*BalanceInfo, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("deepseek balance: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw rawBalanceResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if len(raw.BalanceInfos) == 0 {
		return nil, nil
	}

	b := raw.BalanceInfos[0]
	return &BalanceInfo{
		IsAvailable:     raw.IsAvailable,
		Currency:        b.Currency,
		TotalBalance:    b.TotalBalance,
		GrantedBalance:  b.GrantedBalance,
		ToppedUpBalance: b.ToppedUpBalance,
	}, nil
}
