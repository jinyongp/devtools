# Local network contract

This extends the existing port, process, and HTTP proxy contracts on macOS,
Linux, and WSL. Windows browsers connecting to WSL have a separate client
trust/DNS boundary.

## Configuration

Projects use one versionless `devtools.toml`. Listener addresses belong to the
user registry and are not copied into project files.

| Field | Contract |
| --- | --- |
| `env_files` | Root array, default `[]`, paths relative to the configuration directory |
| `ports.NAME.port` | Existing preferred integer or environment expression; omitted/empty/0 environment result selects automatic allocation |
| `ports.NAME.strict` | Existing exact-port policy; incompatible with automatic allocation |
| `proxies.NAME.port` | Project port key |
| `proxies.NAME.host` | Complete hostname; existing reserved templates remain supported |
| `proxies.NAME.listener` | Registered listener name; omitted means `default` |
| `proxies.NAME.dns` | `auto` by default, or `hosts` / `external` |
| `bind.KEY.port` | Numeric binding, or loopback URL with `format = "url"` |
| `bind.KEY.proxy` | Route URL; excludes `port`, `format`, and `template` |
| `bind.KEY.template` | Existing template; excludes typed selectors |

Environment precedence is process environment, earlier files, then later files.
Missing files are ignored; invalid/unreadable files give sanitized file/line errors.
`${NAME}` requires a value; `${NAME:-fallback}` handles unset/empty values. Expansion
is one pass without shell execution. Dotenv files do not inject every value into
application environments. Reserved `${profile}`, `${instance.alias}`, and `${proxy}`
references retain their host-template meaning.

## Registry and identities

The registry is the only source of port numbers. Project backend numbers remain
Assignments; listener/origin/global backend numbers are Reservations. Both collections
share uniqueness under the registry lock. Reads of v1/v2 preserve bytes; the first
new network mutation writes v3 atomically. Older readers/writers reject v3.

Global `proxy add --global NAME PORT --host HOST` creates an exact backend reference
even if an external server is using the port. Project preferred integers retain
non-strict fallback/range behavior. Managed execution separately requires a free
port and runtime claim; it never adopts or kills an external server.

`RouteRef` is project `{profile, instance_id, proxy}` or global `{route_id UUID}`.
Deleting/recreating a global display name creates a new ID. Hosts owners, activation,
receipts, descriptors, exposure pins, bridge dispatch, and teardown use this identity.
Global routes never require synthetic profiles or instances.

## Listener, activation, and DNS

One daemon per data root owns named socket/generation groups. `default` names an
endpoint, not a TLS policy. Existing HTTP 20200 and `proxy start --port` retain their
meaning. HTTPS requires explicit `configure --https-address IP:PORT`. Configure
rejects running listeners. `start/status/stop/restart/logs --listener NAME` default
to `default`; explicit `--all` freezes its selected ID/generation set. Partial bind
failure and stop close only selected sockets/connections, including WebSockets.

Activation precedence is disabled tombstone → explicit active → compatible implicit.
Existing default literal/reserved-template `.localhost` routes with assignments work
without `up`, including after v3 migration. `down` saves a tombstone; `up` clears it.
Restart and TOML removal/readdition preserve tombstones. Project declarations remain
TOML-owned; `add/rm/clear` mutate only globals. Environment-backed activation stores
resolved public fields and private source digests. Source changes need reactivation;
compatible literal routes retain hot reload when DNS preparation is already satisfied.

`dns=auto` leaves `.localhost` untouched and prepares owned loopback hosts entries for
other hosts. Start/up previews and applies expected-digest changes idempotently.
Wildcard listeners map local hostnames to the matching IPv4/IPv6 loopback address,
never to an unspecified address; specific remote listener addresses require external DNS.
Existing identical mappings require no write/elevation. A constrained helper obtains
TTY authentication. Noninteractive execution uses `sudo -n` and succeeds with an
existing authorization or permitting sudo policy; insufficient permissions return
`permission_required` without a password prompt. External mappings and symlinks are protected. Last-owner
removal and rollback affect only owned unused changes. Read-only commands and the
background daemon never write hosts or prompt for OS privileges.

## TLS and permissions

Explicit HTTPS preparation initializes missing private CA material and issues/renews
leaves. Read/status creates nothing. Corrupt/expired roots are preserved and reported,
never silently replaced. Export contains the public certificate/fingerprint only.
Server start requires valid TLS material and actual bind success. OS/browser trust
is separate client readiness: `not_trusted` does not block the server; clients can
supply the public CA. Trust/untrust are explicit CLI operations. Low-port setup is
needed only when actual bind permissions require it, not merely because an intent
record is missing. Capability setup fixes the binary inode/digest and application
children do not inherit capabilities.
On macOS, unprivileged low ports require explicitly configured wildcard addresses;
specific low-port addresses still require privilege. Unshared routes retain their
loopback request guard. Configuration never silently broadens a bind address.
Darwin low-port availability probes disable address reuse on wildcard sockets to detect
occupied specific sockets without requiring privileged specific-address probes.

