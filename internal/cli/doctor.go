package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retainedbody"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/valuehandoff"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (a *App) registerDoctor() {
	opts := configOptions()
	opts = append(opts, Option{Name: "storage", Boolean: true, Description: "Inspect managed storage in the selected data root without project lookup, initialization or migration."}, Option{Name: "fix", Boolean: true, Description: "With --storage, explicitly prepare, upgrade and verify managed storage."})
	opts = append(opts, Option{Name: "profile", Pattern: project.ProfilePattern, Completion: completeProfile, Description: "Override the profile while retaining project configuration lookup."}, Option{Name: "env", Pattern: `^$|` + project.ProfilePattern, Completion: completeEnv, Description: "Override every selected command's env and enable injection; empty selects common."}, Option{Name: "bundle", Pattern: project.ProfilePattern, Completion: completeSelection, Description: "Override value selection (all, none or one bundle)."}, Option{Name: "project-only", Boolean: true, Description: "Inspect only project defaults; incompatible with command names."}, Option{Name: "check", Boolean: true, Description: "Exit 3 when required checks fail or remain unknown; report is preserved."}, Option{Name: "mode", Default: "foreground", Pattern: `^(foreground|managed)$`, Description: "Use foreground run or managed process preparation constraints."}, Option{Name: "timeout", Default: "30s", Description: "Positive Go duration bounding the complete diagnosis, including all probes."})
	a.commands = append(a.commands, Command{Name: "doctor", Description: "Diagnose all configured commands by default, or named commands; inspect state, per-command checks and summary.", Arguments: []Argument{{Name: "command", Repeatable: true, Pattern: project.CommandPattern, Completion: completeCommand}}, Options: opts, Output: configcontract.Definition("DoctorReport"), Run: a.diagnose})
}
func (a *App) diagnose(parent context.Context, _ IO, r Request) (any, *protocol.Error) {
	if r.Options["project-only"] == "true" && len(r.Args) > 0 {
		return nil, argumentError("--project-only cannot be combined with command names.", "project-only")
	}
	timeout, err := time.ParseDuration(r.Options["timeout"])
	if err != nil || timeout <= 0 {
		return nil, argumentError("--timeout requires a positive Go duration, such as 30s.", "timeout")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if r.Options["storage"] == "true" {
		return a.diagnoseStorage(ctx, r)
	}
	report := doctor.Diagnosis{Scope: "all", Mode: r.Options["mode"], Directory: r.Options["dir"], Selection: []string{}, Checks: []doctor.Check{}, Commands: []doctor.CommandReport{}}
	if len(r.Args) > 0 {
		report.Scope = "selected"
	}
	if r.Options["project-only"] == "true" {
		report.Scope = "project"
	}
	selected := map[string]bool{}
	for _, name := range r.Args {
		selected[name] = true
	}
	for name := range selected {
		report.Selection = append(report.Selection, name)
	}
	sort.Strings(report.Selection)
	finish := func() (any, *protocol.Error) {
		report.Finalize()
		if errors.Is(parent.Err(), context.Canceled) {
			e := protocol.ContextError(parent.Err())
			e.Details = map[string]any{"reason": "canceled", "report": report}
			return nil, e
		}
		if r.Options["check"] == "true" && !report.Ready {
			return nil, protocol.NewError("requirements_failed", "Required checks did not pass.", 3, map[string]any{"reason": report.State, "report": report})
		}
		return report, nil
	}
	unknownRows := func() {
		for _, name := range report.Selection {
			report.Commands = append(report.Commands, doctor.CommandReport{Name: name, State: "unknown", Checks: []doctor.Check{doctor.NewCheck("command", "unknown", "configuration_unresolved", "The command cannot be resolved until the configuration is valid.")}})
		}
	}
	dir, e := location.ExistingDirectory(r.Options["dir"])
	if e != nil {
		report.Checks = append(report.Checks, doctor.NewCheck("directory", "fail", "not_found", "The selected project directory is unavailable."))
		unknownRows()
		return finish()
	}
	report.Directory = dir
	report.Checks = append(report.Checks, doctor.NewCheck("directory", "pass", "accessible", "The selected project directory is accessible."))
	p, resolveErr := project.Resolve(dir, "")
	if resolveErr != nil {
		if resolveErr.Code == "project_not_found" && r.Options["profile"] != "" && len(r.Args) == 0 {
			p, resolveErr = project.Resolve(dir, r.Options["profile"])
			p.Root = dir
			report.Scope = "profile"
		} else {
			c := doctor.NewCheck("configuration", "fail", resolveErr.Code, resolveErr.Message)
			if target, ok := resolveErr.Details["target"].(map[string]any); ok {
				c.Target = target
			}
			if path, ok := resolveErr.Details["path"].(string); ok && path != "" {
				report.ConfigPath = &path
			}
			report.Checks = append(report.Checks, c)
			unknownRows()
			return finish()
		}
	}
	if resolveErr != nil {
		return nil, resolveErr
	}
	if value := r.Options["profile"]; value != "" {
		p.Profile = value
	}
	report.Profile = &p.Profile
	if p.ConfigPath != "" {
		report.ConfigPath = &p.ConfigPath
		report.Directory = p.Root
	}
	report.Checks = append(report.Checks, doctor.NewCheck("configuration", "pass", "valid", "Project configuration and static references are valid."))
	if report.Scope == "all" {
		for name := range p.Commands {
			report.Selection = append(report.Selection, name)
		}
		sort.Strings(report.Selection)
	}
	directory, pathErr := a.dataDirectory()
	if pathErr != nil {
		report.Checks = append(report.Checks, doctor.NewCheck("value_storage", "fail", pathErr.Code, pathErr.Message))
		unknownRows()
		return finish()
	}
	if report.Scope == "project" || report.Scope == "profile" || len(report.Selection) == 0 {
		command := execution.Command{Project: p, Directory: report.Directory, Requirements: p.Requirements, Selection: p.Bundle, Env: r.Options["env"]}
		if command.Selection == "" {
			command.Selection = "all"
		}
		if s := r.Options["bundle"]; s != "" {
			command.Selection = s
		}
		checks := a.passiveChecks(ctx, command, directory, report.Mode, false)
		report.Checks = append(report.Checks, checks...)
		for _, id := range []string{"injection", "readiness", "launcher"} {
			report.Checks = append(report.Checks, doctor.NewCheck(id, "not_applicable", "no_command", "This constraint requires a command selection."))
		}
		return finish()
	}
	for _, name := range report.Selection {
		var env *string
		if value, ok := r.Options["env"]; ok {
			env = &value
		}
		command, e := execution.ResolveConfigured(p, name, env, nil)
		if e != nil {
			check := doctor.NewCheck("command", "fail", "not_declared", "The selected command is not declared. Run devtools command list.")
			check.Remedies = []protocol.Remedy{doctor.Remedy("List declared commands in this project.", []string{"devtools", "command", "list", "--dir", report.Directory})}
			report.Commands = append(report.Commands, doctor.CommandReport{Name: name, Profile: &p.Profile, State: "not_ready", Checks: []doctor.Check{check}})
			continue
		}
		if bundle := r.Options["bundle"]; bundle != "" {
			command.Selection = bundle
		}
		checks := a.passiveChecks(ctx, command, directory, report.Mode, true)
		profile, envName, bundle := p.Profile, command.Env, command.Selection
		report.Commands = append(report.Commands, doctor.CommandReport{Name: name, Profile: &profile, Env: &envName, Selection: &bundle, Checks: checks})
	}
	return finish()
}

func (a *App) storageEngine(root string) storageupgrade.Engine {
	return storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{values.StorageUpgradeAdapter(root), tasks.StorageUpgradeAdapter(), services.StorageUpgradeAdapter(), services.LogStorageUpgradeAdapter(), retainedbody.StorageUpgradeAdapter(), valuehandoff.StorageUpgradeAdapter()}}
}

