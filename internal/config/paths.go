package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Pricing is the per-million-USD price of a model, used to estimate cost
// from real token counts. Loaded from the overlay config file; never touches
// cc switch's model_pricing table.
type Pricing struct {
	In         float64 `json:"in"`
	Out        float64 `json:"out"`
	CacheRead  float64 `json:"cacheRead"`
	CacheCreate float64 `json:"cacheCreate"`
}

// WindowPos is the persisted card position.
type WindowPos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// OverlayConfig is the overlay's own config (cc-enhance side), NOT cc switch.
type OverlayConfig struct {
	Window            WindowPos         `json:"window"`
	SelectedApp       string            `json:"selectedApp"`
	PollIntervalMs    int               `json:"pollIntervalMs"`
	ChartWindowMin    int               `json:"chartWindowMin"`
	ShowEstimatedCost bool              `json:"showEstimatedCost"`
	Collapsed         bool              `json:"collapsed"`
	Pricing           map[string]Pricing `json:"pricing"`
	UserCardHeight    int               `json:"userCardHeight"` // user's manual height (0 = auto)
}

// Paths resolves every data source location, with env overrides.
type Paths struct {
	CCSwitchDB       string
	CCSwitchSettings string
	ClaudeHome       string
	ClaudeSettings   string
	ClaudeProjects   string
	ClaudeSessions   string
	OverlayDir       string
	OverlayConfig    string
	WindowPosition   string
}

func Resolve() *Paths {
	home := userHome()
	db := envOr("CCSWITCH_DB", filepath.Join(home, ".cc-switch", "cc-switch.db"))
	settings := envOr("CCSWITCH_SETTINGS", filepath.Join(home, ".cc-switch", "settings.json"))
	claudeHome := envOr("CLAUDE_HOME", filepath.Join(home, ".claude"))
	overlayDir := envOr("CCENHANCE_DIR", filepath.Join(home, ".cc-enhance"))
	// One-time migration from the pre-rename ~/.cc-overlay dir so existing users
	// keep their config. No-op once ~/.cc-enhance exists.
	migrateLegacyDir(filepath.Join(home, ".cc-overlay"), overlayDir)
	return &Paths{
		CCSwitchDB:       db,
		CCSwitchSettings: settings,
		ClaudeHome:       claudeHome,
		ClaudeSettings:   filepath.Join(claudeHome, "settings.json"),
		ClaudeProjects:   filepath.Join(claudeHome, "projects"),
		ClaudeSessions:   filepath.Join(claudeHome, "sessions"),
		OverlayDir:       overlayDir,
		OverlayConfig:    filepath.Join(overlayDir, "config.json"),
		WindowPosition:   filepath.Join(overlayDir, "window-position.json"),
	}
}

// migrateLegacyDir moves the old config dir to its new home once (both live in
// the user's home, so os.Rename is an atomic same-volume move). No-op if the new
// dir already exists or the old one doesn't.
func migrateLegacyDir(oldDir, newDir string) {
	if newDir == "" || oldDir == "" || oldDir == newDir {
		return
	}
	if _, err := os.Stat(newDir); err == nil {
		return // new already present
	}
	if _, err := os.Stat(oldDir); err != nil {
		return // nothing to migrate
	}
	_ = os.Rename(oldDir, newDir)
}

func userHome() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	return h
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// DefaultOverlayConfig returns sensible defaults (claude app, 1.5s poll,
// estimated cost on, an empty/default pricing map).
func DefaultOverlayConfig() *OverlayConfig {
	return &OverlayConfig{
		Window:            WindowPos{X: -1, Y: -1},
		SelectedApp:       "claude",
		PollIntervalMs:    1500,
		ChartWindowMin:    60,
		ShowEstimatedCost: true,
		Pricing: map[string]Pricing{
			"default": {In: 3, Out: 15, CacheRead: 0.3, CacheCreate: 3.75},
		},
	}
}

// LoadOverlayConfig reads the overlay config, creating it with defaults on first run.
func LoadOverlayConfig(cfgPath, dir string) *OverlayConfig {
	cfg := DefaultOverlayConfig()
	data, err := os.ReadFile(cfgPath)
	if err == nil {
		// Merge into defaults so new fields always have values.
		_ = json.Unmarshal(data, cfg)
	} else {
		// First run: persist defaults.
		_ = os.MkdirAll(dir, 0o755)
		if out, err := json.MarshalIndent(cfg, "", "  "); err == nil {
			_ = os.WriteFile(cfgPath, out, 0o600)
		}
	}
	if cfg.SelectedApp == "" {
		cfg.SelectedApp = "claude"
	}
	if cfg.PollIntervalMs <= 0 {
		cfg.PollIntervalMs = 1500
	}
	if cfg.ChartWindowMin <= 0 {
		cfg.ChartWindowMin = 60
	}
	if cfg.Pricing == nil {
		cfg.Pricing = map[string]Pricing{"default": {In: 3, Out: 15, CacheRead: 0.3, CacheCreate: 3.75}}
	}
	return cfg
}

// Save writes the overlay config back to disk.
func (c *OverlayConfig) Save(cfgPath, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// 0600: config.json may hold the manual API token — keep it owner-only.
	return os.WriteFile(cfgPath, out, 0o600)
}

// SaveWindowPos persists the card position.
func SaveWindowPos(posPath, dir string, p WindowPos) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(posPath, out, 0o644)
}

// LoadWindowPos returns the last persisted position (ok=false if absent).
func LoadWindowPos(posPath string) (WindowPos, bool) {
	var p WindowPos
	data, err := os.ReadFile(posPath)
	if err != nil {
		return p, false
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, false
	}
	return p, true
}

// ---- cc switch settings.json reader ----

// CCSwitchSettings captures only the fields we need from cc switch's settings.json.
// Provider pointers are read from the raw map (keys like currentProviderClaude)
// because not every app_type has an explicit pointer key.
type CCSwitchSettings struct {
	CurrentProvider map[string]string // app -> provider UUID
	VisibleApps     map[string]bool
}

// ReadCCSwitchSettings reads cc switch settings.json defensively.
func ReadCCSwitchSettings(path string) *CCSwitchSettings {
	s := &CCSwitchSettings{CurrentProvider: map[string]string{}, VisibleApps: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return s
	}
	for k, v := range raw {
		if strings.HasPrefix(k, "currentProvider") {
			app := strings.ToLower(strings.TrimPrefix(k, "currentProvider"))
			if str, ok := v.(string); ok && str != "" {
				s.CurrentProvider[app] = str
			}
		}
	}
	if va, ok := raw["visibleApps"].(map[string]any); ok {
		for k, v := range va {
			if b, ok := v.(bool); ok {
				s.VisibleApps[k] = b
			}
		}
	}
	return s
}

// CurrentProviderID returns the configured active provider UUID for an app, "" if unset.
func (s *CCSwitchSettings) CurrentProviderID(app string) string {
	return s.CurrentProvider[strings.ToLower(app)]
}

// AppVisible reports whether cc switch shows this app.
func (s *CCSwitchSettings) AppVisible(app string) bool {
	v, ok := s.VisibleApps[app]
	if !ok {
		return true // unknown -> treat as visible
	}
	return v
}
