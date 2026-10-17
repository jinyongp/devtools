# devtools 개발

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

저장소에서 devtools 자체를 수정하는 개발자를 위한 안내다. 사용자 설치와 첫 실행은
[README](../README.ko.md), 설치된 배포물의 통합 검증은 [검증 안내](../verify/README.md)를 따른다.

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

로컬 `check:all`은 disposable Actions의 사용자·ACL·OS 경계 검사를 대체하지 않는다.
Release 검증은 macOS, Ubuntu 24.04, Windows Server 2025 x64(테스트 호스트),
Windows 11 ARM64와 Go 1.27.1을 사용한다. 현재 runner와 실행 근거·실제 PC 미검증 한계는
[Windows 안내](windows.md)와 [최종 리뷰](windows-support/implementation/W9.md)를 따른다.
Windows 구현 결정의 배경은 [지원 계획](windows-support-plan.md)과
[사전 조사](windows-support/research.md)에 보존한다. 이 기록이 Windows 출시를 의미하지는 않는다.

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
수정 내용이 바로 반영된다. 응답은 `Cache-Control: no-store`로 제공한다.
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

## 도구 추가

- 기능을 별도 `internal/` package에 두고 `internal/cli`에 명령을 등록한다.
  같은 옵션 정의에서 parsing·검증·help·schema를 생성한다.
- handler는 데이터 또는 `protocol.Error`를 반환하고 runner가 직렬화한다.
- 필요한 명령에서만 project context를 선택한다. 사용자 경로는 `paths`를 사용하고
  handler에는 context와 주입 가능한 입출력을 전달한다.
- 정상 입력·실패 경로·해당 기능의 위험을 검사한다.
- 준비·재시도·작업 조정 방식이 바뀌면 사용자 가이드와 Agent Skill을 함께 갱신한다.
- packaging·저장 호환성·분리된 프로세스 수명이 기능에 포함되면 installed-release 검증을 추가한다.


## 릴리스

```sh
devtools run release:plan
devtools run release:publish
```

버전 추천은 HEAD에 포함된 실제 게시된 안정 릴리즈를 기준으로 계산한다.
실패하거나 게시하지 않은 태그는 다음 버전의 기준이 되지 않는다. `--publish`는
깨끗한 main을 올린 뒤 버전과 정확한 커밋 SHA로 Release 워크플로를 요청한다.
로컬에서 릴리즈 태그를 만들지 않는다. Node.js와 인증된 GitHub CLI(`gh`)가 필요하다.

워크플로는 요청한 전체 commit SHA를 checkout하고 macOS·Linux 검사와 Windows x64·ARM64의
재사용 가능한 full 제품 검사를 필수 dependency로 실행한다. 실제 게시인 `publish=true`는
main ref와 main ancestry를 모두 확인한다. `publish=false`는 후보 branch에서도 실행할 수 있다.
Linux는 Agent Skill local/public discovery와 Chromium dashboard smoke를 포함한다.
macOS와 Linux의 `devtools run test:installed`는 시나리오별 180초 제한을 유지한다.

Windows의 필수 검사는 저장소/복구·argv/shim·프로세스/Job·console·네트워크·설치/업데이트·
PowerShell completion·전체 Go suite와 실제 candidate ZIP의 설치·Chromium dashboard를 포함한다.
x64 race와 ARM64 native contention은 구분하며 browser/Node의 실제 CPU도 따로 기록한다.
Windows의 실행 근거는 Actions이며 실제 PC·WSL2 연계 한계는 [Windows 안내](windows.md)를 따른다.

모든 필수 job이 통과하면 macOS/Linux의 네 archive를 만들고 Windows native job이 실제 검증한
두 ZIP을 checksum 확인 후 그대로 포함한다. 여섯 OS/CPU 배포물, Agent Skill과 checksum,
설치기·버전 파일의 identity를 확인한다. 양쪽 Unix OS의 Skill도 byte 단위로 대조한다.
이 단계까지 실패하면 새 태그와 GitHub Release는 만들어지지 않는다.

