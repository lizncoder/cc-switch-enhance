//go:build windows

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows/registry"

	"cc-enhance/internal/ccswitchdb"
	"cc-enhance/internal/claudecode"
	"cc-enhance/internal/config"
	"cc-enhance/internal/deepseekusage"
	"cc-enhance/internal/glmusage"
	"cc-enhance/internal/modelresolve"
	"cc-enhance/internal/pricing"
	"cc-enhance/internal/snapshot"
	"cc-enhance/internal/toolconf"
	"cc-enhance/internal/tray"
)

// App holds all runtime state and is bound to the frontend.
type App struct {
	ctx   context.Context
	paths *config.Paths
	cfg   *config.OverlayConfig

	db *ccswitchdb.DB
	cc *claudecode.Watcher

	availableApps []string
	visible       bool

	cachedSettings *config.CCSwitchSettings

	mu           sync.Mutex
	emitMu       sync.Mutex // serializes buildSnapshot/emit so the 1s watcher and 1.5s loop can't double-fire
	lastSnapshot *snapshot.Snapshot
	lastLimits   []snapshot.PlanLimit
	lastBalance  *snapshot.BalanceInfo
	warned       bool // true while a warning toast has fired and not yet cleared
	barWidth     int // last measured collapsed-bar width, to size the window directly on collapse (no flicker)
	winW, winH   int // current window size (for animation start point)
	animGen      int // bumped to supersede an in-flight resize animation
	animating    bool // true while a resize animation is running
}

// NewApp resolves paths + loads the overlay config.
func NewApp() *App {
	paths := config.Resolve()
	cfg := config.LoadOverlayConfig(paths.OverlayConfig, paths.OverlayDir)
	return &App{
		paths:         paths,
		cfg:           cfg,
		availableApps: []string{"claude"},
		visible:       true,
		winW:          300,
		winH:          360,
	}
}

// startup is called by Wails at launch.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Register the app with Windows so toast notifications actually display.
	// go-toast needs an AppID + GUID in the registry; without this Push is silent.
	if exe, err := os.Executable(); err == nil {
		saErr := toast.SetAppData(toast.AppData{
			AppID:         "cc-enhance",
			GUID:          "{8a2f4c71-3d6e-4b9a-8c1f-2e5d7a3b9f04}",
			ActivationExe: exe,
		})
		_ = saErr // best-effort registration
	}
	if db, err := ccswitchdb.Open(a.paths.CCSwitchDB); err != nil {
		log.Printf("ccswitchdb open failed: %v", err)
	} else {
		a.db = db
	}
	a.refreshSettings()
	a.refreshAvailableApps()
	a.cc = claudecode.NewWatcher(a.paths.ClaudeSessions, a.paths.ClaudeProjects)
	a.cc.Start()
	go tray.Run(a, trayIconBytes)
	go a.loop()
	go a.limitsLoop()
	a.restorePosition()
	a.restoreCollapsed()
}

// domReady fires when the frontend is mounted; push an immediate snapshot.
func (a *App) domReady(ctx context.Context) {
	// The overlay is tray-only — never occupy a taskbar slot. Clicking a
	// taskbar button minimizes a frameless always-on-top window, and there's no
	// clean restore short of the tray menu. Set TOOLWINDOW as soon as the
	// window exists. (applyCollapsed re-asserts this on every toggle.)
	setTaskbarVisible(false)
	a.emit()
}

// shutdown cleans up.
func (a *App) shutdown(ctx context.Context) {
	if a.cc != nil {
		a.cc.Stop()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
	tray.Quit()
}

// beforeClose persists the window position and hides instead of quitting.
func (a *App) beforeClose(ctx context.Context) bool {
	x, y := runtime.WindowGetPosition(ctx)
	_ = config.SaveWindowPos(a.paths.WindowPosition, a.paths.OverlayDir, config.WindowPos{X: x, Y: y})
	runtime.WindowHide(ctx)
	return false
}

func (a *App) loop() {
	ms := a.cfg.PollIntervalMs
	if ms <= 0 {
		ms = 1500
	}
	ticker := time.NewTicker(time.Duration(ms) * time.Millisecond)
	defer ticker.Stop()
	var lastProviderID string
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.refreshSettings()
			a.refreshAvailableApps()
			a.emit()
			// Follow provider switches: when the cc-switch selection changes
			// (DeepSeek ↔ GLM), refresh the quota source immediately so the UI
			// shows balance vs percentages without waiting for the 60s tick.
			if pid := a.currentProviderID(); pid != lastProviderID {
				lastProviderID = pid
				go a.fetchLimits()
			}
		case <-a.cc.Updated():
			a.emit()
		}
	}
}

func (a *App) emit() {
	a.emitMu.Lock()
	defer a.emitMu.Unlock()
	s := a.buildSnapshot()
	a.mu.Lock()
	a.lastSnapshot = s
	a.mu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "snapshot", s)
	}
	a.detectUserResize()
}

