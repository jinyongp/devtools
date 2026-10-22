# Windows storage identity trace

Use this diagnostic toolkit to capture an intermittent file-identity rejection
without publishing a product release. It builds the pinned v0.29.0 source and the
identity-check candidate in disposable source exports. The normal checkout and
installed CLI are not patched or replaced.

## Build

From the repository root, use Python 3.12+ and the Go version required by the source:

```sh
python3 verify/probes/storage-identity/build.py --output /tmp/storage-trace-build
```

The output directory must not exist. The manifest records source commits, binary
hashes and instrumented source hashes. Copy the four executables, `build.json`, and
`run.ps1` into a new folder under the target project's `.tmp` directory. Keep all
evidence there. `build.py` intentionally generates instrumentation in disposable
exports; no trace code is compiled into normal product builds.

## Capture a query failure

In the execution environment where the query failed, run the script without
elevation or environment changes. For example, after placing the toolkit in
`C:\project\.tmp\storage-trace`:

```powershell
$bundle = 'C:\project\.tmp\storage-trace'
$code = Get-Content -LiteralPath "$bundle\run.ps1" -Raw
& ([scriptblock]::Create($code)) -BundleDirectory $bundle -ProjectDirectory 'C:\project' -Profile 'sample' -ExpectedIsolation unrestricted
```

Use `appcontainer` only when testing that environment. The token check and Known
Folder lookup do not open the storage or run `diagnostics`. Environment mismatch
stops the run. An isolated-root override also stops it without changing that value.

Replace `sample` with the profile selected by the project's `devtools.toml`.
Query mode requires `-Profile`; observation, diagnostics and fixture modes do not.

The instrumented v0.29.0 binary runs two task queries, in order,
without preceding `diagnostics` or candidate queries. It stops after the first
failure. The project must select the intended default profile for `task current`;
the workstream query explicitly selects the supplied `-Profile`. Queries retain the original
CLI's normal lock creation and recovery behavior; this is not a zero-write probe.

If comparison is wanted, add `-CompareCandidate`. On a legacy failure, the script
persists that evidence before running the same failing command with the candidate.
It does not compare the candidate when the legacy failure is not reproduced.

After a failure and candidate comparison have already been captured, use
`-DiagnosticsOnly` to check a newer candidate's diagnostics in the same execution
context without repeating task queries. It verifies the environment, hashes and
candidate version, then runs diagnostics once. Only `ready`, `platform`, `paths`
and `issues` are saved from its data; profile items and other payloads are omitted.
The verdict means results were captured; inspect readiness, errors and limitations.
Package identity alone does not establish whether file paths were redirected.

## Exercise disposable fixtures

Add `-MatrixOnly` to the same command. This performs no default-store query. All
fixtures are created below the evidence folder and removed by the test binary.
Cases cover ordinary and Unicode names, case aliases, available directory and file
8.3 aliases, long nested paths, and case-sensitive directories. Live metadata
handles exercise case aliases and parent renaming, including case-only directory
renaming. Rename cases record handle identity before and after the operation.
Unavailable OS features and renames blocked by Windows are reported as skipped.

The candidate harness also creates a populated fixture using the legacy CLI,
checks that candidate queries preserve its task/workstream IDs and titles, and
compares the fixture's complete data-file set and bytes before and after queries.
It then checks that the legacy CLI can read a candidate-created task. Only these
child processes receive a fixture-specific isolated-root override; the invoking
environment and real stores are unchanged. Fixture hashes and contents are not
written to the evidence report. This tests compatibility, not historical user data.

The controlled outside-root handle is a **negative control**, not an incident
reproduction. It checks rejection and trace capture on both source snapshots.
`fixture_matrix_completed_check_case_results` means the fixture harness finished;
inspect each `STORAGE_MATRIX_CASE` verdict to see whether a real path variant failed.

## Evidence and interpretation

To observe the current default-store view without CLI queries or lock creation,
add `-ObserveOnly` to the invocation. This opens an existing data root and fixed
structural entries (`profiles`, `tasks`, `.maintenance\lock.turnstile`) for metadata.
It does not enumerate entries, read contents or follow reparse points. To compare
an existing physical data root already identified in a trace, also pass
`-ObserveDataRoot 'C:\existing\data'`. This selects a metadata target only; it does
not change CLI storage selection. A missing or rejected root is recorded without
creating or repairing it. `store_view_observed_inspect_metadata` means observation
completed, not that storage is healthy or historical data has been verified.

`trace-*.json` files use unique names. They include token-derived isolation,
selected default-path information, binary identity, exact argv, command outcomes
and separated trace events. They omit task bodies, stored values, raw ACLs, SIDs
and environment dumps. Local paths and file IDs are included; keep the files local
or redact those fields before sharing publicly.

For a legacy containment failure, `exact_compared_root` and `exact_compared_file`
are the strings used by the original predicate. The original case-sensitive
decision and returned error are unchanged. The trace additionally reads metadata
from the pinned root, actual file and expected entry resolved from that root.
It opens existing entries only and rejects intermediate reparse points. Compare
the **expected entry and opened file** IDs; a directory and its child normally have
different IDs. The case-folded comparison is evidence only, never a fallback.

The candidate also records up to 16 `verified_entry` events per process, using the
actual handles from successful identity checks. Compare their requested paths,
final paths and file IDs to identify the files used by a successful query. This
does not establish that another execution context sees the same store or that
expected historical data exists. No file contents or content hashes are recorded.

Trace events can describe errors later handled by callers. A trace alone does not
establish command failure. Generic errors are capped at 64, reserving space for the
target comparison; successful checks have a separate 16-event budget. All logging
is capped at 128 events per process. The last
event of each budget marks that limit. Output failure or unavailable metadata can leave evidence
incomplete. Instrumentation can affect timing, so non-reproduction cannot disprove
the original report or prove a repair. Preserve the store and ACLs; do not reset
them merely to obtain a successful result.
