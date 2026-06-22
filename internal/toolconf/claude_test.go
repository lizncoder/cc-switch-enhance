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
