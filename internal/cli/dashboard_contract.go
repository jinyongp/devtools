package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/dashboard"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/protocol"
)

// The HTTP consumer uses the same parser, selection, paging and action binder.
// Its capability is restricted to registered collection queries.
func (a *App) DashboardContracts() dashboard.Contracts {
	return dashboard.Contracts{ResolveAction: a.ResolveAction, Collection: func(ctx context.Context, command, data, cache string, options map[string][]string) (map[string]any, *protocol.Error) {
		if collectionFor(command) == nil {
			return nil, argumentError("Select a collection query.", "")
		}
		bridge := New("dashboard", "")
		dirs, err := paths.Current()
		if err != nil {
			return nil, argumentError("Cannot resolve directories.", "")
		}
		if dirs.Data != data {
			dirs.IsolatedRoot = ""
		}
		dirs.Data, dirs.Cache = data, cache
		bridge.userPaths = func() (paths.Directories, error) { return dirs, nil }
		bridge.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(data, "profiles"), nil }
		args := append([]string{"--format", "json"}, strings.Fields(command)...)
		keys := []string{}
		for key := range options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "query-cache-dir" {
				return nil, argumentError("Dashboard query cache is selected by the server.", command)
			}
			for _, value := range options[key] {
				args = append(args, "--"+key+"="+value)
			}
		}
		var out, diagnostic bytes.Buffer
		code := bridge.Run(ctx, args, IO{Out: &out, Err: &diagnostic, In: strings.NewReader("")})
		var envelope struct {
			Data  map[string]any  `json:"data"`
			Error *protocol.Error `json:"error"`
		}
		raw := out.Bytes()
		if code != 0 {
			raw = diagnostic.Bytes()
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&envelope); err != nil {
			return nil, protocol.NewError("internal_error", "Cannot encode collection response.", 1, nil)
		}
		if code != 0 {
			if envelope.Error == nil {
				return nil, protocol.NewError("internal_error", "Collection query failed without a diagnostic.", 1, nil)
			}
			envelope.Error.ExitCode = code
			return nil, envelope.Error
		}
		return envelope.Data, nil
	}}
}
