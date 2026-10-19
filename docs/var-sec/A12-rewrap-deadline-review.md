# A12 macOS recovery-fixture deadline follow-up

At f3722b2 the actual macOS full race suite passed CLI (389.400s), values
(286.990s) and backup (173.956s), but rewrap's installed-verified crash case
failed at independent root verification with context deadline exceeded. Its
subtest took 25.53s; the old fixture shared one 20-second context across the
crashing child, a separate explicit recovery request, and independent decryption.
The real production paths and Argon2 preset were unchanged from accepted N05.

The test now gives each of these separate operations its own unchanged 20-second
bound. That models a fresh recovery client after child death and a distinct
verification operation, instead of spending the later operation's budget on an
earlier process. It still requires actual child exit 99 at each of six durable
checkpoints, changed/replayed recovery, identical random root and no plaintext
root/passphrase in recovery files. Safe per-phase durations diagnose native load.
No production timeout, KDF cost, checkpoint or assertion was weakened.

Focused actual-process race passes all six cases (6.400s total package time,
`A12-rewrap-operation-deadlines-race.log`). The earlier native failure remains
recorded and actual macOS revalidation is required; local/fake/cross compilation
cannot close that gate. Reviewed request/context ownership, cancellation, child
failure, root-material lifetime and unchanged canary/recovery assertions; no
remaining eligible fixture finding.
