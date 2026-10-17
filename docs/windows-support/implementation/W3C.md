# W3C — Windows quiet console passphrase input

Date: 2026-10-17. Task: `41dcf6d2-6a41-4d24-8352-ce966fbc9e9c`.
Reviewed code: `1a8fa9b9f9542bef4609071d3d3f9842f0ab0cf8`.

## Result and checks

Native prompts save the console mode, disable echo/line/processed input before showing the prompt, consume UTF-16 key events without a background blocked reader, and restore the exact prior mode on every return. Ctrl+C and context cancellation return 130; EOF and input/output failures return an error without returning the partial secret. Failed input flushes the pending console queue. Backspace removes an entire surrogate pair and invalid UTF-16 is rejected. Input remains bounded by the existing UTF-8 passphrase limit. Redirected input retains the file/pipe contract and does not open a console prompt.

The shared redirected-reader handoff now clears canceled/failed read buffers and does not retain a secret in an abandoned buffered result channel. A pre-canceled request does not start a reader. Cancellation closes a closable pipe/file input. Arbitrary custom io.Reader implementations retain their own interruption behavior; the console path uses no reader goroutine.

- [Native Actions 39001319891](https://github.com/jinyongp/devtools/actions/runs/39001319891), immutable reviewed commit: both amd64 and arm64 PASS, 10 console test/subtest rows per CPU, no skip/failure. Each fixture child owns a real hidden Windows console and asserts echo is already disabled at prompt delivery and the exact prior mode is restored afterward. Success/Unicode/backspace, Ctrl+C, explicit cancellation, timeout, EOF, writer failure and invalid Unicode are covered. Actual pipe cancellation and normalization also pass.
- Existing native process argv/stdio/exit/cancellation tests and filesystem/lease/preflight checks remain passing on both CPUs. Console input is not a substitute for W4 Job ownership and CTRL_BREAK group forwarding.
- Local Windows x64 console/pipe checks PASS. Unix CLI suite, CLI race checks and vet PASS; ARM64 CLI cross-build PASS. The final common-reader cancellation/clearing adjustment was followed by the ordinary Unix CLI suite/vet and required native CI.
- Retained JSONL under W3C-actions-39001319891 normalizes text line endings/trailing blanks.

## Last review loop

| Area | State | Findings and verification |
| --- | --- | --- |
| Quiet mode and restoration | reviewed | Prompt is delivered only after quiet mode; real-console fixtures verify exact restoration on success and every required failure path |
| Cancellation and ownership | reviewed | Event polling owns stdin and leaves no blocked console goroutine; queue flush occurs before mode restoration after errors; real pipe cancellation closes its reader |
| Unicode, editing and limits | reviewed | Actual UTF-16 key records cover Korean and surrogate-pair backspace; invalid surrogate input is rejected, UTF-8 size validation remains shared |
| Secret handling | reviewed | No secret added to argv/log/errors. Review fixed abandoned read-result retention and partial error-buffer cleanup; buffers are cleared on return |
| Existing callers and foreground behavior | reviewed | Transfer prompt callback/confirmation/error contracts remain unchanged; noninteractive input stays separate; existing native foreground stream/exit/cancel tests pass |
| Native ABI and architecture | reviewed | Real INPUT_RECORD calls pass on both native CPUs with no skip. No shell, ConPTY or runtime dependency added |
| Scope and final integration | reviewed | Actual visible PS5.1/7/cmd keyboard UX remains required W8L; Job/CTRL_BREAK and managed lifetime remain W4. Intermediate public gate is preserved |

No remaining in-scope finding or unexecuted W3C automated requirement. This record does not pass W4, W8L or W9.
