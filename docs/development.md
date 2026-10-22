# devtools 개발

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

저장소에서 devtools 자체를 수정하는 개발자를 위한 안내다. 사용자 설치와 첫 실행은
[README](../README.ko.md), 설치된 배포물의 통합 검증은 [검증 안내](../verify/README.md)를 따른다.
CI·로컬 검사의 역할과 수동 진단 구분은 [검증 구성](verification.md)에서 확인한다.

## 개발 도구와 첫 빌드

Go 1.27.x(`go.mod`의 정확한 버전은 1.27.1), Node.js 24.x, Python 3, Git과
`uvx`를 포함한 uv가 필요하다. macOS·Linux·WSL의 설치 시나리오는 curl·OpenSSL·tar·POSIX sh도 사용한다.
프로젝트 명령은 `devtools.toml`에 `{목적}:{명령}` 이름으로 선언되어 있다.
먼저 이 checkout의 CLI를 빌드해 PATH 앞에 둔다.

macOS·Linux·WSL:

```sh
go build -o bin/devtools ./cmd/devtools
export PATH="$PWD/bin:$PATH"
devtools version
devtools command list
devtools run check:all
```

Windows 네이티브 PowerShell:

```powershell
go build -o bin/devtools.exe ./cmd/devtools
$env:PATH = "$(Get-Location)\bin;" + $env:PATH
devtools version
devtools command list
devtools run check:all
```

`version`으로 선택된 실행 파일을 확인하고, `check:all`이 종료 코드 0으로 끝나는지 확인한다.
다시 빌드할 때는 `devtools run build:cli`, 입력·출력 계약은 `devtools schema COMMAND`로 조회한다.
사용자용 실행 파일에는 이 개발 도구들이 필요하지 않다.

## 변경에 맞는 검사 선택

`devtools run check:all`은 Skill·dashboard JavaScript·설치기·릴리스·프로젝트 명령·성능 runner의
회귀 검사, Go 형식·vet·테스트를 실행한다. 플랫폼별 Go 검사 범위는 다음과 같다.

| 환경 | 일반 검사 | 추가 제품 검증 |
| --- | --- | --- |
| macOS·Linux | Go race 포함 | 설치된 release 전체 시나리오, Linux Chromium smoke |
| Windows x64 | 일반 Go 테스트 | 필수 Actions의 cgo·호환 mingw-w64 race와 native 제품 검사 |
| Windows ARM64 | 일반 Go 테스트 | 필수 Actions의 native 프로세스·잠금 경쟁 검사; Go race detector 없음 |
| WSL | Linux 빌드와 검사 | 실제 WSL2/Windows 네트워크는 별도 미검증 범위 |

macOS·Linux CI는 OS마다 정적 검사와 네 개의 Go race 작업(`cli`, `values`, `vault`,
`rest`)을 병렬로 실행한다. `valuehandoff`·`runnerengine`은 별도 `handoff` 작업이
네 개의 Unix OS·아키텍처에서 실행하며 `rest`에는 중복 포함하지 않는다.
새 패키지는 자동으로 `rest`에 포함한다. 각 작업은 `devtools run check:static` 또는
`devtools run test:go-lane cli`·`handoff`처럼 로컬에서도 실행할 수 있다.
`handoff`는 캐시된 테스트 결과를 재사용하지 않으며 `check:all`의 전체 패키지 범위는 유지한다.
Release에서는 설치 시나리오·프로필 교환용 export도 Go 검사와 병렬로 실행하며,
Linux의 Chromium·Skill discovery 검사는 별도 작업이다. 모든 작업이 성공해야
패키징·게시로 진행한다. Go 캐시는 검사 작업별로 분리한다.

Unix의 네 Go race 작업은 성공·실패 모두 `unix-tests-<OS>-<lane>-attempt-<번호>`
아티팩트에 테스트 로그, 실제 소스 SHA, 시작 시각, Go·OS 이미지·CPU 아키텍처·디스크 정보를
30일간 보존한다.
재실행은 새 attempt 이름으로 저장하므로 첫 실패의 근거를 덮어쓰지 않는다.
설정 내용과 전체 환경 변수는 수집하지 않는다. 설정 포맷 테스트가 실패하면
첫째·둘째 평가를 구분하고 오류 코드·`facts.stage`·`facts.os_code`를 확인한다.
프로젝트 파일의 읽기·메타데이터·게시 단계와 macOS ACL 시스템 호출을 구분하며,
게시 후 관찰 실패는 `causes`에 원래 읽기 오류를 남긴다.

