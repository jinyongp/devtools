# Local protection and output

## Initialize once; reuse sessions

- Discover `schema vault status` and `schema vault init`; older builds may not support protection.
- `vault status` is passive. Public-only roots need no vault; protected-root value writes/execution require authentication even for a public profile. No-injection commands keep their behavior.
- Before the first secret, use one-time `vault init` with existing native protection, or explicitly choose `--backend passphrase` on a keyringless host. Isolated roots require passphrase protection.
- Do not install/start a keyring or silently switch backends.
- Native authentication UI requires `--interactive`. Use hidden terminal input or explicit private-file/piped input; never put a passphrase in argv/env.
- Reuse a warm key session across commands/profiles. Do not initialize/unlock for every command.
- Manual lock survives daemon restart; passphrase sessions need explicit unlock after idle expiry. Metadata/diagnostics do not unlock or extend idle.
- Lock cannot revoke values already delivered to apps.

## Recover without replacing authority

- `vault recover` completes interrupted storage transitions after explicit unlock; it verifies existing protection.
- Do not delete key/catalog/recovery files or invent a replacement key.
- Unknown admission/launch stays unresolved until trustworthy recovery evidence exists; PID existence is insufficient.
- Profile transfer passphrases and local vault protection are independent. See [storage.md](storage.md).

## Output policy

- Discover `schema command run` for `output`, `stdout`, `stderr` support.
- `auto`: mask delivered secrets on new non-TTY streams; actual TTY streams are raw. A PTY does not establish a human viewer.
- `redacted`: mask even on TTY. `raw`: explicit exception; choose it only when the task needs it, such as binary output.
- Captured logs are masked before storage on supporting builds, even under raw policy. Historical logs may be unfiltered.
- Application files, network traffic, unknown/encoded tokens and alternate output paths remain outside filtering. Filtering is not isolation.
- Do not bypass redaction failures by silently falling back to raw.
- Stored value edits/lock do not remove an existing execution's masking patterns; retain them through output EOF.
