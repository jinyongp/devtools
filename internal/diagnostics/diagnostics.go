// Package diagnostics aggregates read-only local devtools health metadata.
package diagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/dashboard"
	"github.com/jinyongp/devtools/internal/ports"
	profilecatalog "github.com/jinyongp/devtools/internal/profiles"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/services"
)

type Input struct {
	Data   string "json:\"data\""
	Config string "json:\"config\""
	Cache  string "json:\"cache\""
}

type Paths struct {
	Data   string "json:\"data\""
	Config string "json:\"config\""
	Cache  string "json:\"cache\""
}

type Section struct {
	Status    string "json:\"status\""
	ErrorCode string "json:\"error_code,omitempty\""
}

type Profiles struct {
	Section
	Items []profilecatalog.Summary "json:\"items\""
}

type Ports struct {
	Section
	Instances        int "json:\"instances\""
	Assignments      int "json:\"assignments\""
	Reservations     int "json:\"reservations\""
	MissingInstances int "json:\"missing_instances\""
	UnknownLocations int "json:\"unknown_locations\""
}

type Processes struct {
	Section
	Total       int "json:\"total\""
	Active      int "json:\"active\""
	Ended       int "json:\"ended\""
	Running     int "json:\"running\""
	Failed      int "json:\"failed\""
	Unknown     int "json:\"unknown\""
	Interrupted int "json:\"interrupted\""
}

type Proxy struct {
	Section
	Running bool   "json:\"running\""
	State   string "json:\"state\""
	Port    int    "json:\"port\""
	URL     string "json:\"url\""
	Reason  string "json:\"reason\""
}

type Dashboard struct {
	Section
	Running bool "json:\"running\""
}

type Backup struct {
	Section
	Configured bool   "json:\"configured\""
	Directory  string "json:\"directory\""
}

type Issue struct {
	Section string "json:\"section\""
	Code    string "json:\"code\""
}

type Report struct {
	Ready     bool      "json:\"ready\""
	Paths     Paths     "json:\"paths\""
	Profiles  Profiles  "json:\"profiles\""
	Ports     Ports     "json:\"ports\""
	Processes Processes "json:\"processes\""
	Proxy     Proxy     "json:\"proxy\""
	Dashboard Dashboard "json:\"dashboard\""
	Backup    Backup    "json:\"backup\""
	Issues    []Issue   "json:\"issues\""
}

func healthy() Section { return Section{Status: "pass"} }

func (r *Report) issue(section string, err *protocol.Error) {
	r.Ready = false
	code := "io_error"
	if err != nil && err.Code != "" {
		code = err.Code
	}
	r.Issues = append(r.Issues, Issue{Section: section, Code: code})
}

func canceled(ctx context.Context) *protocol.Error {
	if ctx.Err() == nil {
		return nil
	}
	return protocol.NewError("canceled", "Diagnostics canceled.", 130, nil)
}

