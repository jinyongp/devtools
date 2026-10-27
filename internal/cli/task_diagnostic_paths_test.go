package cli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestTaskEditDiagnosticsKeepLocationsAndOriginalScope(t *testing.T) {
	app := testApp(t)
	w := outputData(t, app, []string{"task", "workstream", "create", "--profile", "app", "--title", "Fixture", "--request-id", tasks.ID()})
	wid := w["item"].(map[string]any)["id"].(string)
	before := outputData(t, app, []string{"task", "workstream", "plan", "show", wid, "--profile", "app"})
	for index := 0; index < 3; index++ {
		for _, kind := range []string{"operation", "type", "reference"} {
			t.Run(fmt.Sprintf("%d/%s", index, kind), func(t *testing.T) {
				ops := []any{}
				for n := 0; n < 3; n++ {
					ops = append(ops, map[string]any{"op": "task.add", "value": map[string]any{"title": fmt.Sprint(n)}})
				}
				path := []any{"operations", fmt.Sprint(index)}
				switch kind {
				case "operation":
					ops[index] = map[string]any{"op": "unknown.operation"}
					path = append(path, "op")
				case "type":
					ops[index] = map[string]any{"op": "task.add", "value": map[string]any{"title": 42}}
					path = append(path, "value", "title")
				case "reference":
					ops[index] = map[string]any{"op": "task.update", "id": "00000000-0000-4000-8000-000000000001", "value": map[string]any{"title": "changed"}}
					path = append(path, "id")
				}
				body, _ := json.Marshal(map[string]any{"reason": "Fixture", "operations": ops})
				args := []string{"task", "workstream", "edit", wid, "--profile", "app", "--stdin", "--if-revision", fmt.Sprint(w["revision"]), "--request-id", tasks.ID()}
				code, _, diagnostic := invoke(t, app, string(body), args...)
				if code != 2 && !(kind == "reference" && code == 3) {
					t.Fatal(code, diagnostic)
				}
				var env map[string]any
				if err := json.Unmarshal([]byte(diagnostic), &env); err != nil {
					t.Fatal(err)
				}
				assertContract(t, "Failure", env)
				details := env["error"].(map[string]any)["details"].(map[string]any)
				if details["facts"].(map[string]any)["operation_index"] != float64(index) {
					t.Fatal(details)
				}
				if !reflect.DeepEqual(details["sources"].([]any)[0].(map[string]any)["path"], path) {
					t.Fatal(details)
				}
				_, _, text := invoke(t, app, string(body), append([]string{"--format", "text"}, args...)...)
				location := fmt.Sprintf("operations[%d].%s", index, strings.Join(anyStrings(path[2:]), "."))
				if !strings.Contains(text, location) {
					t.Fatal(text, location)
				}
				planRemedy := false
				for _, item := range details["remedies"].([]any) {
					r := item.(map[string]any)
					argv := anyStrings(r["argv"].([]any))
					if c, _, e := invoke(t, app, "", argv[1:]...); c != 0 {
						t.Fatal(argv, c, e)
					}
					if strings.Join(argv[1:5], " ") == "task workstream plan show" {
						planRemedy = true
					}
				}
				if kind == "reference" && !planRemedy {
					t.Fatal("missing original plan remedy", details)
				}
				if after := outputData(t, app, []string{"task", "workstream", "plan", "show", wid, "--profile", "app"}); !reflect.DeepEqual(before, after) {
					t.Fatal("failed edit changed plan", after)
				}
			})
		}
	}
}

func TestTaskCoverageFailurePreservesChecks(t *testing.T) {
	app := testApp(t)
	w := outputData(t, app, []string{"task", "workstream", "create", "--profile", "app", "--title", "Fixture", "--request-id", tasks.ID()})
	wid := w["item"].(map[string]any)["id"].(string)
	code, _, diagnostic := invoke(t, app, "", "task", "workstream", "activate", wid, "--profile", "app", "--if-revision", fmt.Sprint(w["revision"]), "--request-id", tasks.ID())
	var env map[string]any
	if code != 3 || json.Unmarshal([]byte(diagnostic), &env) != nil {
		t.Fatal(code, diagnostic)
	}
	assertContract(t, "Failure", env)
	details := env["error"].(map[string]any)["details"].(map[string]any)
	if len(details["checks"].([]any)) != 3 {
		t.Fatal("coverage reasons lost", details)
	}
}

