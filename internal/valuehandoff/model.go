// Package valuehandoff implements bounded, approved delivery to cooperative
// runners owned by the same OS user. It is not an application sandbox.
package valuehandoff

import (
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

const UUIDPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
const IDPattern = `^[0-9a-f]{32}$`

var uuidRE = regexp.MustCompile(UUIDPattern)
var idRE = regexp.MustCompile(IDPattern)
var keyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Selection struct {
	Keys   []string `json:"keys"`
	Bundle string   `json:"bundle,omitempty"`
}

func (s Selection) valid() bool {
	if s.Keys == nil || len(s.Keys) > 5000 || s.Bundle != "" && !vault.ValidSelection(s.Bundle) {
		return false
	}
	seen := map[string]bool{}
	for _, k := range s.Keys {
		if !keyRE.MatchString(k) || seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}

type PrepareInput struct {
	ExternalExecutionID string    `json:"external_execution_id"`
	Selection           Selection `json:"selection"`
	Env                 string    `json:"env,omitempty"`
}

// Execution is the public observation surface. Session tickets, ciphertext,
// endpoint addresses and masking material are deliberately absent.
type Execution struct {
	ID                  string    `json:"execution_id"`
	InputID             string    `json:"input_id"`
	ExternalExecutionID string    `json:"external_execution_id"`
	Revision            uint64    `json:"revision"`
	CreatedAt           time.Time `json:"created_at"`
	ExpiresAt           time.Time `json:"expires_at"`
	DeliveryState       string    `json:"delivery_state"`
	ExecutionState      string    `json:"execution_state"`
	RunnerID            string    `json:"runner_id,omitempty"`
	RunnerInstanceID    string    `json:"runner_instance_id,omitempty"`
	ReporterInstanceID  string    `json:"reporter_instance_id,omitempty"`
	DeliveryID          string    `json:"delivery_id,omitempty"`
	ExternalRef         string    `json:"external_ref,omitempty"`
	ExitCode            *int      `json:"exit_code,omitempty"`
	Reason              string    `json:"reason,omitempty"`
	FactSource          string    `json:"fact_source,omitempty"`
}
type Result struct {
	Profile  string    `json:"profile"`
	Item     Execution `json:"item"`
	Changed  bool      `json:"changed"`
	Replayed bool      `json:"replayed"`
}
type View struct {
	Profile string    `json:"profile"`
	Item    Execution `json:"item"`
}
type record struct {
	Item              Execution             `json:"item"`
	Ticket            vault.LaunchTicket    `json:"ticket"`
	Protection        [32]byte              `json:"protection"`
	Env               string                `json:"env"`
	BundleID          string                `json:"bundle_id"`
	Slots             []vault.SlotReference `json:"slots"`
	DeliveryRequestID string                `json:"delivery_request_id,omitempty"`
}
type storedValue struct {
	Key   string          `json:"key"`
	Kind  vault.ValueKind `json:"kind"`
	Value string          `json:"value"`
}
type snapshot struct {
	Values []storedValue `json:"values"`
}

func (snapshot) MarshalJSON() ([]byte, error) { return nil, vault.ErrDenied }
func (snapshot) MarshalText() ([]byte, error) { return nil, vault.ErrDenied }
func (snapshot) Format(s fmt.State, _ rune)   { io.WriteString(s, "[REDACTED]") }

type receipt struct {
	Action      string    `json:"action"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Pending     bool      `json:"pending"`
	ErrorCode   string    `json:"error_code,omitempty"`
	Result      Result    `json:"result"`
}

func failure(code string) *protocol.Error {
	return protocol.NewError(code, "Cannot complete the approved execution operation.", 3, nil)
}
func invalid() *protocol.Error {
	return protocol.NewError("invalid_argument", "Provide a valid execution request and canonical lowercase IDs.", 2, nil)
}
func opaque(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, c := range []byte(s) {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}
