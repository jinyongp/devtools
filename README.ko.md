# devtools

[English](README.md) | [한국어](README.ko.md)

devtools는 프로젝트의 개발환경과 작업을 한곳에서 관리하는 CLI입니다.
환경변수와 secret을 저장해 명령 실행에 사용하고, 개발 서버의 실행·포트·로컬 주소를
관리할 수 있습니다. 작업과 진행 기록을 남겨 다른 세션이나 에이전트에서 이어갈 수도 있습니다.

## 지원 환경

- **macOS·Linux·WSL:** 아래 설치 방법을 사용합니다. WSL에는 Linux용 CLI를 설치하세요.
- **Windows 네이티브:** v0.25.0에 x64·ARM64 지원을 제공할 예정입니다.
  v0.25.0 게시 전에는 검증된 후보 자산을 사용하세요. 대상은 Windows 11 25H2·26H2입니다.
  [Windows 안내](docs/windows.md)에서 후보 설치와 Actions 검증 범위·실제 PC 미검증 한계를 확인하세요.

Windows와 WSL의 데이터·서버·CA는 별개입니다. 두 환경을 함께 사용한다면
[WSL 안내](docs/wsl.md)를 먼저 확인하세요.

## 설치

Homebrew를 사용한다면 다음 명령으로 설치하세요. 설치 후 바로 `devtools`로 실행할 수 있습니다.

```sh
brew install jinyongp/tap/devtools
```

Homebrew는 zsh·bash·fish 자동완성 파일도 함께 설치합니다. 셸 자동완성이 활성화되어
있으면 `devtools` 뒤에서 Tab으로 명령과 옵션을 고를 수 있습니다.
[자동완성 설정](docs/completion.md)에서 셸별 설정 방법을 확인하세요.

### 설치 스크립트 사용

아래 명령을 터미널에 붙여 넣으세요. 설치기가 운영체제와 CPU를 확인해 최신 안정
버전을 `~/.local/bin/devtools`에 설치하고 짧은 명령 `dvt`도 제공합니다. curl이 필요합니다.

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh
```

설치한 명령을 어디서든 실행할 수 있도록 설치 경로를 PATH에 추가하세요.

```sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
dvt version
```

새 터미널에서도 사용하려면 위의 `export` 줄을 사용하는 셸의 설정 파일
(예: zsh의 `~/.zshrc`)에 추가하세요.
[설치 안내](docs/install.md)에서 버전 지정과 설치 위치 변경 방법을 확인할 수 있습니다.

## 시작하기

처음 사용할 프로젝트 디렉터리에서 실행하세요. `myapp`은 값을 저장할 profile 이름입니다.
같은 이름의 profile이 있다면 기존 값을 공유하므로 새 예제에는 사용하지 않은 이름을 고르세요.
아래 첫 예제는 macOS·Linux·WSL과 Windows 후보에서 같습니다.

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

Windows 후보의 PowerShell 또는 cmd:

```powershell
devtools run --env local -- cmd.exe /d /c set LOG_LEVEL
```

각각 `debug`, `LOG_LEVEL=debug`가 출력됩니다. 실제 프로젝트에서는 `--` 뒤에
`pnpm dev`, `go run .` 등 설치된 프로젝트 도구의 명령을 넣으세요. devtools 명령은
JSON을 반환하고, 실행한 자식 프로그램의 출력은 그대로 전달합니다.

생성된 `devtools.toml`은 Git에 커밋해 두세요. 같은 profile을 사용하는 프로젝트와
worktree는 저장한 값을 공유합니다. 환경변수와 secret은 사용자 전용 권한의 평문 파일로
저장되며, 백업은 별도로 암호화합니다. secret 등록과 프로젝트 명령 설정은
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

- 값과 설정: [변수·secret·명령 실행](docs/cli-contract.md), [개발환경 진단](docs/doctor.md).
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

에이전트와 사용한다면 [Skill도 함께 업데이트](docs/agent-skill.md#업데이트)하세요.
CLI와 Skill은 각각 사용하는 설치 도구로 갱신합니다.

MIT 라이선스로 제공됩니다. 자세한 내용은 [LICENSE](LICENSE)를 참고하세요.
프로젝트 개발과 기여는 [개발 안내](docs/development.md)를 참고하세요.
