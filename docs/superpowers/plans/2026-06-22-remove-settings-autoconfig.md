# Remove Settings Panel + Auto Config Resolution — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the overlay's manual provider/Base-URL/Token settings panel and auto-resolve API creds + model from cc-switch, falling back to each tool's native config (Claude/Codex/OpenCode) when cc-switch is absent.

**Architecture:** A new dependency-free `internal/toolconf` package holds one reader per tool; `app.go`'s `activeCreds()` calls it after the cc-switch DB lookup, and `buildSnapshot()` seeds model/provider display from it when cc-switch has no provider. The manual-config layer, warn-threshold configurability, settings UI, and their backing types/queries are removed.

**Tech Stack:** Go 1.22+ (Wails v2 backend), Vue 3 + Vite frontend, SQLite (cc-switch DB), new dep `github.com/BurntSushi/toml` for Codex's `config.toml`.

**Spec:** `docs/superpowers/specs/2026-06-22-remove-settings-autoconfig-design.md`

## Global Constraints

- Hardcode alert thresholds: `85` (GLM percent), `10` (DeepSeek balance CNY), `10` (hysteresis) — passed as literals at the single `snapshot.EvaluateAlert` call site (`app.go:519`). No configurability.
- `internal/toolconf` must be dependency-free (no import of `internal/config`); it resolves all paths internally via `os.UserHomeDir()` + env.
- cc-switch-absent is a **silent, supported mode**: no error chip for a missing DB, no hint text. (`SchemaError` chip stays.)
- Model name + provider label + base URL are seeded from `toolconf` when cc-switch yields no provider.
- Token resolution caveat: Codex/OpenCode tokens often live in env vars (`env_key` / `{env:VAR}`); if the overlay didn't inherit that env, creds return baseURL-only → quota silently skipped. Acceptable.
- Every task ends with `go build ./...` passing before commit.

## File Structure

**Create:**
- `internal/toolconf/env.go` — `resolveEnv(s)` ({env:VAR} expansion).
- `internal/toolconf/claude.go` — `claude(settingsPath)` reader.
- `internal/toolconf/codex.go` — `codex(configTomlPath)` reader.
- `internal/toolconf/opencode.go` — `opencode(configPath, authPath)` reader + `stripJSONC`.
- `internal/toolconf/toolconf.go` — `Creds` type, `Active(appType)` dispatcher, default-path helpers.
- `internal/toolconf/*_test.go` + `internal/toolconf/testdata/*` fixtures.

**Modify:**
- `app.go` — rewire `activeCreds()` + `buildSnapshot()`; hardcode thresholds; delete 5 methods + 2 types.
- `internal/config/paths.go` — drop 5 `OverlayConfig` fields + defaults.
- `internal/ccswitchdb/queries.go` — drop `ProviderCreds` + `ProviderCred`.
- `frontend/src/App.vue` — remove gear button, settings card, refs/functions, imports, CSS.
- `frontend/wailsjs/go/main/App.{d.ts,js}` + `models.ts` — regenerate (remove stale bindings).
- `go.mod` / `go.sum` — add `github.com/BurntSushi/toml`.

**Dependency-safe order:** build `toolconf` (Tasks 1–5) → rewire `app.go` to use it (6–7) → delete dead methods (8) → remove config fields + DB query now that nothing references them (9) → frontend (10) → verify (11).

---

### Task 1: toolconf — env resolver

**Files:**
- Create: `internal/toolconf/env.go`
- Test: `internal/toolconf/env_test.go`

**Interfaces:**
- Produces: `func resolveEnv(s string) string` — expands `{env:VAR}` → `os.Getenv(VAR)`; returns `s` unchanged (including when `VAR` is unset).

- [ ] **Step 1: Write the failing test**

```go
// internal/toolconf/env_test.go
package toolconf

import "testing"

func TestResolveEnv(t *testing.T) {
	t.Setenv("MY_KEY", "secret-value")

	cases := []struct{ in, want string }{
		{"{env:MY_KEY}", "secret-value"},
		{"{env:UNSET_VAR}", ""},        // unset → empty
		{"plain-string", "plain-string"}, // not an env ref → unchanged
		{"https://x.io/api", "https://x.io/api"}, // literal preserved
		{"", ""},
	}
	for _, c := range cases {
		if got := resolveEnv(c.in); got != c.want {
			t.Errorf("resolveEnv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolconf/ -run TestResolveEnv -v`
Expected: FAIL / build error (`resolveEnv undefined`).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/toolconf/env.go
package toolconf

import (
	"os"
	"strings"
)