Windows의 전체 Go suite는 x64·ARM64 각각 `suite-cli`, `suite-values`, `suite-tasks`,
`suite-handoff`, `suite-runtime`, `suite-rest` 여섯 작업으로 병렬 실행한다.
긴 CLI·값 처리·작업 관리 패키지를 따로 실행하고, 값 전달과 프로세스·서버 검사도 분리한다.
새 패키지는 자동으로 `suite-rest`에 포함한다. 분할한 전체 패키지의 테스트와 vet가
모두 성공해야 Windows 필수 검사가 통과하며, Release의 소스 검증 재사용도 같은 여섯 작업을
양쪽 아키텍처에서 확인한다. 실제 사용자·ACL 검사는 `suite-rest`, 패키지 관리자 shim과
ARM64의 x64 provider fixture는 `suite-runtime`에서 준비한다.

각 runner 안에서는 `go test -p 1`을 유지한다. 실제 Argon2 파생이 runner의 CPU를
동시에 점유해 프로세스 재시작·IPC 검사의 기한을 넘기는 일을 피하면서, 별도 runner 간에는
동시에 실행한다. 테스트 범위와 제한 시간은 유지하며, 실행한 패키지 목록은 작업별
`windows-go-packages.json` 아티팩트에 남긴다.
로컬 Windows에서는 `python -B scripts/project.py windows-go-packages suite-runtime`으로
같은 패키지 목록을 확인할 수 있다.

로컬 `check:all`은 disposable Actions의 사용자·ACL·OS 경계 검사를 대체하지 않는다.

조건부 값 변경·생성의 복구 경계는 다음 검사로 확인한다.
`go test -race ./internal/maintenance ./internal/values -run 'Test(ValueReceiptTransactionSurvivesRealProcessKill|ManagedMutationRealProcessResponseLossAndCandidateKill|SecretGenerate)'`는
저장 엔진의 candidate·journal 준비·첫 교체·전체 교체·durable commit·응답 전 경계에서
owned 프로세스를 강제 종료하고 새 reader로 복구한다. 저장 엔진 검사는 opaque
profile/receipt 쌍의 원자성을, 실제 values/session 프로세스 검사는 생성 후보와
commit 후 응답 유실·나중 writer 보존을 검증한다. production 환경변수 failpoint는 없다.
프로세스 종료 검사는 갑작스러운 전원 제거를 검증한 결과가 아니다.
`python3 -B verify/run.py secret_mutations`는 같은 소스의 패키지를 설치한 뒤 CLI guard,
응답 유실 재시도, 생성값의 실제 child 전달과 passive lookup을 검사한다.
새 시나리오는 파일 이름으로 자동 등록된다. Windows 설치물은 native product journey에서
같은 guard·생성·lookup을 검사하며, 해당 native job 결과를 로컬 Linux 결과로 대신하지 않는다.

`python3 -B verify/run.py value_handoff`는 설치한 CLI와 별도로 빌드한 호출자 소유 실행기를
연결한다. 준비·명시 승인·1회 전달·응답 유실 재시도·로그 마스킹·실행기 교체 후 결과 보고를
실제 helper 실행 횟수와 비교한다. 잠금 뒤 이전 ticket을 재사용할 수 없는지도 확인한다.
외부 계정이나 실제 사용자 keyring은 사용하지 않고, 생성한 실행기와 세션만 정리한다.

외부 receiver의 Unix transport 계약은 `go test -race ./internal/cli ./internal/vault
-run 'Test(ExternalRunner|RunnerDial|RunnerListener)'`로 확인한다. Linux의 독립 receiver는
CLI address 응답만 받아 socket을 만들고 내부 listener·wire helper 없이 등록·전달·ACK 유실과
재시도를 검사한다. 내부 transport 디렉터리가 끝까지 없는지도 확인한다. Unix listener 검사는
기존 endpoint와 종료 중 교체된 endpoint를 보존하는지 확인한다. 다른 UID·소유권 검사는
아래 disposable native peer 작업에서 별도로 실행하며 비특권 로컬 검사로 대체하지 않는다.

