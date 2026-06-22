//go:build !windows

package claudecode

// pidAlive is a no-op stub off Windows (the overlay targets Windows; this keeps
// the package compiling for the cross-platform test/internal packages).
func pidAlive(pid int) bool { return pid > 0 }
