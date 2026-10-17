//go:build !windows

package execution

import (
	"os/exec"
	"testing"
)

func trueExecutable(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	return path
}
