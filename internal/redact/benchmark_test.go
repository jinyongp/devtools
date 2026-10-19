package redact

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func benchmarkPatterns(count, length int) [][]byte {
	result := make([][]byte, count)
	for i := range result {
		prefix := fmt.Sprintf("dvt_%08d_", i)
		result[i] = []byte(prefix + strings.Repeat("x", length-len(prefix)))
	}
	return result
}

// Construction is separate from streaming so a warm stream result does not
// hide setup cost. There is no equivalent masker in baseline 89100eb.
func BenchmarkMatcherConstruction(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("patterns=%d", count), func(b *testing.B) {
			patterns := benchmarkPatterns(count, 32)
			b.ReportAllocs()
			for b.Loop() {
				matcher, err := New(patterns)
				if err != nil {
					b.Fatal(err)
				}
				matcher.Destroy()
			}
		})
	}
}

// The unprotected control is an overhead reference, not an equally secure
// baseline. Timed streams include Writer allocation, actual writes, EOF and
// destruction, but exclude immutable matcher and synthetic input construction.
func BenchmarkProtectedStream(b *testing.B) {
	for _, fixture := range []struct {
		name          string
		count, length int
	}{
		{"unprotected-control", 0, 32}, {"patterns=10", 10, 32},
		{"patterns=100", 100, 32}, {"patterns=1000", 1000, 32},
		{"max-pattern", 1, MaxPatternBytes},
	} {
		for _, chunk := range []int{1, 4096, 32768} {
			b.Run(fmt.Sprintf("%s/chunk=%d", fixture.name, chunk), func(b *testing.B) {
				patterns := benchmarkPatterns(fixture.count, fixture.length)
				matcher, err := New(patterns)
				if err != nil {
					b.Fatal(err)
				}
				defer matcher.Destroy()
				line := bytes.Repeat([]byte{'q'}, max(4096, fixture.length+1))
				wantLine := len(line)
				if len(patterns) > 0 {
					copy(line, patterns[0])
					wantLine += len(Replacement) - len(patterns[0])
				}
				repeats := max(1, (1<<20)/len(line))
				data := bytes.Repeat(line, repeats)
				wantSize := wantLine * repeats
				// Validate the independent fixture expectation before timing allocations.
				var check bytes.Buffer
				initial := NewWriter(&check, matcher)
				if _, err := initial.Write(data); err != nil {
					b.Fatal(err)
				}
				if err := initial.Close(); err != nil {
					b.Fatal(err)
				}
				if check.Len() != wantSize || (len(patterns) > 0 && bytes.Contains(check.Bytes(), patterns[0])) {
					b.Fatal("unexpected protected fixture result")
				}
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					writer := NewWriter(io.Discard, matcher)
					for offset := 0; offset < len(data); offset += chunk {
						end := min(len(data), offset+chunk)
						if n, err := writer.Write(data[offset:end]); err != nil || n != end-offset {
							b.Fatal("stream write failed", err)
						}
						if writer.position-writer.consumed >= uint64(max(1, matcher.longest)) {
							b.Fatal("unbounded withheld bytes")
						}
					}
					if err := writer.Close(); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(max(1, matcher.longest)-1), "withheld-max-B")
				// Per-writer backing arrays; excludes automaton, runtime and sink memory.
				b.ReportMetric(float64(5*max(1, matcher.longest)+outputChunk), "buffer-cap-B")
			})
		}
	}
}
