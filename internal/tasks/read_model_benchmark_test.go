package tasks

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkTaskReadBatch(b *testing.B) {
	for _, size := range []struct{ nodes, runs int }{{200, 10000}, {1000, 50000}} {
		b.Run(fmt.Sprintf("nodes_%d_runs_%d", size.nodes, size.runs), func(b *testing.B) {
			s := NewState()
			ids := make([]string, size.nodes)
			for n := range ids {
				ids[n] = fmt.Sprint(n)
				item := &Item{ID: ids[n], Kind: "task", Order: n, Props: Object{}}
				if n > 0 {
					item.Depends = []string{ids[n-1]}
				}
				s.Items[item.ID] = item
			}
			for n := 0; n < size.runs; n++ {
				id := fmt.Sprint(n)
				s.Runs[id] = &Run{ID: id, TaskID: ids[n%len(ids)], State: "released"}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				s.readIndex = nil
				s.prepareReads()
				for _, id := range ids {
					if s.Current(id) != nil {
						b.Fatal("unexpected active run")
					}
				}
				if len(s.successors(ids[0])) != size.nodes-1 {
					b.Fatal("incomplete graph")
				}
			}
		})
	}
}

func BenchmarkTaskGraphCursorHistory(b *testing.B) {
	for _, history := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("events_%d", history), func(b *testing.B) {
			store, _, _, _ := benchmarkV3HistoryStore(b, history)
			if _, err := store.Execute(context.Background(), Request{Action: "task.add", Body: Object{"title": "second"}, Options: map[string]string{"request-id": ID()}}); err != nil {
				b.Fatal(err)
			}
			first, err := store.Query(context.Background(), Query{Command: "tree", Options: map[string]string{"limit": "1"}})
			if err != nil || str(first, "cursor") == "" {
				b.Fatal(first, err)
			}
			q := Query{Command: "tree", Options: map[string]string{"limit": "1", "cursor": str(first, "cursor")}}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				out, err := store.Query(context.Background(), q)
				if err != nil || len(out["nodes"].([]Object)) != 1 || num(out, "revision") != num(first, "revision") {
					b.Fatal(out, err)
				}
			}
		})
	}
}
