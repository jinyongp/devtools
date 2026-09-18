package tasks

import (
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestDiagnosticsUseProtocolRemedy(t *testing.T) {
	state := NewState()
	err := state.noChangeFailure(Request{}, "app")
	remedies, ok := err.Details["remedies"].([]protocol.Remedy)
	if !ok || len(remedies) != 1 {
		t.Fatalf("unexpected remedies type/value: %#v", err.Details["remedies"])
	}
	want := protocol.Remedy{Argv: []string{"devtools", "task", "current", "--profile", "app"}, RequiredInputs: []string{}, Message: "Inspect current execution state."}
	if !reflect.DeepEqual(remedies[0], want) {
		t.Fatalf("unexpected remedy: %#v", remedies[0])
	}
}
