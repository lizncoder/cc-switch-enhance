package snapshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// EvaluateAlert decides the alert state from the latest quota data.
//   - dangerous: GLM any window percent >= percentThr, or DeepSeek balance <= balanceThr.
//   - safe:      GLM all windows <= percentThr - hysteresis, or DeepSeek balance >= balanceThr + hysteresis.
//   - reason:    human description of what crossed the line (empty when not dangerous).
//
// The band between (threshold - hysteresis) and threshold is the hysteresis
// zone: neither dangerous nor safe, so the caller's warned flag holds (no flapping).
func EvaluateAlert(limits []PlanLimit, balance *BalanceInfo, percentThr int, balanceThr, hysteresis float64) (dangerous, safe bool, reason string) {
	if len(limits) > 0 {
		safeLine := float64(percentThr) - hysteresis
		var over []string
		allSafe := true
		for _, p := range limits {
			pct := float64(p.Percent)
			if pct >= float64(percentThr) {
				over = append(over, fmt.Sprintf("%s %d%%", p.Window, p.Percent))
			}
			if pct > safeLine {
				allSafe = false
			}
		}
		sort.Strings(over) // stable order for deterministic reason text
		return len(over) > 0, allSafe && len(over) == 0, strings.Join(over, " · ")
	}
	if balance != nil {
		amt, _ := strconv.ParseFloat(balance.TotalBalance, 64)
		safeLine := balanceThr + hysteresis
		if amt <= balanceThr {
			return true, false, fmt.Sprintf("余额 ¥%s", trimAmount(balance.TotalBalance))
		}
		if amt >= safeLine {
			return false, true, ""
		}
		return false, false, "" // hysteresis band
	}
	return false, false, ""
}

// trimAmount drops trailing zeros for compact display ("6.20" -> "6.2", "50.00" -> "50").
func trimAmount(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
