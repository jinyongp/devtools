# 설치된 release 검증

[Documentation index](../docs/README.md) · [Development](../docs/development.md)

CI·로컬 검사 선택과 수동 진단의 구분은 [검증 구성](../docs/verification.md)을 따른다.

`python3 -B verify/run.py cli_guidance`는 protocol 13 archive를 설치해 A/B isolation
root의 후속 행동, direct argv 위치, 신규 profile effects, kind 충돌, revision/request
복구, help/status 계약, import·설정 safe facts와 실제 managed launcher의 redaction
한도를 검사한다. 기존 Release의 Linux/macOS `test:installed` 전체 선택에 포함된다.
Go의 한도별 경계·손상 frame 검사와 native Windows suite를 대체하지 않는다.

`python3 -B verify/run.py sandbox_queries`는 설치 archive로 공개·legacy·보호 저장소의
메타데이터 조회를 실제 읽기 전용 sandbox에서 검사한다. Linux는 bubblewrap,
macOS는 sandbox-exec가 필요하다. 도구·정책 준비 실패는 skip하지 않는다.
쓰기 open·생성·변경·rename·unlink 차단 대조, 기존 writer 배제, 저장소 inventory 불변,
누락 잠금·pending/orphan의 실패와 외부 복구 후 재조회까지 검사한다.
기존 Unix validation의 macos-15/ubuntu-24.04 job이 source·환경·binary hash·결과를 보존한다.
이 Seatbelt 증거는 실제 Codex macOS 세션의 성공 여부와 구분한다.

## Framework·Library·Provider 호환성

protocol 12의 기본 준비는 `devtools run test:installed proxy_auto`로 검사한다.
실제 archive CLI와 서버로 auto/off/disabled/force, HTTP·CA 지정 HTTPS, 혼합 batch의
peer readiness, 두 worktree, 모드 재사용과 구 receipt replay를 확인한다. OS hosts·trust는
변경하지 않는다. Unix native handoff job이 Linux/macOS에서 실행하고 Windows product는
기본 `project up`의 실제 HTTPS를 확인한다. WSL browser job은 같은 자동 준비 시나리오를
WSL2에서 실행한 뒤 기존 Windows browser DNS·trust 경계를 검증한다.
현재 commit의 native 실행 결과가 있어야 해당 플랫폼의 통과로 인정한다.

Host 정책은 `check:proxy-compat`로 실제 개발 서버·브라우저를 연결해 검사한다.
Node 22.22.2, pnpm 11.24.0, root의 Playwright 1.63.0과 fixture lockfile이 필요하다.
직접 의존성 버전은 [fixture manifest](fixtures/proxy-compat/package.json)에 고정되어 있다.
설치된 candidate archive를 풀어 `--binary`로 지정한다. baseline은 공식 v0.31.0 archive의
checksum을 검증한 뒤 사용한다. 비교 결과에는 CLI identity·binary/lock hash를 기록한다.

Linux의 DNS·CA 신뢰는 일회용 브라우저 컨테이너에 준비한다. 개발 호스트의 trust store나
hosts 파일은 변경하지 않는다. HTTPS 검증을 끄는 옵션은 사용하지 않는다.

```sh
corepack pnpm install --frozen-lockfile --ignore-scripts
corepack pnpm --dir verify/fixtures/proxy-compat install --frozen-lockfile --ignore-scripts
docker build -t devtools-proxy-browser:1.63.0 verify/proxy-browser
devtools run check:proxy-compat --binary /absolute/candidate/devtools --baseline /absolute/baseline/devtools --client-mode docker --artifacts /tmp/proxy-evidence
```

기본 실행은 8개 소비자 × HTTP/HTTPS × rewrite/preserve의 Chromium matrix다.
baseline에는 rewrite만 적용한다. Nuxt는 실제 DevTools Pages/Components RPC 응답·HMR·동일
Origin 경고를 검사한다. 의도적인 다른 Origin probe의 경고는 별도로 센다.
React/Vue/Next/webpack의 counter 상태와 문서 유지, SvelteKit의 module-store 상태·SSR·탐색·form,
ws echo/close, Socket.IO polling→upgrade·websocket-first·ack·재연결을 확인한다.
브라우저 WS가 backend 포트로 직접 연결하면 실패한다.

