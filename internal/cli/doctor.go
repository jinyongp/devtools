package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func (a *App) registerDoctor() {
	options := profileOptions(true)
	options = append(options, Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory to diagnose."})
	check := object(map[string]any{
		"id":               stringSchema(),
		"status":           map[string]any{"enum": []string{"pass", "fail", "skipped"}},
		"message":          stringSchema(),
		"remedies":         map[string]any{"type": "array", "items": remedySchema()},
		"expected_version": stringSchema(),
	}, "id", "status", "message", "remedies")
	a.commands = append(a.commands, Command{Name: "doctor", Description: "Diagnose project prerequisites; inspect data.ready and checks for actionable failures.", Arguments: []Argument{{Name: "command"}}, Options: options, Output: object(map[string]any{"ready": map[string]any{"type": "boolean"}, "profile": stringSchema(), "directory": stringSchema(), "env": stringSchema(), "command": stringSchema(), "checks": map[string]any{"type": "array", "items": check}}, "ready", "profile", "directory", "env", "command", "checks"), Run: a.diagnose})
}

func (a *App) diagnose(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
	report := doctor.New()
	report.Env = r.Options["env"]
	if len(r.Args) > 0 {
		report.Command = r.Args[0]
	}
	dir, e := location.Canonical(r.Options["dir"])
	report.Directory = dir
	info, statErr := os.Stat(dir)
	if e != nil || statErr != nil || !info.IsDir() {
		report.Add("directory", "fail", "The selected directory is unavailable.", doctor.Remedy("Choose an existing project directory with --dir.", nil, "dir"))
		return report, nil
	}
	report.Add("directory", "pass", "Project directory is accessible.")
	p, err := project.Resolve(dir, "")
	if err != nil {
		if err.Code == "project_not_found" && r.Options["profile"] != "" && report.Command == "" {
			p, err = project.Resolve(dir, r.Options["profile"])
			report.Add("configuration", "skipped", "Using an explicit profile without project requirements.")
		} else {
			report.Add("configuration", "fail", "Project configuration could not be resolved: "+err.Code+".", doctor.Remedy("Check devtools.toml, or use --profile for profile-only diagnosis.", nil))
			return report, nil
		}
	}
	if err != nil {
		return nil, err
	}
	if p.Source == "file" {
		report.Add("configuration", "pass", "Project configuration is valid.")
		report.Directory = p.Root
	}
	if explicit := r.Options["profile"]; explicit != "" {
		p.Profile = explicit
	}
	report.Profile = p.Profile
	report.Add("profile", "pass", "Project profile is selected.")

	command := execution.Command{
		Project:      p,
		Directory:    report.Directory,
		Env:          report.Env,
		Inject:       true,
		Requirements: p.Requirements,
	}
	if report.Command != "" {
		var envOverride *string
		if env, explicit := r.Options["env"]; explicit {
			envOverride = &env
		}
		command, err = execution.ResolveConfigured(p, report.Command, envOverride, nil)
		if err != nil {
			if err.Code == "command_not_found" {
				report.Add("command", "fail", "The named command is undefined.", doctor.Remedy("List configured commands and choose one from devtools.toml.", []string{"devtools", "command", "list", "--dir", report.Directory}))
				return report, nil
			}
			return nil, err
		}
		report.Env = command.Env
		report.Directory = command.Directory
	}

	directory, err := a.dataDirectory()
	if err != nil {
		return nil, err
	}
	state, err := (values.Store{Directory: directory, Profile: p.Profile}).Read()
	if err != nil {
		report.Add("value_storage", "fail", "Profile value storage could not be read.", doctor.Remedy("Check the data directory and its private file permissions.", nil))
		state = nil
	} else {
		report.Add("value_storage", "pass", "Profile value storage is readable.")
		if err = state.CheckEnv(report.Env); err != nil {
			argv := []string{"devtools", "env", "create", report.Env, "--profile", p.Profile}
			report.Add("env", "fail", "The selected env is undefined.", doctor.Remedy("Create the selected env or choose an existing env.", argv))
		} else {
			report.Add("env", "pass", "The selected common/env layer is available.")
		}
	}
	if _, err := (tasks.Store{Directory: filepath.Join(filepath.Dir(directory), "tasks"), Profile: p.Profile}).Read(); err != nil {
		report.Add("task_storage", "fail", "Profile task storage could not be read.", doctor.Remedy("Check task data and its private file permissions.", nil))
	} else {
		report.Add("task_storage", "pass", "Profile task storage is readable.")
	}

	prepared := execution.Prepared{Command: command, Args: command.Arguments(), State: state}
	if report.Command != "" && (len(command.Bind) > 0 || len(command.Serve) > 0) {
		var prepareErr *protocol.Error
		prepared, prepareErr = a.previewExecution(ctx, command, state)
		if prepareErr != nil {
			report.Add("ports", "fail", "Port or binding diagnosis failed: "+prepareErr.Code+".", doctor.Remedy("Check service assignments, references, and active runs.", nil))
			prepared = execution.Prepared{Command: command, Args: command.Arguments(), State: state}
		} else {
			report.Add("ports", "pass", "Port and binding checks passed; new assignments are finalized by run.")
		}
	}
	for _, check := range prepared.Checks(ctx, os.Environ(), report.Command != "") {
		report.Checks = append(report.Checks, check)
		if check.Status == "fail" {
			report.Ready = false
		}
	}
	return report, nil
}
