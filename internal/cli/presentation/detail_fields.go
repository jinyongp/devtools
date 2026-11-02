package presentation

// Fixed public field membership reviewed against protocol 22 command schemas.
// Ordering is applied by detailFieldOrder; unknown fields remain in JSON.
var detailFieldMembership = map[string]map[string]string{
	"backup inspect": {
		"":     "item",
		"item": "created_at profiles",
	},
	"backup status": {
		"":     "item",
		"item": "configured directory recipient",
	},
	"command env": {
		"": "env env_keys pending",
	},
	"command explain": {
		"":     "config_path directory item profile provenance revision unresolved",
		"item": "bind env exec inject name output ready requirements selection serve values_update",
	},
	"command inspect": {
		"":     "item profile values",
		"item": "bind env exec inject name output ready requirements selection serve values_update",
	},
	"config inspect": {
		"": "config_path document format revision",
	},
	"dashboard status": {
		"":     "item",
		"item": "running server_id",
	},
	"expose status": {
		"":     "item",
		"item": "auth client_setup created_at generation host id listener origin_port provider reason route_ref serve_port state url",
	},
	"instance show": {
		"":     "item",
		"item": "alias directory instance_id location_status profile",
	},
	"port show": {
		"":     "item",
		"item": "alias directory instance_id location_status name port profile",
	},
	"process request show": {
		"":     "item",
		"item": "request_id result state",
	},
	"process status": {
		"": "item values",
	},
	"profile inspect": {
		"":     "item",
		"item": "active_process_count env_count envs instance_count instances process_count processes profile secrets task_counts tasks values variables",
	},
	"project inspect": {
		"":     "item paths",
		"item": "config_path profile root source",
	},
	"project request show": {
		"":     "item",
		"item": "request_id result state",
	},
	"proxy ca status": {
		"":     "item trusted",
		"item": "fingerprint not_after present",
	},
	"proxy env": {
		"":     "env env_keys item listener pending upstream_ready",
		"item": "host listener port route_ref route_url status url",
	},
	"proxy status": {
		"":     "item items",
		"item": "daemon_running generation https_port id name port reason running started_at state url",
	},
	"values execution status": {
		"":     "item profile",
		"item": "created_at delivery_id delivery_state execution_id execution_state exit_code expires_at external_execution_id external_ref fact_source input_id reason reporter_instance_id revision runner_id runner_instance_id",
	},
	"values inspect": {
		"": "env profile selection values",
	},
	"values request show": {
		"":     "item profile",
		"item": "created_at expires_at request_id result state",
	},
	"values revision": {
		"": "env integrity_verified profile revision",
	},
	"values runner show": {
		"":     "item profile",
		"item": "enabled instance_id protocol redaction_protocol runner_id",
	},
	"variable get": {
		"":         "integrity_verified metadata profile value",
		"metadata": "key kind overrides source",
	},
	"vault status": {
		"":     "item",
		"item": "backend idle_expires_at rewrap state vault_id",
	},
	"version": {
		"": "commit protocol_version version",
	},
}