// resolveEnv expands OpenCode's {env:VAR} references to the value of the named
// environment variable. Anything that isn't a {env:...} literal is returned
// unchanged (including when VAR is unset, which yields "").
func resolveEnv(s string) string {
	const prefix = "{env:"
	const suffix = "}"
	if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix) {
		name := s[len(prefix) : len(s)-len(suffix)]
		if name != "" {
			return os.Getenv(name)
		}
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/toolconf/ -run TestResolveEnv -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/toolconf/env.go internal/toolconf/env_test.go
git commit -m "feat(toolconf): {env:VAR} resolver"
```

---

### Task 2: toolconf — Claude reader

**Files:**
- Create: `internal/toolconf/claude.go`
- Test: `internal/toolconf/claude_test.go`
- Fixture: `internal/toolconf/testdata/claude.json`

**Interfaces:**
- Produces: `func claude(settingsPath string) (Creds, bool)` — reads `~/.claude/settings.json` JSON: `env.ANTHROPIC_BASE_URL`, `env.ANTHROPIC_AUTH_TOKEN`, top-level `model`. `Creds{Provider:"Claude"}`. Returns `false` when file missing/malformed or all three fields blank.

> **Note:** `Creds` is defined in Task 5 (`toolconf.go`). For this task to compile in isolation, define a temporary `Creds` struct in `claude.go`; Task 5 moves it to `toolconf.go` and removes the duplicate. **Simpler alternative:** do Task 5 first. To keep TDD bite-sized, this task defines `Creds` in `toolconf.go` minimally now (see Step 3).

- [ ] **Step 1: Write the failing test**

```go
// internal/toolconf/claude_test.go
package toolconf

import "testing"

func TestClaude(t *testing.T) {
	c, ok := claude("testdata/claude.json")
	if !ok {
		t.Fatalf("claude returned ok=false")
	}
	if c.BaseURL != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.Token != "sk-claude-token" {
		t.Errorf("Token = %q", c.Token)
	}
	if c.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q", c.Model)
	}
	if c.Provider != "Claude" {
		t.Errorf("Provider = %q", c.Provider)
	}
}

