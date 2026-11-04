package ports

import (
	"context"
	"errors"
	"os"
	"sort"
	"time"

	"github.com/jinyongp/devtools/internal/progress"
)

type ListenerProcess struct {
	PID     int    `json:"pid"`
	Program string `json:"program"`
}

// ListenerObservation identifies OS listeners, never authority to stop a PID.
// Several processes can share one inherited socket.
type ListenerObservation struct {
	Status    string            `json:"status"`
	Reason    string            `json:"reason"`
	Processes []ListenerProcess `json:"processes"`
}

func ObserveListener(ctx context.Context, port int) ListenerObservation {
	result := ListenerObservation{Status: "unavailable", Reason: "listener_not_identified", Processes: []ListenerProcess{}}
	if port < 1 || port > 65535 {
		result.Reason = "assignment_missing"
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	finish := progress.Begin(ctx, "Inspecting the TCP listener")
	defer finish()
	processes, err := listenerProcesses(ctx, port)
	seen := map[int]bool{}
	for _, process := range processes {
		if process.PID > 0 && !seen[process.PID] {
			seen[process.PID] = true
			result.Processes = append(result.Processes, process)
		}
	}
	sort.Slice(result.Processes, func(i, j int) bool { return result.Processes[i].PID < result.Processes[j].PID })
	if len(result.Processes) > 0 {
		result.Status, result.Reason = "observed", ""
	}
	if err != nil {
		if len(result.Processes) > 0 {
			result.Status = "partial"
		}
		switch {
		case errors.Is(err, context.Canceled):
			result.Reason = "canceled"
		case errors.Is(err, context.DeadlineExceeded):
			result.Reason = "deadline_exceeded"
		case errors.Is(err, os.ErrPermission):
			result.Reason = "permission_denied"
		default:
			result.Reason = "observer_unavailable"
		}
	}
	if len(result.Processes) > 16 {
		result.Processes = result.Processes[:16]
		result.Status, result.Reason = "partial", "too_many_listeners"
	}
	return result
}
