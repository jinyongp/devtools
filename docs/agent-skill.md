# 에이전트 스킬 설치

devtools 스킬은 에이전트가 필요한 명령만 조회하고, 작업 점유·인계와 재시도를
올바르게 처리하도록 안내합니다. 명령별 옵션은 CLI에서 필요할 때 조회하므로
스킬에 전체 스키마를 넣지 않습니다.

스킬은 실행 파일에 포함되어 있습니다. 먼저 내용을 확인할 수 있습니다.

```sh
devtools skill
```

## Codex에서 사용하기

개인 스킬 디렉터리에 저장하세요. `CODEX_HOME`을 지정한 환경에서는 그 경로를 사용합니다.

```sh
mkdir -p "${CODEX_HOME:-$HOME/.codex}/skills/devtools"
devtools skill > "${CODEX_HOME:-$HOME/.codex}/skills/devtools/SKILL.md"
```

새 세션을 열고 `$devtools`로 스킬을 지정하거나 devtools를 사용하는 작업을 요청하세요.
에이전트 실행 환경의 PATH에 `devtools`가 있어야 합니다.

다른 에이전트에서는 해당 제품이 지원하는 스킬 디렉터리의 `devtools/SKILL.md`에
같은 내용을 저장하면 됩니다. 스킬을 등록하는 위치와 로드 방법은 제품별로 다릅니다.

## 업데이트

Homebrew 설치는 `brew upgrade jinyongp/tap/devtools`, 스크립트 설치는
`devtools update`로 바이너리를 업데이트한 뒤 위 저장 명령을 다시 실행하세요.
저장 명령은 기존 `SKILL.md`를 교체하므로 직접 수정한 내용이 있으면 먼저 보관해 주세요.
스킬의 버전은 이를 출력한 devtools 바이너리 버전을 따릅니다.

스킬 설치는 안내 파일을 저장하는 동작입니다. profile 등록이나 작업 생성은
에이전트가 실제 요청을 수행할 때 CLI로 진행합니다.