게시 job은 준비된 `release-assets` artifact를 내려받아 태그를 생성하고,
고정 SHA의 `releaseway/actions`로 immutable GitHub Release를 게시한다.
같은 버전의 태그가 이미 있으면 동일 커밋인지 확인해 재사용하며, 다른 커밋의 태그는
교체하지 않는다. 릴리즈 노트는 `notes: standard` 기본 템플릿으로
게시된 이전 릴리즈 이후의 커밋을 Features·Fixes 등으로 분류한다. 이 경로를 사용하려면
저장소에서 immutable releases를 활성화해야 한다.
게시 단계가 실패하면 같은 실행의 실패한 job을 재실행한다. 준비한 artifact를 그대로
사용하므로 새 버전이나 태그 교체 없이 Releaseway의 게시 재시도를 사용할 수 있다.
artifact는 30일간 보관한다. GitHub Release 게시 이후 Homebrew와 installer 배포는
독립적으로 실행한다. installer job은 `.github/workflows/pages.yml`에 게시된 태그를
전달하고, 해당 workflow가 릴리스의
`install.sh`와 해당 태그에 존재하는 `install.ps1`을 각각
`jinyongp.dev/devtools/install.sh`, `jinyongp.dev/devtools/install.ps1`에 배포한다.
각 파일은 선택한 태그의 소스와 정확히 대조하며 과거 Unix-only 태그에는 Windows 파일을 요구하지 않는다.
Pages source는 저장소
설정에서 GitHub Actions로 한 번 활성화해야 하며, workflow를 수동 실행하면 최신 안정
릴리스를 바로 게시할 수 있다.

태그 생성과 게시 없이 모든 필수 검증·패키징을 실행하려면 다음 명령을 사용한다.
후보 branch는 --ref에 해당 branch를 지정하고 같은 후보의 전체 SHA를 commit에 전달한다.
Homebrew·Pages도 게시하지 않는다. 이 경우 `release-assets` artifact에서 준비된 배포물을 확인할 수 있다.

```sh
gh workflow run release.yml --ref main -f version=0.23.0 -f commit="$(git rev-parse HEAD)" -f publish=false
```

로컬에서 스킬 배포물만 확인하려면
`devtools run release:skill VERSION`을 실행한다.
[배포 상세](install.md#github-releases-게시)와
[설치된 release 검증](../verify/README.md)을 참고한다.

### Homebrew 배포

Pull Request에서는 `.github/workflows/homebrew.yml`이 고정 SHA의
`releaseway/homebrew-actions` 검사 워크플로를 호출해
`.github/homebrew/formula.yml`을 렌더링하고 Formula 계약을 검증한다.

안정 릴리스 게시가 성공하면 같은 Release 실행의 Homebrew job이 배포된
`releaseway/homebrew-actions` publish 워크플로에 태그 커밋, 버전, 목적지 tap과
배포 키를 전달한다. 워크플로는 태그 커밋의 소스로 Formula를 생성하고 native Homebrew
검증을 통과한 뒤 `jinyongp/homebrew-tap`의 `Formula/devtools.rb`를 직접 갱신한다.
사전 릴리스는 GitHub Releases에만 게시하고 Homebrew는 건너뛴다.

배포에는 devtools 저장소의 `HOMEBREW_TAP_DEPLOY_KEY` Actions secret을 사용한다.
Homebrew 단계가 실패해도 이미 게시된 GitHub Release는 유지되며 해당 job을 다시 실행할 수 있다.
Dependabot은 GitHub Actions 의존성을 매주 그룹으로 갱신하므로 Releaseway full-SHA pin도
일반 Actions 업데이트와 함께 검토한다. Formula 빌드는 `updateManager=homebrew`를 기록해
업데이트를 Homebrew로 안내한다. 빌드 도구는 `go.mod`에 선언한 정확한 Go 버전을 사용하며,
Homebrew의 Go 패치 버전이 다르면 Go toolchain 다운로드 기능으로 필요한 버전을 준비한다.
