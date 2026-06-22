// Package claudecode reads Claude Code's live session/transcript files.
// Only used for app_type=claude (the instant in/out fast path); other apps
// fall back to cc switch's proxy_request_logs.
package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Session is one entry from ~/.claude/sessions/<PID>.json.
type Session struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// CurrentSession scans the sessions directory and returns the most recently
// updated live session, or nil if none exists.
func CurrentSession(sessionsDir string) *Session {
	entries, err := filepath.Glob(filepath.Join(sessionsDir, "*.json"))
	if err != nil || len(entries) == 0 {
		return nil
	}
	var sessions []Session
	for _, e := range entries {
		data, err := os.ReadFile(e)
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.SessionID == "" {
			continue
		}
		sessions = append(sessions, s)
	}
	if len(sessions) == 0 {
		return nil
	}
	// Keep only sessions whose PID is still running — Claude Code leaves stale
	// session files after exit, and a dead PID would otherwise be reported "live".
	live := sessions[:0]
	for _, s := range sessions {
		if pidAlive(s.PID) {
			live = append(live, s)
		}
	}
	sessions = live
	if len(sessions) == 0 {
		return nil
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].UpdatedAt > sessions[j].UpdatedAt })
	return &sessions[0]
}
