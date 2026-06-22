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
