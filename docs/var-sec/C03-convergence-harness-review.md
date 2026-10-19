# C03 actual convergence measurement harness review

The existing CLI benchmark times one invocation, so it cannot measure commit to
supervisor observation or independently confirmed automatic application. The
separate perf/convergence.py uses the existing actual CLI, private root and
value-free authenticated retirement helper, without an external service or mock.
It compares notify/restart policies on the same public-key/one-secret fixture;
their outcomes differ and are not advertised as equal-work speed comparisons.

Review covered ownership/root propagation, no secret argv/env/output, readiness
against the actual new frozen revision, declared dataset sizes, monotonic timing,
CLI/poll overhead, alternating order, run budget, failure/cleanup, output collision
and partial evidence. Each root has three updates, then the final opted root must
pause the fourth and keep the fifth unlaunched over 4.5 seconds. The actual watcher
must observe that latest goal; elapsed time alone is not proof it reconciled.
No stored PID is a signal target. Only private project controls and verified
session retirement precede deletion; unconfirmed cleanup retains its private root.

Corrections during review: moved binary identity/hash acquisition under the output
finalizer; append the case before measurement and samples as they complete, so a
later error preserves partial rows; distinguish measurement completion from
confirmed cleanup; require the actual fifth watcher goal rather than inferring
reconciliation from a sleep. These affect tooling, not production restart policy.

Final actual ten-key smoke passes both policies, three updates each, readiness,
three-auto-replacement cap, fifth-goal pause and confirmed cleanup. It is only
smoke, not the final distribution. Existing-output rejection preserves its digest;
invalid binary identity fails with parseable complete:false and no fixture rows.
Final smoke artifact's harness hash matches source. Earlier prototype/review
smokes are retained privately and not pooled into final samples. Full measurement
starts from a committed clean source after this reviewed tool/operating-doc unit.

Generic Skill quick validation's unchanged compatibility-field mismatch is the
known B03 tool limitation. The applicable official Skill/package checks and full
C03 project checks still apply after the operating-reference update. No default
automation is enabled by documentation; user-selected managed opt-in remains
required. No remaining eligible harness finding; C03's final integration ledger,
measurements and whole Z01 review are separate completion requirements.
