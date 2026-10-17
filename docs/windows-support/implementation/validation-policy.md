# Validation policy amendment — 2026-10-17

User decision: “Actions 검증으로 확정하고 실제 PC 미검증 한계 공개”.

This replaces the previous required Windows 11 x64 desktop eight-row gate. Continue Windows execution only on GitHub Actions. Required product gates cover native amd64/arm64 Actions and existing Unix regression checks at the same candidate revision/artifacts. Record actual runner OS/build and architecture; Server x64 does not prove Windows 11 x64 desktop compatibility.

W8L now reconciles CI evidence and publishes actual-PC NOT RUN limits. UAC prompt interaction, actual-PC browser/trust integration, terminal closure, actual-PC installation/update and final-product WSL2 networking remain unverified. Historical standalone local probes remain historical evidence. Linux fixtures cannot prove Windows/WSL networking. No Windows 10, self-hosted equipment or new local PC setup is required.

Task-by-task and final W9 review loops remain required. No automatic test may be removed or failed/missing required CI row relabelled as a pass. This scope amendment does not authorize a real release, tag or main merge.
