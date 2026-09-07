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
