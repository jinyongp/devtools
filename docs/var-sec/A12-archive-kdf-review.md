# A12 archive KDF input bound

Review of the untrusted profile/backup archive boundary found that age v1.3.2
exports scrypt logN=18 but its default identity accepts up to 22 before authenticating
the header. With r=8 this permits approximately 4 GiB of KDF working memory.
The new fixed export/import maximum is 18 (approximately 256 MiB), preserving
all devtools-created passphrase archives. Custom external archives above that
cost now fail with the existing invalid-backup behavior. X25519 is unchanged.

The cheap mode probe rejects excessive, negative, overflowing and noncanonical
cost strings before password selection. The actual decrypt identity separately
sets its maximum to 18, so replacing the file after probing cannot bypass it.
The probe does not authenticate or consume plaintext; actual age authentication
still governs import. Lower valid costs remain compatible. No password or value
is added to argv, logs, errors or files.

Before-fix synthetic header tests reproduced acceptance of 19, 22, -1,
overflow, +18 and 018 without running an expensive derivation. The fixed race
checks pass for mode recognition, rejection, real passphrase export/import and
recovery, wrong passwords and existing X25519 recognition. Full backup and
profiletransfer race suites pass (backup 33.876s). CLI passphrase/archive/transfer race regressions also pass (16.257s).
Fresh native acceptance is recorded separately when complete.

Reviewed header parsing/error propagation, private file permissions on both
platforms, file replacement, direct decoder callers, recipient compatibility,
password prompt ordering and existing archive authentication. No remaining
eligible finding in this change. This bounds one KDF invocation, not total
machine resources against many independent same-user commands.

Actual compiled CLI 1e4bebd default noninteractive imports were also checked with
synthetic headers: factor 18 reaches passphrase_required, while 19/22/negative/
overflow/+18/018 return invalid_backup before password selection. No password
was supplied, so those checks do not run a KDF. Raw safe result rows are included
in the final A12 evidence. Both Windows native x64 and ARM64 independently pass the new archive-cost
test in the exact same source suites (0.05s/0.06s). Full-run installation and
exchange acceptance remains separate until the required jobs finish.
