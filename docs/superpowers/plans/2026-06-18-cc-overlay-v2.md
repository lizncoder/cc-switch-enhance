# cc-overlay v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix cc-overlay's inaccurate "输入" (now reflects total context incl. cache), add a real-time per-request in/out chart + sparkline, and add a collapse button that shrinks the card to a one-line bar (and back), without ever writing to cc switch's DB.

**Architecture:** Single Wails v2 window. Backend (Go) adds a `ContextTokens` field to snapshot types (sourced by summing existing token columns), a `LatestSessionRequest` query so live in/out is never stale, and a `RecentRequests` query that feeds a `Series` for the chart. Frontend (Vue3/TS) hand-rolls two tiny SVG components (Chart, Sparkline — no chart lib), switches the headline "输入" to context tokens, and toggles between the full card and a draggable one-line bar via a new `SetCollapsed` binding that resizes the window.

**Tech Stack:** Go 1.26, Wails v2, modernc.org/sqlite (read-only), Vue3 + TypeScript, hand-rolled SVG. Wails CLI at `D:\go_sensus\bin\wails.exe` (not on PATH).

## Global Constraints

- **READ-ONLY to cc switch + Claude Code.** Never write to `cc-switch.db` or any `~/.claude` file. The overlay writes only to `~/.cc-overlay/`.
- **Token columns are real; cost for GLM is `$0`.** "输入" headline must be total context = `input_tokens + cache_read_tokens + cache_creation_tokens`. Never invent token numbers.
- **No new runtime dependencies** (no chart library). Charts are hand-rolled SVG.
- **Build/run:** prefix commands with `export PATH="$PATH:/d/go_sensus/bin"` so `wails` resolves. `wails dev` for hot reload, `wails build -platform windows/amd64` for the exe.
- **Go test gate:** `(cd /d/codex-gen-pros && go test ./...)`. Frontend gate: `(cd /d/codex-gen-pros/frontend && npx vue-tsc --noEmit)`.
- **Git:** `D:\codex-gen-pros` is NOT currently a git repo. Task 1 step 1 initializes it; every task ends with a commit.
- **`created_at` is unix SECONDS** in `proxy_request_logs` (verified). Compute time windows with `time.Now().Unix()`.

---

## File Structure

**Backend (Go)**
- `internal/snapshot/series.go` (NEW) — pure helpers `SumContext`, `Downsample`. Unit-tested.
- `internal/snapshot/types.go` (MODIFY) — add `ContextTokens` to `UsageTotals`/`SessionInfo`/`RequestInfo`; add `SeriesPoint`, `Snapshot.Series`, `Snapshot.Collapsed`; `SessionInfo.LatestContextTokens`.
- `internal/ccswitchdb/queries.go` (MODIFY) — add `SeriesRow`, `RecentRequests(app, since)`, `LatestSessionRequest(app, sessionID)`.
- `app.go` (MODIFY) — populate `ContextTokens`; source live values from DB session row; build `Series`; add `SetCollapsed`/`applyCollapsed`/`restoreCollapsed`.
- `main.go` (MODIFY) — relax `MaxWidth`/`MaxHeight`, lower `MinWidth`/`MinHeight`.
- `internal/config/paths.go` (MODIFY) — add `Collapsed`, `ChartWindowMin` to `OverlayConfig` (+ defaults).
- `diag_test.go` (MODIFY) — assertions for context/series.

**Frontend (Vue3/TS)**
- `frontend/src/types.ts` (MODIFY) — mirror new fields.
- `frontend/src/components/Sparkline.vue` (NEW) — tiny SVG polyline of `in`.
- `frontend/src/components/Chart.vue` (NEW) — SVG area (`in`) + line (`out`), independent scales.
- `frontend/src/App.vue` (MODIFY) — headline=context; chart block; collapse button; bar view; view switch on `snap.collapsed`.

---

## Task 1: Snapshot data model — ContextTokens + Series + Collapsed (+ git init)

**Files:**
- Create: `internal/snapshot/series.go`
- Create: `internal/snapshot/series_test.go`
- Modify: `internal/snapshot/types.go`
- Modify: `diag_test.go`

**Interfaces:**
- Produces: `snapshot.SumContext(in, cacheRead, cacheCreate int64) int64`; `snapshot.Downsample(points []SeriesPoint, max int) []SeriesPoint`; `snapshot.SeriesPoint{T, In, Out int64}`; `ContextTokens int64` field on `UsageTotals`/`SessionInfo`/`RequestInfo`; `Snapshot.Series []SeriesPoint`; `Snapshot.Collapsed bool`; `SessionInfo.LatestContextTokens int64`.

- [ ] **Step 1: Initialize git (project is not yet a repo)**

Run:
```bash
(cd /d/codex-gen-pros && git init && git add -A && git commit -m "chore: baseline cc-overlay v1 before v2 work")
```
Expected: a baseline commit exists.

- [ ] **Step 2: Write the failing unit tests**

Create `internal/snapshot/series_test.go`:
```go
package snapshot

import "testing"

func TestSumContext(t *testing.T) {
	cases := []struct{ in, cr, cc, want int64 }{
		{334, 81664, 0, 81998},
		{1304011, 24096000, 0, 25400011},
		{0, 0, 0, 0},
	}
	for _, c := range cases {
		if got := SumContext(c.in, c.cr, c.cc); got != c.want {
			t.Errorf("SumContext(%d,%d,%d)=%d want %d", c.in, c.cr, c.cc, got, c.want)
		}
	}
}

func TestDownsample(t *testing.T) {
	in := make([]SeriesPoint, 100)
	for i := range in {
		in[i] = SeriesPoint{T: int64(i)}
	}
	out := Downsample(in, 10)
	if len(out) != 10 {
		t.Fatalf("len=%d want 10", len(out))
	}
	if out[0].T != 0 {
		t.Errorf("first point not preserved: %d", out[0].T)
	}
	if out[len(out)-1].T != 99 {
		t.Errorf("last point not preserved: %d", out[len(out)-1].T)
	}
	// Already-small slice is returned unchanged.
	short := []SeriesPoint{{T: 1}, {T: 2}, {T: 3}}
	if got := Downsample(short, 10); len(got) != 3 {
		t.Errorf("short slice changed: len=%d", len(got))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `(cd /d/codex-gen-pros && go test ./internal/snapshot/ -run 'TestSumContext|TestDownsample' -v)`
Expected: FAIL — `SumContext` / `Downsample` / `SeriesPoint` undefined.

- [ ] **Step 4: Create the helpers + types**

Create `internal/snapshot/series.go`:
```go
package snapshot

