# 셸 자동완성

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

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

## PowerShell 5.1 / 7

PowerShell 자동완성은 현재 소스의 protocol v7 후보에 포함됩니다. Windows 자산의 게시 상태와
지원 범위는 [Windows 안내](windows.md)를 확인하세요. 설치본이 이 셸을 지원하는지는
`devtools completion --help`로 확인할 수 있습니다.

다음 명령으로 생성한 파일을 직접 검토하고 현재 세션에 로드하세요. UTF-8을 명시해
한글을 보존합니다. installer는 이 파일이나 shell profile을 자동으로 수정하지 않습니다.
업데이트 후 다시 생성하면 새 metadata가 반영됩니다.

```powershell
devtools completion powershell | Set-Content -Encoding utf8 .\devtools-completion.ps1
. .\devtools-completion.ps1
```

`devtools`/`dvt`와 `.exe` 이름에 native completer를 등록합니다. AST의 literal token만
읽고 UTF-8 NUL-delimited stdin으로 이름을 조회합니다. `$()`·변수 등 동적 표현식이
있으면 후보 조회를 생략하며 실행하지 않습니다. 공백·한글·single quote가 포함된 후보는
PowerShell single quote 규칙으로 escape합니다. cmd에는 자동완성 engine을 제공하지 않습니다.

## 동작 범위

`devtools completion zsh|bash|fish|powershell`는 선택한 셸의 스크립트를 표준 출력으로 반환합니다.
명령과 옵션 후보는 CLI 명령 정의에서 생성됩니다. 데이터가 필요한 위치에서는
로컬 devtools가 현재 저장된 이름과 ID를 조회합니다.

| 입력 위치 | 동적 후보 |
| --- | --- |
| `--profile` | 값·task 저장소와 현재 프로젝트의 profile 이름 |
| 값 명령·`command run`·`run`·`process start`의 `--env`, `env remove` | 선택한 profile의 env 이름 |
| `var get/set/unset`, `sec set/unset` | 선택한 profile·env에서 사용할 수 있는 키 이름 |
| `command inspect`, `command run`, `run`, `process start`, `doctor`, `project up/status/restart/logs/down` | `devtools.toml`에 정의한 명령 이름 |
| task·workstream·validation의 ID 인수 | 해당 종류의 ID |
| `--workstream`, `--task`, `--task-ids`, `--validation-ids`, `--depends-on` | 해당 관계에 맞는 ID |
| `task checkpoint`, `task release`, `task checkpoint list` | 실행 run ID |

커서 앞에 입력한 `--profile`·`--env`를 반영하며, profile을 생략하면 현재 프로젝트를
사용합니다. `command inspect --dir`과 `process start --dir`은 지정한 프로젝트의 명령을 제안합니다.
`task --workstream`으로 범위를 정하면 해당 workstream의 task ID를 제안합니다.

예를 들어 `devtools var get --env local ` 뒤에서 Tab을 누르면 `local`에서 사용할 수 있는
변수 키를 고를 수 있습니다. `devtools task show ` 뒤에서는 task ID를 고릅니다.
`devtools run docs:` 뒤에서는 `docs:dev`처럼 특수문자가 포함된 명령 이름도 고릅니다.
v0.23.0의 CLI protocol v6에서는 `run`, `command run`, `cmd run`의 명령 이름 앞에서만
devtools 옵션을 완성합니다. 이름 뒤는 자식 인자 영역이므로 devtools 옵션과 profile·env
후보를 제안하지 않습니다. 이전 v0.22.3에서는 `--` 뒤부터 후보를 생략합니다.
Bash 자동완성은 공백과 셸 문법 문자를 escape해 이름을 하나의 인수로 전달합니다.

후보에는 이름과 ID만 포함합니다. 변수·secret의 값, task 본문, 실행 점유 증명은
자동완성으로 노출하지 않습니다. 값을 받는 일반 옵션 다음에는 직접 값을 입력하며,
`--`부터는 실행할 자식 명령의 인수로 취급합니다.

조회는 저장된 스냅샷을 읽고, 데이터·캐시를 변경하지 않습니다. 입력한 접두사에 맞는
후보를 정렬해 최대 100개 반환합니다. 조회가 200ms를 넘거나 저장소를 읽을 수 없으면
동적 후보를 생략합니다. 수정·삭제한 이름은 다음 조회에 반영됩니다.
