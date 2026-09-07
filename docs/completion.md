# 셸 자동완성

`devtools` 뒤에서 Tab을 누르면 현재 위치에 맞는 명령·하위 명령·별칭·옵션을
완성합니다. zsh와 fish는 후보 옆에 설명도 표시합니다.

Homebrew 설치는 세 셸의 자동완성 파일을 함께 설치합니다. 사용하는 셸의
자동완성 기능을 활성화해 두세요. [Homebrew 셸 설정 안내](https://docs.brew.sh/Shell-Completion)를
참고할 수 있습니다.

설치 스크립트나 바이너리로 설치했다면 아래에서 사용하는 셸의 설정을 적용하세요.

## zsh

`~/.zshrc`에 다음 내용을 추가하세요. 기존 설정에서 `compinit`을 실행한다면
그 아래에 마지막 줄만 추가하면 됩니다.

```sh
autoload -Uz compinit
compinit
source <(devtools completion zsh)
```

## bash

`~/.bashrc`에 다음 줄을 추가하세요. macOS 로그인 셸에서는 `~/.bash_profile`에서
`~/.bashrc`를 불러오도록 설정하세요.

```sh
source <(devtools completion bash)
```

## fish

다음 명령으로 자동완성 파일을 설치하세요. 업데이트 후 다시 실행하면 새 명령도 반영됩니다.

```fish
mkdir -p ~/.config/fish/completions
devtools completion fish > ~/.config/fish/completions/devtools.fish
```

## 동작 범위

`devtools completion zsh|bash|fish`는 선택한 셸의 스크립트를 표준 출력으로 반환합니다.
명령과 옵션 후보는 CLI 명령 정의에서 생성됩니다. 데이터가 필요한 위치에서는
로컬 devtools가 현재 저장된 이름과 ID를 조회합니다.

| 입력 위치 | 동적 후보 |
| --- | --- |
| `--profile` | 값·task 저장소와 현재 프로젝트의 profile 이름 |
| 값 명령·`run`·`process start`의 `--env`, `env remove` | 선택한 profile의 env 이름 |
| `var get/set/unset`, `sec set/unset` | 선택한 profile·env에서 사용할 수 있는 키 이름 |
| `run`, `process start` | `devtools.toml`에 정의한 명령 이름 |
| task·workstream·validation의 ID 인수 | 해당 종류의 ID |
| `--workstream`, `--task`, `--task-ids`, `--validation-ids`, `--depends-on` | 해당 관계에 맞는 ID |
| `task checkpoint`, `task release`, `task checkpoint list` | 실행 run ID |

커서 앞에 입력한 `--profile`·`--env`를 반영하며, profile을 생략하면 현재 프로젝트를
사용합니다. `process start --dir`은 지정한 프로젝트의 명령을 제안합니다.
`task --workstream`으로 범위를 정하면 해당 workstream의 task ID를 제안합니다.

예를 들어 `devtools var get --env local ` 뒤에서 Tab을 누르면 `local`에서 사용할 수 있는
변수 키를 고를 수 있습니다. `devtools task show ` 뒤에서는 task ID를 고릅니다.

후보에는 이름과 ID만 포함합니다. 변수·secret의 값, task 본문, 실행 점유 증명은
자동완성으로 노출하지 않습니다. 값을 받는 일반 옵션 다음에는 직접 값을 입력하며,
`--`부터는 실행할 자식 명령의 인수로 취급합니다.

조회는 저장된 스냅샷을 읽고, 데이터·캐시를 변경하지 않습니다. 입력한 접두사에 맞는
후보를 정렬해 최대 100개 반환합니다. 조회가 200ms를 넘거나 저장소를 읽을 수 없으면
동적 후보를 생략합니다. 수정·삭제한 이름은 다음 조회에 반영됩니다.
