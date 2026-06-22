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
