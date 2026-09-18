# Agent Skill 설치

devtools Agent Skill은 [Agent Skills 규약](https://agentskills.io/specification)을 따르는
표준 Skill입니다. 원본은 저장소의 `skills/devtools/SKILL.md`이며,
`skills` CLI가 저장소에서 직접 발견하고 사용하는 agent에 맞는 위치에 설치합니다.

## 설치

프로젝트에서 사용할 때는 한 명령이면 됩니다.

```sh
npx skills add jinyongp/devtools
```

`skills` CLI가 지원하는 agent를 감지하고 설치 대상을 선택하도록 안내합니다.
직접 `.agents/skills`, `.codex/skills` 같은 경로를 만들거나 파일을 복사할 필요가 없습니다.

모든 프로젝트에서 사용하려면 global scope로 설치합니다.

```sh
npx skills add jinyongp/devtools --global
```

자동화처럼 입력 없이 설치하거나 저장소에 Skill이 여러 개 생긴 뒤에도 이름을 고정해야 할 때만 Skill 이름과 agent·확인 옵션을 명시합니다.

```sh
npx skills add jinyongp/devtools --skill devtools --agent <agent> --yes
```

설치 상태는 `skills` CLI에서 확인합니다.

```sh
npx skills list
npx skills list --global
```

## 업데이트

CLI와 Agent Skill은 각각 사용하는 설치 도구로 업데이트합니다.

Homebrew로 devtools를 설치했다면:

```sh
brew upgrade jinyongp/tap/devtools
npx skills update devtools
```

설치 스크립트를 사용했다면:

```sh
devtools update
npx skills update devtools
```

global Skill은 다음과 같이 업데이트합니다.

```sh
npx skills update devtools --global
```

Skill은 현재 devtools 명령을 `--help`와 `schema`로 발견하도록 작성되어 있습니다.
CLI를 오래된 버전으로 고정해서 사용하는 환경에서는 Skill도 그 환경에서 실제 제공하는
명령 계약을 기준으로 동작해야 합니다.

## 제거

프로젝트 설치:

```sh
npx skills remove devtools
```

global 설치:

```sh
npx skills remove devtools --global
```

## 형식과 검증

Skill 디렉터리는 Agent Skills 규약의 기본 구조를 그대로 사용합니다.

```text
skills/
└── devtools/
    └── SKILL.md
```

`SKILL.md`에는 필수 `name`·`description` frontmatter와 실행 지침이 들어 있습니다.
공식 reference validator로 원본을 확인할 수 있습니다.

```sh
uvx --from skills-ref==0.1.1 agentskills validate skills/devtools
```

Release CI는 reference validator와 함께 실제 `skills` CLI가 저장소에서
`devtools` Skill을 발견하는지도 검증합니다.

## 오프라인 또는 수동 설치

GitHub 저장소를 `skills` CLI에서 직접 가져올 수 없는 환경을 위해 release에는
Skill archive와 SHA-256 checksum도 계속 제공합니다. 일반 설치에는 필요하지 않습니다.

```sh
version=$(curl -fsSL https://github.com/jinyongp/devtools/releases/latest/download/version.txt)
base="https://github.com/jinyongp/devtools/releases/download/v${version}"
curl -fLO "${base}/devtools-skill_${version}.tar.gz"
curl -fLO "${base}/devtools-skill_${version}.tar.gz.sha256"
```

checksum을 확인한 뒤 사용하는 agent가 읽는 Skill 디렉터리에 archive의 `devtools/`
디렉터리를 배치합니다. 이 방식에서는 설치 위치와 업데이트를 사용자가 직접 관리합니다.

Agent Skill은 지침 파일을 설치하는 기능입니다. profile 등록, secret 저장, task 생성 같은
devtools 데이터 변경은 agent가 실제 사용자 요청을 수행할 때 CLI를 통해 이루어집니다.
