# 저장소 정리와 보관함

[문서 목록](README.md) · [백업과 복구](backup.md)

`devtools cleanup`은 정리 후보를 미리 보고 선택한 항목을 개인 보관함으로 옮긴다.
보관함은 원본과 같은 전역 데이터 경로의 `archives` 아래에 있다.
공간을 영구 해제하는 purge는 별도 단계다.

## 후보 확인과 적용

```sh
devtools cleanup preview
devtools cleanup preview --profile app
```

전체 미리보기와 profile별 미리보기 중 필요한 범위를 선택한다.
응답의 `data.id`를 `PLAN_ID`로, `data.items`에서 고른 항목의 `id`를 `ITEM_ID`로 사용한다.
미리보기는 10분 동안 유효하다. 적용할 항목과 새 요청 UUID를 지정한다.

```sh
devtools cleanup apply PLAN_ID --item ITEM_ID --item ANOTHER_ID --request-id UUID
```

| 후보 | 기준과 보존 동작 |
| --- | --- |
| 만료된 조회 캐시 | 만료 시각을 지난 스냅샷. task 조회 시에도 30분이 지난 캐시를 자동 정리한다. |
| 종료 로그 | 종료 후 7일이 지난 수집 로그. 프로세스 기록이 보관된 뒤에도 남은 로그의 소유권·시각을 추적한다. |
| 완료 프로세스 기록 | 종료 후 30일 경과. 같은 instance의 실행 중인 프로세스를 보호한다. 기록과 로그는 별도로 보관한다. |
| 완료 작업 기록 | 마지막 action 이후 30일이 지나고 모든 task/workstream이 완료·취소된 profile. 의존 관계와 이력을 함께 보관한다. |
| 오래된 일반 백업 | 설정 디렉터리의 `devtools-UUID.age` 중 30일이 지난 파일. 수정 시각 기준 최신 3개와 복구 직전 safety backup은 유지한다. |
| 사라진 instance | 디렉터리가 사라졌고 관리 프로세스·실행 claim·실제 포트 점유가 없는 연결과 할당. |

profile을 지정하면 그 profile의 작업·프로세스·instance 후보를 계산한다.
공유 조회 캐시는 함께 확인하고 일반 백업 보존 정책은 전체 미리보기에서 계산한다.
개인 값 저장소는 유지한다. 로그 원문은 보관함에서도 개인 파일로 보호한다.

## 충돌과 부분 실패 재시도

최초 변경 전에는 미리보기와 현재 상태를 비교한다. 데이터·작업·실행·포트 점유가
달라졌으면 `revision_conflict`로 새 미리보기를 요구한다.

변경이 시작되면 선택 집합을 해당 요청의 복구 기록에 고정한다.
같은 요청 UUID와 입력으로 재시도하면 미완료 항목만 이어간다.
미리보기 만료나 먼저 성공한 항목의 삭제로 남은 항목이 사라지지 않는다.
남은 원본이 외부에서 바뀌면 계속 `revision_conflict`다.
일부가 이미 보관된 상태에서 나머지 항목이 실패할 수 있으므로 보관함도 확인한다.

## 보관함 조회와 복구

```sh
devtools cleanup archives
devtools cleanup restore ARCHIVE_ID
```

`ARCHIVE_ID`는 archives 응답의 `data.items`에서 선택한다.
restore는 원래 위치에 데이터를 복원한다. 변경된 원본이나 재할당된 포트가 있으면 충돌한다.
작업을 보관한 뒤 새 작업을 만든 profile에는 이전 기록을 덮어쓰지 않는다.
instance 복구는 연결·할당을 복원하며 실제 프로젝트 디렉터리는 별도로 준비한다.

## 영구 삭제

보관한 지 30일이 지난 항목은 명시적으로 purge할 수 있다.

```sh
devtools cleanup purge ARCHIVE_ID
```

purge는 보관한 payload를 삭제하고 처리 메타데이터만 유지한다.
restore와 purge는 같은 archive ID로 반복할 수 있으며 이미 처리한 항목은
`changed: false`다. apply의 동일 요청 재전송은 최초 결과를 유지하고
`replayed: true`를 반환한다. 공통 응답 규칙은 [CLI 출력 계약](cli-output.md)을 따른다.

Dashboard의 **Storage & recovery**에서도 후보 선택, 복구와 영구 삭제를 제공한다.
백업 생성·복구를 사용하려면 [백업 공개키와 디렉터리](backup.md#키와-기본-위치-준비)를 먼저 설정한다.