`--isolated-root PATH` / `DEVTOOLS_ISOLATED_ROOT` selects independent config/data/cache/
runtime and propagates to daemon/children. Explicit flags precede command names and
do not consume child argv. Isolated mode rejects shared hosts/trust/binary-capability
mutations before helpers run. It permits `.localhost`/external DNS and high-port TLS
with a supplied CA. Kernel ports and provider accounts remain shared resources.

## Execution and public environment queries

`run`, `process start`, and `project up` prepare networking only with `--proxy`.
Selected serve keys determine the declared route union; absent serve/route declarations
give `proxy_not_configured`. Candidate snapshots, union allocations, and all bindings
are checked in one temporary registry transition; binding failure saves nothing.
Later DNS/listener failure preserves assigned numbers for retry and rolls back only
new owned unused hosts/sockets. All children launch before readiness waits.

Proxy-mode batch restart prepares the entire candidate snapshot in the initiating CLI
before stopping any frozen original ID. All selected replacements launch before
readiness waits. Receipts preserve original/replacement IDs and launch/readiness phases;
retry does not stop successful replacements. Preparation recognizes original owned
claims. Partial sibling failure preserves successful siblings. Mixed batches use the
recorded per-child modes; all-legacy mode preserves existing order/fingerprints. No
missing command or unselected peer starts implicitly. Project down remains process-only.

`command env COMMAND` / `proxy env NAME [--global]` read public network values without
allocating, starting, initializing CA, or changing activation. Unresolved values are
pending. Inactive routes retain known URLs; listener/DNS/trust/upstream readiness is
separate. Profile vars/secrets, secret-derived templates, and full parent env are excluded.
Global foreground `proxy run --global NAME -- PROGRAM ARG...` needs no project/profile
and prepares the selected route's DNS/listener before exec, holding its exact backend
claim. It injects `PORT` plus peers' `DEVTOOLS_<SERVICE>_PORT/URL/ROUTE_URL`. Names normalize
uppercase/nonalphanumeric-to-underscore; collisions fail before launch. Generated keys
override parent values. Exit releases the claim; explicit down/rm controls route lifetime.

## Exposure, replay, and cleanup

`expose start NAME` selects project scope; `--global` selects global scope explicitly.
The state machine is preparing → starting → active → stopping → stopped;
`auth_required` / `recovery_required` keep ingress closed. Guard/pin precedes provider
start; application dispatch waits for committed public URL/identity. Cloudflared needs
auth or explicit no-auth. Provider installation/login remain external prerequisites.
Tailscale owns only its selected Serve handler. LAN exposes selected routes on an
explicitly reachable listener and does not claim automatic DNS advertisement.

| Event | Result |
| --- | --- |
| Identical UUID/auth/mode | Replay/resume the same identity/generation |
| UUID with changed auth/mode | `request_conflict` |
| Auth lost on daemon restart | Closed ingress, `auth_required`; completed replay does not restore auth |
| Source/pin mismatch or stop | Revoke generation and close only target HTTP/SSE/HTTP2/WS work |
| Provider teardown unconfirmed | Preserve reservation/pin and recoverable metadata |

Auth enters by stdin/private file and remains in daemon memory. A private host-local
HMAC commitment compares retries; raw auth/unkeyed password hashes/commitment keys are
absent from public status, logs, backup, and transfer. A loopback HTTP origin bridge
dispatches a fixed route, rewrites backend Host as before, and supplies public HTTPS
authority/scheme from the committed provider record. Forwarded request headers never
establish trust. Public ingress skips local redirects. Source checks run per request
and at most one second apart for streams, preserving other shared HTTP2 streams.

Lock order: UUID → sorted resource keys → registry. Runtime probes under the registry
lock are nonblocking. OS/provider/IPC work runs outside it; commit rechecks operation,
generation, and digest. Journals precede side effects. Selected teardown preserves
shared CA/trust. Explicit `--all --remove-ca` rechecks surviving TLS listener/active/
pending/recovery references and confirms untrust before key deletion. Failure preserves
keys/recovery data. Inactive declarations alone do not permanently pin the CA. General
backup/transfer excludes host-local live resources.

Tailscale node metadata comes from the installed CLI. Serve configuration mutations
use LocalAPI GET ETags and conditional POST, preserving unknown/unselected fields.
The installed CLI resolves platform socket/credentials; those credentials remain in
memory. Missing conditional-update support or concurrent external changes fail closed.
Stop rechecks the exact owned root handler and preserves sibling paths and ports.

Actual Linux/macOS/WSL, Windows-browser, LAN-peer, and provider evidence is distinct
from injected-helper/unit tests and cross compilation; unexecuted checks stay pending.
