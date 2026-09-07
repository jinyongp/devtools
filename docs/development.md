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
`.github/homebrew/formula.yml`을 tap의 고정된 생성 action에 전달합니다.
자동화 코드는 고정 SHA의 `tap-tools`, Formula 변경 대상은 최신 main의 `tap`으로
각각 체크아웃합니다. 기존 Formula 위에서 갱신하므로 이후 릴리스도 같은 경로로 배포합니다.
태그 커밋의 소스로 Formula를 생성하고 audit·소스 설치·테스트를 통과하면
`jinyongp/homebrew-tap`의 `Formula/devtools.rb`를 갱신합니다.
사전 릴리스는 GitHub Releases에 게시하고, Homebrew에는 안정 버전을 제공합니다.

배포에는 devtools 저장소의 `HOMEBREW_TAP_DEPLOY_KEY` Actions secret을 사용합니다.
Homebrew 단계가 실패하면 GitHub 릴리스는 유지되며 해당 실패 job을 재실행할 수 있습니다.
Formula 빌드는 `updateManager=homebrew`를 기록해 업데이트를 Homebrew로 안내합니다.
빌드 도구는 `go.mod`에 선언한 정확한 Go 버전을 사용합니다. Homebrew의 Go 패치
버전이 다르면 Go의 toolchain 다운로드 기능으로 필요한 버전을 준비합니다.
