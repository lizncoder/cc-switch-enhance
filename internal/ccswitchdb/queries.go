package ccswitchdb

import (
	"database/sql"
	"errors"
)

// Provider is the active cc switch provider row for an app.
type Provider struct {
	ID              string
	Name            string
	Category        string
	WebsiteURL      string
	CostMultiplier  float64
	LimitDailyUSD   sql.NullFloat64
	LimitMonthlyUSD sql.NullFloat64
	SettingsConfig  string // raw JSON
}

// Totals is an aggregate of tokens + cost + counts.
type Totals struct {
	Requests     int64
	Successes    int64
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheCreate  int64
	CostUSD      float64
	LatencyMS    int64
}

// RequestRow is the most recent proxy/session log row.
type RequestRow struct {
	Model         string
	RequestModel  string
	InputTokens   int64
	OutputTokens  int64
	CacheRead     int64
	CacheCreate   int64
	TotalCostUSD  float64
	LatencyMS     int64
	StatusCode    int64
	ErrorMessage  string
	CreatedAt     int64
}

// ModelRow is a per-model aggregate for today.
type ModelRow struct {
	Model        string
	Requests     int64
	InputTokens  int64
	OutputTokens int64
	TotalCostUSD float64
}

// SeriesRow is one per-request sample (raw columns) for the chart/sparkline.
type SeriesRow struct {
	CreatedAt    int64
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheCreate  int64
}

var errNoRows = errors.New("no rows")

