// internal/toolconf/env_test.go
package toolconf

import "testing"

func TestResolveEnv(t *testing.T) {
	t.Setenv("MY_KEY", "secret-value")

	cases := []struct{ in, want string }{
		{"{env:MY_KEY}", "secret-value"},
		{"{env:UNSET_VAR}", ""},        // unset → empty
		{"plain-string", "plain-string"}, // not an env ref → unchanged
		{"https://x.io/api", "https://x.io/api"}, // literal preserved
		{"", ""},
	}
	for _, c := range cases {
		if got := resolveEnv(c.in); got != c.want {
			t.Errorf("resolveEnv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