func TestClaudeMissingFile(t *testing.T) {
	if _, ok := claude("testdata/does-not-exist.json"); ok {
		t.Errorf("want ok=false for missing file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolconf/ -run TestClaude -v`
Expected: FAIL / build error.

- [ ] **Step 3: Write minimal implementation**

Create fixture:
```json
// internal/toolconf/testdata/claude.json
{
  "model": "claude-sonnet-4-6",
  "env": {
    "ANTHROPIC_BASE_URL": "https://open.bigmodel.cn/api/anthropic",
    "ANTHROPIC_AUTH_TOKEN": "sk-claude-token"
  }
}
```

Create `internal/toolconf/toolconf.go` (the `Creds` type lives here permanently):
```go
// internal/toolconf/toolconf.go
package toolconf

// Creds is the active provider's API endpoint + auth, read from a tool's own
// config files when cc-switch is absent. The zero value with ok=false means
// nothing usable was found.
type Creds struct {
	BaseURL  string
	Token    string
	Model    string
	Provider string
}
```

Create the reader:
```go
// internal/toolconf/claude.go
package toolconf

import (
	"encoding/json"
	"os"
)

// claude reads Claude Code's ~/.claude/settings.json: env.ANTHROPIC_BASE_URL,
// env.ANTHROPIC_AUTH_TOKEN, and the top-level model. ok=false if the file is
// missing/malformed or every field is blank.
func claude(settingsPath string) (Creds, bool) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return Creds{}, false
	}
	var raw struct {
		Model string            `json:"model"`
		Env   map[string]string `json:"env"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return Creds{}, false
	}
	c := Creds{
		BaseURL:  raw.Env["ANTHROPIC_BASE_URL"],
		Token:    raw.Env["ANTHROPIC_AUTH_TOKEN"],
		Model:    raw.Model,
		Provider: "Claude",
	}
	if c.BaseURL == "" && c.Token == "" && c.Model == "" {
		return Creds{}, false
	}
	return c, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/toolconf/ -run TestClaude -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/toolconf/toolconf.go internal/toolconf/claude.go internal/toolconf/claude_test.go internal/toolconf/testdata/claude.json
git commit -m "feat(toolconf): Claude settings.json reader"
```

---

### Task 3: toolconf — Codex reader

**Files:**
- Create: `internal/toolconf/codex.go`
- Test: `internal/toolconf/codex_test.go`
- Fixture: `internal/toolconf/testdata/codex.toml`
- Modify: `go.mod`, `go.sum` (add `github.com/BurntSushi/toml`)

**Interfaces:**
- Produces: `func codex(configTomlPath string) (Creds, bool)` — parses TOML: top-level `model_provider` + `model`; `[model_providers.<p>]` → `base_url`, `env_key`, `name`. `Token = os.Getenv(env_key)`. `Provider` = `name` or `"Codex"`. `ok=false` if missing/malformed, no `model_provider`, provider block absent, or `base_url` blank.

- [ ] **Step 1: Add the TOML dependency**

Run:
```bash
go get github.com/BurntSushi/toml
go mod tidy
```
Expected: `go.mod` gains `github.com/BurntSushi/toml`; `go.sum` updated.

- [ ] **Step 2: Write the failing test**

```go
// internal/toolconf/codex_test.go
package toolconf

import "testing"

func TestCodex(t *testing.T) {
	t.Setenv("CODEX_GLM_KEY", "sk-codex-glm")
	c, ok := codex("testdata/codex.toml")
	if !ok {
		t.Fatalf("codex returned ok=false")
	}
	if c.BaseURL != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.Token != "sk-codex-glm" {
		t.Errorf("Token = %q (want from $CODEX_GLM_KEY)", c.Token)
	}
	if c.Model != "glm-4.6" {
		t.Errorf("Model = %q", c.Model)
	}
	if c.Provider != "GLM" {
		t.Errorf("Provider = %q", c.Provider)
	}
}

func TestCodexMissingFile(t *testing.T) {
	if _, ok := codex("testdata/nope.toml"); ok {
		t.Errorf("want ok=false for missing file")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/toolconf/ -run TestCodex -v`
Expected: FAIL / build error (`codex undefined`).

- [ ] **Step 4: Write minimal implementation**

Fixture:
```toml
# internal/toolconf/testdata/codex.toml
model_provider = "glm"
model = "glm-4.6"

[model_providers.glm]
name = "GLM"
base_url = "https://open.bigmodel.cn/api/anthropic"
env_key = "CODEX_GLM_KEY"
wire_api = "responses"
```

Reader:
```go
// internal/toolconf/codex.go
package toolconf

import (
	"os"

	"github.com/BurntSushi/toml"
)

type codexProvider struct {
	Name    string `toml:"name"`
	BaseURL string `toml:"base_url"`
	EnvKey  string `toml:"env_key"`
}

type codexConfig struct {
	ModelProvider  string                   `toml:"model_provider"`
	Model          string                   `toml:"model"`
	ModelProviders map[string]codexProvider `toml:"model_providers"`
}

// codex reads Codex's $CODEX_HOME/config.toml. The active provider's base_url is
// in [model_providers.<model_provider>]; its API key lives in the env var named
// by env_key (not in the file). ok=false if missing/malformed or base_url blank.
func codex(configTomlPath string) (Creds, bool) {
	data, err := os.ReadFile(configTomlPath)
	if err != nil {
		return Creds{}, false
	}
	var cfg codexConfig
	if toml.Unmarshal(data, &cfg) != nil {
		return Creds{}, false
	}
	if cfg.ModelProvider == "" {
		return Creds{}, false
	}
	p, ok := cfg.ModelProviders[cfg.ModelProvider]
	if !ok {
		return Creds{}, false
	}
	c := Creds{
		BaseURL:  p.BaseURL,
		Token:    os.Getenv(p.EnvKey),
		Model:    cfg.Model,
		Provider: firstNonEmpty(p.Name, "Codex"),
	}
	if c.BaseURL == "" {
		return Creds{}, false
	}
	return c, true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/toolconf/ -run TestCodex -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/toolconf/codex.go internal/toolconf/codex_test.go internal/toolconf/testdata/codex.toml
git commit -m "feat(toolconf): Codex config.toml reader"
```

---

### Task 4: toolconf — OpenCode reader

**Files:**
- Create: `internal/toolconf/opencode.go`
- Test: `internal/toolconf/opencode_test.go`
- Fixtures: `internal/toolconf/testdata/opencode.json`, `internal/toolconf/testdata/opencode-auth.json`

**Interfaces:**
- Produces: `func opencode(configPath, authPath string) (Creds, bool)` — parses JSONC config: `"model"` split on `/` → provider name + model; `provider.<name>.options.baseURL` + `apiKey` (via `resolveEnv`). If `apiKey` empty, reads `auth.json` token for that provider (best-effort `{"<provider>":{"token":"..."}}`). Single-provider fallback when model has no `/`. `ok=false` if missing/malformed or nothing usable.

- [ ] **Step 1: Write the failing test**

```go
// internal/toolconf/opencode_test.go
package toolconf

import "testing"

func TestOpenCode(t *testing.T) {
	t.Setenv("OC_KEY", "sk-opencode")
	c, ok := opencode("testdata/opencode.json", "testdata/opencode-auth.json")
	if !ok {
		t.Fatalf("opencode returned ok=false")
	}
	if c.BaseURL != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.Token != "sk-opencode" {
		t.Errorf("Token = %q (want resolved {env:OC_KEY})", c.Token)
	}
	if c.Model != "glm-4.6" {
		t.Errorf("Model = %q", c.Model)
	}
}

// When the config apiKey is absent, the token is read from auth.json.
func TestOpenCodeAuthFallback(t *testing.T) {
	cfg := "testdata/opencode-auth-only.json" // fixture with no apiKey in options
	// Create this fixture in Step 4 alongside the others.
	c, ok := opencode(cfg, "testdata/opencode-auth.json")
	if !ok {
		t.Fatalf("ok=false")
	}
	if c.Token != "sk-from-auth" {
		t.Errorf("Token = %q (want from auth.json)", c.Token)
	}
}

func TestOpenCodeJSONCComments(t *testing.T) {
	// testdata/opencode.jsonc contains // comments AND a https:// URL; the
	// stripper must keep the URL intact (string-aware) while dropping comments.
	c, ok := opencode("testdata/opencode.jsonc", "")
	if !ok {
		t.Fatalf("ok=false")
	}
	if c.BaseURL != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("BaseURL = %q (URL must survive JSONC strip)", c.BaseURL)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolconf/ -run TestOpenCode -v`
Expected: FAIL / build error (`opencode undefined`).

- [ ] **Step 3: Run test to verify it fails — confirm**

(Already covered by Step 2.)

- [ ] **Step 4: Write minimal implementation**

Fixtures:

`internal/toolconf/testdata/opencode.json`:
```json
{
  "$schema": "https://opencode.ai/config.json",
  "model": "glm/glm-4.6",
  "provider": {
    "glm": {
      "name": "GLM",
      "options": {
        "apiKey": "{env:OC_KEY}",
        "baseURL": "https://open.bigmodel.cn/api/anthropic"
      }
    }
  }
}
```

`internal/toolconf/testdata/opencode-auth-only.json` (no apiKey → must come from auth.json):
```json
{
  "model": "glm/glm-4.6",
  "provider": {
    "glm": { "options": { "baseURL": "https://open.bigmodel.cn/api/anthropic" } }
  }
}
```

`internal/toolconf/testdata/opencode-auth.json`:
```json
{ "glm": { "token": "sk-from-auth" } }
```

`internal/toolconf/testdata/opencode.jsonc` (comment + URL; string-aware strip must keep URL):
```jsonc
{
  // leading comment
  "model": "glm/glm-4.6", // trailing comment
  "provider": {
    "glm": { "options": { "baseURL": "https://open.bigmodel.cn/api/anthropic" } }
  }
}
```

Reader:
```go
// internal/toolconf/opencode.go
package toolconf

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

type opencodeProviderOptions struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseURL"`
}
type opencodeProvider struct {
	Name    string                `json:"name"`
	Options opencodeProviderOptions `json:"options"`
}
type opencodeConfig struct {
	Model    string                       `json:"model"`
	Provider map[string]opencodeProvider `json:"provider"`
}

// opencode reads OpenCode's opencode.json (+ optional auth.json for the token).
// model is "provider/model"; the provider block carries baseURL + apiKey
// (apiKey may be {env:VAR}). ok=false if missing/malformed or nothing usable.
func opencode(configPath, authPath string) (Creds, bool) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Creds{}, false
	}
	data = stripJSONC(data)
	var cfg opencodeConfig
	if json.Unmarshal(data, &cfg) != nil {
		return Creds{}, false
	}
	name, model := splitModel(cfg.Model)
	if name == "" {
		// No "provider/model" prefix: use the sole configured provider if unambiguous.
		if len(cfg.Provider) == 1 {
			for k := range cfg.Provider {
				name = k
			}
		} else {
			return Creds{}, false
		}
	}
	p := cfg.Provider[name]
	c := Creds{
		BaseURL:  p.Options.BaseURL,
		Token:    resolveEnv(p.Options.APIKey),
		Model:    model,
		Provider: firstNonEmpty(p.Name, name),
	}
	if c.Token == "" {
		c.Token = opencodeAuth(authPath, name)
	}
	if c.BaseURL == "" && c.Token == "" && c.Model == "" {
		return Creds{}, false
	}
	return c, true
}

// opencodeAuth reads a provider's token from OpenCode's auth.json (best-effort;
// handles {"<provider>": {"token": "..."}}). Empty if absent/other shape.
func opencodeAuth(authPath, provider string) string {
	data, err := os.ReadFile(authPath)
	if err != nil {
		return ""
	}
	var raw map[string]map[string]string
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}
	if entry, ok := raw[provider]; ok {
		return entry["token"]
	}
	return ""
}

// splitModel splits "provider/model" → (provider, model). No "/" → ("", s).
func splitModel(s string) (provider, model string) {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// stripJSONC removes // line comments and /* */ block comments from JSONC while
// preserving // and /* that appear inside string literals (e.g. https:// URLs).
func stripJSONC(data []byte) []byte {
	var out bytes.Buffer
	inStr := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inStr {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inStr = false
			}
			continue
		}
		if ch == '"' {
			inStr = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(data) && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i-- // outer loop will i++
			continue
		}
		if ch == '/' && i+1 < len(data) && data[i+1] == '*' {
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++ // skip closing */
			continue
		}
		out.WriteByte(ch)
	}
	return out.Bytes()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/toolconf/ -run TestOpenCode -v`
Expected: PASS (all three subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/toolconf/opencode.go internal/toolconf/opencode_test.go internal/toolconf/testdata/opencode.json internal/toolconf/testdata/opencode-auth-only.json internal/toolconf/testdata/opencode-auth.json internal/toolconf/testdata/opencode.jsonc
git commit -m "feat(toolconf): OpenCode opencode.json reader (JSONC, auth fallback)"
```

---

### Task 5: toolconf — Active dispatcher

**Files:**
- Modify: `internal/toolconf/toolconf.go`
- Test: `internal/toolconf/toolconf_test.go`

**Interfaces:**
- Produces: `func Active(appType string) (Creds, bool)` — dispatches `claude`/`codex`/`opencode` to their readers with default paths; unknown appType → `(Creds{}, false)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/toolconf/toolconf_test.go
package toolconf

import "testing"

func TestActiveUnknownApp(t *testing.T) {
	if _, ok := Active("nope"); ok {
		t.Errorf("want ok=false for unknown appType")
	}
}

func TestActiveClaudeHitsReader(t *testing.T) {
	// Default path ~/.claude/settings.json is unlikely in CI; we only assert
	// Active("claude") returns false (not panic) when the file is absent.
	// The claude() reader itself is unit-tested in Task 2.
	if _, ok := Active("claude"); ok {
		// ok=true only if the real file exists on this machine; both outcomes fine.
		t.Logf("Active(claude) found a real settings.json on this host")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolconf/ -run TestActive -v`
Expected: FAIL / build error (`Active undefined`).

- [ ] **Step 3: Write minimal implementation**

Rewrite `internal/toolconf/toolconf.go` to its final form (imports must precede the type, so the whole file is rewritten rather than appended). The `Creds` type from Task 2 stays; `Active` + helpers are added:
```go
// internal/toolconf/toolconf.go
package toolconf

import (
	"os"
	"path/filepath"
)

// Creds is the active provider's API endpoint + auth, read from a tool's own
// config files when cc-switch is absent. The zero value with ok=false means
// nothing usable was found.
type Creds struct {
	BaseURL  string
	Token    string
	Model    string
	Provider string
}

// Active resolves the active provider's Creds for appType from the tool's own
// native config files (used when cc-switch is absent). ok=false if the appType
// is unknown or nothing usable was found. Paths resolve from CODEX_HOME / home.
func Active(appType string) (Creds, bool) {
	home := userHome()
	switch appType {
	case "claude":
		return claude(filepath.Join(home, ".claude", "settings.json"))
	case "codex":
		return codex(codexConfigPath(home))
	case "opencode":
		return opencode(
			filepath.Join(home, ".config", "opencode", "opencode.json"),
			filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		)
	}
	return Creds{}, false
}

func codexConfigPath(home string) string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return filepath.Join(v, "config.toml")
	}
	return filepath.Join(home, ".codex", "config.toml")
}

func userHome() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	return h
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/toolconf/ -v`
Expected: PASS (all toolconf tests).

- [ ] **Step 5: Verify the whole package builds**

Run: `go build ./internal/toolconf/`
Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add internal/toolconf/toolconf.go internal/toolconf/toolconf_test.go
git commit -m "feat(toolconf): Active dispatcher with default paths"
```

---

### Task 6: Wire toolconf into activeCreds(); drop manual fallback

**Files:**
- Modify: `app.go` — `activeCreds()` (≈ lines 922–951) and `fetchLimits()` default branch (≈ lines 899–916).

**Interfaces:**
- Consumes: `toolconf.Active(appType string) (toolconf.Creds, bool)` (Task 5).
- Produces: `activeCreds()` now returns toolconf creds when cc-switch has no provider with a token.

- [ ] **Step 1: Read the current functions to confirm line ranges**

Run: `grep -n "func (a \*App) activeCreds\|func (a \*App) fetchLimits" app.go`
Open `app.go` at those offsets. The exact code is shown in Steps 2–3.

- [ ] **Step 2: Replace activeCreds() manual-token branch with toolconf**

Current tail of `activeCreds()` (≈ lines 947–950):
```go
	if mt := strings.TrimSpace(a.cfg.ManualToken); mt != "" {
		return a.cfg.ManualBaseURL, mt, true
	}
	return "", "", false
}
```
Replace with:
```go
	// Fallback: the selected tool's own native config (when cc-switch is absent
	// or has no provider with a token).
	if c, ok := toolconf.Active(app); ok && c.Token != "" {
		return c.BaseURL, c.Token, true
	}
	return "", "", false
}
```

- [ ] **Step 3: Remove the manual fallback in fetchLimits()'s default branch**

Current `default:` case in `fetchLimits()` (≈ lines 899–916):
```go
	default:
		// Unknown provider kind ...
		// If the manual config points to a known provider ...
		if mt := strings.TrimSpace(a.cfg.ManualToken); mt != "" {
			mb := strings.ToLower(a.cfg.ManualBaseURL)
			switch {
			case strings.Contains(mb, "deepseek"):
				a.applyDeepSeekBalance(mt)
			case strings.Contains(mb, "bigmodel.cn"), strings.Contains(mb, "z.ai"):
				a.applyLimits(glmusage.Fetch(a.cfg.ManualBaseURL, mt))
			}
		}
	}
}
```
Replace the `default:` body with:
```go
	default:
		// Unknown provider kind (e.g. OpenCode's opencode.ai relay). No
		// balance/quota API to call — leave plan data empty so the UI hides
		// those sections rather than erroring.
	}
}
```

- [ ] **Step 4: Add the toolconf import**

In `app.go`'s import block, add:
```go
	"cc-enhance/internal/toolconf"