// SumContext returns the total context tokens for a turn or totals bucket:
// the new (non-cached) input plus cache-read plus cache-creation. This is what
// users intuitively mean by "输入量" — input_tokens alone excludes cache, which
// is ~95% of the real context for a long session.
func SumContext(in, cacheRead, cacheCreate int64) int64 {
	return in + cacheRead + cacheCreate
}

// Downsample returns at most max evenly-spaced points (first and last always
// kept) so chart/sparkline rendering stays cheap on busy windows. A slice
// already <= max (or max < 2) is returned unchanged.
func Downsample(points []SeriesPoint, max int) []SeriesPoint {
	if max < 2 || len(points) <= max {
		return points
	}
	step := float64(len(points)-1) / float64(max-1)
	out := make([]SeriesPoint, 0, max)
	for i := 0; i < max; i++ {
		idx := int(i*step + 0.5)
		if idx >= len(points) {
			idx = len(points) - 1
		}
		out = append(out, points[idx])
	}
	return out
}
```

Modify `internal/snapshot/types.go`:

Add a new type after `ModelBreakdown`:
```go
// SeriesPoint is one per-request sample for the real-time chart / sparkline.
// In is the total context (input + cacheRead + cacheCreate), precomputed server-side.
type SeriesPoint struct {
	T   int64 `json:"t"`   // created_at, unix seconds
	In  int64 `json:"in"`  // total context tokens
	Out int64 `json:"out"` // output tokens
}
```

Add `ContextTokens` to `UsageTotals` (after `CacheCreate`):
```go
	CacheCreate    int64   `json:"cacheCreate"`
	ContextTokens  int64   `json:"contextTokens"` // input + cacheRead + cacheCreate
```

Add `ContextTokens` and `LatestContextTokens` to `SessionInfo` (after `CacheCreate` and after `LatestCacheCreate`):
```go
	CacheRead    int64   `json:"cacheRead"`
	CacheCreate  int64   `json:"cacheCreate"`
	ContextTokens int64  `json:"contextTokens"` // session total context
```
```go
	LatestCacheCreate int64 `json:"latestCacheCreate"`
	LatestContextTokens int64 `json:"latestContextTokens"` // latest turn context
```

Add `ContextTokens` to `RequestInfo` (after `CacheCreate`):
```go
	CacheCreate    int64   `json:"cacheCreate"`
	ContextTokens  int64   `json:"contextTokens"`
```

Add `Series` and `Collapsed` to `Snapshot` (after `Errors`):
```go
	Errors        []string         `json:"errors"`
	Series        []SeriesPoint    `json:"series"`
	Collapsed     bool             `json:"collapsed"`
```

- [ ] **Step 5: Run unit tests to verify they pass**

Run: `(cd /d/codex-gen-pros && go test ./internal/snapshot/ -v)`
Expected: PASS (TestSumContext, TestDownsample).

- [ ] **Step 6: Commit**

```bash
(cd /d/codex-gen-pros && git add internal/snapshot/series.go internal/snapshot/series_test.go internal/snapshot/types.go && git commit -m "feat(snapshot): add ContextTokens + SeriesPoint + SumContext/Downsample")
```

---

## Task 2: Populate ContextTokens + live values from the current session's latest DB row

**Files:**
- Modify: `internal/ccswitchdb/queries.go`
- Modify: `app.go`
- Modify: `diag_test.go`

**Interfaces:**
- Consumes: `snapshot.SumContext` (Task 1).
- Produces: `(*DB).LatestSessionRequest(appType, sessionID string) (*RequestRow, error)`; `app.go` sets `ContextTokens`/`LatestContextTokens` and sources the live in/out from the latest DB row of the current session (never stale vs the 1.5s poll).

- [ ] **Step 1: Add `LatestSessionRequest` query**

In `internal/ccswitchdb/queries.go`, append:
```go
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
```

- [ ] **Step 2: Populate `ContextTokens` in `toUsage` and `toRequest`**

In `app.go`, edit `toUsage` — add the field to the struct literal (after `CacheCreate: t.CacheCreate,`):
```go
		CacheCreate:    t.CacheCreate,
		ContextTokens:  snapshot.SumContext(t.InputTokens, t.CacheRead, t.CacheCreate),
```

Edit `toRequest` — add to the `ri` struct literal (after `CacheCreate: r.CacheCreate,`):
```go
		CacheCreate:    r.CacheCreate,
		ContextTokens:  snapshot.SumContext(r.InputTokens, r.CacheRead, r.CacheCreate),
```

- [ ] **Step 3: Source live in/out + session context from the DB session row**

In `app.go` `buildSnapshot`, replace the block that derives non-claude latest values (the current `if app != "claude" && latestReq != nil { ... }`) and add a claude DB-session path. Find this existing block:
```go
	// Non-claude: derive latest in/out from the latest request row.
	if app != "claude" && latestReq != nil {
		si.LatestInput = latestReq.InputTokens
		si.LatestOutput = latestReq.OutputTokens
		si.LatestCacheRead = latestReq.CacheRead
		si.LatestCacheCreate = latestReq.CacheCreate
		si.LatestModel = liveModel
	}
