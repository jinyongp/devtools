# Navigation contract inventory

Baseline: `46dfba1417e00b793fc2a5d2c3c4864d8ba1b28f`, 198 canonical commands.
Protocol 23 adds `cleanup maintain`, bringing the current catalog to 199.
Aliases resolve through the same registry entry. This inventory classifies every
entry; `TestNavigationInventoryCoversRegistry` rejects missing and duplicate names.
The protocol 23 behavior is defined in [the plan](cli-navigation-recovery-plan.md).

## Paged collections

These return independently selectable resource/history rows. Filters precede
sorting and pagination. Command metadata publishes applicable states, time fields,
sorts and text columns. Default limit 50, maximum 200. Existing task completion,
scope and ordering rules remain in force.

```navigation-collection
variable list
secret list
env list
command list
bundle list
values uses
values execution list
task list
task current
task history
task checkpoint list
task workstream list
task workstream history
task validation list
port list
instance list
proxy list
expose list
profile list
process list
cleanup archives
```

| Family | Scope / searchable public fields | State and time semantics |
| --- | --- | --- |
| process | canonical project, profile across locations, instance; command/env | current service vs execution history; created/updated/ended |
| expose | canonical project or explicit global/profile; route/provider | current or history; created only where recorded |
| values execution | selected profile; public command/runner names | current unresolved delivery or history; execution/delivery states, actual created/updated |
| task / workstream | existing selected profile; title | repeated states OR; existing completion/scope; created/updated |
| validation | existing selected profile/workstream; title | recorded result, scope; actual created/updated |
| current / history / checkpoint | existing profile and target | run/task/workstream/action; actual event times |
| variable / secret | profile/env; key only | source; no value search or invented time |
| env / bundle / command | profile or project; name only | no invented state or time |
| port / instance | project/profile/instance; service or alias | protocol/location state where real; no mtime inference |
| proxy | project/profile or explicit global; host/route | listener and observed route state; preserve global semantics |
| profile | global; name | no invented state/time |
| archives | global or explicit profile; kind/name | archive state and recorded archive time |

## Complete diagnostic selections

Arrays here describe one selected operation, its prerequisites, graph, comparison,
or manifest. Removing arbitrary rows would conceal evidence or invalidate a digest.
These are intentionally not generic collection pages. Nested actions still pass
through the common recovery binder.

```navigation-diagnostic
project inspect
vault status
values revision
command env
proxy env
command inspect
config inspect
config check
command explain
values inspect
values runner address
values runner show
task show
task next
task context
task impact
task tree
task workstream show
task workstream context
task workstream impact
task workstream tree
task workstream check
task workstream spec show
task workstream plan show
task validation show
dashboard status
doctor
diagnostics
port show
port check
instance show
proxy status
expose status
proxy ca status
backup status
backup inspect
profile inspect
profile diff
process status
process check
process wait
project status
cleanup preview
```

`project status` deliberately shows all commands selected by the lifecycle request
and their readiness; it is not an implicit process history. `values uses` is a
paged collection of metadata references, while `values inspect` describes a single
complete selected value plan. Runner address/show selects one runner; there is no
runner-inventory command to page. Proxy status with `--all` is a complete listener
health report. Cleanup preview retains the whole digest-bound plan and aggregated
retention buckets; `cleanup archives` is the navigable archive collection.

## Immutable receipts and exports

These describe one original request or an explicit complete export. They preserve
replay fingerprints, revisions and exact logical results; list options do not apply.

```navigation-export
values request show
task export
task workstream export
process request show
project request show
profile export
```

## Native streams and discovery

Logs and artifacts retain their existing bounded/native behavior. A log line is
not a searchable metadata row and is never copied into a query snapshot. Help and
schema expose the actual registry and general syntax templates, not recovery argv.

```navigation-artifact
license
help
version
schema
command run
config schema
dashboard
proxy logs
proxy run
proxy ca export
process logs
project logs
completion
```

## Mutation results

Arrays are the result of one mutation, not independently paged resources. Preview
and recovery actions are checked by the same command resolver as read guidance.
No TTL is applied to user definitions/tasks. Preview input is public argv, a private
file or stdin; the last two require explicit re-supply and do not create snapshots.

