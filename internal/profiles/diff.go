package profiles

import (
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

type TaskMetadata struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Title            string `json:"title"`
	State            string `json:"state"`
	CompletionStatus string `json:"completion_status"`
}

type InstanceIdentity struct {
	Alias     *string `json:"alias"`
	Directory string  `json:"directory"`
}

type Canonical struct {
	Profile   string             `json:"profile"`
	Envs      []string           `json:"envs"`
	Variables []ValueScope       `json:"variables"`
	Secrets   []ValueScope       `json:"secrets"`
	Items     []TaskMetadata     `json:"items"`
	Instances []InstanceIdentity `json:"instances"`
}

type NameChange struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type ValueChange struct {
	Key    string      `json:"key"`
	Action string      `json:"action"`
	Left   *ValueScope `json:"left,omitempty"`
	Right  *ValueScope `json:"right,omitempty"`
}

type TaskChange struct {
	ID     string        `json:"id"`
	Kind   string        `json:"kind"`
	Action string        `json:"action"`
	Left   *TaskMetadata `json:"left,omitempty"`
	Right  *TaskMetadata `json:"right,omitempty"`
}

type InstanceChange struct {
	Action string            `json:"action"`
	Left   *InstanceIdentity `json:"left,omitempty"`
	Right  *InstanceIdentity `json:"right,omitempty"`
}

type Diff struct {
	Left      string           `json:"left"`
	Right     string           `json:"right"`
	Different bool             `json:"different"`
	Envs      []NameChange     `json:"envs"`
	Variables []ValueChange    `json:"variables"`
	Secrets   []ValueChange    `json:"secrets"`
	Items     []TaskChange     `json:"items"`
	Instances []InstanceChange `json:"instances"`
}

func (c Catalog) Canonical(profile string) (Canonical, *protocol.Error) {
	result := Canonical{Profile: profile, Envs: []string{}, Variables: []ValueScope{}, Secrets: []ValueScope{}, Items: []TaskMetadata{}, Instances: []InstanceIdentity{}}
	if !project.ValidProfile(profile) {
		return result, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, map[string]any{"field": "profile"})
	}
	summaries, err := c.List()
	if err != nil {
		return result, err
	}
	var summary *Summary
	for index := range summaries {
		if summaries[index].Profile == profile {
			summary = &summaries[index]
			break
		}
	}
	if summary == nil {
		return result, protocol.NewError("profile_not_found", "The selected profile does not exist in devtools storage.", 3, map[string]any{"profile": profile})
	}
	if summary.Values {
		state, readErr := (values.Store{Directory: filepath.Join(c.Data, "profiles"), Profile: profile}).Read()
		if readErr != nil {
			return result, readErr
		}
		result.Envs = state.EnvNames()
		for _, metadata := range state.ScopeInventory() {
			item := ValueScope{Key: metadata.Key, Common: metadata.Common, Envs: append([]string{}, metadata.Envs...)}
			if metadata.Kind == values.Variable {
				result.Variables = append(result.Variables, item)
			} else if metadata.Kind == values.Secret {
				result.Secrets = append(result.Secrets, item)
			}
		}
	}
	if summary.Tasks {
		state, readErr := (tasks.Store{Directory: filepath.Join(c.Data, "tasks"), Profile: profile}).Read()
		if readErr != nil {
			return result, readErr
		}
		for _, item := range state.Items {
			if item == nil || !state.Included(item) || item.Kind != "task" && item.Kind != "workstream" && item.Kind != "validation" {
				continue
			}
			assessment := state.Assessment(item.ID)
			result.Items = append(result.Items, TaskMetadata{ID: item.ID, Kind: item.Kind, Title: item.Title, State: item.State, CompletionStatus: assessment.CompletionStatus})
		}
		sort.Slice(result.Items, func(i, j int) bool {
			if result.Items[i].Kind != result.Items[j].Kind {
				return result.Items[i].Kind < result.Items[j].Kind
			}
			return result.Items[i].ID < result.Items[j].ID
		})
	}
	portState, portErr := (ports.Store{Directory: filepath.Join(c.Data, "ports")}).Read()
	if portErr != nil {
		return result, portErr
	}
	for _, instance := range portState.Instances {
		if instance.Profile != profile {
			continue
		}
		var alias *string
		if instance.Alias != nil {
			copy := *instance.Alias
			alias = &copy
		}
		result.Instances = append(result.Instances, InstanceIdentity{Alias: alias, Directory: instance.Directory})
	}
	sort.Slice(result.Instances, func(i, j int) bool { return instanceKey(result.Instances[i]) < instanceKey(result.Instances[j]) })
	return result, nil
}

