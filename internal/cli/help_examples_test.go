package cli

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Bind documented template inputs for parser/schema checks without invoking
// handlers or mistaking example identities for existing resources.
func reviewedExampleTokens(t *testing.T, line string) []string {
	t.Helper()
	bindings := map[string]string{"REVISION": strings.Repeat("a", 64), "DIGEST": strings.Repeat("b", 64), "PROFILE_REVISION": "17", "INPUT_EXECUTION_ID": strings.Repeat("c", 32), "CURSOR": "inspected-cursor", "VERSION": "0.38.0"}
	for i, key := range []string{"REQUEST_UUID", "PREVIOUS_REQUEST_UUID", "EXECUTION_ID", "WS_ID", "TASK_ID", "RUN_ID", "VALIDATION_ID", "BASIS_ID", "RECORD_ID", "RUNNER_ID", "RUNNER_INSTANCE_ID", "EXPOSURE_ID", "PLAN_ID", "CANDIDATE_ID", "ARCHIVE_ID", "PREREQ_TASK_ID", "PREREQ_WS_ID"} {
		bindings[key] = fmt.Sprintf("%08x-0000-4000-8000-000000000001", i+1)
	}
	tokens := regexp.MustCompile("'[^']*'|\"[^\"]*\"|[^[:space:]]+").FindAllString(line, -1)
	for i, token := range tokens {
		if len(token) >= 2 && (token[0] == '\'' && token[len(token)-1] == '\'' || token[0] == '"' && token[len(token)-1] == '"') {
			token = token[1 : len(token)-1]
		}
		if value, ok := bindings[token]; ok {
			token = value
		}
		tokens[i] = token
	}
	if len(tokens) > 2 && tokens[1] == "--format" {
		if tokens[2] != "auto" && tokens[2] != "json" && tokens[2] != "text" {
			t.Fatal("invalid root format", line)
		}
		tokens = append(tokens[:1], tokens[3:]...)
	}
	return tokens
}

func TestReviewedExamplesCoverEveryCanonicalCommand(t *testing.T) {
	a := testApp(t)
	if len(reviewedExamples) != len(a.commands) {
		t.Fatal("command/example coverage diverged")
	}
	count := 0
	for _, c := range a.commands {
		examples, ok := reviewedExamples[c.Name]
		if !ok || len(examples) == 0 {
			t.Fatal("missing reviewed examples", c.Name)
		}
		for i, e := range examples {
			if e.Purpose == "" || e.Command != c.Examples[i] {
				t.Fatal(c.Name, e)
			}
			count++
		}
	}
	if count != 337 {
		t.Fatal("reviewed example inventory changed without a contract update", count)
	}
}

func TestProxyListHelpExplainsScopesWithoutMutationNoise(t *testing.T) {
	a := testApp(t)
	_, help, found := a.discovery("proxy list", false)
	if !found {
		t.Fatal("missing help")
	}
	for _, want := range []string{"All profiles and global routes", "One profile across its worktrees", "Global routes only", "Replace my-app with your profile name.", "Schema: devtools schema proxy list"} {
		if !strings.Contains(help, want) {
			t.Fatal(want, help)
		}
	}
	for _, bad := range []string{"Workflow:", "UUID", "JSON input example:", "Root options"} {
		if strings.Contains(help, bad) {
			t.Fatal("irrelevant help", bad, help)
		}
	}
	if strings.Count(help, "--isolated-root PATH") != 1 || strings.Index(help, "Examples:") > strings.Index(help, "Common options:") {
		t.Fatal(help)
	}
}
