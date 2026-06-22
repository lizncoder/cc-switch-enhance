package glmusage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchParsesLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/monitor/usage/quota/limit" {
			t.Errorf("path=%q want /api/monitor/usage/quota/limit", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "tok-123" {
			t.Errorf("Authorization=%q want tok-123", got)
		}
		resp := rawResponse{Code: 200, Success: true}
		resp.Data.Limits = []rawLimit{
			{Type: "TOKENS_LIMIT", Unit: 3, Percentage: 86, NextResetMS: 1782100000000},
			{Type: "TOKENS_LIMIT", Unit: 6, Percentage: 12, NextResetMS: 1782500000000},
			{Type: "TIME_LIMIT", Unit: 0, Percentage: 1, NextResetMS: 1782900000000},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	limits, err := Fetch(srv.URL+"/api/anthropic", "tok-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 3 {
		t.Fatalf("limits=%d want 3", len(limits))
	}
	if limits[0].Window != "5小时" || limits[0].Kind != "Token" || limits[0].Percent != 86 {
		t.Errorf("limit0=%+v", limits[0])
	}
	if limits[1].Window != "7天" || limits[1].Kind != "Token" {
		t.Errorf("limit1=%+v", limits[1])
	}
	if limits[2].Kind != "MCP" || limits[2].Window != "月度" {
		t.Errorf("limit2=%+v want MCP/月度", limits[2])
	}
}

func TestFetchEmptyInputs(t *testing.T) {
	if _, err := Fetch("", "tok"); err != nil {
		t.Errorf("empty baseURL should no-op, got %v", err)
	}
	if _, err := Fetch("https://x", ""); err != nil {
		t.Errorf("empty token should no-op, got %v", err)
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	if _, err := Fetch(srv.URL+"/api/anthropic", "tok"); err == nil {
		t.Error("expected error on HTTP 500")
	}
}

func TestStripToDomain(t *testing.T) {
	if d := stripToDomain("https://open.bigmodel.cn/api/anthropic"); d != "https://open.bigmodel.cn" {
		t.Errorf("got %q", d)
	}
	if d := stripToDomain("not a url"); d != "" {
		t.Errorf("got %q want empty", d)
	}
}
