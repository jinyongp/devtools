# devtools 개발

저장소에서 devtools 자체를 수정하는 개발자를 위한 안내다.
사용자 설치와 실행은 [README](../README.md)를 참고한다.


Requires Go 1.27.x, Node.js 24.x, Python 3, Git, curl, OpenSSL, tar, just, and uv with `uvx`.

```sh
just build
just check
just verify proxies
./bin/devtools version
./bin/devtools schema
./bin/devtools project inspect
```

`just check` validates the Agent Skill and its release archive with
`skills-ref==0.1.1`, runs the Dashboard JavaScript regression tests with Node's
built-in test runner, checks `scripts/install.sh` syntax, checks formatting, runs
`go vet`, and runs Go tests with the race detector. `just check-dashboard` runs only
the Dashboard JavaScript tests.
`just check-skill` runs only the official format validator, while
`just check-skill-release` also tests archive structure, checksums, source identity,
and invalid fixtures. The repository also exact-pins `skills` 1.6.0; after `pnpm install`,
`pnpm test:skill-discovery` verifies local repository discovery and
`pnpm test:skill-discovery:public` verifies the public `jinyongp/devtools` source.
Tag-triggered release validation runs on macOS and Ubuntu 24.04 with
Go 1.27.1. The workflow disables setup-uv caching because uv is used only to run
the Agent Skills validator and this repository has no Python dependency manifest.
WSL uses the Linux build; testing in an actual WSL environment is a separate check.
`just verify proxies`는 배포 아카이브를 설치한 격리 환경에서 HTTP route,
WebSocket, 동적 port 변경, daemon 재시작과 listener reservation을 검증한다.

Go 단위 테스트는 OS가 관리하는 비어 있는 port 번호를 미리 골라 닫은 뒤 재사용하지 않는다.
port 가용성이나 proxy listener가 테스트의 직접 대상이 아니면 주입 가능한 probe/listener를
사용해 상태 전이와 저장소 동작을 결정적으로 검증한다. 실제 TCP bind, occupied/free 판정,
IPv4/IPv6 listener, 설치된 바이너리의 proxy 동작은 `just verify ports proxies`에서 검증한다.
이 경계를 유지해 호스트의 포트 사용 상태나 병렬 프로세스 때문에 단위 테스트가 흔들리지 않게 한다.


## Dashboard 화면 개발

저장소에서 다음 명령을 실행하고 출력된 링크를 연다.

```sh
just dashboard
```

개발 전용 실행 파일을 빌드한 뒤 서버를 터미널에서 실행한다.
`internal/dashboard/assets`의 HTML·JS·CSS를 수정하고 브라우저를 새로고침하면
수정 내용이 바로 반영된다. 응답은 `Cache-Control: no-store`로 제공한다.
Go 코드를 수정했을 때는 `Ctrl+C`로 종료하고 `just dashboard`를 다시 실행한다.

현재 사용자의 devtools 데이터와 저장소의 profile을 사용한다. 개발 서버는
별도의 임시 registry와 포트를 사용하며 종료할 때 임시 파일을 정리한다.
기존 dashboard와 개발 서버를 동시에 열어둘 수 있다.
격리 데이터로 확인하려면 별도의 HOME을 사용한다. Linux와 WSL에서는
XDG_DATA_HOME·XDG_CONFIG_HOME·XDG_CACHE_HOME도 격리 디렉터리로 지정한다.

일반 `devtools dashboard`는 실행 파일에 내장된 화면을 제공한다.
DOM 없이 실행하는 빠른 JavaScript 회귀 테스트는 `pnpm test:dashboard`로 확인한다.
실제 Chromium smoke는 빌드된 CLI와 격리한 HOME/XDG 경로를 사용한다.

```sh
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
just build
pnpm test:dashboard:browser
```