func (a *App) diagnoseStorage(ctx context.Context, r Request) (any, *protocol.Error) {
	directory, condition := a.dataDirectory()
	if condition != nil {
		return nil, condition
	}
	engine := a.storageEngine(filepath.Dir(directory))
	var report storageupgrade.Report
	if r.Options["fix"] == "true" {
		report, condition = engine.Fix(ctx)
	} else {
		report, condition = engine.Inspect(ctx)
	}
	if condition != nil {
		if condition.Details == nil {
			condition.Details = map[string]any{}
		}
		condition.Details["report"] = report
		if condition.Code == "deadline_exceeded" {
			condition.Message = "Storage inspection timed out before it could finish."
			if report.Changed || report.Effects != "none" {
				condition.Message = "Storage upgrade timed out before verification completed."
			}
			argv := []string{}
			message := "Review the reported storage check and use a longer --timeout before retrying."
			timeout, _ := time.ParseDuration(r.Options["timeout"])
			if longer := timeout * 2; longer > timeout {
				argv = []string{"devtools", "doctor", "--storage", "--check", "--timeout", max(2*time.Minute, longer).String()}
				message = "Check storage with a longer timeout before applying upgrades."
			}
			condition.Details["remedies"] = []protocol.Remedy{protocol.LocalAction(argv, []string{}, message, "read").Remedy(r.Invocation)}
		}
		return nil, condition
	}
	if !report.Ready && (r.Options["check"] == "true" || r.Options["fix"] == "true") {
		message := "Managed storage could not be verified."
		remedy := protocol.LocalAction([]string{}, []string{}, "Resolve the reported storage problem before retrying.", "read")
		if report.State == "migration_required" {
			message = "Managed storage requires preparation or upgrade."
			remedy = protocol.LocalAction([]string{"devtools", "doctor", "--storage", "--fix"}, []string{}, "Resume the storage upgrade.", "value_write")
		}
		condition = protocol.NewError("requirements_failed", message, 3, map[string]any{
			"reason": report.State, "report": report, "effects": report.Effects, "retry": "after_dependency_change",
			"remedies": []protocol.Remedy{remedy.Remedy(r.Invocation)},
		})
		return nil, condition
	}
	return report, nil
}

