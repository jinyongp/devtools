package cli

import (
	"github.com/jinyongp/devtools/internal/testfixture"
	"testing"
)

func commandFixture(t *testing.T, kind string) []string {
	t.Helper()
	switch kind {
	case "level":
		return []string{testfixture.Copy(t, "env"), "LEVEL"}
	case "marker":
		return []string{testfixture.Copy(t, "env"), "MARKER"}
	case "token":
		return []string{testfixture.Copy(t, "env"), "TOKEN"}
	case "cwd-level-first", "cwd-level", "raw-fail", "secret-and-true", "plain", "cat", "hold":
		return []string{testfixture.Copy(t, kind)}
	default:
		t.Fatal("unknown command fixture", kind)
		return nil
	}
}
