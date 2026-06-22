// internal/toolconf/env.go
package toolconf

import (
	"os"
	"strings"
)

// resolveEnv expands OpenCode's {env:VAR} references to the value of the named
// environment variable. Anything that isn't a {env:...} literal is returned
// unchanged (including when VAR is unset, which yields "").
func resolveEnv(s string) string {
	const prefix = "{env:"
	const suffix = "}"
	if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix) {
		name := s[len(prefix) : len(s)-len(suffix)]
		if name != "" {
			return os.Getenv(name)
		}
	}
	return s
}
