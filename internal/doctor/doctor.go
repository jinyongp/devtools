// Package doctor evaluates prerequisites shared by diagnosis and execution.
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/versionrange"
)

type Check struct {
	ID       string                  `json:"id"`
	Status   string                  `json:"status"`
	Required bool                    `json:"required"`
	Reason   string                  `json:"reason"`
	Message  string                  `json:"message"`
	Target   map[string]any          `json:"target"`
	Sources  []configcontract.Source `json:"sources"`
	Facts    map[string]any          `json:"facts"`
	Remedies []protocol.Remedy       `json:"remedies"`
}

func Remedy(message string, argv []string, inputs ...string) protocol.Remedy {
	if argv == nil {
		argv = []string{}
	}
	if inputs == nil {
		inputs = []string{}
	}
	effect := "read"
	for _, s := range argv {
		switch s {
		case "set", "create", "unlock", "init":
			effect = "value_write"
		}
	}
	return protocol.Remedy{Argv: argv, RequiredInputs: inputs, Message: message, Effect: effect}
}
func NewCheck(id, status, reason, message string) Check {
	return Check{ID: id, Status: status, Required: status != "not_applicable", Reason: reason, Message: message, Target: map[string]any{"kind": "prerequisite", "name": id}, Sources: []configcontract.Source{}, Facts: map[string]any{}, Remedies: []protocol.Remedy{}}
}
func State(checks []Check) string {
	state := "ready"
	for _, c := range checks {
		if !c.Required {
			continue
		}
		if c.Status == "fail" {
			return "not_ready"
		}
		if c.Status == "unknown" || c.Status == "skipped" {
			state = "unknown"
		}
	}
	return state
}

type Input struct {
	Command                                string
	Args                                   []string
	Profile, Directory, Env                string
	Requirements                           project.Requirements
	State                                  *values.State
	Inject, CheckInjection                 bool
	Executable                             string
	Environment                            []string
	PathOverride                           *string
	SearchOverrides                        map[string]string
	Public                                 map[string]string
	Registration                           map[string]values.Kind
	RegistrationKnown                      bool
	EnvironmentUnknown, SearchOrigin, Mode string
	ReadyExec                              []string
	Source                                 func([]string) configcontract.Source
}

