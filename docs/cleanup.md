# 저장소 정리와 보관함

`devtools cleanup`은 정리 후보와 영향을 먼저 계산하고 선택한 항목을 개인 보관함으로 옮긴다. 보관함은 원본 데이터와 같은 전역 데이터 경로의 `archives` 아래에 있으며, 파일은 개인 권한으로 보호한다.

```sh
devtools cleanup preview
devtools cleanup preview --profile app
devtools cleanup apply PLAN_ID --item ITEM_ID --item ANOTHER_ID --request-id UUID
devtools cleanup archives
devtools cleanup restore ARCHIVE_ID
devtools cleanup purge ARCHIVE_ID
```

미리 보기는 10분 동안 유효하다. `items`의 ID로 적용 대상을 고르고, 같은 요청 UUID와 입력으로 재시도한다. 미리 보기 이후 데이터·작업 상태·실행·port 점유가 달라지면 `revision_conflict`로 새 미리 보기를 요청한다. 적용 과정이 중단되면 같은 요청으로 보관된 항목을 확인하고 이어간다. 일부 항목이 이미 보관된 뒤 나머지 항목에서 충돌할 수 있으며, 보관함 조회와 restore로 복구할 수 있다.

| 후보 | 기준과 보존 동작 |
| --- | --- |
| 만료된 조회 캐시 | 명시된 만료 시각을 지난 스냅샷. task 조회 시에도 30분이 지난 캐시를 자동 정리한다. |
| 종료 로그 | 종료 후 7일을 지난 수집 로그. 원문은 보관함에서도 개인 파일로 취급한다. |
| 완료 프로세스 기록 | 종료 후 30일 경과. 실행 중인 같은 instance를 보호한다. |
| 완료 작업 기록 | 마지막 action 이후 30일이 지나고 모든 task/workstream이 완료·취소된 profile. 의존 관계와 이력을 profile 전체로 함께 보관한다. |
| 오래된 일반 백업 | 설정된 디렉터리의 `devtools-UUID.age` 파일 중 30일이 지난 항목. 수정 시각 기준 최신 3개를 유지한다. 복구 직전 safety backup은 별도로 보존한다. |
| 사라진 instance | 디렉터리가 사라졌고 관리 프로세스·실행 claim·실제 port 점유가 없는 연결과 할당. |

profile을 지정하면 해당 profile의 작업·프로세스·instance 후보를 계산한다. 공유 조회 캐시는 함께 확인하며 일반 백업 보존 정책은 전체 미리 보기에서 계산한다. 개인 값 저장소는 유지한다.

restore는 원래 위치에 데이터를 복원한다. 그 위치에 변경된 데이터가 있거나 port가 재할당되어 있으면 충돌을 반환한다. 작업 기록을 보관한 이후 새 작업을 만든 profile에는 기존 기록을 바로 덮어쓰지 않는다. instance 복구는 연결과 할당을 복원하며 실제 프로젝트 디렉터리는 프로젝트의 복구 흐름으로 준비한다.

보관함으로 옮기는 단계는 복구 가능성을 유지한다. 실제 공간을 해제하는 purge는 보관 후 30일이 지난 항목에 명시적으로 실행한다. purge 후에는 보관된 payload를 제거하고 처리 메타데이터를 유지한다. restore와 purge는 같은 archive ID로 반복 실행할 수 있다.

dashboard의 **Storage & recovery**에서 후보 선택, 보관함 복구와 영구 삭제를 실행할 수 있다. profile을 선택하면 암호화 백업 생성과 복구 미리 보기·적용도 제공한다. 백업 공개키와 기본 디렉터리는 `devtools backup configure`로 준비한다.
