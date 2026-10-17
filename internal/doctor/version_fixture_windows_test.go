package doctor

import (
	"github.com/jinyongp/devtools/internal/testfixture"
	"testing"
)

func doctorVersionFixture(t *testing.T, dir string) string {
	return testfixture.CopyIn(t, dir, "version")
}
