# Local proxy, HTTPS and sharing

Use `proxy` when worktrees need stable `.localhost` hostnames instead of direct
port URLs. Routes come from each instance's tracked `[proxies.NAME]` declaration
and current port assignment. When a route uses `${instance.alias}`, give every
routed instance an alias with `instance name`. Allocate the referenced service
port, then inspect `proxy list` before starting the user-global daemon. Do not
start one daemon per project or worktree; route, alias, and assignment changes
are picked up on the next request.

## Listeners and integrated preparation

`proxy start` and `proxy stop` require a request UUID. Reuse a UUID with identical
input only after an uncertain response. The listener port remains reserved after
stop; changing it requires starting the stopped daemon with an explicit new port.
Treat non-`ready` list entries as diagnostics to resolve, not fallback targets.
The default listener remains loopback HTTP on port 20200. Configure named HTTP/HTTPS
groups with `proxy configure`; omitted `listener` selects the group named `default`.
Use `proxy status --all` to distinguish the daemon from each listener. Start/stop selects
one group. Configure while stopped; active exposure pins protect route/listener/port identity.

On protocol 12, `run COMMAND`, `process start COMMAND --request-id UUID`, and
`project up web api --request-id UUID` automatically prepare routes matching served
port keys. Auto preserves explicitly disabled routes before environment expansion.
No matching route or all disabled means ordinary port execution. `--proxy` requires
a match across the selection and reactivates disabled routes. A batch may include
port-only servers and workers. `--no-proxy` / `--proxy=false` skip preparation only;
existing routes/listeners/hosts/sharing and normal binding/value/tool checks remain.
Do not substitute direct URLs for proxy bindings. Both flags together are invalid;
`--no-proxy=false` selects auto. Direct `run -- PROGRAM` never auto-prepares.
Batch preparation
allocates the selected union and resolves binding/DNS/listeners before launching all
children, then waits for readiness. Restart inherits proxy mode and prepares the whole
replacement batch before stopping originals. Partial failure preserves successful siblings.
`project down` stops backend processes; `proxy down ROUTE` deactivates a route until up.
Existing active executions retain their stored mode on omitted start/up and restart.
Use stop/start to convert an off execution. New request UUIDs freeze mode/selection;
old UUIDs replay old semantics. Failed required preparation does not fall back to off.
Queries/doctor and Dashboard starts remain app-only; proxy restarts use the CLI.

## DNS, bindings and external backends

Project routes declare host, port, optional listener and dns. Omitted dns is auto:
`.localhost` needs no hosts change; custom hosts are prepared inside automatic/explicit preparation by the
initiating CLI, which previews owned changes and handles OS authentication. Noninteractive
execution uses `sudo -n` with preauthorized permissions; insufficient permissions return
`permission_required` without a password prompt. `dns=external` uses user-prepared DNS. Queries and daemon
watches never change system files. For owned hosts repair, inspect `proxy hosts
preview ROUTE`; apply its unchanged digest with `proxy hosts apply ROUTE --apply
DIGEST --request-id UUID`. Do not overwrite unrelated hosts entries.

Root env_files resolve relative to the project;
process env > earlier file > later file. `${NAME}` is required; `${NAME:-fallback}` covers
unset/empty. Expansion is single-pass without shell execution. Environment-backed routes
use activation snapshots; file changes require reactivation and shell changes apply on next up.

Port bindings default to numbers; format=url selects loopback HTTP. Proxy bindings default
to route URLs, preferring configured HTTPS. `command env NAME` and `proxy env NAME` read only
public network bindings/pending diagnostics, without allocating, starting or exposing profile
variables, secrets, parent env or secret-derived templates. Known URL, readiness and client
trust are separate. For an already running backend use `proxy add NAME PORT --global --host
HOST`, then `proxy up NAME --global`; this does not own the backend. Projectless `proxy run
NAME --global -- PROGRAM ARG...` claims the exact free port and provides PORT/URL/ROUTE_URL
and normalized peer DEVTOOLS_* keys. Global scope requires explicit --global.

## Backend Host policy

