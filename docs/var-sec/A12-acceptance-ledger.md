# A12 acceptance coverage ledger

This ledger maps the accepted report section 21 and workstream U/D/F conditions
to actual implementation checks. Prior A00–A11/N01–N05 reviews remain the
per-unit source/fix history; this is their integration coverage index. Model-only
research and cross-compilation do not substitute for native product execution.

| Condition | Implementation / fixture | Independently checked behavior |
| --- | --- | --- |
| K01–K02 | internal/vault/profile_test.go | NonceFailure, SealedSnapshot: fresh RNG, wrong key/tag/nonce/ciphertext and no partial commit |
| K03–K04 | internal/vault/profile_test.go; internal/vault/bundle_test.go | ManifestAuthenticatesContentMembershipAndIdentity, sealed catalog and missing profile; no expired override fallback |
| K05–K06 | internal/vault/keys_test.go; internal/backup/archive_kdf_limit_test.go | KDFHeaderRejectedBeforeDerivation; real preset/wrong password; archive cost bound before password/KDF |
| K07 | internal/vault/keys_test.go; internal/vault/blob_test.go | KeysetContextAndStableProfileWrapping; protected recovery domain and StorageV1 compatibility |
| K08 | internal/values/vault_migration_test.go; scripts/check-legacy-vault.py | real 89100eb producer, actual old MAC/receipts and synthetic original key/canary scan; no plaintext replacement key |
| K09 | internal/vault/rotation_test.go; internal/vault/rewrap_test.go | actual durable crash boundaries, root continuity, stale epoch/generation admission rejection; no public rotation command invented |
| I01–I02 | internal/vault/ipc_native_unix_test.go; internal/vault/ipc_native_windows_test.go | actual different UID/SID peers, private ACL and local-only production pipe against SMB loopback positive control |
| I03 | internal/vault/session_boundary_test.go; internal/vault/session_order_test.go | foreign transport, fake/stale endpoint and challenge/vault/session mismatch fail before key exchange |
| I04 | internal/vault/session_capacity_test.go | same-user raw wire client actually receives warm key; documented user identity trust boundary |
| I05 | internal/vault/session_capacity_test.go; internal/vault/session_boundary_test.go | actual oversized/malformed IPC, eight admitted connections then prompt rejection/recovery; real concurrent KDF serialized |
| I06 | internal/vault/session_test.go; internal/vault/storage_compat_test.go | protocol mismatch and control-only retirement; session v3 independent of storage/HKDF v1 |
| L01/L06 | internal/vault/session_test.go; internal/vault/session_inspection_test.go | poll/inspect does not touch deadline or unwrap; fake-clock idle boundary separately from actual native cold reopen |
| L02 | verify/scenarios/vault_session.py; verify/wsl_vault.py | two real PTYs, daemon death/manual latch; actual owned distro termination/restart then explicit unlock |
| L03–L05 | internal/vault/launch_test.go; internal/cli/launcher_test.go | lock before admission keeps old; after admission drains actual spawn; real launcher/coordinator/daemon death keeps unknown and lease drain |
| E01 | internal/values/snapshot_test.go; internal/vault/native_session_reuse_test.go | one key access for 10/100/1000 slots; actual native warm reuse plus 32 concurrent uses and explicit cold/lock distinctions |
| E02 | internal/values/snapshot_test.go; internal/values/modern_manage_test.go | atomic batch cannot mix revisions; owned frozen payload remains unchanged after caller/storage changes |
| E03–E06 | internal/cli/launcher_test.go; internal/cli/value_auto_goal_test.go; internal/cli/value_auto_crash_test.go; internal/cli/value_auto_outage_test.go | actual stop barrier, fixed payload, post-stop missing executable/readiness failure, completion response loss, same UUID and unknown; no record-PID termination |
| E07 | internal/execution/snapshot_test.go; internal/execution/bundle_test.go | none/inject false and clean/allowlist parent policy are distinct; explicit env does not override none |
| E08 | internal/execution/environment_windows_test.go; internal/process/input_linux_test.go; internal/process/input_windows_test.go | actual Windows casefold collision before port preparation/live restart; platform size rejection and no argv/file fallback |
| O01–O02 | internal/redact/matcher_test.go | independent randomized reference, every partition/overlap/UTF-8/newline and maximum/short/empty patterns |
| O03–O04 | verify/scenarios/output_redaction.py; verify/scenarios/tty.py | actual non-TTY stdout/stderr, capture before disk; explicit binary stdout keeps stderr protected; real PTY |
| O05–O06 | internal/redact/streams_test.go; internal/redact/matcher_test.go | actual blocked file consumer cancellation/backpressure; bounded memory; error/cancel/EOF never switches to raw |
| O07 | internal/cli/launcher_test.go | old execution keeps original patterns after lock/value replacement; application environment not recalled |
| B01–B02 | internal/vault/forward_test.go; internal/values/vault_migration_test.go; scripts/check-legacy-vault.py | actual process death at every durable encrypted-forward checkpoint, no mixed revision; scan new state/temp/receipts/logs |
| B03 | docs/secret-protection.md; verify/scenarios/vault.py | source dotenv/old backups remain sensitive; no secure-erasure or original-file deletion promise |
| B04 | scripts/check-legacy-vault.py | actual old CLI/dashboard reads/mutations reject protected formats and leave new files unchanged |
| B05 | scripts/check-profile-exchange.py; internal/vault/logical_test.go | four actual native producers/twelve consumers: fresh vault/profile/generation, preserved slots and nonempty bundle IDs/members; session authority not transferred |
| P01 | scripts/check-macos-vault.py; scripts/check-macos-installed-vault.py; docs/var-sec/N01-final-review.md | real Intel/ARM Keychain, installed CLI/update/signature change denial and explicit recovery; creator ACL retained |
| P02 | docs/var-sec/N02-final-review.md; docs/var-sec/N04-final-review.md | actual Windows x64/ARM64 DPAPI/user identity/pipe/private ACL/installer/update/process/browser; browser ARM emulation disclosed |
| P03 | docs/var-sec/N03-final-review.md | actual existing Secret Service, absent/stopped service does not activate, locked collection, PAM logout/relogin; no implicit backend downgrade |
| P04 | docs/var-sec/N04-final-review.md | actual WSL2 owned distro, multiple terminals, manual/unlocked restart paths and explicit passphrase after durable manual lock |
| U01 | internal/values/modern_test.go; internal/values/public_integrity_test.go | vault-free public-only, sticky sealed, protected public unsigned manifest=false, passive locked reads and no irrelevant unlock |
| U02 | internal/values/dotenv_test.go; verify/scenarios/vault.py | no sec get/--value, stdin/file only, new dotenv defaults secret, stable kind, UTF-8/NUL; no shell evaluation |
| U03–U04 | internal/cli/vault_test.go; scripts/dashboard-browser.spec.mjs; docs/secret-protection.md | actual hidden input/error/remedy/passive status/schema/completion/diagnostics/browser; raw/TTY/same-user limits |
| D01 | docs/var-sec-contract.md; internal/vault/canonical_test.go | independent canonical vector, private material serialization, bounded formats, lock/resource order and version contracts |
| F01 (A portion) | docs/var-sec/A12-final-performance-results.md | all 7020 timings, matched baseline/current-runtime arms, 18 RSS rows, concurrent/cold/warm, redactor throughput/buffer, native unwrap counts; convergence separately C03 |

The complete native result, local final-source check commands and exact source
applicability are recorded in A12-final-review.md and its structured evidence.
This index alone is not a passing validation. C01–C05/A01 integration belongs to
C03; B03 owns installed bundle/uses/schema acceptance; Z01 owns the whole diff.
Known limits remain same-user trust, replay of complete historical storage,
existing application environments, physical memory erasure, developer signing
identity/notarization and physical-LAN testing beyond the stated fixtures.
