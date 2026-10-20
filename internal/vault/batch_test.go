package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestBatchProcessesWholeProfileOnce(t *testing.T) {
	for _, count := range []int{100, 1000, 5000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			p, key := profileFixture(t)
			before, _ := p.Encode()
			stats := map[string]int{}
			changed, err := p.batch(context.Background(), key, rand.Reader, func(m *Mutation) error {
				for i := range count {
					if _, err := m.Set(fmt.Sprintf("NEW_%04d", i), Secret, "", "synthetic-batch-value"); err != nil {
						return err
					}
				}
				return nil
			}, func(phase string) { stats[phase]++ })
			if err != nil || !changed {
				t.Fatal(changed, err)
			}
			for _, phase := range []string{"verify", "clone", "sign", "encode"} {
				if stats[phase] != 1 {
					t.Fatalf("%s count=%d", phase, stats[phase])
				}
			}
			if stats["open"] != 0 {
				t.Fatal("new secrets opened existing slots")
			}
			original, _ := DecodeProfile(before)
			if !reflect.DeepEqual(original.Values, p.Values[:len(original.Values)]) {
				t.Fatal("untouched slots changed")
			}
			if err = p.Verify(key); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestBatchDecryptsOnlyComparedSlots(t *testing.T) {
	p, key := profileFixture(t)
	cases := []struct {
		name  string
		opens int
		apply func(*Mutation) error
	}{
		{"public", 0, func(m *Mutation) error { _, err := m.Set("URL", Public, "", "new-public"); return err }},
		{"bundle", 0, func(m *Mutation) error { _, err := m.SetBundle("api", []string{"TOKEN"}); return err }},
		{"env", 0, func(m *Mutation) error { _, err := m.CreateEnv("staging"); return err }},
		{"unset", 0, func(m *Mutation) error { _, err := m.Unset("TOKEN", Secret, "local"); return err }},
		{"single", 1, func(m *Mutation) error { _, err := m.Set("TOKEN", Secret, "", "next-secret"); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := p.clone()
			opens := 0
			if _, err := q.batch(context.Background(), key, rand.Reader, tc.apply, func(phase string) {
				if phase == "open" {
					opens++
				}
			}); err != nil {
				t.Fatal(err)
			}
			if opens != tc.opens {
				t.Fatalf("opened %d want %d", opens, tc.opens)
			}
		})
	}
}
func TestBatchFailureAndCancellationLeaveOriginal(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			p, key := profileFixture(t)
			before, _ := p.Encode()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			changed, err := p.Batch(ctx, key, func(m *Mutation) error {
				if _, err := m.Set("NEW", Secret, "", "synthetic-uncommitted"); err != nil {
					return err
				}
				if cancelled {
					cancel()
					_, err := m.Set("MORE", Secret, "", "must-not-run")
					return err
				}
				_, err := m.SetBundle("broken", []string{"MISSING"})
				return err
			})
			if changed || err == nil {
				t.Fatal(changed, err)
			}
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			after, _ := p.Encode()
			if !bytes.Equal(before, after) {
				t.Fatal("partial candidate committed")
			}
		})
	}
}
func TestBatchSameSecretPreservesAllBytes(t *testing.T) {
	p, key := profileFixture(t)
	before, _ := p.Encode()
	changed, err := p.Batch(context.Background(), key, func(m *Mutation) error {
		_, err := m.Set("TOKEN", Secret, "local", "synthetic-local-token")
		return err
	})
	after, _ := p.Encode()
	if err != nil || changed || !bytes.Equal(before, after) {
		t.Fatal("no-op rewrote secret", err)
	}
}