```
(Check the existing import grouping; place it with the other `cc-enhance/internal/...` imports alphabetically.)

- [ ] **Step 5: Verify it builds (note: a.cfg.ManualToken/ManualBaseURL still referenced by dead methods — Task 8 removes them; build still passes because those methods still compile)**

Run: `go build ./...`
Expected: success (no output). The manual fields still exist in `OverlayConfig` (removed in Task 9), and the dead methods still reference them — that's fine for now.

- [ ] **Step 6: Commit**

```bash
git add app.go
git commit -m "refactor(app): activeCreds falls back to toolconf; drop manual fallback"
```

---

### Task 7: buildSnapshot — hardcode thresholds, seed model/provider from toolconf, drop cc-switch-absent chip

**Files:**
- Modify: `app.go` — `buildSnapshot()` (≈ lines 244–251 db-ready block; ≈ 259–283 provider block; ≈ 371–388 model resolution; ≈ 515–519 alert call).

**Interfaces:**
- Consumes: `toolconf.Active` (Task 5).
- Produces: no cc-switch-absent error chip; model/provider/baseURL seeded from toolconf; alert thresholds hardcoded.

- [ ] **Step 1: Hardcode the alert thresholds**

Find the `snapshot.EvaluateAlert(...)` call (≈ line 519). Current:
```go
		a.cfg.WarnPercent, a.cfg.WarnBalance, a.cfg.WarnHysteresis)