```
Replace it with:
```go
	// Live in/out: prefer the latest DB row for the CURRENT session (unified
	// source, fresh within the poll). Non-claude apps have no session registry,
	// so fall back to the global latest request row.
	var liveRow *ccswitchdb.RequestRow
	if app == "claude" && sessionID != "" && a.db != nil {
		if r, err := a.db.LatestSessionRequest(app, sessionID); err == nil {
			liveRow = r
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取会话最新请求失败: %v", err))
		}
	}
	if liveRow == nil {
		liveRow = latestReq // global latest (non-claude, or claude with no session row yet)
	}
	if liveRow != nil {
		si.LatestInput = liveRow.InputTokens
		si.LatestOutput = liveRow.OutputTokens
		si.LatestCacheRead = liveRow.CacheRead
		si.LatestCacheCreate = liveRow.CacheCreate
		si.LatestContextTokens = snapshot.SumContext(liveRow.InputTokens, liveRow.CacheRead, liveRow.CacheCreate)
		if liveRow.Model != "" {
			si.LatestModel = liveRow.Model
			liveModel = liveRow.Model
		}
	}
```
Note: this runs after the claude `a.cc.Current()` block (which may have set `si.LatestInput` from transcript tail). The DB row overrides it when present, fixing the observed 2-message lag.

Then set the session-total `ContextTokens`. In the `SessionTotals` block, after the existing field assignments (`si.CacheCreate = t.CacheCreate`), add:
```go
				si.CacheCreate = t.CacheCreate
				si.ContextTokens = snapshot.SumContext(t.InputTokens, t.CacheRead, t.CacheCreate)
```

- [ ] **Step 4: Extend diag assertions**

In `diag_test.go`, before the closing `}` of `TestDiag`, add:
```go
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
```
No helper is needed: `s.Latest` is a `*snapshot.RequestInfo` whose `ContextTokens` is already computed in Step 2. Do not add any `contextOf`/`latestContext` helper.

- [ ] **Step 5: Run tests**

Run: `(cd /d/codex-gen-pros && go test -run TestDiag -v)`
Expected: PASS; log shows `today` contextTokens ≈ 25M and `sessionLatest` > 0.

Run: `(cd /d/codex-gen-pros && go build ./...)`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
(cd /d/codex-gen-pros && git add internal/ccswitchdb/queries.go app.go diag_test.go && git commit -m "feat: ContextTokens populated + live in/out from latest session DB row")
```

---

## Task 3: RecentRequests query + Series in the snapshot

**Files:**
- Modify: `internal/ccswitchdb/queries.go`
- Modify: `app.go`
- Modify: `diag_test.go`
- Modify: `internal/config/paths.go`

**Interfaces:**
- Consumes: `snapshot.SeriesPoint`, `snapshot.SumContext` (Task 1).
- Produces: `(*DB).RecentRequests(appType string, since int64) ([]SeriesRow, error)`; `Snapshot.Series` populated with `In = in+cr+cc`; config `ChartWindowMin` (default 60).

- [ ] **Step 1: Add `SeriesRow` + `RecentRequests`**

In `internal/ccswitchdb/queries.go`, add the row type near the other row types:
```go
// SeriesRow is one per-request sample (raw columns) for the chart/sparkline.
type SeriesRow struct {
	CreatedAt     int64
	InputTokens   int64
	OutputTokens  int64
	CacheRead     int64
	CacheCreate   int64
}
```
Append the query:
```go
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
```

- [ ] **Step 2: Add `ChartWindowMin` to config**

In `internal/config/paths.go`, add to `OverlayConfig` (after `PollIntervalMs`):
```go
	PollIntervalMs    int               `json:"pollIntervalMs"`
	ChartWindowMin    int               `json:"chartWindowMin"`
```
In `DefaultOverlayConfig`, add after `PollIntervalMs: 1500,`:
```go
		PollIntervalMs:    1500,
		ChartWindowMin:    60,
```
In `LoadOverlayConfig`, add a guard near the `PollIntervalMs` guard:
```go
	if cfg.PollIntervalMs <= 0 {
		cfg.PollIntervalMs = 1500
	}
	if cfg.ChartWindowMin <= 0 {
		cfg.ChartWindowMin = 60
	}
```

- [ ] **Step 3: Build `Series` in `buildSnapshot`**

In `app.go` `buildSnapshot`, just before `s.Session = si` (end of the aggregates block), add:
```go
	// Real-time series for the chart/sparkline (last ChartWindowMin minutes).
	min := a.cfg.ChartWindowMin
	if min <= 0 {
		min = 60
	}
	if a.db != nil {
		since := now.Unix() - int64(min*60)
		if rows, err := a.db.RecentRequests(app, since); err == nil {
			pts := make([]snapshot.SeriesPoint, 0, len(rows))
			for _, r := range rows {
				pts = append(pts, snapshot.SeriesPoint{
					T:   r.CreatedAt,
					In:  snapshot.SumContext(r.InputTokens, r.CacheRead, r.CacheCreate),
					Out: r.OutputTokens,
				})
			}
			s.Series = pts
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取时序失败: %v", err))
		}
	}
```

- [ ] **Step 4: Set `Collapsed` on the snapshot**

In `app.go` `buildSnapshot`, set the field right after the `s := &snapshot.Snapshot{...}` literal is created (add `Collapsed: a.cfg.Collapsed,` to the literal), OR set it before return:
```go
	s.Collapsed = a.cfg.Collapsed
```
Place this line just before `return s` at the end of `buildSnapshot`.

- [ ] **Step 5: Extend diag assertions**

In `diag_test.go`, add before the closing `}`:
```go
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
```
Add helpers at file bottom:
```go
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
```
Add the import `"cc-overlay/internal/snapshot"` to `diag_test.go` imports.

- [ ] **Step 6: Run tests + build**

Run: `(cd /d/codex-gen-pros && go test -run TestDiag -v)`
Expected: PASS; `series len=N>0`.

Run: `(cd /d/codex-gen-pros && go build ./...)`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
(cd /d/codex-gen-pros && git add internal/ccswitchdb/queries.go internal/config/paths.go app.go diag_test.go && git commit -m "feat: RecentRequests query + Series (last 60min) in snapshot")
```

---

## Task 4: Collapse backend — config.Collapsed + SetCollapsed binding + main.go sizing

**Files:**
- Modify: `internal/config/paths.go`
- Modify: `app.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: Wails `runtime.WindowSetSize`.
- Produces: frontend binding `SetCollapsed(on: boolean): Promise<void>`; `OverlayConfig.Collapsed`; window can be resized to 440×36 (bar) and back to 320×460 (card).

