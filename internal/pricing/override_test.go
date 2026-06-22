package pricing

import (
	"math"
	"testing"

	"cc-enhance/internal/config"
)

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestEstimateModelPrecedence(t *testing.T) {
	prices := map[string]config.Pricing{
		"default": {In: 3, Out: 15, CacheRead: 0.3, CacheCreate: 3.75},
		"glm-5.2": {In: 1, Out: 1, CacheRead: 1, CacheCreate: 1},
	}
	// Exact model match wins over default.
	got := Estimate(prices, "glm-5.2", 1e6, 1e6, 1e6, 1e6)
	if !approxEq(got, 4) {
		t.Errorf("model match: got %v want 4", got)
	}
	// Unknown model falls back to "default" (1M in @ $3 = $3).
	got = Estimate(prices, "unknown", 1e6, 0, 0, 0)
	if !approxEq(got, 3) {
		t.Errorf("default fallback: got %v want 3", got)
	}
}

func TestEstimateMath(t *testing.T) {
	prices := map[string]config.Pricing{
		"default": {In: 3, Out: 15, CacheRead: 0.3, CacheCreate: 3.75},
	}
	// 2M in @ 3 + 1M out @ 15 + 10M cacheRead @ 0.3 + 0 cacheCreate
	got := Estimate(prices, "default", 2e6, 1e6, 10e6, 0)
	want := 2*3 + 1*15 + 10*0.3 // 6 + 15 + 3 = 24
	if !approxEq(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestEstimateEmpty(t *testing.T) {
	if Estimate(nil, "x", 1e6, 1e6, 1e6, 1e6) != 0 {
		t.Error("nil prices should be 0")
	}
	if Estimate(map[string]config.Pricing{}, "x", 1, 1, 1, 1) != 0 {
		t.Error("empty prices should be 0")
	}
}

func TestEstimateNoMatchNoDefault(t *testing.T) {
	prices := map[string]config.Pricing{"glm": {In: 2, Out: 2, CacheRead: 2, CacheCreate: 2}}
	if Estimate(prices, "unknown", 1e6, 1e6, 1e6, 1e6) != 0 {
		t.Error("no model match and no default should be 0")
	}
}
