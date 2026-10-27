package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestStaticContractsRejectBeforeDependencies(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000001"
	cases := []struct {
		command string
		input   map[string]any
	}{
		{"vault rewrap", map[string]any{"from-backend": "passphrase", "backend": "passphrase", "current-passphrase-stdin": true, "new-passphrase-stdin": true, "request-id": id}},
		{"backup restore", map[string]any{"file": "absent", "identity-file": "absent-key", "profile": "app", "apply": strings.Repeat("a", 64)}},
		{"proxy configure", map[string]any{"https-address": "127.0.0.1:9443", "disable-https": true, "request-id": id}},
		{"proxy setup", map[string]any{"check": true, "remove": true}},
		{"proxy setup", map[string]any{}},
		{"proxy teardown", map[string]any{"apply": strings.Repeat("a", 64)}},
		{"proxy teardown", map[string]any{"request-id": id}},
		{"proxy list", map[string]any{"global": true, "profile": "app"}},
		{"proxy status", map[string]any{"all": true, "listener": "default"}},
		{"proxy up", map[string]any{"args": []any{"route"}, "all": true, "request-id": id}},
		{"proxy add", map[string]any{"args": []any{"route", "3000"}, "host": "route.localhost", "request-id": id}},
		{"proxy clear", map[string]any{"global": true, "request-id": id}},
		{"vault init", map[string]any{"backend": "passphrase", "passphrase-file": "absent", "passphrase-stdin": true}},
		{"vault init", map[string]any{"backend": "native", "passphrase-stdin": true}},
		{"task workstream create", map[string]any{"stdin": true, "title": "mixed", "request-id": id}},
		{"task workstream create", map[string]any{"file": "absent", "stdin": true, "request-id": id}},
		{"doctor", map[string]any{"args": []any{"missing"}, "project-only": true}},
		{"doctor", map[string]any{"timeout": "nonsense"}},
		{"proxy start", map[string]any{"port": "65536", "request-id": id}},
	}
	app := testApp(t)
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%02d/%s", i, tc.command), func(t *testing.T) {
			data := outputData(t, app, append([]string{"schema"}, strings.Fields(tc.command)...))
			compiler := jsonschema.NewCompiler()
			if err := compiler.AddResource("urn:static", data["input_schema"]); err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile("urn:static")
			if err != nil {
				t.Fatal(err)
			}
			if schema.Validate(tc.input) == nil {
				t.Fatal("schema accepted invalid combination")
			}
			args := strings.Fields(tc.command)
			for k, v := range tc.input {
				if k == "args" {
					for _, arg := range v.([]any) {
						args = append(args, arg.(string))
					}
					continue
				}
				if flag, ok := v.(bool); ok {
					args = append(args, fmt.Sprintf("--%s=%t", k, flag))
				} else {
					args = append(args, "--"+k, v.(string))
				}
			}
			reader := &observedSecretReader{}
			var out, diagnostic bytes.Buffer
			code := app.Run(context.Background(), args, IO{In: reader, Out: &out, Err: &diagnostic})
			if code != 2 || reader.read || !strings.Contains(diagnostic.String(), "invalid_argument") {
				t.Fatalf("code=%d read=%v %s", code, reader.read, &diagnostic)
			}
		})
	}
}

func TestPortAndDurationContractBoundaries(t *testing.T) {
	app := testApp(t)
	for _, tc := range []struct {
		command, field string
		valid, invalid []string
	}{
		{"proxy start", "port", []string{"1", "9999", "10000", "65535"}, []string{"0", "01", "65536", "99999", "-1"}},
		{"doctor", "timeout", []string{"1ns", "1us", "1µs", "1μs", ".1ms", "+1h2m3.5s", "1.s", "999999999999999999999h"}, []string{"nonsense", "1", "1d", "-1s"}},
	} {
		data := outputData(t, app, append([]string{"schema"}, strings.Fields(tc.command)...))
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("urn:scalar", data["input_schema"]); err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile("urn:scalar")
		if err != nil {
			t.Fatal(err)
		}
		for _, valid := range []bool{true, false} {
			inputs := tc.invalid
			if valid {
				inputs = tc.valid
			}
			for _, value := range inputs {
				input := map[string]any{tc.field: value}
				if tc.command == "proxy start" {
					input["request-id"] = "00000000-0000-4000-8000-000000000001"
				}
				if (schema.Validate(input) == nil) != valid {
					t.Fatalf("%s %q valid=%v", tc.field, value, valid)
				}
			}
		}
	}
}
