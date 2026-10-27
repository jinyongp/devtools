package cli

import (
	"context"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

const portPattern = `^([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])$`

func presentInput(name string) map[string]any { return map[string]any{"required": []string{name}} }
func noTogether(inputs ...map[string]any) map[string]any {
	return map[string]any{"not": map[string]any{"allOf": inputs}}
}
func togetherInputs(a, b string) []map[string]any {
	return []map[string]any{{"if": presentInput(a), "then": presentInput(b)}, {"if": presentInput(b), "then": presentInput(a)}}
}

// Static contracts describe combinations independently of filesystem, protection
// and saved state. Runtime preflight below runs before those dependencies.
func staticContract(c *Command) {
	add := func(rules ...map[string]any) { c.InputAllOf = append(c.InputAllOf, rules...) }
	switch c.Name {
	case "vault init", "vault unlock":
		add(noTogether(presentInput("passphrase-file"), trueFlag("passphrase-stdin")))
		if c.Name == "vault init" {
			add(map[string]any{"if": map[string]any{"not": map[string]any{"required": []string{"backend"}, "properties": map[string]any{"backend": map[string]any{"const": "passphrase"}}}}, "then": map[string]any{"not": map[string]any{"anyOf": []any{presentInput("passphrase-file"), trueFlag("passphrase-stdin")}}}})
		}
	case "vault rewrap":
		add(noTogether(trueFlag("current-passphrase-stdin"), trueFlag("new-passphrase-stdin")))
		for _, side := range [][2]string{{"current", "from-backend"}, {"new", "backend"}} {
			add(noTogether(presentInput(side[0]+"-passphrase-file"), trueFlag(side[0]+"-passphrase-stdin")), map[string]any{"if": map[string]any{"properties": map[string]any{side[1]: map[string]any{"const": "native"}}, "required": []string{side[1]}}, "then": map[string]any{"not": map[string]any{"anyOf": []any{presentInput(side[0] + "-passphrase-file"), trueFlag(side[0] + "-passphrase-stdin")}}}})
		}
	case "backup restore", "proxy teardown":
		add(togetherInputs("apply", "request-id")...)
	case "proxy configure":
		add(noTogether(presentInput("https-address"), trueFlag("disable-https")))
	case "proxy setup":
		add(map[string]any{"if": trueFlag("check"), "then": map[string]any{"not": map[string]any{"anyOf": []any{trueFlag("remove"), presentInput("request-id")}}}, "else": presentInput("request-id")})
	case "proxy list":
		add(noTogether(trueFlag("global"), presentInput("profile")))
	case "proxy start", "proxy stop", "proxy status", "proxy restart", "proxy logs":
		add(noTogether(trueFlag("all"), presentInput("listener")))
		if c.Name == "proxy start" {
			add(noTogether(trueFlag("all"), presentInput("port")))
		}
	case "proxy add", "proxy rm", "proxy clear":
		add(trueFlag("global"))
		if c.Name == "proxy clear" {
			add(trueFlag("all"))
		} else {
			add(map[string]any{"not": trueFlag("all")})
		}
	case "proxy up", "proxy down":
		add(map[string]any{"oneOf": []any{map[string]any{"required": []string{"args"}, "properties": map[string]any{"args": map[string]any{"minItems": 1}}, "not": trueFlag("all")}, map[string]any{"allOf": []any{trueFlag("all"), map[string]any{"properties": map[string]any{"args": map[string]any{"maxItems": 0}}}}}}})
	case "proxy run":
		add(trueFlag("global"), map[string]any{"required": []string{"child_args"}, "properties": map[string]any{"child_args": map[string]any{"minItems": 1}}})
	case "doctor":
		add(map[string]any{"if": trueFlag("project-only"), "then": map[string]any{"properties": map[string]any{"args": map[string]any{"maxItems": 0}}}})
	case "expose start":
		auth := []any{presentInput("auth-file"), trueFlag("auth-stdin"), trueFlag("no-auth")}
		for i := range auth {
			for j := i + 1; j < len(auth); j++ {
				add(map[string]any{"not": map[string]any{"allOf": []any{auth[i], auth[j]}}})
			}
		}
		local := map[string]any{"not": map[string]any{"required": []string{"provider"}, "properties": map[string]any{"provider": map[string]any{"enum": []string{"lan", "cloudflared", "tailscale"}}}}}
		add(map[string]any{"if": local, "then": map[string]any{"not": map[string]any{"anyOf": append(auth, presentInput("alias"), presentInput("serve-port"))}}, "else": map[string]any{"required": []string{"request-id"}, "anyOf": auth}})
		for _, pair := range [][2]string{{"alias", "lan"}, {"serve-port", "tailscale"}} {
			add(map[string]any{"if": presentInput(pair[0]), "then": map[string]any{"required": []string{"provider"}, "properties": map[string]any{"provider": map[string]any{"const": pair[1]}}}})
		}
	}
	if strings.HasPrefix(c.Name, "port ") || c.Name == "instance list" || c.Name == "instance show" {
		add(noTogether(presentInput("instance"), presentInput("dir")))
	}
	if strings.HasPrefix(c.Name, "task ") && c.BodySchema != nil {
		add(noTogether(presentInput("file"), trueFlag("stdin")))
		fields := c.BodySchema["properties"].(map[string]any)
		for _, option := range c.Options {
			for field := range fields {
				if taskField(field) == option.Name {
					add(noTogether(presentInput("file"), presentInput(option.Name)), noTogether(trueFlag("stdin"), presentInput(option.Name)))
				}
			}
		}
		if c.Name == "task claim" {
			add(noTogether(map[string]any{"required": []string{"args"}, "properties": map[string]any{"args": map[string]any{"minItems": 1}}}, presentInput("workstream")))
		}
	}
	for i := range c.Options {
		o := &c.Options[i]
		if c.Name == "doctor" && o.Name == "timeout" {
			o.Pattern = `^\+?(([0-9]+(\.[0-9]*)?|\.[0-9]+)(ns|us|µs|μs|ms|s|m|h))+$`
		}
		if c.Name == "proxy start" && o.Name == "port" || c.Name == "expose start" && o.Name == "serve-port" {
			o.Pattern = portPattern
		}
	}
	if c.Name == "proxy add" {
		c.Arguments[1].Pattern = portPattern
	}
	run := c.Run
	c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		if strings.HasPrefix(c.Name, "task ") && c.BodySchema != nil {
			if r.Options["file"] != "" && r.Options["stdin"] == "true" {
				return nil, argumentError("Choose JSON file or stdin.", "")
			}
			if r.Options["file"] != "" || r.Options["stdin"] == "true" {
				for field := range c.BodySchema["properties"].(map[string]any) {
					if _, ok := r.Options[taskField(field)]; ok {
						return nil, argumentError("Choose JSON input or field options.", field)
					}
				}
			}
		}
		if e := staticPreflight(c.Name, r); e != nil {
			return nil, e
		}
		return run(ctx, streams, r)
	}
}

