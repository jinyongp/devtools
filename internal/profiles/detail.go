package profiles

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

type Summary struct {
	Profile            string `json:"profile"`
	Values             bool   `json:"values"`
	Tasks              bool   `json:"tasks"`
	EnvCount           int    `json:"env_count"`
	InstanceCount      int    `json:"instance_count"`
	ProcessCount       int    `json:"process_count"`
	ActiveProcessCount int    `json:"active_process_count"`
}

type ValueScope struct {
	Key    string   `json:"key"`
	Common bool     `json:"common"`
	Envs   []string `json:"envs"`
}

type TaskCount struct {
	Kind  string `json:"kind"`
	State string `json:"state"`
	Count int    `json:"count"`
}

type Instance struct {
	ID        string  `json:"instance_id"`
	Alias     *string `json:"alias"`
	Directory string  `json:"directory"`
}

type Process struct {
	ID              string              `json:"execution_id"`
	Command         string              `json:"command"`
	Env             string              `json:"env"`
	State           string              `json:"state"`
	ReadyConfigured bool                `json:"ready_configured"`
	Readiness       *services.Readiness `json:"readiness,omitempty"`
}

type Detail struct {
	Summary
	Envs       []string     `json:"envs"`
	Variables  []ValueScope `json:"variables"`
	Secrets    []ValueScope `json:"secrets"`
	TaskCounts []TaskCount  `json:"task_counts"`
	Instances  []Instance   `json:"instances"`
	Processes  []Process    `json:"processes"`
}

func nameSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

func (c Catalog) List() ([]Summary, *protocol.Error) {
	names, err := c.Names()
	if err != nil {
		return nil, err
	}
	valueNames, err := storedProfileNames(filepath.Join(c.Data, "profiles"))
	if err != nil {
		return nil, err
	}
	taskNames, err := storedProfileNames(filepath.Join(c.Data, "tasks"))
	if err != nil {
		return nil, err
	}
	valuesPresent, tasksPresent := nameSet(valueNames), nameSet(taskNames)

	portState, err := (ports.Store{Directory: filepath.Join(c.Data, "ports")}).Read()
	if err != nil {
		return nil, err
	}
	instanceCounts := map[string]int{}
	for _, instance := range portState.Instances {
		instanceCounts[instance.Profile]++
	}
	processRecords, err := (services.Store{Data: c.Data}).StoredRecords()
	if err != nil {
		return nil, err
	}
	processCounts, activeCounts := map[string]int{}, map[string]int{}
	for _, record := range processRecords {
		processCounts[record.Profile]++
		if record.EndedAt == nil && record.State != "interrupted" {
			activeCounts[record.Profile]++
		}
	}

	items := make([]Summary, 0, len(names))
	for _, name := range names {
		summary := Summary{Profile: name, Values: valuesPresent[name], Tasks: tasksPresent[name], InstanceCount: instanceCounts[name], ProcessCount: processCounts[name], ActiveProcessCount: activeCounts[name]}
		if summary.Values {
			envs, readErr := (values.Store{Directory: filepath.Join(c.Data, "profiles"), Profile: name}).CompletionNames("", "")
			if readErr != nil {
				return nil, readErr
			}
			summary.EnvCount = len(envs)
		}
		if summary.Tasks {
			if _, readErr := (tasks.Store{Directory: filepath.Join(c.Data, "tasks"), Profile: name}).CompletionIDs("", ""); readErr != nil {
				return nil, readErr
			}
		}
		items = append(items, summary)
	}
	return items, nil
}

func (c Catalog) Inspect(ctx context.Context, profile string) (Detail, *protocol.Error) {
	var detail Detail
	if !project.ValidProfile(profile) {
		return detail, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, map[string]any{"field": "profile"})
	}
	summaries, err := c.List()
	if err != nil {
		return detail, err
	}
	found := false
	for _, summary := range summaries {
		if summary.Profile == profile {
			detail.Summary = summary
			found = true
			break
		}
	}
	if !found {
		return detail, protocol.NewError("profile_not_found", "The selected profile does not exist in devtools storage.", 3, map[string]any{"profile": profile})
	}
	detail.Envs = []string{}
	detail.Variables = []ValueScope{}
	detail.Secrets = []ValueScope{}
	detail.TaskCounts = []TaskCount{}
	detail.Instances = []Instance{}
	detail.Processes = []Process{}

	if detail.Values {
		state, readErr := (values.Store{Directory: filepath.Join(c.Data, "profiles"), Profile: profile}).Read()
		if readErr != nil {
			return Detail{}, readErr
		}
		detail.Envs = state.EnvNames()
		for _, metadata := range state.ScopeInventory() {
			item := ValueScope{Key: metadata.Key, Common: metadata.Common, Envs: append([]string{}, metadata.Envs...)}
			if metadata.Kind == values.Variable {
				detail.Variables = append(detail.Variables, item)
			} else if metadata.Kind == values.Secret {
				detail.Secrets = append(detail.Secrets, item)
			}
		}
	}
	if detail.Tasks {
		state, readErr := (tasks.Store{Directory: filepath.Join(c.Data, "tasks"), Profile: profile}).Read()
		if readErr != nil {
			return Detail{}, readErr
		}
		counts := map[string]int{}
		for _, item := range state.Items {
			if !state.Included(item) {
				continue
			}
			key := item.Kind + "\x00" + item.State
			counts[key]++
		}
		for key, count := range counts {
			for index := range key {
				if key[index] == 0 {
					detail.TaskCounts = append(detail.TaskCounts, TaskCount{Kind: key[:index], State: key[index+1:], Count: count})
					break
				}
			}
		}
		sort.Slice(detail.TaskCounts, func(i, j int) bool {
			if detail.TaskCounts[i].Kind != detail.TaskCounts[j].Kind {
				return detail.TaskCounts[i].Kind < detail.TaskCounts[j].Kind
			}
			return detail.TaskCounts[i].State < detail.TaskCounts[j].State
		})
	}

	portState, portErr := (ports.Store{Directory: filepath.Join(c.Data, "ports")}).Read()
	if portErr != nil {
		return Detail{}, portErr
	}
	for _, instance := range portState.Instances {
		if instance.Profile == profile {
			detail.Instances = append(detail.Instances, Instance{ID: instance.ID, Alias: instance.Alias, Directory: instance.Directory})
		}
	}
	sort.Slice(detail.Instances, func(i, j int) bool {
		if detail.Instances[i].Directory != detail.Instances[j].Directory {
			return detail.Instances[i].Directory < detail.Instances[j].Directory
		}
		return detail.Instances[i].ID < detail.Instances[j].ID
	})

	statusContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	processes, processErr := (services.Store{Data: c.Data}).List(statusContext, profile)
	if processErr != nil {
		return Detail{}, processErr
	}
	for _, record := range processes {
		detail.Processes = append(detail.Processes, Process{ID: record.ID, Command: record.Command, Env: record.Env, State: record.State, ReadyConfigured: record.ReadyConfigured, Readiness: record.Readiness})
	}
	return detail, nil
}