CI는 기존 `--scheme http`·`--scheme https`로 나누어 각 작업에서 모든 소비자를 실행한다.
각 작업은 baseline 8개·candidate 16개이며 총 48개 조합을 유지한다.
같은 scheme의 baseline과 candidate rewrite/preserve는 같은 작업에 둔다.
로컬에서는 `--scheme`을 생략하면 전체를 실행하고 `--case`로 범위를 먼저 좁힐 수도 있다.
새 소비자도 두 작업에서 자동 실행한다. 각 결과의 `elapsed_ms`에는 준비·정리 시간도 포함한다.

Firefox·WebKit의 핵심 subset은 다음처럼 실행한다.

```sh
devtools run check:proxy-compat --binary /absolute/candidate/devtools --case nuxt --case vite-react --case ws --case socketio --scheme https --browser firefox --browser webkit --client-mode docker
```

실제 공유는 명시 `--live`에서만 시작한다. 아래 LAN 검사는 Docker bridge의 별도 네트워크
클라이언트를 사용한다. 물리 장치나 다른 OS의 DNS·신뢰·방화벽 상태를 대신 검증하지 않는다.

```sh
devtools run check:proxy-compat --binary /absolute/candidate/devtools --case nuxt --case ws --case socketio --scheme https --provider lan --live --auth-mode basic --client-mode docker
```

`--auth-mode none`은 명시 no-auth control이다. Cloudflared 2026.10.3을 미리 준비한 뒤
`--provider cloudflared --live`로 실제 공개 ingress를 검사한다. Quick Tunnel SSE는 미지원으로
제외한다. 공개 URL의 DNS·edge readiness도 실제 클라이언트의 검증된 HTTP 응답으로 확인한다.
실패한 기능 검사를 자동 재시도하지 않는다.

Tailscale은 host와 준비된 두 번째 tailnet 장치 모두 1.104.2여야 한다.
`--provider tailscale --live --peer-network container:tailscale-peer`를 사용한다.
두 장치 IP가 같거나 로그인·HTTPS·전용 Serve 포트 8443/8444가 준비되지 않았으면 실패한다.
Serve 설정의 전후 일치와 소유 handler 정리를 검사한다. 모의 LocalAPI 검사를 실제 tailnet
결과로 기록하지 않는다. Actions의 명시 tailscale job은 GitHub OIDC로 runner와 peer를 각각
ephemeral·pre-approved 장치로 등록한다. 개인 계정·개인망과 분리한 전용 테스트 tailnet을 사용한다.
OIDC 설정이나 HTTPS/Serve 권한이 없으면 전제 부재로 실패하며 pass/skip으로 바꾸지 않는다.

### Tailscale CI의 최초 OIDC 설정

