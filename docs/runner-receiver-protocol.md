# Implement a Unix runner receiver

Use `values runner address RUNNER_ID --instance INSTANCE_ID` to obtain all
transport metadata needed by a same-user Unix receiver. Implementing this contract
requires no devtools Go imports or knowledge of its storage directories. Follow
[secret automation](secret-automation.md#prepare-and-approve-a-local-execution)
for vault setup, registration, approval, delivery and reconciliation commands.

This page specifies runner protocol 1 and redaction protocol 1 for this checkout.
Older CLI builds can require unpublished transport directories despite reporting
the same protocol versions; upgrade those builds rather than recreating that
storage layout. Command discovery alone does not establish endpoint-only support.

## Obtain and own the endpoint

Choose a canonical lowercase runner UUID and a fresh instance UUID for each
receiver process. Keep the runner UUID when starting a successor. The address
response contains `root_id`, `vault_id`, `runner_id`, `instance_id`, `endpoint`,
`protocol: 1` and `redaction_protocol: 1`. Keep these fields together. Metadata
does not unlock the vault or authorize execution.

Address lookup is passive: it computes metadata without creating transport
directories, reserving a listener or changing a registration. The receiver owns
endpoint preparation and cleanup; neither registration nor delivery prepares it.

On Linux and macOS:

1. Use the exact returned `endpoint` as an absolute Unix stream socket path.
   Long paths may resolve to a short private directory under the canonical system
   `/tmp` path. Treat the endpoint as opaque; do not derive it from IDs or create
   a parallel directory below the devtools data root.
2. Create its missing parent with mode `0700`, owned by the current UID. Use
   canonical paths without symlink components. Require that same owner and exact
   mode on an existing parent, with no special mode bits; do not repair it.
3. Bind without unlinking an existing path. A socket already present, including
   an apparently stale socket, is a conflict. A probe followed by unlink is unsafe
   because another receiver can bind between those operations. Do not replace
   regular files or symlinks either. Resolve ownership and old jobs before using
   a fresh instance UUID and its newly computed endpoint.
4. Set the socket to mode `0600`, owned by the current UID, before registering.
   Both the parent and socket must retain their identities during connection.
5. On shutdown, close the listener and unlink only its own socket when the named
   socket and parent still have their original identities and owner. Remove the
   endpoint parent only when it is still owned, unchanged and empty. Never remove
   a successor's path or recursively delete an endpoint directory.

Authenticate the connected peer's kernel UID before exchanging any protocol
bytes. Linux uses `SO_PEERCRED`; macOS uses `LOCAL_PEERCRED`. Require the current
UID in both directions. Filesystem ownership checks do not replace peer checks.
The sender checks the actual endpoint parent and socket; it does not require an
additional transport directory. Validation failure leaves existing objects intact.

Windows uses named pipes, SID checks and private ACLs instead of Unix filesystem
parents. Its reference listener retains its existing storage setup. This page's
endpoint-only preparation procedure does not specify an independent Windows
receiver. Native Windows and WSL retain separate stores and identity boundaries.

## Frame and identity rules

Each connection carries one probe or delivery exchange. Prefix every JSON frame
with its byte length as an unsigned 32-bit big-endian integer. Read the complete
length and body, handling partial reads and writes. Reject zero-length frames and
lengths above the applicable limit before allocating the body: control frames are
at most 128 KiB and the encoded private payload is at most 16 MiB.

Use UTF-8 JSON objects with the exact lowercase field names below. Reject unknown
or duplicate fields, nulls, malformed UTF-8 and trailing JSON data. All listed
fields are required. Empty `values` and `patterns` arrays use `[]`.

Identifiers and challenges have these forms:

| Fields | Form |
| --- | --- |
| `root_id` | 64 lowercase hexadecimal characters, copied from address metadata. |
| `vault_id`, `execution_id`, `input_id`, `delivery_id` | 32 lowercase hexadecimal characters. Copy the expected identity rather than generating a substitute. |
| `runner_id`, `instance_id` | Canonical lowercase UUIDs from address metadata. |
| `server_challenge`, `client_challenge` | Fresh unpredictable 16-byte values encoded as 32 lowercase hexadecimal characters, generated for this connection by their respective peers. |
| `external_execution_id` | The caller's exact job identity: 1–128 printable ASCII bytes (`0x20`–`0x7e`); never put a secret here. |

The receiver sends `hello` first. The sender validates protocol and every metadata
identity before sending a probe or bind. The receiver validates the echoed server
challenge, client challenge and all identities before replying. Every subsequent
response repeats both challenges exactly. Close on a mismatch, malformed frame,
unsupported protocol or canceled operation. Use bounded handshake timeouts.

## Registration and approval probe

Registration and a new approval each open a connection and use this exchange.
Every frame has integer `protocol: 1`; `type` is the literal in the first column.

| Direction and type | Remaining fields |
| --- | --- |
| Receiver → sender, `hello` | `root_id`, `vault_id`, `runner_id`, `instance_id`, `server_challenge` |
| Sender → receiver, `probe` | `root_id`, `vault_id`, `runner_id`, `instance_id`, `server_challenge`, `client_challenge` |
| Receiver → sender, `registered` | `server_challenge`, `client_challenge`, integer `redaction_protocol: 1` |

A successful probe grants no delivery authority and carries no values or patterns.
An identical successful registration request replays its receipt without probing
again; receipt replay does not establish current receiver health.

## Approved delivery

Delivery opens a new connection with a fresh `hello`, then uses these frames.
Every frame again has integer `protocol: 1`.

| Direction and type | Remaining fields |
| --- | --- |
| Sender → receiver, `bind` | `root_id`, `vault_id`, `runner_id`, `instance_id`, `server_challenge`, `client_challenge`, `execution_id`, `input_id`, `external_execution_id` |
| Receiver → sender, `ready` | `server_challenge`, `client_challenge`, integer `redaction_protocol: 1` |
| Sender → receiver, `payload` | `execution_id`, `input_id`, `delivery_id`, `external_execution_id`, `values`, `redaction` |
| Receiver → sender, `accepted` | `server_challenge`, `client_challenge`, `execution_id`, `input_id`, `delivery_id`, integer `redaction_protocol: 1` |

Validate the bind and admit the exact execution/input/job before sending `ready`.
The payload's execution, input and job identities must match the bind. Keep its
delivery identity for the ACK; a receiver does not choose a replacement ID.

`values` is an array of objects with exactly `key`, `kind` and `value`:

- `key` matches `^[A-Za-z_][A-Za-z0-9_]*$`; keys are unique, with at most 5,000
  entries.
- `kind` is `variable` or `secret`.
- `value` is an exact UTF-8 string without NUL. Preserve its whitespace and bytes.

`redaction` is an object with integer `protocol: 1` and `patterns`, an array of
standard base64 strings representing raw pattern bytes. It contains each nonempty
secret value in the same order as `values`, excluding variables and empty secrets.
Validate that correspondence. Patterns are sensitive even though their wire
encoding is base64; keep them and values out of ordinary JSON, logs and errors.

Install literal-byte and Go JSON-string-body masking for both output streams
before acknowledging acceptance or producing any application output. Match across
write boundaries and retain the received patterns until both streams reach EOF,
including after vault lock or value changes. Fail closed if the matcher cannot be
installed: limits are 4,096 normalized patterns, 64 KiB per pattern, 4 MiB of
normalized pattern bytes, plus the matcher state limits described in
[output protection](output-protection.md). There is no raw-output fallback.

Send `accepted` only after accepting the input and installing its log protection.
It confirms acceptance, not application start or completion. A cooperating engine
should durably reserve its job identity before ACK and provide lookup for
reconciliation. The receiver never receives the vault master key.

## Lost responses and recovery

The sender commits durable send intent before sending the private payload and
does not send that input again. A partial send, sender death or missing/invalid
ACK after intent leaves the outcome unknown. Reusing the request UUID or choosing
a new UUID cannot authorize a replacement send.

This deduplication scope depends on the original data root and its retained
execution/request history. Deleting the root or restoring an older complete root
ends that scope; it cannot establish that an external job never ran.

Use the original job identity to query the engine, then report verified facts
through the CLI. Do not infer that no job ran from an absent ACK or recreate an
execution merely to obtain a successful response. Follow
[uncertain delivery recovery](secret-automation.md#recover-an-uncertain-delivery-or-execution)
for request replay, status and reporting rules.