// detectUserResize persists manual window-size edits by the user. While
// expanded it tracks height; while collapsed it tracks bar width — so the
// user can drag the bar wider and have it stick.
func (a *App) detectUserResize() {
	if a.ctx == nil {
		return
	}
	w, h := runtime.WindowGetSize(a.ctx)
	if w == 0 || h == 0 {
		return
	}
	if a.cfg.Collapsed {
		// Collapsed width is fixed at 360; don't track or override it.
		return
	}

	// Expanded: track height, auto-collapse on extreme shrink.
	// Skip while an animation is in flight (winH is mid-change) to avoid
	// persisting transient heights or false auto-collapses.
	if a.animating {
		return
	}
	if h < 180 && a.winH >= 200 && h != a.winH {
		a.cfg.UserCardHeight = 0
		_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
		a.winH = h // prevent re-trigger before collapse applies
		go func() {
			_ = a.SetCollapsed(true)
			a.emit()
		}()
		return
	}
	// Persist a manually-chosen expanded height.
	if h != a.winH && h >= 200 && h <= 640 {
		a.cfg.UserCardHeight = h
		a.winH = h
		_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
	}
}

// currentProviderID returns the cc-switch selected claude provider id, or "".
func (a *App) currentProviderID() string {
	app := a.cfg.SelectedApp
	if app == "" {
		app = "claude"
	}
	settings := a.cachedSettings
	if settings == nil {
		settings = config.ReadCCSwitchSettings(a.paths.CCSwitchSettings)
	}
	if settings == nil {
		return ""
	}
	return settings.CurrentProviderID(app)
}

// ---- snapshot assembly ----