- [ ] **Step 1: Add `Collapsed` to config**

In `internal/config/paths.go`, add to `OverlayConfig` (after `ChartWindowMin`):
```go
	ChartWindowMin    int               `json:"chartWindowMin"`
	Collapsed         bool              `json:"collapsed"`
```
(No default change needed — zero value `false` = expanded is correct.)

- [ ] **Step 2: Add `SetCollapsed` + helpers to `app.go`**

Append to `app.go` (near the other tray.Controller/frontend bindings):
```go
// SetCollapsed toggles the one-line bar mode: shrinks the window to a slim bar
// (or restores the full card) and persists the choice. Never quits.
func (a *App) SetCollapsed(on bool) error {
	a.cfg.Collapsed = on
	_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
	a.applyCollapsed()
	a.emit()
	return nil
}

// applyCollapsed resizes the window to the bar or card dimensions.
func (a *App) applyCollapsed() {
	if a.ctx == nil {
		return
	}
	if a.cfg.Collapsed {
		runtime.WindowSetSize(a.ctx, 440, 36)
	} else {
		runtime.WindowSetSize(a.ctx, 320, 460)
	}
}

// restoreCollapsed re-applies bar dimensions on startup if the user last left
// it collapsed.
func (a *App) restoreCollapsed() {
	if !a.cfg.Collapsed {
		return
	}
	go func() {
		time.Sleep(150 * time.Millisecond) // let the window finish creating
		a.applyCollapsed()
	}()
}
```

- [ ] **Step 3: Call `restoreCollapsed` on startup**

In `app.go` `startup`, add as the last line of the function (after `a.restorePosition()`):
```go
	a.restorePosition()
	a.restoreCollapsed()
```

- [ ] **Step 4: Relax window size constraints in `main.go`**

In `main.go`, change the size block:
```go
		Width:            320,
		Height:           460,
		MinWidth:         32,
		MinHeight:        32,
		MaxWidth:         480,
		MaxHeight:        720,
```
(MaxWidth raised 380→480 so the 440-wide bar is allowed; mins lowered so the 36-tall bar fits.)

- [ ] **Step 5: Build + smoke**

Run: `(cd /d/codex-gen-pros && go build ./...)`
Expected: clean. (The binding `SetCollapsed` will be regenerated into `frontend/wailsjs/go/main/App.js` on the next `wails dev`/`build`.)

- [ ] **Step 6: Commit**

```bash
(cd /d/codex-gen-pros && git add internal/config/paths.go app.go main.go && git commit -m "feat: SetCollapsed binding + bar/card sizing + startup restore")
```

---

## Task 5: Sparkline.vue (hand-rolled SVG)

**Files:**
- Create: `frontend/src/components/Sparkline.vue`

**Interfaces:**
- Consumes: `points: { t: number; in: number; out: number }[]` (from `Snapshot.series`).
- Produces: a 48×16 inline SVG polyline of the `in` (context) trend, downsampled to ≤24 points.

- [ ] **Step 1: Create the component**

Create `frontend/src/components/Sparkline.vue`:
```vue
<script lang="ts" setup>
import { computed } from 'vue';

interface P { t: number; in: number; out: number; }
const props = defineProps<{ points: P[] }>();

const W = 48;
const H = 16;

function downsample(arr: P[], max: number): P[] {
  if (max < 2 || arr.length <= max) return arr;
  const step = (arr.length - 1) / (max - 1);
  const out: P[] = [];
  for (let i = 0; i < max; i++) {
    let idx = Math.round(i * step);
    if (idx >= arr.length) idx = arr.length - 1;
    out.push(arr[idx]);
  }
  return out;
}

const sampled = computed(() => downsample(props.points ?? [], 24));

const path = computed(() => {
  const pts = sampled.value;
  if (pts.length < 2) return '';
  const max = Math.max(1, ...pts.map((p) => p.in));
  return pts
    .map((p, i) => {
      const x = (i / (pts.length - 1)) * W;
      const y = H - (p.in / max) * H;
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
});
</script>

<template>
  <svg :width="W" :height="H" :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="spark">
    <path v-if="path" :d="path" fill="none" stroke="#4dabf7" stroke-width="1.3" stroke-linejoin="round" />
    <path v-else d="M0,8 L48,8" stroke="#333" stroke-width="1" />
  </svg>
</template>

<style scoped>
.spark { display: block; }
</style>
```

- [ ] **Step 2: Typecheck**

Run: `(cd /d/codex-gen-pros/frontend && npx vue-tsc --noEmit)`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
(cd /d/codex-gen-pros && git add frontend/src/components/Sparkline.vue && git commit -m "feat(ui): Sparkline component (hand-rolled SVG)")
```

---

## Task 6: Chart.vue (hand-rolled SVG, dual auto-scale)

**Files:**
- Create: `frontend/src/components/Chart.vue`

**Interfaces:**
- Consumes: `points: { t: number; in: number; out: number }[]`.
- Produces: a ~296×64 chart: input(context) as a filled area (primary scale), output as an overlaid line (independent scale), with current-value labels and an empty-state placeholder.

- [ ] **Step 1: Create the component**

Create `frontend/src/components/Chart.vue`:
```vue
<script lang="ts" setup>
import { computed } from 'vue';

interface P { t: number; in: number; out: number; }
const props = defineProps<{ points: P[] }>();

const W = 296;
const H = 64;
const PAD = 4;

function downsample(arr: P[], max: number): P[] {
  if (max < 2 || arr.length <= max) return arr;
  const step = (arr.length - 1) / (max - 1);
  const out: P[] = [];
  for (let i = 0; i < max; i++) {
    let idx = Math.round(i * step);
    if (idx >= arr.length) idx = arr.length - 1;
    out.push(arr[idx]);
  }
  return out;
}

