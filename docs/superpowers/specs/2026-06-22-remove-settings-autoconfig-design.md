# Remove Settings Panel + Auto Config Resolution — Design

**Date:** 2026-06-22
**Branch:** `cc-overlay-v2`
**Status:** Approved (brainstormed)

## Goal

Remove the overlay's self-managed provider/Base-URL/Token settings layer entirely. The overlay should resolve the API endpoint + token (and model name) **automatically**: from cc-switch if present, otherwise from the selected tool's own native config files (Claude / Codex / OpenCode). No settings UI remains.

## Background

Today the overlay has a settings panel (gear button → "设置 · GLM 额度") that bundles two concerns:

1. **Manual provider/Base URL/Token override** — `OverlayConfig.ManualBaseURL` / `ManualToken`, surfaced via `GetManualConfig` / `SetManualConfig` / `ListProviders` / `ApplyProviderCreds`. Consumed by `activeCreds()` as a fallback for the quota/balance fetch.
2. **Quota-alert thresholds** — `WarnPercent` / `WarnBalance` / `WarnHysteresis`, edited via `SetWarnThresholds`, consumed at `app.go:519` by `snapshot.EvaluateAlert`.

`activeCreds()` already reads cc-switch's active provider from the DB first (the common case); the manual layer is a redundant override. The user wants it gone: auto-read cc-switch, and when cc-switch is absent, read the tool's own config.

## Decisions (from brainstorming)

- **Scope:** Remove the entire settings panel + gear icon. Hardcode alert thresholds to `85 / 10 / 10` (current defaults); remove the configurability.
- **Fallback depth (no cc-switch):** creds-only — read the tool's native API baseURL+token so 额度/余额 still resolves. Usage stats (today/month/session) stay empty because they depend on cc-switch's `proxy_request_logs` table.
- **Tools to support in the fallback:** Claude, Codex, OpenCode.
- **Empty usage (no cc-switch):** render silently as empty/zero — no error chip, no hint text.
- **Model name (no cc-switch):** seed `s.Model.Display` from the tool-native reader so the model name still shows.

## Architecture

New package `internal/toolconf` holds one reader per tool. `app.go`'s `activeCreds()` calls it as the second-priority source (after cc-switch). `buildSnapshot()` seeds model display from it when cc-switch provides no provider.

### Config sources (verified)

| Tool | Path | Format | Token |
|------|------|--------|-------|
| Claude | `~/.claude/settings.json` (already resolved as `paths.ClaudeSettings`) | JSON: `env.ANTHROPIC_BASE_URL`, `env.ANTHROPIC_AUTH_TOKEN`, top-level `model` | direct |
| Codex | `$CODEX_HOME/config.toml` (default `~/.codex`) — `CODEX_HOME` env override | TOML: top-level `model_provider` + `model`; `[model_providers.<p>]` → `base_url`, `env_key` | `os.Getenv(env_key)` |
| OpenCode | `~/.config/opencode/opencode.json` (Win: `%USERPROFILE%\.config\opencode\opencode.json`) | JSON/JSONC: `"model": "<provider>/<model>"`, `"provider": { "<name>": { "options": { "baseURL", "apiKey" } } }`; `{env:VAR}` refs | resolve `{env:VAR}`; else `auth.json` token |

**Caveat (non-blocking):** Codex and OpenCode frequently store the token in an **env var** (`env_key` / `{env:VAR}`). If the overlay process did not inherit that env, the reader returns baseURL-only → no quota fetch. This is acceptable degradation, not a regression (the manual fallback it replaces is also being removed). OAuth-based logins (Codex ChatGPT login, some OpenCode auths) produce tokens that cannot query GLM/DeepSeek — host mismatch leaves quota empty.

## Components

### 1. Removal surface