// Inspect reads aggregate subsystem state without creating sessions, mutations,
// archive payloads, process logs, or execution contexts.
func Inspect(ctx context.Context, input Input) (Report, *protocol.Error) {
	report := Report{
		Ready:     true,
		Paths:     Paths{Data: input.Data, Config: input.Config, Cache: input.Cache},
		Profiles:  Profiles{Section: healthy(), Items: []profilecatalog.Summary{}},
		Ports:     Ports{Section: healthy()},
		Processes: Processes{Section: healthy()},
		Proxy:     Proxy{Section: healthy()},
		Dashboard: Dashboard{Section: healthy()},
		Backup:    Backup{Section: healthy()},
		Issues:    []Issue{},
	}
	if !filepath.IsAbs(input.Data) || !filepath.IsAbs(input.Config) || !filepath.IsAbs(input.Cache) {
		return Report{}, protocol.NewError("invalid_argument", "Diagnostics requires absolute user directories.", 2, nil)
	}
	if err := canceled(ctx); err != nil {
		return Report{}, err
	}

	profiles, err := (profilecatalog.Catalog{Data: input.Data}).List()
	if err != nil {
		report.Profiles.Section = Section{Status: "fail", ErrorCode: err.Code}
		report.issue("profiles", err)
	} else {
		report.Profiles.Items = profiles
	}

	portState, portErr := (ports.Store{Directory: filepath.Join(input.Data, "ports")}).Read()
	if portErr != nil {
		report.Ports.Section = Section{Status: "fail", ErrorCode: portErr.Code}
		report.issue("ports", portErr)
	} else {
		report.Ports.Instances = len(portState.Instances)
		report.Ports.Assignments = len(portState.Assignments)
		report.Ports.Reservations = len(portState.Reservations)
		for _, instance := range portState.Instances {
			info, statErr := os.Stat(instance.Directory)
			if errors.Is(statErr, os.ErrNotExist) {
				report.Ports.MissingInstances++
			} else if statErr != nil || !info.IsDir() {
				report.Ports.UnknownLocations++
			}
		}
	}

	processes, processErr := (services.Store{Data: input.Data}).List(ctx, "")
	if processErr != nil {
		if err := canceled(ctx); err != nil {
			return Report{}, err
		}
		report.Processes.Section = Section{Status: "fail", ErrorCode: processErr.Code}
		report.issue("processes", processErr)
	} else {
		report.Processes.Total = len(processes)
		processCounts := map[string]int{}
		activeCounts := map[string]int{}
		for _, record := range processes {
			processCounts[record.Profile]++
			if record.EndedAt != nil {
				report.Processes.Ended++
			} else if record.State != "interrupted" {
				report.Processes.Active++
				activeCounts[record.Profile]++
			}
			switch record.State {
			case "running":
				report.Processes.Running++
			case "failed":
				report.Processes.Failed++
			case "unknown":
				report.Processes.Unknown++
			case "interrupted":
				report.Processes.Interrupted++
			}
		}
		if report.Profiles.Status == "pass" {
			for index := range report.Profiles.Items {
				profile := &report.Profiles.Items[index]
				profile.ProcessCount = processCounts[profile.Profile]
				profile.ActiveProcessCount = activeCounts[profile.Profile]
			}
		}
	}

	proxyStatus, proxyErr := (proxy.Manager{Data: input.Data}).Status(ctx)
	if proxyErr != nil {
		if err := canceled(ctx); err != nil {
			return Report{}, err
		}
		report.Proxy.Section = Section{Status: "fail", ErrorCode: proxyErr.Code}
		report.issue("proxy", proxyErr)
	} else {
		report.Proxy.Running = proxyStatus.Running
		report.Proxy.State = proxyStatus.State
		report.Proxy.Port = proxyStatus.Port
		report.Proxy.URL = proxyStatus.URL
		report.Proxy.Reason = proxyStatus.Reason
	}

	dashboardStatus, dashboardErr := dashboard.Status(ctx, input.Cache)
	if dashboardErr != nil {
		if err := canceled(ctx); err != nil {
			return Report{}, err
		}
		report.Dashboard.Section = Section{Status: "fail", ErrorCode: dashboardErr.Code}
		report.issue("dashboard", dashboardErr)
	} else if running, ok := dashboardStatus["running"].(bool); ok {
		report.Dashboard.Running = running
	}

	backupStatus, backupErr := (backup.Engine{Data: input.Data, Config: input.Config, Cache: input.Cache}).Status()
	if backupErr != nil {
		report.Backup.Section = Section{Status: "fail", ErrorCode: backupErr.Code}
		report.issue("backup", backupErr)
	} else {
		report.Backup.Configured = backupStatus.Configured
		report.Backup.Directory = backupStatus.Directory
	}

	if err := canceled(ctx); err != nil {
		return Report{}, err
	}
	return report, nil
}
