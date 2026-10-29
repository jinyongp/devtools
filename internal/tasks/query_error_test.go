package tasks

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageerr"
)

func TestTaskQueryPreparationError(t *testing.T) {
	for _, state := range []string{"initialization_required", "recovery_required"} {
		cause := storageerr.Wrap("open_file", "private_file", "access_denied", &os.PathError{Op: "open", Path: "/private-synthetic-path", Err: os.ErrPermission})
		e := (Store{Profile: "app"}).queryGateError(context.Background(), &maintenance.QueryPreparationError{State: state, Cause: cause})
		public := protocol.Public(e, "task current")
		facts := public.Details["facts"].(map[string]any)
		if e.Code != "storage_error" || e.ExitCode != 1 || facts["state"] != state || facts["stage"] != "open_file" || public.Details["reason"] != "access_denied" {
			t.Fatal("lost storage contract", public)
		}
		if public.Details["effects"] != "unknown" || public.Details["retry"] != "after_dependency_change" {
			t.Fatal("failed fallback claimed no effects")
		}
		r := public.Details["remedies"].([]protocol.Remedy)
		if len(r) != 1 || r[0].Effect != "value_write" || !reflect.DeepEqual(r[0].Argv, []string{"devtools", "task", "next", "--profile", "app"}) {
			t.Fatal("preparation requires non-paged task remedy", r)
		}
		body, _ := json.Marshal(public)
		if strings.Contains(string(body), "private-synthetic-path") {
			t.Fatal("private path leaked")
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		e := (Store{}).queryGateError(context.Background(), &maintenance.QueryPreparationError{State: "recovery_required", Cause: cause})
		if e.Code != protocol.ContextError(cause).Code {
			t.Fatal(e)
		}
		wrapped := storageerr.Wrap("write_file", "private_file", "", cause)
		cacheError := (Store{}).queryCacheError(Query{Command: "tree"}, "write_query_cache", wrapped)
		if cacheError.Code != protocol.ContextError(cause).Code || cacheError.ExitCode != protocol.ContextError(cause).ExitCode {
			t.Fatal("cache publication hid cancellation", cacheError)
		}
	}
}
