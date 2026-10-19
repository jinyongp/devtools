# C03 managed value convergence results

Actual production runtime: 1e4bebd; committed clean measurement source:
008f91c944643beb72ac4b1c7e51892819838818. [All raw rows and identities](evidence/C03-convergence-008f91c.json)
retain binary/harness digests, exact command, Python/kernel/CPU and source state.
Linux WSL2 x86_64, Ryzen 7 9800X3D. No other heavy local project checks ran during
measurement; external host load was uncontrolled. This is not native macOS or
Windows timing. Their actual lifecycle/fault acceptance remains in A12 evidence.

Each size has that many public values plus one revision variable and one secret.
Both policies use identical logical inputs and actual managed Python children,
key sessions and readiness probes. Three independent roots/policy/size each
perform three sequential commits (nine observations per table row, 54 total).
Policy order alternates per root. All 18 cases complete and confirm cleanup.

Times are milliseconds from the value-write CLI return to the observed outcome.
Notify observes the supervisor's pending state while the original app remains
alive; restart observes applied/readiness and independently confirmed new child
input. These are different outcomes, so a relative speedup between them would be
misleading. Notify's subsequent explicit manual application is timed separately
in the raw rows. Commit call and total start-to-observation durations are retained.

| Base public keys | Policy / observed outcome | Median ms | Min–max ms |
| ---: | --- | ---: | ---: |
| 10 | notify / pending | 675.097 | 673.822–915.104 |
| 10 | restart / applied + ready + child | 805.440 | 803.055–1057.159 |
| 100 | notify / pending | 930.205 | 686.401–934.633 |
| 100 | restart / applied + ready + child | 881.019 | 757.979–1069.947 |
| 1000 | notify / pending | 950.051 | 663.493–955.022 |
| 1000 | restart / applied + ready + child | 890.646 | 815.712–1115.184 |

Observations include CLI query cost and 50ms polling, not an internal commit/event
timestamp. Results are descriptive upper-bound observations. Three commits share
each root/cache; nine observations are not nine independent hosts. Small samples
cannot establish reliable p99, an SLA, significance or monotonic scaling. A12's
separate 1,000-sample execution study supplies the execution-preparation p99.

The final opted root confirms three successful automatic replacements, then
fourth-goal paused/restart_storm. A fifth goal is actually observed by the watcher
and stays unlaunched over 4.5 seconds (more than two configured periodic intervals).
Records remain four (original plus three successors), with one active app still
using revision three. No timeout/window/cap was weakened. Failure latches and
actual coordinator death/post-stop/readiness/response-loss cases are independently
verified by the full native C02/A12 fault suite, not inferred from timing samples.

Setup/import/explicit unlock and cleanup are outside the timer. No cache clear,
secret reveal, implicit backend change, raw fallback, rollback or PID-based signal
is introduced. Baseline 89100eb versus protected-runtime CLI/RSS/component
comparisons remain in A12-final-performance-results.md; no absent baseline
restart automation is fabricated. All raw samples are kept without outlier removal.
