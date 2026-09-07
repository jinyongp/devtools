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
자동완성 후보는 CLI 명령 정의에서 생성됩니다. Tab 처리에는 저장소 조회나
devtools 프로세스 실행이 필요하지 않습니다.

profile·env 이름, task ID, 변수·secret 값은 직접 입력합니다. 값을 받는 옵션 다음에는
값을 입력할 자리를 유지하고, `--`부터는 실행할 자식 명령의 인수로 취급합니다.
