package cli

func boolSchema() map[string]any { return map[string]any{"type": "boolean"} }
func arraySchema(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": item}
}
func routeRefSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		object(map[string]any{"scope": map[string]any{"const": "project"}, "profile": stringSchema(), "instance_id": map[string]any{"type": "string", "pattern": "^[0-9a-f]{32}$"}, "proxy": stringSchema()}, "scope", "profile", "instance_id", "proxy"),
		object(map[string]any{"scope": map[string]any{"const": "global"}, "route_id": map[string]any{"type": "string", "pattern": uuidPattern}}, "scope", "route_id"),
	}}
}
func routeEndpointSchema() map[string]any {
	return object(map[string]any{"route_ref": routeRefSchema(), "host": stringSchema(), "listener": stringSchema(), "port": map[string]any{"type": "integer", "minimum": 0, "maximum": 65535}, "url": stringSchema(), "route_url": stringSchema(), "status": stringSchema()}, "route_ref", "host", "listener", "port", "url", "route_url", "status")
}
func listenerDefinitionSchema() map[string]any {
	f := map[string]any{}
	for _, key := range []string{"id", "name", "http_reservation", "https_reservation", "http_host", "https_host", "generation"} {
		f[key] = stringSchema()
	}
	f["legacy_loopback"] = boolSchema()
	return object(f, "id", "name", "generation")
}
func mutationSchema(fields map[string]any, required ...string) map[string]any {
	fields["changed"] = boolSchema()
	fields["replayed"] = boolSchema()
	return object(fields, append(required, "changed", "replayed")...)
}
func caStatusSchema() map[string]any {
	return object(map[string]any{"present": boolSchema(), "fingerprint": stringSchema(), "not_after": map[string]any{"type": []string{"string", "null"}, "format": "date-time"}}, "present", "fingerprint", "not_after")
}
func proxyEnvironmentSchema() map[string]any {
	s := publicEnvSchema()
	f := s["properties"].(map[string]any)
	f["item"] = routeEndpointSchema()
	f["listener"] = map[string]any{"oneOf": []any{proxyStatusSchema(), map[string]any{"type": "null"}}}
	f["upstream_ready"] = boolSchema()
	return s
}

func globalRouteSchema() map[string]any {
	f := map[string]any{}
	for _, key := range []string{"id", "name", "host", "reservation", "listener", "dns"} {
		f[key] = stringSchema()
	}
	return object(f, "id", "name", "host", "reservation", "listener", "dns")
}
func routeMutationSchema(action string) map[string]any {
	if action == "add" {
		return mutationSchema(map[string]any{"item": globalRouteSchema()}, "item")
	}
	if action == "rm" || action == "clear" {
		return mutationSchema(map[string]any{"items": arraySchema(globalRouteSchema())}, "items")
	}
	var single map[string]any
	if action == "up" {
		single = mutationSchema(map[string]any{"item": object(map[string]any{"route_ref": routeRefSchema(), "active": boolSchema(), "host": stringSchema(), "listener": stringSchema(), "dns": stringSchema()}, "route_ref", "active", "host", "listener", "dns")}, "item")
	} else {
		single = mutationSchema(map[string]any{"route_ref": routeRefSchema()}, "route_ref")
	}
	return map[string]any{"oneOf": []any{single, mutationSchema(map[string]any{"items": arraySchema(single)}, "items")}}
}

func networkDiagnosticsSchema() map[string]any {
	remedy := object(map[string]any{"argv": arraySchema(stringSchema()), "required_inputs": arraySchema(stringSchema()), "message": stringSchema()}, "argv", "required_inputs", "message")
	return diagnosticsSectionSchema(map[string]any{"listeners": arraySchema(proxyStatusSchema()), "routes": arraySchema(proxyItemSchema()), "exposures": arraySchema(exposureSchema()), "ca": caStatusSchema(), "trusted": boolSchema(), "setup": map[string]any{"type": "object"}, "hosts": map[string]any{"type": "object"}, "isolated": boolSchema(), "boundaries": arraySchema(stringSchema()), "remedies": arraySchema(remedy), "pending_operations": map[string]any{"type": []string{"array", "null"}, "items": stringSchema()}}, "listeners", "routes", "exposures", "ca", "trusted", "setup", "hosts", "isolated", "boundaries", "remedies", "pending_operations")
}