func TestTaskForeignValidationOwnerKeepsScopeAndRetry(t *testing.T) {
	app := testApp(t)
	create := func(title string) string {
		w := outputData(t, app, []string{"task", "workstream", "create", "--profile", "app", "--title", title, "--request-id", tasks.ID()})
		return w["item"].(map[string]any)["id"].(string)
	}
	wid, other := create("Selected"), create("Other")
	before := outputData(t, app, []string{"task", "workstream", "plan", "show", wid, "--profile", "app"})
	body := func(owner string) string {
		return fmt.Sprintf(`{"reason":"PRIVATE-CONTEXT-CANARY","operations":[{"op":"validation.add","value":{"title":"Check","method":"Test","workstream_id":%q}}]}`, owner)
	}
	args := []string{"task", "workstream", "edit", wid, "--profile", "app", "--stdin", "--if-revision", fmt.Sprint(before["revision"]), "--request-id", tasks.ID()}
	code, _, diagnostic := invoke(t, app, body(other), args...)
	var env map[string]any
	if code != 2 || json.Unmarshal([]byte(diagnostic), &env) != nil || strings.Contains(diagnostic, "PRIVATE-") {
		t.Fatal(code, diagnostic)
	}
	assertContract(t, "Failure", env)
	details := env["error"].(map[string]any)["details"].(map[string]any)
	facts := details["facts"].(map[string]any)
	if facts["operation_index"] != float64(0) || facts["reference_id"] != other || !reflect.DeepEqual(details["sources"].([]any)[0].(map[string]any)["path"], []any{"operations", "0", "value", "workstream_id"}) {
		t.Fatal(details)
	}
	planRemedy := false
	for _, value := range details["remedies"].([]any) {
		argv := anyStrings(value.(map[string]any)["argv"].([]any))
		if c, _, err := invoke(t, app, "", argv[1:]...); c != 0 {
			t.Fatal(argv, c, err)
		}
		if strings.Contains(strings.Join(argv, " "), "task workstream plan show "+wid) {
			planRemedy = true
		}
	}
	if after := outputData(t, app, []string{"task", "workstream", "plan", "show", wid, "--profile", "app"}); !planRemedy || !reflect.DeepEqual(before, after) {
		t.Fatal("missing selected scope or failed edit changed plan", after)
	}
	// Failed requests do not consume a UUID; fixing the owner can use it, and
	// retrying that successful request must replay its original result.
	for n := 0; n < 2; n++ {
		c, out, err := invoke(t, app, body(wid), args...)
		if c != 0 || json.Unmarshal([]byte(out), &env) != nil || env["data"].(map[string]any)["replayed"] != (n == 1) {
			t.Fatal(c, out, err)
		}
	}
}

func TestTaskLeafDiagnosticsPreserveNestedTypesWithoutInputValues(t *testing.T) {
	app := testApp(t)
	for _, tc := range []struct{ body, path string }{
		{`{"title":42,"description":"PRIVATE-CREDENTIAL"}`, `["title"]`},
		{`{"title":"Fixture","acceptance":[42]}`, `["acceptance","0"]`},
	} {
		code, _, diagnostic := invoke(t, app, tc.body, "task", "add", "--profile", "app", "--stdin", "--request-id", tasks.ID())
		var env map[string]any
		if code != 2 || json.Unmarshal([]byte(diagnostic), &env) != nil || strings.Contains(diagnostic, "PRIVATE-") {
			t.Fatal(code, diagnostic)
		}
		assertContract(t, "Failure", env)
		details := env["error"].(map[string]any)["details"].(map[string]any)
		path, _ := json.Marshal(details["sources"].([]any)[0].(map[string]any)["path"])
		facts := details["facts"].(map[string]any)
		if string(path) != tc.path || facts["expected_type"] != "string" || facts["observed_type"] != "number" {
			t.Fatal(details)
		}
	}
	request := tasks.ID()
	args := []string{"task", "add", "--profile", "app", "--stdin", "--request-id", request}
	if c, _, e := invoke(t, app, `{"title":42}`, args...); c != 2 {
		t.Fatal(c, e)
	}
	if c, _, e := invoke(t, app, `{"title":"Corrected"}`, args...); c != 0 {
		t.Fatal("failed validation consumed request UUID", c, e)
	}
}
