package cli

import (
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

// Extend only result roots and their declared alternatives/references. Nested
// domain objects and persisted schemas are not invocation presentation data.
func publicResultField(schema map[string]any, name string, definition map[string]any, required bool) {
	if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/$defs/") {
		if defs, ok := schema["$defs"].(map[string]any); ok {
			if target, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any); ok {
				publicResultField(target, name, definition, required)
			}
		}
	}
	if schema["type"] == "object" || schema["properties"] != nil {
		properties, _ := schema["properties"].(map[string]any)
		if properties == nil {
			properties = map[string]any{}
			schema["properties"] = properties
		}
		properties[name] = definition
		if required {
			names := []string{}
			switch values := schema["required"].(type) {
			case []string:
				names = append(names, values...)
			case []any:
				for _, value := range values {
					if key, ok := value.(string); ok {
						names = append(names, key)
					}
				}
			}
			found := false
			for _, key := range names {
				found = found || key == name
			}
			if !found {
				schema["required"] = append(names, name)
			}
		}
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		switch branches := schema[key].(type) {
		case []any:
			for _, value := range branches {
				if branch, ok := value.(map[string]any); ok {
					publicResultField(branch, name, definition, required)
				}
			}
		case []map[string]any:
			for _, branch := range branches {
				publicResultField(branch, name, definition, required)
			}
		}
	}
}

func previewApplyAction(c Command, r Request, out map[string]any) protocol.Remedy {
	argv := append([]string{"devtools"}, strings.Fields(c.Name)...)
	argv = append(argv, r.Args...)
	var required []string
	added := map[string]bool{}
	appendValue := func(name, value string) { argv = append(argv, "--"+name, value); added[name] = true }
	// These are the reviewed public selectors of the 19 preview-capable commands.
	public := map[string]bool{"all": true, "as": true, "dir": true, "file": true, "key": true, "listener": true, "profile": true, "remove-ca": true, "replace": true, "revision": true}
	for _, option := range c.Options {
		name := option.Name
		if name == "request-id" {
			appendValue(name, "REQUEST_UUID")
			required = append(required, "a new request UUID")
			continue
		}
		if name == "dry-run" || name == "apply" || name == "check" || name == "isolated-root" {
			continue
		}
		_, supplied := r.OptionOffsets[name]
		if r.OptionOffsets == nil {
			_, supplied = r.Options[name]
		}
		if !supplied {
			continue
		}
		if name == "identity-file" || name == "passphrase-file" {
			appendValue(name, "PRIVATE_FILE")
			required = append(required, "the same private --"+name+" input")
			continue
		}
		if name == "passphrase-stdin" || name == "stdin" {
			if r.Options[name] == "true" {
				argv = append(argv, "--"+name)
				required = append(required, "the same reviewed --"+name+" input")
			} else {
				argv = append(argv, "--"+name+"=false")
			}
			continue
		}
		if !public[name] {
			required = append(required, "the same --"+name+" selection")
			appendValue(name, "VALUE")
			continue
		}
		if option.Boolean {
			argv = append(argv, "--"+name+"="+r.Options[name])
			added[name] = true
		} else if option.Repeatable {
			for _, value := range r.ListOptions[name] {
				appendValue(name, value)
			}
		} else {
			value := r.Options[name]
			if name == "dir" {
				if path, ok := out["config_path"].(string); ok && path != "" {
					value = filepath.Dir(path)
				}
			}
			if name == "dir" || name == "file" {
				if absolute, err := filepath.Abs(value); err == nil {
					value = absolute
				} else {
					required = append(required, "the same working directory for --"+name)
				}
			}
			appendValue(name, value)
		}
	}
	for _, option := range c.Options {
		if option.Name == "profile" && !added["profile"] {
			if profile, ok := out["profile"].(string); ok && profile != "" {
				appendValue("profile", profile)
			}
		}
		if option.Name == "dir" && !added["dir"] {
			if path, ok := out["config_path"].(string); ok {
				appendValue("dir", filepath.Dir(path))
			}
		}
		if !added["revision"] {
			if option.Name == "revision" {
				if revision, ok := out["revision"].(string); ok && revision != "" {
					appendValue("revision", revision)
				}
			}
		}
	}
	if c.Name == "config migrate" || c.Name == "backup restore" || c.Name == "profile import" || c.Name == "proxy teardown" {
		digest, _ := out["apply_digest"].(string)
		if digest == "" {
			digest, _ = out["digest"].(string)
		}
		if digest == "" {
			if item, ok := out["item"].(map[string]any); ok {
				digest, _ = item["digest"].(string)
			}
		}
		if digest == "" {
			digest = "DIGEST"
			required = append(required, "the matching preview digest")
		}
		appendValue("apply", digest)
	}
	if c.Name == "command add" && len(r.Child) > 0 {
		argv = append(argv, "--", "PROGRAM", "ARGUMENTS...")
		required = append(required, "the reviewed executable and arguments")
	}
	effect := "config_write"
	if strings.HasPrefix(c.Name, "bundle ") || c.Name == "backup restore" || c.Name == "profile import" {
		effect = "value_write"
	}
	if c.Name == "proxy teardown" {
		effect = "system_write"
	}
	message := "Apply the reviewed operation using the same inputs."
	if c.Name == "config fmt" {
		message = "Apply the reviewed formatting."
	}
	return protocol.LocalAction(argv, required, message, effect).Remedy(r.Invocation)
}
