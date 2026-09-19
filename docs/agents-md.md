# AGENTS.md 작업 지침

devtools는 프로젝트의 AGENTS.md를 특정 에이전트 형식으로 변환하지 않고
원문 그대로 해석 가능한 source chain으로 제공합니다.

    devtools guidance resolve src/server/handler.go

--dir은 프로젝트 루트 또는 프로젝트 안의 현재 작업 디렉터리를 지정합니다.
상대 TARGET은 --dir 기준으로 해석합니다.

    devtools guidance resolve handler.go --dir src/server

## 적용 범위

resolver는 devtools.toml이 있는 프로젝트 루트를 경계로 사용합니다. 실제 작업
대상의 디렉터리까지 올라가며 AGENTS.md를 다음 순서로 반환합니다.

    <project>/AGENTS.md
    <project>/src/AGENTS.md
    <project>/src/server/AGENTS.md

src/server/handler.go가 대상이면 다른 sibling 디렉터리의 AGENTS.md는 포함하지
않습니다. 기존 디렉터리를 대상으로 할 수도 있고, 아직 생성되지 않은 새 파일 경로를
대상으로 할 수도 있습니다.

응답의 sources는 넓은 범위에서 좁은 범위 순서입니다. 각 항목에는 프로젝트 기준
path, 해당 지침이 적용되는 scope, 원문 content, SHA-256 revision, byte
크기가 포함됩니다. revision은 전체 chain의 순서와 각 source revision을 포함합니다.

## 완전성

AGENTS.md가 없는 것은 정상입니다. 읽어야 할 파일이 있으나 invalid UTF-8,
크기 제한, 읽기 오류 때문에 원문을 제공할 수 없으면 complete=false와
diagnostics를 반환합니다. 호출자는 부분 결과를 전체 지침으로 간주하면 안 됩니다.

resolver는 프로젝트 루트 밖으로 탐색하지 않습니다. 작업 대상이나 AGENTS.md
symlink가 프로젝트 경계를 벗어나면 요청을 거부합니다.

## Agent Skills와의 관계

AGENTS.md는 해당 프로젝트와 디렉터리에서 따라야 할 작업 지침입니다. Agent Skill은
특정 종류의 작업에 재사용하는 절차와 resource입니다. devtools는 둘을 서로 변환하거나
합치지 않습니다.

- devtools guidance resolve TARGET: 실제 작업 대상에 적용되는 프로젝트 지침 원문
- devtools skill list: 사용 가능한 Skill metadata
- devtools skill inspect NAME: 선택한 Skill 원문과 resource inventory

에이전트나 호스트는 작업 Context에서 두 source를 함께 제공할 수 있습니다. 실제 권한,
secret 접근, 명령 실행 허용 여부는 이 문서들의 내용으로 확장되지 않습니다.
