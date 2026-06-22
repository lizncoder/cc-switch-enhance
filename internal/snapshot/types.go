package snapshot

// Snapshot is the single aggregated payload pushed to the frontend via
// runtime.EventsEmit("snapshot", s). All token counts are real values; cost
// is split into "real" (from cc switch) and "estimated" (from the overlay's
// pricing override).
type Snapshot struct {
	AppType       string           `json:"appType"`
	AvailableApps []string         `json:"availableApps"`
	GeneratedAt   string           `json:"generatedAt"`
	Provider      ProviderInfo     `json:"provider"`
	Model         ModelInfo        `json:"model"`
	Today         UsageTotals      `json:"today"`
	Month         UsageTotals      `json:"month"`
	Session       SessionInfo      `json:"session"`
	Latest        *RequestInfo     `json:"latest"`
	PerModelToday []ModelBreakdown `json:"perModelToday"`
	Errors        []string         `json:"errors"`
	Series        []SeriesPoint    `json:"series"`
	Collapsed     bool             `json:"collapsed"`
	TodayAllAppsTokens int64       `json:"todayAllAppsTokens"`
	Tokens5h        int64          `json:"tokens5h"`
	Tokens7d        int64          `json:"tokens7d"`
	PlanLimits      []PlanLimit     `json:"planLimits"`
	Balance         *BalanceInfo    `json:"balance,omitempty"`
	Warn            bool            `json:"warn"`
	WarnReason      string          `json:"warnReason"`
	WeeklyUsage     []DayUsage      `json:"weeklyUsage"`
}

// DayUsage is one day's total token consumption for the weekly bar chart.
type DayUsage struct {
	Date    string `json:"date"`    // "06-13" short format
	Tokens  int64  `json:"tokens"`  // context + output + cache for the day
	IsToday bool   `json:"isToday"` // marks the dynamic today bar
}

type ProviderInfo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Category        string  `json:"category"`
	WebsiteURL      string  `json:"websiteUrl"`
	CostMultiplier  float64 `json:"costMultiplier"`
	LimitDailyUSD   *float64 `json:"limitDailyUsd"`
	LimitMonthlyUSD *float64 `json:"limitMonthlyUsd"`
}

type ModelInfo struct {
	Display  string `json:"display"`   // resolved current model
	BaseURL  string `json:"baseUrl"`
	BaseHost string `json:"baseHost"`
	LiveModel string `json:"liveModel"` // model actually seen in the latest log/transcript
	Match    bool   `json:"match"`      // live == display
}

type UsageTotals struct {
	Requests     int64   `json:"requests"`
	Successes    int64   `json:"successes"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CacheRead    int64   `json:"cacheRead"`
	CacheCreate  int64   `json:"cacheCreate"`
	ContextTokens int64  `json:"contextTokens"` // input + cacheRead + cacheCreate
	RealCostUSD  float64 `json:"realCostUsd"`
	EstCostUSD   float64 `json:"estCostUsd"`
	ShowEstCost  bool    `json:"showEstCost"`
}

type SessionInfo struct {
	Live         bool    `json:"live"`
	PID          int     `json:"pid"`
	Status       string  `json:"status"` // "busy" | "idle"
	SessionID    string  `json:"sessionId"`
	Cwd          string  `json:"cwd"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CacheRead    int64   `json:"cacheRead"`
	CacheCreate  int64   `json:"cacheCreate"`
	ContextTokens int64  `json:"contextTokens"` // session total context
	RealCostUSD  float64 `json:"realCostUsd"`
	EstCostUSD   float64 `json:"estCostUsd"`
	LatestInput  int64   `json:"latestInput"`  // instant from transcript tail
	LatestOutput int64   `json:"latestOutput"`
	LatestCacheRead int64 `json:"latestCacheRead"`
	LatestCacheCreate int64 `json:"latestCacheCreate"`
	LatestContextTokens int64 `json:"latestContextTokens"` // latest turn context
	LatestModel  string  `json:"latestModel"`
	AgeSec       int64   `json:"ageSec"`
}

type RequestInfo struct {
	Model        string  `json:"model"`
	RequestModel string  `json:"requestModel"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CacheRead    int64   `json:"cacheRead"`
	CacheCreate  int64   `json:"cacheCreate"`
	ContextTokens int64  `json:"contextTokens"`
	TotalCostUSD float64 `json:"totalCostUsd"`
	EstCostUSD   float64 `json:"estCostUsd"`
	LatencyMS    int64   `json:"latencyMs"`
	StatusCode   int     `json:"statusCode"`
	Error        string  `json:"error"`
	AgeSec       int64   `json:"ageSec"`
}

type ModelBreakdown struct {
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	TotalCostUSD float64 `json:"totalCostUsd"`
}

// SeriesPoint is one per-request sample for the real-time chart / sparkline.
// In is the total context (input + cacheRead + cacheCreate), precomputed server-side.
type SeriesPoint struct {
	T   int64 `json:"t"`   // created_at, unix seconds
	In  int64 `json:"in"`  // total context tokens
	Out int64 `json:"out"` // output tokens
}

// PlanLimit is one GLM/Z.ai Coding Plan usage window with its percentage and
// next-reset time (sourced from the plan quota API, not cc switch's DB).
type PlanLimit struct {
	Window      string `json:"window"`
	Kind        string `json:"kind"`
	Percent     int    `json:"percent"`
	NextResetMS int64  `json:"nextResetMs"`
}

// BalanceInfo is an account balance snapshot from non-GLM providers (e.g.
// DeepSeek) that don't expose time-window quota percentages. The frontend
// shows this as a compact balance row when PlanLimits are unavailable.
type BalanceInfo struct {
	IsAvailable     bool   `json:"isAvailable"`
	TotalBalance    string `json:"totalBalance"`
	GrantedBalance  string `json:"grantedBalance"`
	ToppedUpBalance string `json:"toppedUpBalance"`
	Currency        string `json:"currency"`
}
