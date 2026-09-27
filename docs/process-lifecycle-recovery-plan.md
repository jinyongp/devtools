# Process lifecycle / cleanup recovery plan

## 상태

상태: 구현 전 설계 계획
기준: 2026-09-27 `main` @ `6d93f0a3c24976a108cf9c4aa374dbe6c467559e`

이 계획은 다음 세 결함을 하나의 process lifecycle correctness 작업으로 해결한다.

1. 완료 process record를 cleanup한 뒤 `profile list`가 `process_not_found`로 실패한다.
2. cleanup apply가 일부 항목을 처리한 뒤 실패하면 같은 request ID로 남은 항목을 이어서 처리하지 못한다.
3. `starting` record만 남고 supervisor가 사라진 실행은 같은 start request ID에서 영구적으로 `process_pending`이 된다.

현재 공개 CLI 상태 이름과 retry contract는 유지한다. 같은 start request ID가 supervisor 손실 뒤 새 supervisor를 자동으로 다시 생성하도록 바꾸지 않는다.

## 목표

- active, archived, corrupt process storage를 저장 계층에서 구분한다.
- 정상 cleanup으로 제거한 record는 profile/process enumeration을 깨뜨리지 않는다.
- record가 이유 없이 사라진 저장소 손상은 계속 오류로 감지한다.
- cleanup apply는 최초 mutation 시작 시 선택된 workset을 durable하게 고정하고, 자신의 선행 mutation 때문에 재시도가 막히지 않는다.
- 응답 유실·프로세스 중단 뒤 같은 cleanup request ID가 완료된 항목을 반복하지 않고 미완료 항목을 이어간다.
- start가 publish한 `starting` record의 supervisor가 실제로 사라졌다면 같은 request ID 재시도가 해당 execution을 durable `interrupted/supervisor_lost`로 종결한다.
- 살아 있거나 기동 중인 supervisor를 lost로 오판하여 중복 프로세스를 만들지 않는다.
- 기존 private permissions, atomic replacement, fsync, process operations lock, maintenance lock 보장을 유지한다.

## 비목표

이번 작업에는 다음을 포함하지 않는다.

- restart preflight 순서 변경
- foreground TTY/process-group 처리
- read context propagation
- profile filename representation 변경
- raw log 저장 알고리즘 변경
- task journal snapshot/lock 구조 변경
- process archive 전체 구조나 retention 기간 변경
- 동일 start request ID에서 replacement supervisor 자동 실행
- stale control/lease 파일의 별도 retention 정책

## 1. Process storage 상태 모델

현재 `processes/<execution-id>/`의 UUID 디렉터리 존재만으로는 record가 active인지 cleanup으로 archive된 것인지 구분할 수 없다. 이를 명시적 상태로 만든다.

각 execution directory는 저장 계층에서 다음 네 상태 중 하나다.

### Active

- `record.json`이 존재하고 유효하다.
- `record.archive.json`은 없거나, restore/archive의 짧은 전이 중 유효한 marker가 함께 존재할 수 있다.
- enumeration과 direct status의 정상 대상이다.

### Archived

- `record.json`이 없다.
- 유효한 `record.archive.json` marker가 존재하거나, marker 도입 전 릴리스가 만든 **valid legacy completed-process archive proof**가 존재한다.
- process history가 cleanup archive로 이동된 의도적인 상태다.
- active process/profile enumeration에서는 제외한다.
- direct `process status EXECUTION_ID`는 기존 공개 계약대로 `process_not_found`로 취급한다.

### Missing

- execution directory 자체가 존재하지 않는다.
- direct lookup은 `process_not_found`다.
- enumeration에는 대상 자체가 없다.

### Corrupt

- UUID execution directory는 존재하지만 `record.json`, 유효한 archive marker, valid legacy archive proof가 모두 없다.
- archive marker 또는 legacy proof metadata가 malformed/private-file 규칙을 위반한다.
- enumeration에서 조용히 건너뛰지 않고 storage 오류로 처리한다.

이 구분을 `services.Store`의 공통 storage inspection 경계로 둔다. `List()`, `StoredRecords()`, `ProfileNames()`, direct record read가 각자 파일 부재를 해석하지 않는다.

