package network

// ExecutionSnapshot contains public network values and private source digests.
// Store it beside an execution, never as part of public process diagnostics.
type ExecutionSnapshot struct {
	ID           string            `json:"id"`
	InstanceID   string            `json:"instance_id"`
	Ports        map[string]int    `json:"ports"`
	Bindings     map[string]string `json:"bindings"`
	Activations  []Activation      `json:"activations"`
	Listeners    map[string]string `json:"listeners"`
	SourceDigest string            `json:"source_digest"`
	IsolatedRoot string            `json:"isolated_root,omitempty"`
}
