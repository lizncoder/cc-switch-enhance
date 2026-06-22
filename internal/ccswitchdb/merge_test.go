package ccswitchdb

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testDBPath resolves the cc-switch DB path without importing config (keeps the
// ccswitchdb package dependency-free). Skips when absent.
func testDBPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CCSWITCH_DB"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	return filepath.Join(home, ".cc-switch", "cc-switch.db")
}

// openTestDB opens the real cc-switch DB or skips.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := testDBPath(t)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("cc switch DB not found at %s", path)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func assertTotalsEq(t *testing.T, name string, a, b Totals) {
	t.Helper()
	// NOTE: the cc-switch DB is live; a request may land between the merged and
	// the per-window queries. This test exists to catch gross SQL bugs (a typo in
	// the 16-column merged query would diverge wildly), not single-row races.
	if a.Requests != b.Requests {
		t.Errorf("%s requests: merged=%d old=%d", name, b.Requests, a.Requests)
	}
	if a.Successes != b.Successes {
		t.Errorf("%s successes: merged=%d old=%d", name, b.Successes, a.Successes)
	}
	if a.InputTokens != b.InputTokens {
		t.Errorf("%s input: merged=%d old=%d", name, b.InputTokens, a.InputTokens)
	}
	if a.OutputTokens != b.OutputTokens {
		t.Errorf("%s output: merged=%d old=%d", name, b.OutputTokens, a.OutputTokens)
	}
	if a.CacheRead != b.CacheRead {
		t.Errorf("%s cacheRead: merged=%d old=%d", name, b.CacheRead, a.CacheRead)
	}
	if a.CacheCreate != b.CacheCreate {
		t.Errorf("%s cacheCreate: merged=%d old=%d", name, b.CacheCreate, a.CacheCreate)
	}
}

// TestSchemaOK confirms the real cc-switch DB has every column the queries need
// (so SchemaError is nil on a healthy install). Regression guard: if cc-switch
// renames a column, this flips and the overlay degrades to a single chip instead
// of a wall of per-query errors.
func TestSchemaOK(t *testing.T) {
	db := openTestDB(t)
	if err := db.SchemaError(); err != nil {
		t.Fatalf("SchemaError = %v, want nil (cc-switch schema drifted?)", err)
	}
}

// TestMultiWindowTotalsEquivalence proves the single merged scan returns the
// same numbers as the four separate TodayTotals/MonthTotals calls it replaces.
func TestMultiWindowTotalsEquivalence(t *testing.T) {
	db := openTestDB(t)
	const app = "claude"
	now := time.Now()
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	firstMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	h5 := now.Add(-5 * time.Hour).Unix()
	d7 := now.Add(-7 * 24 * time.Hour).Unix()
	since := firstMonth.Unix()
	if d7 < since {
		since = d7
	}

	month, today, h5Sum, d7Sum, err := db.MultiWindowTotals(app, since, firstMonth.Unix(), startToday.Unix(), h5, d7)
	if err != nil {
		t.Fatalf("MultiWindowTotals: %v", err)
	}

	if old, err := db.TodayTotals(app, startToday.Unix(), now.Unix()); err != nil {
		t.Fatalf("TodayTotals(today): %v", err)
	} else {
		assertTotalsEq(t, "today", old, today)
	}
	if old, err := db.MonthTotals(app, firstMonth.Unix(), now.Unix()); err != nil {
		t.Fatalf("MonthTotals: %v", err)
	} else {
		assertTotalsEq(t, "month", old, month)
	}
	if old, err := db.TodayTotals(app, h5, now.Unix()); err != nil {
		t.Fatalf("TodayTotals(5h): %v", err)
	} else {
		if want := old.InputTokens + old.OutputTokens + old.CacheRead + old.CacheCreate; h5Sum != want {
			t.Errorf("5h sum: merged=%d old=%d", h5Sum, want)
		}
	}
	if old, err := db.TodayTotals(app, d7, now.Unix()); err != nil {
		t.Fatalf("TodayTotals(7d): %v", err)
	} else {
		if want := old.InputTokens + old.OutputTokens + old.CacheRead + old.CacheCreate; d7Sum != want {
			t.Errorf("7d sum: merged=%d old=%d", d7Sum, want)
		}
	}
}

// TestWeeklyTotalsEquivalence proves the single GROUP BY scan returns the same
// per-day totals as the old 7-query loop (one TodayTotals per day).
func TestWeeklyTotalsEquivalence(t *testing.T) {
	db := openTestDB(t)
	const app = "claude"
	now := time.Now()
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	got, err := db.WeeklyTotals(app, startToday.AddDate(0, 0, -6).Unix())
	if err != nil {
		t.Fatalf("WeeklyTotals: %v", err)
	}
	for i := 6; i >= 0; i-- {
		dayStart := startToday.AddDate(0, 0, -i)
		dayEnd := dayStart.AddDate(0, 0, 1)
		old, err := db.TodayTotals(app, dayStart.Unix(), dayEnd.Unix()-1)
		if err != nil {
			t.Fatalf("TodayTotals(day %d): %v", i, err)
		}
		want := old.InputTokens + old.OutputTokens + old.CacheRead + old.CacheCreate
		key := dayStart.Format("2006-01-02")
		if gotTok := got[key]; gotTok != want {
			t.Errorf("day %s: merged=%d old=%d", key, gotTok, want)
		}
	}
}
