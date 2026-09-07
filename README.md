# devtools

개발환경과 작업을 관리하는 agent-first CLI. 프로젝트별 변수·secret, 로컬 포트,
백그라운드 프로세스와 작업 기록을 하나의 도구로 관리한다.
macOS, Linux, WSL을 지원한다.

## 설치

터미널에서 실행한다. 운영체제에 맞는 최신 버전을 `~/.local/bin`에 설치한다.

```sh
devtools_installer=$(curl -fsSL https://github.com/jinyongp/devtools/releases/latest/download/install.sh) &&
  sh -c "$devtools_installer" -- install
```

설치 경로를 PATH에 추가하고 확인한다.

```sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
```

계속 사용하려면 같은 PATH 설정을 셸 설정 파일과 에이전트 실행 환경에 추가한다.
설치에는 curl이 필요하다. [버전 지정·설치 위치 변경](docs/install.md)도 지원한다.

## 시작하기

프로젝트 디렉터리에서 profile을 연결하고 환경변수를 등록한다.

```sh
devtools init --profile myapp
devtools var set LOG_LEVEL --value info
devtools env create local
devtools var set LOG_LEVEL --env local --value debug
devtools run --env local -- sh -c 'echo "$LOG_LEVEL"'
```

마지막 명령은 `debug`를 출력한다. 실제 프로젝트에서는 `sh -c ...` 자리에
`pnpm dev`, `go run .` 등 기존 실행 명령을 넣으면 된다.

생성된 `devtools.toml`은 Git으로 추적한다. 같은 profile을 사용하는 worktree는
전역 저장소의 값을 공유하고, `local`·`staging` 같은 env별로 값을 덮어쓸 수 있다.
secret은 `devtools sec`로 등록하며 조회에는 메타데이터만 제공한다.
활성 값 저장소는 사용자 전용 권한의 평문 파일이고, 백업은 별도로 암호화한다.

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
`devtools schema` 또는 `devtools COMMAND --help`로 명령을 탐색할 수 있다.
조회·변경 결과는 JSON이며, `run`은 자식 프로그램의 출력과 종료 코드를 전달한다.

## 업데이트

```sh
devtools_installer=$(curl -fsSL https://github.com/jinyongp/devtools/releases/latest/download/install.sh) &&
  sh -c "$devtools_installer" -- update
```

기존 profile 데이터와 프로젝트 설정을 유지하며 실행 파일을 교체한다.

## 개발

소스 빌드, 검증과 릴리스 절차는 [개발 안내](docs/development.md)를 참고한다.
