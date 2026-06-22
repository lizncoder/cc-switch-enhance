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
	Name    string                  `json:"name"`
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
