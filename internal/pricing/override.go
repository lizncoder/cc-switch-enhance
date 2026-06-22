// Package pricing estimates cost from real token counts using the overlay's
// own pricing map (never cc switch's model_pricing table).
package pricing

import "cc-enhance/internal/config"

// Estimate returns an estimated USD cost for the given real token counts.
// Precedence: prices[model] -> prices["default"] -> 0.
func Estimate(prices map[string]config.Pricing, model string, in, out, cacheRead, cacheCreate int64) float64 {
	if len(prices) == 0 {
		return 0
	}
	p, ok := prices[model]
	if !ok {
		p, ok = prices["default"]
		if !ok {
			return 0
		}
	}
	return float64(in)*p.In/1e6 +
		float64(out)*p.Out/1e6 +
		float64(cacheRead)*p.CacheRead/1e6 +
		float64(cacheCreate)*p.CacheCreate/1e6
}
