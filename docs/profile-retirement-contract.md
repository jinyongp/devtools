# Profile retirement 계약

상태: 구현 전 계약
기준: 2026-09-18 `main`

## 목적

사용이 끝난 profile을 안전하게 보관해 active profile 목록과 실행 경로에서 제외하고,
필요하면 같은 환경에서 다시 복구할 수 있게 한다.

Profile 데이터는 현재 여러 저장 영역에 걸쳐 있다.

| 영역 | 현재 소유 데이터 | retirement 처리 |
| --- | --- | --- |
| `profiles/<hex>.json` | var, sec, env | 암호화 archive에 포함하고 active 파일 제거 |
| `tasks/<hex>.json` | task/workstream/validation 이력, run/context/receipt | 정규화한 snapshot을 archive에 포함하고 active 파일 제거 |
| `ports/registry.json` | profile별 instance, assignment | 해당 profile의 instance와 assignment 제거 |
| `processes/<execution>/record.json` | managed process 이력 | 유지 |
| `processes/<execution>/output.log` | 선택적으로 보관한 raw log | 유지하고 기존 retention 적용 |
| `processes/receipts` | process mutation 재시도 기록 | 유지하되 retired profile의 start/restart 재개는 차단 |
| `project-operations` | project up/down/restart receipt | 유지하되 retired profile의 up/restart 재개는 차단 |
| `backup-receipts/values-<profile>-*` | Dashboard value mutation receipt | active profile과 함께 무효화 |
| task/dashboard query cache | profile별 조회 snapshot | retirement/restore 성공 시 무효화 |
| 프로젝트의 `devtools.toml` | profile 선택과 command/port 설정 | 수정하지 않음 |

Process 이력과 raw log를 archive payload에 복제하지 않는다. 기존 process cleanup과 log expiry가
동일한 보존 정책을 계속 적용한다.

## 수명주기 모델

Profile identity에 별도 lifecycle registry를 추가한다. 기존 저장 파일 유무만으로 retired 상태를
표현하지 않는다.

최소 상태는 다음과 같다.

- `active`: 기존 동작을 허용한다. 기존 profile은 registry entry가 없어도 active로 해석한다.
- `retired`: archive가 존재하고 active mutation/start를 거절한다.
- `purged`: archive payload가 제거된 tombstone. 초기 구현에서는 생성하지 않는다.

기존 설치의 profile migration을 요구하지 않는다. Retire 시 처음 lifecycle entry를 만든다.

Retired tombstone은 process history가 남아 있어도 profile을 active discovery에서 제외하고,
values/tasks 파일이 사라진 상태에서 같은 이름의 빈 profile이 자동 생성되는 것을 막는다.

## 공개 명령

초기 구현 범위는 다음 명령으로 제한한다.

```sh
devtools profile archive PROFILE
devtools profile archive PROFILE --apply DIGEST --request-id UUID
devtools profile archives
devtools profile restore ARCHIVE_ID --identity-file FILE
devtools profile restore ARCHIVE_ID --identity-file FILE --apply DIGEST --request-id UUID
```

`archive`와 `restore`의 기본 동작은 preview다. 실제 변경에는 preview가 반환한 digest와
새 request ID를 함께 전달한다.

초기 구현에는 permanent purge와 profile 이름 재사용을 넣지 않는다. Archive payload 삭제와
retired 이름 재사용은 process history 및 tombstone 정책과 함께 별도 계약으로 다룬다.

## Archive preview

Preview는 maintenance와 process 상태를 읽고 다음 항목을 반환한다.

- profile 이름
- values/tasks 존재 여부
- env/task/workstream/validation 개수
- 연결된 instance/assignment 개수
- stored process history 개수
- active process 개수
- archive 적용 가능 여부와 blocker
- current state에 묶인 digest
- secret 값과 raw log를 제외한 metadata diff/summary

다음 조건은 적용 blocker다.

- profile이 존재하지 않음
- 이미 retired 상태
- active managed process 또는 살아 있는 supervisor lease 존재
- 읽을 수 없거나 손상된 values/tasks/port storage
- backup recipient 미설정
- 현재 다른 maintenance operation과 충돌
- retirement 대상 state가 preview 이후 변경됨

Task의 logical run이 running 상태인 것은 blocker로 사용하지 않는다. Archive snapshot을 만들 때
active task run을 release하고 execution context/receipt를 제거한다. Retirement 이후 늦은 task
요청은 tombstone 검사에서 거절한다.

