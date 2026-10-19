# B02 delivered input versions

This unit records only profile contributions to the actual frozen execution
payload. B02 remains active: passive inspection, usage reporting, status/UI/schema
integration and their acceptance checks follow this prerequisite.

## Reviewed contract and areas

- Reviewed `values.InputVersion`, `execution.Command.ValueInputSpec`, materialization,
  owned metadata copies, launcher transport validation and persisted service records.
- Effective slot identity includes name, common/env source, value ID, slot ID and
  logical version, with profile ID and storage generation. The opaque digest hashes
  metadata, never plaintext. Key epoch, display/selection spelling and unrelated
  slots do not change effective input. Equal text in a distinct override slot still
  represents a different input source. Same-value assignment preserves its version.
- Binding templates contribute their actual `var.*` references even without injection;
  escaped references do not. Profile variables overwritten by literal/port bindings
  do not contribute unless used by another template. Isolated-root control injection
  shadows a profile value of that name. Parent environment is a separate contract.
- Old providers without stable identity remain unknown (`nil`); a new execution with
  no profile input has a known empty slot list. No app `getenv` observation is claimed.
- Records persist safe versions, not the private snapshot's admission tickets,
  session endpoints, launch credentials or values. Preparation is recorded before
  spawn; a consumer must check execution start/state before calling this delivered.
- Metadata copies own arrays. Transport validation recomputes the delivery version
  from the frozen source metadata; launcher reception also matches it to the record.

## Review loop and evidence

Review found that environment selector spelling would falsely change a common-only
input. The digest now includes each actual slot's source env instead. Source tests
use real authenticated store mutations to cover same value, unrelated change,
common/override precedence and new bundle members. Additional tests cover binding
filtering, unknown/empty distinctions, replacement generation, metadata-only epoch
changes, copy ownership and inconsistent transport rejection.

- `go test -race ./internal/values ./internal/execution ./internal/services ./internal/cli`
  passed (62.292s, 4.058s, 7.199s, 45.650s); log
  `/tmp/devtools-values-plan/B02-delivery-full-race.log`.
- After adding transport consistency validation, affected focused race checks passed:
  `go test -race ./internal/execution ./internal/services ./internal/cli -run
  'TestValueInputSpec|TestBundleExecution|TestFrozenLauncher|TestPublicManaged' -count=1`;
  log `/tmp/devtools-values-plan/B02-delivery-validation-race2.log`.
- New actual snapshot assertions confirm public-only classification, selected and
  empty delivery, immutable copies and rejection of a modified delivery version.

All areas of this unit reviewed; no unresolved eligible finding. No native result
is claimed for this unit. Native final-source and public B02 consumer checks remain
required by the workstream gates. Storage format 1 and CLI protocol 7 are unchanged.
