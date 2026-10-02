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

버전 추천은 HEAD에 포함된 실제 게시된 안정 릴리즈를 기준으로 계산한다.
실패하거나 게시하지 않은 태그는 다음 버전의 기준이 되지 않는다. `--publish`는
깨끗한 main을 올린 뒤 버전과 정확한 커밋 SHA로 Release 워크플로를 요청한다.
로컬에서 릴리즈 태그를 만들지 않는다. Node.js, pnpm과 인증된 GitHub CLI(`gh`)가 필요하다.

워크플로는 대상 커밋이 main에 포함됐는지 확인하고 macOS와 Linux 검사를 병렬로
실행한다. Linux는 Agent Skill의 local/public discovery와 Chromium dashboard smoke를
포함한다. 두 OS 모두 `just verify`를 통과해야 하며, 각 설치 시나리오는 180초를
넘기면 이름을 출력하고 실패한다. 검사가 끝나면 네 플랫폼의 CLI와 Agent Skill을
패키징하고 체크섬·바이너리 버전·커밋·양쪽 OS의 Skill 일치를 확인한다.
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
`install.sh`를 `jinyongp.dev/devtools/install.sh`에 배포한다. Pages source는 저장소
설정에서 GitHub Actions로 한 번 활성화해야 하며, workflow를 수동 실행하면 최신 안정
릴리스를 바로 게시할 수 있다.

태그 생성과 게시 없이 모든 검증·패키징을 실행하려면 다음 명령을 사용한다.
이 경우 `release-assets` artifact에서 준비된 배포물을 확인할 수 있다.

```sh
gh workflow run release.yml --ref main -f version=0.23.0 -f commit="$(git rev-parse HEAD)" -f publish=false
```

로컬에서 스킬 배포물만 확인하려면
`just release-skill VERSION`을 실행한다.
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
