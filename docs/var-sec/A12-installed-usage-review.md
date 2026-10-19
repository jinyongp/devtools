# A12 installed metadata contract follow-up

Extends the existing installed vault journey within A12 U01/U04/D01 integration:
actual packaged CLI, encrypted root/key session, managed Python child and shared
value store. No fake server/API, real user vault or OS keyring is used.

The fixture checks vault-free public inspection, frontend/backend/deploy reusable
selection, declared backend versus actually delivered frontend, exact delivered
slot refs, JSON schema required fields, default notify discovery and absence of
public/secret values or session/launch authority in inspect/uses/command reports.
Passive queries and schema discovery must leave the full vault status unchanged.
An unselected secret update remains current; a selected public update must reach
the actual supervisor watcher as pending while the same app and sole execution
remain alive, with no automatic apply. Subsequent existing manual process/project
restart and none/missing/locked cases remain intact. Locked frontend and none
inspection must be verification_pending with no verified current input.

`bin/devtools run test:installed vault` passes at current production 1e4bebd
(`A12-installed-usage-contract.log`). Review covered installer/runner cleanup,
parent env clearing, metadata canaries, no-touch timing boundary, schema versus
independent behavioral expectations, polling bound, preserved explicit restart
assertions and synthetic input-file permissions. No eligible remaining finding.
This is additional local Linux/WSL installed evidence, not a claim that these
new assertions ran in the earlier native CI checkout. Native CLI/API and browser
coverage there is separately recorded. B03 consumes this shared evidence only
after A12 completion; its own full stage acceptance/Skill review remains required.
