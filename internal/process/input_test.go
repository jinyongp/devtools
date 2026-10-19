package process

import (
	"fmt"
	"strings"
	"testing"
)

func TestParentPolicySeparatesInheritanceAndManagementCredentials(t *testing.T) {
	parent := []string{"BASE=keep", "PARENT_TOKEN=synthetic-parent-token", "DEVTOOLS_TASK_CONTEXT=synthetic-control-credential"}
	for _, tc := range []struct {
		policy ParentPolicy
		count  int
		token  bool
	}{{ParentPolicy{}, 2, true}, {ParentPolicy{Mode: ParentClean}, 0, false}, {ParentPolicy{Mode: ParentAllowlist, Allow: []string{"BASE"}}, 1, false}} {
		selected, e := SelectParent(parent, tc.policy)
		if e != nil {
			t.Fatal(e)
		}
		if len(selected) != tc.count {
			t.Fatal("wrong parent selection")
		}
		joined := strings.Join(selected, "\n")
		if strings.Contains(joined, "DEVTOOLS_TASK_CONTEXT") || strings.Contains(joined, "PARENT_TOKEN") != tc.token {
			t.Fatal("credential selection violated policy")
		}
	}
	if _, e := SelectParent(parent, ParentPolicy{Mode: "none"}); e == nil {
		t.Fatal("stored-value selection accepted as parent policy")
	}
}
func TestExecutionInputErrorsNeverEchoValues(t *testing.T) {
	sentinel := "synthetic-sensitive-marker"
	for _, environment := range [][]string{{"TOKEN=" + sentinel + "\x00"}, {"TOKEN=" + sentinel + strings.Repeat("x", MaxExecutionInputBytes)}} {
		e := ValidateExecInput([]string{"program"}, environment)
		if e == nil || strings.Contains(fmt.Sprint(e), sentinel) {
			t.Fatal("unsafe input error")
		}
	}
}