func (a *App) passiveChecks(ctx context.Context, command execution.Command, directory, mode string, executable bool) []doctor.Check {
	checks := []doctor.Check{}
	registered := map[string]values.Kind{}
	prerequisites := map[string]values.Prerequisite{}
	public := map[string]string{}
	known := true
	environmentUnknown := ""
	needs := !executable || command.NeedsValues() || len(command.Requirements.Vars)+len(command.Requirements.Secs) > 0
	if needs {
		spec := command.ValueInputSpec()
		spec.Inject = true
		version, observed, observedPrerequisites, e := values.InspectSearchPrerequisites(ctx, filepath.Dir(directory), command.Project.Profile, command.Env, spec)
		if e != nil {
			known = false
			if observedPrerequisites != nil {
				known = true
				prerequisites = observedPrerequisites
			}
			status := "unknown"
			switch e.Code {
			case "env_not_found", "bundle_not_found", "value_expired", "key_not_found", "kind_conflict", "vault_locked_manual", "vault_recovery_required", "migration_required":
				status = "fail"
			}
			if e.Code == vault.ErrIntegrity.Error() || e.Code == vault.ErrInvalid.Error() || e.Code == vault.ErrRecovery.Error() || e.Code == vault.ErrKeyMissing.Error() || e.Code == "storage_error" {
				status = "fail"
			}
			if command.Inject {
				environmentUnknown = "environment_unresolved"
			}
			a.scopeQueryRemedy(e, command.Env)
			check := doctor.NewCheck("value_inputs", status, e.Code, "Selected value metadata could not be authenticated; inspect the reported storage and protection state.")
			check.Diagnostic = protocol.ObservationDiagnostic(e)
			if remedies, ok := e.Details["remedies"].([]protocol.Remedy); ok {
				check.Remedies = remedies
			}
			checks = append(checks, check)
			if vaultStatus, err := vault.InspectSession(ctx, filepath.Join(filepath.Dir(directory), "vault")); err == nil {
				vaultCheck := doctor.NewCheck("vault_session", "unknown", string(vaultStatus.State), "The vault is not opened by diagnosis.")
				if vaultStatus.State == vault.LockedManual || vaultStatus.State == vault.Locking || vaultStatus.State == vault.RecoveryRequired || vaultStatus.State == vault.Locked && vaultStatus.Backend == "passphrase" {
					vaultCheck.Status = "fail"
					vaultCheck.Message = "The vault must be explicitly unlocked or recovered before these inputs can be authenticated."
					vaultCheck.Remedies = []protocol.Remedy{doctor.Remedy("Inspect the protection state and explicitly unlock or recover it.", []string{"devtools", "vault", "status"})}
				}
				checks = append(checks, vaultCheck)
			}
		} else {
			public = observed
			prerequisites = observedPrerequisites
			if version != nil {
				for _, slot := range version.Slots {
					kind := values.Variable
					if slot.Kind == vault.Secret {
						kind = values.Secret
					}
					registered[slot.Name] = kind
				}
			}
			checks = append(checks, doctor.NewCheck("value_inputs", "pass", "authenticated", "Selected value metadata is authenticated without opening secret values."))
		}
	}
	input := doctor.Input{Command: command.Name, Args: command.Arguments(), Profile: command.Project.Profile, Directory: command.Directory, Env: command.Env, Requirements: command.Requirements, Inject: command.Inject, CheckInjection: executable, Environment: os.Environ(), Public: public, Registration: registered, RegistrationKnown: known, EnvironmentUnknown: environmentUnknown, Mode: mode}
	input.Prerequisites = prerequisites
	if command.Project.Document != nil {
		input.Source = func(path []string) configcontract.Source {
			return command.Project.Document.CommandSource(command.Name, path)
		}
	}
	if executable && len(command.Exec) > 0 {
		input.Executable = command.Exec[0]
	}
	if mode == "managed" && command.Ready != nil {
		input.ReadyExec = command.Ready.Exec
	}
	if len(command.Bind)+len(command.Serve) > 0 {
		prepared, e := a.previewExecution(ctx, command, values.DiagnosticPublicState(command.Project.Profile, command.Env, public))
		if e != nil {
			status, message := "unknown", "Network/value bindings could not be resolved without execution."
			switch e.Code {
			case "port_run_active", "port_in_use", "port_not_found", "instance_not_found", "port_assignment_changed":
				status, message = "fail", e.Message
			}
			check := doctor.NewCheck("bindings", status, e.Code, message)
			projected := protocol.Public(e, "doctor")
			check.Facts, _ = projected.Details["facts"].(map[string]any)
			if remedies, ok := e.Details["remedies"].([]protocol.Remedy); ok {
				check.Remedies = remedies
			}
			checks = append(checks, check)
			for key := range command.Bind {
				if strings.EqualFold(key, "PATH") || strings.EqualFold(key, "PATHEXT") {
					input.EnvironmentUnknown = "binding_unresolved"
				}
			}
			if strings.Contains(input.Executable, "${") {
				input.EnvironmentUnknown = "binding_unresolved"
			}
		} else {
			if prepared.Release != nil {
				defer prepared.Release()
			}
			if executable && len(prepared.Args) > 0 {
				input.Executable = prepared.Args[0]
			}
			input.Args = prepared.Args
			input.PathOverride = prepared.PathOverride
			input.SearchOverrides = prepared.SearchOverrides
			checks = append(checks, doctor.NewCheck("bindings", "pass", "resolved", "Public bindings and preview port assignments were resolved without reservation."))
		}
	}
	checks = append(checks, doctor.CheckRequirements(ctx, input)...)
	return checks
}