func (a *App) buildSnapshot() *snapshot.Snapshot {
	now := time.Now()
	app := a.cfg.SelectedApp
	if app == "" {
		app = "claude"
	}
	s := &snapshot.Snapshot{
		AppType:       app,
		GeneratedAt:   now.Format(time.RFC3339),
		AvailableApps: a.availableApps,
	}
	dbReady := true
	if a.db == nil {
		dbReady = false // silent: toolconf will provide provider/model/creds
	} else if se := a.db.SchemaError(); se != nil {
		s.Errors = append(s.Errors, se.Error()) // one chip instead of N per-query failures
		dbReady = false
	}

	// Active provider.
	settings := a.cachedSettings
	currentID := ""
	if settings != nil {
		currentID = settings.CurrentProviderID(app)
	}
	var provider *ccswitchdb.Provider
	if dbReady {
		p, err := a.db.ActiveProvider(app, currentID)
		if err == nil {
			provider = p
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取 %s provider 失败: %v", app, err))
		}
	}

	settingsConfigJSON := ""
	var fallbackModel, fallbackBaseURL string
	if provider != nil {
		settingsConfigJSON = provider.SettingsConfig
		s.Provider = snapshot.ProviderInfo{
			ID:              provider.ID,
			Name:            provider.Name,
			Category:        provider.Category,
			WebsiteURL:      provider.WebsiteURL,
			CostMultiplier:  provider.CostMultiplier,
			LimitDailyUSD:   nullFloat(provider.LimitDailyUSD),
			LimitMonthlyUSD: nullFloat(provider.LimitMonthlyUSD),
		}
	} else if tc, ok := toolconf.Active(app); ok {
		// No cc-switch provider: seed from the tool's own config so the overlay
		// still shows model/provider/baseURL. (No error chip — silent mode.)
		fallbackModel = tc.Model
		fallbackBaseURL = tc.BaseURL
		s.Provider = snapshot.ProviderInfo{Name: tc.Provider}
	}

	// Claude Code model tier (settings.json `model`).
	claudeTier := ""
	if app == "claude" {
		claudeTier = a.readClaudeModelTier()
	}

	// Latest request row (live model + non-claude latest in/out).
	var latestReq *ccswitchdb.RequestRow
	if dbReady {
		r, err := a.db.LatestRequest(app)
		if err == nil {
			latestReq = r
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取最近请求失败: %v", err))
		}
	}
	liveModel := ""
	if latestReq != nil {
		liveModel = latestReq.Model
		if liveModel == "" {
			liveModel = latestReq.RequestModel
		}
	}

	// Session (claude: live registry + transcript; others: latest log session).
	si := snapshot.SessionInfo{}
	var sessionID string
	if app == "claude" {
		if cur := a.cc.Current(); cur != nil {
			si.Live = true
			si.PID = cur.PID
			si.Status = cur.Status
			si.SessionID = cur.SessionID
			si.Cwd = cur.Cwd
			if cur.StartedAt > 0 {
				startedSec := cur.StartedAt / 1000
				if startedSec > 0 {
					si.AgeSec = now.Unix() - startedSec
				}
			}
			sessionID = cur.SessionID
			u := a.cc.LatestUsage()
			if u.HasUsage {
				si.LatestInput = u.InputTokens
				si.LatestOutput = u.OutputTokens
				si.LatestCacheRead = u.CacheRead
				si.LatestCacheCreate = u.CacheCreate
				si.LatestModel = u.Model
				if u.Model != "" {
					liveModel = u.Model
				}
			}
		}
	}
	if sessionID == "" && dbReady {
		if sid, err := a.db.LatestSessionID(app); err == nil && sid != "" {
			sessionID = sid
			si.SessionID = sid
		}
	}
	// Live in/out: prefer the latest DB row for the CURRENT session (unified
	// source, fresh within the poll). Non-claude apps have no session registry,
	// so fall back to the global latest request row.
	var liveRow *ccswitchdb.RequestRow
	if app == "claude" && sessionID != "" && dbReady {
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

	// Model resolution.
	cfgDisplay, baseURL := modelresolve.Resolve(app, settingsConfigJSON, claudeTier)
	display := cfgDisplay
	if display == "" {
		display = liveModel
	}
	if display == "" {
		display = fallbackModel
	}
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	// When the provider is an aggregated relay (OpenCode Go), show the
	// provider/brand name instead of a specific model name.
	if strings.Contains(strings.ToLower(baseURL), "opencode.ai") && provider != nil {
		display = provider.Name
	}
	s.Model = snapshot.ModelInfo{
		Display:   display,
		BaseURL:   baseURL,
		BaseHost:  modelresolve.HostOf(baseURL),
		LiveModel: liveModel,
		Match:     display != "" && liveModel != "" && modelresolve.Normalize(display) == modelresolve.Normalize(liveModel),
	}

	// Today / month / session aggregates.
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if dbReady {
		// Today / month / 5h / 7d in a single conditional-sum scan (was 4
		// separate queries). Month is read from the logs directly — cc switch's
		// rollups lag and can omit the current month; logs retain ~30 days.
		firstMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		h5Start := now.Add(-5 * time.Hour).Unix()
		d7Start := now.Add(-7 * 24 * time.Hour).Unix()
		since := firstMonth.Unix()
		if d7Start < since {
			since = d7Start
		}
		monthT, todayT, t5h, t7d, err := a.db.MultiWindowTotals(app, since, firstMonth.Unix(), startToday.Unix(), h5Start, d7Start)
		if err == nil {
			s.Today = a.toUsage(todayT, display)
			s.Month = a.toUsage(monthT, display)
			s.Tokens5h = t5h
			s.Tokens7d = t7d
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取用量统计失败: %v", err))
		}
		// All-apps today total (matches cc switch's dashboard: sums every app_type).
		if allT, err := a.db.TodayTotalsAll(startToday.Unix(), now.Unix()); err == nil {
			s.TodayAllAppsTokens = allT.InputTokens + allT.OutputTokens + allT.CacheRead + allT.CacheCreate
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取全部 app 今日用量失败: %v", err))
		}
		if sessionID != "" {
			if t, err := a.db.SessionTotals(app, sessionID); err == nil {
				si.Requests = t.Requests
				si.InputTokens = t.InputTokens
				si.OutputTokens = t.OutputTokens
				si.CacheRead = t.CacheRead
				si.CacheCreate = t.CacheCreate
				si.ContextTokens = snapshot.SumContext(t.InputTokens, t.CacheRead, t.CacheCreate)
				si.RealCostUSD = t.CostUSD
				si.EstCostUSD = pricing.Estimate(a.cfg.Pricing, display,
					t.InputTokens, t.OutputTokens, t.CacheRead, t.CacheCreate)
			} else {
				s.Errors = append(s.Errors, fmt.Sprintf("读取会话用量失败: %v", err))
			}
		}
		if rows, err := a.db.PerModelToday(app, startToday.Unix()); err == nil {
			for _, m := range rows {
				s.PerModelToday = append(s.PerModelToday, snapshot.ModelBreakdown{
					Model:        m.Model,
					Requests:     m.Requests,
					InputTokens:  m.InputTokens,
					OutputTokens: m.OutputTokens,
					TotalCostUSD: m.TotalCostUSD,
				})
			}
		}
	}
	// For aggregated relays like OpenCode Go, only show models that are
	// explicitly configured in the provider's settings (not every model
	// that happened to appear in the request log).
	if strings.Contains(strings.ToLower(baseURL), "opencode.ai") && provider != nil {
		keep := collectProviderModels(settingsConfigJSON)
		if len(keep) > 0 {
			filtered := s.PerModelToday[:0]
			for _, m := range s.PerModelToday {
				if keep[m.Model] {
					filtered = append(filtered, m)
				}
			}
			s.PerModelToday = filtered
		}
	}
	// Real-time series for the chart/sparkline (last ChartWindowMin minutes).
	min := a.cfg.ChartWindowMin
	if min <= 0 {
		min = 60
	}
	if dbReady {
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
			// Cap the series so the IPC payload and SVG render stay bounded
			// regardless of request volume (the chart only renders ~120 pts).
			s.Series = snapshot.Downsample(pts, 120)
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取时序失败: %v", err))
		}
	}
	s.Session = si

	// Weekly usage: per-day token totals for the last 7 days (today dynamic).
	// Reuses TodayTotals over each day's range — cc-switch's logs retain ~30d.
	if dbReady {
		// 7-day per-day totals in one GROUP BY scan (was 7 queries).
		wm, werr := a.db.WeeklyTotals(app, startToday.AddDate(0, 0, -6).Unix())
		if werr == nil {
			weekly := make([]snapshot.DayUsage, 0, 7)
			for i := 6; i >= 0; i-- {
				dayStart := startToday.AddDate(0, 0, -i)
				weekly = append(weekly, snapshot.DayUsage{
					Date:    dayStart.Format("01-02"),
					Tokens:  wm[dayStart.Format("2006-01-02")],
					IsToday: i == 0,
				})
			}
			s.WeeklyUsage = weekly
		} else {
			s.Errors = append(s.Errors, fmt.Sprintf("读取周用量失败: %v", werr))
		}
	}

	// Latest request card.
	s.Latest = a.toRequest(latestReq, now)

	a.mu.Lock()
	s.PlanLimits = a.lastLimits
	s.Balance = a.lastBalance
	a.mu.Unlock()
	s.Collapsed = a.cfg.Collapsed

	// Quota warning: evaluate current state, drive the warned state machine.
	// warned fires one toast on entering "dangerous" and re-arms only after
	// the metrics drop to "safe" (below the hysteresis band) — no flap.
	danger, safe, reason := snapshot.EvaluateAlert(s.PlanLimits, s.Balance,
		85, 10, 10)
	// Only flash/alert while the model is actively busy — if it's idle or
	// no session, a low-balance/over-quota state shouldn't keep pulsing.
	if !s.Session.Live || s.Session.Status != "busy" {
		danger = false
		safe = true // re-arm so the next busy period can warn fresh
	}
	s.Warn = danger
	s.WarnReason = reason
	if danger && !a.warned {
		a.warned = true
		body := "cc-enhance：" + reason
		if reason == "" {
			body = "cc-enhance：额度接近上限"
		}
		go a.sendToast("额度预警", body)
	} else if safe && a.warned {
		a.warned = false
	}
	return s
}

