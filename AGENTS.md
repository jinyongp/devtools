# Devtools implementation and review

For CLI, lifecycle, storage, or recovery changes, read the user-outcome review
criteria in [docs/verification.md](docs/verification.md#사용자-결과를-기준으로-리뷰하기).
Use the existing checks, installed scenarios, and native workflows described there.

- Recover the user's intended result and agreed limitations before defining checks.
  Review the failure, visible explanation, next action, and resulting state together.
- A parser accepting a suggested command does not establish that it resolves the
  problem. Execute affected guidance in an owned fixture when safe, preserving its
  profile, directory, storage context, and request identity.
- When a reported defect escaped earlier checks, identify what those checks missed
  and inspect callers sharing that cause. A regression test for one message alone
  does not close the review of the shared behavior.
- Distinguish source inspection, executed journeys, and native platform results.
  Required failed or pending checks leave completion open. Report material gaps
  instead of inferring readiness from test counts or shared dispatcher wiring.
- Keep accepted safety and compatibility boundaries. Do not make tests pass by
  bypassing locks, integrity checks, required native coverage, or unknown outcomes.