```
Replace the three arguments with literals:
```go
		85, 10, 10)
```

- [ ] **Step 2: Drop the cc-switch-absent error chip**

Current (≈ lines 244–251):
```go
	dbReady := true
	if a.db == nil {
		s.Errors = append(s.Errors, "无法打开 cc switch 数据库（请确认 cc switch 已安装）")
		dbReady = false
	} else if se := a.db.SchemaError(); se != nil {
		s.Errors = append(s.Errors, se.Error())
		dbReady = false
	}
```
Replace with (cc-switch-absent is now silent; only schema mismatch warns):
```go
	dbReady := true
	if a.db == nil {
		dbReady = false // silent: toolconf will provide provider/model/creds
	} else if se := a.db.SchemaError(); se != nil {
		s.Errors = append(s.Errors, se.Error())
		dbReady = false
	}
```

- [ ] **Step 3: Seed model/provider/baseURL from toolconf when cc-switch has no provider**

Declare fallback vars and fill the else-branch. Current provider block (≈ lines 269–283):
```go
	settingsConfigJSON := ""
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
	} else {
		s.Errors = append(s.Errors, fmt.Sprintf("未找到 %s 的当前 provider", app))
	}
```
Replace with:
```go
	settingsConfigJSON := ""
	var fallbackModel, fallbackProviderName, fallbackBaseURL string
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
		fallbackProviderName = tc.Provider
		fallbackBaseURL = tc.BaseURL
		s.Provider = snapshot.ProviderInfo{Name: tc.Provider}
	}
