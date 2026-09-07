# devtools

devtools는 개발환경과 작업을 한곳에서 관리하는 CLI입니다. 프로젝트별 환경변수와
secret을 보관하고, 로컬 포트와 백그라운드 서버를 관리하며, 에이전트가 작업을
나누거나 다른 세션에서 이어갈 수 있도록 진행 기록을 남깁니다.
macOS, Linux, WSL에서 사용할 수 있습니다.

## 설치

아래 명령을 터미널에 붙여 넣으세요. 설치기가 운영체제와 CPU를 확인해 최신 안정
버전을 `~/.local/bin/devtools`에 설치합니다. curl이 필요하며 Go 설치는 필요하지 않습니다.

```sh
curl -fsSL https://github.com/jinyongp/devtools/releases/latest/download/install.sh | sh -s -- install
```

처음 설치했다면 아래 명령으로 설치 경로를 PATH에 추가하세요. PATH는 셸이
실행 파일을 찾는 경로입니다. 설정 후에는 어느 디렉터리에서든 `devtools`로 실행할 수 있습니다.

```sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
```

`devtools version`이 설치된 버전을 반환하면 준비가 끝났습니다. 새 터미널에서도
사용하려면 위의 `export` 줄을 사용하는 셸의 설정 파일(예: zsh의 `~/.zshrc`)에
추가하세요. 에이전트에도 실행 환경의 PATH에 `~/.local/bin`을 포함해 주세요.
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

생성된 `devtools.toml`은 Git에 커밋해 두세요. 같은 profile을 사용하는 worktree는
전역 저장소의 값을 공유하고, `local`·`staging` 같은 env별로 값을 덮어쓸 수 있습니다.
secret은 `devtools sec`로 등록하며 조회에는 메타데이터만 제공합니다.
활성 값 저장소는 사용자 전용 권한의 평문 파일이고, 백업은 별도로 암호화합니다.

## 사용 가이드

| 하고 싶은 일 | 안내 |
| --- | --- |
| 변수·secret 등록, 기존 .env 가져오기, 명령 실행 | [값과 실행](docs/cli-contract.md) |
| 필요한 도구와 설정 확인 | [doctor](docs/doctor.md) |
| 포트 충돌 관리, 다른 프로젝트 URL 연결 | [포트](docs/ports.md) |
| 서버를 백그라운드로 실행하고 준비 완료까지 대기 | [프로세스](docs/processes.md) · [준비 확인](docs/process-readiness.md) |
| 계획 수립, 작업 분담, 세션 인계 | [task와 workstream](docs/tasks.md) |
| 브라우저에서 프로젝트 관리 | [dashboard](docs/management-api-contract.md) |
| 백업·복구, 오래된 데이터 정리 | [백업](docs/backup.md) · [정리](docs/cleanup.md) |

에이전트는 [devtools 스킬](skills/devtools/SKILL.md)을 참고하고,
`devtools schema` 또는 `devtools COMMAND --help`로 명령을 탐색할 수 있습니다.
조회·변경 결과는 JSON이며, `run`은 자식 프로그램의 출력과 종료 코드를 전달합니다.

## 업데이트

이미 설치한 devtools를 최신 안정 버전으로 바꾸려면 아래 명령을 실행하세요.

```sh
devtools update
```

기존 profile 데이터와 프로젝트 설정을 유지하며 실행 파일을 교체합니다.
완료 후 `devtools version`으로 새 버전을 확인하세요.

## 개발

devtools 자체를 수정하거나 배포하려면 [개발 안내](docs/development.md)를 참고하세요.