## Archive payload

Archive는 configured age X25519 recipient로 암호화한다. Agent가 archive를 생성할 때 private
identity가 필요하지 않다.

암호화 평문에는 다음 데이터만 포함한다.

- archive format version
- profile identity
- 생성 시각
- values snapshot
- 정규화된 tasks snapshot
- retirement 당시 instance metadata 요약

Instance metadata 요약은 사용자에게 이전 연결 상태를 설명하기 위한 정보다. Port registry
record와 assignment 자체는 restore 대상에 포함하지 않는다.

Task snapshot은 기존 backup restore와 동일한 정규화 규칙을 사용한다.

- task/workstream/validation 이력 유지
- running claim에 release event 추가
- execution context 제거
- task retry receipt 제거

Values snapshot은 기존 backup validation/restore 경계를 사용한다.

Archive manifest에는 cipher digest, profile, 생성 시각, values/tasks 존재 여부와 개수 metadata만
둔다. Secret 값, task execution context, process control token, raw log는 manifest에 기록하지 않는다.

## Retirement apply

적용 시 lock 순서를 고정한다.

1. managed process operations lock
2. global maintenance lock
3. port registry lock

Managed process start는 같은 operations lock 안에서 retired tombstone을 확인하도록 변경한다.
따라서 active process 검사와 tombstone 게시 사이에 새 process가 생기지 않는다.

Task/value mutation은 global maintenance lock 아래에서 tombstone을 확인한다. Port mutation은
profile lifecycle gate를 통과한 뒤 registry lock을 사용한다.

Apply는 preview digest를 현재 state와 다시 비교한다. 일치할 때 다음 변경을 하나의 recoverable
transaction으로 게시한다.

- encrypted archive와 manifest를 완성
- retired tombstone 생성
- active values 파일 제거
- active tasks 파일 제거
- profile-scoped value management receipt 제거
- 해당 profile의 port instance와 assignment 제거
- task/dashboard query snapshot 무효화

현재 `maintenance.Replace`는 profiles/tasks/backup-receipts만 교체할 수 있고 port registry와
profile lifecycle entry를 다루지 않는다. 구현 전에 recoverable replacement 범위를 확장하거나,
동등한 durability를 제공하는 전용 profile retirement transaction을 추가해야 한다.

중간 실패나 프로세스 종료 뒤 다음 cooperating storage access가 before-image를 복구할 수 있어야 한다.
Port state만 제거되거나 values/tasks만 제거된 반쪽 상태를 정상 결과로 허용하지 않는다.

Archive cipher를 작성한 뒤 active transaction이 실패했다면 active profile은 그대로 유지한다.
미참조 archive 파일은 정리 가능한 orphan으로 취급한다.

## Retired profile의 동작

Retired 상태에서는 다음 mutation/start를 `profile_retired`로 거절한다.

- var/sec/env 변경
- task/workstream/validation mutation과 claim
- process start/restart
- project up/restart
- port allocate, instance name/remove 등 profile runtime state 변경
- 같은 이름을 대상으로 하는 profile import/backup restore replacement

다음 read/history 동작은 허용한다.

- `profile archives`와 archive metadata 조회
- 기존 execution ID의 `process status`, `process logs`
- cleanup의 기존 process/log retention 처리
- 프로젝트 설정 자체를 읽는 `project inspect`

`doctor`가 retired profile을 선택하면 `profile_retired` 상태와 restore remedy를 structured check로
보고한다. Command 실행은 시작 전에 같은 gate에서 실패한다.

Active profile 목록과 Dashboard profile selector에서는 retired profile을 제외한다. Historical
process record만 남아 있어도 active profile로 다시 나타나지 않는다.

## Restore preview

Restore는 archive identity file로 전체 cipher를 인증·복호화한 뒤 payload와 manifest를 검증한다.

Preview는 다음 충돌을 확인한다.

- tombstone의 archive ID와 요청 archive가 다름
- 같은 profile 이름으로 active values/tasks data가 다시 생김
- 같은 profile의 port instance가 이미 생김
- retirement 이후 lifecycle state가 변경됨
- archive payload 손상 또는 잘못된 identity
- 지원하지 않는 archive format

Process history 존재는 restore blocker가 아니다. 기존 history는 같은 profile의 과거 실행 이력으로
계속 유지한다.

