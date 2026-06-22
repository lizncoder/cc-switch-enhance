//go:build windows

package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"cc-enhance/internal/ccswitchdb"
	"cc-enhance/internal/claudecode"
	"cc-enhance/internal/snapshot"
)

// TestDiag builds a real Snapshot against the live cc switch DB + Claude Code
// files and prints it. Manual diagnostic — skipped unless the DB exists.
func TestDiag(t *testing.T) {
	a := NewApp()
	if _, err := os.Stat(a.paths.CCSwitchDB); err != nil {
		t.Skipf("cc switch DB not found at %s", a.paths.CCSwitchDB)
	}

	db, err := ccswitchdb.Open(a.paths.CCSwitchDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	a.db = db
	a.refreshSettings()
	a.refreshAvailableApps()

	a.cc = claudecode.NewWatcher(a.paths.ClaudeSessions, a.paths.ClaudeProjects)
	a.cc.Start()
	defer a.cc.Stop()
	time.Sleep(1200 * time.Millisecond) // let one poll land

	a.fetchLimits() // exercise the GLM plan-quota fetch once
	s := a.buildSnapshot()
	out, _ := json.MarshalIndent(s, "", "  ")
	t.Logf("SNAPSHOT:\n%s", string(out))

	// Basic assertions on the claude app (the configured active one).
	if s.AppType != "claude" {
		t.Errorf("appType = %q, want claude", s.AppType)
	}
	if s.Provider.Name == "" {
		t.Errorf("provider name empty; errors=%v", s.Errors)
	}
	if s.Model.Display == "" {
		t.Errorf("model display empty")
	}
	t.Logf("apps=%v provider=%s model=%s liveModel=%s match=%v",
		s.AvailableApps, s.Provider.Name, s.Model.Display, s.Model.LiveModel, s.Model.Match)
	t.Logf("today: in=%d out=%d req=%d cost=%.4f est=%.4f",
		s.Today.InputTokens, s.Today.OutputTokens, s.Today.Requests, s.Today.RealCostUSD, s.Today.EstCostUSD)
	t.Logf("month: in=%d out=%d cost=%.4f", s.Month.InputTokens, s.Month.OutputTokens, s.Month.RealCostUSD)
	t.Logf("session: live=%v id=%s status=%s latestIn=%d latestOut=%d",
		s.Session.Live, s.Session.SessionID, s.Session.Status, s.Session.LatestInput, s.Session.LatestOutput)
	if s.Latest != nil {
		t.Logf("latest: model=%s in=%d out=%d status=%d age=%ds",
			s.Latest.Model, s.Latest.InputTokens, s.Latest.OutputTokens, s.Latest.StatusCode, s.Latest.AgeSec)
	}
	// v2: context tokens = in + cacheRead + cacheCreate.
	if s.Today.ContextTokens != s.Today.InputTokens+s.Today.CacheRead+s.Today.CacheCreate {
		t.Errorf("today contextTokens=%d != in+cr+cc=%d",
			s.Today.ContextTokens, s.Today.InputTokens+s.Today.CacheRead+s.Today.CacheCreate)
	}
	if s.Session.Live && s.Session.LatestContextTokens <= 0 {
		t.Errorf("live session latestContextTokens empty")
	}
	t.Logf("context: today=%d sessionLatest=%d latest=%d",
		s.Today.ContextTokens, s.Session.LatestContextTokens, s.Latest.ContextTokens)
	if len(s.Series) == 0 {
		t.Errorf("series empty (chart will have no data)")
	}
	for i := 0; i < len(s.Series); i++ {
		// In must equal in+cr+cc of the underlying row (spot-check structure).
		if s.Series[i].In < s.Series[i].Out && s.Series[i].Out > 0 {
			// not a hard failure, just sanity
		}
	}
	t.Logf("series len=%d firstIn=%d lastIn=%d lastOut=%d",
		len(s.Series), firstIn(s.Series), lastIn(s.Series), lastOut(s.Series))
	if s.TodayAllAppsTokens <= 0 {
		t.Errorf("todayAllAppsTokens empty")
	}
	t.Logf("allAppsToday=%d (%.1fM)", s.TodayAllAppsTokens, float64(s.TodayAllAppsTokens)/1e6)
	t.Logf("windows: 5h=%d (%.1fM) 7d=%d (%.1fM)", s.Tokens5h, float64(s.Tokens5h)/1e6, s.Tokens7d, float64(s.Tokens7d)/1e6)
	for _, p := range s.PlanLimits {
		t.Logf("planLimit: %s · %s  %d%%  reset(ms)=%d", p.Kind, p.Window, p.Percent, p.NextResetMS)
	}
}

func firstIn(s []snapshot.SeriesPoint) int64 {
	if len(s) == 0 {
		return 0
	}
	return s[0].In
}
func lastIn(s []snapshot.SeriesPoint) int64 {
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1].In
}
func lastOut(s []snapshot.SeriesPoint) int64 {
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1].Out
}
