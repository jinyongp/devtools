package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func (a *App) registerDoctor() {
	options := profileOptions(true)
	options = append(options, Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory to diagnose."})
	a.commands = append(a.commands, Command{Name: "doctor", Description: "Diagnose project prerequisites; inspect data.ready and checks for actionable failures.", Arguments: []Argument{{Name: "command"}}, Options: options, Output: object(map[string]any{"ready": map[string]any{"type": "boolean"}, "profile": stringSchema(), "directory": stringSchema(), "env": stringSchema(), "command": stringSchema(), "checks": map[string]any{"type": "array", "items": object(map[string]any{"id": stringSchema(), "status": map[string]any{"enum": []string{"pass", "fail", "skipped"}}, "message": stringSchema(), "remedy": stringSchema(), "expected_version": stringSchema()}, "id", "status", "message")}}, "ready", "profile", "directory", "env", "command", "checks"), Run: a.diagnose})
}

func (a *App) diagnose(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
	report := doctor.New()
	report.Env = r.Options["env"]
	if len(r.Args) > 0 {
		report.Command = r.Args[0]
	}
	dir, e := filepath.Abs(r.Options["dir"])
	if e == nil {
		dir, e = filepath.EvalSymlinks(dir)
	}
	report.Directory = dir
	info, statErr := os.Stat(dir)
	if e != nil || statErr != nil || !info.IsDir() {
		report.Add("directory", "fail", "The selected directory is unavailable.", "Choose an existing project directory with --dir.")
		return report, nil
	}
	report.Add("directory", "pass", "Project directory is accessible.", "")
	p, err := project.Resolve(dir, "")
	if err != nil {
		if err.Code == "project_not_found" && r.Options["profile"] != "" && report.Command == "" {
			p, err = project.Resolve(dir, r.Options["profile"])
			report.Add("configuration", "skipped", "Using an explicit profile without project requirements.", "")
		} else {
			report.Add("configuration", "fail", "Project configuration could not be resolved: "+err.Code+".", "Check devtools.toml, or use --profile for profile-only diagnosis.")
			return report, nil
		}
	}
	if err != nil {
		return nil, err
	}
	if p.Source == "file" {
		report.Add("configuration", "pass", "Project configuration is valid.", "")
		report.Directory = p.Root
	}
	if explicit := r.Options["profile"]; explicit != "" {
		p.Profile = explicit
	}
	report.Profile = p.Profile
	report.Add("profile", "pass", "Project profile is selected.", "")
	requirements := p.Requirements.Merge(project.Requirements{})
	inject := true
	executable := ""
	var boundPath *string
	if report.Command != "" {
		command, ok := p.Commands[report.Command]
		if !ok {
			report.Add("command", "fail", "The named command is undefined.", "Choose a command from devtools.toml.")
			return report, nil
		}
		requirements = requirements.Merge(command.Requirements)
		inject = command.Inject
		executable = command.Exec[0]
		if _, ok := r.Options["env"]; ok {
			inject = true
		} else {
			report.Env = command.Env
		}
		report.Directory = p.Root
	}
	directory, err := a.dataDirectory()
	if err != nil {
		return nil, err
	}
	state, err := (values.Store{Directory: directory, Profile: p.Profile}).Read()
	if err != nil {
		report.Add("value_storage", "fail", "Profile value storage could not be read.", "Check the data directory and its private file permissions.")
		state = nil
	} else {
		report.Add("value_storage", "pass", "Profile value storage is readable.", "")
		if err = state.CheckEnv(report.Env); err != nil {
			report.Add("env", "fail", "The selected env is undefined.", "Create it with devtools env create or select an existing env.")
		} else {
			report.Add("env", "pass", "The selected common/env layer is available.", "")
		}
	}
	if _, err := (tasks.Store{Directory: filepath.Join(filepath.Dir(directory), "tasks"), Profile: p.Profile}).Read(); err != nil {
		report.Add("task_storage", "fail", "Profile task storage could not be read.", "Check task data and its private file permissions.")
	} else {
		report.Add("task_storage", "pass", "Profile task storage is readable.", "")
	}
	if command, ok := p.Commands[report.Command]; ok && (len(command.Bind) > 0 || len(command.Serve) > 0) {
		s, e := a.portStore()
		if e == nil {
			var defaults []int
			defaults, e = portDefaults()
			if e == nil {
				prepared, _, prepareErr := s.Prepare(ctx, p, command, report.Env, state, defaults, true)
				e = prepareErr
				if e == nil {
					executable = prepared.Args[0]
					if path, ok := prepared.Bind["PATH"]; ok {
						boundPath = &path
					}
				}
			}
		}
		if e != nil {
			report.Add("ports", "fail", "Port or binding diagnosis failed: "+e.Code+".", "Check service assignments, references, and active runs.")
		} else {
			report.Add("ports", "pass", "Port and binding checks passed; new assignments are finalized by run.", "")
		}
	}
	for _, check := range doctor.CheckRequirements(ctx, doctor.Input{Directory: report.Directory, Env: report.Env, Requirements: requirements, State: state, Inject: inject, Executable: executable, PathOverride: boundPath}) {
		report.Checks = append(report.Checks, check)
		if check.Status == "fail" {
			report.Ready = false
		}
	}
	return report, nil
}