### Marker 이전 릴리스의 legacy archived proof

현재 릴리스까지의 `completed_process` cleanup은 archive entry/payload를 만든 뒤 `record.json`을 삭제했지만 execution directory에 tombstone을 남기지 않았다. 따라서 업그레이드 직후 존재하는 unmarked UUID hole을 전부 corruption으로 바꾸면 과거 정상 cleanup 결과를 깨뜨린다.

공통 compatibility reader를 `internal/archiveproof` 같은 중립 package로 둔다. `services`가 `cleanup`을 import해 cycle을 만들거나 두 package가 archive JSON 해석을 복제하지 않는다. **services enumeration은 먼저 process directories를 검사하고 unmarked UUID hole이 하나 이상 있을 때만 proof reader를 호출한다.** 따라서 정상 active/marker-only storage가 unrelated cleanup archive 상태에 새로 종속되지 않는다.

reader는 `archives/<archive-id>/entry.json`의 최소 wire metadata를 읽어 completed-process proof index를 만든다. private regular `entry.json`을 읽을 수 있고 wire metadata가 valid completed-process proof로 해석되는 entry만 index에 넣는다. malformed/unknown/unrelated archive entry는 proof 후보에서 제외하되 그 자체 때문에 process enumeration 전체를 실패시키지 않는다. 특정 unmarked hole에 대해 결과적으로 valid proof가 하나도 없으면 그 hole이 corruption이므로 process enumeration은 storage error가 된다. archives root 자체를 읽을 수 없거나 private-storage 검증이 실패해 proof 존재 여부를 판단할 수 없는 상태에서 unmarked hole이 있으면 역시 storage error다.

valid proof 조건:

- archive directory 이름/entry ID가 같은 valid UUID
- `kind == "completed_process"`
- `source == <data>/processes/<execution-id>/record.json` exact path
- valid exact profile
- non-zero `archived_at`
- `restored_at == nil`
- private regular entry metadata
- `purged_at` 여부는 proof를 무효화하지 않음. purge는 payload만 제거하고 entry metadata를 유지하기 때문

archive payload 존재를 proof 필수조건으로 사용하지 않는다. 이미 purge된 정상 old archive도 계속 archived state로 판정해야 한다.

같은 record source를 가리키는 old archive entry가 여러 개라면 valid proofs를 `archived_at, archive_id` 순으로 정렬한다. profile이 서로 다르면 corruption이다. profile이 같으면 첫 proof를 canonical legacy owner로 사용한다. 이는 old cleanup이 archive entry write 뒤 source remove 전에 실패하고 사용자가 새 preview/request로 다시 cleanup해 duplicate entry를 만든 경우에도 deterministic하다.

passive `profile list/process list/status`는 legacy proof를 근거로 archived state를 해석할 뿐 marker를 쓰지 않는다. 따라서 조회가 migration mutation이 되지 않는다. 이후 old archive를 restore해 `record.json`이 다시 생기면 active record가 우선하며 legacy proof는 enumeration에 영향을 주지 않는다. 그 record를 새 버전에서 다시 cleanup하면 그때부터 정상 marker를 만든다.

archive entry가 `restored_at != nil`인데 record가 사라진 경우는 legitimate legacy archive proof가 아니다. out-of-band deletion으로 보고 corruption을 유지한다.

## 2. Archived record marker

새 marker는 execution directory의 `record.archive.json`에 private regular file로 저장한다.

최소 schema:

```json
{
  "version": 1,
  "archive_id": "cleanup item UUID",
  "archived_at": "RFC3339 timestamp",
  "profile": "ExactProfileName",
  "ended_at": "RFC3339 timestamp",
  "capture_logs": true
}
```

marker는 command/directory/env/control token 같은 process record payload를 복제하지 않는다. 역할은 (a) `record.json` 부재가 정상 cleanup 결과임을 증명하는 durable tombstone이면서, (b) record가 먼저 archive된 뒤에도 남아 있는 `output.log`의 최소 소유권/retention metadata를 제공하는 것이다. `profile`, `ended_at`, `capture_logs`는 원래 valid `Record`와 exact하게 검증해 marker에 기록한다.