func scopeEqual(left, right ValueScope) bool {
	if left.Common != right.Common || len(left.Envs) != len(right.Envs) {
		return false
	}
	for index := range left.Envs {
		if left.Envs[index] != right.Envs[index] {
			return false
		}
	}
	return true
}

func taskEqual(left, right TaskMetadata) bool {
	return left.Kind == right.Kind && left.Title == right.Title && left.State == right.State && left.CompletionStatus == right.CompletionStatus
}

func instanceKey(instance InstanceIdentity) string {
	alias := ""
	if instance.Alias != nil {
		alias = *instance.Alias
	}
	return instance.Directory + "\x00" + alias
}

func valueChanges(left, right []ValueScope) []ValueChange {
	leftByKey, rightByKey := map[string]ValueScope{}, map[string]ValueScope{}
	keys := map[string]bool{}
	for _, item := range left {
		leftByKey[item.Key], keys[item.Key] = item, true
	}
	for _, item := range right {
		rightByKey[item.Key], keys[item.Key] = item, true
	}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	changes := []ValueChange{}
	for _, key := range names {
		leftItem, leftOK := leftByKey[key]
		rightItem, rightOK := rightByKey[key]
		switch {
		case !leftOK:
			copy := rightItem
			changes = append(changes, ValueChange{Key: key, Action: "added", Right: &copy})
		case !rightOK:
			copy := leftItem
			changes = append(changes, ValueChange{Key: key, Action: "removed", Left: &copy})
		case !scopeEqual(leftItem, rightItem):
			leftCopy, rightCopy := leftItem, rightItem
			changes = append(changes, ValueChange{Key: key, Action: "changed", Left: &leftCopy, Right: &rightCopy})
		}
	}
	return changes
}

func taskChanges(left, right []TaskMetadata) []TaskChange {
	leftByID, rightByID := map[string]TaskMetadata{}, map[string]TaskMetadata{}
	ids := map[string]bool{}
	for _, item := range left {
		leftByID[item.ID], ids[item.ID] = item, true
	}
	for _, item := range right {
		rightByID[item.ID], ids[item.ID] = item, true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	changes := []TaskChange{}
	for _, id := range ordered {
		leftItem, leftOK := leftByID[id]
		rightItem, rightOK := rightByID[id]
		switch {
		case !leftOK:
			copy := rightItem
			changes = append(changes, TaskChange{ID: id, Kind: rightItem.Kind, Action: "added", Right: &copy})
		case !rightOK:
			copy := leftItem
			changes = append(changes, TaskChange{ID: id, Kind: leftItem.Kind, Action: "removed", Left: &copy})
		case !taskEqual(leftItem, rightItem):
			leftCopy, rightCopy := leftItem, rightItem
			changes = append(changes, TaskChange{ID: id, Kind: leftItem.Kind, Action: "changed", Left: &leftCopy, Right: &rightCopy})
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].ID < changes[j].ID
	})
	return changes
}

