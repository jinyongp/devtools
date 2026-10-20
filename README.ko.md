# devtools

[English](README.md) | [한국어](README.ko.md)

devtools는 프로젝트의 개발환경과 작업을 한곳에서 관리하는 CLI입니다.
환경변수와 secret을 저장해 명령 실행에 사용하고, 개발 서버의 실행·포트·로컬 주소를
관리할 수 있습니다. 작업과 진행 기록을 남겨 다른 세션이나 에이전트에서 이어갈 수도 있습니다.

## 설치

아래 명령은 최신 안정 버전을 설치합니다. 설치 스크립트를 사용하면 Go를 따로 설치할 필요가 없습니다.

### Windows (PowerShell)

```powershell
irm https://jinyongp.dev/devtools/install.ps1 | iex
```

설치 후 새 터미널에서 `devtools version`을 실행하세요. 설치기가 PC에 맞는 x64·ARM64를
선택하고 사용자 PATH에 등록합니다. 관리자 권한은 필요하지 않습니다.
이미 설치되어 있어도 같은 설치 명령을 다시 실행하면 업데이트됩니다.
Windows 11 25H2·26H2를 지원합니다. 자세한 조건은 [Windows 안내](docs/windows.md)를 참고하세요.

### macOS / Linux / WSL

Homebrew를 사용한다면:

```sh
brew install jinyongp/tap/devtools
```

Homebrew는 `devtools`를 가리키는 `dvt` 심볼릭 링크와 bash·zsh·fish 자동완성 파일도
함께 설치합니다. 셸 설정은 [자동완성 가이드](docs/completion.md)를 참고하세요.

