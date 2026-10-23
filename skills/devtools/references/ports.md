# Dynamic ports and application connections

## Use declarations before port-selection code

- Free/dynamic port requested: use `[ports.NAME]` with `strict = false` (the default), optionally a preferred `port` and `range`.
- `serve = ["NAME"]` allocates/reuses the local service assignment at execution.
- `bind` passes that assignment to the app as an environment variable; `${bind.NAME}` also passes it in configured `exec` arguments.
- Use a binding template for URLs. Reuse the same resolved port across server, frontend, desktop app and test consumers.
- Inspect `devtools config schema` for installed field/forms; `port --help` and `instance --help` for assignment operations.

## Minimal wiring

Adapt the command and variable names to the app; do not assume every server reads `PORT`.

```toml
[ports.web]
port = 3000
range = [3000, 3099]
strict = false

[commands.web]
exec = ["pnpm", "exec", "vite", "--port", "${bind.APP_PORT}", "--strictPort"]
serve = ["web"]

[commands.web.bind]
APP_PORT = { port = "web" }
APP_URL = { template = "http://127.0.0.1:${bind.APP_PORT}" }
```

- Devtools chooses the port. Vite's `--strictPort` keeps the app on that resolved port; it does **not** make devtools allocation strict.
- Environment-reading server: keep its normal `exec` and choose its expected variable as the bind key.
- Argument-reading app: put `${bind.NAME}` in configured `exec`. Each array element remains one argument; no shell evaluation or splitting occurs.
- Caller-appended CLI arguments are literal; they do not receive this interpolation.
- Bindings apply even with `inject = false`. Port/proxy bindings do not need bundle membership.
- `${var.KEY}` in a template reads a public profile/env value; that referenced key must belong to the selected bundle. Templates cannot read secrets or chain other template bindings.
- A selected secret with the same name as a bind causes `binding_conflict`; binds overwrite public values with the same name.

## Allocation is not readiness

- Identity: profile + canonical execution directory + service name. Same-profile worktrees share values, but get separate port assignments.
- Inspect `port list/show/check` and `instance list/show` before changing assignments.
- `port` is preferred; `range` bounds automatic choices; `strict = true` requires the configured port for initial allocation.
- Port/strict/range changes affect new assignments; they do not move an existing one. Inspect, stop affected servers, release/reallocate only the intended inactive assignment, then restart its consumers when a move is requested.
- Existing assignments have reuse/conflict rules. If an occupied saved assignment blocks launch, inspect ownership and live executions; do not promise that `strict = false` replaces every stored conflict automatically.
- A stored assignment does not prove a server is running. `port check` observes occupancy/reachability; use application readiness for dependent work.
- Do not let the application silently choose another port after allocation; consumers would retain the wrong address.

## Connect another command or worktree

- A consumer bind can reference an existing assignment without declaring `serve`. Do not declare a second server merely to obtain its address.
- Cross-instance consumers need an existing assignment. Start/allocate the intended server first.
- Cross-profile references require an explicit instance alias/ID; never infer another profile's worktree from the current directory.
- Give a worktree an alias with `instance name`; coordinate consumers when changing aliases or assignments.

```toml
[commands.check.bind]
BACKEND_PORT = { profile = "backend", instance = "main", port = "api" }
BACKEND_URL = { template = "http://127.0.0.1:${bind.BACKEND_PORT}" }
```

- For stable hostnames across port changes, use proxy bindings: [network.md](network.md).
- `command env NAME` can inspect public network bindings on supporting builds without starting or allocating; pending is not a usable address.
- Release only inspected inactive assignments. Prune only the intended missing execution locations; inaccessible is not missing.
- Never kill an arbitrary PID or reclaim an occupied port based only on a stored record.
