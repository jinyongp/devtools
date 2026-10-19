# C02 installed proxy automatic-update journey

The new `verify/scenarios/value_updates.py` scenario runs the actual packaged and
installed CLI, encrypted passphrase vault/key session, managed launcher, readiness
probe and TLS proxy on owned loopback ports. Its explicit isolated root prevents
system hosts/trust/capability/provider changes; those native network boundaries
are separate existing CI gates. No server or IPC implementation is mocked.

After initial readiness, a protected profile's public revision changes. The
opted server must report applied/readiness_passed, retain original/replacement
IDs, preserve proxy mode/port binding and serve the new revision through the
same TLS route. The app reports only revision/token-presence/PID; the PID is
compared for replacement and never used as a termination target. Secret bytes
and passphrase must not enter CLI or application responses.

Manual lock produces verification_pending while the same routed app remains
available. Explicit unlock reconverges to current without another execution.
There must be exactly two records and one active server. Finally, private project
and proxy controls stop the owned fixture; the installed runner retires its key
sessions before removing data. No user vault or normal networking is changed.

Actual installed Linux/WSL journey passes (`C02-installed-auto-proxy.log`, exit 0),
including TLS/SNI/certificate validation and actual network-aware helper branch.
Review covered same-user root propagation, snapshot/goal before stop, inherited
selection, readiness, routed child lifetime, secret-free observations and cleanup.
No unresolved eligible scenario finding. Native/final-source stage gates remain
separate; this test is not actual system DNS/trust mutation or a Windows test.
