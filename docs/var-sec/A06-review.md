# A06 키 세션·사용자 IPC 구현 검토

범위: `internal/vault/session*.go`, `ipc*.go`, OS protector 선택과 같은 binary의
private daemon/native helper entrypoint. 사용자 CLI·실행 snapshot·실제 spawn admission은
A07/A08/A11 후속이다. 이 단계의 잠금은 key cache와 IPC 전달을 보호하며 이미 반환한
키/앱 환경을 회수하지 않는다. 실제 native/다른 사용자/WSL 증거는 N04의 필수 조건이다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 키 소유·idle·재해제 | reviewed | 8시간 idle, absolute max 없음, status 미연장, 성공한 내부 key use만 연장, 타이머와 admission 전 expiry 검사 |
| 수동 잠금·복구 | reviewed | atomic private latch, 성공한 explicit unlock만 해제, key slot fingerprint 변경 시 cache 폐기, recovery는 자동 해제 금지 |
| IPC frame·binding | reviewed | 128KiB frame/8 connection/30초 request, duplicate·unknown field 거절, vault/session/protocol·매 연결 nonce 검증 |
| Unix peer·endpoint | reviewed | private pathname socket, client/server UID 확인, daemon OS lease 뒤에만 stale socket 정리 |
| Windows pipe | reviewed source/compile | local-only·현재 SID/SYSTEM DACL·first-instance·양쪽 process SID·client identification SQOS, overlapped connect pin/cancel/drain |
| daemon lifecycle | reviewed | on-demand detached same binary, 시작/daemon OS lease, private endpoint, stable control retirement 뒤 OS lease 확인, key cache 재사용 없음 |
| 잠금·IPC 순서 | reviewed | 새 admission 닫기→manual intent 저장→기존 key response write drain→ack; 기존 unlock challenge epoch 무효화 |
| native 대기·KDF | reviewed | single key/KDF mutex, 고정 KDF, native unwrap 별도 동일 binary child와 anonymous pipe, 취소 시 kill/WaitDelay로 daemon 무한 대기 방지 |
| 민감 출력·상속 | reviewed | root/passphrase는 private frame에만, owned buffers 정리, 원시 backend 오류 미출력, task context/주입 secret env·터미널 stdin 미상속 |
| 실제 경계 검사 | reviewed Linux | 별도 daemon 프로세스 재사용·정상 retirement·강제 kill 후 latch 유지, private real D-Bus와 별도 helper pipe/cancel |
| 실제 다른 사용자·macOS·Windows·WSL | pending N04 | cross-compile이나 fixture로 이 항목을 통과 처리하지 않음 |

리뷰에서 수정한 결함:

- 키 response write보다 lock ack가 앞설 수 있던 경합을 admission read/write barrier로
  수정했다. 전송을 의도적으로 멈춘 테스트에서 durable latch가 먼저 기록되고 ack는
  drain 뒤에만 도착함을 확인했다.
- 오래된 연결의 explicit unlock이 뒤늦게 manual lock을 해제하지 않도록 lock epoch를
  hello에 묶었다. 새 요청만 새 epoch로 해제할 수 있다.
- IPC timeout만으로 동기 native API/인증 UI를 취소할 수 없으므로 native unwrap을
  제한 시간 있는 child로 분리했다. 실제 child가 D-Bus 응답을 기다리는 동안 취소하여
  session mutex가 풀리고 키가 열린 상태가 되지 않음을 확인했다.
- 조회가 없을 때도 idle key가 폐기되도록 타이머를 넣었다. 요청 경로도 만료를 검사한다.
- 복구 오류 뒤 일반 Use가 OS backend를 자동 재해제하던 경로를 막았다. key file을
  복구해도 explicit unlock 전에는 recovery 상태를 유지한다.
- parser가 부분 decode 뒤 실패해도 passphrase/key buffer를 정리하도록 했다.

검증: `go test -race ./internal/vault ./internal/values ./internal/execution ./cmd/devtools`,
해당 vet, Windows amd64/arm64·Darwin amd64/arm64·Linux arm64 compile/vet.
유휴 시간 경계는 fake clock이며 실제 8시간 대기 시험이 아니다. native 취소 검사는
private D-Bus fixture이며 실제 Keychain UI/DPAPI/Secret Service 저장 검증은 N01–N04에 남는다.
동일 사용자 악성 프로세스·전체 호스트 탈취를 차단하는 sandbox라는 보장은 없다.
사용자의 기존 profile/plaintext runtime은 아직 전환하지 않았으며 migration과 CLI 통합은 후속이다.
