package cli

import (
	"fmt"
	"strings"
)

func reviewedExampleWorkflow(c Command) []string {
	joined := strings.Join(commandExamples(c), " ")
	var steps []string
	if strings.Contains(joined, "REVISION") {
		if strings.Contains(joined, "PROFILE_REVISION") {
			steps = append(steps, "Read PROFILE_REVISION from the selected task/workstream query; it is a numeric profile revision.")
		} else if strings.HasPrefix(c.Name, "config ") || strings.HasPrefix(c.Name, "command ") {
			steps = append(steps, "Read REVISION from devtools config inspect for the same project.")
		} else {
			steps = append(steps, "Read REVISION from devtools values revision for the same profile; bundle list also returns its edit revision.")
		}
	}
	if strings.Contains(joined, "DIGEST") {
		steps = append(steps, "Use DIGEST from a fresh preview of the same target and options; refresh it after state changes.")
	}
	var sources []string
	for _, input := range []struct{ token, source string }{
		{"INPUT_EXECUTION_ID", "values execution prepare/list"}, {"EXECUTION_ID", "process list/status"},
		{"WS_ID", "task workstream list"}, {"TASK_ID", "task list"}, {"RUN_ID", "task current or claim"},
		{"VALIDATION_ID", "task validation list"}, {"BASIS_ID", "task validation basis"}, {"RECORD_ID", "task validation record/show"},
		{"RUNNER_ID", "values runner show"}, {"RUNNER_INSTANCE_ID", "the actual runner instance"},
		{"EXPOSURE_ID", "expose list"}, {"PLAN_ID", "cleanup preview"}, {"CANDIDATE_ID", "cleanup preview"}, {"ARCHIVE_ID", "cleanup archives"},
		{"PREREQ_TASK_ID", "a different prerequisite task"}, {"PREREQ_WS_ID", "a different prerequisite workstream"},
	} {
		for _, token := range strings.Fields(joined) {
			if token == input.token {
				sources = append(sources, input.token+" from "+input.source)
				break
			}
		}
	}
	if len(sources) > 0 {
		steps = append(steps, "Use inspected identifiers: "+strings.Join(sources, "; ")+".")
	}
	if strings.Contains(joined, "CURSOR") {
		steps = append(steps, "Use CURSOR from the preceding page with the same scope, filters, page size and query cache directory.")
	}
	if strings.Contains(joined, "PREVIOUS_REQUEST_UUID") {
		steps = append(steps, "PREVIOUS_REQUEST_UUID is the original operation UUID; do not generate a replacement for recovery.")
	}
	if operationIDPolicyFor(c) != nil {
		steps = append(steps, operationIDWorkflow)
	}
	for _, option := range c.Options {
		if option.Name == "context" {
			steps = append(steps, "Use this run's private DEVTOOLS_TASK_CONTEXT. Do not copy it into shared examples or logs.")
			break
		}
	}
	if c.Name == "vault rewrap" {
		steps = append(steps, "Select original and target backends explicitly. Use hidden input or private credential files; only one side may read stdin. Native authentication requires --interactive.", "After interruption retain the UUID, backend selections and target credentials. Inspect vault status and follow pending-operation recovery. Rewrap preserves manual lock and does not revoke old copies or app credentials.")
	}
	if c.Name == "secret generate" {
		steps = append(steps, "Inspect secret list metadata before --replace. Secret plaintext is never returned.")
	}
	return steps
}

func writeExamples(b *strings.Builder, c Command) {
	if len(c.Examples) == 0 {
		return
	}
	b.WriteString("\nExamples:\n")
	reviewed := reviewedExamples[c.Name]
	width := 0
	for _, e := range reviewed {
		if len(e.Command)+len(e.Purpose)+5 <= 76 {
			width = max(width, len(e.Command))
		}
	}
	for i, command := range c.Examples {
		purpose := ""
		if i < len(reviewed) {
			purpose = reviewed[i].Purpose
		}
		if purpose == "" {
			fmt.Fprintf(b, "  %s\n", command)
		} else if len(command)+len(purpose)+5 <= 76 {
			fmt.Fprintf(b, "  %-*s  # %s\n", width, command, purpose)
		} else {
			fmt.Fprintf(b, "  # %s\n  %s\n", purpose, command)
		}
	}
	if strings.Contains(strings.Join(c.Examples, " "), "my-app") {
		b.WriteString("  Replace my-app with your profile name.\n")
	}
}

func humanRule(c Command, rule staticOptionRule) string {
	if c.Name == "proxy list" && strings.HasSuffix(rule.id, ".scope") {
		return ""
	}
	if rule.kind != "forbid" && rule.kind != "together" {
		return rule.message
	}
	var labels []string
	for _, test := range rule.tests {
		if test.mode != "present" && test.mode != "active" {
			return rule.message
		}
		labels = append(labels, "--"+test.name)
	}
	if rule.kind == "together" {
		return "Provide " + strings.Join(labels, " and ") + " together."
	}
	return "Do not combine " + strings.Join(labels, " and ") + "."
}

func styledHelp(s string, sgr bool) string {
	if !sgr {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Usage:") || line != "" && !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			lines[i] = "\x1b[1m" + line + "\x1b[0m"
		}
	}
	return strings.Join(lines, "\n")
}