func instanceChanges(left, right []InstanceIdentity) []InstanceChange {
	leftByKey, rightByKey := map[string]InstanceIdentity{}, map[string]InstanceIdentity{}
	keys := map[string]bool{}
	for _, item := range left {
		key := instanceKey(item)
		leftByKey[key], keys[key] = item, true
	}
	for _, item := range right {
		key := instanceKey(item)
		rightByKey[key], keys[key] = item, true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	changes := []InstanceChange{}
	for _, key := range ordered {
		leftItem, leftOK := leftByKey[key]
		rightItem, rightOK := rightByKey[key]
		if leftOK && rightOK {
			continue
		}
		if leftOK {
			copy := leftItem
			changes = append(changes, InstanceChange{Action: "removed", Left: &copy})
		} else {
			copy := rightItem
			changes = append(changes, InstanceChange{Action: "added", Right: &copy})
		}
	}
	return changes
}

func envChanges(left, right []string) []NameChange {
	leftSet, rightSet := nameSet(left), nameSet(right)
	all := map[string]bool{}
	for name := range leftSet {
		all[name] = true
	}
	for name := range rightSet {
		all[name] = true
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	changes := []NameChange{}
	for _, name := range names {
		if leftSet[name] == rightSet[name] {
			continue
		}
		action := "added"
		if leftSet[name] {
			action = "removed"
		}
		changes = append(changes, NameChange{Name: name, Action: action})
	}
	return changes
}

func CanonicalSnapshot(profile string, valueData, taskData []byte) (Canonical, error) {
	result := Canonical{Profile: profile, Envs: []string{}, Variables: []ValueScope{}, Secrets: []ValueScope{}, Items: []TaskMetadata{}, Instances: []InstanceIdentity{}}
	if len(valueData) > 0 {
		state, err := values.InspectSnapshot(valueData, profile)
		if err != nil {
			return result, err
		}
		result.Envs = state.EnvNames()
		for _, metadata := range state.ScopeInventory() {
			item := ValueScope{Key: metadata.Key, Common: metadata.Common, Envs: append([]string{}, metadata.Envs...)}
			if metadata.Kind == values.Variable {
				result.Variables = append(result.Variables, item)
			} else if metadata.Kind == values.Secret {
				result.Secrets = append(result.Secrets, item)
			}
		}
	}
	if len(taskData) > 0 {
		state, err := tasks.InspectSnapshot(taskData, profile)
		if err != nil {
			return result, err
		}
		for _, item := range state.Items {
			if item == nil || !state.Included(item) || item.Kind != "task" && item.Kind != "workstream" && item.Kind != "validation" {
				continue
			}
			assessment := state.Assessment(item.ID)
			result.Items = append(result.Items, TaskMetadata{ID: item.ID, Kind: item.Kind, Title: item.Title, State: item.State, CompletionStatus: assessment.CompletionStatus})
		}
		sort.Slice(result.Items, func(i, j int) bool {
			if result.Items[i].Kind != result.Items[j].Kind {
				return result.Items[i].Kind < result.Items[j].Kind
			}
			return result.Items[i].ID < result.Items[j].ID
		})
	}
	return result, nil
}

func Compare(left, right Canonical, includeInstances bool) Diff {
	result := Diff{Left: left.Profile, Right: right.Profile, Envs: envChanges(left.Envs, right.Envs), Variables: valueChanges(left.Variables, right.Variables), Secrets: valueChanges(left.Secrets, right.Secrets), Items: taskChanges(left.Items, right.Items), Instances: []InstanceChange{}}
	if includeInstances {
		result.Instances = instanceChanges(left.Instances, right.Instances)
	}
	result.Different = len(result.Envs)+len(result.Variables)+len(result.Secrets)+len(result.Items)+len(result.Instances) > 0
	return result
}

func (c Catalog) Diff(leftProfile, rightProfile string) (Diff, *protocol.Error) {
	result := Diff{Left: leftProfile, Right: rightProfile, Envs: []NameChange{}, Variables: []ValueChange{}, Secrets: []ValueChange{}, Items: []TaskChange{}, Instances: []InstanceChange{}}
	left, err := c.Canonical(leftProfile)
	if err != nil {
		return result, err
	}
	right, err := c.Canonical(rightProfile)
	if err != nil {
		return result, err
	}
	return Compare(left, right, true), nil
}