`services.Store`가 marker의 생성·검증·제거 규칙을 소유하고 cleanup은 해당 API를 사용한다. cleanup package의 archive 내부 형식을 services package가 역으로 읽게 만들지 않는다.

marker는 cleanup archive payload 자체가 아니므로 archive `purge` 때 제거하지 않는다. purge 후에도 archived execution directory가 남아 있는 동안 `record.json` 부재의 정상 근거로 유지한다. 향후 execution directory 자체를 제거하는 별도 GC가 도입되면 directory 전체 삭제를 atomic lifecycle 경계로 다루며, marker만 먼저 지워 corrupt hole을 만들지 않는다.

### Completed process retirement 순서

`completed_process`를 retire할 때 순서는 다음과 같다.

1. source `record.json`을 읽고 preview stamp와 일치하는지 확인한다.
2. cleanup archive payload를 durable하게 쓴다.
3. cleanup archive entry metadata를 durable하게 쓴다.
4. 같은 archive ID를 가리키는 `record.archive.json`을 durable하게 쓴다.
5. 원본 `record.json`을 제거한다.

따라서 `record.json` 제거 전에 항상 archive payload와 marker가 존재한다.

중간 실패 시:

- 2~3 전에 실패: 원본 active record 유지.
- 3 이후 marker 전에 실패: 원본 record 유지, retry가 marker부터 이어간다.
- marker 이후 remove 전에 실패: record+marker가 함께 존재하며 active로 읽힌다. retry가 remove를 이어간다.
- remove 이후 응답 유실: marker가 archived 상태를 증명하므로 retry가 완료 상태를 재구성한다.

### Restore 순서

`completed_process` restore는 다음 순서로 처리한다.

1. archive payload 검증.
2. 원래 `record.json`이 없으면 atomic write로 복원한다.
3. 같은 archive ID의 `record.archive.json`을 제거한다.
4. marker 제거까지 성공한 뒤 archive entry의 `restored_at`을 기록한다.

record와 marker가 잠시 함께 존재하는 상태는 active로 해석한다. marker 제거 실패는 restore 완료로 기록하지 않아 같은 archive ID로 재시도할 수 있게 한다.

## 3. Enumeration 규칙 통일

`services.Store`에 execution directory를 검사하는 내부 primitive를 만들고 다음 규칙을 공통 적용한다.

- valid active record: 반환
- valid archived marker + no record: skip
- no record/marker + valid legacy completed-process proof: legacy archived로 skip
- directory absent: direct lookup은 `process_not_found`
- UUID directory + no record + no marker + no legacy proof: storage error
- malformed record/marker/proof: storage error

현재 `List()`가 모든 `process_not_found`를 무조건 skip하고 `StoredRecords()`는 같은 상황을 즉시 실패시키는 비대칭을 제거한다.

이 변경 후 정상 completed-process cleanup은 `process list`와 `profile list` 모두 성공하며, 임의로 `record.json`만 삭제한 손상 fixture는 두 enumeration 경로에서 오류가 유지된다.

process/profile enumeration은 archived marker/legacy proof를 skip하지만 cleanup 전용 storage scan은 archived owner metadata를 읽을 수 있어야 한다. 따라서 completed process record만 먼저 archive되고 `output.log`가 남은 경우에도 다음 `cleanup preview --profile ...`가 그 로그를 `expired_log` 후보로 다시 발견한다.

future marker는 원래 `ended_at`/capture를 보존한다. marker 이전 legacy proof는 exact ended/capture를 갖고 있지 않아도 안전하게 residual log를 정리할 수 있다. old `completed_process` archive 자체가 종료 후 30일 cutoff를 통과해야만 생성됐으므로, valid legacy proof가 있는 execution directory에 `output.log`가 남아 있다면 그 file은 raw-log 7일 retention을 이미 충족한 것으로 본다. file 존재 자체가 captured output의 증거이며 archive payload가 purge된 뒤에도 entry metadata proof는 남는다.

### Expired-log owner stamp