또는 curl로 설치하세요.

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
```

curl로 설치했다면 새 터미널에서도 사용할 수 있도록 `export` 줄을 셸 설정 파일
(예: `~/.zshrc`)에 추가하세요. WSL 안에서도 이 Linux용 설치 방법을 사용합니다.

짧은 명령 `dvt`도 제공합니다. 다른 프로그램이 이미 사용하는 이름이면 해당 파일을 보존합니다.
설치 위치 변경은 [설치와 업데이트](docs/install.md), 셸 설정은 [자동완성](docs/completion.md)을 참고하세요.
Windows와 WSL의 데이터·서버·CA는 별개입니다. 함께 사용할 때는 [WSL 안내](docs/wsl.md)를 확인하세요.

## 시작하기

처음 사용할 프로젝트 디렉터리에서 실행하세요. `myapp`은 값을 저장할 profile 이름입니다.
같은 이름의 profile이 있다면 기존 값을 공유하므로 새 예제에는 사용하지 않은 이름을 고르세요.
아래 첫 예제는 macOS·Linux·WSL과 Windows에서 같습니다.

```sh
devtools init --profile myapp
devtools var set LOG_LEVEL --value info
devtools env create local
devtools var set LOG_LEVEL --env local --value debug
devtools var get LOG_LEVEL --env local
```

마지막 JSON 응답의 `data.value`가 `debug`이면 `local` 환경의 덮어쓰기를 확인한 것입니다.
자식 명령에 이 값을 전달하려면 사용하는 셸에 맞게 실행하세요.

macOS·Linux·WSL의 POSIX 셸:

```sh
devtools run --env local -- sh -c 'echo "$LOG_LEVEL"'
```

Windows의 PowerShell 또는 cmd:

```powershell
devtools run --env local -- cmd.exe /d /c set LOG_LEVEL
```

각각 `debug`, `LOG_LEVEL=debug`가 출력됩니다. 실제 프로젝트에서는 `--` 뒤에
`pnpm dev`, `go run .` 등 설치된 프로젝트 도구의 명령을 넣으세요. devtools 명령은
JSON을 반환하고, 자식 프로그램의 출력은 JSON으로 감싸지 않습니다.
[출력 보호](docs/output-protection.md)를 지원하는 빌드에서는 non-TTY stdout·stderr의
전달된 secret을 기본 마스킹합니다. TTY 출력에는 설정한 정책을 적용합니다.

생성된 `devtools.toml`은 Git에 커밋해 두세요. 같은 profile을 사용하는 프로젝트와
worktree는 저장한 값을 공유합니다. 현재 소스는 secret을 로컬 암호화하고
OS 보호 또는 암호 기반 키 세션을 재사용합니다.
공개 변수는 조회할 수 있습니다. `vault` 명령을 제공하는 빌드에서는 처음 한 번
`devtools vault init`으로 준비합니다. [설정과 적용 빌드](docs/secret-protection.md)를 확인하세요.
secret 등록과 프로젝트 명령 설정은
[값과 실행 가이드](docs/cli-contract.md)를 참고하세요.

명령 사용법이 필요하면 `devtools --help` 또는 `devtools task --help`처럼
기능별 도움말을 확인하세요. 등록한 프로젝트 명령은 `devtools command list`로 볼 수 있습니다.

### 등록한 명령 실행하기

자주 쓰는 명령은 `devtools.toml`에 등록하세요. 프로젝트에 `docs:dev` 스크립트가
있다면 다음처럼 이름을 붙일 수 있습니다.

```toml
[commands."dev:docs"]
exec = ["pnpm", "run", "docs:dev"]
```

devtools 옵션은 명령 이름 앞에 두세요. devtools v0.23.0 이상에서는
실행 프로그램의 인자 앞에 `--`를 붙이거나 생략할 수 있습니다.

```sh
devtools run dev:docs --port 3000
devtools run dev:docs -- --port 3000
devtools run --env local dev:docs --port 3000
```

등록한 명령의 도움말은 `devtools run dev:docs --help`로,
devtools 자체의 도움말은 `devtools run --help`로 확인하세요.

이름 뒤의 인자는 `--env`, `--help`까지 모두 등록한 명령에 전달됩니다.
v0.22.3 등 이전 버전에서는 구분자 `--`를 붙이세요.
버전별 사용법과 두 형식은 [명령 실행 가이드](docs/cli-contract.md#이름-명령에-추가-인자-전달)를 참고하세요.

## 사용 가이드

[전체 문서](docs/README.md)에서 사용자 가이드, API 계약, 개발 안내와 과거 설계 기록을 찾을 수 있습니다.

- 값과 설정: [변수·secret·명령 실행](docs/cli-contract.md), [로컬 secret 보호](docs/secret-protection.md),
  [출력 보호](docs/output-protection.md), [개발환경 진단](docs/doctor.md).
- 서버와 주소: [포트](docs/ports.md), [서버 실행·종료](docs/processes.md),
  [준비 확인](docs/process-readiness.md), [로컬 도메인·HTTPS·공유](docs/local-network.md).
- 작업 관리: [task·workstream·dashboard](docs/tasks.md) — `devtools dashboard`가 반환한
  `data.item.url`로 브라우저에 접속합니다. 로그인 링크는 인증정보이므로 공유하지 마세요.
- 데이터 관리: [Profile 조회·이동](docs/profiles.md), [암호화 백업·복구](docs/backup.md),
  [저장소 정리](docs/cleanup.md).
- 셸과 자동화: [자동완성](docs/completion.md), [CLI 출력 계약](docs/cli-output.md).

## 에이전트와 사용하기

사용 중인 코딩 에이전트에 devtools Skill을 설치하면 프로젝트 준비, 서버 관리,
작업 기록에 필요한 사용 지침을 제공할 수 있습니다.

```sh
npx skills add jinyongp/devtools
```

설치 도구가 에이전트를 감지하고 설치 위치를 안내합니다. 모든 프로젝트에서 사용하려면
`--global`을 추가하세요. 자세한 설정과 업데이트는 [Agent Skill 설치](docs/agent-skill.md)를 참고하세요.

## 업데이트

Homebrew로 설치했다면 Homebrew로 업데이트하세요.

```sh
brew upgrade jinyongp/tap/devtools
```

설치 스크립트로 설치했다면 `devtools update`로 최신 안정 버전으로 갱신합니다.

```sh
devtools update
```

기존 profile 데이터와 프로젝트 설정을 유지하며 실행 파일을 교체합니다.
완료 후 `devtools version`으로 새 버전을 확인하세요.
vault 지원 빌드에서 기존 secret을 사용하려면 [설정과 저장 전환](docs/secret-protection.md)을
따르세요. 실행 파일 업데이트만으로 이전 자료가 암호화되지는 않습니다.

에이전트와 사용한다면 [Skill도 함께 업데이트](docs/agent-skill.md#업데이트)하세요.
CLI와 Skill은 각각 사용하는 설치 도구로 갱신합니다.

MIT 라이선스로 제공됩니다. 자세한 내용은 [LICENSE](LICENSE)를 참고하세요.
프로젝트 개발과 기여는 [개발 안내](docs/development.md)를 참고하세요.
