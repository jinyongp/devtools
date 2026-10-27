// diagnostic-contract checks captured public failures from installed binaries.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/protocol"
)

func main() {
	body, err := io.ReadAll(io.LimitReader(os.Stdin, configcontract.Limit+1))
	if err != nil || len(body) > configcontract.Limit {
		fmt.Fprintln(os.Stderr, "invalid diagnostic batch")
		os.Exit(2)
	}
	value, issue := configcontract.Decode(body)
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