// sendToast fires a Windows toast via PowerShell + the Windows Runtime, using a
// system-registered AUMID ("Microsoft.Windows.Explorer") so it displays without a
// custom Start-menu shortcut. Best-effort: errors are logged.
func (a *App) sendToast(title, body string) {
	script := "$ErrorActionPreference='Stop';" +
		"[void][Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime];" +
		"[void][Windows.UI.Notifications.ToastNotification,Windows.UI.Notifications,ContentType=WindowsRuntime];" +
		"[void][Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom,ContentType=WindowsRuntime];" +
		"$x=[Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent(1);" +
		"$t=$x.GetElementsByTagName('text');" +
		fmt.Sprintf("$t.Item(0).AppendChild($x.CreateTextNode('%s'))|Out-Null;", psSingle(title)) +
		fmt.Sprintf("$t.Item(1).AppendChild($x.CreateTextNode('%s'))|Out-Null;", psSingle(body)) +
		"$n=New-Object Windows.UI.Notifications.ToastNotification $x;" +
		"[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('Microsoft.Windows.Explorer').Show($n)"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-EncodedCommand", psEncode(script))
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("toast failed: %v %s", err, out)
	}
}

// psEncode base64-encodes a string as UTF-16LE for PowerShell -EncodedCommand
// (avoids command-line encoding issues with CJK text).
func psEncode(s string) string {
	u16 := utf16.Encode([]rune(s))
	b := make([]byte, len(u16)*2)
	for i, v := range u16 {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// psSingle escapes single quotes for a PowerShell single-quoted string.
func psSingle(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (a *App) toUsage(t ccswitchdb.Totals, model string) snapshot.UsageTotals {
	u := snapshot.UsageTotals{
		Requests:     t.Requests,
		Successes:    t.Successes,
		InputTokens:  t.InputTokens,
		OutputTokens: t.OutputTokens,
		CacheRead:     t.CacheRead,
		CacheCreate:   t.CacheCreate,
		ContextTokens: snapshot.SumContext(t.InputTokens, t.CacheRead, t.CacheCreate),
		RealCostUSD:   t.CostUSD,
		ShowEstCost:  a.cfg.ShowEstimatedCost,
	}
	u.EstCostUSD = pricing.Estimate(a.cfg.Pricing, model, t.InputTokens, t.OutputTokens, t.CacheRead, t.CacheCreate)
	return u
}

func (a *App) toRequest(r *ccswitchdb.RequestRow, now time.Time) *snapshot.RequestInfo {
	if r == nil {
		return nil
	}
	m := r.Model
	if m == "" {
		m = r.RequestModel
	}
	ri := &snapshot.RequestInfo{
		Model:        r.Model,
		RequestModel: r.RequestModel,
		InputTokens:  r.InputTokens,
		OutputTokens: r.OutputTokens,
		CacheRead:     r.CacheRead,
		CacheCreate:   r.CacheCreate,
		ContextTokens: snapshot.SumContext(r.InputTokens, r.CacheRead, r.CacheCreate),
		TotalCostUSD:  r.TotalCostUSD,
		EstCostUSD:   pricing.Estimate(a.cfg.Pricing, m, r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheCreate),
		LatencyMS:    r.LatencyMS,
		StatusCode:   int(r.StatusCode),
		Error:        r.ErrorMessage,
	}
	if r.CreatedAt > 0 {
		ri.AgeSec = now.Unix() - r.CreatedAt
	}
	return ri
}

// ---- helpers ----

func (a *App) readClaudeModelTier() string {
	data, err := os.ReadFile(a.paths.ClaudeSettings)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	if v, ok := m["model"].(string); ok {
		return v
	}
	return ""
}

func (a *App) refreshSettings() {
	a.cachedSettings = config.ReadCCSwitchSettings(a.paths.CCSwitchSettings)
}

func (a *App) refreshAvailableApps() {
	if a.db == nil {
		return
	}
	apps, err := a.db.ListApps()
	if err != nil || len(apps) == 0 {
		return
	}
	// Only keep apps that actually exist in the DB. A stale SelectedApp
	// (e.g. "opencode", which has no providers) used to be force-added and
	// produced an empty/broken view.
	dbApps := make(map[string]bool, len(apps))
	for _, ap := range apps {
		dbApps[ap] = true
	}
	settings := a.cachedSettings
	if settings == nil {
		settings = config.ReadCCSwitchSettings(a.paths.CCSwitchSettings)
		a.cachedSettings = settings
	}
	var filtered []string
	for _, ap := range apps {
		if dbApps[ap] && settings.AppVisible(ap) {
			filtered = append(filtered, ap)
		}
	}
	// If the persisted SelectedApp is no longer a real app, fall back to claude
	// instead of keeping a dead selection that renders nothing.
	if sel := a.cfg.SelectedApp; sel != "" && !dbApps[sel] {
		a.cfg.SelectedApp = "claude"
		_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
	}
	sort.Strings(filtered)
	if len(filtered) > 0 {
		a.availableApps = filtered
	}
}

func (a *App) restorePosition() {
	p, ok := config.LoadWindowPos(a.paths.WindowPosition)
	if !ok || (p.X <= 0 && p.Y <= 0) {
		return
	}
	// Ignore off-screen positions — e.g. a saved coord from an external
	// monitor that's since been disconnected. Restoring it would put the
	// window somewhere the user can't see; fall back to the default position.
	if !pointOnScreen(p.X, p.Y) {
		return
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		runtime.WindowSetPosition(a.ctx, p.X, p.Y)
	}()
}

// ---- frontend bindings ----

// GetSnapshot returns the latest snapshot (building one on demand if needed).
// NOTE: must NOT hold a.mu across buildSnapshot() — buildSnapshot locks a.mu
// internally (for PlanLimits), and Go's mutex is not reentrant (that deadlock
// froze the UI on "加载中").
func (a *App) GetSnapshot() *snapshot.Snapshot {
	a.mu.Lock()
	cached := a.lastSnapshot
	a.mu.Unlock()
	if cached != nil {
		return cached
	}
	s := a.buildSnapshot()
	a.mu.Lock()
	a.lastSnapshot = s
	a.mu.Unlock()
	return s
}

// ListApps returns the selectable app_types.
func (a *App) ListApps() []string { return a.availableApps }

// SetApp switches the monitored app and persists the choice.
func (a *App) SetApp(app string) error {
	if app == "" {
		return nil
	}
	a.cfg.SelectedApp = app
	_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
	a.emit()
	return nil
}

// Quit terminates the app.
func (a *App) Quit() { runtime.Quit(a.ctx) }

// SetCollapsed toggles the one-line bar mode: shrinks the window to a slim bar
// (or restores the full card) and persists the choice. Never quits.
func (a *App) SetCollapsed(on bool) error {
	a.cfg.Collapsed = on
	_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
	a.applyCollapsed()
	a.emit()
	return nil
}

// SetBarWidth caches the measured collapsed-bar content width. If we're
// currently collapsed and the measured width differs (e.g. the first collapse
// used an unmeasured placeholder), snap the window to the real width right
// away — no need to wait for the next collapse.
func (a *App) SetBarWidth(w int) {
	// Collapsed width is now fixed at 360px (set in applyCollapsed). Ignore
	// frontend measurements — they were unreliable under flex layout and kept
	// shrinking the bar.
	_ = w
}

// SetCardHeight auto-sizes the expanded window to fit content. Called by the
// frontend after each snapshot render; clamped to [200, 640] for sanity.
// Skipped once the user has set a manual height (UserCardHeight != 0) —
// their choice wins over auto-fit.
func (a *App) SetCardHeight(h int) {
	if a.ctx == nil || a.cfg.Collapsed || a.cfg.UserCardHeight != 0 {
		return
	}
	if h < 200 {
		h = 200
	} else if h > 640 {
		h = 640
	}
	a.winH = h
	runtime.WindowSetSize(a.ctx, overlayWidth, h)
}

// SetUserHeight persists a manually-chosen card height and applies it. Once
// set, auto-fit (SetCardHeight) is disabled — the user's value wins. Pass 0
// to clear and re-enable auto-fit.
func (a *App) SetUserHeight(h int) error {
	if h != 0 && (h < 200 || h > 640) {
		return nil // ignore out-of-range
	}
	a.cfg.UserCardHeight = h
	_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
	if h != 0 && a.ctx != nil && !a.cfg.Collapsed {
		a.winH = h
		runtime.WindowSetSize(a.ctx, overlayWidth, h)
	}
	return nil
}

// overlayWidth is the single window width shared by the collapsed bar and the
// expanded card, so the overlay never changes width when toggling between them.
const overlayWidth = 320

// applyCollapsed resizes the window to the bar or card dimensions. The overlay
// is always tray-only (no taskbar slot) — see setTaskbarVisible in domReady.
func (a *App) applyCollapsed() {
	if a.ctx == nil {
		return
	}
	if a.cfg.Collapsed {
		// Collapsed bar: fixed width matching the expanded card (overlayWidth),
		// so the overlay keeps the same width in both states.
		runtime.WindowSetSize(a.ctx, overlayWidth, 36)
		a.winW = overlayWidth
	} else {
		// Expand: set width first, then a default height. The frontend's fitCard
		// will measure the real content height and call SetCardHeight to adjust.
		// No animation here — animateTo fought with fitCard and caused flicker.
		h := 360
		if a.cfg.UserCardHeight >= 200 && a.cfg.UserCardHeight <= 640 {
			h = a.cfg.UserCardHeight
		}
		runtime.WindowSetSize(a.ctx, overlayWidth, h)
		a.winW, a.winH = overlayWidth, h
	}
	// Overlay is tray-only regardless of state (see domReady).
	setTaskbarVisible(false)
}

// animateTo smoothly resizes the window from its current size to (targetW,
// targetH) over ~180ms with ease-out. A bumped animGen supersedes any
// in-flight animation so rapid collapse/expand doesn't fight itself.
func (a *App) animateTo(targetW, targetH int) {
	a.animateToEase(targetW, targetH, false)
}

// animateCollapse uses easeOutBack (spring overshoot) for the snappy collapse.
// animateTo (expand) uses plain easeOut — no overshoot, which caused a visible
// flicker past the target size before settling.
func (a *App) animateToEase(targetW, targetH int, overshoot bool) {
	a.mu.Lock()
	a.animGen++
	a.animating = true
	gen := a.animGen
	startW, startH := a.winW, a.winH
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.animating = false; a.mu.Unlock() }()
	if startW == targetW && startH == targetH {
		return
	}
	const steps = 24
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		var e float64
		if overshoot {
			c1 := 1.70158
			c3 := c1 + 1
			t1 := t - 1
			e = 1 + c3*t1*t1*t1 + c1*t1*t1 // easeOutBack: spring
		} else {
			t1 := t - 1
			e = 1 - t1*t1*t1 // easeOutCubic: smooth, no overshoot
		}
		w := startW + int(e*float64(targetW-startW)+0.5)
		h := startH + int(e*float64(targetH-startH)+0.5)
		runtime.WindowSetSize(a.ctx, w, h)
		a.mu.Lock()
		if a.animGen != gen {
			a.mu.Unlock()
			return // a newer animation started; abort
		}
		a.winW, a.winH = w, h
		a.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
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

// fetchLimits refreshes plan quota (GLM percentages) or account balance
// (DeepSeek). The billing model follows the CURRENTLY ACTIVE provider — the
// cc-switch selection — so switching providers in cc-switch (DeepSeek ↔ GLM)
// immediately switches what the UI shows. When cc-switch is absent, the
// selected tool's own native config (toolconf) is a fallback for creds only.
// Safe to call periodically; failures are silent (the UI keeps the last good
// values). The auth token is used only for this request and is never logged.
func (a *App) fetchLimits() {
	// Clear stale plan data so switching apps doesn't show the old app's
	// quota (e.g. GLM percentages when viewing opencode).
	a.mu.Lock()
	a.lastLimits = nil
	a.lastBalance = nil
	a.mu.Unlock()

	base, token, ok := a.activeCreds()
	if !ok {
		return
	}
	host := strings.ToLower(base)
	switch {
	case strings.Contains(host, "deepseek"):
		a.applyDeepSeekBalance(token)
	case strings.Contains(host, "bigmodel.cn"), strings.Contains(host, "z.ai"):
		// GLM/Z.ai Coding Plan exposes time-window quota percentages.
		a.applyLimits(glmusage.Fetch(base, token))
	default:
		// Unknown provider kind (e.g. OpenCode's opencode.ai relay). No
		// balance/quota API to call — leave plan data empty so the UI hides
		// those sections rather than erroring.
	}
}

// activeCreds returns the base URL + token of the currently active provider:
// follows the selected app (cc-switch) so the quota source matches the app
// the user is viewing, then falls back to the tool's own native config (toolconf).
func (a *App) activeCreds() (base, token string, ok bool) {
	app := a.cfg.SelectedApp
	if app == "" {
		app = "claude"
	}
	if a.db != nil {
		settings := a.cachedSettings
		if settings == nil {
			settings = config.ReadCCSwitchSettings(a.paths.CCSwitchSettings)
		}
		currentID := ""
		if settings != nil {
			currentID = settings.CurrentProviderID(app)
		}
		if p, err := a.db.ActiveProvider(app, currentID); err == nil && p != nil {
			var cfg struct {
				Env map[string]string `json:"env"`
			}
			if json.Unmarshal([]byte(p.SettingsConfig), &cfg) == nil {
				if t := cfg.Env["ANTHROPIC_AUTH_TOKEN"]; t != "" {
					return cfg.Env["ANTHROPIC_BASE_URL"], t, true
				}
			}
		}
	}
	// Fallback: the selected tool's own native config (when cc-switch is absent
	// or has no provider with a token).
	if c, ok := toolconf.Active(app); ok && c.Token != "" {
		return c.BaseURL, c.Token, true
	}
	return "", "", false
}

// applyDeepSeekBalance fetches DeepSeek account balance and stores it.
func (a *App) applyDeepSeekBalance(token string) bool {
	info, err := deepseekusage.Fetch(token)
	if err != nil || info == nil {
		return false
	}
	b := &snapshot.BalanceInfo{
		IsAvailable:     info.IsAvailable,
		Currency:        info.Currency,
		TotalBalance:    info.TotalBalance,
		GrantedBalance:  info.GrantedBalance,
		ToppedUpBalance: info.ToppedUpBalance,
	}
	a.mu.Lock()
	a.lastBalance = b
	a.lastLimits = nil // GLM percentages and DeepSeek balance are mutually exclusive per provider
	a.mu.Unlock()
	return true
}

// applyLimits stores fetched plan limits; returns true on success.
func (a *App) applyLimits(limits []glmusage.Limit, err error) bool {
	if err != nil || limits == nil {
		return false
	}
	pl := make([]snapshot.PlanLimit, len(limits))
	for i, l := range limits {
		pl[i] = snapshot.PlanLimit{Window: l.Window, Kind: l.Kind, Percent: l.Percent, NextResetMS: l.NextResetMS}
	}
	a.mu.Lock()
	a.lastLimits = pl
	a.lastBalance = nil // GLM percentages and DeepSeek balance are mutually exclusive per provider
	a.mu.Unlock()
	return true
}

// limitsLoop polls the plan quota at a slow cadence (every 60s) so we don't
// hammer the GLM API on every 1.5s UI tick.
func (a *App) limitsLoop() {
	a.fetchLimits()
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
			a.fetchLimits()
			a.emit()
		}
	}
}

// ---- tray.Controller ----

func (a *App) ToggleShow() {
	a.mu.Lock()
	wasVisible := a.visible
	a.visible = !wasVisible
	a.mu.Unlock()
	if wasVisible {
		runtime.WindowHide(a.ctx)
	} else {
		runtime.WindowShow(a.ctx)
	}
}

func (a *App) SetAlwaysOnTop(on bool) { runtime.WindowSetAlwaysOnTop(a.ctx, on) }

// SetAutoStart implements tray.Controller and is also bound to the frontend.
// It enables or disables login-time launch via Windows registry.
func (a *App) SetAutoStart(on bool) {
	if err := a.setAutoStartRegistry(on); err != nil {
		log.Printf("set auto-start: %v", err)
	}
	// Update tray menu state after the change.
	tray.SetAutoStartState(on)
}

// OpenCCSwitch launches (or focuses, via Tauri single-instance) the cc-switch
// app so the user can switch providers without leaving the overlay workflow.
// Best-effort: logs a notice if the executable isn't found.
func (a *App) OpenCCSwitch() {
	for _, p := range ccSwitchExeCandidates() {
		if _, err := os.Stat(p); err == nil {
			if err := exec.Command(p).Start(); err != nil {
				log.Printf("open cc-switch: start %s: %v", p, err)
			}
			return
		}
	}
	log.Printf("open cc-switch: executable not found in any candidate path")
}

// CCSwitchInstalled reports whether a cc-switch executable is reachable at any
// candidate install path (common dirs + Start Menu shortcut targets). The
// frontend uses it to show the launcher only when cc-switch is actually present.
func (a *App) CCSwitchInstalled() bool {
	for _, p := range ccSwitchExeCandidates() {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// GetAutoStart reports whether cc-enhance is registered to launch at user login
// (HKCU\Software\Microsoft\Windows\CurrentVersion\Run).
func (a *App) GetAutoStart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.READ)
	if err != nil {
		return false
	}
	defer k.Close()
	val, _, err := k.GetStringValue("cc-enhance")
	if err != nil || val == "" {
		return false
	}
	// Also verify the value points to our own executable (not a stale entry from
	// a different install path).
	myExe, err := os.Executable()
	if err != nil {
		return true // registry key present, assume true on error
	}
	return strings.EqualFold(val, myExe)
}

// setAutoStartRegistry enables or disables login-time launch via Windows registry.
func (a *App) setAutoStartRegistry(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("open registry Run key: %w", err)
	}
	defer k.Close()
	if on {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
		if err := k.SetStringValue("cc-enhance", exe); err != nil {
			return fmt.Errorf("set registry value: %w", err)
		}
	} else {
		if err := k.DeleteValue("cc-enhance"); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("delete registry value: %w", err)
		}
	}
	return nil
}

