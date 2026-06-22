package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func writeJSONL(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestParseLatestUsage(t *testing.T) {
	dir := t.TempDir()
	// Latest assistant message wins; user lines and non-usage lines are skipped.
	p := writeJSONL(t, dir, "s.jsonl",
		`{"type":"user","timestamp":"2026-06-22T10:00:00+08:00"}`+"\n"+
			`{"type":"assistant","timestamp":"2026-06-22T10:00:01+08:00","message":{"model":"glm-5.2","usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":200,"cache_creation_input_tokens":0}}}`+"\n"+
			`{"type":"assistant","timestamp":"2026-06-22T10:00:02+08:00","message":{"model":"glm-5.2","usage":{"input_tokens":500,"output_tokens":50,"cache_read_input_tokens":1000,"cache_creation_input_tokens":5}}}`+"\n")

	u, off := ParseLatestUsage(p, 0)
	if !u.HasUsage {
		t.Fatal("HasUsage false")
	}
	if u.Model != "glm-5.2" {
		t.Errorf("model=%q want glm-5.2", u.Model)
	}
	if u.InputTokens != 500 || u.OutputTokens != 50 || u.CacheRead != 1000 || u.CacheCreate != 5 {
		t.Errorf("usage = in=%d out=%d cr=%d cc=%d, want 500/50/1000/5", u.InputTokens, u.OutputTokens, u.CacheRead, u.CacheCreate)
	}
	// A full read advances the offset to EOF.
	info, _ := os.Stat(p)
	if off != info.Size() {
		t.Errorf("offset=%d want EOF %d", off, info.Size())
	}
}

func TestParseLatestUsageOffsetIdempotent(t *testing.T) {
	dir := t.TempDir()
	content := `{"type":"assistant","timestamp":"2026-06-22T10:00:01+08:00","message":{"model":"a","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n"
	p := writeJSONL(t, dir, "s.jsonl", content)

	_, off := ParseLatestUsage(p, 0)
	if off != int64(len(content)) {
		t.Fatalf("offset=%d want EOF %d", off, len(content))
	}
	// Re-reading from EOF yields no new usage and a stable offset.
	u, off2 := ParseLatestUsage(p, off)
	if u.HasUsage {
		t.Error("expected no new usage at EOF offset")
	}
	if off2 != off {
		t.Errorf("offset drifted %d -> %d", off, off2)
	}
}

func TestParseLatestUsageShrink(t *testing.T) {
	// If the file shrank (replaced), offset > size should reset and re-read.
	dir := t.TempDir()
	p := writeJSONL(t, dir, "s.jsonl",
		`{"type":"assistant","timestamp":"2026-06-22T10:00:01+08:00","message":{"model":"a","usage":{"input_tokens":99,"output_tokens":0}}}`+"\n")
	u, _ := ParseLatestUsage(p, 99999) // offset way past EOF
	if !u.HasUsage || u.InputTokens != 99 {
		t.Errorf("shrink should re-read from 0: %+v", u)
	}
}
