# A12 final runtime performance measurements

Runtime candidate: `f3722b2b408bc94850787eae01b57c58f4787d9a`; plaintext baseline:
`89100eb98888895a450b2ebd403e4e1c9b5f52d2`. Both built with Go 1.27.2 and
matched `-trimpath`/version/commit flags. Linux WSL2 x86_64, Ryzen 9800X3D,
hyperfine 1.20.0. Native OS acceptance is a separate gate; this document records
measurements, not a release approval or performance SLA.

The initial/final-size latency harnesses started clean at f3722b2/70dde46; the
larger tail study and component benchmarks started clean at 4d29330. The latter
commits change only benchmark tooling, documentation and a crash-test launcher,
not the measured runtime. Every record preserves actual binary identity/digest.
No other heavy local project checks were run during the measurements. External
host load was not controlled. Setup, Argon2 unlock, restore, daemon retirement
and helper compilation are outside the hyperfine timed region.

## Matched CLI execution and CRUD

36 CRUD cases per arm (12 operations × 10/100/1,000 public keys plus one secret)
use three warmups and ten samples. Three full-profile `true` execution cases per
arm initially use 50 samples. A separate 1,000-sample-per-size execution study
adds empirical p99; the initial observations are retained, not replaced.
[All 7,020 individual timings and metadata](evidence/A12-final-warm-f3722b2.json).

Full-profile execution includes CLI startup, actual snapshot/materialization,
admission, output protection setup and the child exit. Times below are ms;
p95/p99 are nearest-rank empirical quantiles of 1,000 observations per row.

| Public keys + one secret | Baseline median / p95 / p99 | Protected median / p95 / p99 |
| ---: | ---: | ---: |
| 10 | 7.494 / 8.200 / 8.712 | 21.015 / 38.124 / 88.108 |
| 100 | 7.537 / 8.201 / 8.684 | 21.115 / 22.494 / 23.521 |
| 1,000 | 9.119 / 10.040 / 10.636 | 34.939 / 36.245 / 38.215 |

The ten-key protected tail has 41 observations above 50ms, clustered in part of
the run. Its first/last-half medians are 20.489/22.087ms. The initial 50-sample
run median/p95 was 19.082/20.155ms. The cause is unconfirmed; do not infer a
monotonic key-count relationship, an SLA or statistical significance from this
single-host sequence. The original and encrypted implementations do different
security work; additional cost is not an equal-function optimization comparison.
No verification, durable write, nonce or output-protection rule was weakened.

## Process RSS and all-secret profiles

[Raw rows and exact harness digest](evidence/A12-memory-f3722b2.json).
`perf/memory.py` uses 10/100/1,000 synthetic **secret** values (42 bytes each),
three independent roots/workers per size/arm, five warm CLI runs per root and
four concurrent runs. Thus each CLI RSS row has 15 warm observations; concurrent
samples remain separate. GNU time measures CLI/waited-child maximum RSS, not
aggregate simultaneous memory. The baseline has no key daemon.

| Secret keys | Baseline CLI peak RSS median KiB | Protected CLI peak RSS median KiB | Protected daemon high-water KiB, three independent roots |
| ---: | ---: | ---: | --- |
| 10 | 16,620 | 19,496 | 149,788 / 149,768 / 149,856 |
| 100 | 16,620 | 21,288 | 151,036 / 149,764 / 150,568 |
| 1,000 | 17,388 | 29,704 | 150,184 / 151,456 / 150,500 |

Each daemon's high-water mark follows three cold explicit unlock cycles in that
one process; it is not three independent peaks or live secret size. Argon2's
64MiB workspace and allocator retention contribute to process memory. The
all-secret outer wall medians (including GNU time startup) are 21.800/24.752/
45.808ms in the protected arm; these are not the hyperfine measurement boundary.
Four concurrent CLI peaks are not summed into a fictitious aggregate RSS.

The recorded idle deadline remains approximately eight hours after legitimate
use, and explicit lock returns locked_manual with no idle deadline. Lock CLI
acknowledgement times and post-lock RSS are retained. This describes key-cache
policy; it cannot prove physical erasure, sensitive-copy lifetime, revocation of
an existing application's environment, or zeroization of all Go/OS copies.

## Fresh component measurements

[All raw calibrated estimates](evidence/A12-final-components-f3722b2.json),
count=5, benchtime=100ms. Medians across calibrated batches:

| Component | Median | Allocated B/op | Specific invariant |
| --- | ---: | ---: | --- |
| Actual Argon2id cold explicit unlock | 24.802ms | 67,202,772 | 1 KDF/op |
| Cached key Use/Destroy | 0.119ms | 28,262 | 0 KDF/op |
| Redactor, 10 patterns, 4KiB chunks | 157.13MB/s | 33,120 | backing 32,928B; withhold ≤31B |
| Redactor, 1,000 patterns, 4KiB chunks | 156.01MB/s | 33,120 | backing 32,928B; withhold ≤31B |
| Redactor, max 64KiB pattern, 4KiB chunks | 186.62MB/s | 360,640 | backing 360,448B; withhold ≤65,535B |

Component allocations are not RSS. Batch estimates are not individual request
p95/p99. Matcher construction is measured separately from warm streaming. The
unprotected stream control has different security work and is not a secure
baseline. Fresh measurements supersede use of older component numbers for this
acceptance, while older records remain as history.

Native unwrap request reuse is measured separately with the unchanged actual OS
protector in its bounded helper: one cold unwrap, zero after warm 10/100/1,000
Use batches plus 32 concurrent uses, one after simulated idle expiry, zero while
manually locked, one explicit unlock. These are logical native unwrap requests,
not OS syscall counts, and those use-batch sizes are not CLI key counts. The final
native acceptance evidence must establish actual pass events on each OS.

Review covered both binary identities, matching workloads/flags, complete sample
counts, cleanup, cold/warm/native distinctions, empirical tail variability,
independent peaks and memory/erasure limits. No arbitrary pass threshold or
performance optimization was introduced. Native and whole-stage acceptance
remain separate until their required results are recorded.
