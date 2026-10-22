package execution

import "github.com/jinyongp/devtools/internal/protocol"

func hasCauseCode(e *protocol.Error, code string) bool {
	if e == nil {
		return false
	}
	if e.Code == code {
		return true
	}
	causes, _ := e.Details["causes"].([]any)
	for _, cause := range causes {
		if m, ok := cause.(map[string]any); ok && m["code"] == code {
			return true
		}
	}
	return false
}
