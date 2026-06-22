package claudecode

import (
	"sync"
	"time"
)

// Watcher polls Claude Code's session registry + live transcript and exposes
// the current session and the latest assistant-message usage. Polling (rather
// than fsnotify) keeps the lifecycle simple and survives file replacement.
type Watcher struct {
	sessionsDir string
	projectsDir string

	mu             sync.Mutex
	current        *Session
	transcriptPath string
	offset         int64
	latest         AssistantUsage

	updated chan struct{}
	stop    chan struct{}
	started bool
}

// NewWatcher creates a (not-yet-started) watcher.
func NewWatcher(sessionsDir, projectsDir string) *Watcher {
	return &Watcher{
		sessionsDir: sessionsDir,
		projectsDir: projectsDir,
		updated:     make(chan struct{}, 1),
		stop:        make(chan struct{}),
	}
}

// Start launches the poll loop. Safe to call once.
func (w *Watcher) Start() {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.mu.Unlock()
	go w.loop()
}

// Stop terminates the poll loop.
func (w *Watcher) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
}

// Updated returns a channel signaled (non-blocking, coalesced) when the
// current session or latest usage changes.
func (w *Watcher) Updated() <-chan struct{} { return w.updated }

// Current returns the latest known live session (nil if none).
func (w *Watcher) Current() *Session {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current
}

// LatestUsage returns the latest assistant-message usage from the transcript.
func (w *Watcher) LatestUsage() AssistantUsage {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.latest
}

func (w *Watcher) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	// Prime immediately so the app gets data without waiting a tick.
	w.poll()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

func (w *Watcher) poll() {
	changed := false
	sess := CurrentSession(w.sessionsDir)

	w.mu.Lock()
	prevID := ""
	if w.current != nil {
		prevID = w.current.SessionID
	}
	if sess == nil {
		if w.current != nil {
			w.current = nil
			w.transcriptPath = ""
			w.offset = 0
			w.latest = AssistantUsage{}
			changed = true
		}
		w.mu.Unlock()
		if changed {
			w.signal()
		}
		return
	}
	if sess.SessionID != prevID {
		// Session changed: reset transcript state, will resolve below.
		w.current = sess
		w.transcriptPath = ""
		w.offset = 0
		w.latest = AssistantUsage{}
		changed = true
	} else {
		w.current = sess // refresh status/updatedAt
	}

	if w.transcriptPath == "" {
		w.transcriptPath = FindTranscript(w.projectsDir, sess.SessionID)
		w.offset = 0
	}
	path := w.transcriptPath
	offset := w.offset
	w.mu.Unlock()

	if path == "" {
		if changed {
			w.signal()
		}
		return
	}

	latest, newOffset := ParseLatestUsage(path, offset)

	w.mu.Lock()
	w.offset = newOffset
	if latest.HasUsage && (!w.latest.HasUsage || !latest.Timestamp.Equal(w.latest.Timestamp)) {
		w.latest = latest
		changed = true
	}
	w.mu.Unlock()

	if changed {
		w.signal()
	}
}

func (w *Watcher) signal() {
	select {
	case w.updated <- struct{}{}:
	default: // coalesce: a wake-up is already pending
	}
}