// ListApps returns every app_type that has at least one provider.
func (d *DB) ListApps() ([]string, error) {
	var apps []string
	err := d.query(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT DISTINCT app_type FROM providers ORDER BY app_type`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a string
			if err := rows.Scan(&a); err != nil {
				return err
			}
			apps = append(apps, a)
		}
		return rows.Err()
	})
	return apps, err
}

// providerCols is the shared column list + COALESCE shaping for a Provider row.
const providerCols = `id, COALESCE(name,''), COALESCE(category,''), COALESCE(website_url,''),
	CAST(cost_multiplier AS REAL),
	CAST(limit_daily_usd AS REAL), CAST(limit_monthly_usd AS REAL),
	COALESCE(settings_config,'{}')`

// scanProvider runs a single-row provider query and scans it into a Provider.
func (d *DB) scanProvider(query string, args ...any) (*Provider, error) {
	p := &Provider{}
	err := d.query(func(db *sql.DB) error {
		row := db.QueryRow(query, args...)
		if err := row.Scan(&p.ID, &p.Name, &p.Category, &p.WebsiteURL,
			&p.CostMultiplier, &p.LimitDailyUSD, &p.LimitMonthlyUSD, &p.SettingsConfig); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNoRows
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ActiveProvider resolves the current provider for an app. Prefer the explicit
// currentID from cc switch's settings.json; only fall back to is_current=1 when
// that exact row is missing. The old `id=? OR is_current=1` merge could pick a
// DIFFERENT provider when currentID pointed at a since-deleted row, attributing
// usage (and sending the token) to the wrong account.
func (d *DB) ActiveProvider(appType, currentID string) (*Provider, error) {
	if currentID != "" {
		if p, err := d.scanProvider(`SELECT `+providerCols+
			` FROM providers WHERE app_type=? AND id=? LIMIT 1`, appType, currentID); err == nil {
			return p, nil
		} else if !errors.Is(err, errNoRows) {
			return nil, err // real error (e.g. locked) — surface it, don't mask with fallback
		}
	}
	p, err := d.scanProvider(`SELECT `+providerCols+
		` FROM providers WHERE app_type=? AND is_current=1 LIMIT 1`, appType)
	if errors.Is(err, errNoRows) {
		return nil, errNoRows
	}
	return p, err
}

// TodayTotals sums live proxy_request_logs in [since, until) unix seconds.
func (d *DB) TodayTotals(appType string, since, until int64) (Totals, error) {
	return d.totalsLogs(`SELECT COUNT(*),
			COALESCE(CAST(SUM(CASE WHEN status_code=200 THEN 1 ELSE 0 END) AS INTEGER),0),
			CAST(COALESCE(SUM(input_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(output_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(cache_read_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(cache_creation_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(total_cost_usd),0) AS REAL)
			FROM proxy_request_logs WHERE app_type=? AND created_at>=? AND created_at<?`,
		appType, since, until)
}

// TodayTotalsAll sums proxy_request_logs across ALL app_types in [since, until).
// Used for the "all apps today" total that matches cc switch's dashboard figure.
func (d *DB) TodayTotalsAll(since, until int64) (Totals, error) {
	t := Totals{}
	err := d.query(func(db *sql.DB) error {
		row := db.QueryRow(`SELECT COUNT(*),
				COALESCE(CAST(SUM(CASE WHEN status_code=200 THEN 1 ELSE 0 END) AS INTEGER),0),
				CAST(COALESCE(SUM(input_tokens),0) AS INTEGER),
				CAST(COALESCE(SUM(output_tokens),0) AS INTEGER),
				CAST(COALESCE(SUM(cache_read_tokens),0) AS INTEGER),
				CAST(COALESCE(SUM(cache_creation_tokens),0) AS INTEGER),
				CAST(COALESCE(SUM(total_cost_usd),0) AS REAL)
				FROM proxy_request_logs WHERE created_at>=? AND created_at<?`, since, until)
		return row.Scan(&t.Requests, &t.Successes, &t.InputTokens, &t.OutputTokens,
			&t.CacheRead, &t.CacheCreate, &t.CostUSD)
	})
	return t, err
}

// MonthTotals sums proxy_request_logs in [since, until) unix seconds. We read
// directly from the logs (not usage_daily_rollups) because cc switch's rollups
// lag and can omit the entire current month; logs retain ~30 days, which always
// covers the current month.
func (d *DB) MonthTotals(appType string, since, until int64) (Totals, error) {
	return d.TodayTotals(appType, since, until)
}

// SessionTotals sums proxy_request_logs for a single session_id.
func (d *DB) SessionTotals(appType, sessionID string) (Totals, error) {
	return d.totalsLogs(`SELECT COUNT(*),
			COALESCE(CAST(SUM(CASE WHEN status_code=200 THEN 1 ELSE 0 END) AS INTEGER),0),
			CAST(COALESCE(SUM(input_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(output_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(cache_read_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(cache_creation_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(total_cost_usd),0) AS REAL)
			FROM proxy_request_logs WHERE app_type=? AND session_id=?`,
		appType, sessionID)
}

// totalsLogs runs a 7-column proxy_request_logs aggregate into Totals.
func (d *DB) totalsLogs(query, appType string, extra ...any) (Totals, error) {
	t := Totals{}
	err := d.query(func(db *sql.DB) error {
		args := append([]any{appType}, extra...)
		row := db.QueryRow(query, args...)
		return row.Scan(&t.Requests, &t.Successes, &t.InputTokens, &t.OutputTokens,
			&t.CacheRead, &t.CacheCreate, &t.CostUSD)
	})
	return t, err
}

// LatestSessionID returns the most recent session_id recorded for an app
// (used for non-Claude apps that have no local session registry).
func (d *DB) LatestSessionID(appType string) (string, error) {
	var sid string
	err := d.query(func(db *sql.DB) error {
		row := db.QueryRow(`SELECT session_id FROM proxy_request_logs
			WHERE app_type=? AND session_id IS NOT NULL AND session_id<>''
			ORDER BY created_at DESC LIMIT 1`, appType)
		if err := row.Scan(&sid); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNoRows
			}
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return sid, nil
}

// LatestRequest returns the most recent log row for an app (nil if none).
func (d *DB) LatestRequest(appType string) (*RequestRow, error) {
	r := &RequestRow{}
	err := d.query(func(db *sql.DB) error {
		row := db.QueryRow(`SELECT COALESCE(model,''), COALESCE(request_model,''),
			CAST(COALESCE(input_tokens,0) AS INTEGER),
			CAST(COALESCE(output_tokens,0) AS INTEGER),
			CAST(COALESCE(cache_read_tokens,0) AS INTEGER),
			CAST(COALESCE(cache_creation_tokens,0) AS INTEGER),
			CAST(COALESCE(total_cost_usd,0) AS REAL),
			CAST(COALESCE(latency_ms,0) AS INTEGER),
			CAST(COALESCE(status_code,0) AS INTEGER),
			COALESCE(error_message,''),
			CAST(created_at AS INTEGER)
			FROM proxy_request_logs WHERE app_type=? ORDER BY created_at DESC LIMIT 1`, appType)
		if err := row.Scan(&r.Model, &r.RequestModel, &r.InputTokens, &r.OutputTokens,
			&r.CacheRead, &r.CacheCreate, &r.TotalCostUSD, &r.LatencyMS, &r.StatusCode,
			&r.ErrorMessage, &r.CreatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNoRows
			}
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// LatestSessionRequest returns the most recent log row for a single session_id
// (nil if none). Used as the live in/out source for the current session — the
// DB is the unified source of truth and stays fresh within the poll interval.
func (d *DB) LatestSessionRequest(appType, sessionID string) (*RequestRow, error) {
	if sessionID == "" {
		return nil, nil
	}
	r := &RequestRow{}
	err := d.query(func(db *sql.DB) error {
		row := db.QueryRow(`SELECT COALESCE(model,''), COALESCE(request_model,''),
			CAST(COALESCE(input_tokens,0) AS INTEGER),
			CAST(COALESCE(output_tokens,0) AS INTEGER),
			CAST(COALESCE(cache_read_tokens,0) AS INTEGER),
			CAST(COALESCE(cache_creation_tokens,0) AS INTEGER),
			CAST(COALESCE(total_cost_usd,0) AS REAL),
			CAST(COALESCE(latency_ms,0) AS INTEGER),
			CAST(COALESCE(status_code,0) AS INTEGER),
			COALESCE(error_message,''),
			CAST(created_at AS INTEGER)
			FROM proxy_request_logs WHERE app_type=? AND session_id=?
			ORDER BY created_at DESC LIMIT 1`, appType, sessionID)
		if err := row.Scan(&r.Model, &r.RequestModel, &r.InputTokens, &r.OutputTokens,
			&r.CacheRead, &r.CacheCreate, &r.TotalCostUSD, &r.LatencyMS, &r.StatusCode,
			&r.ErrorMessage, &r.CreatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNoRows
			}
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// RecentRequests returns per-request samples since `since` (unix seconds),
// oldest first, for the real-time chart. cc switch's session_log rows ARE the
// source; cost is intentionally omitted (chart is token-only).
func (d *DB) RecentRequests(appType string, since int64) ([]SeriesRow, error) {
	var out []SeriesRow
	err := d.query(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT CAST(created_at AS INTEGER),
			CAST(COALESCE(input_tokens,0) AS INTEGER),
			CAST(COALESCE(output_tokens,0) AS INTEGER),
			CAST(COALESCE(cache_read_tokens,0) AS INTEGER),
			CAST(COALESCE(cache_creation_tokens,0) AS INTEGER)
			FROM proxy_request_logs WHERE app_type=? AND created_at>=?
			ORDER BY created_at ASC`, appType, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r SeriesRow
			if err := rows.Scan(&r.CreatedAt, &r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheCreate); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// WeeklyTotals returns per-day token totals (in+out+cacheRead+cacheCreate)
// for the 7-day window starting at sinceUnix, keyed by local calendar date
// ("YYYY-MM-DD", matching SQLite's date(...,'localtime')). Days with no rows
// are absent; callers fill zeros for the 7 expected buckets. One scan replaces
// the old 7-query loop in buildSnapshot. sinceUnix should be startToday-6days.
func (d *DB) WeeklyTotals(appType string, sinceUnix int64) (map[string]int64, error) {
	out := map[string]int64{}
	err := d.query(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT date(created_at,'unixepoch','localtime') AS day,
			CAST(COALESCE(SUM(input_tokens),0)+COALESCE(SUM(output_tokens),0)
			   +COALESCE(SUM(cache_read_tokens),0)+COALESCE(SUM(cache_creation_tokens),0) AS INTEGER)
			FROM proxy_request_logs WHERE app_type=? AND created_at>=?
			GROUP BY day`, appType, sinceUnix)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var day string
			var tok int64
			if err := rows.Scan(&day, &tok); err != nil {
				return err
			}
			out[day] = tok
		}
		return rows.Err()
	})
	return out, err
}

// MultiWindowTotals computes the month / today / 5h / 7d aggregates in a single
// scan of proxy_request_logs using conditional sums. sinceUnix must be the
// earliest of the four bounds (min(monthStart, d7Start)) so every window's rows
// are in range; rows with created_at>=bound contribute to that window. Equivalent
// to four separate TodayTotals/MonthTotals calls (NULL-safe per column).
func (d *DB) MultiWindowTotals(appType string, sinceUnix, monthStart, todayStart, h5Start, d7Start int64) (month, today Totals, h5Sum, d7Sum int64, err error) {
	// 7 conditional aggregates for a window, parameterized by its bound.
	windowCols := func() string {
		return `SUM(CASE WHEN created_at>=? THEN 1 ELSE 0 END),
			SUM(CASE WHEN created_at>=? AND status_code=200 THEN 1 ELSE 0 END),
			CAST(COALESCE(SUM(CASE WHEN created_at>=? THEN input_tokens END),0) AS INTEGER),
			CAST(COALESCE(SUM(CASE WHEN created_at>=? THEN output_tokens END),0) AS INTEGER),
			CAST(COALESCE(SUM(CASE WHEN created_at>=? THEN cache_read_tokens END),0) AS INTEGER),
			CAST(COALESCE(SUM(CASE WHEN created_at>=? THEN cache_creation_tokens END),0) AS INTEGER),
			CAST(COALESCE(SUM(CASE WHEN created_at>=? THEN total_cost_usd END),0) AS REAL)`
	}
	query := "SELECT " + windowCols() + ", " + windowCols() + ", " +
		`CAST(COALESCE(SUM(CASE WHEN created_at>=? THEN COALESCE(input_tokens,0)+COALESCE(output_tokens,0)+COALESCE(cache_read_tokens,0)+COALESCE(cache_creation_tokens,0) END),0) AS INTEGER),` +
		`CAST(COALESCE(SUM(CASE WHEN created_at>=? THEN COALESCE(input_tokens,0)+COALESCE(output_tokens,0)+COALESCE(cache_read_tokens,0)+COALESCE(cache_creation_tokens,0) END),0) AS INTEGER) ` +
		"FROM proxy_request_logs WHERE app_type=? AND created_at>=?"
	args := []any{
		// month window (7 bound placeholders)
		monthStart, monthStart, monthStart, monthStart, monthStart, monthStart, monthStart,
		// today window (7 bound placeholders)
		todayStart, todayStart, todayStart, todayStart, todayStart, todayStart, todayStart,
		// 5h and 7d token sums
		h5Start, d7Start,
		// WHERE
		appType, sinceUnix,
	}
	e := d.query(func(db *sql.DB) error {
		row := db.QueryRow(query, args...)
		return row.Scan(
			&month.Requests, &month.Successes, &month.InputTokens, &month.OutputTokens,
			&month.CacheRead, &month.CacheCreate, &month.CostUSD,
			&today.Requests, &today.Successes, &today.InputTokens, &today.OutputTokens,
			&today.CacheRead, &today.CacheCreate, &today.CostUSD,
			&h5Sum, &d7Sum,
		)
	})
	if e != nil {
		err = e
	}
	return month, today, h5Sum, d7Sum, err
}

// PerModelToday groups today's logs (since unix seconds) by model.
func (d *DB) PerModelToday(appType string, since int64) ([]ModelRow, error) {
	var out []ModelRow
	err := d.query(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT COALESCE(model,'(unknown)'),
			COUNT(*),
			CAST(COALESCE(SUM(input_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(output_tokens),0) AS INTEGER),
			CAST(COALESCE(SUM(total_cost_usd),0) AS REAL)
			FROM proxy_request_logs WHERE app_type=? AND created_at>=?
			GROUP BY model ORDER BY 5 DESC`, appType, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m ModelRow
			if err := rows.Scan(&m.Model, &m.Requests, &m.InputTokens, &m.OutputTokens, &m.TotalCostUSD); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}
