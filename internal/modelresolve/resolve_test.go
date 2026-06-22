package modelresolve

import "testing"

func TestResolveClaudeTierModelName(t *testing.T) {
	cfg := `{"env":{"ANTHROPIC_BASE_URL":"https://open.bigmodel.cn/api/anthropic","ANTHROPIC_DEFAULT_SONNET_MODEL_NAME":"glm-5.2"}}`
	disp, base := Resolve("claude", cfg, "sonnet")
	if disp != "glm-5.2" {
		t.Errorf("disp=%q want glm-5.2", disp)
	}
	if base != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("base=%q", base)
	}
}

func TestResolveClaudeTierFallbackToAnthropicModel(t *testing.T) {
	// tier-specific name missing -> fall through to ANTHROPIC_MODEL
	cfg := `{"env":{"ANTHROPIC_BASE_URL":"https://x","ANTHROPIC_MODEL":"my-model"}}`
	disp, _ := Resolve("claude", cfg, "opus")
	if disp != "my-model" {
		t.Errorf("disp=%q want my-model", disp)
	}
}

func TestResolveClaudeStripsSuffix(t *testing.T) {
	cfg := `{"env":{"ANTHROPIC_MODEL":"glm-5.2[1M]"}}`
	disp, _ := Resolve("claude", cfg, "")
	if disp != "glm-5.2" {
		t.Errorf("disp=%q want glm-5.2 (suffix stripped)", disp)
	}
}

func TestResolveGemini(t *testing.T) {
	disp, _ := Resolve("gemini", "{}", "")
	if disp != "gemini-official" {
		t.Errorf("disp=%q want gemini-official", disp)
	}
}

func TestResolveCodexEmpty(t *testing.T) {
	disp, base := Resolve("codex", `{"env":{}}`, "")
	if disp != "" || base != "" {
		t.Errorf("codex should derive nothing, got disp=%q base=%q", disp, base)
	}
}

func TestResolveOpencode(t *testing.T) {
	cfg := `{"options":{"baseURL":"https://opencode.ai"},"models":{"a":{"name":"deepseek-chat"}}}`
	disp, base := Resolve("opencode", cfg, "")
	if disp != "deepseek-chat" {
		t.Errorf("disp=%q want deepseek-chat", disp)
	}
	if base != "https://opencode.ai" {
		t.Errorf("base=%q", base)
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://open.bigmodel.cn/api/anthropic": "open.bigmodel.cn",
		"http://localhost:8080":                  "localhost:8080",
		"":                                       "",
		"not-a-url":                              "not-a-url",
	}
	for in, want := range cases {
		if got := HostOf(in); got != want {
			t.Errorf("HostOf(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	if Normalize("GLM-5.2[1M]") != "glm-5.2" {
		t.Errorf("Normalize(GLM-5.2[1M])=%q want glm-5.2", Normalize("GLM-5.2[1M]"))
	}
	if Normalize("Sonnet") != "sonnet" {
		t.Errorf("Normalize should lowercase")
	}
}
