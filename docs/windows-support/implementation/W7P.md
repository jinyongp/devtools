# W7P — Portable developer commands and verified archives

Date: 2026-10-17. Task: `c043d1f1-72be-4653-a0ca-879e10f1358d`.
Reviewed code: `1a5f34cdaa9470ef53ce002b9bd0c8a0495659dc`.

## Result and scope

Project build/check/package orchestration uses stdlib Python. The tracked config invokes scripts/project: Unix executes its small Python3 shim, Windows PATHEXT selects project.cmd and Python. Existing .sh entries remain compatibility shims. No shell wildcard, /dev/null or rm executable is needed by the portable path; Node tests use real native fixture executables and Git path identity rather than POSIX scripts. Developer Python/Go/Node/uvx requirements do not become installed CLI runtime dependencies.

The package builder emits the existing four Linux/macOS tar.gz targets and Windows amd64/arm64 ZIPs with sha256 sidecars. It checks exact archive entries, header/CPU, Go module/GOOS/GOARCH/CGO metadata, and a linked build identity that also supplies runtime version/commit. Native packages execute version to verify the response. Cross packages verify the same identity statically; this is not a claim to have executed a foreign CPU/OS binary. Actual native Windows CI executes both Windows architectures.

Go intentionally omits the -ldflags build setting with -trimpath (GOROOT src/cmd/go/internal/load/pkg.go). The builder therefore links a bounded ASCII devtools-build-v1 identity used by the runtime version handler. Ordinary -X main.version/main.commit builds retain their prior behavior when the package identity is absent. Public version JSON fields and protocol remain unchanged.

Skill archives still use the existing Go canonical writer. Python verifies checksum, exact canonical entries/content, reproducibility, official validation and negative fixtures. A targeted Git attribute fixes canonical SKILL.md bytes to LF across hosts. Release-tag publication logic is ported to Python with the same exact commit/ancestry/non-overwrite rules; tests use disposable local Git remotes. No real release/tag was published.

Windows installer-check awaits W7I and full native product integration awaits W8. This task does not pass the Windows check:all product gate. Unix check:all retains its complete race gate; native Windows check orchestration uses the standard Go suite, with platform product coverage supplied separately by native CI.

## Actual checks

- [Native Actions 39003386019](https://github.com/jinyongp/devtools/actions/runs/39003386019), exact reviewed commit: both native CPUs PASS. Each has 12 Node project/release tests, official Agent Skill checks and 7 Python archive tests, 1 Go build-identity test, and a real native ZIP with correct version/commit. The Python suite executes the extracted CLI with PATH empty, proving no Python/Go/Git Bash runtime lookup for version.
- Both CPU ZIPs contain one regular devtools.exe. amd64 PE machine 0x8664, SHA-256 2026d84b8d9e7377cd13fa69a19722958f135ab26d915d8d2618e669717d1246; ARM64 PE machine 0xaa64, SHA-256 2a2db12f99697b6f4b8522a34b89aa1c0f3b253a8a434f73bc379f988b226264. Each inspected image has exactly one matching linked identity. ZIPs remain in the Actions artifact; inspection and checksum text are retained in Git.
- Existing native filesystem/command/console/storage checks remain passing; each CPU still has 194 complete task/value/profile/maintenance storage rows without skip/failure.
- Unix real checkout CLI run check:all PASS, including skill validation, dashboard, installer/bootstrap, release/project, perf checks, gofmt, vet and full Go race suite. Node release/project rechecks PASS after the path-identity correction.
- Local generation of all six target archives PASS: correct ELF/Mach-O/PE CPU and Go/version/commit identity, exact member structure and checksum. Only the host Linux amd64 image is executed locally; both Windows native runtime checks are supplied by CI.
- Raw result text in W7P-actions-39003386019 has normalized line endings/trailing blanks. Later W2B private-I/O changes are excluded from this immutable tooling result.

## Last review loop

| Area | State | Findings and verification |
| --- | --- | --- |
| Project command parity | reviewed | Checkout CLI precedence, first failed gate exit propagation, explicit version/environment and native compatibility entry tests pass on Unix and both Windows CPUs |
| Interpreter and shell boundary | reviewed | Unix Python3 and Windows Python selected by small native shims; Windows cmd entry tested; wildcards expanded as argv and clean uses stdlib filesystem operations |
| Archive and metadata | reviewed | Six target headers checked; native ZIP version/commit and standalone PATH-empty execution pass; wrong PE CPU, wrong identity and invalid ZIP members rejected |
| Publication failures | reviewed | Invalid version and failed Go archive commands publish no partial output. Skill duplicate/link/tampered content is rejected even with a matching checksum |
| Skill parity | reviewed | Original Go writer retained; repeated archives/checksums and official valid/invalid-description checks pass; LF source bytes fixed for cross-host reproducibility |
| Release safety and fixture isolation | reviewed | Same commit/tag retry and conflict/ancestry rules preserved; inherited Git environment cannot mutate caller repositories/indexes; all publication tests use local bare fixtures |
| Windows directory aliases | reviewed | Native CI exposed Git long names versus Node 8.3 aliases. Fixture now compares physical dev/ino identity rather than normalized strings; both CPUs pass |
| Consumers and Unix regression | reviewed | Updated tracked commands, package.json, compatibility scripts and release tooling inspected. Full real CLI check:all passes on Unix; W7I/W8 gates remain explicit |

No remaining in-scope W7P finding or unexecuted required W7P gate. This record does not pass installer, full product support or W9.
