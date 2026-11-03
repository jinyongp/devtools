package cli

import (
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

// An operation ID is generated at execution, never while parsing or validating
// guidance. Selector options do not have this policy.
type operationIDPolicy struct {
	when *optionTest
}

const operationIDWorkflow = "Omit --request-id for a new operation; the CLI generates and returns it. For an uncertain response, reuse the original ID and identical input with --request-id. Save an explicit UUID before execution when recovery must survive losing the entire response."

func requestIDOption(pattern string, when ...optionTest) Option {
	if len(when) > 1 {
		panic("operation request ID accepts one trigger")
	}
	policy := &operationIDPolicy{}
	if len(when) == 1 {
		policy.when = &when[0]
	}
	return Option{Name: "request-id", Pattern: pattern, OperationID: policy, Description: "Optional operation UUID; generated when omitted. Reuse the returned ID and identical input after an uncertain response."}
}

func operationIDPolicyFor(c Command) *operationIDPolicy {
	for _, option := range c.Options {
		if option.Name == "request-id" {
			return option.OperationID
		}
	}
	return nil
}

// Triggered operations retain the reverse dependency: an explicit ID alone
// cannot supply the caller's revision or authorize application of a preview.
func operationIDRules(c Command) []staticOptionRule {
	policy := operationIDPolicyFor(c)
	if policy == nil || policy.when == nil || policy.when.mode != "present" || policy.when.negated {
		return nil
	}
	return []staticOptionRule{{
		id: strings.ReplaceAll(c.Name, " ", ".") + ".request-id-scope", kind: "when",
		message: "--request-id requires --" + policy.when.name + ".",
		tests:   []optionTest{supplied("request-id"), *policy.when},
	}}
}

func operationIDResultContract(c *Command) {
	if operationIDPolicyFor(*c) != nil {
		publicResultField(c.Output, "request_id", stringSchema(), false)
	}
}

func prepareOperationID(c Command, r *Request) string {
	policy := operationIDPolicyFor(c)
	if policy == nil || policy.when != nil && !policy.when.matches(*r) {
		return ""
	}
	id := r.Options["request-id"]
	if id == "" {
		id = protocol.NewRequestID()
		r.Options["request-id"] = id
	}
	return id
}
