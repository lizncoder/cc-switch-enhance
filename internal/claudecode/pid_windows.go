//go:build windows

package claudecode

import "golang.org/x/sys/windows"

// stillActive is the GetExitCodeProcess sentinel for a running process.
const stillActive = 259

// pidAlive reports whether the process with the given PID is still running.
// Claude Code leaves stale session files after exit; without this check a dead
// PID would be reported "live", which (via the alert gate in app.go) silently
// suppresses quota warnings and shows frozen usage forever.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false // access denied or no such process -> treat as not live
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
