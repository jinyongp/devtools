package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netsetup"
	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/truststore"
)

func decodeWindowsOperation(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing operation input")
	}
	return nil
}
func windowsNetworkHelperEntry() bool {
	if len(os.Args) != 3 || os.Args[1] != "__windows-network-helper" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	err := privilege.ServePipe(ctx, os.Args[2], windowsHelperOperation)
	if err != nil {
		os.Exit(1)
	}
	return true
}

func windowsHelperOperation(ctx context.Context, kind string, body json.RawMessage) *protocol.Error {
	invalid := func() *protocol.Error {
		return protocol.NewError("invalid_argument", "Invalid constrained Windows helper request.", 2, nil)
	}
	switch kind {
	case "hosts":
		var request hosts.Request
		if decodeWindowsOperation(body, &request) != nil {
			return invalid()
		}
		return (hosts.System{}).Apply(ctx, request)
	case "trust":
		var request truststore.Request
		if decodeWindowsOperation(body, &request) != nil {
			return invalid()
		}
		if (truststore.System{}).Apply(ctx, request) != nil {
			return protocol.NewError("trust_failed", "Cannot reconcile the selected CurrentUser Root certificate and its private ownership receipt; external certificates and CA keys are preserved.", 3, nil)
		}
		return nil
	case "netsetup":
		var request netsetup.Request
		if decodeWindowsOperation(body, &request) != nil {
			return invalid()
		}
		exe, err := os.Executable()
		if err != nil {
			return invalid()
		}
		return netsetup.SystemApply(ctx, request, exe)
	}
	return invalid()

}
