# devtools 개발

저장소에서 devtools 자체를 수정하는 개발자를 위한 안내다.
사용자 설치와 실행은 [README](../README.md)를 참고한다.


Requires Go 1.27.x, Git (for integration tests), and just.

```sh
just build
just check
./bin/devtools version
./bin/devtools schema
./bin/devtools project inspect
```

`just check` checks formatting, runs `go vet`, and runs tests with the race
detector. Tag-triggered release validation runs on macOS and Linux with Go 1.27.1. WSL uses
the Linux build; testing in an actual WSL environment is a separate check.


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
URL 상태 복원 회귀 테스트는 `pnpm test:dashboard`로 실행한다.

## 도구 추가

- Add the tool's behavior in its own `internal/` package.
- Register commands in `internal/cli`. Options generate flag parsing, validation,
  help, and schema discovery from the same definition.
- Return data or a `protocol.Error` from handlers. The runner serializes results.
- Resolve project context only for commands that need it. Use `paths` for user
  storage locations. Handlers receive context and injectable input/output.
- Test valid input, failure behavior, and command-specific risks.


## 릴리스

```sh
pnpm release
pnpm release --publish
```

버전을 추천받고 main과 태그를 함께 올린다. 태그가 단일 Release 워크플로의
검증·패키징·게시를 시작한다. [배포 상세](install.md#github-releases-게시)와
[격리된 설치 검증](../docker/README.md)을 참고한다.

### Homebrew 배포

안정 릴리스 게시가 성공하면 같은 Release 실행의 Homebrew job이
`.github/homebrew/formula.yml`을 tap의 고정된 재사용 워크플로에 전달합니다.
공통 워크플로가 고정 SHA의 자동화 코드와 최신 main의 Formula 데이터를
각각 체크아웃합니다. 호출자는 명세와 릴리스 정보, 배포 키를 전달합니다.
태그 커밋의 소스로 Formula를 생성하고 audit·소스 설치·테스트를 통과하면
`jinyongp/homebrew-tap`의 `Formula/devtools.rb`를 갱신합니다.
사전 릴리스는 GitHub Releases에 게시하고, Homebrew에는 안정 버전을 제공합니다.

배포에는 devtools 저장소의 `HOMEBREW_TAP_DEPLOY_KEY` Actions secret을 사용합니다.
Homebrew 단계가 실패하면 GitHub 릴리스는 유지되며 해당 실패 job을 재실행할 수 있습니다.
Formula 빌드는 `updateManager=homebrew`를 기록해 업데이트를 Homebrew로 안내합니다.
빌드 도구는 `go.mod`에 선언한 정확한 Go 버전을 사용합니다. Homebrew의 Go 패치
버전이 다르면 Go의 toolchain 다운로드 기능으로 필요한 버전을 준비합니다.