```navigation-result
init
vault init
vault recover
vault rewrap
vault unlock
vault lock
variable set
variable unset
variable get
secret set
secret unset
env create
env remove
secret generate
config fmt
config edit
config migrate
config tool add
config tool update
config tool remove
config preset add
config preset update
config preset remove
command add
command update
command remove
command rename
import
bundle set
bundle rename
bundle remove
values execution approve
values execution cancel
values execution deliver
values execution report
values execution prepare
values execution status
values runner register
values runner unregister
task sync
task workstream edit
task workstream create
task workstream update
task add
task update
task workstream spec set
task workstream plan set
task depends set
task workstream depends set
task attach
task detach
task hold
task unhold
task cancel
task reopen
task workstream cancel
task workstream reopen
task workstream activate
task workstream close
task claim
task resume
task takeover
task unclaim
task checkpoint
task release
task done
task validation add
task validation update
task validation basis
task validation record
task validation accept
task validation waive
task validation unwaive
dashboard stop
port allocate
port release
port prune
instance name
instance move
instance remove
proxy start
proxy stop
proxy setup
proxy teardown
proxy configure
proxy restart
proxy hosts preview
proxy hosts apply
proxy hosts remove
expose start
expose stop
proxy add
proxy rm
proxy clear
proxy up
proxy down
proxy ca init
proxy trust
proxy untrust
backup keygen
backup configure
backup create
backup restore
profile transfer prepare
profile import
process start
process stop
process restart
project up
project restart
project down
cleanup apply
cleanup maintain
cleanup restore
cleanup purge
update
```

`variable get` returns protected value content, not a collection. `values execution
status` may authenticate delivery metadata but selects one execution. They retain
their existing value/protection rules and never feed public metadata search.

## Recovery producers and consumers

| Producer boundary | Coverage and required binding |
| --- | --- |
| protocol diagnostic/storage/guidance | generic error fallback, nested checks and causes, local root, declared effects |
| CLI parse/suggestions/cli | unknown command/option, nearest help, discovery; registry parser/static rules |
| CLI preview_output/output/config/hosts/profile | all 19 previews plus hosts apply; same public inputs, new UUID, digest/revision; private/stdin needs_input |
| CLI init/vault/passphrase/vault_rewrap | profile discovery/collision, private authentication inputs, interactive vs agent |
| CLI query/doctor/install_storage/update | same selection, env applicability, explicit storage preparation, no query-side migration |
| services store/request_recovery and lifecycle | execution status/logs, partial restart, original request recovery and batch identity |
| execution command/output_failure and doctor | selected command/env/bundle/mode, no diagnostic input substitution |
| ports run_observation | known managed execution stop, fresh UUID, no unsupported profile |
| proxy lifecycle | listener/route/provider recovery and externally unknown effects |
| values recovery/snapshot and valuehandoff inspect/deliver | exact request replay, passive status, private input acquisition |
| tasks diagnostics/edit/query_cache_directory | target/profile/revision, contexts remain private, explicit cache input |
| storageupgrade engine and backup | explicit prepare/restore, observed digest, permissions and lock causes |
| CLI outputController / dashboard manage | bind before public projection/serialization; same typed action contract and renderer |

Dynamic paths include help-group generation, task mutation/query definitions,
typed doctor check reports, nested failure reports, saved immutable receipts and
cache continuation actions. Binding is at the public boundary so persisted logical
results and fingerprints are unchanged. All emitted **ready** argv are reparsed
against the existing registry, including writes; needs_input/manual never masquerade
as an executable command. External commands do not inherit devtools root flags.

## Storage ownership and retention

| Owner | Current / retained evidence | Removable detail |
| --- | --- | --- |
| services / lifecycle | live/uncertain, latest service outcome, restart chains, batch and request identity | resolved terminal detail after 30d, captured logs after 7d |
| proxy exposure / netops | active/unknown provider effect and immutable request outcomes | resolved terminal detail after 30d; never provider deletion by TTL |
| valuehandoff | authenticated unresolved delivery, receipts and request identity | payload at existing expiry/confirmed terminal outcome; resolved detail after 30d |
| values / tasks / definitions | user-authored data, contexts, exact replay contract | only existing explicit remove/archive; no age deletion |
| query snapshots | root/filter/sort-bound, metadata only, 30m | owned expired snapshots during publication; cursor reads never prune |
| cleanup archives | restore body, references, existing 30d purge guard | explicit purge only |

No new no-expiry replay deadline, daemon or query-side cleanup. Migration prepares
current indexes/compact bodies under the existing writer gate; it does not expire
history. Unknown old end times and referenced bodies carry preservation reasons.
