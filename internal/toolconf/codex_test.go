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
