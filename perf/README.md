# Measuring devtools performance

[Documentation index](../docs/README.md) · [Development](../docs/development.md)

Run the end-to-end suite to find slow commands and compare changes under the same
conditions. It uses [hyperfine](https://github.com/sharkdp/hyperfine) for repeated
measurements and creates disposable project data for every fixture size. Your
existing profiles, tasks, servers and configuration stay outside these fixtures.

The end-to-end runner currently supports Linux and WSL. It requires Python 3.10+
with its standard library, Go from `go.mod`, hyperfine 1.20+, curl, Git and the usual
Unix `true` and `printf` programs. Project commands live in `devtools.toml`.
Start from a shell with your development toolchain loaded, then select this
checkout's CLI:

```sh
go build -o bin/devtools ./cmd/devtools
export PATH="$PWD/bin:$PATH"
```
The runner looks for hyperfine on `PATH`, then in the ignored `bin/hyperfine`
location; `--hyperfine` can select another executable.

## Start with the whole suite

```sh
devtools run perf:all --list
devtools run perf:all
```

`devtools run perf:all` builds the current CLI and measures all groups serially at 10, 100 and
1,000 live tasks and variable keys. Each case has three warmup runs and ten timed
runs. There is also one draft workstream with specification and plan bodies.
The default body size is 1 KiB and there are no additional historical updates.
Builds, fixture generation, initial authentication and state restoration happen
outside the timed command. A full run includes managed daemon lifecycles and all
canonical commands' help paths in the measured CLI; allow several minutes.
The discovery count follows the measured binary's schema automatically.

For a quick check of every group:

```sh
devtools run perf:all --sizes 10 --runs 3 --warmup 1
```

Results go to a new `perf-results/<timestamp>/` directory, ignored by Git:

- `summary.md` shows mean, p95 and standard deviation in milliseconds.
- `summary.csv` provides the same statistics in seconds for spreadsheets.
- `summary.json` contains the samples, binary hash/version, source commit,
  dirty-worktree status and measurement environment.
- Numbered JSON files preserve hyperfine's raw results; matching logs preserve
  its diagnostics.
- `coverage.json` distinguishes actual runtime command cases from help discovery.
  `catalog.json` records the complete command schema of the measured binary.

A failed command, timeout or incomplete sample set fails the run. Completed
samples remain available, and the summary records that the run is incomplete.
Cleanup uses the isolated daemons' control channels before deleting their data.
If cleanup fails, the runner retains the fixture and prints its path.

## Narrow down a slow area

```sh
devtools run perf:all tasks values --sizes 10,100,1000
devtools run perf:all tasks --case workstream --body-kib 1,64,256 --sizes 10
devtools run perf:all tasks --case context --sizes 1 --histories 1000,10000,100000
devtools run perf:all process proxy dashboard --sizes 10
devtools run perf:all discovery --sizes 10
devtools run perf:all tasks --case workstream --sizes 10,100 --attach-tasks \
  --completed-workstreams 0,10,100 --body-kib 1,64,256
```

Groups are `cli`, `project`, `values`, `tasks`, `backup`, `process`, `proxy`,
`dashboard` and `discovery`. `--case` selects names containing the given text.
Sizes, extra-history counts, body sizes and completed-workstream counts form a
Cartesian product. `--completed-workstreams` retains that many synthetic closed
workstreams, each with spec and plan bodies and no child tasks. The selected
workstream stays draft. Document bodies are distinct across documents and
workstreams, while their lengths remain fixed. `--attach-tasks` attaches the live
tasks to it with short descriptions, so `--body-kib` varies document size without
also multiplying task description size. Without this flag, live tasks remain independent and their
descriptions also use `--body-kib`.

Hold the live size fixed when investigating history growth; measure live-data growth
separately. Large history with many live tasks also increases fixture setup cost.
Extra history is imported in batches of 200 updates. This varies history volume
with fixed live data, rather than accumulating live claims and retry receipts.

Current workstream detail/context queries assess the selected workstream and
its prerequisites; edit previews use current state rather than replaying the
entire history. Current reads still decode the profile-wide storage snapshot,
so retained workstreams and document bodies can increase latency. Measure
completed-workstream growth separately from event-history growth.

New checkpoints store large document bodies once in a snapshot-only table,
including bodies referenced by recent canonical edit events. Existing snapshots
remain readable and acquire this encoding at the next normal checkpoint. The
WAL and public history retain the full document text. Older CLI versions can
recover from the WAL when they do not recognize this disposable snapshot format.

Read cases reuse their fixture during warmup and sampling, including storage and
Dashboard caches. Mutation cases restore the baseline before **every** warmup
and timed run, removing prior changes and request receipts. The same request UUID
therefore performs a fresh mutation each time. Managed lifecycle cases also stop
their owned services after each run. Preparation and cleanup are excluded from
the reported duration. These cases measure execution, JSON/output generation and
process startup using `--shell=none --output=pipe`.

Dashboard and proxy HTTP cases include curl startup and the response transfer.
They measure the local server path; browser rendering and interaction require a
separate browser performance investigation. The proxy HTTP fixture has one
registered project and backend. Network updates, interactive editors, migrations
and several destructive lifecycle variants currently have help-discovery coverage
rather than runtime cases. The coverage file makes these gaps visible.

## Compare a candidate with a baseline

```sh
devtools run perf:all tasks --sizes 100 --output /tmp/devtools-baseline
# Make a change, then rerun the same sampling configuration.
devtools run perf:all tasks --sizes 100 --output /tmp/devtools-candidate \
  --compare /tmp/devtools-baseline/summary.json
```

A positive percentage means the candidate is slower. Cases match by name, live
size, extra history and body size. The runner rejects incomplete baselines or
changes in CPU, OS, architecture, kernel, hyperfine version, run count or warmup
count. Use the same storage device and quiet host for both runs. Filesystem cache
state and background activity can still affect results; a small delta with high
variance is a reason to collect more samples, rather than declare a regression.
With ten samples, p95 is effectively the slowest observed run.

To measure an existing release or a specific hyperfine executable:

```sh
devtools run perf:all --binary /absolute/path/to/devtools \
  --hyperfine /absolute/path/to/hyperfine --sizes 10
```

Fixture generation uses the current source's storage format. An older binary must
support that format; incompatible binaries fail during setup or preflight.
`--timeout` limits each hyperfine invocation, including samples and hooks (300
seconds by default). Initial setup and preflight commands have their own timeouts.
Choose a fresh `--output` directory for every run.

## Diagnose the cost inside a command

```sh
devtools run perf:go
devtools run perf:go --benchtime 10x --runs 5 --output /tmp/devtools-go-perf
```

This runs the existing Go benchmarks with allocation reporting and saves
`go-benchmarks.txt`. It covers fixed-cardinality task histories of 1k/10k/100k
events, mutations, cursor pagination, large workstream bodies, lifecycle replay,
1/50/200-project proxy routing and bounded log I/O. Go's benchmark timer excludes
its declared fixture setup; Go results are distinct from hyperfine end-to-end
timings. The suite runs packages serially without the race detector to avoid
instrumentation and package contention. Use `devtools run check:perf` for runner regression
tests; performance runs are deliberately separate from correctness checks.

## Add a workload

Add a `Case` in `cases_for()` in `run.py`, including its canonical commands for
coverage. Set `reset=True` for a mutation; use `changed=True` when its response
supports a fresh-change assertion. Declare a service prerequisite for managed
process, proxy or Dashboard cases. Keep the measured argv as the real CLI or HTTP
client invocation, so Python orchestration is outside the measurement. Extend the
fixture with synthetic data and run a small end-to-end check before comparing
performance. Session credentials belong only to the private fixture manifest;
exported command names identify workloads without recording those credentials.

## Protected value fixtures

When the measured binary advertises `vault init`, fixture setup explicitly migrates
the synthetic legacy values into a passphrase-protected vault. Older baseline
binaries keep their legacy fixture. Both contain the same logical public values and
secret. The harness records the selected mode in `metadata.vault_measurement`.

Before copying or replacing fixture storage, the harness stops its services and
retires its own key daemon through the value-free control helper, waiting for the
real daemon lease. It then restores the snapshot and explicitly unlocks a reusable
session before timing the command. Initialization, migration, Argon2 unlock, helper
build and daemon retirement are outside timed work. These samples measure ordinary
commands with an available key session; they do not measure cold unlock latency,
native key-store API calls or the daemon's retained memory.

The `project/run-injected-noop` case runs `true` through direct execution with the
fixture's full profile injected. Unlike configured `noop` (`inject=false`), this
includes value snapshot preparation, admission and output-filter setup even though
the child produces no output. Select it with `project --case run-injected-noop`.

## Measure process memory separately

On Linux/WSL with GNU `/usr/bin/time` and `/proc`, compare the original plaintext
CLI with a protected candidate using independent disposable profiles:

```sh
python3 -B perf/memory.py --baseline /tmp/devtools-baseline \
  --candidate /tmp/devtools-candidate --sizes 10,100,1000 --repeats 3 \
  --output /tmp/devtools-process-memory.json
```

Both binaries must already be built with matched flags. Every profile contains
synthetic secret values. Each size has three independent roots per arm, five warm
CLI runs per root and four concurrent CLI runs. The protected arm also performs
three cold explicit unlock cycles in one owned daemon. Root order alternates per
repeat. Creation, import and cleanup are outside measured runs. The output retains
binary identities, all raw samples and incomplete results after a failure; an
existing output file is never overwritten. Failed retirement retains private data.

GNU time gives the maximum RSS of the CLI and its waited children, not their
summed simultaneous memory. The separate owned daemon's `/proc` VmHWM is its
process high-water mark; three cold cycles do not create three independent peaks.
The recorded wall time includes GNU time startup and cannot be substituted for
the hyperfine CLI boundary. Concurrent CLI peaks are not aggregate RSS. Idle
expiry/manual-lock state and lock acknowledgement time describe cache policy;
RSS after lock cannot establish secret erasure, sensitive-copy lifetime, or the
amount of live secret material. Native unwrap counts are measured by separate
native session tests, and no RSS acceptance threshold is assumed.

## Measure managed value convergence

Use an already-built candidate with vault support on Linux/WSL:

```sh
python3 -B perf/convergence.py --binary bin/devtools \
  --sizes 10,100,1000 --roots 3 --output /tmp/devtools-convergence.json
```

The harness owns isolated passphrase roots, actual CLI/key sessions, managed
Python children and readiness probes. Each dataset has the stated number of
public values plus one public revision and one secret. Three independent roots
per policy each perform three sequential commits; policy order alternates.
The default run budget is 300 seconds; `--budget-seconds` changes it. Cleanup is
outside that budget and uses only private project controls and authenticated
session retirement. Unconfirmed cleanup retains its root. Existing output is
never overwritten; incomplete runs retain completed and partial sample rows.

Notify measures commit-return to actual supervisor pending observation; restart
measures commit-return to applied/readiness and independently observed new child
input. These are different outcomes, not equal-work throughput arms. Notification
must leave the original app alive; its subsequent explicit manual restart is timed
separately. The final opted root also verifies three replacements then a paused
fourth goal and no launch for a fifth goal over 4.5 seconds (more than two
configured reconciliation periods), with the actual watcher observing that goal.

Observations include 50ms polling and CLI query overhead. Setup, explicit unlock
and cleanup are outside the convergence timer. The output keeps binary/harness
identity, source state, hardware, all raw durations/outcomes and storm counts.
Small single-host samples give exploratory distributions, not an SLA or reliable
tail/significance estimate. Original-to-protected baseline timings belong to the
separate matched CLI harness.

## Measure encrypted bulk writes

Use `mutations.py` for the work excluded from the process-memory fixture's timed
region: new secret imports, actual overwrites, same-value imports and previews.
Build baseline and candidate with the same Go version and flags, including
`-buildvcs=false -trimpath`, and set their actual revisions in `main.commit`.
The harness needs Linux/WSL, GNU time, Go (for the owned-session cleanup helper)
and Python 3.11+. Both binaries must support explicit passphrase vault setup.

```sh
python3 -B perf/mutations.py --baseline /tmp/devtools-mutation-before \
  --candidate /tmp/devtools-mutation-after --sizes 100,1000,5000 \
  --repeats 3 --large-repeats 1 --contention-size 4000 \
  --output /tmp/devtools-bulk-mutations.json
```

Every size/repeat/arm gets an independent disposable root and warm passphrase
session. Arm order alternates by repeat. Setup, input generation and cleanup are
outside timing; CLI timing includes GNU time startup. The harness checks import
classification, stored inventory, byte-preserving no-ops/previews and real child
execution. Above 4,096 values it selects one imported value through a bundle for
child validation, preserving the output matcher's existing pattern limit. It verifies session retirement before deleting its fixture. Existing
result files are preserved, and partial rows remain after failure.

Sizes above 1,000 measure new import only. Defaults give three samples for
100/1,000 and one exploratory 5,000-key sample. Cross-profile contention is also
exploratory: a process still running may already be printing its result, so use
`TestPreparedWriteAllowsOtherProfileReadExecutionAndCommit` for deterministic
proof that preparation releases the gate. RSS is separate from allocations and
cannot prove sensitive-copy lifetime or erasure.

Measure actual encrypted overwrite allocations and gate phases separately:

```sh
go test ./internal/values -run '^$' -bench '^BenchmarkModernBatchOverwrite$' \
  -benchmem -benchtime=200ms -count=3
```

This benchmark excludes key initialization and initial seeding. It alternates two
inputs against one fixed profile and includes durable file publication. Phase
metrics report preparation, shared read wait/hold and exclusive commit wait/hold
in ns/op; `B/op` and `allocs/op` cover the whole measured update. The observer is
internal and absent from normal CLI responses. It measures file-gate boundaries,
not just Go mutex/block time. Commit hold still includes full snapshot
verification/serialization and filesystem sync; its cost is not constant in bytes.
