# 설치된 release 검증

[Documentation index](../docs/README.md) · [Development](../docs/development.md)

devtools의 설치 후 공개 CLI 흐름을 실제 패키징된 실행 파일로 검증한다.
아래 Unix runner는 macOS/Linux/WSL용 테스트 release를 만들고, 각 시나리오를 독립적인
HOME·XDG 경로·작업 디렉터리에서 실행한다. Windows native는 아래 전용 Actions 검사를 사용한다.

GitHub Actions에서는 fresh hosted runner가 OS 환경 격리를 담당한다. 로컬 실행도 사용자 데이터와
Git 전역 설정을 별도 임시 경로로 분리하므로 현재 devtools 상태를 건드리지 않는다.

## 실행

저장소 루트에서 현재 소스로 CLI를 빌드하고 PATH에 먼저 등록한다.

```sh
go build -o bin/devtools ./cmd/devtools
export PATH="$PWD/bin:$PATH"
```

```sh
# 전체 시나리오
devtools run test:installed

# 설치·업데이트 시나리오
devtools run test:installed install

# 작업 관리·인계·대시보드 시나리오
devtools run test:installed tasks

# 환경 진단·명령 실행 전 필수 조건 시나리오
devtools run test:installed doctor

# 포트 할당·실제 서버 실행·프로젝트 간 참조
devtools run test:installed ports

# reverse proxy route·WebSocket·daemon 재시작
devtools run test:installed proxies

# fresh batch·peer restart·CA 검증 HTTPS·선택 listener 정리·global foreground·격리
devtools run test:installed network

# 외부 계정 없이 provider 장애·복구와 실제 WebSocket·HTTP/2 연결 검증
devtools run test:installed network_failures tailscale_simulated cloudflared_faults http2_exposure

# 암호화 백업·복구·재시도·충돌·직전 백업
devtools run test:installed backup

# Profile 탐색·값 비노출·recipient·import preview/apply와 동시 replay
devtools run test:installed profiles

# 실제 서버의 다중 시작·readiness·재사용·worktree 격리·부분 재시도
devtools run test:installed project_lifecycle

# 프로세스 준비 확인·대기·취소·실행 기준 유지
devtools run test:installed readiness

# 시나리오 목록
devtools run test:installed --list
```

기본 실행은 현재 OS/architecture용 `0.0.0-test.1`, `0.0.0-test.2` CLI release와
`0.0.0-test.1` Agent Skill artifact를 임시 디렉터리에 만든다. 모든 시나리오가 끝나면
release와 사용자 데이터가 자동으로 삭제된다. 시나리오가 실패해도 runner가 격리 HOME에서
시작한 proxy, dashboard, managed process를 정리한 뒤 임시 데이터를 삭제한다.

각 시나리오는 진행 로그를 즉시 출력한다. 180초 안에 끝나지 않으면 해당 시나리오의
이름과 제한 시간을 stderr에 출력하고, 관리 프로세스를 정리한 뒤 종료 코드 124로
실패한다. 다음 시나리오는 실행하지 않는다.

Go, Python 3, POSIX sh, tar, Git, curl과 OpenSSL이 필요하다. completion 시나리오는 설치된
bash·zsh·fish를 감지해 사용할 수 있는 shell만 검증한다. Release CI의 Linux runner는
zsh와 fish를 명시적으로 설치해 세 shell을 모두 확인한다.

각 시나리오는 임시 HOME의 `.local/bin/devtools`에 release를 설치한다. macOS 데이터는
해당 HOME 아래 `Library/Application Support/devtools`, Linux 데이터는 격리된 XDG 경로에
저장한다. `GIT_CONFIG_GLOBAL=/dev/null`과 `GIT_CONFIG_NOSYSTEM=1`도 적용한다.

## Windows native Actions

