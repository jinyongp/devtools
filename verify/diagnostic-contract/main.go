// diagnostic-contract checks captured public failures from installed binaries.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/protocol"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func main() {
	body, err := io.ReadAll(io.LimitReader(os.Stdin, configcontract.Limit+1))
	if err != nil || len(body) > configcontract.Limit {
		fmt.Fprintln(os.Stderr, "invalid diagnostic batch")
		os.Exit(2)
	}
	value, issue := configcontract.Decode(body)
	if len(os.Args) == 2 && os.Args[1] == "--body-schema" && issue == nil {
		checkBodySchema(value)
		return
	}
	items, ok := value.([]any)
	if issue != nil || !ok || len(items) == 0 {
		fmt.Fprintln(os.Stderr, "provide a nonempty diagnostic array")
		os.Exit(2)
	}
	for index, item := range items {
		if issues := configcontract.Validate("Failure", item); len(issues) > 0 {
			fmt.Fprintf(os.Stderr, "diagnostic %d violates Failure: %s\n", index, issues[0].Rule)
			os.Exit(1)
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"validated": len(items), "protocol_version": protocol.ProtocolVersion})
}

// The installed binary supplies the schema; expectations come from the fixture,
// independently of the schema's keywords or the runtime implementation.
func checkBodySchema(value any) {
	batch, ok := value.(map[string]any)
	if !ok {
		os.Exit(2)
	}
	cases, ok := batch["cases"].([]any)
	if !ok || len(cases) == 0 {
		os.Exit(2)
	}
	for _, formats := range []bool{false, true} {
		compiler := jsonschema.NewCompiler()
		if formats {
			compiler.AssertFormat()
		}
		if err := compiler.AddResource("urn:installed-body", batch["schema"]); err != nil {
			os.Exit(2)
		}
		schema, err := compiler.Compile("urn:installed-body")
		if err != nil {
			os.Exit(2)
		}
		for index, raw := range cases {
			row, ok := raw.(map[string]any)
			if !ok {
				os.Exit(2)
			}
			want, ok := row["valid"].(bool)
			if !ok || (schema.Validate(row["value"]) == nil) != want {
				fmt.Fprintf(os.Stderr, "body case %d disagrees with expectation (formats=%v)\n", index, formats)
				os.Exit(1)
			}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"validated": len(cases), "format_assertions": []bool{false, true}})
}