`expired_log` preview stamp는 `output.log` bytes만 해시하지 않는다. owner representation에 따라 stable metadata와 logical log bytes를 canonicalize한다.

```text
active-or-marker owner = {
  mode: "record",
  profile,
  ended_at,
  capture_logs
}

legacy owner = {
  mode: "legacy_completed_process",
  profile,
  archive_id,
  archived_at
}

stamp = sha256(canonical(owner) || logical-log-bytes)
```

active `record.json`과 future `record.archive.json`이 함께 존재하는 transition에서는 profile/ended_at/capture metadata가 exact match해야 하고 record-mode stamp가 동일하다. archive ID/archived_at은 future record→marker 전환의 stamp에는 넣지 않는다.

unmarked legacy hole은 canonical proof로 선택된 archive entry의 immutable `archive_id/profile/archived_at`만 owner stamp에 사용한다. `purged_at`은 나중에 바뀔 수 있으므로 포함하지 않는다. 따라서 archive payload purge가 log preview를 거짓 conflict로 만들지 않는다.

cleanup apply의 expired-log retire도 source 제거 전 동일 owner resolver와 logical log reader를 사용해 stamp를 다시 검증한다. owner metadata나 log 내용이 실제로 바뀌면 `revision_conflict`다. 이 경로가 없으면 partial failure뿐 아니라 과거 릴리스에서 record item만 cleanup한 사용자의 log가 영구 고아가 된다.

## 4. Cleanup apply durable workset

현재 incomplete retry는 매번 `scan()`을 다시 실행한다. 이것이 이미 retire된 process record 때문에 아직 남은 log를 발견하지 못하는 직접 원인이다.

apply semantics를 두 단계로 분리한다.

### Apply 시작 전

아직 해당 request ID의 receipt가 없을 때만:

1. preview snapshot 존재와 TTL을 검증한다.
2. 선택 ID가 preview에 실제 포함되어 있는지 검증한다.
3. 현재 `scan()` 결과와 kind/source/stamp를 비교하여 preview stale 여부를 검증한다.
4. 선택된 `candidate` 전체를 exact workset으로 receipt에 저장한다.
5. receipt write가 성공한 뒤에만 첫 retire mutation을 시작한다.

즉 preview freshness는 "apply 시작 가능 여부"의 gate다.

### Apply 시작 후

incomplete receipt가 존재하면:

- preview TTL을 다시 gate로 사용하지 않는다.
- `scan()`으로 workset을 재발견하지 않는다.
- receipt에 저장된 exact candidates를 기준으로 이어간다.
- 각 candidate 자체의 source stamp/runtime 조건은 `retire()`가 실행 시 다시 확인한다.
- 이미 archive entry가 존재하는 항목은 idempotent retirement 경로로 수렴한다.

receipt에는 기존 `plan`, `ids`, `done`, `result`에 더해 durable selected candidates를 저장한다. 각 retire 성공 뒤 현재 누적 `result`를 progress receipt에 다시 기록한다. progress receipt write가 실패하거나 응답이 유실되더라도 archive entry 자체를 보고 retry가 완료 항목을 재구성할 수 있어야 하며, 이미 retire된 source를 다시 요구하지 않는다.

### 기존 incomplete receipt 호환

업그레이드 이전 형식의 incomplete receipt에는 selected candidate가 없다.

- 참조한 preview snapshot이 아직 존재하면 그 snapshot에서 선택 candidate를 재구성해 새 receipt 형식으로 승격한다.
- 이미 생성된 archive entry는 그대로 재사용한다.
- preview snapshot까지 사라져 남은 candidate identity/stamp를 복구할 수 없는 경우에는 추측하지 않고 `revision_conflict`를 반환한다.

completed receipt의 replay contract는 그대로 유지한다.

## 5. Starting execution reconciliation

동일 start request ID는 execution ID이기도 하므로 replacement execution을 만들지 않는다.

기존 record가 다음 상태면 기존 의미를 유지한다.

- `StartedAt != nil`: 저장된 실행 결과로 수렴
- `EndedAt != nil`: terminal 결과로 수렴
- `StartedAt == nil && EndedAt == nil`: supervisor ownership을 확인

