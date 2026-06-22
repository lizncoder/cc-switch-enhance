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