Protocol 11 adds project `proxies.NAME.host_policy = "rewrite" | "preserve"` and
global `proxy add --global --host-policy`. Omission/explicit rewrite retain the
loopback Host; preserve forwards the validated incoming authority, including port
and case. TCP still targets the assigned 127.0.0.1 port. Origin is unchanged.
Sharing inherits the route policy; preserve uses the committed public authority.
Forwarding headers are regenerated, never accepted as authority from the client.
Keep consumer Host/Origin checks enabled and allow only the intended public hostname.
Nuxt DevTools compares authority, including port; no-Origin and scheme behavior are
consumer-specific. Verify a real RPC query and HMR, not just an upgrade or quiet log.

Globals have no policy update command: stop their exposures, down/rm, then add/up
with the intended policy. Recreating a name creates a new route UUID. Project edits
use TOML/config edit, then reactivate environment-backed routes and restart affected
servers if their allowlists/bindings changed. Policy changes revoke old exposure pins.
Network control version 2 rejects old daemons; stop exposures, then explicitly
`proxy restart --all --request-id UUID`. Do not restart the user's daemon unasked.
For downgrade remove preserve from projects, recreate preserve globals as rewrite,
stop all affected exposures and check pins are gone using the new CLI first.
Stopped history is readable by old readers; older than registry-v4 readers still
need their pre-upgrade backup. Do not edit private registry/journal files by hand.

## HTTPS and host setup

HTTPS preparation creates a private host-local CA. `proxy ca status/export` reads public
metadata/certificates; explicit `proxy trust/untrust` changes OS trust.
On macOS, untrust retains a public deny record and deletes the
keychain certificate; explicit trust replaces the deny. Clients can use an
exported CA before OS trust is registered. `proxy setup --check` reads low-port intent;
explicit setup manages minimal installed-executable bind capability on Linux.
On macOS, explicitly use wildcard addresses for unprivileged low ports; specific-address
low ports require privilege, and setup does not grant it. Unshared routes still reject
non-loopback requests. Use high ports when a loopback socket is required.
WSL Linux DNS/trust and Windows browser DNS/trust are separate. After external binary replacement,
missing capability is a recovery action, not a reason to run all of devtools as root.

## Share a route

Sharing requires the user's requested exposure scope. The default `expose start
ROUTE` provider is local and only reads the route URL; external sharing requires
an explicit provider and request UUID. Save the returned exposure ID. Use `expose
list` or `expose status ID` for live state, and `expose stop ID --request-id UUID`
to stop that exposure without shutting down unrelated routes or backends.

`expose start ROUTE --provider lan|cloudflared|tailscale` requires auth from stdin/a private
file or explicit --no-auth. Keep user:password out of argv/TOML/logs/evidence. LAN requires
a reachable explicitly configured listener and client DNS/trust; .local is not automatically
advertised. External providers must already be installed; Tailscale also needs login and
HTTPS. Quick Tunnel does not support SSE. Public state excludes auth, commitments, private
keys and provider ownership records. Live auth stays in daemon memory. After auth loss
ingress stays closed with auth_required: reconcile/stop, then start a new operation.
Source drift/provider failure revokes that exposure's streams and may require recovery.
Tailscale stop removes only the exact owned Serve handler; never reset all Serve config.
Serve updates use LocalAPI ETags; concurrent external edits fail closed. LocalAPI credentials
resolved through the installed CLI remain in memory and never enter provider configuration.

## Isolated operation and teardown

`--isolated-root PATH`/DEVTOOLS_ISOLATED_ROOT separates config/data/cache/runtime and
propagates to daemons/children. Shared hosts/trust/executable capability changes are disabled;
kernel ports and external providers remain shared. High-port localhost HTTPS works with an
explicit public CA. `proxy teardown --listener NAME` previews cleanup; apply the same
selection with --apply DIGEST --request-id UUID. It preserves project/global declarations
and shared CA/trust. Only explicit --all --remove-ca removes CA after reference checks and
confirmed OS untrust; failure preserves keys/recovery metadata. Backup/profile transfer
exclude host-local CA, auth commitments, live exposure and OS state.
