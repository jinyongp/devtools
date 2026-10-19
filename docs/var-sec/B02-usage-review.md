# B02 usage reporting final review

Scope: B02 cached inspection (`a30e440`), delivered versions (`ba70e10`) and the
passive CLI, service and dashboard consumers in this commit. A12/B03 native and
integration gates and C01/C02 convergence/restart remain separate required work.

## Coverage ledger

| Area | Review and evidence |
| --- | --- |
| Warm-only authentication | Reviewed `InspectInput` and cached RPC: OpenSessionClient only, no SessionManager, unlock, native helper or KDF; same lock ordering, bounded five-second inspection, owned keys destroyed; cold/manual lock and exact idle preservation verified through real IPC. |
| Manifest/catalog and data exposure | Complete protected catalog/profile authentication precedes reference selection. ResolveSelectedMetadata opens no values, including public variables; tampered manifest rejected even for none. Only names/kind/source/IDs/versions/incarnation/verification escape. No plaintext hash, values, launch ticket, endpoint or session authority. |
| Public/legacy/absence | Public-only inspection needs no vault and reports unverified integrity. Missing storage has a stable known empty delivery, with no profile publication. Legacy inspection does not migrate or manufacture stable identity. |
| Effective input comparison | Real common/override and same-value/unselected/bundle membership mutations tested in the delivery unit. Actual profile re-encryption keeps comparison current. Actual logical replacement creates a new generation and a pending input. Bindings and shadowed injection are filtered; missing or secret var binding fails. |
| Selected expiry | Inspection reads metadata only and matches actual preparation's selected expiry rules. Expired injected secret fails; a metadata-only public binding does not use an expired secret. No common fallback. |
| Declared versus delivered | ValueUses has separate declared/prepared/delivered kinds and filters canonical project directory. Current goal follows the same config plus explicit env/bundle overrides as manual restart. No started timestamp, unknown supervisor state or old record without InputVersion remains unknown. |
| Status/CLI/schema | values inspect/uses, command list/inspect selection and current metadata, process list/status observation maps reviewed. Normal report with failed/pending inspection returns JSON success plus explicit state/reason; project query failures remain errors. Current/pending/failed/unknown/verification_pending remain distinct. |
| Dashboard | Authenticated process API returns observations separately from stored records; details show actual recorded versions, selection and output mode. Text is escaped through textContent. Existing refresh picks up changes without extending key idle; no automatic restart introduced. |
| Unchanged callers | Reviewed frozen payload/record matching, normal manual process/project restarts, config selection precedence, metadata copy ownership, record archive/backup consumers, aliases/completion and polling. Whole project validation and actual browser/server checks apply. |

## Review loop

The first implementation attempted to reuse a value snapshot for passive metadata;
that opened public values and missed injected secret expiry when secret opening was
disabled. Replaced it with full-authentication metadata resolution and explicit
preparation-compatible expiry validation. Review also added missing-name/secret-kind
binding validation, a five-second bound, actual restore/re-encryption comparisons,
and stable absent-profile comparison. Unknown historical deliveries are checked
before attempting a current comparison. No unresolved eligible B02 finding remains.

A first browser invocation used the old prebuilt `bin/devtools` and failed on the
missing delivery field. It is not passing evidence. Rebuilt source to an explicit
private CLI path; all three browser journeys passed in 19.6s, including real managed
Node process delivery, pending change, manual lock verification pending, same app
still alive, no public/secret canary on the process view, and owned cleanup.

## Checks and limits

- `./scripts/project check` with isolated tool/cache directories: exit 0, full
  project checks, gofmt, vet, whole Go race, dashboard JS and existing script gates;
  `/tmp/devtools-values-plan/B02-check-all.log`.
- Whole affected packages race: values 63.922s, services 8.159s, execution 4.036s,
  CLI 46.304s, dashboard 3.892s; `B02-usage-full-race.log`.
- After binding validation and additional assertions, focused race checks passed
  in `B02-inspection-binding-race.log`, `B02-inspection-final-race.log` and
  `B02-restore-rotation-race.log`. Whole project race also includes the final
  binding behavior; later changes are additional assertions/import formatting/docs.
- CLI public/secret canaries and session idle tests pass. Existing diagnostics
  canary tests pass in the whole suite. Windows ARM64 values test compilation
  passes (`B02-values-windows-compile.log`), not a native execution claim.
- Current source browser: `B02-usage-browser-current.log` (3 pass). Existing
  key-clear/no-idle and bundle management journeys remain included.

Logs above live under `/tmp/devtools-values-plan`. Native final-source runs remain
required and will include the new session protocol 3 and browser case. The earlier
B01 native run b9ddd11 is green but is not evidence for B02 runtime changes.
No app getenv or external credential success observation is claimed. Parent env
is independent. Returned metadata is an observation, not a launch admission or a
promise to revoke a previously delivered environment. C01 adds durable convergence;
C02 adds only explicitly selected managed auto-restarts.