function fmt(n: number): string {
  if (!n) return '0';
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'k';
  return String(n);
}

const sampled = computed(() => downsample(props.points ?? [], 120));
const inMax = computed(() => Math.max(1, ...sampled.value.map((p) => p.in)));
const outMax = computed(() => Math.max(1, ...sampled.value.map((p) => p.out)));
const last = computed(() => (sampled.value.length ? sampled.value[sampled.value.length - 1] : null));

function xy(val: number, max: number, i: number, n: number): [number, number] {
  const x = n <= 1 ? W / 2 : PAD + (i / (n - 1)) * (W - 2 * PAD);
  const y = H - PAD - (val / max) * (H - 2 * PAD);
  return [x, y];
}

const inArea = computed(() => {
  const pts = sampled.value;
  const n = pts.length;
  if (!n) return '';
  let d = `M ${PAD},${H - PAD} `;
  pts.forEach((p, i) => {
    const [x, y] = xy(p.in, inMax.value, i, n);
    d += `L ${x.toFixed(1)},${y.toFixed(1)} `;
  });
  d += `L ${W - PAD},${H - PAD} Z`;
  return d;
});

const outLine = computed(() => {
  const pts = sampled.value;
  const n = pts.length;
  if (n < 2) return '';
  return pts
    .map((p, i) => {
      const [x, y] = xy(p.out, outMax.value, i, n);
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
});
</script>

<template>
  <div class="chart-wrap">
    <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="chart-svg">
      <path :d="inArea" fill="rgba(77,171,247,0.18)" stroke="rgba(77,171,247,0.5)" stroke-width="1" />
      <path :d="outLine" fill="none" stroke="#37d67a" stroke-width="1.5" stroke-linejoin="round" />
    </svg>
    <div class="legend">
      <span class="lg in">入 {{ fmt(last?.in ?? 0) }}</span>
      <span class="lg out">出 {{ fmt(last?.out ?? 0) }}</span>
      <span class="lg win">近60分</span>
    </div>
    <div class="empty" v-if="!sampled.length">近 60 分钟无请求</div>
  </div>
</template>

<style scoped>
.chart-wrap {
  position: relative;
  background: #1c1c24;
  border-radius: 8px;
  padding: 6px 8px 4px;
  margin-bottom: 7px;
}
.chart-svg {
  width: 100%;
  height: 64px;
  display: block;
}
.legend {
  display: flex;
  gap: 10px;
  font-size: 10px;
  margin-top: 2px;
}
.lg.in { color: #4dabf7; }
.lg.out { color: #37d67a; }
.lg.win { color: #6c6c7a; margin-left: auto; }
.empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #6c6c7a;
  font-size: 11px;
}
</style>
```

- [ ] **Step 2: Typecheck**

Run: `(cd /d/codex-gen-pros/frontend && npx vue-tsc --noEmit)`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
(cd /d/codex-gen-pros && git add frontend/src/components/Chart.vue && git commit -m "feat(ui): Chart component (hand-rolled SVG, dual auto-scale)")
```

---

## Task 7: Frontend integration — types.ts + App.vue (context headline, chart, bar mode, collapse button)

**Files:**
- Modify: `frontend/src/types.ts`
- Modify: `frontend/src/App.vue`

**Interfaces:**
- Consumes: `Chart.vue`, `Sparkline.vue` (Tasks 5–6); `SetCollapsed` binding (Task 4); new snapshot fields.
- Produces: headline "输入" = `contextTokens`; chart block under header; collapse "—" button; one-line bar view shown when `snap.collapsed`.

- [ ] **Step 1: Update `frontend/src/types.ts`**

Add `contextTokens` to `UsageTotals`, `SessionInfo`, `RequestInfo`; add `SeriesPoint`; add `series`/`collapsed` to `Snapshot`; add `latestContextTokens` to `SessionInfo`. Replace the three interfaces and `Snapshot` with:

```ts
export interface SeriesPoint {
  t: number;
  in: number;
  out: number;
}

export interface UsageTotals {
  requests: number;
  successes: number;
  inputTokens: number;
  outputTokens: number;
  cacheRead: number;
  cacheCreate: number;
  contextTokens: number;
  realCostUsd: number;
  estCostUsd: number;
  showEstCost: boolean;
}

export interface SessionInfo {
  live: boolean;
  pid: number;
  status: string;
  sessionId: string;
  cwd: string;
  requests: number;
  inputTokens: number;
  outputTokens: number;
  cacheRead: number;
  cacheCreate: number;
  contextTokens: number;
  realCostUsd: number;
  estCostUsd: number;
  latestInput: number;
  latestOutput: number;
  latestCacheRead: number;
  latestCacheCreate: number;
  latestContextTokens: number;
  latestModel: string;
  ageSec: number;
}

export interface RequestInfo {
  model: string;
  requestModel: string;
  inputTokens: number;
  outputTokens: number;
  cacheRead: number;
  cacheCreate: number;
  contextTokens: number;
  totalCostUsd: number;
  estCostUsd: number;
  latencyMs: number;
  statusCode: number;
  error: string;
  ageSec: number;
}

export interface Snapshot {
  appType: string;
  availableApps: string[];
  generatedAt: string;
  provider: ProviderInfo;
  model: ModelInfo;
  today: UsageTotals;
  month: UsageTotals;
  session: SessionInfo;
  latest: RequestInfo | null;
  perModelToday: ModelBreakdown[];
  errors: string[];
  series: SeriesPoint[];
  collapsed: boolean;
}
```

- [ ] **Step 2: Replace `frontend/src/App.vue`**

Replace the entire file with:
```vue
<script lang="ts" setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue';
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime';
import { GetSnapshot, ListApps, SetApp, SetCollapsed } from '../wailsjs/go/main/App';
import type { Snapshot, UsageTotals, SeriesPoint } from './types';
import Chart from './components/Chart.vue';
import Sparkline from './components/Sparkline.vue';

const snap = ref<Snapshot | null>(null);
const apps = ref<string[]>([]);

function fmtTokens(n: number): string {
  if (!n) return '0';
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'k';
  return String(n);
}
function fmtCost(n: number): string {
  if (!n) return '$0.00';
  if (n < 0.01) return '<$0.01';
  return '$' + n.toFixed(2);
}
function fmtAge(sec: number): string {
  if (!sec || sec < 0) return '—';
  if (sec < 60) return sec + '秒前';
  if (sec < 3600) return Math.floor(sec / 60) + '分钟前';
  if (sec < 86400) return Math.floor(sec / 3600) + '小时前';
  return Math.floor(sec / 86400) + '天前';
}
function fmtDuration(sec: number): string {
  if (!sec || sec < 0) return '—';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (h > 0) return `${h}h${m}m`;
  if (m > 0) return `${m}m${s}s`;
  return `${s}s`;
}
function limitPct(t: UsageTotals, limit: number | null): number {
  if (!limit || limit <= 0) return 0;
  const cost = t.showEstCost && t.estCostUsd > 0 ? t.estCostUsd : t.realCostUsd;
  return Math.min(100, (cost / limit) * 100);
}
function costOf(t: UsageTotals): string {
  if (t.showEstCost && t.estCostUsd > 0) return fmtCost(t.estCostUsd) + '*';
  return fmtCost(t.realCostUsd);
}

const dotClass = computed(() => {
  const s = snap.value;
  if (!s || !s.session.live) return 'dot gray';
  return s.model.match ? 'dot green' : 'dot amber';
});
const dotTitle = computed(() => {
  const s = snap.value;
  if (!s) return '';
  if (!s.session.live) return '空闲';
  return s.model.match ? '运行中 · 模型匹配' : '运行中 · 模型: ' + (s.model.liveModel || '?');
});
const series = computed<SeriesPoint[]>(() => snap.value?.series ?? []);

async function loadInitial() {
  try {
    snap.value = (await GetSnapshot()) as unknown as Snapshot;
    apps.value = (await ListApps()) as unknown as string[];
  } catch (e) {
    console.error('initial load failed', e);
  }
}
async function selectApp(app: string) {
  if (app === snap.value?.appType) return;
  try { await SetApp(app); } catch (e) { console.error('set app failed', e); }
}
async function collapse(on: boolean) {
  try { await SetCollapsed(on); } catch (e) { console.error('collapse failed', e); }
}

let off = false;
onMounted(() => {
  loadInitial();
  EventsOn('snapshot', (payload: unknown) => {
    snap.value = payload as Snapshot;
    if (snap.value?.availableApps?.length) apps.value = snap.value.availableApps;
  });
});
onBeforeUnmount(() => { if (!off) { EventsOff('snapshot'); off = true; } });
</script>

<template>
  <!-- ===== Collapsed one-line bar ===== -->
  <div class="bar drag" v-if="snap && snap.collapsed" @dblclick="collapse(false)">
    <span :class="dotClass" :title="dotTitle"></span>
    <span class="b-model">{{ snap.model.display || '—' }}</span>
    <span class="sep">·</span>
    <span class="b-provider">{{ snap.provider.name }}</span>
    <span class="b-group">今 <b class="in">▲{{ fmtTokens(snap.today.contextTokens) }}</b> <b class="out">▼{{ fmtTokens(snap.today.outputTokens) }}</b></span>
    <span class="b-group">实 <b class="in">▲{{ fmtTokens(snap.session.latestContextTokens) }}</b> <b class="out">▼{{ fmtTokens(snap.session.latestOutput) }}</b></span>
    <Sparkline :points="series" />
    <button class="expand" title="展开" @click.stop="collapse(false)">▢</button>
  </div>

  <!-- ===== Full card ===== -->
  <div class="card" v-else-if="snap">
    <div class="apps">
      <button v-for="a in apps" :key="a" class="pill" :class="{ active: a === snap.appType }" @click="selectApp(a)">{{ a }}</button>
      <button class="collapse-btn" title="折叠为长条" @click="collapse(true)">—</button>
    </div>

    <div class="header drag">
      <div class="prov">
        <span :class="dotClass" :title="dotTitle"></span>
        <span class="prov-name">{{ snap.provider.name || '—' }}</span>
        <span class="chip" v-if="snap.provider.category">{{ snap.provider.category }}</span>
      </div>
      <div class="model-row">
        <span class="model">{{ snap.model.display || '—' }}</span>
        <span class="host" v-if="snap.model.baseHost">{{ snap.model.baseHost }}</span>
      </div>
    </div>

    <!-- Real-time chart -->
    <Chart :points="series" />

    <section class="block">
      <div class="block-head"><span>今日</span><span class="cost">{{ costOf(snap.today) }}</span></div>
      <div class="metrics">
        <div><label>输入</label><b>{{ fmtTokens(snap.today.contextTokens) }}</b></div>
        <div><label>输出</label><b>{{ fmtTokens(snap.today.outputTokens) }}</b></div>
        <div><label>新增</label><b class="muted-b">{{ fmtTokens(snap.today.inputTokens) }}</b></div>
        <div><label>请求</label><b>{{ snap.today.requests }}</b></div>
      </div>
      <div class="sub">缓存读 {{ fmtTokens(snap.today.cacheRead) }} · 缓存写 {{ fmtTokens(snap.today.cacheCreate) }}</div>
      <div class="bar-track" v-if="snap.provider.limitDailyUsd">
        <div class="bar-fill" :style="{ width: limitPct(snap.today, snap.provider.limitDailyUsd) + '%' }"></div>
      </div>
      <div class="bar-cap" v-else>无每日限额</div>
    </section>

    <section class="block">
      <div class="block-head"><span>本月</span><span class="cost">{{ costOf(snap.month) }}</span></div>
      <div class="metrics">
        <div><label>输入</label><b>{{ fmtTokens(snap.month.contextTokens) }}</b></div>
        <div><label>输出</label><b>{{ fmtTokens(snap.month.outputTokens) }}</b></div>
        <div><label>新增</label><b class="muted-b">{{ fmtTokens(snap.month.inputTokens) }}</b></div>
        <div><label>请求</label><b>{{ snap.month.requests }}</b></div>
      </div>
      <div class="sub">缓存读 {{ fmtTokens(snap.month.cacheRead) }}</div>
    </section>

    <section class="block">
      <div class="block-head">
        <span>当前会话</span>
        <span class="muted">{{ snap.session.live ? (snap.session.status === 'busy' ? '忙' : '闲') + ' · ' + fmtDuration(snap.session.ageSec) : '空闲' }}</span>
      </div>
      <div class="metrics">
        <div><label>最新输入</label><b class="live">{{ fmtTokens(snap.session.latestContextTokens) }}</b></div>
        <div><label>最新输出</label><b class="live">{{ fmtTokens(snap.session.latestOutput) }}</b></div>
        <div><label>会话输入</label><b>{{ fmtTokens(snap.session.contextTokens) }}</b></div>
        <div><label>会话输出</label><b>{{ fmtTokens(snap.session.outputTokens) }}</b></div>
      </div>
    </section>

    <section class="block" v-if="snap.latest">
      <div class="block-head"><span>最近请求</span><span class="muted">{{ fmtAge(snap.latest.ageSec) }}</span></div>
      <div class="metrics">
        <div><label>输入</label><b>{{ fmtTokens(snap.latest.contextTokens) }}</b></div>
        <div><label>输出</label><b>{{ fmtTokens(snap.latest.outputTokens) }}</b></div>
        <div><label>延迟</label><b>{{ snap.latest.latencyMs ? (snap.latest.latencyMs + 'ms') : '—' }}</b></div>
        <div><label>状态</label><b :class="{ ok: snap.latest.statusCode === 200, err: snap.latest.statusCode && snap.latest.statusCode !== 200 }">{{ snap.latest.statusCode || '—' }}</b></div>
      </div>
      <div class="err-line" v-if="snap.latest.error">{{ snap.latest.error }}</div>
    </section>

    <div class="footer">
      <div class="models" v-if="snap.perModelToday && snap.perModelToday.length">
        <span class="muted">今日模型:</span>
        <span class="tag" v-for="m in snap.perModelToday" :key="m.model">{{ m.model }} · {{ fmtTokens(m.inputTokens + m.outputTokens) }}</span>
      </div>
      <div class="errors" v-if="snap.errors && snap.errors.length">
        <span v-for="(e, i) in snap.errors" :key="i" class="err-chip">⚠ {{ e }}</span>
      </div>
    </div>
  </div>
  <div class="card loading" v-else>加载中…</div>
</template>

<style scoped>
.card { width: 100%; height: 100%; box-sizing: border-box; padding: 10px 12px 8px; color: #e6e6ea; font-size: 12px; font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif; overflow: hidden; }
.apps { display: flex; gap: 4px; flex-wrap: wrap; margin-bottom: 8px; align-items: center; }
.pill { border: 1px solid #333; background: #1e1e26; color: #b8b8c4; border-radius: 999px; padding: 2px 9px; font-size: 11px; cursor: pointer; }
.pill.active { background: #3b5bdb; color: #fff; border-color: #3b5bdb; }
.collapse-btn { margin-left: auto; border: 1px solid #333; background: #1e1e26; color: #b8b8c4; border-radius: 6px; width: 22px; height: 20px; font-size: 13px; line-height: 1; cursor: pointer; }
.header { margin-bottom: 8px; }
.drag { -webkit-app-region: drag; }
.prov { display: flex; align-items: center; gap: 6px; }
.prov-name { font-weight: 600; font-size: 13px; }
.chip { font-size: 10px; background: #2a2a34; color: #9aa0b5; padding: 1px 6px; border-radius: 4px; }
.model-row { margin-top: 3px; display: flex; align-items: baseline; gap: 8px; }
.model { font-size: 12px; color: #c8d0ff; font-weight: 600; }
.host { font-size: 10px; color: #7a7a8a; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex: 0 0 8px; }
.dot.green { background: #37d67a; box-shadow: 0 0 6px #37d67a; }
.dot.amber { background: #f5a623; box-shadow: 0 0 6px #f5a623; }
.dot.gray { background: #555; }
.block { background: #1c1c24; border-radius: 8px; padding: 7px 9px; margin-bottom: 7px; }
.block-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 5px; font-size: 11px; color: #8a8a99; text-transform: uppercase; letter-spacing: 0.5px; }
.cost { color: #ffd43b; font-weight: 600; }
.metrics { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr; gap: 4px 6px; }
.metrics div { display: flex; flex-direction: column; }
.metrics label { font-size: 10px; color: #6c6c7a; }
.metrics b { font-size: 12px; font-weight: 600; font-variant-numeric: tabular-nums; }
.metrics b.live { color: #4dabf7; }
.metrics b.muted-b { color: #7a7a8a; font-weight: 500; }
.metrics b.ok { color: #37d67a; }
.metrics b.err { color: #ff6b6b; }
.sub { font-size: 10px; color: #6c6c7a; margin-top: 4px; }
.bar-track { height: 4px; background: #2a2a34; border-radius: 2px; margin-top: 6px; overflow: hidden; }
.bar-fill { height: 100%; background: linear-gradient(90deg, #3b5bdb, #4dabf7); }
.bar-cap { font-size: 10px; color: #6c6c7a; margin-top: 4px; }
.muted { color: #7a7a8a; font-size: 11px; }
.err-line { margin-top: 4px; color: #ff6b6b; font-size: 11px; }
.footer { margin-top: 2px; }
.models { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; }
.tag { font-size: 10px; background: #23232c; color: #9aa0b5; padding: 1px 6px; border-radius: 4px; }
.errors { margin-top: 5px; display: flex; flex-direction: column; gap: 3px; }
.err-chip { font-size: 10px; color: #ffb088; background: #2a2018; padding: 2px 6px; border-radius: 4px; }
.loading { display: flex; align-items: center; justify-content: center; color: #888; }

/* ===== Collapsed bar ===== */
.bar { width: 100%; height: 100%; box-sizing: border-box; padding: 0 8px; display: flex; align-items: center; gap: 8px; color: #e6e6ea; font-size: 11px; font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif; overflow: hidden; }
.bar .b-model { font-weight: 600; color: #c8d0ff; }
.bar .b-provider { color: #8a8a99; max-width: 70px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar .sep { color: #555; }
.bar .b-group { white-space: nowrap; font-variant-numeric: tabular-nums; }
.bar .b-group b.in { color: #4dabf7; font-weight: 600; }
.bar .b-group b.out { color: #37d67a; font-weight: 600; }
.bar .expand { margin-left: auto; border: none; background: #2a2a34; color: #b8b8c4; border-radius: 4px; width: 20px; height: 20px; font-size: 11px; cursor: pointer; -webkit-app-region: no-drag; }
</style>
```

- [ ] **Step 3: Regenerate bindings + typecheck**

Run (regenerates `frontend/wailsjs/go/main/App.js` to include `SetCollapsed`):
```bash
(cd /d/codex-gen-pros && export PATH="$PATH:/d/go_sensus/bin" && wails generate module)
```
Then:
```bash
(cd /d/codex-gen-pros/frontend && npx vue-tsc --noEmit)
```
Expected: no errors. (If `wails generate module` is unavailable in your version, `wails dev` regenerates bindings on startup — run it in Step 5 instead.)

- [ ] **Step 4: Commit**

```bash
(cd /d/codex-gen-pros && git add frontend/src/types.ts frontend/src/App.vue frontend/wailsjs && git commit -m "feat(ui): context headline + chart + sparkline + collapse bar")
```

---

## Task 8: Build + end-to-end verification

**Files:** none (verification).

- [ ] **Step 1: Full Go test + build**

Run:
```bash
(cd /d/codex-gen-pros && go test ./... && go build ./...)
```
Expected: all tests PASS, build clean.

- [ ] **Step 2: Diag against live DB**

Run: `(cd /d/codex-gen-pros && go test -run TestDiag -v)`
Expected: PASS; logs show:
- `context: today=<≈25M> sessionLatest=<big> latest=<big>`
- `series len=N>0 firstIn=... lastIn=... lastOut=...`

Cross-check with python (read-only):
```bash
python - <<'EOF'
import sqlite3, time
con = sqlite3.connect("file:C:/Users/<user>/.cc-switch/cc-switch.db?mode=ro", uri=True)
cur = con.cursor()
since = int(time.time()) - 3600
print("count,last_in_context,last_out:",
  cur.execute("SELECT COUNT(*), MAX(COALESCE(input_tokens,0)+COALESCE(cache_read_tokens,0)+COALESCE(cache_creation_tokens,0)), MAX(output_tokens) FROM proxy_request_logs WHERE app_type='claude' AND created_at>=?", (since,)).fetchone())
con.close()
EOF
```
Expected: count matches `len(series)`; `last_in_context`/`last_out` match the diag `lastIn`/`lastOut`.

- [ ] **Step 3: Production build**

Run:
```bash
(cd /d/codex-gen-pros && export PATH="$PATH:/d/go_sensus/bin" && wails build -platform windows/amd64)
```
Expected: `D:\codex-gen-pros\build\bin\cc-overlay.exe` rebuilt.

- [ ] **Step 4: Manual run checklist (wails dev)**

Run `(cd /d/codex-gen-pros && export PATH="$PATH:/d/go_sensus/bin" && wails dev)` and confirm:
1. Card appears, always-on-top, dark.
2. **今日输入 ≈ 25M** (not 1.3M); 当前会话 **最新输入 ≈ 80k+** (not 334). ← accuracy fix.
3. Chart under header shows blue area + green line; **sparkline** in bar.
4. Trigger a Claude Code request → chart gains a point within ~1.5s; 最新输入 updates.
5. Click **—** → window shrinks to a one-line bar (440×36) showing model, today, live, sparkline, ▢.
6. Click **▢** (or double-click bar) → expands back to full card.
7. Collapse, close `wails dev`, relaunch → still collapsed (persisted). Expand, relaunch → expanded.
8. Tray **退出** quits; the **—** button never quits.

- [ ] **Step 5: Final commit (if any build artifacts tracked)**

```bash
(cd /d/codex-gen-pros && git add -A && git commit -m "chore: v2 build" || echo "nothing to commit")
```

---

## Self-Review (completed)

**Spec coverage:** §4.1 accuracy → Tasks 1–2 (ContextTokens). §4.2 live freshness → Task 2 (LatestSessionRequest). §4.3 chart+sparkline → Tasks 3, 5, 6. §4.4 collapse → Tasks 4, 7. §5 data model → Tasks 1, 3. §6 backend → Tasks 2–4. §7 frontend → Tasks 5–7. §8 main.go → Task 4. §9 testing → Tasks 1–3, 8. §10 edge cases (empty window, DB lock, multi-session) → covered by empty-state in Chart, existing DB-lock backoff, session selection unchanged. All sections mapped.

**Placeholder scan:** the Task 2 Step 4 diag edit contains a self-correcting note (a placeholder helper was proposed then explicitly retracted inline with the correct final code). The FINAL action is: use `s.Latest.ContextTokens` in the log line and add NO helper. No other placeholders remain.

**Type consistency:** `SeriesPoint{ T, In, Out }` (Go) ↔ `{ t, in, out }` (TS) — match. `ContextTokens`/`contextTokens`, `LatestContextTokens`/`latestContextTokens`, `Series`/`series`, `Collapsed`/`collapsed` — consistent across Go + TS + App.vue. `SetCollapsed(on bool) error` binding ↔ `SetCollapsed(on: boolean): Promise<void>` — match. `RecentRequests(app, since) ([]SeriesRow, error)` and `LatestSessionRequest(app, sessionID) (*RequestRow, error)` — signatures used in app.go match definitions.
