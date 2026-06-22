// Package modelresolve derives the display model name + base URL for an app
// from its provider settings_config JSON.
package modelresolve

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Resolve returns the configured display model name and base URL for an app.
// claudeTier is Claude Code settings.json's `model` (opus/sonnet/haiku).
// Returns empty display if it can't be derived (caller falls back to the
// live log model).
func Resolve(app, settingsConfigJSON, claudeTier string) (display, baseURL string) {
	var sc map[string]any
	_ = json.Unmarshal([]byte(settingsConfigJSON), &sc)
	switch strings.ToLower(app) {
	case "claude":
		display, baseURL = resolveClaude(sc, claudeTier)
	case "opencode":
		display, baseURL = resolveOpencode(sc)
	case "codex":
		// model lives inside an opaque config blob; rely on the live log model.
		baseURL = ""
	case "gemini":
		display = "gemini-official"
	}
	return stripSuffix(display), baseURL
}

func resolveClaude(sc map[string]any, tier string) (string, string) {
	env := asMap(sc["env"])
	base := str(env, "ANTHROPIC_BASE_URL")
	t := strings.ToUpper(strings.TrimSpace(tier))
	if t != "" {
		if n := str(env, "ANTHROPIC_DEFAULT_"+t+"_MODEL_NAME"); n != "" {
			return n, base
		}
		if m := str(env, "ANTHROPIC_DEFAULT_"+t+"_MODEL"); m != "" {
			return m, base
		}
	}
	if m := str(env, "ANTHROPIC_MODEL"); m != "" {
		return m, base
	}
	if m := str(sc, "model"); m != "" {
		return m, base
	}
	return "", base
}

func resolveOpencode(sc map[string]any) (string, string) {
	baseURL := str(asMap(sc["options"]), "baseURL")
	models := asMap(sc["models"])
	// models is { "<id>": { "name": "<Display>" } }; take the first named model.
	for _, v := range models {
		m := asMap(v)
		if n := str(m, "name"); n != "" {
			return n, baseURL
		}
	}
	return "", baseURL
}

// HostOf extracts the host:port from a base URL for compact display.
func HostOf(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		// Fall back to the raw string trimmed of scheme.
		s := strings.TrimPrefix(rawURL, "https://")
		s = strings.TrimPrefix(s, "http://")
		return strings.TrimRight(s, "/")
	}
	return u.Host
}

// Normalize lowercases + strips model suffixes for match comparison.
func Normalize(s string) string {
	return strings.ToLower(stripSuffix(s))
}

// stripSuffix removes trailing bracketed tags like "[1M]" / "[200k]".
func stripSuffix(s string) string {
	return strings.TrimSpace(strings.TrimSpace(trimTrailingBrackets(s)))
}

// trimTrailingBrackets removes any number of trailing "[...]" segments.
func trimTrailingBrackets(s string) string {
	for {
		trimmed := strings.TrimSpace(s)
		if strings.HasSuffix(trimmed, "]") {
			i := strings.LastIndex(trimmed, "[")
			if i >= 0 {
				s = trimmed[:i]
				continue
			}
		}
		return s
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func str(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case string:
			return t
		default:
			if t != nil {
				return strings.TrimSpace(toStr(t))
			}
		}
	}
	return ""
}

func toStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
