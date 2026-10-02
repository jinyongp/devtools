# devtools

[English](README.md) | [한국어](README.ko.md)

devtools는 프로젝트의 개발환경과 작업을 한곳에서 관리하는 CLI입니다.
환경변수와 secret을 저장해 명령 실행에 사용하고, 개발 서버의 실행·포트·로컬 주소를
관리할 수 있습니다. 작업과 진행 기록을 남겨 다른 세션이나 에이전트에서 이어갈 수도 있습니다.
macOS, Linux, WSL에서 사용할 수 있습니다.

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

프로젝트 디렉터리로 이동한 뒤 아래 예시를 실행해 보세요. `myapp`은 프로젝트를
구분하는 profile 이름입니다. 사용할 프로젝트 이름으로 바꾸면 됩니다.

```sh
devtools init --profile myapp
devtools var set LOG_LEVEL --value info
devtools env create local
devtools var set LOG_LEVEL --env local --value debug
devtools run --env local -- sh -c 'echo "$LOG_LEVEL"'
```

이 예시는 공통 `LOG_LEVEL`을 `info`로 등록하고, `local` 환경에서는 `debug`로
덮어씁니다. 마지막 명령이 `debug`를 출력하면 환경변수 주입이 동작한 것입니다.
실제 프로젝트에서는 `sh -c ...` 자리에 `pnpm dev`, `go run .` 등 기존 실행 명령을 넣으세요.

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

devtools 옵션은 명령 이름 앞에 두세요. 현재 안정 버전인 v0.22.3에서는
실행 프로그램의 인자를 `--` 뒤에 붙입니다.

```sh
devtools run dev:docs -- --port 3000
devtools run --env local dev:docs -- --port 3000
```

등록한 명령의 도움말은 `devtools run dev:docs -- --help`로,
devtools 자체의 도움말은 `devtools run --help`로 확인하세요.

아직 배포하지 않은 `main` 버전에서는 구분자를 생략할 수 있습니다.

```sh
devtools run --env local dev:docs --port 3000
```

이 버전에서는 이름 뒤의 인자가 `--env`, `--help`까지 모두 등록한 명령에 전달됩니다.
버전별 사용법과 두 형식은 [명령 실행 가이드](docs/cli-contract.md#이름-명령에-추가-인자-전달)를 참고하세요.

## 사용 가이드

| 하고 싶은 일 | 안내 |
| --- | --- |
| 변수·secret 등록, 기존 .env 가져오기, 명령 실행 | [값과 실행](docs/cli-contract.md) |
| 실행에 필요한 도구·설정 확인, 설치 상태 점검 | [환경 진단](docs/doctor.md) |
| 포트 충돌 관리, 다른 프로젝트 URL 연결 | [포트](docs/ports.md) |
| worktree별 `.localhost` 주소로 개발 서버 연결 | [로컬 reverse proxy](docs/proxy.md) |
| 서버 실행·상태·로그·재시작 관리 | [프로세스](docs/processes.md) · [준비 확인](docs/process-readiness.md) |
| 계획 수립, 작업 분담, 세션 인계 | [task와 workstream](docs/tasks.md) |
| 브라우저에서 환경변수와 작업 관리 | `devtools dashboard`를 실행한 뒤 출력된 접속 링크 열기 |
| 프로젝트 설정 비교, 다른 컴퓨터로 이동 | [Profile 관리](docs/profiles.md) |
| 암호화 백업·복구, 오래된 데이터 정리 | [백업](docs/backup.md) · [정리](docs/cleanup.md) |

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
