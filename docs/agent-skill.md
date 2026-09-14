# Agent Skill 설치

devtools 스킬은 에이전트가 필요한 명령만 조회하고, 작업 점유·인계와 재시도를
올바르게 처리하도록 안내합니다. 명령별 옵션은 CLI에서 필요할 때 조회하므로
스킬에 전체 스키마를 넣지 않습니다.

현재 스킬은 원자적 계획 편집과 미리보기, lifecycle과 current/stale의 구분,
실행 기준 sync, task basis의 실행 컨텍스트 및 workstream basis의 관찰 리비전을
안내합니다. 또한 worktree별 `.localhost` route를 구성할 때 instance alias와 port
할당을 먼저 확인하고, 사용자 전역 proxy daemon 하나를 재시도 안전하게 관리하도록
안내합니다. proxy의 자세한 설정과 진단 방법은 [로컬 reverse proxy](proxy.md)를
참고하세요.

Agent Skill은 CLI와 독립된 배포물입니다. 에이전트 실행 환경의 `PATH`에 `devtools`가
있어야 하며, CLI와 스킬은 같은 릴리스 버전을 사용해야 합니다. 첫 실제 task 변경은
task 저장 형식을 v2로 전환하므로 같은 profile을 쓰는 CLI도 함께 업데이트하세요.
조회와 미리보기는 저장 형식을 전환하지 않습니다.

## 릴리스에서 설치하기

아래 예시는 최신 안정 릴리스의 아카이브와 체크섬을 현재 디렉터리에 받습니다.

```sh
version=$(curl -fsSL https://github.com/jinyongp/devtools/releases/latest/download/version.txt)
base="https://github.com/jinyongp/devtools/releases/download/v${version}"
curl -fLO "${base}/devtools-skill_${version}.tar.gz"
curl -fLO "${base}/devtools-skill_${version}.tar.gz.sha256"
```

Linux에서는 `sha256sum`, macOS에서는 `shasum`으로 전송 무결성을 확인하세요.

```sh
sha256sum -c "devtools-skill_${version}.tar.gz.sha256"
# macOS: shasum -a 256 -c "devtools-skill_${version}.tar.gz.sha256"
```

프로젝트에서 공유하려면 `.agents/skills/`에 압축을 풉니다.

```sh
mkdir -p .agents/skills
tar -xzf "devtools-skill_${version}.tar.gz" -C .agents/skills
```

설치 결과는 다음 구조입니다.

```text
.agents/skills/
└── devtools/
    └── SKILL.md
```

모든 프로젝트에서 사용하는 사용자 스킬로 설치하려면 클라이언트가 검색하는 사용자
스킬 디렉터리를 사용하세요. 교차 클라이언트 관례를 지원한다면 다음과 같이 설치합니다.

```sh
mkdir -p "$HOME/.agents/skills"
tar -xzf "devtools-skill_${version}.tar.gz" -C "$HOME/.agents/skills"
```

`.agents/skills/`는 Agent Skills 파일 형식이 강제하는 경로가 아닙니다. 사용하는
클라이언트가 이 경로를 검색하는지 확인하세요. 새 세션을 열고 devtools를 사용하는
작업을 요청하면 클라이언트가 `name`과 `description`으로 스킬을 발견합니다.

## 저장소에서 설치하기

저장소의 `skills/devtools/`가 스킬의 원본입니다. 체크아웃한 버전을 프로젝트에
설치할 때는 디렉터리 전체를 복사합니다.

```sh
mkdir -p .agents/skills/devtools
cp -R skills/devtools/. .agents/skills/devtools/
```

공식 reference validator로 원본이나 설치본을 확인할 수 있습니다. 이 명령에는 `uvx`가
필요합니다.

```sh
uvx --from skills-ref==0.1.1 agentskills validate .agents/skills/devtools
```

## 업데이트

Homebrew 설치는 `brew upgrade jinyongp/tap/devtools`, 스크립트 설치는
`devtools update`로 CLI를 업데이트합니다. 이어서 새 버전의 Agent Skill 아카이브를
받아 같은 스킬 디렉터리에 다시 압축 해제하세요. 기존 `devtools/` 디렉터리를
교체하므로 직접 수정한 내용이 있으면 먼저 보관해야 합니다.

스킬 설치는 안내 파일을 저장하는 동작입니다. profile 등록이나 작업 생성은
에이전트가 실제 요청을 수행할 때 CLI로 진행합니다.
