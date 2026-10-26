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

PowerShell 자동완성은 Windows에서도 사용할 수 있습니다.
지원 범위는 [Windows 안내](windows.md)를 확인하세요. 설치본이 이 셸을 지원하는지는
`devtools completion --help`로 확인할 수 있습니다.

설치기와 `devtools update`는 PowerShell 프로필이나 자동완성 파일을 만들거나 수정하지 않습니다.
사용하는 PowerShell에서 `$PROFILE`을 열고 다음 줄을 직접 추가하세요.
PowerShell 5.1과 7은 서로 다른 프로필을 사용하므로 각 셸에서 설정하세요.

```powershell
devtools completion powershell | Out-String | Invoke-Expression
```

현재 창에서도 같은 줄을 실행하면 즉시 적용됩니다. 새 세션마다 현재 설치된 바이너리에서
스크립트를 생성하므로 업데이트 후 자동완성 파일을 다시 만들 필요가 없습니다.
`-NoProfile`로 시작한 셸은 프로필을 읽지 않습니다. 실행 정책과 Tab 키 설정은 변경하지 않습니다.

v0.35.0의 자동 등록을 사용했다면 프로필의 `# devtools completion: begin`부터
`# devtools completion: end`까지의 블록을 위 한 줄로 교체하세요.
새 설치기와 updater는 기존 블록과 `%LOCALAPPDATA%\Programs\devtools\completion\powershell.ps1`을
자동 삭제하지 않습니다. 더 이상 참조하지 않는 생성 파일은 사용자가 삭제할 수 있습니다.

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
| `command inspect/explain/update/remove/rename`, `command run`, `run`, `process start`, `doctor`, `project up/status/restart/logs/down` | `devtools.toml`에 정의한 명령 이름. rename은 첫 번째 인자만 해당 |
| `config tool update/remove`, `config preset update/remove` | 해당 프로젝트에 선언한 tool·preset 이름 |
| `profile inspect`, `profile diff`의 양쪽 인자 | 저장된 profile과 현재 프로젝트의 profile 이름 |
| `port allocate` | 선택한 실행 위치에 선언한 포트 이름 |
| `port show/check/release` | 선택한 실행 위치에 남아 있는 할당 이름 |
| `instance move/remove`, port·instance의 `--instance` | 선택한 profile의 instance alias와 `id:ID` |
| `process status/stop/restart/logs/check/wait` | supervisor를 호출하지 않고 읽은 저장된 execution ID |
| `bundle set/rename/remove`, `import --bundle` | 선택한 profile의 bundle 이름. rename은 첫 번째 인자만 해당 |
| 실행·진단·값 조회의 `--bundle` | `all`, `none`, 선택한 profile의 bundle 이름 |
| `bundle set --key` | 선택한 profile의 변수·secret 이름. env에만 존재하는 키도 포함하며 값과 내부 ID는 제외 |
| `--output`, `--mode`, task `--state` 등 고정 선택 옵션 | 해당 옵션의 검증 정의에 있는 허용값 |
| task·workstream·validation의 ID 인수 | 해당 종류의 ID |
| `--workstream`, `--task`, `--task-ids`, `--validation-ids`, `--depends-on` | 해당 관계에 맞는 ID |
| `task checkpoint`, `task release`, `task checkpoint list` | 실행 run ID |

커서 앞에 입력한 `--profile`·`--env`를 반영하며, profile을 생략하면 현재 프로젝트를
사용합니다. `command inspect --dir`과 `process start --dir`은 지정한 프로젝트의 명령을 제안합니다.
`task --workstream`으로 범위를 정하면 해당 workstream의 task ID를 제안합니다.

예를 들어 `devtools var get --env local ` 뒤에서 Tab을 누르면 `local`에서 사용할 수 있는
변수 키를 고를 수 있습니다. `devtools task show ` 뒤에서는 task ID를 고릅니다.
`devtools run docs:` 뒤에서는 `docs:dev`처럼 특수문자가 포함된 명령 이름도 고릅니다.
v0.23.0 이상에서는 `run`, `command run`, `cmd run`의 명령 이름 앞에서만
devtools 옵션을 완성합니다. 이름 뒤는 자식 인자 영역이므로 devtools 옵션과 profile·env
후보를 제안하지 않습니다. 이전 v0.22.3에서는 `--` 뒤부터 후보를 생략합니다.
bash 자동완성은 공백과 셸 문법 문자를 escape해 이름을 하나의 인수로 전달합니다.

후보에는 이름과 ID만 포함합니다. 변수·secret의 값, task 본문, 실행 점유 증명은
자동완성으로 노출하지 않습니다. 값을 받는 일반 옵션 다음에는 직접 값을 입력하며,
`--`부터는 실행할 자식 명령의 인수로 취급합니다.

새 이름을 만드는 `command add`, `config tool/preset add`, `env create`, rename의 새 이름과
새 작업의 `--request-id`에는 기존 항목을 제안하지 않습니다. `process restart --env/--bundle`은
저장된 실행의 profile을 상속하므로 현재 디렉터리 기준 후보를 제안하지 않습니다.
값 전달 request/execution/runner와 cleanup plan/archive ID는 후보 공급자에 포함하지 않습니다.
인증이나 복구 범위를 추측하지 않고 필요한 ID는 해당 조회 결과에서 사용하세요.
`--expected-run`도 해당 task의 조회에서 확인한 현재 run ID를 직접 사용합니다.

조회는 저장된 스냅샷을 읽고, 데이터·캐시를 변경하지 않습니다. 입력한 접두사에 맞는
후보를 정렬해 최대 100개 반환합니다. 조회가 200ms를 넘거나 저장소를 읽을 수 없으면
동적 후보를 생략합니다. 수정·삭제한 이름은 다음 조회에 반영됩니다.

## 구현 경계

선택 대상은 명령 등록 시 `Argument.Completion`과 `Option.Completion`에 선언합니다.
검증용 정규식이나 `name`, `id` 같은 인자 이름으로 entity 종류를 추론하지 않습니다.
이 메타데이터는 내부용이며 공개 JSON schema에는 추가되지 않습니다.

공통 `__complete` 엔진이 커서의 인자 위치·옵션·별칭과 선택된 dir/profile/env를 해석하고,
해당 후보 공급자가 선언 또는 수동적인 저장소 snapshot에서 식별자만 조회합니다.
정렬·접두사 필터·중복 제거·100개 제한·200ms 예산은 엔진이 소유합니다.
고정 선택 옵션은 parser와 공유하는 literal alternatives 판독기로 검증 정의를 재사용합니다.
셸 adapter는 같은 프로토콜을 호출하고 quoting과 셸 등록을 담당합니다.
`./bin/devtools`처럼 경로로 입력했다면 PATH의 다른 설치본 대신 입력한 실행 파일에 조회합니다.
실행 handler, readiness/네트워크 probe, vault unlock·세션 갱신·migration은 Tab 조회에서 호출하지 않습니다.