**`app.go`**
- Delete methods: `GetManualConfig`, `SetManualConfig`, `ListProviders`, `ApplyProviderCreds`, `SetWarnThresholds`.
- Delete types: `ManualConfig`, `ProviderCred`.
- `fetchLimits()`: remove the "unknown provider → fall back to manual config" branch (the `if mt := a.cfg.ManualToken...` block, ~lines 906–915). Unknown hosts now simply leave quota empty.
- `activeCreds()`: remove the manual-token branch (~lines 947–949); replace with toolconf fallback (see §3).
- `buildSnapshot()` alert call (~line 519): pass constants `85, 10, 10` instead of `a.cfg.WarnPercent/WarnBalance/WarnHysteresis`.
- `buildSnapshot()` `db==nil` branch: stop appending "无法打开 cc switch 数据库（请确认 cc switch 已安装）". cc-switch-absent is now a supported mode. (`SchemaError` chip stays — that's a real incompatibility.)

**`internal/config/paths.go`**
- Remove from `OverlayConfig`: `ManualBaseURL`, `ManualToken`, `WarnPercent`, `WarnBalance`, `WarnHysteresis`.
- Remove the corresponding defaults from `DefaultOverlayConfig`.
- Old `config.json` files carrying these keys are safely ignored (unknown JSON fields are skipped by `json.Unmarshal`).

**`internal/ccswitchdb/queries.go`**
- Remove `ProviderCreds` (query) and the ccswitchdb `ProviderCred` type — their only callers (`ListProviders` / `ApplyProviderCreds`) are deleted.

**Frontend (`frontend/src/App.vue` + bindings)**
- Remove the gear button (`.gear-btn`) and the settings card template (`v-else-if="snap && settingsOpen"`).
- Remove refs/functions: `settingsOpen`, `cfgBase`, `cfgToken`, `cfgHasToken`, `cfgWarnPct`, `cfgWarnBal`, `cfgProviderId`, `providers`, `openSettings`, `onProviderChange`, `saveCfg`.
- Remove imports: `GetManualConfig`, `SetManualConfig`, `ListProviders`, `SetWarnThresholds`, `ApplyProviderCreds`.
- Remove `.settings` / `.gear-btn` CSS.
- Regenerate `frontend/wailsjs/go/main/App.{d.ts,js}` and `models.ts` (wails generates these from Go; a `wails dev`/`wails build` regenerates — remove stale `ManualConfig`/`ProviderCred` entries).

### 2. New `internal/toolconf` package

```
package toolconf

type Creds struct {
    BaseURL  string
    Token    string
    Model    string
    Provider string // human label, e.g. provider name or app title
}

// Active resolves the active provider's creds for an appType from the tool's own
// config files. Used only when cc-switch is absent/unreadable. ok=false when
// nothing usable is found. Reads default paths (CODEX_HOME / standard homes).
func Active(appType string) (Creds, bool)
```

Each reader takes **explicit paths** (testable with `t.TempDir()` fixtures):

- **`claude.go`** — `claude(settingsPath string) (Creds, bool)`: unmarshal JSON, read `env.ANTHROPIC_BASE_URL`, `env.ANTHROPIC_AUTH_TOKEN`, `model`.
- **`codex.go`** — `codex(configTomlPath string) (Creds, bool)`: parse TOML (BurntSushi/toml), read top-level `model_provider` + `model`, resolve `[model_providers.<p>]` → `base_url` + `env_key`, `token = os.Getenv(env_key)`.
- **`opencode.go`** — `opencode(configPath, authPath string) (Creds, bool)`: parse JSON (strip JSONC comments), read `"model"` → split `<provider>/<model>`, resolve `provider.<name>.options.baseURL` + `apiKey`; resolve `{env:VAR}` via `resolveEnv`; if `apiKey` empty/`{env:}` unresolved, read token from `auth.json`.
- **`env.go`** — `resolveEnv(s string) string`: if `s` matches `{env:VAR}` return `os.Getenv(VAR)`, else return `s`.

`Active(appType)` dispatches: `"claude"`→claude, `"codex"`→codex, `"opencode"`→opencode; unknown → `(Creds{}, false)`.

`Active(appType)` resolves all default paths **internally** (via `os.UserHomeDir()` + env), keeping the package dependency-free and the `Active(appType)` signature clean. The per-tool readers (which take explicit paths) are the testable cores; `Active` just wires the defaults:

- Claude: `~/.claude/settings.json`.
- Codex: `$CODEX_HOME/config.toml` or `~/.codex/config.toml`.
- OpenCode: `~/.config/opencode/opencode.json` (Win: `%USERPROFILE%\.config\opencode\opencode.json`); auth `~/.local/share/opencode/auth.json` (Linux). **Windows auth path to be confirmed during build** (likely `%LOCALAPPDATA%\opencode\auth.json`).

### 3. Credentials + model resolution flow (`app.go`)

`activeCreds()` new priority:
1. cc-switch DB active provider `settings_config.env` (unchanged) → return on hit.
2. `toolconf.Active(app)` → return on hit.
3. `("", "", false)`.

`fetchLimits()`: unchanged structure; just gets its creds from the new `activeCreds()`. No manual fallback branch.

`buildSnapshot()`, when `provider == nil` (no cc-switch provider):
- Call `tc, ok := toolconf.Active(app)`.
- If `ok`: seed `s.Model.Display = tc.Model` (when display would otherwise be empty); set a minimal `s.Provider` (name from `tc.Provider` or the app title); do **not** append the "未找到 provider" chip.
- Usage sections naturally render empty (no data rows) — no chip, no hint.

### 4. New dependency

`github.com/BurntSushi/toml` — added to `go.mod` for Codex's `config.toml`. (Confirmed no TOML lib exists today.)

## Error handling / degradation

- No new error chips. cc-switch-absent is silent; provider/model/quota come from toolconf when available.
- `SchemaError` (cc-switch present, incompatible schema) still surfaces its chip.
- Tool readers are best-effort: missing/malformed files → `(Creds{}, false)`, no panic, no log spam.
- Token-not-in-env (Codex/OpenCode) → baseURL-only creds → quota fetch skipped for that host. Acceptable.

## Testing

- **`internal/toolconf`**: unit tests with fixture files (`testdata/claude.json`, `codex.toml`, `opencode.json`) asserting `(baseURL, token, model, provider)` extraction + `{env:}` resolution (set env via `t.Setenv`). Readers take explicit paths → `t.TempDir()`.
- **Regression**: existing `ccswitchdb` tests unaffected; `go vet ./...`; `go build ./...` green.
- **Manual**: temporarily rename cc-switch DB → confirm provider/model/quota still resolve from tool config; restore → confirm common case unchanged. Also confirm the gear button + settings panel are gone from the UI.

## Out of scope

- Parsing tool-native usage logs/sessions for stats without cc-switch (deferred — usage stays empty).
- Any new UI for the removed thresholds (they are hardcoded constants now).
- Migrating old `ManualBaseURL`/`ManualToken` values to anything (they are simply dropped).