Smoke는 dashboard 로그인, Values 화면의 변수 수정, URL navigation과 session 유지 상태의 reload,
CLI에서 확인한 최종 값을 한 흐름으로 검증한다. Release의 Linux CI에서는 Chromium의 OS 의존성도
함께 설치하며 이 smoke가 통과한 뒤 설치된 release 시나리오를 실행한다. Playwright는
`@playwright/test` 1.63.0으로 정확히 고정한다.

## 도구 추가

- Add the tool's behavior in its own `internal/` package.
- Register commands in `internal/cli`. Options generate flag parsing, validation,
  help, and schema discovery from the same definition.
- Return data or a `protocol.Error` from handlers. The runner serializes results.
- Resolve project context only for commands that need it. Use `paths` for user
  storage locations. Handlers receive context and injectable input/output.
- Test valid input, failure behavior, and command-specific risks.
- Update the public guide and Agent Skill when the command changes how
  users or agents should prepare, retry, or coordinate work.
- Add an installed-release scenario when packaging, storage compatibility,
  or detached process behavior is part of the feature.


## 릴리스

```sh
pnpm release
pnpm release --publish
```

버전을 추천받고 main과 태그를 함께 올린다. 태그가 Release 워크플로를 시작하면
태그 검증 뒤 macOS와 Linux CI를 병렬로 실행한다. Linux CI는 Agent Skill의 local/public `skills` discovery와 실제 Chromium dashboard smoke를 확인하고,
macOS와 Linux CI는 모두 `just verify`로 설치된 release 시나리오를 통과해야 한다. 두 CI가 성공한 뒤 CLI·Agent Skill을
패키징하고 GitHub Release를 게시한다. 안정 릴리스 게시 후에는 `.github/workflows/pages.yml`이 릴리스의
`install.sh`를 `jinyongp.dev/devtools/install.sh`에 배포한다. Pages source는 저장소
설정에서 GitHub Actions로 한 번 활성화해야 하며, workflow를 수동 실행하면 최신 안정
릴리스를 바로 게시할 수 있다. 로컬에서 스킬 배포물만 확인하려면
`just release-skill VERSION`을 실행한다.
[배포 상세](install.md#github-releases-게시)와
[설치된 release 검증](../verify/README.md)을 참고한다.

### Homebrew 배포

안정 릴리스 게시가 성공하면 같은 Release 실행의 Homebrew job이
`.github/homebrew/formula.yml`을 tap의 고정된 재사용 워크플로에 전달합니다.
공통 워크플로가 고정 SHA의 자동화 코드와 최신 main의 Formula 데이터를
각각 체크아웃합니다. 호출자는 명세와 릴리스 정보, 배포 키를 전달합니다.
Dependabot이 두 Homebrew 워크플로를 전용 그룹으로 묶어 매주 고정 SHA 업데이트를
제안합니다. tap이 제공하는 권한 검사 워크플로는 검증된 Dependabot 커밋이 두 SHA만
같은 최신 main 커밋으로 변경했는지 확인한 뒤 squash auto-merge를 예약합니다.
저장소 설정에서 auto-merge와 squash merge를 허용하고, main 규칙에서 `auto-merge`
검사를 필수 상태 검사로 지정해야 자동 병합이 활성화됩니다.
태그 커밋의 소스로 Formula를 생성하고 audit·소스 설치·테스트를 통과하면
`jinyongp/homebrew-tap`의 `Formula/devtools.rb`를 갱신합니다.
사전 릴리스는 GitHub Releases에 게시하고, Homebrew에는 안정 버전을 제공합니다.

배포에는 devtools 저장소의 `HOMEBREW_TAP_DEPLOY_KEY` Actions secret을 사용합니다.
Homebrew 단계가 실패하면 GitHub 릴리스는 유지되며 해당 실패 job을 재실행할 수 있습니다.
Formula 빌드는 `updateManager=homebrew`를 기록해 업데이트를 Homebrew로 안내합니다.
빌드 도구는 `go.mod`에 선언한 정확한 Go 버전을 사용합니다. Homebrew의 Go 패치
버전이 다르면 Go의 toolchain 다운로드 기능으로 필요한 버전을 준비합니다.
