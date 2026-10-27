package configcontract

import (
	"reflect"
	"slices"
	"testing"
)

func TestEditDiagnosticsSelectKnownOperationBranch(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		path   []string
		reason string
	}{
		{`{"operations":[{"op":"command.add","name":"web","value":{"exec":[42]}}]}`, []string{"operations", "0", "value", "exec", "0"}, "invalid_type"},
		{`{"operations":[{"op":"command.update","name":"web","value":{"inject":42}}]}`, []string{"operations", "0", "value", "inject"}, "invalid_type"},
		{`{"operations":[{"op":"command.rename","name":"web","new_name":42}]}`, []string{"operations", "0", "new_name"}, "invalid_type"},
		{`{"operations":[{"op":"unknown.operation"}]}`, []string{"operations", "0", "op"}, "invalid_value"},
		{`{"operations":[{"name":"web"}]}`, []string{"operations", "0", "op"}, "missing_argument"},
		{`{"operations":[{"op":42}]}`, []string{"operations", "0", "op"}, "invalid_type"},
	} {
		value, issue := Decode([]byte(tc.raw))
		if issue != nil {
			t.Fatal(issue)
		}
		issues := Validate("Edit", value)
		if len(issues) == 0 || !reflect.DeepEqual(issues[0].Path, tc.path) || issues[0].Reason != tc.reason {
			t.Fatal(tc.raw, issues)
		}
	}
}

func TestOperationDiagnosticChoiceDoesNotDependOnSchemaOrder(t *testing.T) {
	compiled.Do(compile)
	schema := *compiled.definitions["Operation"]
	schema.OneOf = slices.Clone(schema.OneOf)
	slices.Reverse(schema.OneOf)
	bad, _ := Decode([]byte(`{"op":"command.add","name":"web","value":{"exec":[42]}}`))
	good, _ := Decode([]byte(`{"op":"command.add","name":"web","value":{"exec":["true"]}}`))
	branch := operationFor(&schema, bad)
	if branch == nil || branch.Validate(bad) == nil || branch.Validate(good) != nil {
		t.Fatal("wrong branch after reordering")
	}
	if operationFor(&schema, map[string]any{"op": "unknown.operation"}) != nil {
		t.Fatal("invented an unknown branch")
	}
}
