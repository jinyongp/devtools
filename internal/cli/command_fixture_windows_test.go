package cli

import (
	"github.com/jinyongp/devtools/internal/testfixture"
	"testing"
)

func commandFixture(t *testing.T, kind string) []string {
	t.Helper()
	switch kind {
	case "true", "version", "version-bad", "check-marker", "bind", "output-fail":
		return []string{testfixture.Copy(t, kind)}
	case "dev", "zeta":
		return []string{testfixture.Copy(t, "print"), kind}
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

func versionToolFixture(t *testing.T, bad bool) string {
	t.Helper()
	mode := "version"
	if bad {
		mode = "version-bad"
	}
	return testfixture.Copy(t, mode)
}
