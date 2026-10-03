package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkBoundedLog4KiBAtCapacity(b *testing.B) {
	const writes = 512
	const chunkSize = 4 << 10
	root := privateTempDir(b)
	initial := bytes.Repeat([]byte{'a'}, logLogicalCap)
	payload := bytes.Repeat([]byte{'b'}, chunkSize)
	var totalRewrite, totalRead, totalPhysicalReads, totalCompactions, totalFsync int64

	b.ReportAllocs()
	b.SetBytes(writes * chunkSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		path := filepath.Join(root, fmt.Sprintf("log-%d", i))
		stats := &logIOStats{}
		store := boundedLogStore{path: path, stats: stats}
		if _, err := store.write(initial); err != nil {
			b.Fatal(err)
		}
		stats.mu.Lock()
		stats.AppendBytes, stats.RewriteBytes, stats.ReadBytes, stats.PhysicalReads, stats.Fsyncs, stats.Compactions = 0, 0, 0, 0, 0, 0
		stats.mu.Unlock()
		b.StartTimer()

		for n := 0; n < writes; n++ {
			if _, err := store.write(payload); err != nil {
				b.Fatal(err)
			}
		}

		b.StopTimer()
		body, exists, err := (boundedLogStore{path: path}).snapshot(context.Background())
		if err != nil || !exists || len(body) != logLogicalCap {
			b.Fatalf("snapshot: len=%d exists=%v err=%v", len(body), exists, err)
		}
		stats.mu.Lock()
		totalRewrite += stats.RewriteBytes
		totalRead += stats.ReadBytes
		totalPhysicalReads += stats.PhysicalReads
		totalCompactions += stats.Compactions
		totalFsync += stats.Fsyncs
		stats.mu.Unlock()
		_ = os.Remove(path)
		_ = os.Remove(path + ".lock")
		b.StartTimer()
	}
	logical := float64(b.N * writes * chunkSize)
	if logical > 0 {
		b.ReportMetric(float64(totalRewrite)/logical, "rewrite/input")
		b.ReportMetric(float64(totalRead)/logical, "read/input")
	}
	if b.N > 0 {
		b.ReportMetric(float64(totalPhysicalReads)/float64(b.N), "physical-reads/op")
		b.ReportMetric(float64(totalCompactions)/float64(b.N), "compactions/op")
		b.ReportMetric(float64(totalFsync)/float64(b.N), "fsyncs/op")
	}
}

func BenchmarkBoundedLog1KiBHighFrequency(b *testing.B) {
	const writes = 1024
	payload := bytes.Repeat([]byte{'x'}, 1<<10)
	root := privateTempDir(b)
	b.ReportAllocs()
	b.SetBytes(int64(writes * len(payload)))
	for i := 0; i < b.N; i++ {
		path := filepath.Join(root, fmt.Sprintf("hf-%d", i))
		store := boundedLogStore{path: path}
		for n := 0; n < writes; n++ {
			if _, err := store.write(payload); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkBoundedLogLargeWrite(b *testing.B) {
	payload := bytes.Repeat([]byte{'z'}, 2<<20)
	root := privateTempDir(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		path := filepath.Join(root, fmt.Sprintf("large-%d", i))
		store := boundedLogStore{path: path}
		if _, err := store.write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
