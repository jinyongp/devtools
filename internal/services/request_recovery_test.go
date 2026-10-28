package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestCompletedProcessRecoveryProvesIntentWithoutConfig(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy=", legacy), func(t *testing.T) {
			s := Store{Data: privateTempDir(t)}
			root := privateTempDir(t)
			caller := root
			if !legacy {
				caller = filepath.Join(root, "subdir")
				if e := os.Mkdir(caller, 0700); e != nil {
					t.Fatal(e)
				}
			}
			q := Request{Action: "start", Directory: root, Command: "web", RequestID: tasks.ID()}
			if !legacy {
				input := struct {
					Profile, Root string
					Request       Request
				}{"app", root, q}
				_, e := (netops.Store{Data: s.Data}).ChooseExecution(context.Background(), q.RequestID, input, network.PrepareAuto, func() (netops.ExecutionPlan, *protocol.Error) { return netops.ExecutionPlan{}, nil }, caller)
				if e != nil {
					t.Fatal(e)
				}
			}
			now := time.Now().UTC()
			r := Record{ID: tasks.ID(), Profile: "app", Directory: root, Command: "web", CreatedAt: now, EndedAt: &now, State: "failed", Reason: "execution_failed"}
			body, _ := json.Marshal(q)
			path := filepath.Join(s.root(), "receipts", q.RequestID+".json")
			if e := writePrivate(path, receipt{Fingerprint: fmt.Sprintf("%x", sha256.Sum256(body)), Done: true, Result: Result{Item: r, Changed: true}}); e != nil {
				t.Fatal(e)
			}
			before, _ := os.ReadFile(path)
			out, found, e := s.RecoverCompleted(context.Background(), q, caller, network.PrepareAuto)
			if !found || e == nil || e.Code != "execution_failed" || !out.Replayed || out.Item.ID != r.ID {
				t.Fatal(out, found, e)
			}
			wrong := q
			wrong.Command = "other"
			if _, found, e := s.RecoverCompleted(context.Background(), wrong, caller, network.PrepareAuto); !found || e == nil || e.Code != "request_conflict" {
				t.Fatal(found, e)
			}
			if _, found, e := s.RecoverCompleted(context.Background(), q, caller, network.PrepareOff); !found || e == nil {
				t.Fatal("changed preparation mode accepted", found, e)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("replay changed immutable receipt")
			}
		})
	}
}

func TestReplayOnlyCannotAdmitMissingReceipt(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	q := Request{Action: "start", Directory: privateTempDir(t), Command: "web", RequestID: tasks.ID(), ReplayOnly: true}
	if _, e := s.Apply(context.Background(), q); e == nil || e.Code != "process_pending" {
		t.Fatal(e)
	}
	if _, e := os.Stat(s.path(q.RequestID, "record.json")); !os.IsNotExist(e) {
		t.Fatal("replay admitted new execution", e)
	}
	if _, e := os.Stat(filepath.Join(s.root(), "receipts", q.RequestID+".json")); !os.IsNotExist(e) {
		t.Fatal("replay created receipt", e)
	}
}

func TestLegacyRecoveryWithoutScopeUsesOrdinaryRetry(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	q := Request{Action: "start", Directory: privateTempDir(t), Command: "web", RequestID: tasks.ID()}
	body, _ := json.Marshal(q)
	if e := writePrivate(filepath.Join(s.root(), "receipts", q.RequestID+".json"), receipt{Fingerprint: fmt.Sprintf("%x", sha256.Sum256(body)), Done: true, Result: Result{Item: Record{ID: q.RequestID}}}); e != nil {
		t.Fatal(e)
	}
	if _, found, e := s.RecoverCompleted(context.Background(), q, q.Directory, network.PrepareAuto); found || e != nil {
		t.Fatal("guessed legacy scope", found, e)
	}
	if out, e := s.Apply(context.Background(), q); e != nil || !out.Replayed {
		t.Fatal("ordinary legacy replay changed", out, e)
	}
}
