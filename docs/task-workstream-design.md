# Task와 workstream 설계

[문서 목록](README.md) · [사용법](tasks.md) · [API 계약](task-api-contract.md) · [계획 편집](task-edit-api.md)

이 문서는 작업 데이터의 공유 범위와 실행·완료 모델을 설명한다.
명령별 입력·출력은 설치한 CLI의 `devtools schema`로 확인한다.

## 데이터 모델과 공유 범위

| 개념 | 역할 |
| --- | --- |
| profile | 같은 사용자 계정에서 프로젝트·worktree·clone이 공유하는 데이터 범위. |
| workstream | 하나의 목표에 대한 요구사항, 완료 조건, 명세·계획, task와 검증을 연결하는 단위. |
| task | 독립적으로 또는 workstream 안에서 점유하고 수행하는 작업. |
| run | task를 수행하는 실행 시도. 인계는 기존 run을 검사하고 새 run을 만든다. |
| checkpoint | 실행 맥락과 다음 행동을 복구하는 기록. |
| validation | 검증 정의와 특정 기준에 대한 결과. |

CLI가 명세·계획의 Markdown 본문, 구조화된 참조, action 이력과 요청 영수증의 원본을
사용자별 데이터 디렉터리에 보관한다. 입력 파일은 등록 시 읽으며 이후 파일 변경을
자동 반영하지 않는다. 현재 컴퓨터의 같은 사용자 계정과 profile이 공유 경계다.
다른 기기에는 [profile 전송](profiles.md)을 사용한다.

| 환경 | 기본 데이터 디렉터리 |
| --- | --- |
| macOS | `~/Library/Application Support/devtools/data` |
| Linux·WSL | `$XDG_DATA_HOME/devtools`, 기본값 `~/.local/share/devtools` |
| Windows | Known Folder LocalAppData 아래 `devtools/data` |

Windows AppContainer는 일반 PowerShell과 다른 Known Folder를 사용할 수 있다.
실제 경로와 저장 제약은 [Windows 안내](windows.md#storage-and-processes)를 따른다.

## 정의, 실행, 완료를 분리하는 이유

task와 workstream의 lifecycle은 action 이력으로 유지한다. 정의 편집은 실행 중이나
완료 후에도 가능하므로 lifecycle만으로 최신 계획의 완료 여부를 판단할 수 없다.
포함·제외 범위, `current/stale` 완료 상태, 현재 점유를 각각 계산한다.

편집은 Markdown 본문과 구조화된 참조를 하나의 요청으로 적용한다. 리비전 비교로
동시 편집을 감지하고 최종 의존 그래프의 순환·범위·소속을 검사한다.
description과 완료 조건처럼 의미를 바꾸는 수정은 관련 task와 검증의 기준을 바꾸며,
title·표시 순서는 의미 기준을 바꾸지 않는다. 완료 이력은 삭제하지 않는다.

검증 결과는 profile revision과 독립적인 `basis_id`에 연결한다. 무관한 checkpoint나
다른 task의 수정이 기존 결과를 무효화하지 않도록 하기 위해서다.
과거 기준의 결과는 이력에 남지만 현재 마감 근거로 자동 승격되지 않는다.
재사용은 현재 기준을 확인하고 명시적으로 accept한다.

## 점유와 인계

claim은 작업 선택과 점유를 하나의 변경으로 처리한다. 여러 세션이 같은 task를
점유할 수 없으며 시간 경과만으로 점유를 해제하지 않는다.
변경 가능한 실행 작업은 발급한 실행 컨텍스트를 요구한다.
인계 후 이전 컨텍스트는 새 실행을 변경할 수 없다.

worktree의 현재 실행 연결은 탐색을 돕는 정보다. 점유와 완료의 원본은 전역 작업
저장소이며 연결 정보만으로 실행 권한을 부여하지 않는다.
에이전트는 재개할 때 현재 계획·점유·검증 기준을 조회하고 필요한 sync나 인계를 수행한다.

## 실행 준비와 마감

workstream의 activate는 명세·계획·task·완료 조건·검증 연결의 구조를 검사한다.
편집 저장은 미완성 계획에도 허용하지만 coverage가 부족한 작업은 실행·마감 단계에서 차단한다.

task done은 유효한 실행 컨텍스트와 현재 기준의 필수 검증을 요구한다.
workstream close는 포함 task의 현재 완료 또는 취소, 모든 현재 점유의 해제,
통합 완료 조건과 필수 검증을 확인한다. 과거에 close했다는 사실만으로 변경된
계획을 다시 완료로 판정하지 않는다. 상세 전이와 재시도 규칙은 [API 계약](task-api-contract.md)을 따른다.

## 저장과 조회

[저장 계층](storage-design.md)이 동시 접근, 원자적 변경, 요청 영수증과 복구를 담당한다.
요청 ID는 같은 입력의 재전송을 식별한다. 응답이 유실돼도 새 ID로 중복 적용하지 않는다.
backup과 profile 전송은 작업 이력을 보존하되 실행 컨텍스트를 복원하지 않는다.

조회 평가는 정렬된 항목, 역방향 의존 관계, 소속과 활성 run의 인덱스를 한 번 만들어
재사용한다. 인덱스는 해당 State에만 속하며 이벤트 적용 시 폐기한다.
편집 미리보기의 clone에는 가변 인덱스를 공유하지 않는다.

그래프 cursor는 생성 시점의 공개 view와 양방향 관계를 보존한다.
후속 페이지는 현재 journal을 다시 읽지 않으므로 동일 탐색의 상태가 중간에 바뀌지 않는다.
지원하지 않는 캐시 형식은 `cursor_invalid`로 새 조회를 요구한다.

## CLI, Dashboard와 에이전트

CLI와 Dashboard는 같은 검증·저장·리비전·요청 ID 규칙을 사용한다.
Dashboard의 인증과 변경 허용 범위는 [관리 API 계약](management-api-contract.md)을 따른다.
에이전트는 계획의 적절성, 코드 상태와 검증 근거의 일치, 완료 판단을 맡는다.
도구는 제공된 기록과 기준의 일관성을 검사하며 외부 작업의 실제 수행을 대신 입증하지 않는다.
