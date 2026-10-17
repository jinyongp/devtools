package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
)

func readTerminalPassphrase(ctx context.Context, streams IO, prompt string) (string, *protocol.Error) {
	return "", protocol.NewError("unsupported_platform", "Windows console input is not implemented in this intermediate build.", 3, nil)
}