### 신규 start: lease ownership을 fork/exec 전에 handoff

신규 실행은 "record를 쓰고 supervisor가 나중에 lease를 잡는" 순서를 더 이상 사용하지 않는다. caller가 supervisor를 시작하기 **전에** `lease.lock`의 exclusive flock을 획득하고 그 open file descriptor를 supervisor child에 상속한다. Linux `flock`은 open file description에 연결되고 fork/exec를 거쳐 유지되므로 parent가 종료되어도 child가 같은 descriptor를 보유하는 동안 lock이 유지된다. macOS/WSL은 release targeted test에서 같은 handoff contract를 검증한다.

execution directory에 private `launch.json` protocol marker를 추가한다.

```json
{
  "version": 1,
  "lease_handoff": "inherited-fd"
}
```

start publish는 UUID execution directory를 바로 만들지 않고 같은 `processes/` filesystem의 hidden staging directory에서 완성한다. staging name은 `.launch-<execution-id>-<nonce>`처럼 valid execution ID와 절대 겹치지 않는 fixed pattern을 사용한다. process enumeration은 valid UUID directory만 보므로 staging은 공개 state가 아니다. `operations.lock`을 획득한 새 mutation은 시작 시 남아 있는 private stale `.launch-*` directory를 검증 후 제거한다; 같은 lock 때문에 active start staging과 경쟁하지 않는다.

순서:

1. `os.Executable()` 등 parent-side hidden supervisor command 준비를 먼저 끝낸다.
2. `processes/` 아래 private staging directory(0700)를 만든다.
3. staging의 `lease.lock`을 exclusive acquire하고 **fd 자체를 보유**한다.
4. staging에 `state=starting` record와 `launch.json` v1 marker를 각각 durable하게 쓴 뒤 staging directory를 sync한다.
5. staging directory를 `processes/<execution-id>`로 atomic rename하고 `processes/` root를 sync한다. 이 rename이 execution record의 visible publication point다. destination UUID dir가 이미 존재하면 overwrite하지 않고 conflict/storage error로 중단한다.
6. published path의 `lease.lock`이 inherited fd와 같은 file인지 다시 검증한다.
7. `exec.Cmd.ExtraFiles`로 lease fd를 child fd 3에 전달하고 hidden invocation에 lease-fd 3을 명시한다.
8. `cmd.Start()`이 성공한 뒤 parent copy를 close한다. child가 같은 open file description을 계속 보유하므로 flock ownership에는 빈 창이 없다.
9. supervisor는 inherited fd를 private regular `lease.lock` path와 `os.SameFile`/fstat로 검증하고, 별도 fd로 flock을 다시 획득하지 않는다. 검증 직후 **해당 fd에 close-on-exec를 설정**해 이후 managed command/probe exec가 lease fd를 재상속하지 못하게 한다. inherited lease를 보유한 상태에서 record를 읽고 기존 `EndedAt != nil` fencing check를 수행한 뒤 command를 시작한다.
10. supervisor 종료 시 inherited fd를 close하여 lease를 해제한다. managed child가 supervisor보다 오래 살아도 lease를 보유할 수 없어 supervisor loss 판정이 child fd leak 때문에 영구 pending되지 않는다.

visible UUID rename 전 record/marker/lease 준비가 실패하면 staging을 제거하고 execution은 존재하지 않는 것으로 남긴다. staging cleanup 자체가 실패해도 UUID enumeration을 깨뜨리지는 않지만 다음 operations-lock holder가 cleanup을 재시도하며 persistent private-path 오류면 새 mutation 전에 storage error로 막는다.

UUID publication 이후 `cmd.Start()`이 실패하면 parent는 아직 lease를 보유한다. lease를 해제하기 전에 published `record.json`을 terminal `failed/supervisor_start_failed`로 durable update한다. 따라서 visible UUID directory가 `record.json` 없는 hole로 남지 않는다.

parent crash window:

- staging rename 이전 crash: public execution record 없음; hidden staging은 다음 operations mutation이 정리한다.
- UUID publication 이후 child fork 전 crash: v1 marker + free lease이므로 supervisor 부재를 즉시 확정할 수 있다.
- fork/exec 이후 parent crash: child가 inherited descriptor를 보유하므로 lease는 계속 held 상태다. child scheduling/RPC publication이 늦어도 retry가 terminalize할 수 없다.
- child exec/start 자체가 실패하면 inherited copy도 사라지고 parent가 terminal failure record를 게시한 뒤 lease를 해제한다.

이 구조는 PID/generation fencing을 추가하지 않고 현재 flock lease 자체를 launch ownership token으로 만든다.

### legacy starting record fallback

marker 도입 전 릴리스의 `starting` record에는 inherited-handoff proof가 없다. 신규 staging protocol에서는 visible UUID publication 전에 marker까지 durable하므로 새 버전이 marker 없는 fresh record를 정상적으로 만들지 않는다. 이 legacy state에만 기존 start polling deadline과 동일한 10초 startup grace를 compatibility fallback으로 사용한다.

- grace 안에서 RPC가 없고 lease가 free여도 `starting/process_pending`을 유지한다.
- grace가 지난 뒤 lease를 실제 acquire하고, 보유한 상태에서 record를 다시 읽어 여전히 unstarted이면 `interrupted/supervisor_lost` terminal record를 publish한다.
- lease를 얻지 못하면 old/new supervisor가 실제로 살아 있을 수 있으므로 pending을 유지한다.

### reconciliation primitive

`Status()`, start retry, stop이 서로 다른 사실을 만들지 않도록 protocol marker + lease probe/claim을 공통화한다.

- `Status()`는 read-only다. RPC 실패 시 v1 handoff marker가 있고 lease가 held이면 `StartedAt == nil`인 record는 stored `starting`을 그대로 반환한다. 이미 `StartedAt != nil`인데 RPC만 unavailable한 경우에는 기존 `unknown/supervisor_unavailable` 의미를 유지한다. lease가 free이면 effective `interrupted/supervisor_lost`를 **grace 없이** 판정한다. marker가 없으면 unstarted legacy record에만 10초 grace를 적용한다.
- start retry는 같은 판정을 사용한다. lost를 terminalize할 때는 boolean `leaseFree` 결과로 write하지 않고 lease fd를 실제 acquire/hold -> record 재읽기 -> terminal publish -> release 순서를 사용한다.
- explicit stop은 사용자 취소 의도이므로 marker 유무와 관계없이 startup grace를 기다리지 않는다. RPC가 없고 lease를 claim할 수 있으면 lease를 보유한 채 record를 재읽고 terminalize한다. lease가 held라면 supervisor/control publication을 기다리는 상태일 수 있으므로 기존 `process_unavailable`/retry 의미를 유지한다.
- late supervisor는 inherited/acquired lease를 보유한 뒤 terminal record를 읽으면 command를 시작하지 않고 종료한다.

따라서 신규 start에는 scheduler race를 grace로 추정하지 않고 lease handoff로 제거하며, old persisted state에만 bounded grace를 사용한다. 같은 execution에 대해 status가 lost를 확정한 뒤 start retry가 영구 `process_pending`으로 남는 불일치도 제거한다.

## 7. 공개 동작

공개 state/error 이름은 추가하지 않는다.

### completed process cleanup 후

```text
cleanup apply  -> exit 0
process list   -> exit 0, archived execution 제외
profile list   -> exit 0
process status <archived-id> -> process_not_found
```

### cleanup 부분 실패 후

```text
첫 apply:
  앞선 item archive 성공
  뒤 item storage_error

같은 plan + item set + request-id 재시도:
  앞선 item 재수행 없음
  남은 item 이어서 처리
  성공하면 replayed: true
```

새 preview가 residual file을 발견할 수 있는지 여부는 resume correctness의 전제가 아니다.

### lost start 후

신규 lease-handoff record:

```text
lease held:
  process status/start retry -> starting or process_pending

lease free + RPC unavailable:
  process status -> interrupted / supervisor_lost
  same request-id start -> terminal interrupted record로 즉시 수렴
```

legacy/no-launch-marker record:

```text
startup grace 안:
  process status -> starting
  same request-id start -> process_pending

grace 이후 lease free:
  process status -> interrupted / supervisor_lost
  same request-id start -> terminal interrupted record로 수렴
```

새 실제 실행이 필요하면 새 request-id로 process start한다.

`project up`은 기존 orchestration대로 terminal/failed child를 확인하면 새로운 child request ID를 발급해 다음 retry에서 새 execution을 시작할 수 있다.

## 8. 구현 작업 단위

### WI-1 — Process storage state primitive

대상:
- `internal/services/store.go`
- `internal/services/store_test.go`

작업:
- archived marker schema/path와 residual-log용 최소 profile/ended_at/capture metadata 추가
- marker 이전 old cleanup archive entry를 검증하는 neutral `archiveproof` compatibility reader 추가
- active/archived/legacy-archived/missing/corrupt inspection helper 추가
- `read`, `List`, `StoredRecords`, `ProfileNames`을 공통 규칙으로 전환
- proof 없는 unmarked missing record만 corruption으로 유지

완료 조건:
- archived marker가 있는 execution은 enumeration에서 제외
- marker 없는 UUID hole이라도 valid un-restored legacy completed-process proof가 있으면 archived로 제외
- proof의 archive payload가 이미 purge되어도 entry metadata만으로 archived 판정 유지
- `restored_at` proof 또는 proof 없는 UUID hole은 storage error
- duplicate valid legacy proofs가 같은 profile이면 deterministic canonical proof를 선택하고 profile이 다르면 corruption
- direct marker/legacy archived status는 `process_not_found`

### WI-2 — Cleanup retirement/resume durability

대상:
- `internal/cleanup/cleanup.go`
- `internal/cleanup/cleanup_test.go`
- `verify/scenarios/cleanup.py`

작업:
- `completed_process` retire/restore에 services marker API 연결
- cleanup receipt에 selected candidate workset 저장
- initial apply와 resume apply 경로 분리
- incomplete resume에서 fresh `scan()` 의존 제거
- archived process marker와 legacy archived proof를 cleanup 전용 scan에 포함해 record-only cleanup 뒤 남은 log 재발견
- archive entry 기반 idempotent progress 복원

완료 조건:
- record cleanup 뒤 profile/process list 성공
- completed process item만 선택해 성공한 뒤 남은 `output.log`가 다음 preview에서 다시 `expired_log`로 발견됨
- record archive 성공 후 log archive가 실패해도 같은 request ID로 log까지 완료
- apply 시작 뒤 preview TTL 만료가 resume을 막지 않음
- source가 외부에서 실제 변경된 remaining item은 계속 `revision_conflict`

### WI-3 — Lost-start reconciliation

대상:
- `internal/services/store.go`
- `internal/services/store_test.go`
- `internal/services/supervisor.go`
- `cmd/devtools/main.go`

작업:
- lease helper가 release closure뿐 아니라 inheritable locked fd ownership을 안전하게 전달할 수 있는 내부 API 추가
- hidden `.launch-*` staging directory에서 lease + `starting` record + `launch.json` marker를 완성한 뒤 UUID execution directory로 atomic rename하여 visible publication
- hidden `__process-serve` invocation에 `ExtraFiles` lease fd를 전달하고 supervisor가 inherited fd를 validate/adopt
- supervisor의 기존 "자기 fd로 lease를 새로 acquire" 경로를 inherited-handoff path와 legacy/internal test fallback으로 분리. **`launch.json` v1 marker가 있으면 inherited fd가 필수**이며 fd 누락/불일치는 command 시작 전 storage/execution failure로 종료한다. marker가 없는 old hidden invocation에서만 self-acquire fallback을 허용한다.
- prelaunch failure는 parent-held lease 아래 record를 terminal failed로 만든 뒤 release
- boolean `leaseFree` 중심 mutation을 lease claim/hold primitive로 분리
- v1 launch marker record는 free lease를 grace 없이 lost로 판정하고, marker 없는 legacy record만 10초 fallback grace 적용
- status/start retry/explicit stop의 protocol-aware supervisor-lost 판정 공통화