장기 auth key나 OAuth client secret을 저장하지 않는다. 테스트 tailnet에 federated identity를
한 번 등록하면 이후 실행에서는 GitHub의 짧은 수명 토큰으로 장치를 등록한다.
[Tailscale의 공식 GitHub 연동](https://tailscale.com/docs/integrations/github/github-action)과
[workload identity 설정](https://tailscale.com/docs/features/workload-identity-federation)을 따른다.

1. 테스트 tailnet의 Access controls → Tags → Create tag에서 Tag name은 `ci`, Tag owner는
   목록의 `autogroup:admin`을 선택해 저장한다. CI 기기 간 TCP 8443/8444를 허용한다.
   전용 테스트망의 최소 정책 예시는 다음과 같다.

   ```json
   {
     "tagOwners": {"tag:ci": ["autogroup:admin"]},
     "grants": [
       {"src": ["tag:ci"], "dst": ["tag:ci"], "ip": ["tcp:8443", "tcp:8444"]}
     ]
   }
   ```

2. [Trust credentials](https://console.tailscale.com/admin/settings/trust-credentials)에서
   Credential → OpenID Connect를 선택하고 아래 값으로 등록한다.

   | 항목 | 값 |
   |---|---|
   | Description | `devtools-ci` |
   | Issuer | GitHub: `https://token.actions.githubusercontent.com` |
   | Subject | `repo:jinyongp@21158729/devtools@1359600141:ref:refs/heads/*` |
   | Custom claim | `event_name` = `workflow_dispatch` |
   | Scope | Auth Keys의 Write (`auth_keys`) |
   | Tags | 목록에서 `tag:ci`를 선택한다. 검색 칸에 입력만 한 상태로는 등록할 수 없다. |

   Subject의 저장소 접두어는 현재 GitHub 설정을 조회해 확인한다.

   ```sh
   gh api repos/jinyongp/devtools/actions/oidc/customization/sub
   ```

   이 저장소는 `use_immutable_subject: true`이며 `sub_claim_prefix`에 소유자·저장소 ID가
   포함된다. 다른 저장소에 적용할 때도 그 저장소의 실제 접두어에 `:ref:refs/heads/*`를 붙인다.
   이름만 넣은 구 형식은 [immutable Subject](https://github.blog/changelog/2026-04-23-immutable-subject-claims-for-github-actions-oidc-tokens/)와 일치하지 않는다.

3. 생성된 Client ID를 GitHub 저장소 Settings → Secrets and variables → Actions → Variables의
   `TS_OAUTH_CLIENT_ID`로 저장한다. Client ID는 비밀이 아니다. Audience는 Tailscale의 생성 규칙인
   `api.tailscale.com/<Client ID>`를 workflow에서 구성한다.
4. 테스트 tailnet의 MagicDNS와 HTTPS certificates를 활성화하고 Serve를 허용한다.
5. Unix 또는 compatibility workflow를 해당 branch에서 `live_provider=tailscale`로 dispatch한다.
   Subject와 custom claim은 이 저장소의 branch에서 명시 실행한 job만 허용한다.

OIDC 권한은 Tailscale job과 이를 호출하는 reusable workflow job에만 전달한다. 개발 서버에는
GitHub token과 OIDC token 발급 환경 변수를 전달하지 않는다. peer의 등록 권한은 `docker exec`의
일시적 환경으로만 전달하며 container 설정에 저장하지 않는다. 종료 시 peer는 로그아웃·제거하고
runner는 Tailscale Action의 종료 처리에서 로그아웃한다. 일회용 runner의 현재 사용자만
Tailscale operator로 지정해 Serve LocalAPI를 변경할 수 있게 한다. Action은 `up`에
`--accept-routes`를 이미 넣으므로 중복 전달하지 않고, 접속 직후 `set --accept-routes=false`로 끈다.
장치 신원은 기본 `--state=mem:`에 두되, HTTPS 인증서 저장에 필요한 `--statedir`는
일회용 runner의 임시 경로로 지정한다. 이 경로의 인증서·개인 키는 artifact에 포함하지 않는다.

### 플랫폼별 브라우저와 evidence

Windows는 disposable Actions에서 `--client-mode local --disposable-trust`로 실행한다.
해당 runner의 hosts에 테스트 hostname의 loopback 항목만 추가하고, 실패 시에도 소유 항목을
제거한다. Chromium과 별도로 동작하는 HTTP/RPC 클라이언트도 같은 공개 이름을 사용한다.
관리자 권한의 일회용 runner에서 테스트 CA만 LocalMachine Root에 넣고 정확한 thumbprint로
제거한다. CurrentUser Root의 사용자 확인 창은 별도 수동 검증 항목이다. 일반 개발 호스트에서
자동 신뢰 등록하지 않는다. `wsl_proxy_compat.py`는 고유 WSL2 distro에서 Linux CLI·서버를 실행하고
Windows Chromium으로 실제 HTTPS·RPC·WS를 확인한다. 소유 distro만 종료·삭제하며 kernel·가상화
전제 부재를 WSL1이나 cross compilation의 성공으로 대체하지 않는다.

결과는 `summary.json`, 개별 `result.json`, `browser-result.json`, `browser-trace.json`, 정제한
서버 로그와 실패 screenshot에 남긴다. trace에는 이벤트 종류·프레임 바이트 수·선택한 RPC/DOM
사실만 기록한다. 인증 헤더·Vite token·전체 RPC payload를 담는 원본 Playwright trace는 저장하지 않는다.
정리가 미확정이면 실패로 남기고 private `recovery_root`를 보존한다. 이를 CI artifact에 올리지 않는다.

브라우저 의존성 없이 설치 CLI의 기본값·invalid enum·UUID·실제 HTTP/WS·공유 중지·rewrite 복구는
`test:installed proxy_policy`로 검사한다. 별도 `python3 -B verify/proxy_policy_contract.py --binary CANDIDATE --baseline BASELINE`은
실제 구 daemon 거절·명시 restart·구 strict reader의 중지 기록 읽기와 downgrade 후 HTTP/WS도 확인한다.

`.github/workflows/proxy-compat.yml`은 Linux 전체·Firefox/WebKit subset·LAN peer·Windows native browser·
실제 WSL2 browser를 연결하며 Unix workflow의 필수 gate에서 호출한다.
공개 Cloudflared/Tailscale job은 Unix 또는 compatibility workflow dispatch의 `live_provider`를
명시해야 실행한다. macOS arm64·Windows x64/ARM64 Go/CLI는 기존 Unix/Windows native workflow가 담당한다.

## 설치 시나리오의 실행 범위

devtools의 설치 후 공개 CLI 흐름을 실제 패키징된 실행 파일로 검증한다.
아래 Unix runner는 macOS/Linux/WSL용 테스트 release를 만들고, 각 시나리오를 독립적인
HOME·XDG 경로·작업 디렉터리에서 실행한다. Windows native는 아래 전용 Actions 검사를 사용한다.

GitHub Actions에서는 fresh hosted runner가 OS 환경 격리를 담당한다. 로컬 실행도 사용자 데이터와
Git 전역 설정을 별도 임시 경로로 분리하므로 현재 devtools 상태를 건드리지 않는다.

Windows의 간헐적인 저장소 신원 검사 실패는 별도 [storage identity trace 도구](probes/storage-identity/README.md)로
조사한다. 배포 없이 고정 소스의 진단 빌드를 만들고, 원래 비교값과 파일 신원을 기록할 수 있다.
실제 저장소 조회와 일회용 fixture 검사는 구분하며 결과는 대상 프로젝트의 `.tmp` 안에 둔다.

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

# 설정 편집·revision 충돌·주석 보존 migration·전체 명령 진단
devtools run test:installed config_edit

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

# vault 설정·재사용 세션·잠긴 metadata·JSON/schema/completion
devtools run test:installed vault

# 저장값 변경 알림·명시 자동 restart·TLS proxy·readiness
devtools run test:installed value_updates

# 승인된 로컬 실행기 전달·응답 유실·로그 보호·교체 실행기의 결과 복구
devtools run test:installed value_handoff

# 실제 두 PTY의 공유 세션·daemon crash 뒤 수동 잠금 유지
devtools run test:installed vault_session

# 출력 보호·binary stdout 예외·stderr·캡처·PTY·느린 소비자 취소
devtools run test:installed output_redaction

# 프로세스 준비 확인·대기·취소·실행 기준 유지
devtools run test:installed readiness

# 시나리오 목록
devtools run test:installed --list
```

기본 실행은 현재 OS/architecture용 `0.0.0-test.1`, `0.0.0-test.2` CLI release와
`0.0.0-test.1` Agent Skill artifact를 임시 디렉터리에 만든다. 모든 시나리오가 끝나면
release와 사용자 데이터가 자동으로 삭제된다. secret을 사용하는 시나리오는 명시
passphrase vault를 준비하며 native keyring을 변경하지 않는다. 테스트가 만든 키 세션은
해당 private endpoint의 OS 사용자와 challenge를 확인한 value-free control로 종료한다. 시나리오가 실패해도 runner가 격리 HOME에서
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
소스·합성 버전 installer 호환성 검사를 재사용하고 최종 버전의 product lane 2개만 다시 실행한다.
설치 엔진 5개 중 하나라도 누락·실패·skip됐거나 근거 조회에 오류가 있으면 전체 재실행한다.
자세한 조건은 [개발 가이드](../docs/development.md#릴리스)를 따른다. Windows native 실행은
Actions에서 검증하며, Unix의 verify/run.py·HOME/XDG 격리·POSIX shell 절차를 대신 사용하지 않는다.

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

PowerShell 크래시의 반복 재현은 수동 `focus=powershell`/`installer` 진단에서만 요청한다.
일반 full 실행은 기존 installer 시나리오를 엔진당 1회 실행하며 startup·stdio 반복 probe는 0회다.
`installer_repetitions`(1–20), `powershell_startup_probes`(0–1000),
`powershell_stdio_probes`(0–2000)를 지정해 반복하고 `installer_package_commit`으로
원래 패키지 SHA를 고정할 수 있다. 이 실행은 full gate나 재사용 근거를 대체하지 않는다.
실패는 자동 재시도하지 않는다. 호출 위치·종료 코드·stdout/stderr·Application 이벤트와
WER minidump를 artifact에 수집한다. dump 설정은 일회용 Actions VM에만 적용한다.

2026-10-23 조사 기준, [최초 Release 실행](https://github.com/jinyongp/devtools/actions/runs/39821194100)의
ARM64 위 x64 PowerShell 7.6.6 `0xC0000005` 원인은 미확정이다. 당시 stderr는
`Fatal error. 0xC0000005`뿐이며 dump/stack이 없었다.
[원래 소스의 installer 8회 검사](https://github.com/jinyongp/devtools/actions/runs/39831751407)와
[세 환경의 실제 subprocess 함수 총 3,000회 검사](https://github.com/jinyongp/devtools/actions/runs/39836188801)는
통과했으나 최초 오류의 해결 근거로 간주하지 않는다. 제품 설치기 수정 없이 진단 수집을 보강했고,
재발 시 확보한 dump와 이벤트로 실패 모듈·호출 경로를 확인한다.

Release의 `profile-exchange`는 이미 빌드한 Linux·macOS·Windows x64/ARM64 CLI와 합성 데이터로
암호화 archive를 교환한다. 네 native consumer가 다른 세 producer의 archive를 import해 총 12개
방향을 확인한다. 값·secret·env override·공유 bundle ID와 membership·논리 값/슬롯 버전,
task·workstream·validation, preview/apply/retry를 검사한다. 대상의 profile ID·저장 세대·keyset은 새로 만든다.
artifact 전송이 mode/ACL을 보존하지 않으므로 hash 확인 후 테스트 소유 복사본에 원래 private 권한을
복원한다. importer의 권한 검사는 유지한다. 경로 문자열은 그대로 보존하며 자동 변환은 약속하지 않는다. 별도 빌드는 하지 않고
package 작업과 병렬 실행하되 게시 전에 성공을 요구한다. 실제 사용자 데이터는 사용하지 않는다.

실제 PC의 UAC·CurrentUser trust/browser 통합·터미널 닫기·설치/update 관찰과 최종 WSL2/Windows
browser 연계는 NOT RUN이다. runner browser 성공이나 과거 standalone probe로 그 범위를
확대하지 않는다. [지원 범위와 한계](../docs/windows.md)를 함께 확인한다.

var/sec의 실제 Keychain·DPAPI·Secret Service·다른 UID/SID·logout·소유 WSL2 재시작은
[native 수용 검증 절차](../docs/development.md#varsec-통합-수용-검증)를 따른다.
일반 installed 시나리오의 passphrase fixture와 OS별 native 검증을 구분한다.
WSL vault 재시작 통과를 Windows 브라우저 네트워크 검증으로 확대하지 않는다.

## 독립 플랫폼 probe

`verify/probes/native_windows.go`는 제품 코드와 분리한 Windows API 검사다.
`.github/workflows/windows-platform.yml`을 `focus=api`로 수동 실행하면 x64·ARM64 runner에서
이 파일을 직접 빌드·실행하고 결과를 artifact에 보관한다. 기본 Go test graph에는 포함하지 않는다.
이 probe의 성공이 제품의 full native gate를 대체하지는 않는다. 지원 전 build blocker를
수집하던 별도 `Windows support preflight` workflow는 이 진단으로 통합했다.

`verify/probes/wsl.py`는 WSL에서 Windows PowerShell interop와 임시 loopback 연결을 관찰한다.
현재 WSL 배포판을 종료하거나 네트워크 설정을 바꾸지 않는다.
WSL에서 `python3 verify/probes/wsl.py`로 실행하며, 관측 결과만으로
NAT/mirrored 모드 전체의 지원 여부를 판정하지 않는다.

## 기존 release 검증

이미 만든 테스트 release를 사용할 때는 runner를 직접 실행한다.

```sh
python3 verify/run.py --releases /path/to/releases all
```

`--releases` 디렉터리에는 선택한 호스트용 CLI archive와 checksum이 있어야 한다.
`discovery` 시나리오를 실행한다면 Agent Skill archive와 checksum도 포함한다.
한 번의 실행에서는 release를 한 번 준비하고 시나리오별 HOME·XDG·프로세스 정리는 유지한다.
로그의 `Test release preparation`과 `Scenario duration (including cleanup)`으로
빌드 비용과 시나리오 비용을 구분한다. 재사용할 release의 소스·버전·호스트 일치는 호출자가 확인한다.

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