main의 Unix validation은 기존 전체 race 작업에 더해 Ubuntu 24.04 x64·ARM64,
macOS 15 ARM64·Intel에서 설치 시나리오와 전달·실행기 race 검사를 실행한다.
disposable 호스트의 sudo 검사는 실제 다른 UID를 사용하며 skip을 통과로 인정하지 않는다.
Windows x64·ARM64의 native product 작업도 같은 설치 시나리오를 사용한다.
실제 다른 SID의 새 runner 채널 접근 거절은 vault preflight에서 필수로 확인한다.
runner 이미지별 아키텍처는 [GitHub 공식 표](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)를
기준으로 선택하고, 작업 안에서도 `GOHOSTARCH`를 확인한다. Windows ARM64의 일반 검사는
x64 race 결과나 교차 컴파일로 대체하지 않는다. 각 커밋의 전체 Actions가 통과해야 통합 검증이 완료된다.

Release 검증은 macOS 15, Ubuntu 24.04, Windows Server 2025 x64(테스트 호스트),
Windows 11 ARM64와 Go 1.27.1을 사용한다. 지원 범위와 실제 PC 미검증 한계는
[Windows 안내](windows.md)를 따른다. 실행별 결과는 해당 커밋의 Actions run과
artifact에서 확인한다.

변경 영역만 먼저 확인할 때는 다음 검사를 사용한다.

- `devtools run check:dashboard`: DOM 없는 Node JavaScript 회귀 검사.
- `devtools run check:project`: 집계 검사의 실패 전파와 release packaging 입력.
- `devtools run check:skill`: `skills-ref==0.1.1` 공식 형식 검증.
- `devtools run check:skill-release`: 공식 검증, archive 구성·checksum·소스 일치·잘못된 fixture 거절.
- `devtools run test:installed proxies`: Unix에서 실제 배포 archive를 설치해 HTTP·WebSocket·동적 port·daemon 재시작·reservation 검증.

저장소는 `skills` 1.6.0을 정확히 고정한다. `pnpm install --frozen-lockfile` 후
`devtools run check:skill-discovery`로 로컬 발견을 확인한다.
`check:skill-discovery-public`은 공개 `jinyongp/devtools`를 확인하므로 현재 checkout 검증과 구분한다.
CI의 uv는 공식 Skill 검증에만 사용하며 Python dependency manifest가 없어 setup-uv cache는 비활성화한다.

Go 단위 테스트는 OS가 고른 포트를 먼저 닫은 뒤 재사용하지 않는다. 실제 bind가 대상이 아니면
주입 가능한 probe/listener로 상태와 저장 동작을 검증한다. TCP bind·occupied/free·IPv4/IPv6와
설치된 CLI의 동작은 `devtools run test:installed ports proxies`로 확인한다.

반복 가능한 성능 측정은 `devtools run perf:all`(hyperfine end-to-end) 또는
`devtools run perf:go`(Go benchmark)를 사용한다. 격리 fixture·크기·이력·baseline 비교는
[성능 안내](../perf/README.md)를 따른다. end-to-end runner는 Linux·WSL용이다.

## var/sec 통합 수용 검증

저장·세션·실행 보호는 Release workflow의 native OS job으로 검증한다.
Keychain Intel/ARM, Windows DPAPI x64/ARM64·UID/SID/pipe·설치/업데이트,
Linux Secret Service 가용/부재·logout, 별도 소유 WSL2 배포판 종료·재시작,
12방향 profile/bundle 전송을 각각 확인한다.
로컬 test나 cross compile로 이 실제 검증을 대체하지 않는다. WSL vault 검증은
WSL/Windows 네트워크나 실제 사용자 저장소 이전까지 검증했다는 뜻이 아니다.

`devtools run test:installed vault profiles value_updates output_redaction vault_session`은
실제 패키징/설치한 CLI의 암호 저장·선택·사용처·기본 알림, opted 관리 서버의 TLS proxy
재시작/readiness, 출력 보호와 재사용 세션을 격리 데이터로 확인한다. `vault_session`의
로컬 두 PTY와 실제 distro restart는 별도 근거다. 자동 반영의 실제 프로세스 종료·응답
유실·post-stop 실패와 storm 제한은 Go 통합 검사 및 native suite에도 포함된다.

`perf/convergence.py`는 같은 10/100/1,000개 데이터에서 notify pending과 자동 적용의
수렴 관찰 시간을 각각 보존한다. 서로 다른 결과를 같은 처리량처럼 비교하지 않는다.
실행 준비 latency·RSS·cold/warm·native unwrap·redactor 측정과 함께 [성능 안내](../perf/README.md)를
읽는다. 세션 조회로 수명을 늘리거나 검증·잠금·출력 보호를 생략해 측정하지 않는다.
게시 없는 Actions 검증과 실제 릴리스 게시, 사용자 vault 이전은 별도 작업이다.

