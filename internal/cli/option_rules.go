package cli

import (
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

// These predicates describe only static CLI inputs. They neither interpret a
// JSON Schema nor read files, profiles or authentication state.
type optionTest struct {
	name         string
	mode         string
	values       []string
	defaultValue string
	negated      bool
}

func supplied(name string) optionTest  { return optionTest{name: name, mode: "present"} }
func activated(name string) optionTest { return optionTest{name: name, mode: "active"} }
func valueIs(name, fallback string, values ...string) optionTest {
	return optionTest{name: name, mode: "value", defaultValue: fallback, values: values}
}
func without(test optionTest) optionTest { test.negated = !test.negated; return test }
func positional(name string) optionTest  { return optionTest{name: name, mode: "args"} }

func (t optionTest) matches(r Request) bool {
	value := r.Options[t.name]
	matched := false
	switch t.mode {
	case "present":
		_, matched = r.Options[t.name]
		if r.OptionOffsets != nil {
			_, matched = r.OptionOffsets[t.name]
		}
	case "active":
		matched = value == "true"
	case "value":
		if _, ok := r.Options[t.name]; !ok {
			value = t.defaultValue
		}
		for _, v := range t.values {
			if value == v {
				matched = true
			}
		}
	case "args":
		matched = len(r.Args) > 0
		if t.name == "child_args" {
			matched = len(r.Child) > 0
		}
	}
	if t.negated {
		return !matched
	}
	return matched
}

func (t optionTest) schema() map[string]any {
	var s map[string]any
	switch t.mode {
	case "present":
		s = presentInput(t.name)
	case "active":
		s = trueFlag(t.name)
	case "args":
		s = map[string]any{"required": []string{t.name}, "properties": map[string]any{t.name: map[string]any{"minItems": 1}}}
	case "value":
		property := map[string]any{"enum": t.values}
		s = map[string]any{"properties": map[string]any{t.name: property}}
		defaultMatches := false
		for _, v := range t.values {
			if v == t.defaultValue {
				defaultMatches = true
			}
		}
		if !defaultMatches {
			s["required"] = []string{t.name}
		}
	}
	if t.negated {
		return map[string]any{"not": s}
	}
	return s
}

type staticOptionRule struct {
	id      string
	kind    string
	message string
	tests   []optionTest
	code    string
}

func (rule staticOptionRule) schema() map[string]any {
	items := []map[string]any{}
	for _, test := range rule.tests {
		items = append(items, test.schema())
	}
	switch rule.kind {
	case "forbid":
		return map[string]any{"not": map[string]any{"allOf": items}}
	case "required":
		return map[string]any{"allOf": items}
	case "together":
		return map[string]any{"allOf": []any{map[string]any{"if": items[0], "then": items[1]}, map[string]any{"if": items[1], "then": items[0]}}}
	case "one":
		return map[string]any{"oneOf": items}
	case "when":
		return map[string]any{"if": items[0], "then": items[1]}
	case "when_any":
		return map[string]any{"if": items[0], "then": map[string]any{"anyOf": items[1:]}}
	}
	panic("unknown static option rule")
}

func (rule staticOptionRule) failure(r Request) *protocol.Error {
	matches := []bool{}
	count := 0
	for _, test := range rule.tests {
		m := test.matches(r)
		matches = append(matches, m)
		if m {
			count++
		}
	}
	valid := true
	switch rule.kind {
	case "forbid":
		valid = count != len(matches)
	case "required":
		valid = count == len(matches)
	case "together":
		valid = matches[0] == matches[1]
	case "one":
		valid = count == 1
	case "when":
		valid = !matches[0] || matches[1]
	case "when_any":
		valid = !matches[0] || count > 1
	}
	if valid {
		return nil
	}
	e := argumentError(rule.message, "")
	if rule.code != "" {
		e = portFailure(rule.code)
		e.Message = rule.message
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	options := []string{}
	seen := map[string]bool{}
	for _, test := range rule.tests {
		if test.mode != "args" && !seen[test.name] {
			options = append(options, test.name)
			seen[test.name] = true
		}
	}
	e.Details["facts"] = map[string]any{"rule_id": rule.id, "options": options}
	return e
}

func optionRules(c *Command) []staticOptionRule {
	rules := []staticOptionRule{}
	add := func(kind, id, message string, tests ...optionTest) {
		rules = append(rules, staticOptionRule{id: strings.ReplaceAll(c.Name, " ", ".") + "." + id, kind: kind, message: message, tests: tests})
	}
	switch c.Name {
	case "command run", "process start", "project up":
		add("forbid", "proxy-selection", "Specify only one of --proxy and --no-proxy, including explicit false values.", supplied("proxy"), supplied("no-proxy"))
	case "vault init", "vault unlock":
		add("forbid", "passphrase-source", "Choose --passphrase-file or --passphrase-stdin; they cannot be used together.", supplied("passphrase-file"), activated("passphrase-stdin"))
		if c.Name == "vault init" {
			for _, source := range []optionTest{supplied("passphrase-file"), activated("passphrase-stdin")} {
				add("forbid", "native-"+source.name, "Passphrase input requires --backend=passphrase.", without(valueIs("backend", "native", "passphrase")), source)
			}
		}
	case "vault rewrap":
		add("forbid", "stdin-consumer", "--current-passphrase-stdin and --new-passphrase-stdin cannot both be true.", activated("current-passphrase-stdin"), activated("new-passphrase-stdin"))
		for _, side := range [][2]string{{"current", "from-backend"}, {"new", "backend"}} {
			add("forbid", side[0]+"-source", "Choose --"+side[0]+"-passphrase-file or --"+side[0]+"-passphrase-stdin.", supplied(side[0]+"-passphrase-file"), activated(side[0]+"-passphrase-stdin"))
			for _, source := range []optionTest{supplied(side[0] + "-passphrase-file"), activated(side[0] + "-passphrase-stdin")} {
				add("forbid", source.name+"-backend", "--"+side[1]+"=native does not accept "+side[0]+" passphrase input.", valueIs(side[1], "", "native"), source)
			}
		}
	case "backup restore", "proxy teardown":
		add("together", "apply-request", "--apply and --request-id must be provided together.", supplied("apply"), supplied("request-id"))
	case "proxy configure":
		add("forbid", "https-selection", "Choose --https-address or --disable-https; they cannot be used together.", supplied("https-address"), activated("disable-https"))
	case "proxy setup":
		add("forbid", "check-remove", "--check and --remove cannot both be true.", activated("check"), activated("remove"))
		add("forbid", "check-request", "--check does not accept --request-id.", activated("check"), supplied("request-id"))
		add("when", "mutation-request", "Setup changes require --request-id; use --check for inspection.", without(activated("check")), supplied("request-id"))
	case "proxy list":
		add("forbid", "scope", "Choose --global or --profile.", activated("global"), supplied("profile"))
	case "proxy start", "proxy stop", "proxy status", "proxy restart", "proxy logs":
		add("forbid", "listener-selection", "--all cannot be combined with --listener.", activated("all"), supplied("listener"))
		if c.Name == "proxy start" {
			add("forbid", "all-port", "--all cannot be combined with --port.", activated("all"), supplied("port"))
		}
	case "proxy add", "proxy rm", "proxy clear":
		add("required", "global-scope", "This route mutation requires --global.", activated("global"))
		if c.Name == "proxy clear" {
			add("required", "clear-all", "Clear requires --global --all.", activated("all"))
		} else {
			add("forbid", "named-route", "Use a route name; --all is supported by proxy clear.", activated("all"))
		}
	case "proxy up", "proxy down":
		add("one", "route-selection", "Choose a route name or --all, exclusively.", positional("args"), activated("all"))
	case "proxy run":
		add("required", "global-scope", "Proxy run requires --global.", activated("global"))
		add("required", "child", "Provide a program after --.", positional("child_args"))
	case "doctor":
		add("forbid", "project-selection", "--project-only cannot be combined with command names.", activated("project-only"), positional("args"))
	case "expose start":
		auth := []optionTest{supplied("auth-file"), activated("auth-stdin"), activated("no-auth")}
		for i := range auth {
			for j := i + 1; j < len(auth); j++ {
				add("forbid", auth[i].name+"-"+auth[j].name, "Select one authentication input or --no-auth.", auth[i], auth[j])
			}
		}
		local := valueIs("provider", "local", "local")
		external := valueIs("provider", "local", "lan", "cloudflared", "tailscale")
		for _, test := range append(append([]optionTest{}, auth...), supplied("alias"), supplied("serve-port")) {
			add("forbid", "local-"+test.name, "Authentication and provider options require an external provider.", local, test)
		}
		add("when", "external-request", "External sharing requires --request-id UUID.", external, supplied("request-id"))
		add("when_any", "external-auth", "External sharing requires --auth-file, --auth-stdin, or --no-auth.", append([]optionTest{external}, auth...)...)
		rules[len(rules)-1].code = "authentication_required"
		add("when", "alias-provider", "--alias requires --provider=lan.", supplied("alias"), valueIs("provider", "local", "lan"))
		add("when", "serve-provider", "--serve-port requires --provider=tailscale.", supplied("serve-port"), valueIs("provider", "local", "tailscale"))
	}
	if strings.HasPrefix(c.Name, "port ") || c.Name == "instance list" || c.Name == "instance show" {
		add("forbid", "location", "Choose --instance or --dir.", supplied("instance"), supplied("dir"))
	}
	if strings.HasPrefix(c.Name, "task ") && c.BodySchema != nil {
		add("forbid", "body-source", "Choose JSON file or stdin.", supplied("file"), activated("stdin"))
		fields := c.BodySchema["properties"].(map[string]any)
		for _, option := range c.Options {
			for field := range fields {
				if taskField(field) == option.Name {
					add("forbid", "file-"+option.Name, "Do not combine --file with --"+option.Name+".", supplied("file"), supplied(option.Name))
					add("forbid", "stdin-"+option.Name, "Do not combine --stdin with --"+option.Name+".", activated("stdin"), supplied(option.Name))
				}
			}
		}
		if c.Name == "task claim" {
			add("forbid", "claim-selection", "Choose a task ID or --workstream.", positional("args"), supplied("workstream"))
		}
	}
	return rules
}
