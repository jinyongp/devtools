# N05 native execution / migration acceptance

Reviewed exact source `574c44a798c379e1b2c406fb3375aba2f8ea2385` in
[nonpublishing run 39272297952](https://github.com/jinyongp/devtools/actions/runs/39272297952).
All 26 required checks passed; three publication jobs were skipped. Evidence is
[N05-native-574c44a.json](evidence/N05-native-574c44a.json): exact-source job IDs,
raw artifact digests, named Windows test events, four installed legacy results
and all 12 native exchange directions with direct new-identity assertions.

| Requirement | Required concrete evidence |
| --- | --- |
| L03/E03 before admission | TestFrozenLauncherRealProcessRestartAndLock rejects locked snapshot without stopping old app; public managed replacement generation fails before old stop. |
| L04/E03 after admission | Actual old-stop file barrier, lock intent persisted, acknowledgement waits for real spawn completion, captured values/config survive changes after preparation. |
| L05/coordinator + launcher death | TestLaunchRealProcessCrashRetainsUnknown, TestLaunchRealDaemonCrashDuringLockDrainsAcrossReplacement: real OS process kills, replacement daemon observes durable manual intent and live launch gate, unknown never assumed canceled. |
| Client death | TestRestartRealClientCrashBeforeAdmissionAndDuringOldStop/prepared and /old-stop: actual client kill, old app preserved before admission; canceled launcher doesn't start after death; confirmed old stop reported outage; identical UUID doesn't duplicate execution. |
| Lost stop response | TestRestartPreservesStopMutationWhenColdStartPreflightFails/accepted_stop_response_lost: actual HTTP cancellation after durable accepted stop; changed result retained across failed restart preflight. |
| E04 spawn failure | Disposable executable removed after old stop, outage correctly reported; later file recovery doesn't turn same UUID into another run. New late-stop outage regression separately covered. |
| E05 request retry | Original execution and target IDs retained in frozen launcher, services receipt and project batch/partial failure tests; completed siblings not repeated. |
| E06 unknown/stale ownership | Actual OS leases and inherited handle/file identity tests; supervisor RPC checks original execution ID/token; no record-PID fallback. Native Windows supervisor crash/readiness/tree cleanup tests use owned handles. No forced kernel PID-number recycling claim. |
| K08/B01 migration crash | TestEncryptedForwardRecoveryAtEveryDurableBoundary: 9 actual separate-process exits at durable checkpoints, old state preserved before intent, afterward decrypt/recover complete after-images, wrong key fails, completion-response replay, conflict on changed request input, canary scan. |
| K09 rewrap crash | TestRewrapRecoveryAtDurableBoundaries: 6 actual separate-process exits, same random root after retry, installed-new-key verification precedes old item removal; failure retains recoverable state/manual latch, no raw root/passphrase in files. No power-failure claim. |
| K08 legacy MAC / receipts | Actual fixed baseline 89100eb CLI + dashboard generate secret receipt/MAC. Current migration then later secret change; same old UUID via new dashboard replays original result without rollback. Scan actual old 32-byte MAC and secret prefix over all migrated data. |
| B04 actual old access | Positive old dashboard metadata + mutation first; after migration actual old CLI read/write/new profile and old dashboard read/write fail storage_error with JSON state unchanged. Current CLI replaces incompatible server, old session gets 401, new session reads metadata. |
| Restore integrity / preview | Actual age archive wrong identity and corruption fail, stale and concurrent goal changes rejected, launch lease drains before publication, same request retry, later mutation + re-encryption preserves receipt replay, public restore cannot downgrade sealed history. |
| B02 own data/output | Actual installed masked stdout/stderr and captured logs plus old running app's old patterns after manual lock/new stored value. New data/temp/journal/receipt scans and native legacy script full data scan after owned worker drain. Explicit app observation/source input fixtures outside protected tree remain synthetic sensitive originals. |
| B03 originals | original.env preserved and explicitly still sensitive; no deletion or secure erase promise for user originals/old backups. |
| B05 cross-device | Four actual native producers, all 12 different-source consumers pass; fresh target profile/storage/key identities, logical ValueIDs/slot versions preserved and no live session authority reused. Nonempty bundle/native membership coverage belongs to B03. |

Runtime fix e0cfd3d reconciles confirmed outage on lost-launch recovery, original
execution retry and passive status. Tests first reproduced outage=false after real
client death. Fixture correction a476cd0 canonicalizes only new macOS test data
path; product O_NOFOLLOW policy stays. Last verifier extension 2eb9fb7 adds actual
old secret receipt replay and MAC scan. Each has a reviewed logical commit.

## Final review and applicability

All table areas reviewed. No unresolved eligible N05 finding remains. Actual
Windows x64/ARM64 suites contain all 20 required named fault/regression tests,
including their real-process child cases. Linux/macOS source CI full race checks,
Mac Intel/ARM Keychain, Linux Secret Service and WSL restart gates all passed.

Actual old CLI/dashboard fixtures on four native hosts passed both old read and
write denials, incompatible dashboard replacement and old-session rejection,
actual secret receipt replay after a later value mutation without rollback, old
MAC/secret canary scan, source-original preservation and owned fixture cleanup.
All 12 native consumers explicitly assert fresh target vault/profile/generation
and preserved logical slot versions. Earlier green runs without these last
assertions were not substituted for this final result.

The Unix built fixture CLI reports commit unknown/version dev; that fact is kept
in the evidence, alongside fixed source checkout, job identity and binary digest.
Windows installed products report the exact candidate revision. This acceptance
is for N05's source. Subsequent C01 consumer/C02 changes, performance gate A12,
B03/C03 product integration and Z01 whole-change review remain separate gates.
No release was published and no real user vault was migrated by these fixtures.
