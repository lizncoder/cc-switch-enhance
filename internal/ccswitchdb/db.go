// Package ccswitchdb opens cc switch's SQLite store READ-ONLY and runs the
// usage queries. It never writes — cc switch owns the database.
package ccswitchdb

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// DB wraps a read-only connection to cc-switch.db.
type DB struct {
	path      string
	db        *sql.DB
	cols      map[string]bool // "table.col" -> present (proxy_request_logs + providers)
	schemaErr error           // non-nil if required tables/columns are missing
}

// Open opens the database read-only with a busy timeout. The connection is
// limited to a single opener to minimize lock contention with cc switch's writer.
func Open(path string) (*DB, error) {
	dsn := "file:" + toURIPath(path) + "?mode=ro&_busy_timeout=3000&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	d := &DB{path: path, db: db}
	d.probeSchema()
	return d, nil
}

// requiredLogCols / requiredProviderCols are every column the queries in
// queries.go SELECT or filter on. If cc-switch renames or drops any, every
// query fails; we detect it once here so buildSnapshot shows a single clear
// error instead of a wall of per-query chips.
var requiredLogCols = []string{
	"app_type", "session_id", "created_at", "status_code",
	"input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens",
	"total_cost_usd", "model", "request_model", "latency_ms", "error_message",
}
var requiredProviderCols = []string{
	"id", "name", "category", "website_url", "cost_multiplier",
	"limit_daily_usd", "limit_monthly_usd", "settings_config", "is_current", "app_type",
}

// SchemaError returns nil when the cc-switch schema has every table/column the
// queries need, or an error describing what's missing/wrong otherwise.
func (d *DB) SchemaError() error {
	if d == nil {
		return nil
	}
	return d.schemaErr
}

// probeSchema runs once at Open: records which columns exist and, if any
// required column/table is missing, sets schemaErr so callers can degrade
// gracefully instead of running queries that will all fail.
func (d *DB) probeSchema() {
	d.cols = map[string]bool{}
	want := map[string][]string{
		"proxy_request_logs": requiredLogCols,
		"providers":          requiredProviderCols,
	}
	for table := range want {
		_ = d.query(func(db *sql.DB) error {
			rows, err := db.Query("PRAGMA table_info(" + table + ")")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var cid int
				var name, ctype string
				var notnull, pk int
				var dflt sql.NullString
				if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
					return err
				}
				d.cols[table+"."+name] = true
			}
			return rows.Err()
		})
	}
	// A missing table yields zero columns for it — report the table as absent
	// rather than listing every column as missing.
	for table, cols := range want {
		present := 0
		for _, c := range cols {
			if d.cols[table+"."+c] {
				present++
			}
		}
		if present == 0 {
			d.schemaErr = fmt.Errorf("cc-switch 库缺少表 %s（请确认 cc-switch 已安装/升级）", table)
			return
		}
	}
	var missing []string
	for table, cols := range want {
		for _, c := range cols {
			if !d.cols[table+"."+c] {
				missing = append(missing, table+"."+c)
			}
		}
	}
	if len(missing) > 0 {
		d.schemaErr = fmt.Errorf("cc-switch 库表结构不兼容（缺少 %s），请升级 cc-enhance", strings.Join(missing, ", "))
	}
}

// Close releases the connection.
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// query runs fn against the pool, retrying on transient "database is locked"
// errors with a short backoff (cc switch may be mid-write).
func (d *DB) query(fn func(*sql.DB) error) error {
	if d == nil || d.db == nil {
		return errNoDB
	}
	backoffs := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	var last error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		err := fn(d.db)
		if err == nil {
			return nil
		}
		last = err
		if !isLocked(err) {
			return err
		}
		if attempt < len(backoffs) {
			time.Sleep(backoffs[attempt])
		}
	}
	return last
}

func isLocked(err error) bool {
	if err == nil {
		return false
	}
	// Prefer the typed driver error code over string matching (which is
	// locale-fragile). SQLITE_BUSY=5, SQLITE_LOCKED=6; mask the extended bits.
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case 5, 6:
			return true
		}
		return false
	}
	// Defensive fallback for any non-typed error.
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "SQLITE_LOCKED")
}

// toURIPath converts a native Windows path into a SQLite URI path component
// (forward slashes), e.g. C:\Users\a\db.sqlite -> C:/Users/a/db.sqlite.
func toURIPath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

var errNoDB = &dbError{"cc-switch database not open"}

type dbError struct{ msg string }

func (e *dbError) Error() string { return e.msg }
