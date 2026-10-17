package execution

import (
	"github.com/jinyongp/devtools/internal/testfixture"
	"testing"
)

func trueExecutable(t *testing.T) string { return testfixture.Copy(t, "true") }