func CheckRequirements(ctx context.Context, in Input) []Check {
	checks := []Check{}
	add := func(id, status, reason, message string) int {
		c := NewCheck(id, status, reason, message)
		c.Target["profile"] = in.Profile
		c.Target["env"] = in.Env
		checks = append(checks, c)
		return len(checks) - 1
	}
	parent := in.Environment
	if parent == nil {
		parent = os.Environ()
	}
	parent, _ = process.SelectParent(parent, process.ParentPolicy{})
	public := in.Public
	registered := in.Registration
	known := in.RegistrationKnown
	secrets := map[string]string{}
	if in.State != nil {
		registered = map[string]values.Kind{}
		known = true
		public = map[string]string{}
		for _, kind := range []values.Kind{values.Variable, values.Secret} {
			metadata, e := in.State.List(kind, in.Env)
			if e != nil {
				known = false
			}
			for _, m := range metadata {
				registered[m.Key] = kind
				if kind == values.Secret {
					secrets[m.Key] = ""
				}
			}
		}
		if effective, e := in.State.Environment(in.Env); e == nil {
			for k, v := range effective {
				if _, secret := process.EnvironmentValue(secrets, k); !secret {
					public[k] = v
				}
			}
		} else {
			in.EnvironmentUnknown = "environment_unresolved"
		}
	}
	for key, kind := range registered {
		if kind == values.Secret {
			secrets[key] = ""
		}
	}
	filtered := []string{}
	for _, item := range parent {
		name, _, _ := strings.Cut(item, "=")
		if _, secret := process.EnvironmentValue(secrets, name); !secret {
			filtered = append(filtered, item)
		}
	}
	parent = filtered
	if in.Inject && known {
		for _, key := range []string{"PATH", "PATHEXT"} {
			if _, secret := process.EnvironmentValue(secrets, key); secret {
				in.EnvironmentUnknown = "secret_search_environment"
			}
		}
		names := map[string]string{}
		for k, v := range public {
			names[k] = v
		}
		for k := range registered {
			names[k] = ""
		}
		if e := process.ValidateEnvironment(names); e != nil {
			add("environment:names", "fail", "environment_name_conflict", fmt.Sprintf("Injected environment names conflict: %q.", e.Details["keys"]))
			checks[len(checks)-1].Facts["keys"] = e.Details["keys"]
			in.EnvironmentUnknown = "environment_name_conflict"
		}
		selected := map[string]string{}
		for _, key := range []string{"PATH", "PATHEXT"} {
			if v, ok := process.EnvironmentValue(public, key); ok {
				selected[key] = v
			}
		}
		if len(selected) > 0 {
			in.SearchOrigin = "profile"
			parent = process.Environment(parent, selected)
		}
	}
	if in.PathOverride != nil {
		parent = process.Environment(parent, map[string]string{"PATH": *in.PathOverride})
		in.SearchOrigin = "binding"
	}
	if len(in.SearchOverrides) > 0 {
		parent = process.Environment(parent, in.SearchOverrides)
		in.SearchOrigin = "binding"
	}
	if in.SearchOrigin == "" {
		in.SearchOrigin = "parent"
	}
	type lookedUp struct {
		info          os.FileInfo
		args, version string
		check         int
	}
	lookups := []lookedUp{}
	tool := func(id, name string, t project.Tool) {
		t = t.WithDefaults(name)
		status, reason, message := "pass", "present", "Executable "+t.Executable+" is available."
		facts := map[string]any{"executable": t.Executable, "search_origin": in.SearchOrigin}
		if t.Version != "" {
			facts["expected_version"] = t.Version
		}
		var identity os.FileInfo
		independent := t.Version == "" && !strings.Contains(t.Executable, "${") && strings.ContainsAny(t.Executable, `/\`) && (runtime.GOOS != "windows" || filepath.Ext(t.Executable) != "")
		if ctx.Err() != nil {
			status, reason, message = "unknown", "deadline_exceeded", "The diagnosis deadline ended before this check."
			if errors.Is(ctx.Err(), context.Canceled) {
				reason = "canceled"
				message = "Diagnosis was explicitly canceled."
			}
		} else if in.EnvironmentUnknown != "" && !independent {
			status, reason, message = "unknown", in.EnvironmentUnknown, "The selected search environment could not be authenticated."
		} else {
			path, e := process.LookPath(t.Executable, in.Directory, parent)
			info, statErr := os.Stat(path)
			identity = info
			if e != nil || statErr != nil || !process.IsExecutable(info) {
				status, reason, message = "fail", "not_found", "Executable "+t.Executable+" was not found in the selected PATH/PATHEXT."
				if errors.Is(statErr, os.ErrPermission) {
					reason = "permission_denied"
				}
				if statErr == nil && info != nil && !process.IsExecutable(info) {
					reason = "not_executable"
				}
				if e != nil && e.Code == "invalid_argument" {
					reason = "invalid_search_environment"
				}
				if e != nil {
					if value, ok := e.Details["reason"].(string); ok {
						reason = value
					}
				}
				if reason != "not_found" {
					message = "Executable " + t.Executable + " is unavailable: " + reason + "."
				}
			} else if t.Version != "" {
				var observed string
				reason, observed = probe(ctx, path, t.VersionArgs, in.Directory, parent)
				if observed != "" {
					facts["observed_version"] = observed
				}
				if reason == "version_token" {
					if versionrange.Matches(t.Version, observed) {
						reason = "version_match"
					} else {
						reason = "version_mismatch"
					}
				}
				status = "fail"
				if reason == "version_match" {
					status = "pass"
				}
				if reason == "deadline_exceeded" || reason == "canceled" {
					status = "unknown"
				}
				message = "Executable " + t.Executable + ": " + reason + " (expected " + t.Version + ")."
			}
		}
		i := add(id, status, reason, message)
		checks[i].Target = map[string]any{"kind": "tool", "name": name, "profile": in.Profile, "env": in.Env}
		checks[i].Facts = facts
		if identity != nil && t.Version != "" {
			args, _ := json.Marshal(t.VersionArgs)
			lookups = append(lookups, lookedUp{info: identity, args: string(args), version: t.Version, check: i})
		}
		if status != "pass" {
			checks[i].Remedies = []protocol.Remedy{Remedy("Make the declared executable available at a version satisfying the requirement; verify version_args.", nil)}
			checks[i].Remedies[0].Effect = "system_write"
		}
		if in.Source != nil {
			path := []string{"tools", name}
			if id == "command_executable" {
				path = []string{"commands", in.Command, "exec", "0"}
			}
			if id == "readiness_executable" {
				path = []string{"commands", in.Command, "ready", "exec", "0"}
			}
			checks[i].Sources = append(checks[i].Sources, in.Source(path))
		}
	}
	names := []string{}
	for name := range in.Requirements.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tool("tool:"+name, name, in.Requirements.Tools[name])
	}
	// Group by resolved file identity, not executable spelling. Check the whole
	// group: pairwise overlap does not imply a common version for three ranges.
	grouped := make([]bool, len(lookups))
	for i, first := range lookups {
		if grouped[i] {
			continue
		}
		members := []int{}
		requirements := []string{}
		for j, item := range lookups {
			if !grouped[j] && first.args == item.args && os.SameFile(first.info, item.info) {
				grouped[j] = true
				members = append(members, item.check)
				requirements = append(requirements, item.version)
			}
		}
		if !versionrange.Intersects(requirements...) {
			for _, index := range members {
				checks[index].Status = "fail"
				checks[index].Reason = "version_conflict"
				checks[index].Message = "The same executable and probe arguments have version requirements with no common version."
				checks[index].Remedies = []protocol.Remedy{{Effect: "config_write", Argv: []string{}, RequiredInputs: []string{}, Message: "Reconcile the version ranges declared for this executable."}}
			}
		}
	}
	if in.Executable != "" {
		tool("command_executable", in.Executable, project.Tool{Executable: in.Executable})
	}
	if in.Mode == "managed" && len(in.ReadyExec) > 0 {
		tool("readiness_executable", in.ReadyExec[0], project.Tool{Executable: in.ReadyExec[0]})
	}
	if in.Mode == "managed" {
		launcher := func(id string, args []string) {
			if len(args) == 0 {
				return
			}
			status, reason := "pass", "supported"
			if in.EnvironmentUnknown != "" {
				status, reason = "unknown", in.EnvironmentUnknown
			} else if err := process.ValidateManaged(args, in.Directory, parent); err != nil {
				status, reason = "fail", err.Code
			}
			add(id, status, reason, "Managed launcher constraints were checked without starting the application.")
		}
		args := in.Args
		if len(args) == 0 && in.Executable != "" {
			args = []string{in.Executable}
		}
		launcher("launcher", args)
		launcher("readiness_launcher", in.ReadyExec)
	}
	for _, kind := range []values.Kind{values.Variable, values.Secret} {
		keys := append([]string{}, in.Requirements.Vars...)
		label := "var"
		if kind == values.Secret {
			keys = append([]string{}, in.Requirements.Secs...)
			label = "sec"
		}
		sort.Strings(keys)
		for _, key := range keys {
			status, reason, message := "pass", "registered", "Required "+label+" "+key+" is registered."
			if !known {
				status, reason, message = "unknown", "registration_unverified", "Registration could not be authenticated."
			} else if registered[key] != kind {
				status, reason, message = "fail", "not_registered", "Required "+label+" "+key+" is missing or has another kind."
			}
			i := add(label+":"+key, status, reason, message)
			checks[i].Target = map[string]any{"kind": label, "name": key, "profile": in.Profile, "env": in.Env}
			if status == "fail" {
				argv := []string{"devtools", label, "set", key, "--profile", in.Profile}
				required := []string{"value"}
				if in.Env != "" {
					argv = append(argv, "--env", in.Env)
				}
				if kind == values.Secret {
					argv = append(argv, "--stdin")
					required = []string{"stdin"}
				}
				checks[i].Remedies = []protocol.Remedy{Remedy("Register the required key in the selected profile and env.", argv, required...)}
			}
		}
	}
	if in.CheckInjection || in.Executable != "" {
		if !in.Inject && len(in.Requirements.Vars)+len(in.Requirements.Secs) > 0 {
			i := add("injection", "fail", "injection_disabled", "Required values are declared but injection is disabled.")
			checks[i].Remedies = []protocol.Remedy{Remedy("Set inject = true or explicitly select --env.", nil)}
			checks[i].Remedies[0].Effect = "config_write"
		}
	}
	return checks
}

func Failure(checks []Check, command, dir, profile, env, selection, mode string, inject bool) *protocol.Error {
	if State(checks) == "ready" {
		return nil
	}
	code, exit := "requirements_failed", 3
	for _, c := range checks {
		if c.ID == "command_executable" && c.Status == "fail" && c.Reason == "not_found" {
			code, exit = "executable_not_found", 127
		}
	}
	for _, c := range checks {
		if c.Reason == "canceled" {
			code, exit = "canceled", 130
			break
		}
	}
	if selection == "" {
		selection = "all"
	}
	if mode == "" {
		mode = "foreground"
	}
	argv := []string{"devtools", "doctor", "--dir", dir, "--profile", profile, "--bundle", selection, "--mode", mode}
	if inject {
		argv = append(argv, "--env", env)
	}
	if command != "" {
		argv = append(argv, command)
	}
	problems := []string{}
	for _, check := range checks {
		if check.Required && (check.Status == "fail" || check.Status == "unknown") {
			if len(problems) < 5 {
				problems = append(problems, check.ID+" ("+check.Reason+")")
			}
		}
	}
	message := fmt.Sprintf("Command %q prerequisites failed: %s.", command, strings.Join(problems, ", "))
	if code == "executable_not_found" {
		for _, check := range checks {
			if check.ID == "command_executable" {
				message = fmt.Sprintf("Executable %q was not found for command %q. Other prerequisite results are included in checks.", check.Facts["executable"], command)
				break
			}
		}
	}
	if code == "canceled" {
		message = "Command prerequisite checks were explicitly canceled."
	}
	return protocol.NewError(code, message, exit, map[string]any{"phase": "preflight", "reason": code, "target": map[string]any{"kind": "command", "name": command, "profile": profile, "env": env, "command": command, "directory": dir, "bundle": selection}, "checks": checks, "remedies": []protocol.Remedy{Remedy("Diagnose the same command selection and execution mode.", argv)}})
}
