package proxy

import (
	"errors"
	"os"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// Listener logs contain bounded lifecycle metadata. Request bodies, headers,
// backend output, and control credentials never enter this stream.
type ListenerEvent struct {
	Time       time.Time `json:"time"`
	Listener   string    `json:"listener"`
	Generation string    `json:"generation"`
	State      string    `json:"state"`
	Reason     string    `json:"reason"`
}

func (m Manager) listenerEvent(def network.Listener, record Record) {
	path := "listeners/" + def.ID + "-events.json"
	var events []ListenerEvent
	if e := tasks.ReadPrivate(m.path(path), &events); e != nil && !errors.Is(e, os.ErrNotExist) {
		return
	}
	events = append(events, ListenerEvent{Time: time.Now().UTC(), Listener: def.Name, Generation: record.AttemptID, State: record.State, Reason: record.Reason})
	if len(events) > 128 {
		events = events[len(events)-128:]
	}
	_ = m.write(path, events)
}
func (m Manager) NetworkLogs(name string, all bool) ([]ListenerEvent, *protocol.Error) {
	if all && name != "" {
		return nil, proxyFailure("invalid_argument")
	}
	st, e := m.portStore().Read()
	if e != nil {
		return nil, e
	}
	items := []ListenerEvent{}
	found := false
	if st.Network != nil {
		for _, def := range st.Network.Listeners {
			if !all && def.Name != network.ListenerName(name) {
				continue
			}
			found = true
			var events []ListenerEvent
			if e := tasks.ReadPrivate(m.path("listeners/"+def.ID+"-events.json"), &events); e != nil && !errors.Is(e, os.ErrNotExist) {
				return nil, proxyStorageError()
			}
			items = append(items, events...)
		}
	}
	if !found && !all {
		return nil, proxyFailure("listener_not_found")
	}
	return items, nil
}