## 네트워크 검증과 OS 변경

로컬 네트워크 기능의 사용자 흐름은 [가이드](local-network.md), 저장·복구 계약은
[network design](network-design.md)을 따른다. `test:installed network`는 패키징된 실행 파일로
fresh batch·HTTPS·전역 route·격리·listener 정리를 검증한다. Linux/macOS release matrix는
같은 installed suite를 실행한다. 실제 OS hosts/trust/low-port, Windows browser, LAN peer와
provider 실접속은 별도 환경 근거가 필요하며 unit/fake 결과로 대체하지 않는다.

외부 계정 없이 장애를 재현하려면
`devtools run test:installed tailscale_simulated cloudflared_faults http2_exposure`를 실행한다.
패키징된 CLI와 가짜 provider·로컬 LocalAPI 사이의 충돌·응답 유실·강제 종료 복구,
실제 WebSocket frame과 TLS HTTP/2 stream의 종료·다른 연결 보존을 확인한다.
모의 provider와 실제 전송 검증 범위는 [검증 안내](../verify/README.md#검증-경계)에 구분했다.

`verify/platform_network.py --binary bin/devtools --evidence result.json`은 명시
`DEVTOOLS_DISPOSABLE_HOST=1`인 disposable macOS/Linux에서만 실행한다. 실제 hosts·OS trust·
80/443·최소 capability를 변경하고 정리를 확인한다. 실패하면 private CA와 복구 root를 보존한다.
release workflow의 `system_network_smoke`는 기본 false인 opt-in 입력이다.

OS 변경은 비대화형에서도 사전 승인된 sudo 권한으로 실행한다. 권한 부족은
암호 입력 없이 `permission_required`로 반환한다. 시스템 검증도 PTY 없이 실행한다.
macOS untrust는 선택 CA를 명시적으로 불신 처리한 뒤 키체인 인증서를 삭제한다.
공개 CA의 불신 기록은 남으며, 이후 명시적 trust로 다시 신뢰할 수 있다.
검증은 공개 인증서 재등록 후에도 불신 유지, 다른 CA 신뢰 유지, 재신뢰와 CA 키 삭제를
확인한다. OS 인증 대기는 최대 2분이며, 신뢰 제거 확인 전에는 CA 키와 복구 상태를 보존한다.
workflow는 시스템 동작 검증을 독립된 job으로 실행해 일반 검사·설치 검증·패키징의
결과를 함께 남긴다. opt-in 시스템 검증 실패는 전체 workflow 실패로 유지되며
게시도 차단한다.
실제 결과 파일이 없으면 해당 platform 수용 검증은 pending이다.

## Dashboard 화면 개발

저장소에서 다음 명령을 실행하고 출력된 링크를 연다.

```sh
devtools run dev:dashboard
```

개발 전용 실행 파일을 빌드한 뒤 서버를 터미널에서 실행한다.
`internal/dashboard/assets`의 HTML·JS·CSS를 수정하고 브라우저를 새로고침하면
수정 내용이 바로 반영된다. D3 배포 파일의 버전·출처·license·SHA-256은
`internal/dashboard/assets/dependencies.json`에서 관리한다. 응답은 `Cache-Control: no-store`로 제공한다.
Go 코드를 수정했을 때는 `Ctrl+C`로 종료하고 `devtools run dev:dashboard`를 다시 실행한다.

현재 사용자의 devtools 데이터와 저장소의 profile을 사용한다. 개발 서버는
별도의 임시 registry와 포트를 사용하며 종료할 때 임시 파일을 정리한다.
기존 dashboard와 개발 서버를 동시에 열어둘 수 있다.
격리 데이터로 확인하려면 자식 프로세스에도 전달되는 `DEVTOOLS_ISOLATED_ROOT`를
새 임시 경로로 지정한다. Windows Known Folder는 HOME/XDG 재정의로 격리되지 않는다.
이 값은 private config/data/cache/runtime을 한 root 아래 분리한다.

일반 `devtools dashboard`는 실행 파일에 내장된 화면을 제공한다.
DOM 없이 실행하는 빠른 JavaScript 회귀 테스트는 `devtools run check:dashboard`로 확인한다.
실제 Chromium smoke는 빌드된 CLI와 DEVTOOLS_ISOLATED_ROOT를 사용한다.
HOME/XDG도 테스트 라이브러리의 호환성을 위해 별도 임시 경로로 지정한다.

```sh
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
devtools run build:cli
devtools run test:dashboard-browser
```

Smoke는 dashboard 로그인, Values 화면의 변수 수정, URL navigation과 session 유지 상태의 reload,
CLI에서 확인한 최종 값을 한 흐름으로 검증한다. Release의 Linux CI에서는 Chromium의 OS 의존성도
함께 설치하며 이 smoke가 통과한 뒤 설치된 release 시나리오를 실행한다. Playwright는
`@playwright/test` 1.63.0으로 정확히 고정한다.

## Agent Skill 검증

Skill 원본은 `skills/devtools/SKILL.md`이며 목적별 지침은 같은 디렉터리의
`references/`에 둔다. 원본과 참조를 함께 수정하고 다음 검사를 사용한다.

```sh
devtools run check:skill
devtools run check:skill-release
devtools run check:skill-discovery
```

`check:skill`은 고정 버전의 공식 reference validator로 규약을 검사한다.
`check:skill-release`는 archive 구성·checksum과 전체 원본의 일치를 확인한다.
`check:skill-discovery`는 현재 checkout을 검사하고,
`check:skill-discovery-public`은 별도로 공개 GitHub 소스를 확인한다.
배포 파일에는 필요한 참조를 함께 포함한다. 사용자 설치·업데이트는 [설치 가이드](agent-skill.md)를 따른다.

## 도구 추가

- 기능을 별도 `internal/` package에 두고 `internal/cli`에 명령을 등록한다.
  같은 옵션 정의에서 parsing·검증·help·schema를 생성한다.
- handler는 데이터 또는 `protocol.Error`를 반환하고 runner가 직렬화한다.
- 필요한 명령에서만 project context를 선택한다. 사용자 경로는 `paths`를 사용하고
  handler에는 context와 주입 가능한 입출력을 전달한다.
- 정상 입력·실패 경로·해당 기능의 위험을 검사한다.
- 준비·재시도·작업 조정 방식이 바뀌면 사용자 가이드와 Agent Skill을 함께 갱신한다.
- packaging·저장 호환성·분리된 프로세스 수명이 기능에 포함되면 installed-release 검증을 추가한다.


## main 병합 검사

main의 필수 상태 검사는 GitHub Actions가 생성하는 `Windows validation`과
`Homebrew validation`이다. GitHub의 branch protection 설정에서 관리하며,
실행 workflow가 없는 과거 `auto-merge / auto-merge` 항목은 사용하지 않는다.
자동 병합 기능 자체와 필수 검사는 별개다.

두 workflow는 문서만 바뀐 PR을 포함해 모든 PR에서 실행된다.
Windows 검사는 x64·ARM64 전체 lane의 성공을, Homebrew 검사는 Formula 생성·검증의
성공을 요구한다. 선행 job이 실패·취소·skip돼도 최종 검사는 실행되어 실패를 반환한다.
Windows focused 실행과 릴리스의 소스 검사 재사용 실행은 `Windows focused validation`으로
구분하므로 전체 PR 검사를 대신하지 않는다. Homebrew는 비게시 수동 검사도 지원한다.

Windows 설정 파일의 권한 보존·원자적 교체만 재현하려면 `windows-platform.yml`을
`focus=config`로 수동 실행한다. x64·ARM64에서 `projectfile`과 `configedit` 검사 및 vet를
수행하고 결과를 artifact에 보관한다. 전체 실행에서는 `suite-rest`가 같은 패키지와 vet를
한 번 검사한다. 독립 Windows API 관찰은 같은 workflow의 `focus=api`를 사용한다.

관리자 예외는 기존 설정을 유지한다. 관리자의 직접 push는 필수 검사를 우회할 수 있으므로
일반 변경은 PR의 두 검사 결과를 확인한 뒤 병합한다. 문서 변경에도 Windows 전체 검사를
실행하며, push의 경로 필터는 PR 검사 적용 범위에 영향을 주지 않는다.

## 릴리스

```sh
devtools run release:plan
devtools run release:publish
```

버전 추천은 HEAD에 포함된 실제 게시된 안정 릴리즈를 기준으로 계산한다.
실패하거나 게시하지 않은 태그는 다음 버전의 기준이 되지 않는다. `--publish`는
깨끗한 main을 올린 뒤 버전과 정확한 커밋 SHA로 Release 워크플로를 요청한다.
로컬에서 릴리즈 태그를 만들지 않는다. Node.js와 인증된 GitHub CLI(`gh`)가 필요하다.

`release:plan`은 remote tag를 fetch하며 게시하지 않는다. GitHub CLI에서 공개 안정 태그 이름만
추려 Node.js로 전달하므로 릴리스 본문·자산 metadata가 커져도 버전 조회 버퍼를 채우지 않는다.
`release:publish`도 같은 조회를 사용한다. API 조회가 실패하면 버전 계산도 중단하며
게시되지 않은 태그만으로 기준 버전을 대체하지 않는다.
저장 형식이 바뀌는 릴리스는 [저장 호환성](profiles.md#저장-형식-업그레이드와-구버전-실행)과
[vault 전환](secret-protection.md#기존-저장소를-전환하기)의 백업·복구 절차를 확인한다.
명시 workflow 입력으로 게시할 때는 공개 안정 릴리스 목록을 좁혀 조회하고,
그 태그가 후보의 ancestor인지와 이후 커밋의 feature/breaking 변경을 확인해 버전을 정한다.
최신 공개 태그가 후보에 포함되지 않으면 단순히 버전 숫자만 올리지 않는다.

```sh
gh release list --limit 10 --json tagName,isDraft,isPrerelease,publishedAt
git merge-base --is-ancestor PREVIOUS_TAG CANDIDATE_SHA
git log --format=%s PREVIOUS_TAG..CANDIDATE_SHA
```

`PREVIOUS_TAG`와 `CANDIDATE_SHA`는 확인한 공개 안정 태그와 최종 커밋으로 바꾼다.
0.x의 feature/breaking은 minor, 나머지는 patch를 올리는 것이 현재 script의 규칙이다.
직접 workflow를 요청해도 아래의 source/version·필수 검사·main ancestry 조건은 유지된다.

워크플로는 요청한 전체 commit SHA를 checkout하고 macOS·Linux 검사와 Windows x64·ARM64의
full 제품 검사를 필수 dependency로 실행한다. Windows 소스 검증은 요청 SHA와 workflow SHA가 같고,
같은 저장소 main push의 전체 lane·필수 단계가 성공한 최신 attempt만 재사용한다. PR·fork·focused 실행,
다른 SHA, 누락·실패·취소된 단계는 인정하지 않는다. API 오류나 근거 부재 시 전체 검사를 실행한다.
Actions 요약에 재사용한 run/attempt를 남긴다. `reuse_validation=false`로 전체 재실행을 강제할 수 있다.
재사용하더라도 Windows 설치/update·최종 버전 패키지의 completion·실제품·Chromium 검사는 다시 실행한다.
PowerShell 엔진별 설치 검사는 PATH·기본 설치 경로를 격리하기 위해 각각 별도 runner에서 병렬 실행한다.
이전 candidate ZIP을 이름만 바꿔 게시하지 않는다. 실제 게시인 `publish=true`는
main ref와 main ancestry를 모두 확인한다. `publish=false`는 후보 branch에서도 실행할 수 있다.
Linux는 Agent Skill local/public discovery와 Chromium dashboard smoke를 포함한다.
macOS와 Linux의 `devtools run test:installed`는 시나리오별 180초 제한을 유지한다.

Windows의 필수 검사는 저장소/복구·argv/shim·프로세스/Job·console·네트워크·설치/업데이트·
PowerShell completion·전체 Go suite와 실제 candidate ZIP의 설치·Chromium dashboard를 포함한다.
x64 race와 ARM64 native contention은 구분하며 browser/Node의 실제 CPU도 따로 기록한다.
Windows의 실행 근거는 Actions이며 실제 PC·WSL2 연계 한계는 [Windows 안내](windows.md)를 따른다.

프로필 전송은 같은 실행에서 빌드한 CLI와 합성 암호화 archive를 공유한다. 네 native consumer
(Linux·macOS·Windows x64·ARM64)가 각각 나머지 세 producer를 import한다. OS 조합마다 전체 suite를
다시 빌드하지 않는다. 이 12방향 검사는 packaging과 병렬 실행하며 게시의 필수 dependency다.

모든 필수 job이 통과하면 macOS/Linux의 네 archive를 만들고 Windows native job이 실제 검증한
두 ZIP을 checksum 확인 후 그대로 포함한다. 여섯 OS/CPU 배포물, Agent Skill과 checksum,
설치기·버전 파일의 identity를 확인한다. 양쪽 Unix OS의 Skill도 byte 단위로 대조한다.
이 단계까지 실패하면 새 태그와 GitHub Release는 만들어지지 않는다.

게시 job은 준비된 `release-assets` artifact를 내려받아 태그를 생성하고,
고정 SHA의 `releaseway/actions`로 immutable GitHub Release를 게시한다.
Releaseway의 고정 SHA와 입력은 `.github/workflows/release.yml`,
설치기 배포 입력은 `.github/workflows/pages.yml`에서 관리한다.
같은 버전의 태그가 이미 있으면 동일 커밋인지 확인해 재사용하며, 다른 커밋의 태그는
교체하지 않는다. 릴리즈 노트는 `notes: standard` 기본 템플릿으로
게시된 이전 릴리즈 이후의 커밋을 Features·Fixes 등으로 분류한다. 이 경로를 사용하려면
저장소에서 immutable releases를 활성화해야 한다.
게시 단계가 실패하면 같은 실행의 실패한 job을 재실행한다. 준비한 artifact를 그대로
사용하므로 새 버전이나 태그 교체 없이 Releaseway의 게시 재시도를 사용할 수 있다.
artifact는 30일간 보관한다. GitHub Release 게시 이후 Homebrew와 installer 배포는
독립적으로 실행한다. 게시 액션의 `diagnostics-path`는 가능한 경우 `release-diagnostics`
artifact로 남기며 진단 파일이 없다고 원격 변경이 없었다고 판단하지 않는다.
installer job은 `.github/workflows/pages.yml`에 게시된 태그를
전달하고, 해당 workflow가 릴리스의
`install.sh`와 해당 태그에 존재하는 `install.ps1`을 각각
`jinyongp.dev/devtools/install.sh`, `jinyongp.dev/devtools/install.ps1`에 배포한다.
각 파일은 선택한 태그의 소스와 정확히 대조하며 과거 Unix-only 태그에는 Windows 파일을 요구하지 않는다.
Releaseway `distribute` step은 해당 태그의 commit SHA와 GitHub asset digest·크기를 먼저
검증하고 `installer-distribution` artifact에 보고서를 남긴다.
Pages source는 저장소
설정에서 GitHub Actions로 한 번 활성화해야 하며, workflow를 수동 실행하면 최신 안정
릴리스를 바로 게시할 수 있다.

태그 생성과 게시 없이 모든 필수 검증·패키징을 실행하려면 다음 명령을 사용한다.
후보 branch는 --ref에 해당 branch를 지정하고 같은 후보의 전체 SHA를 commit에 전달한다.
Homebrew·Pages도 게시하지 않는다. 이 경우 `release-assets` artifact에서 준비된 배포물을 확인할 수 있다.
`VERSION`은 확인한 릴리스 번호(접두사 `v` 제외)로 바꾼다.

```sh
gh workflow run release.yml --ref main -f version=VERSION -f commit="$(git rev-parse HEAD)" -f publish=false
```

로컬에서 스킬 배포물만 확인하려면
`devtools run release:skill VERSION`을 실행한다.
[설치된 release 검증](../verify/README.md)을 참고한다.

### 로컬 배포물 만들기

일반 사용자는 [설치 안내](install.md)를 따른다. 로컬 패키징에는 개발 환경의 Go와 Python 3가 필요하다.
아래 `VERSION`은 게시할 버전 번호로 바꾸며 태그의 `v`는 제외한다.

```sh
devtools run release:cli VERSION
TARGET_OS=linux TARGET_ARCH=amd64 devtools run release:cli VERSION
devtools run release:skill VERSION
```

CLI 기본 대상은 빌드 환경의 OS/CPU다. `TARGET_OS`와 `TARGET_ARCH`로 다른 대상을 선택한다.
`OUTPUT_DIR`로 기본 `dist/` 출력 위치를, `COMMIT`으로 빌드의 커밋 식별자를 지정할 수 있다.
CLI archive에는 실행 파일 하나가 들어가며 `.sha256` 파일을 함께 만든다.
Skill archive에는 `devtools/SKILL.md`가 들어가고 별도 checksum을 만든다.
Skill 패키징 자체에는 `uvx`·`skills-ref`가 필요 없지만 게시 전 `check:all`은 공식 Skill 검증을 실행한다.

### Homebrew 배포

Pull Request와 관련 파일의 main push에서는 `.github/workflows/homebrew.yml`이 고정 SHA의
`releaseway/homebrew-actions` 검사 워크플로를 호출해
`.github/homebrew/formula.yml`을 렌더링하고 Formula 계약을 검증한다.

안정 릴리스 게시가 성공하면 같은 Release 실행의 Homebrew job이 배포된
`releaseway/homebrew-actions` publish 워크플로에 태그 커밋, 버전, 목적지 tap과
배포 키를 전달한다. 워크플로는 태그 커밋의 소스로 Formula를 생성하고 native Homebrew
검증을 통과한 뒤 `jinyongp/homebrew-tap`의 `Formula/devtools.rb`를 직접 갱신한다.
[Formula spec](../.github/homebrew/formula.yml)의 `platforms`는 macOS와 Linux의 ARM64·x64 네 대상을 명시한다.
게시 전 각 대상의 native runner에서 audit·설치·Formula test를 모두 통과해야 한다.
일반 PR의 `check.yml`은 spec 렌더링·audit을 검사하며 이 native 설치 검증을 대신하지 않는다.
게시 워크플로는 source·spec commit과 Formula digest를 각 native 검증 기록에 연결하고,
원본 검증 기록을 포함한 집계 artifact를 30일간 보관한다.
사전 릴리스는 GitHub Releases에만 게시하고 Homebrew는 건너뛴다.

게시 없이 현재 커밋의 native 설치를 확인하려면
`.github/workflows/homebrew-native.yml`을 수동 실행한다. spec의 네 플랫폼에서
동일한 Formula artifact의 digest와 실제 OS·아키텍처를 확인하고 audit·소스 설치·test를
수행한다. 성공한 플랫폼의 검증 기록은 30일간 보관한다.

배포에는 devtools 저장소의 `HOMEBREW_TAP_DEPLOY_KEY` Actions secret을 사용한다.
Homebrew 단계가 실패해도 이미 게시된 GitHub Release는 유지되며 해당 job을 다시 실행할 수 있다.
단, 검증된 Formula SHA-256과 게시 후보가 달라 중단됐다면 push job만 재시도하지 않는다.
현재 tap을 기준으로 Formula 생성·native 검증·게시를 모두 다시 실행해야 한다.
Git이 같은 Formula의 변경을 충돌 없이 합쳤더라도 검증하지 않은 바이트는 게시하지 않는다.
Dependabot은 GitHub Actions 의존성을 매주 그룹으로 갱신하므로 Releaseway full-SHA pin도
일반 Actions 업데이트와 함께 검토한다. Formula 빌드는 `updateManager=homebrew`를 기록해
업데이트를 Homebrew로 안내한다. 빌드 도구는 `go.mod`에 선언한 정확한 Go 버전을 사용하며,
Homebrew의 Go 패치 버전이 다르면 Go toolchain 다운로드 기능으로 필요한 버전을 준비한다.

## 문서 작성과 유지

`docs/`에는 사용법, API 계약, 현재 구조와 운영·복구 절차를 둔다.
기능을 바꾸면 해당 문서를 현재 동작으로 수정한다. 구현 전 제안이나 완료된 작업 목록을
현행 사용법과 섞지 않는다. 문서의 진입점은 [문서 목록](README.md)이다.
공개할 설계 제안은 미구현 상태와 적용 범위를 명시하고 문서 목록의 별도 항목으로 구분한다.

예제와 fixture는 독립적인 가상 프로젝트로 구성한다. 비공개 저장소의 이름, 도메인,
코드 경로, 실행 인수와 설정 내용을 사용자 자료에서 그대로 가져오지 않는다.
이름만 치환한 복사본 대신 설명하려는 제품 동작에 필요한 최소 합성 예제를 작성한다.
프로젝트 이름은 `sample`, 경로는 `/path/to/project` 또는 `C:\project`, 예시 호스트는
`example.test`, `example.invalid` 또는 `localhost` 계열을 사용한다.
제품의 공식 배포 주소와 필수 provider 도메인은 실제 공개 주소를 유지한다.

작업 진행, 리뷰 판정, 일회성 검증 결과와 원시 로그는 task/workstream, 커밋 설명,
Actions run·artifact에서 관리한다. 로컬 시험 결과는 임시 경로에 두며 `docs/`에 추가하지 않는다.
검증 문서는 실행 방법과 판정 기준을 설명하고, 특정 실행의 통과 이력을 누적하지 않는다.
