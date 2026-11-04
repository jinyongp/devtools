package cli

import (
	"context"
	"strings"
	"testing"
)

func TestCompletionUsesPartialInputContracts(t *testing.T) {
	a := testApp(t)
	for _, test := range []struct {
		words []string
		want  string
		omit  string
	}{
		{[]string{"run", "--proxy=false", "--"}, "--profile", "--no-proxy"},
		{[]string{"run", "--no-proxy=false", "--"}, "--profile", "--proxy"},
		{[]string{"var", "set", "KEY", "--profile", "app", "--"}, "--value", "--profile"},
		{[]string{"var", "set", "KEY", "--stdin", "--"}, "--profile", "--value"},
		{[]string{"var", "set", "KEY", "--stdin", "--f"}, "", "--file"},
		{[]string{"var", "set", "KEY", "--stdin=false", "--"}, "--value", "--stdin"},
		{[]string{"var", "set", "KEY", "--file", "input.json", "--"}, "--profile", "--stdin"},
		{[]string{"config", "fmt", "--check", "--"}, "--dir", "--dry-run"},
		{[]string{"doctor", "--storage", "--"}, "--fix", "--profile"},
		{[]string{"doctor", "--storage", "--fix", "--"}, "--help", "--check"},
		{[]string{"proxy", "start", "--all", "--"}, "--help", "--listener"},
		{[]string{"process", "list", "--all-profiles", "--"}, "--mode", "--profile"},
		{[]string{"proxy", "list", "--global", "--"}, "--help", "--profile"},
		{[]string{"--format", "json", "process", "list", "--"}, "--mode", "--columns"},
		{[]string{"--format", "text", "process", "list", "--"}, "--columns", ""},
		{[]string{"process", "list", "--state", "running", "--state", ""}, "failed", "running"},
		{[]string{"process", "list", "--state=running", "--state="}, "--state=failed", "--state=running"},
		// Required flags may still be supplied later; completion must not enforce
		// a fully complete invocation while selecting the next token.
		{[]string{"bundle", "remove", "dev", "--"}, "--revision", ""},
		{[]string{"vault", "init", "--passphrase-stdin", "--backend", ""}, "passphrase", "native"},
		{[]string{"doctor", "--mode", "invalid", "--"}, "", "--profile"},
		{[]string{"help", "process", "s"}, "stop", ""},
		{[]string{"schema", "task", "workstream", ""}, "create", ""},
	} {
		items := a.completionCandidates(context.Background(), test.words, true)
		found := map[string]bool{}
		for _, item := range items {
			found[item.Value] = true
		}
		if test.want != "" && !found[test.want] || test.omit != "" && found[test.omit] {
			t.Errorf("%q: got %v; want %q, omit %q", test.words, items, test.want, test.omit)
		}
	}
}

func TestDescribedCompletionProtocolKeepsPlainProtocol(t *testing.T) {
	a := testApp(t)
	input := "doctor\x00--mode\x00\x00"
	for _, described := range []bool{false, true} {
		args := []string{"__complete"}
		if described {
			args = append(args, "--describe")
		}
		code, out, diagnostic := invoke(t, a, input, args...)
		if code != 0 || diagnostic != "" || !strings.Contains(out, "foreground") || strings.Contains(out, "\t") != described {
			t.Fatal(code, out, diagnostic)
		}
	}
}

func TestCompletionRejectsDuplicateAndUnavailableInlineOptions(t *testing.T) {
	a := testApp(t)
	for _, words := range [][]string{
		{"var", "set", "KEY", "--profile", "app", "--profile=a"},
		{"--format", "json", "--format=t"},
		{"--format", "json", "process", "list", "--columns", ""},
		{"--format", "json", "process", "list", "--columns=c"},
	} {
		if got := a.completionCandidates(context.Background(), words, true); len(got) != 0 {
			t.Fatal(words, got)
		}
	}
}
