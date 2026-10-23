# Local URLs, HTTPS and sharing

## Choose the connection

| Need | Use |
| --- | --- |
| Direct port or loopback URL | Port bindings; [ports.md](ports.md) |
| Stable worktree hostname | Tracked `[proxies.NAME]` and proxy binding |
| Route to an already running backend | `proxy add NAME PORT --global --host HOST`, then `proxy up NAME --global` |
| Projectless backend launch | `proxy run NAME --global -- PROGRAM ARG...` |
| Read a route URL locally | `expose start ROUTE` (local provider) |
| LAN/public access | Explicit `expose` provider and requested exposure scope |

- Reuse declared routes before adding URL-rewrite scripts, tunnels or per-project daemons.
- Routes use the instance's tracked declaration and current assignment. `${instance.alias}` requires `instance name` for each routed worktree.
- Allocate the service and inspect `proxy list`. Use the user-global daemon, not one daemon per project. Non-ready routes are diagnostics, not fallback targets.
- Port bindings default to numbers (`format = "url"` gives loopback HTTP); proxy bindings default to route URLs, preferring configured HTTPS.
- `command env NAME` / `proxy env NAME` return public bindings and pending diagnostics without allocating/starting or exposing profile values, parent env or secret-derived templates.
- Projectless `proxy run` claims the exact free port and supplies PORT/URL/ROUTE_URL plus normalized peer DEVTOOLS_* keys. Global routes do not own already-running backends; global scope needs explicit `--global`.

## Prepare declared routes

- Check installed version/help/schema before choosing integrated preparation.
- Protocol 12 run/start/up automatically prepare matching served-port routes. No match/all disabled means ordinary execution; auto preserves disabled routes before env expansion.
- Older builds require explicit `--proxy`; do not assume automatic preparation exists.
- `--proxy` requires a matching route across the selection and reactivates disabled routes. Batches may include port-only servers/workers.
- `--no-proxy` / `--proxy=false` skip preparation only, not existing routes/listeners/hosts/sharing or binding/value/tool checks. Do not replace proxy bindings with direct URLs.
- Both flags together are invalid; `--no-proxy=false` selects auto. Put run flags before the command name. Direct `run -- PROGRAM` never auto-prepares.
- Batch preparation allocates the union and resolves bindings/DNS/listeners before launching children, then waits for readiness. Restart prepares replacements before stopping originals; successful siblings survive partial failures.
- Existing executions retain effective proxy mode on omitted start/up and restart. Stop/start is needed to convert an off execution. New UUIDs freeze mode/selection; old ones replay old semantics. Required preparation never falls back to off.
- `project down` stops backends; `proxy down ROUTE` deactivates a route. Queries/doctor and Dashboard starts remain app-only.

## Listeners and DNS

- Default listener: loopback HTTP, port 20200. Configure named HTTP/HTTPS groups with `proxy configure`; omitted listener means `default`.
- `proxy status --all` distinguishes daemon/listeners. Start/stop selects one group and needs a request UUID. Configure while stopped.
- Stopping retains the listener port reservation; change it by starting the stopped daemon with an explicit new port. Active exposure pins protect route/listener/port identity.
- Project routes select host, service port, optional listener and DNS policy. `.localhost` needs no hosts change; `dns = "external"` uses user-prepared DNS.
- Auto custom-host preparation is performed by the initiating CLI, with preview and OS authentication. Noninteractive operation uses preauthorized `sudo -n`; missing permission returns `permission_required`.
- Queries and daemon watches never edit system files. Hosts repair: `proxy hosts preview ROUTE`, then apply the unchanged digest with `proxy hosts apply ROUTE --apply DIGEST --request-id UUID`.
- Preserve unrelated hosts entries. DNS, known URL, backend readiness and client trust are separate outcomes.
- Root `env_files` resolve relative to the project. Precedence: process env → earlier file → later file. `${NAME}` is required; `${NAME:-fallback}` covers unset/empty. Expansion is single-pass, with no shell.
- Environment-backed routes use activation snapshots: file edits require reactivation; shell edits apply on next up.

## Backend Host/Origin compatibility

- Protocol 11 supports `host_policy = "rewrite" | "preserve"`; discover installed support.
- Default rewrite uses loopback Host. Preserve forwards validated incoming authority, including case/port; TCP still targets assigned 127.0.0.1. Origin stays unchanged; forwarding headers are regenerated.
- Sharing inherits route policy and uses committed public authority. Keep consumer Host/Origin checks enabled; allow only intended hostnames.
- Verify real RPC and HMR. Nuxt DevTools checks authority including port; no-Origin/scheme handling is consumer-specific. WebSocket upgrade alone is insufficient.
- Global policy change: stop exposures, down/rm, then add/up with intended policy. Recreating a name creates a new UUID.
- Project policy change: edit declarations, reactivate env-backed routes, restart affected bindings/allowlists. Policy changes revoke exposure pins.
- Network control v2 rejects old daemons. If an upgrade requires restart, stop exposures and use explicitly authorized `proxy restart --all --request-id UUID`.
- Downgrade: remove preserve declarations/recreate preserve globals as rewrite, stop affected exposures and verify pins gone using the new CLI first. Older-than-registry-v4 readers need the pre-upgrade backup; never edit private registry/journal files.

## HTTPS and host setup

- HTTPS creates a host-local private CA. `proxy ca status/export` reads public material; `proxy trust/untrust` changes OS trust.
- macOS untrust retains a public deny record and deletes the keychain certificate; explicit trust replaces the deny. Clients can use an exported CA without OS registration.
- `proxy setup --check` reads low-port intent; explicit Linux setup manages minimal installed-executable bind capability. Missing capability after binary replacement is a recovery action, not a reason to run devtools as root.
- macOS unprivileged low ports need wildcard addresses; specific-address low ports require privilege that setup does not grant. Use high ports for loopback-only sockets. Unshared routes reject non-loopback requests.
- Windows/WSL trust and DNS are separate; see [platforms.md](platforms.md).

## Share and stop

- External sharing needs explicit provider (`lan`, `cloudflared`, `tailscale`) and request UUID. Save the exposure ID; inspect `expose list/status`, stop only that exposure with `expose stop ID --request-id UUID`.
- Supply auth through stdin/private file or explicit `--no-auth`; never credentials in argv/TOML/logs/evidence.
- LAN needs a reachable configured listener and client DNS/trust; `.local` is not automatically advertised.
- External providers must already exist. Tailscale needs login and HTTPS; Quick Tunnel does not support SSE.
- Auth lives in daemon memory. Auth loss closes ingress with `auth_required`; reconcile/stop, then start a new operation. Source drift/provider failure revokes streams.
- Public state excludes auth, commitments, private keys and provider ownership records. LocalAPI credentials remain in memory, never provider configuration.
- Tailscale uses LocalAPI ETags and removes only its exact owned Serve handler; concurrent edits fail closed. Never reset all Serve configuration.

## Isolation and teardown

- `--isolated-root PATH` / `DEVTOOLS_ISOLATED_ROOT` separates config/data/cache/runtime and propagates to children/daemons. Kernel ports/providers remain shared; hosts/trust/executable capability changes are disabled.
- High-port localhost HTTPS can use an explicit public CA.
- `proxy teardown --listener NAME` previews; apply the same selection/digest with a request UUID. Declarations and shared CA/trust remain.
- Only explicit `--all --remove-ca` removes CA after reference checks and confirmed OS untrust. Failure preserves keys/recovery metadata.
- Backups/transfers exclude host-local CA, auth commitments, live exposure and OS state.
