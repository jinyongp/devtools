// Package doctor checks declared prerequisites without exposing variable values
// or tool output. It is shared by explicit diagnosis and run preflight.
package doctor

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

type Check struct {
	ID              string            `json:"id"`
	Status          string            `json:"status"`
	Message         string            `json:"message"`
	Remedies        []protocol.Remedy `json:"remedies"`
	ExpectedVersion string            `json:"expected_version,omitempty"`
}
type Report struct {
	Ready     bool    `json:"ready"`
	Profile   string  `json:"profile"`
	Directory string  `json:"directory"`
	Env       string  `json:"env"`
	Command   string  `json:"command"`
	Checks    []Check `json:"checks"`
}

func Remedy(message string, argv []string, requiredInputs ...string) protocol.Remedy {
	if argv == nil {
		argv = []string{}
	}
	if requiredInputs == nil {
		requiredInputs = []string{}
	}
	return protocol.Remedy{Argv: argv, RequiredInputs: requiredInputs, Message: message}
}

func New() Report { return Report{Ready: true, Checks: []Check{}} }
func (r *Report) Add(id, status, message string, remedies ...protocol.Remedy) {
	if remedies == nil {
		remedies = []protocol.Remedy{}
	}
	r.Checks = append(r.Checks, Check{ID: id, Status: status, Message: message, Remedies: remedies})
	if status == "fail" {
		r.Ready = false
	}
}

type Input struct {
	Profile      string
	Directory    string
	Env          string
	Requirements project.Requirements
	State        *values.State
	Inject       bool
	Executable   string
	Environment  []string
	PathOverride *string
}

func CheckRequirements(ctx context.Context, in Input) []Check {
	report := New()
	probeEnv := in.Environment
	if probeEnv == nil {
		probeEnv = os.Environ()
	}
	// Use the effective PATH, while keeping profile secrets out of version probes.
	if in.Inject && in.State != nil {
		if effective, e := in.State.Environment(in.Env); e == nil {
			if path, ok := effective["PATH"]; ok {
				probeEnv = process.Environment(probeEnv, map[string]string{"PATH": path})
			}
		}
	}
	if in.PathOverride != nil {
		probeEnv = process.Environment(probeEnv, map[string]string{"PATH": *in.PathOverride})
	}
	checkTool := func(name string, tool project.Tool) {
		tool = tool.WithDefaults(name)
		id := "tool:" + name
		path, e := process.LookPath(tool.Executable, in.Directory, probeEnv)
		info, statErr := os.Stat(path)
		if e != nil || statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			report.Add(id, "fail", "Required executable is unavailable.", Remedy("Install the declared tool or update the command's PATH.", nil))
			return
		}
		if tool.Version == "" {
			report.Add(id, "pass", "Required executable is available.")
			return
		}
		args := tool.VersionArgs
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		output := &limitedOutput{}
		code, err := process.Execute(probe, append([]string{path}, args...), in.Directory, probeEnv, nil, output, output)
		passed := err == nil && code == 0 && probe.Err() == nil && !output.truncated && matchesVersion(output.data.String(), tool.Version)
		if passed {
			report.Add(id, "pass", "Required exact version is available.")
		} else {
			report.Add(id, "fail", "Exact version check failed or the version probe could not finish.", Remedy("Install the declared exact version; check version_args and the selected PATH.", nil))
		}
		report.Checks[len(report.Checks)-1].ExpectedVersion = tool.Version
	}
	names := []string{}
	for name := range in.Requirements.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ctx.Err() != nil {
			report.Add("canceled", "fail", "Diagnosis canceled.", Remedy("Retry diagnosis.", nil))
			return report.Checks
		}
		checkTool(name, in.Requirements.Tools[name])
	}
	if in.Executable != "" {
		checkTool("command", project.Tool{Executable: in.Executable})
		report.Checks[len(report.Checks)-1].ID = "command_executable"
	}
	registrationRemedy := func(label, key string) protocol.Remedy {
		argv := []string{"devtools", label, "set", key}
		required := []string{}
		if in.Profile != "" {
			argv = append(argv, "--profile", in.Profile)
		} else {
			required = append(required, "profile")
		}
		if in.Env != "" {
			argv = append(argv, "--env", in.Env)
		}
		if label == "sec" {
			argv = append(argv, "--stdin")
			required = append(required, "stdin")
		} else {
			required = append(required, "value")
		}
		return Remedy("Register the required "+map[string]string{"var": "variable", "sec": "secret"}[label]+" in the selected profile and env.", argv, required...)
	}
	for _, kind := range []values.Kind{values.Variable, values.Secret} {
		keys := in.Requirements.Vars
		label := "var"
		if kind == values.Secret {
			keys = in.Requirements.Secs
			label = "sec"
		}
		known := map[string]bool{}
		available := in.State != nil
		if available {
			metadata, e := in.State.List(kind, in.Env)
			available = e == nil
			for _, m := range metadata {
				known[m.Key] = true
			}
		}
		for _, key := range keys {
			if !available {
				report.Add(label+":"+key, "skipped", "Registration check needs readable storage and a valid env.", Remedy("Resolve the storage or env check first.", nil))
			} else if known[key] {
				report.Add(label+":"+key, "pass", "Required key is registered in the selected layer.")
			} else {
				report.Add(label+":"+key, "fail", "Required key is missing or registered with another kind.", registrationRemedy(label, key))
			}
		}
	}
	if in.Executable != "" && !in.Inject && len(in.Requirements.Vars)+len(in.Requirements.Secs) > 0 {
		report.Add("injection", "fail", "This command requires registered values and injection is disabled.", Remedy("Set inject = true for this command or select --env.", nil))
	}
	return report.Checks
}

var versions = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+(?:[-+_][0-9A-Za-z.+_-]+)?`)

func matchesVersion(output, expected string) bool { return versions.FindString(output) == expected }

type limitedOutput struct {
	mu        sync.Mutex
	data      strings.Builder
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := (16 << 10) - b.data.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.data.Write(p)
	return n, nil
}