Port instance/assignment는 restore하지 않는다. 프로젝트 디렉터리가 아직 존재하면 다음
`process start`, `project up`, `port allocate`에서 현재 설정과 사용 가능한 port를 기준으로
다시 연결한다. Strict port 선언은 그 시점의 일반 port conflict 규칙을 따른다.

## Restore apply

Restore도 digest와 request ID를 사용한다.

Apply는 global maintenance transaction 아래에서 다음 변경을 게시한다.

- values snapshot 복원
- normalized task snapshot 복원
- retired tombstone을 restored 상태로 전환하거나 active discovery에서 제거
- profile-scoped stale value receipt 제거
- task/dashboard query cache 무효화

Task context와 retry receipt는 archive 이전 값으로 되살리지 않는다. Retirement 전에 받은 context나
응답 유실된 mutation을 restore 뒤 재전송해도 새 active state를 변경할 수 없어야 한다.

Restore 성공 후 project runtime state는 새 instance로 다시 준비한다.

## Request ID와 재시도

Archive/restore apply는 별도 durable receipt를 사용한다.

- 같은 request ID와 같은 digest/input: 최초 결과 재생
- 같은 request ID와 다른 archive/profile/options: `request_conflict`
- preview 뒤 source/tombstone 변경: `revision_conflict`
- 응답 유실: 같은 request ID와 동일 입력 재시도

Retired profile에 대한 과거 process/project start receipt는 새 process 생성에 사용하지 않는다.
Completed historical result를 조회하는 API와 lifecycle mutation API를 구분한다.

## 보안

- Archive cipher는 configured public recipient로 암호화한다.
- Restore에만 private identity file을 입력한다.
- Identity와 secret 원문을 argv, JSON response, manifest, history에 기록하지 않는다.
- Raw process log를 archive에 복사하지 않는다.
- Archive manifest와 tombstone은 사용자 전용 private file로 저장한다.
- Dashboard restore가 추가되더라도 identity 원문을 저장하거나 HTML에 다시 노출하지 않는다.

## 오류 계약

초기 오류 코드는 다음을 사용한다.

- `profile_not_found`
- `profile_retired`
- `profile_active` — active managed process가 있어 archive 불가
- `backup_not_configured`
- `invalid_archive`
- `invalid_identity`
- `revision_conflict`
- `request_conflict`
- `storage_error`
- `canceled`

오류 details에는 profile/archive ID와 안전한 blocker metadata만 포함한다.

## 구현 전 구조 변경

Retirement mutation 구현 전에 다음 기반 작업이 필요하다.

1. values/tasks/services/ports가 공통으로 조회할 수 있는 저수준 profile lifecycle/tombstone package
2. Catalog의 active/retired discovery 분리
3. process/project start가 receipt 진행 전에 retired 상태를 확인하는 gate
4. values/tasks/port mutation의 common lifecycle gate
5. port registry와 lifecycle tombstone까지 rollback 가능한 multi-file transaction
6. archive receipt와 orphan cipher 정리
7. profile-scoped value management receipt invalidation

이 기반이 없으면 values/tasks만 제거하는 archive 기능을 추가하지 않는다.

## 검증 기준

구현 시 최소 다음 회귀를 요구한다.

- active process가 하나라도 있으면 archive 적용 거절
- active 검사와 apply 사이 process start race 차단
- values/task mutation과 retirement 동시 실행 직렬화
- retirement 중 실패를 values/tasks/ports/tombstone 어느 단계에 주입해도 원본 복구
- retirement 성공 뒤 var/task/process/project/port mutation이 `profile_retired` 반환
- process status/log history는 retirement 뒤에도 조회 가능
- catalog/dashboard selector에서 retired profile 제외
- encrypted archive에 values/tasks만 포함되고 raw log/control token은 없음
- restore가 running task context를 되살리지 않음
- stale value receipt가 restore 뒤 재생되지 않음
- restore 뒤 runtime instance가 새로 연결됨
- 잘못된 identity, 손상 cipher, stale digest, request conflict
- symlink/alias project location이 retirement identity를 분리하지 않음
- backup/profile import가 retired 이름을 암묵적으로 재활성화하지 않음

## 결론

현재 저장 구조에서는 profile 파일 두 개만 제거하면 retirement 요건을 충족하지 못한다. 첫 구현은
encrypted archive, retired tombstone, runtime port 정리, historical process 보존을 하나의 모델로 적용한다.
Permanent purge와 retired 이름 재사용은 별도 후속 계약으로 둔다.