완료 조건:
- 신규 start에서 parent가 `cmd.Start()` 직후 종료되어도 child가 inherited lease를 보유해 free-lease window가 발생하지 않음
- `launch.json` v1 + free lease + unstarted record는 same-request retry가 grace 없이 `interrupted/supervisor_lost`로 수렴
- v1 marker + held lease 동안은 status/start retry가 terminalize하지 않음
- UUID staging rename 전 crash는 public execution hole을 만들지 않고 stale `.launch-*`가 다음 operations-lock mutation에서 정리됨
- UUID publication 시점에는 record+launch marker가 모두 durable하므로 새 protocol은 marker 없는 visible `starting`을 만들지 않음
- marker 이전 old starting fixture만 10초 grace fallback으로 안전하게 lost 수렴
- next replay도 같은 terminal result
- explicit stop은 marker 유무와 관계없이 free lease를 claim할 수 있으면 grace 없이 terminalize
- inherited lease fd/path/private permission mismatch는 supervisor가 command를 시작하기 전에 실패
- staging 단계 storage error가 visible UUID hole을 남기지 않음
- UUID publication 후 `cmd.Start()` failure는 parent-held lease 아래 terminal failed record를 남겨 `starting` orphan을 만들지 않음
- terminal record 게시 뒤 도착한 late supervisor는 command를 실행하지 못함
- Linux unit/integration에서 inherited flock이 parent copy close 뒤 supervisor lifetime 동안 유지되고 supervisor exit 뒤 release됨을 검증
- supervisor가 close-on-exec 설정 후 별도 managed child를 exec하고 종료해도 managed child가 lease를 보유하지 않음을 검증
- macOS/WSL release-targeted test에서 동일 handoff/close-on-exec contract 확인

### WI-4 — Contract regression coverage and docs

대상:
- `docs/processes.md`
- `docs/cleanup.md`
- 관련 verify scenario

작업:
- cleanup resume이 apply 시작 뒤 durable workset을 사용한다는 점 명확화
- supervisor-lost start retry는 같은 execution을 interrupted로 확정하고 새 실행은 새 request ID라는 점 명확화
- cleanup -> process list -> profile list 연속 회귀 추가
- marker 이전 릴리스 fixture: archive entry + missing record + no marker 상태에서 process/profile list 성공, residual log preview 가능, purged payload에서도 proof 유지
- restored legacy archive entry + missing record는 corruption으로 남는 negative fixture 추가
- starting -> supervisor lost -> same request retry 연속 회귀 추가

## 9. 검증 계획

작업별 targeted validation:

```sh
go test ./internal/services
go test ./internal/cleanup
```

통합 회귀:

```sh
python3 verify/run.py cleanup
python3 verify/run.py processes
python3 verify/run.py project_lifecycle
python3 verify/run.py profiles
```

pre-close validation:

```sh
go vet ./...
go test -race ./...
python3 verify/run.py all
```

추가로 기존 재현 probe와 upgrade 호환 경계를 확인한다.

1. completed process record cleanup 후 `profile list` 성공
2. record/log cleanup partial failure 후 same request resume 성공
3. orphan starting record가 same request에서 무한 pending되지 않음
4. marker 도입 전 legacy completed-process archive fixture가 upgrade 후 정상 archived로 인식되고 proof 없는 hole은 계속 storage error

## 10. 완료 기준

다음이 모두 만족되어야 이 작업을 완료로 본다.

- 정상 archive state와 corruption을 파일 부재 하나로 구분하지 않는다.
- cleanup retry가 자신이 앞서 제거한 record 때문에 남은 item을 잃지 않는다.
- cleanup의 same-request resume이 preview 재검색이나 TTL에 종속되지 않는다.
- supervisor가 없어진 starting execution은 durable terminal state로 수렴한다.
- lease를 보유할 수 없는 동안에는 lost supervisor라고 확정하지 않는다.
- 동일 start request가 replacement process를 중복 생성하지 않는다.
- 기존 process/cleanup public JSON shape와 state/error 이름을 불필요하게 변경하지 않는다.
- 전체 race/integration 검증이 통과한다.
