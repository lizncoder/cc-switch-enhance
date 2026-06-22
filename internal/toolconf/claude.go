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