func staticPreflight(name string, r Request) *protocol.Error {
	o := r.Options
	conflict := false
	switch name {
	case "proxy configure":
		conflict = o["https-address"] != "" && o["disable-https"] == "true"
		if conflict {
			return argumentError("Choose --https-address or --disable-https; they cannot be used together.", "disable-https")
		}
	case "proxy setup":
		conflict = o["check"] == "true" && (o["remove"] == "true" || o["request-id"] != "") || o["check"] != "true" && o["request-id"] == ""
	case "proxy teardown", "backup restore":
		conflict = (o["apply"] != "") != (o["request-id"] != "")
	case "proxy list":
		conflict = o["global"] == "true" && o["profile"] != ""
	case "proxy start", "proxy stop", "proxy status", "proxy restart", "proxy logs":
		conflict = o["all"] == "true" && (o["listener"] != "" || o["port"] != "")
	case "proxy up", "proxy down":
		conflict = (len(r.Args) > 0) == (o["all"] == "true")
	case "task claim":
		conflict = len(r.Args) > 0 && o["workstream"] != ""
	case "vault init", "vault unlock":
		conflict = o["passphrase-file"] != "" && o["passphrase-stdin"] == "true"
		if name == "vault init" && o["backend"] != "passphrase" && (o["passphrase-file"] != "" || o["passphrase-stdin"] == "true") {
			conflict = true
		}
	case "vault rewrap":
		conflict = o["current-passphrase-stdin"] == "true" && o["new-passphrase-stdin"] == "true"
		for _, side := range [][2]string{{"current", "from-backend"}, {"new", "backend"}} {
			file, stdin := o[side[0]+"-passphrase-file"] != "", o[side[0]+"-passphrase-stdin"] == "true"
			if file && stdin || o[side[1]] != "passphrase" && (file || stdin) {
				conflict = true
			}
		}
	}
	if (strings.HasPrefix(name, "port ") || name == "instance list" || name == "instance show") && o["instance"] != "" && o["dir"] != "" {
		conflict = true
	}
	if conflict {
		return argumentError("Conflicting or incomplete options; inspect this command's input schema and help.", "")
	}
	return nil
}
