package doctor

import (
	"github.com/jinyongp/devtools/internal/testfixture"
	"testing"
)

func doctorVersionFixture(t *testing.T, dir string) string {
	return testfixture.CopyIn(t, dir, "version")
}

func doctorProbeFixture(t *testing.T, dir string, wait bool) string {
	t.Helper()
	mode := "version-fail"
	if wait {
		mode = "hold"
	}
	return testfixture.CopyIn(t, dir, mode)
}
