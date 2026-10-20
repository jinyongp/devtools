# 네트워크 수용 기준과 검증 경계

[Documentation index](../docs/README.md) · [Development](../docs/development.md)

로컬 네트워크 기능별 검증 위치와 판정 범위를 설명한다. 전체 실행 결과는 해당
commit의 Release workflow에서 확인한다. 이 문서는 검사 위치와 경계를 설명하며,
실행하지 않은 실제 환경 검증을 통과로 대신하지 않는다.

이 표의 설치 시나리오와 OS 변경 항목은 macOS·Linux 기준이다.
Go 검사는 Unix `check:all`의 race suite, 설치 검사는 `test:installed`의 격리 HOME에서 실행한다.
표의 **native job**은 disposable Linux/macOS의 opt-in `system-network` OS 검사다.
실행 결과가 있어야 통과로 인정하며 기본 false인 workflow 입력을 실행 완료로 간주하지 않는다.
Windows native Actions는 [전용 검증 안내](README.md#windows-native-actions)를 따른다.
Windows UAC·CurrentUser trust의 실제 사용자 확인 창을 아래 Unix 결과로 대신하지 않는다.

| 검증 영역 | 검사 위치와 판정 기준 |
| --- | --- |
| 단일 CLI·기존 HTTP·UUID·root 격리 | `install`, `proxies`, `network`: 실제 archive 설치, 공개 schema, daemon/child root 전달, normal root 불변. `internal/paths` 및 OS helper 검사가 공유 자원 변경 차단을 확인한다. |
| registry 호환·기존 route·tombstone | `internal/ports/ports_test.go`, `local_network_test.go`: v1/v2 읽기 bytes 보존, 첫 mutation upgrade, identity/assignment/reservation 보존. `internal/proxy/resolver_test.go`: legacy implicit route 및 down/up 계약. `proxies`: 설치 CLI·worktree·재시작 회귀. |
| listener 독립성·port 소유권 | `network`, `network_failures`: 두 listener, 두 번째 socket bind 실패의 첫 socket rollback, 같은 UUID 재시도, 다른 listener의 기존 WebSocket·새 TLS 연결 보존, 선택 stop/start. `internal/ports`: assignment/reservation 충돌, exact backend 및 preferred/strict 계약. |
| CA·TLS 신뢰 | `internal/ca/ca_test.go`: root 초기화·손상·leaf/SAN·갱신. `network`: OS trust 없이 export한 CA로 HTTPS 검증. native job: 실제 신뢰 등록·해제·재등록 거부·재신뢰·private key 정리. |
| HTTP·TLS·redirect·bridge | `proxies`, `network`, `exposure`, `http2_exposure`: 404/502/503, path/query·비표준 포트 redirect, 실제 WS·SSE·HTTP/2, HTTPS scheme/authority와 위조 forwarding header 차단. |
| DNS·hosts·activation·global identity | `internal/project/network_test.go`, `internal/hosts/hosts_test.go`: env 치환·우선순위, 공유 소유권·외부 수정·symlink·응답 유실 재조정. `TestCustomHostRollbackAfterListenerFailureAndSameRequestRetry`: 실제 listener 실패 후 주입 helper가 임시 hosts를 원복하고 같은 UUID 재시도로 재준비. `network`: isolated custom-host 거절. native job: 실제 custom-host 자동 준비·noninteractive 경계. `cloudflared_faults`: 같은 global 이름 재생성에 새 route UUID, 과거 start conflict·stop replay 격리. |
| low port·바이너리 교체 | `internal/netsetup/setup_test.go`: inode/path 교체, interrupted retry, prepare 실패·commit rollback, isolated guard. native job: 설치 CLI의 실제 80/443, 권한 준비·복구·정리. |
| public binding·조회 | `internal/ports/route_binding_test.go`, `internal/cli/network_env_test.go`: 독립 기대 URL, pending·read-only·민감 값 제외·peer key 충돌. `network`, `ports`: 자식 실행 환경·route URL·전역 foreground 환경. |
| batch·restart·foreground | `network`, `project_lifecycle`, `readiness`, `install`: fresh web/api peer readiness, 모든 선택 child launch 후 wait, busy 외부 backend 연결과 managed 실행 거절, argv·stdio·exit·signal. `internal/lifecycle/network_test.go`: 준비 barrier·mixed modes·부분 재시도. |
| 인증·guard·pin | `exposure`: 인증 요구·동일 UUID replay/인증 변경 conflict·auth loss·비밀 비저장. `internal/proxy/exposure_runtime_test.go`: provider 시작 중 guard. `cloudflared_faults`: pin이 삭제를 막고 명시 stop 후 해제, 재생성 route에 오래된 operation이 연결되지 않음. |
| LAN 선택 공개 | `exposure`, `http2_exposure`: 선택 `.local` route·인증·TLS·다른 route 보존·stop 차단. client DNS/CA 안내는 CLI/schema 검사. 다른 실제 장치에서의 LAN 연결은 별도 실제 환경 검사다. |
| Cloudflared 수명 | `internal/expose/cloudflared_test.go`: URL parsing·timeout·early exit. `exposure`, `cloudflared_faults`: CLI 종료 뒤 관리, 시작 실패 origin 정리, provider/daemon SIGKILL, 자식 정리·인증 유실. provider 실행 파일을 모의하므로 공개 ingress 성공은 주장하지 않는다. |
| Tailscale 설정 소유권 | `internal/expose/tailscale*_test.go`, `tailscale_simulated`: 로그인/HTTPS 전제·LocalAPI 인증·ETag CAS·사용자 handler/미지 필드 보존·선택 stop·동시 충돌·403·응답 유실. 외부 설정 commit 뒤 로컬 record 쓰기 실패 시 provider rollback·origin 종료·동일 요청 복구 요구·명시 stop 후 재시작을 실제 CLI로 확인한다. |
| 변경 감지·선택 revoke·복구 | `exposure`, `http2_exposure`: stop/TOML/env 수정 후 열린 SSE/WS/HTTP2 종료 및 다른 route heartbeat. `internal/expose/sessions_test.go`: 같은 HTTP/2 연결의 다른 exposure stream 보존. `cloudflared_faults`, `tailscale_simulated`: crash·불확실 commit 복구. |
| 진단·schema·docs·UI | CLI/catalog·diagnostics Go 검사, `network` 공개 output schema 검사, dashboard Node 검사 및 Linux CI browser 검사, Skill/release 검사. 자동 browser 검사가 실제 PC의 사용자 조작 전체를 검증하지는 않는다. |
| teardown·transfer | `network`: 선택 listener 정리와 공유 CA·다른 backend 보존. `internal/proxy/teardown_test.go`: drift·untrust 실패/미확정 시 key 보존. backup/transfer 검사: host-local private key·인증·live session 제외. native job: 실제 untrust 확인 후 key 제거. |
| 실제 플랫폼·provider evidence | Linux/macOS archive·native OS 검사는 CI에서 실행한다. WSL+Windows 브라우저 DNS/CA, Windows mount, 실제 LAN peer·Cloudflared 공개 ingress·두 장치 Tailscale은 실제 대상 환경의 evidence를 별도로 요구한다. |

## 실패 주입의 책임 경계

hosts helper는 Go integration에서 임시 파일에만 쓰도록 주입한다. 이는 실제 helper 실행·sudo·OS
저장소 검증과 역할이 다르며 native job이 후자를 확인한다. 저장/영수증 실패의 결정적 재시도는
`internal/proxy/lifecycle_test.go`의 anchor/receipt 검사와 `internal/netops/operations_test.go`의
미완료 phase 재개 검사에서 확인한다. 설치 CLI에서는 provider commit 직후 record 경로에
디렉터리를 놓아 atomic replace를 실패시킨다. 개발 호스트의 hosts·trust·capability를 바꾸지 않는다.

같은 요청의 재시도는 operation 계약에 따른다. listener의 미완료 bind는 같은 UUID로 재개하고,
provider 소유권이 남은 불확실 exposure는 `exposure_recovery_required`로 거절한다.
후자는 명시 stop으로 소유 설정·pin을 정리한 뒤 새 start 요청으로 복구한다.

검사 조합은 각 경계와 연결 흐름을 확인한다. 모든 OS·provider·fault의 전 조합을 검사했다고
해석하지 않는다. 외부 계정과 대상 장치가 필요한 실접속 검사는 자동 모의 검증과 별도로 남는다.

장기 연결의 source/identity watcher는 1초 ticker로 재검사한다. 매 요청 검사도 수행한다.
스케줄링을 포함한 실제 종료 대기는 시나리오에서 제한 시간을 두며, stop 성공 뒤에는 대상
stream의 종료와 다른 stream의 새로운 응답을 관찰한다.