// ccSwitchExeCandidates returns likely cc-switch.exe install paths to probe, in
// priority order: a handful of common install dirs, then Start Menu shortcut
// targets (resolved via the WScript.Shell COM object — these follow wherever
// the user actually installed cc-switch, e.g. D:\APP\ccSwitch\, which no fixed
// path can predict).
func ccSwitchExeCandidates() []string {
	var out []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		out = append(out, la+`\cc-switch\cc-switch.exe`)
	}
	out = append(out,
		`C:\Apps\ccswitch\cc-switch.exe`,
		`C:\Program Files\cc-switch\cc-switch.exe`,
		`C:\Program Files (x86)\cc-switch\cc-switch.exe`,
	)
	for _, lnk := range findCCSwitchShortcuts() {
		t := resolveShortcut(lnk)
		if t == "" {
			continue
		}
		// Guard against a coincidentally-named shortcut pointing elsewhere:
		// only accept targets whose own file name looks like cc-switch.
		base := strings.ToLower(filepath.Base(t))
		if strings.Contains(base, "cc-switch") || strings.Contains(base, "ccswitch") {
			out = append(out, t)
		}
	}
	return out
}

// findCCSwitchShortcuts scans the per-user and all-users Start Menu Programs
// trees for .lnk files whose name looks like cc-switch (case-insensitive).
func findCCSwitchShortcuts() []string {
	dirs := []string{
		os.Getenv("APPDATA") + `\Microsoft\Windows\Start Menu\Programs`,
		os.Getenv("ProgramData") + `\Microsoft\Windows\Start Menu\Programs`,
	}
	var out []string
	for _, d := range dirs {
		if d == "" || d == `\Microsoft\Windows\Start Menu\Programs` {
			continue
		}
		_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
			if err != nil || e == nil || e.IsDir() {
				return nil
			}
			name := strings.ToLower(e.Name())
			if strings.HasSuffix(name, ".lnk") && strings.Contains(name, "cc") && strings.Contains(name, "switch") {
				out = append(out, p)
			}
			return nil
		})
	}
	return out
}

// resolveShortcut reads a .lnk's target path via the WScript.Shell COM object
// (a PowerShell one-liner). Returns "" on any failure.
func resolveShortcut(lnk string) string {
	esc := strings.ReplaceAll(lnk, "'", "''")
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command",
		"(New-Object -ComObject WScript.Shell).CreateShortcut('"+esc+"').TargetPath").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---- utils ----

// collectProviderModels extracts all model name values from a provider's
// settings_config.env (keys matching *_MODEL or *_MODEL_NAME) into a set for
// filtering per-model breakdown views.
func collectProviderModels(settingsConfigJSON string) map[string]bool {
	var sc struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal([]byte(settingsConfigJSON), &sc) != nil || len(sc.Env) == 0 {
		return nil
	}
	keep := make(map[string]bool)
	for k, v := range sc.Env {
		if len(k) > 6 && (k[len(k)-6:] == "_MODEL" || k[len(k)-10:] == "_MODEL_NAME") {
			if v != "" {
				keep[v] = true
				// Also keep the model without [1M]-style suffix (requests may
				// not carry the suffix).
				if i := strings.Index(v, "["); i >= 0 {
					keep[v[:i]] = true
				}
			}
		}
	}
	return keep
}

func nullFloat(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
