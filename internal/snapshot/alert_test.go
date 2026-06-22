package snapshot

import "testing"

func TestEvaluateAlert(t *testing.T) {
	cases := []struct {
		name       string
		limits     []PlanLimit
		balance    *BalanceInfo
		percent    int
		balanceThr float64
		hysteresis float64
		wantDanger bool
		wantSafe   bool
		wantReason string
	}{
		{
			name:       "glm 7d over threshold",
			limits:     []PlanLimit{{Window: "5小时", Percent: 80}, {Window: "7天", Percent: 90}},
			percent:    85, hysteresis: 10,
			wantDanger: true, wantSafe: false, wantReason: "7天 90%",
		},
		{
			name:       "glm both windows over",
			limits:     []PlanLimit{{Window: "5小时", Percent: 87}, {Window: "7天", Percent: 91}},
			percent:    85, hysteresis: 10,
			wantDanger: true, wantSafe: false, wantReason: "5小时 87% · 7天 91%",
		},
		{
			name:       "glm safe below safe-line",
			limits:     []PlanLimit{{Window: "5小时", Percent: 70}, {Window: "7天", Percent: 70}},
			percent:    85, hysteresis: 10,
			wantDanger: false, wantSafe: true, wantReason: "",
		},
		{
			name:       "glm hysteresis band (neither)",
			limits:     []PlanLimit{{Window: "5小时", Percent: 80}},
			percent:    85, hysteresis: 10,
			wantDanger: false, wantSafe: false, wantReason: "",
		},
		{
			name:       "deepseek balance low",
			balance:    &BalanceInfo{TotalBalance: "6.20", Currency: "CNY"},
			balanceThr: 10, hysteresis: 10,
			wantDanger: true, wantSafe: false, wantReason: "余额 ¥6.2",
		},
		{
			name:       "deepseek balance healthy",
			balance:    &BalanceInfo{TotalBalance: "50.00", Currency: "CNY"},
			balanceThr: 10, hysteresis: 10,
			wantDanger: false, wantSafe: true, wantReason: "",
		},
		{
			name:       "no data",
			percent:    85, balanceThr: 10, hysteresis: 10,
			wantDanger: false, wantSafe: false, wantReason: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			danger, safe, reason := EvaluateAlert(c.limits, c.balance, c.percent, c.balanceThr, c.hysteresis)
			if danger != c.wantDanger || safe != c.wantSafe || reason != c.wantReason {
				t.Errorf("EvaluateAlert: danger=%v safe=%v reason=%q; want danger=%v safe=%v reason=%q",
					danger, safe, reason, c.wantDanger, c.wantSafe, c.wantReason)
			}
		})
	}
}