```

Then seed display/baseURL after model resolution. Current (≈ lines 372–388):
```go
	cfgDisplay, baseURL := modelresolve.Resolve(app, settingsConfigJSON, claudeTier)
	display := cfgDisplay
	if display == "" {
		display = liveModel
	}
```
Add fallback seeding immediately after the `if display == "" { display = liveModel }` line:
```go
	if display == "" {
		display = fallbackModel
	}
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
```
(Leave the rest of the model block — the OpenCode opencode.ai display override and `s.Model = snapshot.ModelInfo{...}` — unchanged; `display`/`baseURL` now carry the toolconf values when cc-switch had nothing.)

- [ ] **Step 4: Verify it builds**

Run: `go build ./...`
Expected: success. (`fallbackProviderName` is set but only used to populate `s.Provider.Name` via `tc.Provider` directly — if `go vet` flags `fallbackProviderName` unused, drop that local and use `tc.Provider` inline. Prefer to keep it only if referenced; otherwise remove.)

Run: `go vet ./...`
Expected: success. If `fallbackProviderName` is unused, remove it from the declaration and the assignment.

- [ ] **Step 5: Commit**

```bash
git add app.go
git commit -m "refactor(app): seed model/provider from toolconf; hardcode alert thresholds"
```

---

### Task 8: Delete dead settings methods + types from app.go

**Files:**
- Modify: `app.go` — delete `ManualConfig`, `ProviderCred`, `GetManualConfig`, `SetManualConfig`, `ListProviders`, `ApplyProviderCreds`, `SetWarnThresholds`.

**Interfaces:**
- Produces: `app.go` no longer references `a.cfg.ManualBaseURL`/`ManualToken`/`WarnPercent`/`WarnBalance`/`WarnHysteresis` anywhere (prep for Task 9).

- [ ] **Step 1: Confirm the symbols and their spans**

Run: `grep -n "type ManualConfig\|type ProviderCred\|func (a \*App) GetManualConfig\|func (a \*App) SetManualConfig\|func (a \*App) ListProviders\|func (a \*App) ApplyProviderCreds\|func (a \*App) SetWarnThresholds" app.go`
Note each line; delete each whole declaration (type or func) from its line through the closing `}`.

The spans (from current source):
- `ManualConfig` type (≈ lines 989–997).
- `GetManualConfig` (≈ 999–1008).
- `SetWarnThresholds` (≈ 1010–1026).
- `SetManualConfig` (≈ 1028–1042).
- `ProviderCred` type (≈ 1044–1050).
- `ListProviders` (≈ 1052–1067).
- `ApplyProviderCreds` (≈ 1069 through its closing `}` ≈ 1093).

- [ ] **Step 2: Delete each declaration**

Remove all seven spans listed above. After deletion, verify none of these names remain:
Run: `grep -n "ManualConfig\|ProviderCred\|GetManualConfig\|SetManualConfig\|ListProviders\|ApplyProviderCreds\|SetWarnThresholds" app.go`
Expected: no matches.

- [ ] **Step 3: Verify it builds**

Run: `go build ./...`
Expected: success (the frontend bindings still reference these via `wailsjs/` but that's TS, not Go — Go build is unaffected; bindings cleaned in Task 10).

Run: `go vet ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add app.go
git commit -m "refactor(app): remove dead settings methods + types"
```

---

### Task 9: Remove OverlayConfig fields + ccswitchdb ProviderCreds

**Files:**
- Modify: `internal/config/paths.go` — drop `ManualBaseURL`, `ManualToken`, `WarnPercent`, `WarnBalance`, `WarnHysteresis` + their defaults.
- Modify: `internal/ccswitchdb/queries.go` — drop `ProviderCreds` (func) and `ProviderCred` (type).

**Interfaces:**
- Produces: `OverlayConfig` no longer carries manual/warn fields; `ccswitchdb` no longer exposes `ProviderCreds`.

- [ ] **Step 1: Remove the OverlayConfig fields**

In `internal/config/paths.go`, delete from the `OverlayConfig` struct (≈ lines 34–39):
```go
	ManualBaseURL     string            `json:"manualBaseUrl"`
	ManualToken       string            `json:"manualToken"`
