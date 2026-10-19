# A12 process memory harness review

`perf/memory.py` promotes the independent RSS measurement into a repeatable,
parameterized Linux/WSL harness. It compares provided built binaries; no install,
backend fallback or existing user profile access is performed. Fixtures use an
explicit isolated root and passphrase backend with synthetic values only.

Reviewed GNU time and owned-daemon RSS boundaries, sample independence/order,
three cold cycles versus independent peaks, 4-way concurrent CLI sampling,
manual-lock acknowledgement/cache policy versus erasure, output identity/hash,
partial evidence, and process/data cleanup. Failed native-cache retirement
preserves the private fixture; only an actual owned child handle is killed after
a bounded wait. No PID read from a record is signaled. Output reservation uses
exclusive creation and preserves an existing result, including concurrent writers.

Actual final smoke compares both arms at 10 secret keys with five warm runs,
four concurrent runs and three cold protected unlock cycles. Both complete and
the recorded harness digest matches the current source. Existing-output invocation
fails before work and preserves the prior JSON. Evidence:
`A12-memory-portable-smoke-final.{json,log}` and
`A12-memory-overwrite-rejected.log` under `/tmp/devtools-values-plan`.
This is a tool smoke, not final three-size acceptance. The earlier a476cd0 RSS
experiment remains a prior result, not silently relabeled as the final source.
No unresolved eligible harness review finding; full final measurement follows.
