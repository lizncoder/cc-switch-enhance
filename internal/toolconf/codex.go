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