```
```go
	WarnPercent       int               `json:"warnPercent"`    // GLM percent threshold (default 85)
	WarnBalance       float64           `json:"warnBalance"`    // DeepSeek balance threshold in CNY (default 10)
	WarnHysteresis    float64           `json:"warnHysteresis"` // hysteresis band width (default 10)
```

- [ ] **Step 2: Remove their defaults**

In `DefaultOverlayConfig()` (≈ lines 121–123), delete:
```go
		WarnPercent:    85,
		WarnBalance:    10,
		WarnHysteresis: 10,
```

- [ ] **Step 3: Remove ccswitchdb ProviderCreds + ProviderCred**

In `internal/ccswitchdb/queries.go`, delete the `ProviderCred` type (≈ lines 89–95) and the `ProviderCreds` func (≈ lines 97–122).

- [ ] **Step 4: Verify nothing references them**

Run: `grep -rn "ManualBaseURL\|ManualToken\|WarnPercent\|WarnBalance\|WarnHysteresis\|ProviderCreds" --include=*.go .`
Expected: no matches (the `docs/` hits are markdown, not Go — fine; `wailsjs/` hits are TS, cleaned in Task 10).

Run: `go build ./...`
Expected: success.

Run: `go vet ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add internal/config/paths.go internal/ccswitchdb/queries.go
git commit -m "refactor(config,ccswitchdb): drop manual/warn fields + ProviderCreds query"
```

---

### Task 10: Frontend — remove settings UI + regenerate bindings

**Files:**
- Modify: `frontend/src/App.vue`
- Modify (regenerate or hand-edit): `frontend/wailsjs/go/main/App.d.ts`, `frontend/wailsjs/go/main/App.js`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Produces: no gear button, no settings card; imports/refs/functions/CSS removed; bindings drop the 5 removed methods + 2 model types.

- [ ] **Step 1: Remove the settings imports**

In `frontend/src/App.vue` line 4, change:
```ts
import { GetSnapshot, ListApps, SetApp, SetCollapsed, SetBarWidth, GetManualConfig, SetManualConfig, ListProviders, SetWarnThresholds, SetCardHeight, ApplyProviderCreds } from '../wailsjs/go/main/App';
```
to:
```ts
import { GetSnapshot, ListApps, SetApp, SetCollapsed, SetBarWidth, SetCardHeight } from '../wailsjs/go/main/App';
```

- [ ] **Step 2: Remove the settings refs + functions**

Delete the block from `// Settings panel (manual GLM key for quota fetch).` (≈ line 186) through the end of `saveCfg` (≈ line 227) — i.e. `settingsOpen`, `cfgBase`, `cfgToken`, `cfgHasToken`, `cfgWarnPct`, `cfgWarnBal`, `cfgProviderId`, `providers`, `openSettings`, `onProviderChange`, `saveCfg`.

