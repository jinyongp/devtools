package process

import (
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

type ParentMode string

const (
	ParentInherit   ParentMode = "inherit"
	ParentClean     ParentMode = "clean"
	ParentAllowlist ParentMode = "allowlist"
)

type ParentPolicy struct {
	Mode  ParentMode `json:"mode"`
	Allow []string   `json:"allow,omitempty"`
}

// SelectParent affects inherited variables only. Selecting no stored values is
// separate and cannot promise to remove credentials already present in parent.
func SelectParent(parent []string, policy ParentPolicy) ([]string, *protocol.Error) {
	if policy.Mode == "" {
		policy.Mode = ParentInherit
	}
	if policy.Mode != ParentInherit && policy.Mode != ParentClean && policy.Mode != ParentAllowlist {
		return nil, protocol.NewError("invalid_argument", "Unknown parent environment policy.", 2, nil)
	}
	if policy.Mode != ParentAllowlist && len(policy.Allow) > 0 {
		return nil, protocol.NewError("invalid_argument", "Parent allowlist requires allowlist mode.", 2, nil)
	}
	for _, name := range policy.Allow {
		if name == "" || strings.ContainsAny(name, "=\x00") {
			return nil, protocol.NewError("invalid_argument", "Parent allowlist contains an invalid name.", 2, nil)
		}
	}
	result := make([]string, 0, len(parent))
	for _, item := range parent {
		name, _, ok := splitEnvironment(item)
		if !ok || name == "" {
			return nil, protocol.NewError("invalid_argument", "Parent environment contains an invalid entry.", 2, nil)
		}
		// Task execution credentials authorize control operations, never apps.
		if strings.EqualFold(name, "DEVTOOLS_TASK_CONTEXT") {
			continue
		}
		selected := policy.Mode == ParentInherit
		if policy.Mode == ParentAllowlist {
			for _, allowed := range policy.Allow {
				if sameEnvironmentName(name, allowed) {
					selected = true
					break
				}
			}
		}
		if selected {
			result = append(result, item)
		}
	}
	return result, nil
}