`.github/workflows/windows-platform.yml`의 full gate는 x64와 ARM64에서 실행한다.
release workflow도 이를 필수 dependency로 호출한다. 동일 main push SHA의 전체 성공 근거가 있으면
소스 검사를 재사용하고 installer·최종 버전의 product lane은 다시 실행한다. 누락·오류 시 전체 재실행한다.
자세한 조건은 [개발 가이드](../docs/development.md#릴리스)를 따른다. 이 작업의 최종 Windows 실행 근거는
Actions이며, Unix의 verify/run.py·HOME/XDG 격리·POSIX shell 절차를 대신 사용하지 않는다.

- suite·boundaries·product lane을 x64/ARM64에서 병렬 실행하고 x64 race는 별도 lane으로 실행한다.
  PS5.1·native PS7·ARM64 위 x64 PS7 설치 검사는 각각 별도 runner에서 병렬 실행한다.
  PATH와 기본 설치 경로를 사용하는 전체 시나리오를 엔진마다 그대로 유지한다.
  Go module/build cache를 복원하되 실제 테스트는 `-count=1`로 다시 실행한다.
- 전체 native Go/복구/보안/console/프로세스 suite, 실제 PS5.1/7·npm/pnpm shim,
  installer/update 중단·원복, constrained hosts/trust/provider 검사를 실행한다.
- x64는 호환 cgo/mingw-w64 race 검사를 실행하고 ARM64는 native contention으로 검증한다.
  일반 suite의 대용량 maintenance 선택 항목, handle 누수 실패 주입, lifecycle/TLS 반복 검사는
  boundaries lane에서 명시 실행한다. 일반 패키지별 테스트·vet은 전체 suite로 합친다.
- scripts/test-windows-product.py는 같은 job의 실제 candidate ZIP을 설치해 alias,
  값·secret·task, project/proxy/restart/stop, CA 검증 TLS와 암호화 profile 전송을 확인한다.
  private state는 DEVTOOLS_ISOLATED_ROOT에 분리한다. typed pending만 같은 UUID로 재개한다.
- 실제 packaged CLI와 Chromium으로 로그인·값 수정·reload·CLI 확인·401/403을 검증한다.
  CLI/Node/browser CPU를 구분하고 ARM64 위 x64 browser 에뮬레이션도 그대로 기록한다.
- release-assets에는 해당 native job에서 검증한 Windows ZIP을 checksum 대조 후 그대로 포함한다.
  focused dispatch는 진단용이며 full 최종 gate를 대체하지 않는다.
  결과는 `windows-platform-{arch}-{lane}` artifact에 보관한다. installer lane은
  `installer-ps5`·`installer-ps7`·ARM64의 `installer-x64`로 구분한다.
  새로운 push/PR은 이전 CI를 취소하지만 release 실행은 취소하지 않는다.

Release의 `profile-exchange`는 이미 빌드한 Linux·macOS·Windows x64/ARM64 CLI와 합성 데이터로
암호화 archive를 교환한다. 네 native consumer가 다른 세 producer의 archive를 import해 총 12개
방향을 확인한다. 값·secret·env override·task·workstream·validation, preview/apply/retry를 검사한다.
artifact 전송이 mode/ACL을 보존하지 않으므로 hash 확인 후 테스트 소유 복사본에 원래 private 권한을
복원한다. importer의 권한 검사는 유지한다. 경로 문자열은 그대로 보존하며 자동 변환은 약속하지 않는다. 별도 빌드는 하지 않고
package 작업과 병렬 실행하되 게시 전에 성공을 요구한다. 실제 사용자 데이터는 사용하지 않는다.

실제 PC의 UAC·CurrentUser trust/browser 통합·터미널 닫기·설치/update 관찰과 최종 WSL2/Windows
browser 연계는 NOT RUN이다. runner browser 성공이나 과거 standalone probe로 그 범위를
확대하지 않는다. [지원 범위와 한계](../docs/windows.md)를 함께 확인한다.

## 기존 release 검증

이미 만든 테스트 release를 사용할 때는 runner를 직접 실행한다.

```sh
python3 verify/run.py --releases /path/to/releases all
```

`--releases` 디렉터리에는 선택한 호스트용 CLI archive와 checksum이 있어야 한다.
`discovery` 시나리오를 실행한다면 Agent Skill archive와 checksum도 포함한다.

## 검증 경계

Go unit test는 port 가용성이나 실제 listener가 테스트 목적이 아닌 경우 probe/listener를
주입해 결정적으로 실행한다. 실제 TCP bind, occupied/free 판정, IPv4/IPv6 listener,
설치·업데이트, detached process, proxy HTTP/WebSocket 같은 OS 동작은 이 시나리오에서
패키징된 실행 파일로 검증한다.

GitHub-hosted runner는 job마다 새 VM을 사용하므로 release 검증의 공식 clean OS 환경이다.
별도 sandbox 제약은 release gate에 포함하지 않는다. 외부 네트워크가 필요한 시나리오는 없으며,
설치 시나리오의 HTTPS 검증도 로컬 테스트 서버를 사용한다.

`tailscale_simulated`는 가짜 `tailscale` 실행 파일과 로컬 HTTP LocalAPI를 사용한다.
패키징된 CLI의 인증·조건부 설정 변경, 사용자 설정 보존, 동시 변경 충돌, 권한 오류,
설정 반영 후 응답 유실, 로컬 record 저장 실패의 provider rollback·명시적 복구,
데몬 강제 종료 뒤 소유 설정 복구를 검증한다.
`cloudflared_faults`는 가짜 provider 프로세스로 시작 실패 시 origin 정리, provider·daemon
SIGKILL, 자식 프로세스 정리, 인증 유실과 실제 WebSocket frame의 선택 종료·다른 연결 보존을
검증한다. 같은 이름의 global route 삭제·재생성 뒤 과거 UUID와 pin의 격리도 확인한다.
`network_failures`는 두 번째 socket bind 실패 후 첫 socket 정리·같은 요청 재시도와
다른 listener의 TLS·WebSocket 연결 보존을 확인한다.
`http2_exposure`는 Go 표준 HTTP client와 실제 CLI TLS listener 사이에서
HTTP/2 협상, stop·TOML·env 파일 변경에 따른 한 연결의 다중 stream 종료,
SNI/authority 검사와 다른 route 연결의 새로운 heartbeat 수신을
검증한다. 같은 HTTP/2 연결에서 서로 다른 exposure의 stream을 선택 종료하는 계약은
Go integration test에서 별도로 확인한다.
기존 `exposure` 시나리오도 TOML·env 파일 변경 시 열린 SSE·WebSocket의 선택 종료와
다른 프로젝트·전역 route 연결 보존을 확인한다.

이 시나리오는 전체 installed suite에 자동 포함되어 Linux·macOS GitHub Actions에서 실행된다.
provider 모의 검증은 실제 Tailscale 두 장치 연결, Cloudflared 공개 DNS·ingress 검증과
구분한다. Windows 브라우저의 DNS·CA 저장소 및 Windows mount의 filesystem 동작도
실제 대상 환경에서 확인해야 한다. 기존 Linux·macOS OS 변경 검증은 별도 system-network
job에서 계속 수행한다.

전체 수용 기준별 검사 위치와 남은 실제 환경 경계는 [network-coverage.md](network-coverage.md)에 기록한다.

## 시나리오 추가

`verify/scenarios/<이름>.py`를 추가하면 runner가 파일명으로 시나리오를 찾는다.
`all`은 등록된 전체 시나리오를 이름순으로 실행한다. 여러 이름을 전달하면 지정한 순서로
실행한다.

시나리오는 제공된 HOME 안에서 필요한 프로젝트·파일·설치 상태를 준비하고 공개 CLI를
사용한다. 앞선 시나리오의 상태에 의존하지 않아야 한다. 성공 시 종료 코드 0, 실패 시
0 이외의 값을 반환한다.

`install`은 설치·업데이트, profile 값, 혼합 dotenv 가져오기, 프로젝트 명령, worktree,
파일 권한, HTTPS 다운로드, 신호 전달을 검증한다. `proxies`는 두 worktree의 route,
404·502·503 응답, WebSocket, 동적 port 변경, daemon 재시작과 listener reservation을
검증한다. `tasks`는 명세·계획·검증 등록, 동시 점유, worktree 공유, 체크포인트·인계·완료
재시도, dashboard의 일회용 링크·세션·종료를 검증한다.
