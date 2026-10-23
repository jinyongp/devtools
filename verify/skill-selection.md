# Skill 자동 선택 검증

2026-10-23 실행. 결과: 요청 3개 모두 통과. 전체 요청에 대한 선택 보장은 아니다.

## 실행 조건

- Codex CLI `0.163.0`, 기본 사용자 모델 설정. 모델·추론 강도 override 없음.
- 요청마다 별도 임시 프로젝트와 새 `codex exec --ephemeral --json` 실행.
- `skills add <checkout> --skill devtools --agent codex --yes`로 후보를 프로젝트에 설치.
- 이전 전역 devtools 설치본만 `skills.config`에서 비활성화. 다른 사용자 스킬·index 유지.
- 프롬프트에 스킬 이름·경로·선택 지시 없음. 프로젝트 AGENTS.md에도 선택 지시 없음.
- 내부 `workspace-write` sandbox, 승인 요청 금지. 값 저장소는 프로젝트별 isolated root.
- 서버·클라이언트 실행 금지. 로그의 실제 파일 읽기, 설정 변경, CLI 검사와 산출물로 판정.
- 후보 트리 SHA-256: `7e3926c7f831bcbe40e984ba4a86a16eb31ce06325f54c4d1fc64ef76b8b2185`.
  정렬한 상대 파일명 + NUL + 파일 내용을 순서대로 해시했다. 설치된 3개 사본과 원본 바이트 일치 확인.

## 요청과 관측

| 요청 | 초기 조건 | 관측 | 결과 |
| --- | --- | --- | --- |
| 개발 서버가 3000번에 고정돼 있어서 이미 사용 중이면 실행이 안 돼. 빈 포트를 동적으로 고르고 데스크톱 클라이언트 URL도 같은 포트를 쓰도록 고쳐줘. 설정을 검증하되 서버나 클라이언트는 실행하지 마. | strict 포트, 서버 bind 없음, 클라이언트 URL 상수 | 후보 SKILL.md·ports.md 읽음 → strict=false·serve·bind·exec 치환 적용 → config check 통과. 새 스크립트 없음. 미할당 URL은 검증 보류로 보고. | 통과 |
| 검사 명령이 저장된 local 환경의 PUBLIC_API_ORIGIN을 받아 실행되게 설정해줘. 다른 저장값은 이 명령에 넣지 말고, 값을 파일이나 스크립트로 복사하지 마. 설정만 검증하고 실제 검사 명령은 실행하지 마. | inject=false, common/local 공개 값과 제외할 다른 값 존재 | 후보 SKILL.md·values.md·execution.md 읽음 → 단일 키 bundle 생성 → inject/env/bundle 설정 → config check 통과. values inspect로 local 키 1개만 선택됨 확인. | 통과 |
| message.py의 welcome 함수 인사말을 Hello 대신 Welcome으로 바꿔줘. 함수 인자와 반환 문자열의 나머지 형식은 유지해줘. | devtools.toml과 후보 스킬이 있는 일반 소스 작업 | implement만 읽음. 후보 스킬 읽기·devtools 실행 없음. 함수 문자열만 수정, Python 검증 통과. | 통과 |

JSONL의 완료된 명령 이벤트에서 후보 SKILL.md 읽기는 두 양성 사례 모두 0-based event 11이었다.
첫 파일 변경은 포트 사례 event 26, 값 사례 event 30이었다. 음성 사례에는 후보 읽기 이벤트가 없었다.
자기 보고뿐 아니라 실제 읽기가 변경보다 먼저 수행됐는지 확인했다.

## 재현 방법

1. 위 초기 조건으로 독립 프로젝트를 만들고 후보를 로컬 설치한다. 값 사례는 두 공개 값과 local override를 isolated root에 준비한다.
2. `DEVTOOLS_ISOLATED_ROOT`를 해당 프로젝트의 임시 저장소로 지정한다.
3. 기존 전역 사본이 있다면 그 경로만 비활성화한다. 후보를 명시 호출하거나 전체 본문을 프롬프트에 넣지 않는다.
4. 각 요청을 새 실행에 전달하고 JSONL을 보관한다.

```sh
codex --no-daemon -a never exec --ephemeral --json --sandbox workspace-write \
  --skip-git-repo-check --cd FIXTURE \
  -c 'skills.config=[{path="OLD_GLOBAL_SKILL/SKILL.md",enabled=false}]' -
```

- 첫 시도는 외부 sandbox가 Codex 런타임 디렉터리 쓰기를 막아 모델 시작 전에 실패했다. 런타임 초기화 권한 확보 후 같은 조건으로 실행했다.
- 모델 작업 시도는 사례별 1회. 비-Git fixture의 Git 검사 실패는 각 실행에서 미검증으로 보고했다.
- 음성 사례의 `python` 부재는 같은 실행에서 `python3`로 복구했다.
- 설정·선택만 검증했다. 실제 서버 시작/동적 할당·주입·readiness는 별도 격리 smoke 검사로 검증하며 자동 선택 결과와 구분한다.
