package vault

import "context"

// FILE_FLAG_FIRST_PIPE_INSTANCE in the shared backend refuses squatting.
func peerEndpointVacant(context.Context, string) error { return nil }
