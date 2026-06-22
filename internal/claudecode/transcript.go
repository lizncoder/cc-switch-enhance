package claudecode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// AssistantUsage is the token usage of the latest assistant message in a
// transcript. This is the instant input/output shown in the card before cc
// switch's DB row is written.
type AssistantUsage struct {
	Model         string
	InputTokens   int64
	OutputTokens  int64
	CacheCreate   int64
	CacheRead     int64
	Timestamp     time.Time
	HasUsage      bool
}

type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Model string    `json:"model"`
		Usage *rawUsage `json:"usage"`
	} `json:"message"`
}

type rawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// FindTranscript locates a session's JSONL by sessionId, bypassing cwd
// escaping (which drops non-ASCII path segments). Returns "" if not found.
func FindTranscript(projectsDir, sessionID string) string {
	pattern := filepath.Join(projectsDir, "*", sessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	if len(matches) == 1 {
		return matches[0]
	}
	// Prefer the newest by mtime.
	sort.Slice(matches, func(i, j int) bool {
		fi, _ := os.Stat(matches[i])
		fj, _ := os.Stat(matches[j])
		return fi.ModTime().After(fj.ModTime())
	})
	return matches[0]
}

// ParseLatestUsage reads a transcript from offset and returns the latest
// assistant message's usage plus the new offset (file size). If the file
// shrank (replaced), it re-reads from the start.
func ParseLatestUsage(path string, offset int64) (AssistantUsage, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return AssistantUsage{}, offset
	}
	size := info.Size()
	if size < offset {
		offset = 0
	}
	if size == offset {
		return AssistantUsage{}, offset
	}
	f, err := os.Open(path)
	if err != nil {
		return AssistantUsage{}, offset
	}
	defer f.Close()
	if _, err := f.Seek(offset, 0); err != nil {
		return AssistantUsage{}, offset
	}
	var latest AssistantUsage
	sc := bufio.NewScanner(f)
	// Transcripts can have very long lines (tool results); raise the limit.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var tl transcriptLine
		if json.Unmarshal(line, &tl) != nil {
			continue
		}
		if tl.Type != "assistant" || tl.Message.Usage == nil {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, tl.Timestamp)
		latest = AssistantUsage{
			Model:        tl.Message.Model,
			InputTokens:  tl.Message.Usage.InputTokens,
			OutputTokens: tl.Message.Usage.OutputTokens,
			CacheCreate:  tl.Message.Usage.CacheCreationInputTokens,
			CacheRead:    tl.Message.Usage.CacheReadInputTokens,
			Timestamp:    ts,
			HasUsage:     true,
		}
	}
	return latest, size
}
