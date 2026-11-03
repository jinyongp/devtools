package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/netsetup"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) registerNetsetup() {
	c := Command{Name: "proxy setup", Description: "Explicitly prepare minimal low-port access, or inspect permission intent without changing the OS.", Options: []Option{{Name: "check", Boolean: true, Description: "Read-only executable and setup intent diagnostics."}, {Name: "remove", Boolean: true, Description: "Remove devtools low-port capability and disable intent."}, requestIDOption(uuidPattern, without(activated("check")))}, Output: map[string]any{"type": "object"}}
	check := object(map[string]any{"mode": stringSchema(), "enabled": boolSchema(), "intent_enabled": boolSchema(), "executable_changed": boolSchema(), "recovery_required": boolSchema(), "bind_check": stringSchema()}, "mode", "enabled", "intent_enabled", "executable_changed", "recovery_required", "bind_check")
	c.Output = map[string]any{"oneOf": []any{itemOutput(check), mutationSchema(map[string]any{"intent_enabled": boolSchema(), "mode": stringSchema()}, "intent_enabled", "mode")}}
	c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		d, e := a.currentPaths()
		if e != nil {
			return nil, portFailure("io_error")
		}
		m := netsetup.Manager{Data: d.Data, Isolated: d.IsolatedRoot != "", Terminal: interactive(streams)}
		if r.Options["check"] == "true" {
			item, e := m.Check()
			return map[string]any{"item": item}, e
		}
		return m.Apply(ctx, r.Options["request-id"], r.Options["remove"] != "true")
	}
	a.commands = append(a.commands, c)
}
