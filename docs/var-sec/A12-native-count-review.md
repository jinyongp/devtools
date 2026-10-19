# A12 native session call accounting — prerequisite review

Wraps the original session protector only in tests to count logical native
unwrap/helper requests. The unchanged bounded helper still invokes the actual
Keychain, Secret Service or user DPAPI implementation. Initialization's native
wrap/verification is explicitly excluded; these are not OS syscall counts.

One actual cold Use must unwrap once. Warm batches of 10/100/1,000 Use operations
and 32 concurrent warm uses must unwrap zero times. A deterministic clock expires
the cache without sleeping eight hours; passive Status must not unwrap, the next
cold Use unwraps once, manual locked Use cannot unwrap, and explicit Unlock makes
one native request. This tests request reuse, not a 1,000-key CLI delivery dataset;
full-profile benchmarks measure the latter separately.

The common helper compares returned key material without printing bytes, destroys
owned copies/session keys and cleans synthetic native items. macOS/Linux native
wrappers require the existing disposable-host opt-in; Windows user DPAPI has no
persistent item. macOS/Linux runners require actual pass events, preventing a
skipped native test from satisfying acceptance. No production key policy or ACL
changes, key backend installation or user data migration are introduced.

Local actual IPC/helper code talks only to the private test Secret Service fixture
and passes under race (4.446s, `A12-native-count-fixture-race.log`). The emitted
safe count record is 1 initial cold, 0 warm, 1 after expiry, 0 while manually
locked, 1 explicit unlock, 3 total. It is not an actual GNOME native result.
Windows amd64 test cross-compilation passes. Native OS execution remains pending.
Reviewed counter placement, clock/timer ownership, cache concurrency, material
cleanup, native item cleanup and runner required-test selection; no unresolved
eligible prerequisite finding. Native evidence and A12 acceptance remain pending.

Full vault race suite passes (25.347s, `A12-native-count-full-race.log`), related
vet passes, and native-runner Python files parse successfully. No local native
OS credential store was accessed; the actual native acceptance remains pending.
