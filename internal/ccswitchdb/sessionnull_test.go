package ccswitchdb

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// createTempSchema builds a temp cc-switch-shaped DB (writable), returns its path.
func createTempSchema(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cc-switch.db")
	dsn := "file:" + toURIPath(path) + "?_busy_timeout=3000"
	wdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open writable: %v", err)
	}
	defer wdb.Close()
	schema := `
CREATE TABLE proxy_request_logs (
	id INTEGER PRIMARY KEY,
	app_type TEXT,
	session_id TEXT,
	created_at INTEGER,
	status_code INTEGER,
	input_tokens INTEGER,
	output_tokens INTEGER,
	cache_read_tokens INTEGER,
	cache_creation_tokens INTEGER,
	total_cost_usd REAL,
	model TEXT,
	request_model TEXT,
	latency_ms INTEGER,
	error_message TEXT
);
CREATE TABLE providers (
	id TEXT PRIMARY KEY,
	name TEXT,
	category TEXT,
	website_url TEXT,
	cost_multiplier REAL,
	limit_daily_usd REAL,
	limit_monthly_usd REAL,
	settings_config TEXT,
	is_current INTEGER,
	app_type TEXT
);`
	if _, err := wdb.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return path
}

// insertLog inserts one proxy_request_logs row for the given session.
func insertLog(t *testing.T, path, app, session string, status, in, out, cr, cc int, cost float64, ts int64) {
	t.Helper()
	wdb, err := sql.Open("sqlite", "file:"+toURIPath(path)+"?_busy_timeout=3000")
	if err != nil {
		t.Fatalf("open writable: %v", err)
	}
	defer wdb.Close()
	_, err = wdb.Exec(`INSERT INTO proxy_request_logs
		(app_type, session_id, created_at, status_code, input_tokens, output_tokens,
		 cache_read_tokens, cache_creation_tokens, total_cost_usd, model, request_model, latency_ms, error_message)
		VALUES (?,?,?,?,?,?,?,?,?,'m','m',0,'')`,
		app, session, ts, status, in, out, cr, cc, cost)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// TestSessionTotalsZeroRows reproduces the "读取会话用量失败: sql: Scan error ...
// converting NULL to int64" warning seen in the expanded overlay. When a live
// Claude session exists in the registry but has zero rows in proxy_request_logs
// (just started, before cc switch logs a request), the un-COALESCEd success-count
// SUM returned NULL and scanning into int64 failed. TodayTotals was fixed in
// e1c189c but SessionTotals and TodayTotalsAll were missed.
func TestSessionTotalsZeroRows(t *testing.T) {
	path := createTempSchema(t)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if se := db.SchemaError(); se != nil {
		t.Fatalf("schema error: %v", se)
	}

	totals, err := db.SessionTotals("claude", "no-such-session")
	if err != nil {
		t.Fatalf("SessionTotals with zero rows returned error: %v", err)
	}
	if totals.Requests != 0 || totals.Successes != 0 || totals.InputTokens != 0 {
		t.Errorf("zero-row totals = %+v, want all zero", totals)
	}
}

// TestTodayTotalsAllZeroRows guards the same NULL-scan bug in the all-apps total
// (surfaces as "读取全部 app 今日用量失败" when the DB has no rows in the window).
func TestTodayTotalsAllZeroRows(t *testing.T) {
	path := createTempSchema(t)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.TodayTotalsAll(0, 9999999999); err != nil {
		t.Fatalf("TodayTotalsAll with zero rows returned error: %v", err)
	}
}

// TestSessionTotalsWithRows ensures the COALESCE fix does not alter counts when
// rows exist — success count must reflect status_code=200, totals must accumulate.
func TestSessionTotalsWithRows(t *testing.T) {
	path := createTempSchema(t)
	insertLog(t, path, "claude", "s1", 200, 100, 50, 10, 5, 0.01, 1000)
	insertLog(t, path, "claude", "s1", 500, 200, 0, 0, 0, 0.0, 1001) // non-200
	insertLog(t, path, "claude", "s2", 200, 999, 999, 0, 0, 0.0, 1002) // other session

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	got, err := db.SessionTotals("claude", "s1")
	if err != nil {
		t.Fatalf("SessionTotals s1: %v", err)
	}
	if got.Requests != 2 {
		t.Errorf("Requests = %d, want 2", got.Requests)
	}
	if got.Successes != 1 { // only the 200 row
		t.Errorf("Successes = %d, want 1", got.Successes)
	}
	if got.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", got.InputTokens)
	}
	if got.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", got.OutputTokens)
	}
	if got.CostUSD < 0.0099 || got.CostUSD > 0.0101 {
		t.Errorf("CostUSD = %.4f, want ~0.01", got.CostUSD)
	}
}
