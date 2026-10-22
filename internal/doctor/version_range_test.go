package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
)

func TestRangeProbeAndCompleteConflict(t *testing.T) {
	path := doctorVersionFixture(t, privateTempDir(t))
	for _, c := range []struct {
		ranges  []string
		reasons []string
	}{
		{[]string{"1", "^1.2.0", "1.2"}, []string{"version_match", "version_match", "version_match"}},
		{[]string{"1", "1.9"}, []string{"version_match", "version_mismatch"}},
		{[]string{"1", "2"}, []string{"version_conflict", "version_conflict"}},
		{[]string{"1 || 2", "1 || 3", "2 || 3"}, []string{"version_conflict", "version_conflict", "version_conflict"}},
	} {
		tools := map[string]project.Tool{}
		for i, r := range c.ranges {
			executable := path
			if i%2 == 1 {
				executable = filepath.Dir(path) + string(os.PathSeparator) + "." + string(os.PathSeparator) + filepath.Base(path)
			}
			tools[fmt.Sprint(i)] = project.Tool{Executable: executable, Version: r}
		}
		checks := CheckRequirements(WithCache(context.Background()), Input{Directory: t.TempDir(), Requirements: project.Requirements{Tools: tools}})
		if len(checks) != len(c.reasons) {
			t.Fatal(checks)
		}
		for i, check := range checks {
			if check.Reason != c.reasons[i] || check.Facts["expected_version"] != c.ranges[i] || check.Facts["observed_version"] != "1.2.3" {
				t.Errorf("%q: %+v", c.ranges, checks)
			}
		}
	}
}