- [ ] **Step 3: Remove the gear button**

In the template, find `<button class="gear-btn" title="设置" aria-label="设置" @click="openSettings">...</button>` (≈ line 309) and delete the entire element (including its inner icon markup, through its closing `</button>`).

- [ ] **Step 4: Remove the settings card**

Delete the entire `<div class="card settings" v-else-if="snap && settingsOpen">...</div>` block (≈ lines 277 through its matching close `</div>`).

- [ ] **Step 5: Remove the settings CSS**

Delete the `.settings` rules and the `.gear-btn` rules (≈ lines 404, 411–413), including:
```css
.settings .form { ... }
.settings label { ... }
.settings select, .settings input { ... }
.settings select:focus, .settings input:focus { ... }
.settings input:focus { ... }
.settings .hint { ... }
.save-btn { ... }
.gear-btn { ... }
.gear-btn:hover, .collapse-btn:hover { ... }
```
(Keep `.collapse-btn` hover if it shares the rule — split it so collapse hover stays.)

- [ ] **Step 6: Regenerate wails bindings**

Run:
```bash
wails generate module
```
If the `wails` CLI is unavailable, hand-edit the three files:
- `frontend/wailsjs/go/main/App.d.ts`: remove lines `export function ApplyProviderCreds`, `GetManualConfig`, `ListProviders`, `SetManualConfig`, `SetWarnThresholds`.
- `frontend/wailsjs/go/main/App.js`: remove the matching `export function` blocks.
- `frontend/wailsjs/go/models.ts`: remove the `ManualConfig` and `ProviderCred` interfaces.

- [ ] **Step 7: Build the frontend to verify**

Run:
```bash
cd frontend && npm run build
```
Expected: build succeeds with no references to removed symbols. (Type errors about removed imports would surface here.)

- [ ] **Step 8: Commit**

```bash
git add frontend/src/App.vue frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/models.ts
git commit -m "feat(frontend): remove settings panel + gear button"
```

---

### Task 11: Full build + manual verification

**Files:** none (verification only).

- [ ] **Step 1: Full Go build + vet + tests**

Run:
```bash
go build ./...
go vet ./...
go test ./internal/toolconf/ ./internal/ccswitchdb/
```
Expected: all pass.

- [ ] **Step 2: Confirm no residual references**

Run:
```bash
grep -rn "ManualBaseURL\|ManualToken\|WarnPercent\|WarnBalance\|WarnHysteresis\|ProviderCreds\|GetManualConfig\|SetManualConfig\|ListProviders\|ApplyProviderCreds\|SetWarnThresholds\|ManualConfig\|ProviderCred\|settingsOpen\|openSettings\|gear-btn" --include=*.go --include=*.ts --include=*.vue .
```
Expected: only `docs/` markdown hits (historical plans/specs). No source references.

- [ ] **Step 3: Build the Wails app**

Run:
```bash
wails build
```
Expected: success; produces the overlay binary.

- [ ] **Step 4: Manual — with cc-switch (common case)**

Run the built overlay. Confirm:
- No gear button in the header.
- Provider/model/today/month/session/quota all populate as before (cc-switch DB drives them).
- Quota (GLM % / DeepSeek ¥) still appears for the active provider.

- [ ] **Step 5: Manual — without cc-switch (fallback)**

Temporarily rename the cc-switch DB, e.g.:
```bash
mv ~/.cc-switch/cc-switch.db ~/.cc-switch/cc-switch.db.bak
```
Run the overlay. Confirm:
- No "无法打开 cc switch 数据库" chip; no settings panel.
- Model name + provider label show (read from the active tool's config).
- Usage sections render empty/zero silently (no error chip).
- If the tool's config has a GLM/DeepSeek token in env, quota still resolves; otherwise quota is simply empty.
Restore:
```bash
mv ~/.cc-switch/cc-switch.db.bak ~/.cc-switch/cc-switch.db
```

- [ ] **Step 6: Final commit (if any verification fixups)**

If Steps 1–5 needed no changes, no commit. Otherwise commit the fixups.

---

## Self-Review Notes

- **Spec coverage:** removal surface (Task 8/9/10), toolconf readers (1–5), activeCreds fallback (6), model seed + thresholds + chip (7), TOML dep (3), tests (1–5, 11), manual verify incl. no-cc-switch (11). All spec sections mapped.
- **Type consistency:** `Creds` defined once (Task 2 `toolconf.go`), used identically by all readers and `Active`; `toolconf.Active(appType) (Creds, bool)` signature matches usage in Tasks 6 & 7. `firstNonEmpty` introduced in Task 3 (codex), reused in Task 4 (opencode) — defined once.
- **Compile ordering:** Tasks 6–7 leave dead methods that still reference the soon-to-be-removed config fields; the build stays green because the fields still exist. Task 8 removes the methods; Task 9 removes the fields only after all Go references are gone. Frontend bindings (Task 10) are TS and don't affect `go build`.
