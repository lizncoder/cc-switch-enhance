//go:build windows

package main

import (
	"os"
	"testing"
)

// TestCCSwitchCandidatesResolve proves the Start-Menu-shortcut resolution finds
// a real cc-switch.exe regardless of install location (this machine installed
// to D:\APP\ccSwitch\, which no hardcoded path matches). Skips if cc-switch
// isn't installed.
func TestCCSwitchCandidatesResolve(t *testing.T) {
	cands := ccSwitchExeCandidates()
	t.Logf("candidates: %v", cands)
	found := ""
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			found = c
			break
		}
	}
	if found == "" {
		t.Skipf("cc-switch exe not found in any candidate (not installed?)")
	}
	t.Logf("resolved cc-switch exe: %s", found)
}

func TestFindCCSwitchShortcuts(t *testing.T) {
	lnks := findCCSwitchShortcuts()
	if len(lnks) == 0 {
		t.Skip("no cc-switch Start Menu shortcut found")
	}
	for _, l := range lnks {
		t.Logf("shortcut %s -> %q", l, resolveShortcut(l))
	}
}
