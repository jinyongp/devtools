package tasks

import (
	"context"
	"fmt"
	"testing"
)

func TestGraphDepthZeroContinuationStopsAtRequestedNode(t *testing.T) {
	for _, kind := range []string{"task", "workstream"} {
		t.Run(kind, func(t *testing.T) {
			s := fixture(t)
			workstream := itemID(call(t, s, "workstream.create", "", Object{"title": "container"}))
			add := "task.add"
			command := "tree"
			depends := "task.depends"
			if kind == "workstream" {
				add = "workstream.create"
				command = "workstream tree"
				depends = "workstream.depends"
			}
			ids := []string{}
			for _, title := range []string{"deep", "prerequisite", "dependent"} {
				body := Object{"title": title}
				if kind == "task" {
					body["workstream_id"] = workstream
				}
				ids = append(ids, itemID(call(t, s, add, "", body)))
			}
			call(t, s, depends, ids[1], Object{"depends_on": []string{ids[0]}})
			call(t, s, depends, ids[2], Object{"depends_on": []string{ids[1]}})
			first, e := s.Query(context.Background(), Query{Command: command, Target: ids[2], Options: map[string]string{"depth": "1", "limit": "1"}})
			if e != nil {
				t.Fatal(e)
			}
			cont := first["continuations"].([]Object)
			if len(cont) != 1 || str(cont[0], "id") != ids[1] || num(cont[0], "depth") != 0 {
				t.Fatal(first)
			}
			for _, cursor := range []string{str(first, "cursor"), ""} {
				next, e := s.Query(context.Background(), Query{Command: command, Target: str(cont[0], "id"), Options: map[string]string{"depth": fmt.Sprint(num(cont[0], "depth")), "direction": str(cont[0], "direction"), "cursor": cursor, "limit": "1"}})
				if e != nil {
					t.Fatal(e)
				}
				nodes := next["nodes"].([]Object)
				if len(nodes) != 1 || str(nodes[0], "id") != ids[1] || len(next["edges"].([]Object)) != 0 || len(next["continuations"].([]Object)) != 0 || next["truncated"] != false || next["cursor"] != nil {
					t.Fatal("depth-zero continuation expanded or repeated", next)
				}
				if cursor != "" && num(next, "revision") != num(first, "revision") {
					t.Fatal("snapshot revision changed")
				}
			}
		})
	}
}
