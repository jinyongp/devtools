package redact

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTypedMatcherLimitsReportRejectionCounts(t *testing.T) {
	for _, kind := range []string{"patterns", "pattern_bytes", "total_pattern_bytes", "states", "outputs_per_state"} {
		t.Run(kind, func(t *testing.T) {
			patterns := [][]byte{}
			switch kind {
			case "patterns":
				for i := 0; i <= MaxPatterns; i++ {
					patterns = append(patterns, []byte(fmt.Sprintf("pattern-%04d", i)))
				}
			case "pattern_bytes":
				patterns = append(patterns, bytes.Repeat([]byte{'a'}, MaxPatternBytes+1))
			case "total_pattern_bytes":
				for i := 0; i < 64; i++ {
					p := bytes.Repeat([]byte{'a'}, MaxPatternBytes)
					copy(p[len(p)-2:], []byte(fmt.Sprintf("%02d", i)))
					patterns = append(patterns, p)
				}
				patterns = append(patterns, []byte("z"))
			case "states":
				for i := 0; i < 5; i++ {
					patterns = append(patterns, bytes.Repeat([]byte{byte('a' + i)}, MaxPatternBytes))
				}
			case "outputs_per_state":
				for i := 1; i <= MaxOutputsPerState+1; i++ {
					patterns = append(patterns, []byte(strings.Repeat("a", i)))
				}
			}
			_, err := New(patterns)
			var limit *LimitError
			if !errors.Is(err, ErrLimit) || !errors.As(err, &limit) || !limit.Valid() || limit.Kind != kind || limit.Observed != limit.Limit+1 {
				t.Fatal(err, limit)
			}
		})
	}
}

func TestDuplicateAndEscapedPatternsUseNormalizedCounts(t *testing.T) {
	patterns := make([][]byte, MaxPatterns+1)
	for i := range patterns {
		patterns[i] = []byte("<same>")
	}
	m, err := New(patterns)
	if err != nil {
		t.Fatal(err)
	}
	m.Destroy()
}
