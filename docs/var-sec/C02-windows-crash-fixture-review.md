# C02 Windows coordinator crash-fixture follow-up

The f3722b2 native x64 suite failed only
`TestAutomaticCoordinatorRealDeathDuringOldStopDoesNotRepeat`, waiting for the
old-stop barrier. The same native suite passed ordinary automatic replacement,
goal guard/replay, post-stop spawn/readiness failures, burst cap and watcher tests;
the installed x64 product/browser lane also passed. The run is not accepted as
fully successful.

Review found the crash test launched its owned coordinator with Cmd.Start,
whereas production uses process.StartDetached. On Windows those have different
console ownership; on Unix they differ in session ownership. The fixture now uses
the actual detached launcher while retaining its owned Cmd handle for deliberate
kill/wait. A timeout prints only safe automatic state/reason, allowing native
revalidation to distinguish preparation failure from a missing signal barrier.
No production behavior or native ACL is changed and no timeout is increased.

Local focused race passes (2.714s, `C02-detached-death-fixture-race.log`). Native
Windows revalidation remains pending; the mismatch is confirmed, but it is not
claimed to explain the native failure until the new run is inspected. No gate is
waived or completed on cross-compilation/local Linux evidence.
