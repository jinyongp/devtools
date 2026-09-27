# Devtools correctness, recovery, and storage improvement plan

## 상태

상태: 구현 전 통합 계획
기준: 2026-09-27 `main` @ `6d93f0a3c24976a108cf9c4aa374dbe6c467559e`
대상: 2026-09-27 재현·코드검토에서 확인한 1~9번 문제

이 문서는 구현 순서, 저장계층 경계, 공개 계약, migration, 회귀검증을 하나의 실행계획으로 고정한다.
1~3번의 상세 상태전이와 cleanup resume 설계는 `docs/process-lifecycle-recovery-plan.md`가 하위 설계 문서다. 충돌 시 이 문서의 phase/범위와 하위 문서의 상세 불변식을 함께 만족해야 한다.

## 목표

1. process cleanup/start retry가 partial state에서도 수렴하게 한다.
2. restart가 stop 전에 검증 가능한 실패를 먼저 발견해 정상 실행을 불필요하게 중단하지 않게 한다.
3. foreground `command run`이 실제 TTY의 입력·신호·job-control 의미를 보존하게 한다.
4. lock 대기 중 CLI/HTTP 취소가 즉시 storage boundary까지 전파되게 한다.
5. 공개 profile 128자 계약을 유지하면서 filesystem component 길이에 의존하지 않는 저장 key를 도입한다.
6. raw log capture의 전체 snapshot rewrite를 제거하되 현재 마지막 1 MiB 공개 계약과 private-file 보장을 유지한다.
7. task history 증가와 profile 간 동시 사용에서 전체 journal replay/rewrite 및 global exclusive lock 병목을 제거한다.
8. 기존 atomic replace/recovery, private permissions, symlink 방어, retry-safe request ID 계약을 유지한다.

## 비목표

- 공개 CLI 명령명, JSON envelope, 기존 state/error 이름의 불필요한 변경
- profile 최대 길이를 줄이는 임시 호환성 수정
- process PID를 durable ownership/fencing 수단으로 사용하는 것
- cleanup이 저장손상을 조용히 무시하도록 만드는 것
- raw log를 영구 audit log로 승격하는 것
- task event history를 삭제하거나 retention으로 줄이는 것
- 외부 DB/daemon을 새 storage dependency로 도입하는 것
- 사용자 설정으로 retention/flush/compaction tuning 값을 노출하는 것

## 현재 검증 기준

다음 baseline은 현재 커밋에서 통과한다.

```sh
go vet ./...
go test -race ./...
python3 verify/run.py all
```

하지만 별도 경계 재현에서 1~7번이 확인됐다. 구현은 기존 baseline을 보존하면서 각 경계를 regression test로 편입해야 한다.

---

# Phase 1 — Process lifecycle / cleanup correctness (문제 1~3)

상세 설계: `docs/process-lifecycle-recovery-plan.md`

## P1-1 — archived process storage 상태 명시

현재 UUID process directory와 `record.json` 부재만으로 정상 cleanup과 손상을 구분할 수 없다.

결정:

- `services.Store`가 active / archived / missing / corrupt 상태 판정을 소유한다.
- 정상 completed-process cleanup은 execution directory에 private archive marker를 남긴다. marker는 archive ID뿐 아니라 residual log를 다시 찾는 데 필요한 최소 `profile`/`ended_at`/`capture_logs` metadata를 보존한다.
- process/profile enumeration은 marker-only execution을 skip한다. marker 도입 전 릴리스가 이미 만든 정상 completed-process cleanup은 unmarked UUID hole이므로, `archives/*/entry.json`의 valid un-restored `completed_process` metadata를 공통 `archiveproof` reader로 검증해 **legacy archived proof**로 인정한다. restored proof나 proof 자체가 없는 unmarked hole은 corruption이다. passive 조회는 proof를 읽기만 하고 marker를 새로 쓰지 않는다.
- cleanup 전용 scan은 future marker 또는 legacy proof metadata로 남아 있는 `output.log`를 계속 발견한다. future record/marker owner stamp는 `{mode:"record", profile, ended_at, capture_logs}`, legacy proof owner stamp는 `{mode:"legacy_completed_process", profile, archive_id, archived_at}` + logical log bytes로 계산한다. old completed-process archive는 이미 30일 cutoff를 통과했으므로 legacy proof directory의 residual log는 7일 retention도 충족한 것으로 안전하게 정리할 수 있다. purge가 바꾸는 `purged_at`은 stamp에 포함하지 않는다.
- `List`, `StoredRecords`, `ProfileNames`, direct read가 같은 inspection primitive를 사용한다.
- archived execution은 enumeration에서 제외하고 direct status는 기존 `process_not_found` 계약을 유지한다.

## P1-2 — cleanup apply durable workset

결정:

- 최초 apply에서 preview freshness를 확인한 뒤 selected candidate 전체를 receipt에 durable하게 저장한다.
- 첫 mutation은 receipt write 이후에만 시작한다.
- incomplete retry는 `scan()`이나 preview TTL로 대상을 재발견하지 않는다.
- receipt의 exact candidate + archive entry를 기준으로 진행 상태를 복구한다.
- remaining source가 외부 변경된 경우에는 여전히 `revision_conflict`다.
- legacy incomplete receipt는 preview snapshot이 남아 있으면 승격하고, 복구 근거가 없으면 추측하지 않는다.

## P1-3 — orphan starting execution reconciliation

결정:

- 동일 start request ID는 동일 execution ID를 뜻하며 replacement supervisor를 자동 생성하지 않는다.
- **신규 start는 hidden `.launch-*` staging directory에서 `lease.lock` + `starting` record + `launch.json` v1 marker를 durable하게 완성한 뒤 UUID execution directory로 atomic rename**한다. visible UUID directory가 record 없는 half-state로 나타나지 않는다.
- parent는 staging에서 supervisor fork/exec 전에 `lease.lock`을 acquire하고 fd를 child에 상속한다. `cmd.Start()` 성공 뒤 parent fd를 닫아도 child가 같은 open file description을 보유하므로 scheduler/RPC publication 사이 free-lease window가 없다.
- UUID publication 전 failure는 hidden staging만 정리하고 public execution을 만들지 않는다. publication 후 supervisor launch가 실패하면 parent-held lease 아래 record를 terminal `failed/supervisor_start_failed`로 만든 뒤 release하여 새 orphan `starting`을 남기지 않는다.
- `launch.json` v1 record에서 RPC가 없고 lease가 free면 lost ownership을 grace 없이 확정할 수 있다. start retry는 lease를 실제 acquire/hold -> record 재읽기 -> `interrupted/supervisor_lost` publish 순서로 수렴한다. lease가 held이면 child/supervisor가 살아 있으므로 pending을 유지한다.
- marker 도입 전 old `starting` record에만 기존 10초 startup grace를 compatibility fallback으로 적용한다. 새 staging protocol은 visible UUID publication 전에 marker까지 durable하게 만들므로 marker 없는 신규 visible `starting`을 정상 생성하지 않는다.
- late supervisor는 inherited/acquired lease를 보유한 상태에서 terminal record를 읽으면 command를 실행하지 않는다.
- `Status`는 protocol marker + lease를 같은 규칙으로 read-only 판정한다. explicit stop은 사용자 취소 의도이므로 marker 유무와 관계없이 grace를 기다리지 않고 free lease를 claim한 상태에서 record를 재확인·terminalize한다.

### Phase 1 완료조건

- cleanup -> process list -> profile list가 모두 성공한다.
- marker/proof가 모두 없는 unmarked missing `record.json`은 계속 오류이고, marker 이전 릴리스의 valid legacy archive proof는 정상 archived state로 호환된다.
- cleanup partial failure 뒤 동일 request ID가 잔여 item을 완료한다.
- orphan starting execution이 동일 request ID에서 무한 pending되지 않는다.
- 중복 child/supervisor가 생성되지 않는다.

---

# Phase 2 — Restart pre-stop validation (문제 4)

## 문제

현재 process restart는 old execution을 stop한 뒤 새 start 안에서 `BeforeStart`를 실행한다. project restart는 이 callback을 전달하므로 missing executable과 같은 명백한 실패도 정상 실행을 종료한 뒤 발견한다.

또한 direct `process restart`와 Dashboard restart는 현재 start와 동일한 preflight callback을 설정하지 않아 entry point별 실패 시점도 다르다.

## 결정: preflight를 두 단계로 분리

### A. Non-destructive preflight

old execution을 건드리기 전에 수행한다.

포함:

- 현재 project identity/config 재해석
- command 존재 및 effective env 선택
- value/env 저장소 읽기와 env 유효성
- declared var/sec requirements
- 실행파일/PATH 검사
- bind/serve 정의 및 현재 registry로 해석 가능한 binding
- 현재 process를 중단하지 않고 검사 가능한 project/config 구조 오류

제외:

- old process가 현재 점유하는 port의 free 여부
- stop 이후에만 획득 가능한 claim/reservation
- config 변경으로 새로 필요한 runtime resource의 실제 reservation

이를 위해 `internal/execution`에서 현재 `Preflight`를 다음 경계로 분리한다.

- `Validate(...)`: resource ownership을 바꾸지 않는 stable checks
- `Preflight(...)`: 기존 preview prepare를 사용한 full cold-start checks

`ports`에는 current assignment/binding을 읽되 `Active`/`Available`로 old execution을 충돌 처리하지 않는 read-only binding resolution 경계를 둔다. 새 serve allocation의 availability는 full preflight에 남긴다. config 변경으로 아직 존재하지 않는 새 binding 때문에 argv/PATH 일부를 pre-stop에서 해석할 수 없으면 그 binding에 의존하는 check만 post-stop full preflight로 defer한다. 반대로 literal/현재 해석 가능한 executable 누락은 pre-stop에서 반드시 실패시킨다.

### B. Post-stop full preflight

old execution stop 성공 뒤 기존 full `execution.Preflight`를 다시 수행한다.

목적:

- stop과 start 사이의 상태 변화 재검증
- 실제 port availability/claim 관련 오류 확인
- TOCTOU 때문에 pre-stop 검증만 신뢰하지 않음

### 적용 entry point

동일 정책을 다음에 적용한다.

- `process restart` CLI
- Dashboard process restart
- `project restart`

`services.Store`는 command 의미를 소유하지 않는다. `services.Request`에 restart 전용 phase callback을 추가한다.

```go
type RestartPhase string

const (
    RestartBeforeStop  RestartPhase = "before-stop"
    RestartBeforeStart RestartPhase = "before-start"
)

RestartPreflight func(context.Context, Record, RestartPhase) *protocol.Error `json:"-"`
```

함수 필드는 기존 `BeforeStart`처럼 request fingerprint에서 제외한다. Store가 operations lock 안에서 읽은 authoritative old `Record`를 callback에 직접 넘기므로 caller가 별도 status 조회 결과를 고정하지 않는다.

- old record가 아직 active일 때만 `before-stop` phase를 호출하고, 통과해야 stop mutation을 시작한다.
- old record가 이미 terminal이면 destructive stop이 없으므로 `before-stop`은 생략한다.
- stop 뒤 replacement `start()`에는 `BeforeStart` closure를 주입해 기존 singleton-reuse/preflight 순서를 보존하고, 실제 cold start가 필요해지는 지점에서 `RestartPreflight(ctx, old, before-start)`를 호출한다.
- CLI/Dashboard/project lifecycle callback은 old record의 directory/command와 request의 env override를 결합한다. env override가 없으면 old `EnvOverride`를 상속한다. `before-stop`에서는 `execution.Validate`, `before-start`에서는 full `execution.Preflight`를 호출한다.
- lifecycle manager는 기존 full `Preflight`와 별도로 non-destructive `Validate` dependency를 받아 세 entry point가 동일 정책을 사용하게 한다.

Store는 phase와 retry 순서만 소유하고 validator의 command 의미는 알지 않는다.

`project restart a b ...`는 기존 item-level partial-success 계약을 유지한다. lifecycle manager가 모든 command를 한 번에 preflight한 뒤 batch 전체를 막는 새 all-or-nothing barrier를 추가하지 않는다. 각 미완료 child가 자신의 `Processes.Apply(restart)`에 들어갈 때 authoritative old record를 기준으로 non-destructive `before-stop` validation을 먼저 수행하고, 실패한 child의 old process만 보존한다. 앞서 성공한 restart는 rollback하지 않으며 기존 receipt에 그대로 남는다. 이는 `project up`의 cold-start batch preflight와 의도적으로 다른 restart partial-failure semantics다.

### retry 의미

- pre-stop validation 실패: `changed=false`, old execution 유지. process receipt는 mutation 미시작 상태로 남아 동일 request ID 재시도에서 현재 external config를 다시 검증할 수 있다. config/tool 상태가 고쳐졌다면 같은 요청이 이후 restart를 진행해도 중복 mutation은 없다.
- stop 이후 full preflight 실패: 이미 발생한 stop은 rollback하지 않는다. `changed=true`와 stopped item을 유지한다. 동일 request ID 재시도는 stopped old execution을 다시 stop하지 않고 새 start preflight부터 이어간다.
- project lifecycle의 상위 receipt는 기존 정책대로 non-pending child failure 뒤 새 child request ID를 발급할 수 있지만 최초 고정한 old execution 대상은 바꾸지 않는다.

### Phase 2 회귀

- executable missing -> old process remains running
- invalid env/requirements -> old process remains running
- pre-stop 통과 후 post-stop port conflict -> old process stopped, structured failure
- direct process / project / Dashboard restart의 failure timing 일치
- inherited env/capture semantics 유지

---

# Phase 3 — Foreground TTY process semantics (문제 5)

## 공개 계약

기존 문서의 다음 의미를 실제 TTY에서도 만족해야 한다.

- child stdin/stdout/stderr passthrough
- child exit status 보존
- `SIGINT`, `SIGTERM`, `SIGHUP` 처리
- interactive terminal input 가능
- foreground job stop/resume 가능
- command 종료 후 shell/devtools의 foreground terminal ownership 복원

## 실행 경로 분리

`process.Execute`에서 generic pipe/non-TTY와 interactive TTY를 명시적으로 나눈다.

### Non-TTY

현재 별도 child process group + context signal forwarding 모델을 유지한다.

### Interactive TTY

TTY 판정은 `in`이 `*os.File`이고 해당 fd에 `Tcgetpgrp`가 성공하며 caller process group이 현재 foreground인 경우에만 활성화한다. 단순히 `/dev/tty`가 존재한다는 이유로 pipe stdin을 TTY로 바꾸지 않는다.

interactive 경로에서는 child 전용 process group을 만들고 해당 group에 terminal foreground를 넘긴다. 시작 race를 줄이기 위해 Unix helper는 Go의 `SysProcAttr{Foreground:true, Ctty:ttyFD}` 의미를 사용해 child pgrp 생성과 최초 foreground transfer를 결합한다.

wait/status ownership을 명확히 하기 위해 TTY branch는 `os.StartProcess`/동등한 low-level start + 단일 wait loop를 사용한다. stdout/stderr가 `*os.File`이면 직접 child fd로 넘기고, 내부 호출자가 일반 `io.Writer`를 전달한 경우에만 pipe+copy goroutine을 만들어 wait 종료 후 drain한다. stdin controlling TTY는 child fd 0으로 직접 전달한다.

필수 상태:

- parent foreground pgrp 저장
- child pgrp
- tty fd
- child running/stopped/continued/exited 상태
- parent restore 여부

## job-control 처리

단순 `exec.Cmd.Wait()`만으로는 stopped child를 관찰할 수 없으므로 interactive 경로는 `exec.Cmd.Wait`와 waiter를 섞지 않는다. TTY branch는 직접 child process를 시작하고 하나의 Unix wait loop(`WUNTRACED`/`WCONTINUED`)만 ownership하여 stop/continue/exit 상태를 관찰한다. generic non-TTY branch만 기존 `exec.Cmd` 경로를 유지한다.

상태전이:

1. child 전용 process group 생성 + 최초 foreground transfer
2. child stopped:
   - 현재 tty foreground가 child pgrp일 때만 parent wrapper pgrp로 복원
   - wrapper가 스스로 `SIGSTOP`되어 invoking shell이 top-level job stop을 확실히 인식하게 함
3. shell이 wrapper를 `SIGCONT`로 재개:
   - `Tcgetpgrp`를 다시 확인
   - foreground가 parent wrapper pgrp이면 사용자가 `fg`로 재개한 것이므로 child pgrp로 foreground를 넘긴 뒤 child pgrp에 `SIGCONT`
   - foreground가 다른 pgrp이면 `bg`/background resume으로 보고 terminal owner를 바꾸지 않은 채 child pgrp에만 `SIGCONT`
   - background child가 terminal read로 다시 `SIGTTIN`/stop되면 2번 상태로 돌아가 wrapper job도 다시 stopped가 됨
4. child exited/signaled:
   - 현재 tty foreground가 child pgrp인 경우에만 parent wrapper pgrp로 복원
   - 다른 pgrp가 foreground라면 terminal을 탈취하지 않음
   - exit code를 기존 규칙으로 반환

wait를 수행하는 주체는 항상 하나뿐이며 cancellation goroutine은 child pgrp에 signal만 보내고 wait/reap에는 참여하지 않는다.

terminal pgrp 변경 중 `SIGTTOU` 처리와 복원은 platform-specific helper에 캡슐화한다.

Linux/macOS build-tag helper로 syscall 차이를 격리하며 Windows native 지원 범위를 새로 만들지 않는다. WSL은 Linux 경로로 검증한다.

## signal/cancel 규칙

- terminal-generated Ctrl+C는 foreground child pgrp가 직접 받는다.
- external `SIGTERM`/`SIGHUP` 또는 context cancellation은 child pgrp에 전달한다.
- 3초 grace 후 child group 강제 종료 규칙 유지.
- terminal foreground 복원은 정상 exit, signal exit, start/wait error, cancellation 모두에서 best-effort cleanup으로 수행한다.

## PTY regression

실제 PTY 테스트를 별도 integration helper로 둔다.

필수 시나리오:

- `read x` 입력 후 결과 출력
- Ctrl+C -> child 종료 + terminal 복원
- Ctrl+Z -> wrapper/child stopped -> `fg` -> 입력/종료 가능
- Ctrl+Z -> `bg` -> wrapper가 shell의 foreground terminal을 탈취하지 않음; terminal read child는 다시 stopped 처리
- child 정상 종료 후 다음 shell 입력이 정상
- non-TTY pipe path regression 없음

---

# Phase 4 — Context-aware read locking (문제 6)

## 범위

현재 `context.Background()`로 maintenance lock을 얻는 모든 production read 경계를 제거한다.

대상:

- `values.Store.Read`
- `tasks.Store.Read`
- `tasks.Store.Profiles`
- `tasks.Store.Query`
- Phase 5에서 maintenance-cooperating reader가 될 `profiles.Catalog`의 Names/List/Inspect/Canonical/Diff 계열 top-level API

그리고 이 API를 호출하는 CLI, execution, diagnostics, profiles, dashboard, backup 경계까지 request context를 전달한다. Catalog는 Phase 4에서 먼저 context-aware signature를 갖추고, 실제 maintenance gate 참여는 Phase 5 storage-key migration과 함께 활성화한다.

## API

내부 API는 다음 형태로 통일한다.

```go
Read(ctx context.Context)
Profiles(ctx context.Context)
Query(ctx context.Context, q Query)
Catalog.Names(ctx context.Context)
Catalog.List(ctx context.Context)
Catalog.Inspect(ctx context.Context, profile string)
Catalog.Canonical(ctx context.Context, profile string)
Catalog.Diff(ctx context.Context, left, right string)
```

lock을 잡지 않는 completion snapshot API는 기존 별도 의미를 유지한다.

## 오류 규칙

- caller context가 취소되어 lock acquire가 끝난 경우 `canceled` / exit 130
- 실제 lock/file/storage 문제는 `storage_error` 또는 기존 package storage code
- cancellation을 storage failure로 변환하지 않는다.

공통 acquire helper가 `ctx.Err()`를 검사해 package별 protocol error mapping을 일관되게 한다. mutation 경로의 maintenance acquire도 같은 원칙으로 정리한다.

## 회귀

- maintenance exclusive lock을 잡은 상태에서 `var list` SIGINT -> lock 해제 전 exit 130
- task query cancellation 동일
- `values.Inspect`를 사용하는 Dashboard GET request context cancel -> `canceled`, lock release를 기다리지 않음
- `values.Apply/Update`, task mutation처럼 ctx를 이미 받는 maintenance mutation도 lock wait 중 cancel을 `storage_error`로 덮지 않고 기존 canceled contract로 반환
- 정상 contention은 lock 해제 뒤 성공
- completion의 passive lockless read 의미 유지

---

# Phase 5A — Large-file recoverable replacement foundation

Phase 5의 profile-key migration과 Phase 7의 task generation switch 전에 먼저 구현한다. 현재 `maintenance.Replace`는 기존 target의 before-image를 `restore-pending.json`에 byte array로 내장하기 위해 파일당 128 MiB로 읽는다. task store 자체에는 동일한 journal size 상한이 없으므로, 유효한 큰 legacy task journal을 migration marker로 교체할 때 이 구현이 migration blocker가 될 수 있다.

## on-disk before-image transaction

`maintenance.Replace`의 public 의미는 유지하되 recovery journal 형식을 versioned on-disk transaction으로 바꾼다.

```text
.maintenance/restore-pending.json
.maintenance/transactions/<transaction-id>/manifest.json
.maintenance/transactions/<transaction-id>/before/0000
.maintenance/transactions/<transaction-id>/before/0001
...
```

규칙:

1. caller는 기존처럼 global exclusive maintenance gate를 보유해야 한다.
2. 모든 target relative path를 typed allowlist로 먼저 검증한 뒤 **component-safe target resolver**로 root부터 parent까지 각 path component를 `Lstat`/no-follow 규칙으로 확인한다. 기존 top-level `profiles|tasks|backup-receipts`와 Phase 5 이후 허용하는 `.identity`처럼 고정된 nested component만 resolver가 생성할 수 있다. 임의 parent, symlink, non-directory, group/other-accessible directory는 거절한다. `Replace`의 before-image read, target publish/remove, recovery restore가 모두 이 resolver를 거쳐야 하며 generic `filepath.Join + maintenance.Write`로 nested target을 우회하지 않는다.
3. 존재하는 target은 resolver가 연 parent 아래 final component를 `O_NOFOLLOW`로 열어 private regular file인지 확인한 뒤 transaction `before/`에 **streaming copy**한다. before-image copy는 전체 파일을 메모리에 올리지 않고 `io.Copy` 계열로 복사하며 file sync + transaction directory sync를 완료한다. target이 없던 경우에는 manifest에 `exists:false`만 기록한다.
4. transaction manifest에는 version, validated UUID transaction ID, target relative path, exists flag, before-image relative path, size/digest를 기록한다. before-image bytes 자체는 JSON에 넣지 않는다. **모든 before-image가 완성된 뒤 manifest를 atomic write하고 manifest file + transaction directory를 sync한다.** manifest가 durable하기 전에는 pending pointer를 게시하지 않는다.
5. 모든 before-image와 manifest가 durable해진 뒤 `.maintenance/restore-pending.json`에 `{version:2, transaction_id:..., phase:"applying"}` pointer를 atomic write+sync한다. pointer의 transaction ID는 UUID shape와 private transaction directory를 다시 검증한 뒤 사용한다. 이 pointer가 durable해진 뒤에만 target mutation을 시작한다.
6. 새 target publish/remove는 기존 `maintenance.Write`/directory sync 규칙을 사용한다.
7. 모든 target 변경이 성공하면 같은 pointer를 `phase:"committed"`로 atomic replace+sync한다. **committed pointer publication이 Replace의 logical commit point**다.
8. committed pointer가 durable해진 시점부터 `Replace`의 logical mutation 결과는 성공이다. 이후 pointer 제거+sync와 transaction directory 삭제는 **post-commit cleanup**이다. cleanup이 즉시 성공하면 scratch를 제거하고 반환한다. cleanup이 실패하거나 crash가 나면 committed pointer/scratch를 그대로 남기되 이미 committed target을 rollback하지 않는다. `Replace`는 commit 이후 cleanup failure를 mutation failure로 바꾸지 않고 성공을 반환한다. 다음 `AcquireShared/Exclusive`가 committed pointer를 발견하면 target을 건드리지 않은 채 cleanup을 다시 시도하며, cleanup이 계속 실패하면 그 다음 operation의 lock acquisition을 storage error로 막는다. 이렇게 해야 caller가 이미 commit된 mutation을 실패로 오인해 비-idempotent 상위 동작을 반복하지 않는다.
9. `phase:"applying"` 상태에서 실패/crash가 나면 recovery는 **target을 건드리기 전에** manifest의 모든 relative path, exists flag, before-image private-file 속성, size/digest를 먼저 검증한다. 하나라도 불일치하면 rollback을 시작하지 않고 storage error로 중단한다. 전체 검증이 통과한 뒤에만 before-image를 source로 각 target을 원래 상태로 복원한다. 복원은 before-image를 source로 streaming copy한 private temp file을 target directory에 만들고 file sync → atomic rename → directory sync 순서로 게시한다. before-image 자체를 이동/수정하지 않으므로 recovery 도중 다시 crash해도 다음 recovery가 같은 source로 처음부터 수렴할 수 있다. 모든 target restore가 끝난 뒤에만 pending pointer와 transaction directory를 제거한다.
10. `phase:"committed"` 상태를 recovery가 발견하면 target을 rollback하지 않는다. transaction은 이미 logically committed된 것이므로 pointer/transaction scratch cleanup만 완료한다. commit pointer write 자체가 durable completion을 확인하지 못해 `phase:"applying"`이 남아 있으면 rollback 대상이고, durable `committed` pointer가 관측되면 commit 대상이다. caller-visible success boundary와 recovery 판정을 같은 durable phase로 맞춘다.

새 `recoverPending`은 현재 legacy inline-array journal과 version 2 transaction pointer의 `applying|committed` phase를 모두 읽어 기존 pending recovery와 backward read compatibility를 유지한다. 반대로 구버전 binary가 version 2 pointer를 만나면 기존 decoder가 실패해 maintenance access가 fail-closed해야 하며 partial state를 정상으로 진행해서는 안 된다. storage migration 뒤 in-place downgrade를 지원하지 않는 rollout 계약과 일치한다.

transaction directory 생성도 private-directory component 검증을 적용하고 symlink를 허용하지 않는다. before-image digest는 recovery 전 source 검증용이며, target content를 로그/오류에 노출하지 않는다.

## orphan transaction cleanup

maintenance transaction directory는 domain archive가 아니라 rollback scratch이며 values/task 원문과 secret before-image를 포함할 수 있다. 30일 retention을 두지 않는다.

`AcquireExclusive`가 global exclusive gate를 얻고 pending recovery를 끝낸 직후 `.maintenance/transactions/`를 검사한다. 현재 `restore-pending.json`이 가리키는 transaction 외의 directory는 **즉시 orphan**으로 간주해 private-directory 검증 후 제거한다. global exclusive gate를 새로 획득한 시점에는 다른 cooperating transaction이 동시에 preparing일 수 없으므로 age heuristic이 필요 없다. 삭제 실패는 secret-bearing scratch를 조용히 방치하지 않고 storage error로 보고해 운영자가 private storage 문제를 확인하게 한다. transaction 성공 경로에서도 pointer 제거 직후 자신의 directory를 즉시 제거한다.

## 완료조건

- 128 MiB를 넘는 private task fixture를 target before-image로 사용하는 `maintenance.Replace`가 memory-size gate 때문에 실패하지 않음
- apply 중 N번째 target 실패 후 다음 `Acquire`가 모든 이전 target을 원래 bytes로 복구
- recovery 도중 다시 중단해도 다음 recovery가 수렴
- legacy `restore-pending.json` fixture recovery 유지
- v2 pointer를 이해하지 못하는 old decoder는 fail-closed
- 기존 symlink/private-permission tests 유지

---

# Phase 5 — Filesystem-safe profile storage key (문제 7)

## 공개 identity와 physical key 분리

공개 profile 문법과 최대 128자는 유지한다.

새 canonical physical key:

```text
p1-<sha256(profile ASCII bytes)의 64 lowercase hex>
```

key는 고정 67자로 유지하고, 원래 profile identity는 각 storage domain의 작은 sidecar에 둔다.

```text
profiles/.identity/<key>.json
tasks/.identity/<key>.json
```

identity sidecar 최소 schema:

```json
{
  "version": 1,
  "key": "p1-...",
  "profile": "ExactProfileName",
  "domain": "values|tasks"
}
```

특성:

- state/lock/receipt/head/generation suffix가 추가돼도 key 길이가 늘지 않는다. `<key>.json.lock`은 77자이고 `values-<key>-<uuid>.json`도 116자 수준이라 255-byte component 제한에 충분한 여유가 있다.
- lowercase hex 하나의 canonical representation만 사용하므로 case-insensitive filesystem에서도 안전하다.
- catalog/backup은 `.identity`의 작은 sidecar로 exact profile 이름을 복원하므로 큰 task state/history를 identity discovery 때문에 읽지 않는다.
- direct lookup은 SHA-256 key를 계산한 뒤 identity sidecar의 exact profile/key/domain을 검증하고, 실제 state document의 `profile` 필드도 다시 exact match한다.
- 극단적인 hash collision이 발생해 같은 key의 sidecar가 다른 profile을 가리키면 alternate key를 만들지 않고 storage collision/corruption으로 fail-closed한다.

## 적용 범위

하나의 `internal/profilekey` helper가 다음을 소유한다.

- canonical SHA-256 key 계산
- per-domain identity sidecar path/schema/validation
- legacy hex filename 계산
- canonical filename recognition
- exact stored-profile validation helper

적용 대상:

- values state filename + lock
- tasks journal filename + lock
- values management receipt filename
- backup/restore profile snapshot path
- profile catalog enumeration
- cleanup completed task source validation
- Dashboard의 direct profile-file 존재 확인
- profile retirement/restore 관련 path contract

process, port instance ID처럼 profile filename이 아닌 영역은 변경하지 않는다.

Phase 5는 Phase 5A의 transaction target allowlist를 좁게 확장한다. 기존 `profiles|tasks|backup-receipts/<file>.json` 외에 정확히 다음 typed path만 허용한다.

```text
profiles/.identity/<validated-p1-key>.json
tasks/.identity/<validated-p1-key>.json
```

임의 3-depth relative path를 허용하지 않는다. `profilekey`가 key shape를 검증하고 maintenance layer가 domain/`.identity` component를 고정 검사한다. `.identity` directory와 sidecar는 모두 private component validation/no-follow 규칙을 적용한다.

## migration

lazy, transaction-safe migration을 사용한다. 단순히 legacy 파일을 삭제하면 구버전 바이너리가 같은 profile 이름으로 새 legacy 파일을 만들어 canonical state와 갈라질 수 있으므로, migration은 fail-closed tombstone을 함께 게시한다.

### migration tombstone

legacy path가 filesystem component 한도 안에 존재/생성 가능한 profile에는 private regular JSON marker를 남긴다.

```json
{
  "storage_marker": "profile-key-v1",
  "profile": "ExactProfileName",
  "canonical_key": "p1-..."
}
```

marker는 기존 values `State`나 tasks `Journal` schema에 없는 필드를 포함한다. 따라서 구버전의 strict decoder는 이를 정상 state로 읽거나 덮어쓰지 않고 storage error로 실패한다. symlink는 사용하지 않는다.

legacy state filename은 `<hex>.json`이고 legacy lock은 `<hex>.json.lock`이다. 일반적인 255-byte component filesystem에서는 state path가 profile 125자까지, lock은 122자까지 표현 가능하다. 다만 marker 여부를 이 숫자에 하드코딩하지 않고 **실제 domain directory가 존재/검증된 상태에서 legacy state path를 `Lstat`한 결과**로 결정한다.

`profilekey.LegacyPathStatus(domain, profile)`은 legacy filename을 계산한 뒤 no-follow parent 검증과 `Lstat`을 수행해 `existing | addressable-missing | unaddressable` 세 상태만 반환한다. `ENAMETOOLONG`만 `unaddressable`로 인정하고, `ENOENT`는 filename이 해당 filesystem에서 addressable하다는 뜻으로 본다. 그 외 오류는 storage error다.

- `existing`: actual legacy state면 migration source로 사용하고, recognized marker면 canonical pair와 교차검증한다.
- `addressable-missing`: 신규 canonical state를 만들 때 구버전이 나중에 legacy state를 만들 수 있으므로 `profile-key-v1` marker를 함께 게시해야 한다.
- `unaddressable`: 해당 filesystem에서 구버전 legacy state path 자체를 만들 수 없으므로 marker 없이 canonical pair만 허용한다.

255-byte Linux/macOS/WSL 환경에서는 결과가 기존 계산과 동일하게 대략 1~125 marker 필요, 126~128 unaddressable로 나온다. 123~125는 old routine lock이 먼저 실패하더라도 old raw backup/restore가 state `.json`을 만들 수 있어 marker가 여전히 필요하다. filesystem component limit이 더 큰 환경에서는 긴 profile에도 marker를 정상 생성해 split-brain을 막는다.

values management legacy receipt `values-<hex>-<request-id>.json`도 hard-coded 103자 분기를 쓰지 않는다. canonical receipt miss 때 legacy receipt path를 no-follow read하고, `ENAMETOOLONG`이면 legacy receipt가 그 filesystem에 존재할 수 없는 것으로 처리하며 `ENOENT`면 단순 miss다. 255-byte 환경에서는 기존 계산상 약 103자까지 addressable하다.

### read

maintenance recovery를 먼저 완료한 뒤 domain resolver가 canonical state, canonical identity sidecar, legacy source/marker를 함께 판정한다.

1. canonical state + exact matching `.identity/<key>.json`이 기본 canonical pair다. sidecar의 key/profile/domain, key의 SHA-256 재계산, state document의 embedded profile이 모두 일치해야 한다.
2. `LegacyPathStatus == addressable-missing`인 profile은 canonical pair와 exact matching legacy migration marker가 함께 있어야 정상 migrated/canonical state다. marker가 사라졌으면 구버전이 legacy state를 다시 만들 수 있으므로 corruption으로 실패한다.
3. `LegacyPathStatus == unaddressable`이면 legacy state path 자체가 해당 filesystem에서 표현 불가능하므로 canonical pair만으로 정상이다.
4. `LegacyPathStatus == existing`이고 actual legacy state만 존재하면 아직 migration되지 않은 source로 읽는다.
5. canonical state만 있거나 identity sidecar만 있는 half-pair는 maintenance recovery 이후에도 남아 있으면 corruption이다. 큰 task payload를 읽어 identity를 추측해 sidecar를 자동 복구하지 않는다.
6. canonical pair + actual legacy state, mismatching/malformed marker/sidecar는 split-brain/corruption으로 실패한다. 내용이 우연히 같아도 자동 병합하지 않는다.
7. migration marker만 있고 canonical pair가 없으면 이번 계획 범위에서는 항상 corruption이다. 향후 별도 profile-retirement 기능이 다른 tombstone state를 도입하더라도 이 migration marker를 정상 state로 재해석하지 않고 별도 lifecycle record로 표현해야 한다.
8. 어느 state도 없으면 해당 domain state 없음으로 처리한다.

### mutation

global maintenance coordination 아래 canonical lock을 사용한다.

legacy source에서 시작한 첫 mutation은 하나의 recoverable replacement로 (a) mutated canonical state write, (b) canonical `.identity/<key>.json` sidecar write, (c) legacy source를 migration marker로 교체한다. 세 target 중 일부만 publish된 반쪽 상태는 Phase 5A maintenance recovery가 원복한다.

**기존 legacy state가 없던 신규 profile/domain의 첫 canonical state 생성도 identity sidecar를 같은 transaction에 포함한다.** domain directory를 먼저 private하게 확정한 뒤 `LegacyPathStatus`를 다시 계산한다. `addressable-missing`이면 canonical state + identity sidecar + legacy marker를 하나의 exclusive recoverable replacement로 함께 게시한다. `unaddressable`이면 구버전 legacy state path 자체를 만들 수 없으므로 canonical state + identity sidecar만 게시한다. shared probe와 exclusive commit 사이에는 status를 재계산해 filesystem/state 변화도 다시 검증한다.

이 first-create/migration transaction이 끝난 이후 routine mutation은 canonical state만 갱신하고 immutable identity sidecar/marker는 유지한다. Phase 7에서 shared global gate를 도입하더라도 **처음 canonical state를 만드는 경로와 legacy migration은 global exclusive gate**를 사용하고, 이미 canonical pair(+필요한 marker)가 정착된 단일-file update만 shared gate 대상이 된다.

logical domain deletion 중 **completed-task cleanup처럼 profile identity 자체는 계속 active인 경우**에는 canonical task state + task-domain identity sidecar + 해당 migration marker를 같은 exclusive transaction에서 함께 제거한다. 이 경우 과거 task state가 의도적으로 active storage에서 사라졌으므로 구버전이 이후 새 task state를 만드는 것은 새 active history로 취급할 수 있고, archive restore는 그 active state와 충돌 검사를 수행한다.

반대로 별도 `profile retirement`는 profile 이름 자체를 retired로 차단하는 계약이므로 이 marker-removal 규칙을 재사용하지 않는다. retirement 구현 시에는 lifecycle tombstone과 downgrade blocker가 함께 구버전의 values/tasks 재생성을 fail-closed하도록 유지하고, 이름 재사용 정책이 별도로 확정되기 전에는 blocker를 제거하지 않는다.

### values management receipt migration

profile state path만 canonicalize하고 기존 `backup-receipts/values-<legacy-hex>-<request-id>.json`을 무시하면 같은 Dashboard request ID가 기존 receipt를 찾지 못해 mutation을 재적용할 수 있다. 따라서 receipt도 logical profile identity에 포함한다.

- 새 receipt 이름은 고정 길이 canonical key를 사용하는 `values-<canonical-key>-<request-id>.json`을 사용한다. 최대 component 길이는 약 116자로 현재보다 충분한 suffix 여유를 갖는다.
- request 처리 시 canonical receipt를 먼저 조회하고, 없으면 해당 request ID의 legacy receipt path를 계산해 fallback 조회한다.
- legacy receipt가 있으면 fingerprint/result를 그대로 검증·replay한다. replay 자체를 위해 state mutation을 반복하지 않는다.
- global exclusive gate를 이미 보유한 mutation/recovery 경로에서는 legacy receipt를 canonical receipt로 atomic copy한 뒤 legacy receipt를 제거할 수 있다. 단순 read/replay는 migration write를 필수로 하지 않는다.
- canonical과 legacy receipt가 동시에 존재하면 fingerprint/result가 byte/semantic equivalent인지 검증한다. 다르면 `request_conflict`가 아니라 storage split-brain으로 실패한다.
- profile retirement/restore처럼 profile-scoped value receipts를 무효화하는 경로는 canonical/legacy 두 naming scheme을 모두 식별해 처리한다.

### catalog

Phase 4의 context 전파를 전제로 `profiles.Catalog`의 Names/List/Canonical/Diff 계열 storage read도 request context를 받도록 정리하고, profile/task domain을 열거하는 동안 maintenance gate를 사용하는 cooperating reader로 만든다. Phase 5 시점에는 기존 exclusive gate를 사용하고, Phase 7의 shared/exclusive 도입 후에는 shared gate로 전환한다. 따라서 migration/restore transaction 중간의 canonical+legacy transient 상태를 catalog가 관측하지 않는다.

lock ownership은 top-level API가 정확히 한 번만 가진다. public `Catalog.Names/List/Inspect/Canonical/Diff`는 request context를 받아 gate를 acquire한 뒤 `namesHeld/listHeld/inspectHeld/canonicalHeld` 같은 내부 helper를 호출하고, 내부 helper끼리는 다시 acquire하지 않는다. 이 held helper가 values/tasks 내용을 읽을 때도 `values.Store.Read`/`tasks.Store.Read`의 public auto-acquire 경로를 호출하지 않고 **caller-held-gate read/resolver**를 사용한다. 따라서 Catalog가 global gate를 잡은 상태에서 domain store가 같은 gate를 다시 획득하는 reentrant deadlock을 만들지 않는다.

이미 maintenance gate를 보유하는 backup/restore/cleanup transaction도 public Catalog를 중첩 호출하지 않고 profilekey/domain resolver의 caller-held-gate variant를 사용한다. nested flock/reentrant acquire를 설계 전제로 삼지 않는다.

- canonical identity enumeration은 `profiles/.identity/*.json`, `tasks/.identity/*.json`의 작은 sidecar만 읽는다. sidecar filename key, body key, exact profile, domain, `sha256(profile)` 재계산이 모두 일치해야 한다. matching root `<key>.json` state가 없는 sidecar 또는 sidecar 없는 canonical `<key>.json`은 maintenance recovery 이후 corruption으로 처리한다. `Names` 때문에 큰 state payload를 읽지 않는다.
- `List/Inspect/Canonical`처럼 실제 domain state를 사용하는 경로는 기존과 같이 store read/validation을 수행해 state document의 stored `profile` exact match와 corruption을 검증한다. **현재 `Catalog.List`가 task storage 검증을 위해 `CompletionIDs()`를 호출하는 결합은 제거한다.** completion API는 lockless best-effort 전용이고 catalog correctness 경계가 아니다. Catalog held helper는 caller-held-gate task current-state loader를 직접 사용해 profile identity/revision/materialized state를 검증한다. 기존 `Catalog.List`가 corrupt task storage를 거절하는 계약도 유지하며, Phase 7 이후 그 비용은 compact snapshot + bounded WAL tail loader로 줄인다.
- legacy hex files는 기존 filename decode도 지원한다. actual state을 실제로 여는 경로에서는 문서의 embedded profile과 filename-decoded profile 일치를 검증한다. recognized migration marker이면 exact profile/canonical key를 검증한 뒤 identity source가 아니라 redirect evidence로만 사용한다.
- canonical + matching migration marker는 하나의 정상 migrated profile로 취급한다. canonical + actual legacy state 또는 malformed/mismatching marker는 split-brain storage error로 처리한다. catalog가 임의로 actual states를 dedupe해서 숨기지 않는다.
- unknown files는 기존 domain 정책에 맞춰 무시하거나 invalid storage로 처리하며, canonical prefix를 가장한 malformed file은 오류로 처리한다.
- `backup.Engine.snapshot`처럼 filename hex decode로 profile을 직접 열거하는 경로는 제거한다. **전체 backup(`selected == ""`)**의 현재 범위는 values/tasks storage가 실제 존재하는 profile의 합집합이므로 전체 `profiles.Catalog`(process/port-only profile까지 포함)를 사용하지 않는다. values와 tasks 각각의 logical store resolver가 legacy/canonical identity를 열거하고 backup이 두 집합만 union한다. 이 모드에서 process/port history만 있는 profile이 새 empty entry로 포함되어서는 안 된다. 반면 **명시적 selected profile**은 현재 코드처럼 요청한 valid profile 이름을 먼저 결과 집합에 넣고 values/tasks가 둘 다 없어도 해당 profile entry 하나를 만들 수 있는 동작을 유지한다.
- backup restore의 raw `file(domain, profile)` helper도 `profilekey` canonical path를 사용하고, legacy source read는 각 domain store resolver가 담당한다.

## task storage와 Phase 7 관계

Phase 5에서는 현재 task Journal JSON 형식을 그대로 canonical key 파일로 옮긴다.
Phase 7의 task storage v3는 같은 fixed `p1-<sha256-hex>` key와 task-domain identity sidecar를 유지하므로 profile identity migration을 다시 하지 않는다.

### 회귀

- 128자 profile var/task operations 성공
- canonical lock/receipt path component 모두 한계 이내
- `LegacyPathStatus`가 addressable이면 신규 canonical state가 legacy downgrade marker와 함께 게시되고, unaddressable이면 marker 없이 canonical pair만 허용됨을 확인. 255-byte test filesystem에서는 125/126 경계를 별도 fixture로 고정
- case-sensitive profile identity 유지
- legacy fixture read -> first mutation canonical migration -> 재조회 성공
- canonical/legacy conflicting duplicate -> 명시적 storage failure
- legacy value-management receipt가 같은 request ID에서 mutation 재적용 없이 replay되고 canonical receipt와 충돌 시 storage failure
- backup/restore/cleanup/profile list가 migration 전후 동일 identity 사용

---

# Phase 6 — Bounded raw log append/compaction (문제 8)

## 원칙

첫 구현 목표는 wall-clock 최적화 수치가 아니라 현재 O(1 MiB per child Write) full-rewrite 알고리즘 제거다. raw logs는 계속 마지막 1 MiB 논리 view만 공개한다.

zero-copy나 async buffering을 한 번에 도입하지 않는다. 우선 durability 의미를 유지하면서 append 기반으로 바꾼다.

## 저장 알고리즘

새 `boundedLogStore`가 writer/read/cleanup 논리를 함께 소유한다.

### write / hysteresis compaction

논리 cap: 1 MiB
physical high watermark: 1280 KiB
compaction target: 마지막 1 MiB

stdout/stderr는 기존 하나의 in-process mutex로 Write call 단위 직렬화한다. 여기에 process directory의 private `output.log.lock` advisory lock을 추가해 writer는 exclusive, `process logs`/cleanup logical reader는 shared lock을 잡는다. 따라서 append 기반으로 바뀌어도 다른 process의 reader가 한 child `Write` 또는 compaction 중간 상태를 관측하지 않는다. writer는 lock을 보유한 뒤 현재 physical size와 incoming length를 확인한다.

- 기존 file이 있으면 append/large-write 분기 전에 no-follow/private regular 검증과 `currentSize <= 1280 KiB`를 확인한다. 정상 writer는 lock scope 밖에 high watermark 초과 file을 남기지 않으므로 이미 초과한 file은 storage corruption으로 보고 자동 truncate하지 않는다.
- file이 아직 없으면 첫 payload의 logical tail을 `maintenance.Write` 계열 atomic create로 게시해 file+directory sync를 보장한다.
- `len(p) >= 1 MiB`이면 valid existing file의 과거 내용은 마지막 1 MiB 결과에 기여하지 않으므로 append하지 않고 `p`의 마지막 1 MiB만 atomic replace한다. 저장량과 무관하게 성공한 `Write(p)`는 **반드시 원래 `len(p)`를 반환**해 child stdout/stderr producer가 short write로 오인하지 않게 한다.
- `currentSize + len(p) <= 1280 KiB`이면 `O_APPEND`로 payload를 추가하고 file sync한다.
- `currentSize + len(p) > 1280 KiB`이면 oversize append를 먼저 하지 않는다. 현재 file의 필요한 tail과 `p`를 합쳐 정확히 마지막 1 MiB snapshot을 만든 뒤 atomic replace한다.
- append fd는 각 Write에서 열고 검증한 뒤 sync/close한다. atomic rename 뒤 오래된 inode를 계속 쓰는 long-lived fd는 두지 않는다.
- create/open 모두 no-follow + private regular file/0600, parent directory 0700 규칙을 검증한다.

이 순서에서는 concurrent reader가 high watermark를 크게 초과한 transient file을 볼 수 없다. 작은 4 KiB writes에서 매번 1 MiB rewrite하지 않고 약 256 KiB growth마다 한 번 compaction하며, 한번 1 MiB까지 쌓인 로그에서 compaction 때문에 보존량이 1 MiB 미만으로 떨어지지 않는다. 각 Write의 sync 정책은 유지하므로 이번 단계에서는 crash durability를 약화하지 않는다.

### read

- physical file은 high watermark 근처까지 클 수 있으므로 기존 `maintenance.Read(..., 1<<20)`를 직접 사용하지 않는다.
- no-follow/private regular 검증 후 bounded physical max까지만 읽고 마지막 1 MiB를 반환한다.
- 공개 `process logs` content cap은 그대로 1 MiB.

### cleanup/archive

`expired_log` scan도 raw physical file을 직접 archive하지 않고 logical last-1-MiB snapshot을 `boundedLogStore`에서 얻는다. cleanup candidate의 `Bytes`와 `Stamp`도 physical file size/raw bytes가 아니라 이 logical snapshot의 길이와 SHA-256 digest를 사용한다. 따라서 background compaction이 같은 logical tail을 다른 physical representation으로 다시 써도 preview가 거짓 `revision_conflict`로 무효화되지 않는다.

cleanup apply에서 log payload snapshot + archive publish + source `output.log` 제거를 수행할 때는 `output.log.lock`을 **exclusive**로 잡고 snapshot stamp를 다시 검증한 뒤 remove까지 같은 lock scope에서 처리한다. `process logs`는 shared lock이므로 cleanup이 한 logical snapshot을 archive하는 동안 concurrent reader가 append/compaction/remove 중간 상태를 보지 않는다. supervisor writer도 같은 exclusive lock을 사용하므로 writer와 cleanup이 동시에 source를 바꾸지 않는다.

restore도 `boundedLogStore.Restore`의 exclusive lock 경로를 사용해 logical payload를 `output.log`에 atomic publish한다. restored file은 <=1 MiB라 다음 append부터 같은 알고리즘을 따른다. lock order는 cleanup의 기존 process-operations/global-maintenance lock 뒤 log lock이며, supervisor/process-logs 경로는 global maintenance lock을 역으로 획득하지 않아 cycle을 만들지 않는다.

## measurement gate

변경 전/후 benchmark를 남긴다.

시나리오:

- 4 KiB x 512 writes, already-full logical log
- 1 KiB high-frequency writes
- single >=1 MiB write
- concurrent stdout/stderr serialized writer
- `--capture-logs` on/off child throughput integration probe

측정 항목:

- elapsed time/op + allocs/op (`go test -benchmem`)
- logical input bytes
- test-only `logIOStats` hook이 기록하는 append bytes, atomic-rewrite payload bytes, fsync count, compaction count. production path에는 hook이 nil이면 분기 비용만 남고 통계 저장은 하지 않는다.

acceptance:

- already-full log에 4 KiB x 512를 쓴 structural benchmark에서 atomic-rewrite payload bytes가 **입력 write마다 1 MiB씩 증가하지 않고**, compaction 횟수가 watermark 기준 예상 상한(약 8회; boundary에 따른 ±1 허용) 안에 있을 것
- `process logs`가 항상 정확한 마지막 `min(total_input, 1 MiB)` bytes를 반환하고 restore 뒤에도 동일할 것
- capture off 대비 capture on benchmark의 절대 성능 목표값은 환경 의존이라 merge gate로 고정하지 않는다. 대신 기존 구현 baseline과 새 구현을 같은 benchmark process에서 비교한 before/after 결과를 작업 기록에 남기고, structural write amplification이 개선되지 않거나 wall time이 명확히 역행하면 원인을 해결한 뒤 진행한다.

async flush/batched fsync는 별도 후속 최적화로 둔다.

---

# Phase 7 — Task journal scalability and lock granularity (문제 9)

Phase 5 완료 후 진행한다.

## 목표

- current query가 전체 event history replay에 비례하지 않게 함
- mutation이 전체 journal JSON rewrite에 비례하지 않게 함
- 서로 다른 profile의 routine reads/writes가 global exclusive maintenance lock 때문에 직렬화되지 않게 함
- full event history와 request-idempotency는 유지
- crash recovery/backup/restore consistency는 약화하지 않음

## 7.1 Global maintenance lock을 shared/exclusive gate로 확장

현재 `.maintenance/lock`의 exclusive flock은 multi-file recovery 보장을 위해 유지해야 한다.

새 global gate API:

```go
AcquireShared(ctx, root)
AcquireExclusive(ctx, root) // 현재 Acquire 의미
```

profile-local lock도 mode를 명시한다. 기존 `tasks.Lock`/values flock의 ad-hoc 사용을 그대로 섞지 않고 공통 private flock helper를 둔다.

```go
LockShared(ctx, path)
LockExclusive(ctx, path)
```

둘 다 같은 no-follow/private-file 검증과 context cancellation 규칙을 사용한다. task read/query는 shared profile lock, task mutation/migration/recovery는 exclusive profile lock을 사용한다. values는 기존 domain lock 파일을 exclusive로만 사용하되, read가 별도 profile lock을 필요로 하지 않는 현재 atomic-file snapshot 의미는 유지한다.

shared/exclusive 구현은 단일 `flock`의 fairness에 기대지 않고 **writer-intent turnstile**을 사용한다. 각 RW lock은 gate file과 sibling turnstile file을 가진다.

- shared: turnstile shared 획득 -> gate shared 획득 -> turnstile 해제 -> 작업 -> gate 해제
- exclusive: turnstile exclusive 획득 -> gate exclusive 획득 -> turnstile 해제 -> 작업 -> gate 해제

exclusive waiter가 turnstile을 **획득한 이후에는** gate의 기존 readers가 빠지기를 기다리는 동안 새 reader가 우회하지 못한다. 따라서 reader wave가 이미 대기 중인 writer를 계속 추월하는 현상을 줄인다. 다만 `flock` 자체의 waiter scheduling fairness를 correctness 전제로 삼거나 starvation-free를 절대 보장한다고 주장하지 않는다. global gate는 `.maintenance/lock` + `.maintenance/lock.turnstile`, profile lock은 `<profile-lock>` + `<profile-lock>.turnstile` 조합을 사용한다. 두 파일 모두 private/no-follow 검증 대상이다. 구버전 binary는 turnstile을 모르지만 기존 gate file의 exclusive lock은 계속 존중하므로 안전성은 유지되며, cross-version fairness는 보장하지 않는다.

### shared acquire

routine profile-local read와, global `maintenance.Replace`를 사용하지 않는 단일-profile mutation이 사용한다.

- shared lock을 획득한 뒤 `restore-pending.json` 존재와 `.maintenance/transactions/`의 orphan scratch 존재를 확인한다. shared gate를 이미 획득했다는 것은 cooperating exclusive transaction이 현재 준비/적용 중일 수 없다는 뜻이므로 이 시점의 non-empty transaction directory는 crash residue다.
- pending recovery 또는 orphan transaction scratch가 있으면 shared lock을 놓고 exclusive acquire -> recovery/orphan cleanup -> release -> shared retry한다. 따라서 pointer publish 전 crash로 생긴 secret-bearing before-image가 이후 read-only workload만 이어져도 무기한 남지 않는다.
- shared lock 보유 중에는 새 exclusive restore/replace가 시작될 수 없음
- `values.Store.Update`처럼 자체 profile lock + 단일 atomic file publish로 끝나는 mutation은 shared global gate로 전환할 수 있다.
- `values.Store.Apply`처럼 profile state와 retry receipt를 현재 global `maintenance.Replace`로 함께 교체하는 경로는 이번 단계에서 **exclusive global gate를 유지**한다. 이를 shared로 바꾸려면 별도의 profile-local recoverable transaction이 먼저 필요하며 문제 9 해결의 필수 범위로 확장하지 않는다.

### exclusive acquire

- backup create/export snapshot: 현재처럼 values/tasks 여러 profile/domain을 한 global snapshot window에서 읽는 일관성을 유지한다. read-only라는 이유로 shared gate로 낮추지 않는다.
- backup restore/import preview+apply
- maintenance.Replace 기반 cross-file operation
- cleanup/profile lifecycle처럼 여러 domain을 바꾸는 operation

routine profile mutation 중 global `maintenance.Replace`를 사용하지 않는 경로는:

1. global maintenance shared gate
2. profile-specific exclusive lock

`values.Store.Apply`, backup create/restore/import, cleanup/profile lifecycle처럼 global cross-domain consistency 또는 recovery journal을 요구하는 경로는 global exclusive gate를 유지한다.

routine profile read/query는:

1. global maintenance shared gate
2. task storage의 경우 profile-specific shared lock

lock order는 항상 global gate -> profile lock으로 고정한다.

### shared→exclusive 재시작 규칙

routine mutation entry는 먼저 shared global gate + 필요한 profile exclusive lock으로 현재 physical format을 resolve할 수 있다. 이때 **legacy source migration, canonical first-create, v2→v3 conversion, missing identity publication처럼 global `maintenance.Replace`가 필요한 조건을 발견하면 그 자리에서 lock upgrade를 시도하지 않는다.**

1. profile lock을 먼저 해제한다.
2. global shared gate를 해제한다.
3. global exclusive gate를 새로 획득한다.
4. 같은 profile exclusive lock을 획득한다.
5. storage state와 request receipt/fingerprint를 처음부터 다시 resolve한다. shared probe 결과를 commit 근거로 재사용하지 않는다.
6. 조건이 여전히 migration/first-create이면 exclusive transaction을 수행하고, 다른 concurrent writer가 이미 migration을 끝냈다면 현재 canonical/v3 state에 대해 request replay/new-mutation 판정을 다시 수행한다.

이 restart path는 context cancellation을 그대로 전달하며 shared lock을 보유한 채 exclusive를 기다리는 in-place upgrade를 금지한다. values first canonical creation과 task first v3/migration 모두 같은 규칙을 사용한다.

cross-domain orchestrator는 global gate를 중첩 획득하지 않는다. backup/restore/cleanup/profile-lifecycle처럼 이미 global exclusive gate를 가진 호출자는 tasks/values/catalog의 public auto-acquire API 대신 caller-held-gate variant를 사용한다. caller-held variant는 필요한 profile lock만 추가로 잡고 global gate를 다시 acquire하지 않는다. routine 단일-domain caller만 public API에서 shared gate를 획득한다. 이 ownership 규칙을 API 이름/주석과 lock-order 테스트로 고정하고 reentrant `flock` 동작에 기대지 않는다.

## 7.2 Task storage v3: WAL + materialized snapshot

여기서 `v3`는 **physical task storage format version**이다. 현재 task event semantics의 `JournalVersion == 2`를 3으로 올리는 변경이 아니다. backup/export의 logical `Journal.Version`과 event/replay 의미는 기존 1/2 compatibility를 유지하고, storage resolver만 v2 single-file과 v3 WAL/snapshot 형식을 구분한다.

현재 단일 `Journal{Events, Receipts, Contexts}` JSON을 다음으로 분리한다.

canonical key가 `p1-<sha256-hex>`라면 logical entry는 작은 head pointer와 generation storage로 나눈다. task-domain `.identity/<key>.json`은 generation switch와 무관하게 같은 logical profile identity를 유지한다.

```text
tasks/<key>.json                       # v3 format marker / downgrade blocker
tasks/<key>.head.json                  # active generation pointer
tasks/<key>.lock
tasks/<key>.generations/<generation-id>/snapshot.json
tasks/<key>.generations/<generation-id>/wal
tasks/<key>.generations/<generation-id>/receipts/<2-hex>/<request-id>.json
tasks/<key>.generations/<generation-id>/contexts/<2-hex>/<context-hash>.json
```

`<key>.json`은 v2 Journal 자리에 남는 strict-decoder blocker다.

```json
{
  "storage_marker": "task-v3",
  "profile": "ExactProfileName",
  "head": "p1-....head.json"
}
```

v2-aware 구버전은 이를 Journal로 decode하지 못해 fail-closed한다. v3 resolver는 marker와 head가 서로 같은 exact profile/key를 가리키는지 검증하며 marker/head 중 하나만 남은 상태를 maintenance recovery 이후 corruption으로 취급한다.

head에는 format/version, exact profile, active generation ID만 둔다. generation은 head가 가리킨 뒤에는 동일 generation 안에서 WAL append/snapshot refresh만 수행한다.

generation을 둔 이유는 backup restore/profile replacement가 values와 task logical state를 한 transaction으로 전환할 수 있게 하기 위해서다. 새 task 상태는 별도 generation에 완전히 stage+fsync한 뒤, `maintenance.Replace` 계열 transaction에서는 task-domain identity sidecar + task v3 marker + `tasks/<key>.head.json` pointer를 하나의 logical publication set으로 다룬다. cross-domain restore라면 values canonical state/identity publication도 같은 transaction에 포함한다. transaction 실패 시 old logical state가 유지되고 staged generation은 unreachable orphan이므로 active state가 반쪽으로 바뀌지 않는다.

### WAL event/commit log

한 mutation request를 하나의 transaction frame으로 저장한다.

frame 내용:

- format/version + frame kind(`mutation|historical|metadata`)
- previous/final revision
- request ID + fingerprint (live mutation은 필수, legacy historical/metadata frame은 원본에 없으면 nullable)
- 해당 request/group의 events 전체
- receipt coordination payload digest (receipt가 존재할 때만)
- 새/보존 context mapping이 있으면 context hash + coordination payload digest 목록
- checksum

새로 생성하는 live mutation/zero-event frame은 항상 valid request ID, fingerprint, receipt coordination reference를 가져야 한다. nullable field는 legacy migration에서 원본 정보 자체가 없던 historical representation에만 허용하고 새 mutation이 이 형태를 만들지 못하게 validator로 분리한다.

receipt result와 context mapping body 자체는 WAL에 중복 저장하지 않는다. 각 coordination payload digest는 sidecar의 mutable `state`와 `frame_digest`를 제외한 canonical immutable payload(request/fingerprint/result 또는 context-hash/run-id)의 SHA-256이다. frame은 이 payload digest만 참조하므로 sidecar가 나중에 frame digest를 기록해도 digest cycle이 생기지 않는다.

여러 events를 만드는 하나의 request가 frame 하나이므로 현재 atomic request visibility를 유지한다. 다만 현재 `run.claimed`의 `claimed=false`처럼 **events가 0개여도 receipt 자체를 commit하는 mutation**이 존재하므로, WAL frame은 zero-event mutation을 정식으로 허용한다. 이 경우 `previous_revision == final_revision`이고 receipt/context coordination reference만 저장한다. retry/idempotency result의 authoritative body는 receipt sidecar에 있고 WAL frame은 그 payload digest를 atomic commit과 결합한다.

v2 migration 중 legacy receipt/context map에 event frame만으로 표현되지 않는 항목이 있으면 bounded metadata-only frame으로 기록한다. migration용 metadata frame도 revision을 증가시키지 않는다. receipt/context sidecar는 단순 disposable cache가 아니라 retry/context lookup의 correctness-bearing coordination record이며, WAL metadata는 migration/export/recovery 시 이를 재구성할 수 있는 durable history다. migration 시 원본 map을 sidecar에만 남기지 않는다.

WAL은 length + checksum framing으로 append한다. **이미 active head가 가리키는 generation의 routine mutation에서는** frame append + fsync가 commit point다. 아직 head가 가리키지 않는 staged generation(신규 task profile의 첫 mutation, v2→v3 migration, backup/cleanup restore)은 WAL fsync만으로 공개 state가 아니며, generation 전체가 durable해진 뒤 marker/head pointer를 atomic하게 게시하는 순간이 logical commit point다.

현재 legacy task journal 파일 전체에는 명시적 size cap이 없으므로 v3 reader가 transaction frame 전체를 하나의 고정-size buffer로 allocation하는 설계는 사용하지 않는다. frame header에는 total byte length와 checksum을 두되, decode는 event와 작은 coordination-reference subrecord를 streaming한다. total length는 remaining file bytes를 넘지 않는지 확인하는 경계로만 사용하고, allocation은 각 event/ref subrecord의 기존 입력/schema 상한에 맞춰 제한한다. 큰 receipt result는 WAL frame이 아니라 별도 private sidecar에 있으므로 frame 크기를 불필요하게 키우지 않는다. **새 event-count ceiling은 추가하지 않는다.** reader는 frame boundary까지 subrecord를 순차 처리하고, live mutation은 기존 command validation이 허용한 prepared events를 그대로 frame 하나에 기록한다. migration도 legacy request grouping을 그대로 보존하며 subrecord 단위로 stream한다. 따라서 큰 legacy journal이나 큰 historical request를 단순히 새 total-frame/count cap 때문에 거절하지 않는다. WAL과 generation 파일은 모두 no-follow/private regular file 규칙을 적용한다.

v3의 nested generation/shard directory는 기존 `MkdirAll + final Lstat`만으로 만들지 않는다. tasks package에 validated root부터 각 path component를 순서대로 검사·생성하는 private-directory-tree helper를 두고, 중간 component의 symlink/non-directory/group-or-other writable permission을 모두 거절한다. 새 directory를 생성할 때는 mode 0700으로 만들고 **생성된 directory와 그 parent를 sync한 뒤** 다음 component로 진행한다. generation ID, shard, request ID/context hash는 caller path 문자열을 그대로 받지 않고 내부 검증된 fixed-format component만 사용한다. staged generation은 WAL/snapshot/coordination files를 sync한 뒤 generation tree의 생성된 directory를 bottom-up으로 sync하고, 그 durability가 확인된 뒤에만 head publication을 시작한다. active generation에서 처음 생기는 receipt/context shard도 같은 helper를 사용해 parent-chain durability를 먼저 보장한다. 새 nested layout 때문에 기존 symlink 방어가 약화되어서는 안 된다.

WAL scan은 frame length와 checksum을 구분해 판정한다.

- EOF 때문에 마지막 frame bytes가 완성되지 않은 경우: valid prefix까지만 committed로 취급한다. shared read/query는 incomplete tail을 수정하지 않고 무시한다.
- 다음 mutation/retry는 exclusive profile lock 아래 append 전에 incomplete tail을 valid offset까지 truncate+sync한다.
- frame 전체 bytes가 존재하는데 checksum이 맞지 않거나 중간 frame이 깨진 경우: storage corruption으로 처리하고 자동 truncate/skip하지 않는다.

### materialized snapshot과 durable coordination records

현재 `State`는 `Events`를 포함하므로 그대로 snapshot에 직렬화하지 않는다. v3에는 event history를 제외한 별도 `MaterializedState` projection을 둔다.

snapshot에는:

- format/version
- applied WAL offset/final revision
- 현재 Items/Runs/Tracking, logical Version 등 query/mutation에 필요한 materialized state
- cleanup retention 판정용 `last_event_at` (zero-event receipt는 이 값을 변경하지 않음)
- checksum/identity metadata

snapshot에는 전체 `Events`, 전체 receipt map, 전체 context map을 넣지 않는다. 그렇지 않으면 snapshot rewrite가 다시 history 크기에 비례한다.

retry/context membership은 active generation 아래 sharded **committed** sidecar로 분리하고, in-flight mutation coordination은 history shard와 분리한 generation-local pending transaction으로 둔다.

```text
tasks/<key>.generations/<generation-id>/receipts/<2-hex>/<request-id>.json
tasks/<key>.generations/<generation-id>/contexts/<2-hex>/<context-hash>.json

tasks/<key>.generations/<generation-id>/pending.json
tasks/<key>.generations/<generation-id>/pending/<request-id>/receipt.json
tasks/<key>.generations/<generation-id>/pending/<request-id>/contexts/<context-hash>.json
```

committed receipt/context는 request UUID/context hash의 앞 1 byte(2 hex)로 shard한다. `pending/` tree에는 **현재 profile-exclusive mutation 하나의 in-flight payload만** 존재할 수 있고 committed history 파일을 섞지 않는다. 따라서 recovery가 pending 존재를 확인하기 위해 receipt/context shard 전체를 `ReadDir`하지 않으며 history 크기에 비례하지 않는다.

receipt coordination payload는 최소한 request ID, fingerprint, `context_hash`(해당 action이 execution context를 사용하지 않으면 empty), stored result, immutable `payload_digest`, `frame_digest`를 가진다. context payload는 context hash, run ID, immutable `payload_digest`, `frame_digest`를 가진다. raw context credential은 filename/schema key로 저장하지 않고 기존 receipt result가 필요할 때만 private receipt payload 안에 존재한다.

`pending.json`은 version, request ID, frame digest, pending payload relative paths, 최종 committed shard paths, 각 immutable payload digest를 가진 작은 pointer/manifest다. pending payload files와 pending directory를 모두 sync한 뒤에만 `pending.json`을 atomic write+sync한다. WAL append는 durable pending pointer가 생긴 뒤에만 시작한다.

committed sidecar는 단순 cache가 아니라 **routine membership lookup의 authoritative coordination record**다. WAL은 sidecar body를 복제하지 않고 immutable payload digest를 frame에 bind한다. migration/backup/export은 WAL references와 committed payload를 교차검증하며, sidecar body가 유실된 경우 WAL만으로 result/context를 추측 복원하지 않고 storage data loss로 처리한다. generation switch가 일어나면 이전 generation의 committed/pending tree는 active lookup에서 즉시 제외된다.

현재 `State.prepare`/`resolveContext`가 `map[string]string` 전체를 받는 API도 storage v3 경계에 맞춰 분리한다. 실제 command logic은 context token 하나를 `hash(token) -> runID`로 조회할 뿐이므로 `ContextLookup` 함수/interface를 받도록 바꾸고, v2 adapter는 기존 map lookup, v3 adapter는 해당 shard의 committed context sidecar **한 파일만** 읽는다. 새 credential 생성은 기존처럼 mapping delta를 반환해 commit 단계가 pending sidecar를 만든다. routine mutation이 전체 context sidecar directory를 enumerate하거나 map으로 재구성해서는 안 된다.

active generation의 mutation commit 순서:

1. shared global + exclusive profile lock
2. compact snapshot을 검증한다. valid하면 snapshot offset 이후 WAL tail만 load한다. snapshot이 missing/corrupt하지만 WAL이 valid하면 full WAL replay로 materialized state를 **메모리에서만** 재구성하고 `snapshotNeedsRepair=true`로 표시한다. coordination recovery 전에는 새 snapshot을 publish하지 않는다. WAL 자체 corruption은 복구 대상으로 가장하지 않고 storage error로 중단한다.
3. incomplete WAL tail이 있으면 valid offset까지 truncate+sync한다.
4. **pending coordination recovery**: generation root의 `pending.json` 하나만 확인한다. pointer가 없으면 committed receipt/context shard를 enumerate하지 않는다. pointer가 있으면 schema/path/private-file/digest를 먼저 검증한다. valid snapshot이 있으면 snapshot offset 이후 tail에서 matching frame digest를 찾고, snapshot이 없으면 recovery failure path로 full WAL을 streaming scan한다.
   - matching complete frame이 있고 coordination references/digests가 모두 일치하면 pending receipt/context payload를 각 final shard path로 atomic rename + shard directory sync해 finalize한다. 일부 final file이 이미 존재하면 exact payload/frame digest 일치 여부를 확인해 나머지만 완료한다.
   - matching frame이 없으면 crash-before-WAL-commit으로 보고 pending payload/tree + pointer를 제거하고 directory sync한다.
   - frame은 있는데 digest/reference가 맞지 않거나 final payload가 충돌하면 corruption으로 중단한다.
   finalize/rollback이 끝난 뒤 `pending.json`을 제거+generation directory sync한다. pointer가 없는데 `pending/` 아래 orphan directory가 있으면 WAL commit 전 pointer publication 이전 crash residue이므로 profile exclusive lock 아래 즉시 제거한다. 이 recovery는 pending tree만 열거하고 committed history shard를 스캔하지 않는다.
5. `snapshotNeedsRepair`이면 4번 완료 뒤 현재 valid WAL prefix와 committed coordination 범위에 맞는 compact snapshot을 atomic publish한다.
6. 현재 request가 execution context를 실제 사용하는지 materialized state 기준으로 판정하고, 그 규칙에 맞춰 fingerprint input/context hash를 canonicalize한다. 현재 request ID의 committed receipt `.json`이 존재하면 fingerprint를 비교하고 context-used request에서는 stored `context_hash`도 현재 token hash와 비교한다. 하나라도 다르면 기존 계약대로 `request_conflict`다. 일치하면 stored result를 복사한 뒤 `replayed=true`, `current_revision`, `context_valid`/`context` 같은 **현재 state에 따라 달라지는 replay field는 기존 Store.Execute 규칙대로 다시 계산**해 반환한다. 4번 recovery 뒤 committed receipt가 없으면 새 request로 확정하고 full WAL scan을 하지 않는다. cooperating commit은 WAL보다 durable pending pointer/payload를 먼저 게시하므로 crash 때문에 과거 committed request가 coordination record 없이 생길 수 없다.
7. request validation/prepare. validation/no-change처럼 현재 계약상 receipt를 commit하지 않는 결과면 coordination/WAL을 만들지 않고 반환한다.
8. 최종 receipt result와 새 context mapping으로 immutable coordination payload를 만들고 각각 `payload_digest`를 계산한다. WAL frame에는 events + 이 payload digest/reference만 넣어 frame bytes/checksum을 만든 뒤 `frame_digest`를 계산한다. 이 순서로 frame digest와 coordination payload digest 사이의 순환 의존성을 피한다.
9. `pending/<request-id>/` tree에 receipt/context payload를 final body 형태로 생성하고 file + created-directory chain을 sync한다. 그 다음 small `pending.json` pointer/manifest를 atomic write+sync한다. 이 단계가 실패하면 WAL은 건드리지 않고, 다음 recovery가 pointer 유무에 따라 pending tree를 제거한다.
10. WAL frame append + fsync — **active generation에서는 이 시점이 mutation commit point**다.
11. pending payload의 frame digest와 immutable payload digest가 committed WAL frame의 reference와 모두 일치하는지 대조한 뒤 각 payload를 committed receipt/context shard의 final `.json` path로 atomic rename + directory sync한다. 모두 finalize된 뒤 `pending.json`과 pending request directory를 제거+sync한다. payload body는 다시 쓰지 않는다.
12. snapshot 이후 committed tail이 checkpoint threshold를 넘은 경우에만 compact materialized snapshot을 atomic replace한다. checkpoint offset은 11번까지 finalize가 완료된 frame까지만 포함한다.

checkpoint threshold는 frame count와 tail byte size 두 상한으로 고정한다. 초기 구현 상수는 `CheckpointMaxFrames = 256`, `CheckpointMaxTailBytes = 4 MiB`로 둔다. 둘 중 하나를 넘은 mutation이 compact snapshot을 갱신한다. 제품 설정으로 노출하지 않는다. benchmark가 이 값에서 명확한 회귀를 증명하면 같은 work item 안에서 근거와 함께 조정할 수 있지만, 구현자는 별도 제품 결정을 기다리지 않는다. acceptance는 "routine mutation마다 snapshot 전체를 rewrite하지 않는다", "새 request receipt miss가 full-history scan을 일으키지 않는다", "current-state query가 replay하는 committed tail이 두 threshold 범위로 다시 수렴한다"이다.

10번 이후 11~12번에서 실패하면 caller에는 storage failure를 반환할 수 있지만 이미 fsync된 WAL commit은 취소하지 않는다. 같은 request ID 재시도는 generation-root pending pointer recovery에서 WAL frame digest를 찾아 payload를 final shard로 finalize한 뒤 stored result를 replay한다. 이미 commit된 frame을 다시 append하지 않는다. WAL append 전에 crash하면 pending transaction tree만 제거되고 mutation은 미적용으로 재시도된다.

committed receipt/context sidecar의 임의 삭제는 crash-recovery 경로가 아니라 private storage 일부의 out-of-band data loss로 취급한다. routine path는 이를 탐지하기 위해 매 request마다 full WAL scan하지 않는다. backup/export처럼 전체 logical snapshot을 이미 순회하는 경로는 WAL metadata와 sidecar를 교차검증할 수 있지만, 정상 mutation의 O(history) fallback으로 사용하지 않는다.

query:

1. shared global + shared profile lock
2. compact snapshot load
3. snapshot이 valid하면 snapshot offset 이후 bounded WAL tail만 replay
4. snapshot이 missing/corrupt하지만 WAL이 valid하면 read-only full WAL replay로 현재 materialized state를 구성한다. shared read는 snapshot을 수선하지 않는다.
5. WAL corruption이면 storage error
6. current materialized state query

routine read는 stale/missing snapshot을 메모리 replay로 보완할 수 있지만 committed coordination/snapshot을 고치기 위해 lock upgrade하지 않는다. 다음 mutation/retry/recovery path가 exclusive profile lock 아래 generation-root pending transaction과 snapshot을 수선한다.

logical Version 1이 v3에 존재할 수 있는 경우는 brand-new zero-event receipt 상태 또는 exact restore된 legacy history다. Version 1에서 첫 **eventful** mutation이 발생할 때는 `profile.upgraded` baseline을 현재 코드와 동일하게 계산해야 하므로, 과거 Events가 존재하면 그 한 번에 한해 WAL history iterator를 streaming하여 upgrade baseline을 만든다. 생성된 `profile.upgraded + mutation events`는 하나의 request frame으로 commit되고 이후 snapshot Version은 2가 되므로 routine mutation은 다시 compact state + bounded tail 경로로 돌아간다.

## 7.3 History 보존

WAL은 event history의 durable source이므로 정상 compaction에서 과거 committed frames를 삭제하지 않는다.

향후 event-history retention을 원한다면 별도 제품 계약이 필요하다.

`AtRevision`, history/export/backup은 snapshot current state만으로 대체하지 않고 WAL event stream을 사용한다. WAL reader는 frame 내부 event sequence가 contiguous하고 `previous_revision + event_count == final_revision`인지 검증한다. `AtRevision(n)`은 frame의 `final_revision == n`인 boundary 또는 과거 single-event boundary만 허용하고, 하나의 multi-event request frame 중간 revision을 지정하면 기존과 동일하게 `revision_not_committed`를 반환한다. metadata/zero-event frame은 revision boundary를 새로 만들지 않는다.

## 7.4 v2 migration + triggering mutation atomicity

legacy/canonical v2 single Journal JSON은 **처음 실제로 commit할 v3 mutation**과 함께 변환한다. 단순 read, invalid/no-change request, 기존 v2 receipt replay만으로 storage format을 바꾸지 않는다.

절차:

1. one-time migration은 global maintenance exclusive gate를 먼저 잡고 그 안에서 profile exclusive lock을 잡는다. shared→exclusive lock upgrade는 사용하지 않는다.
2. v2 journal 전체 validation + replay. 먼저 기존 receipt에서 현재 request ID를 확인해 이미 committed된 요청이면 v2 결과를 그대로 replay하고 migration하지 않는다.
3. 현재 request를 v2 state에 대해 validation/prepare한다. 일반 validation/no-change failure처럼 현재 구현이 receipt를 commit하지 않는 결과면 그대로 반환하고 migration하지 않는다. event mutation 또는 `run.claimed claimed=false`처럼 zero-event receipt를 실제 commit해야 하는 경우에만 다음 단계로 간다.
4. legacy receipt를 full v2 replay state와 event history가 아직 메모리에 있는 이 시점에 먼저 canonicalize한다. 기존 `normalizeReceipt`와 동등한 규칙으로 누락된 `previous_revision`, `affected_ids`, `affected_count`를 보완하고, fingerprint/context hash/result가 현재 journal과 일치하는지 검증한다. canonicalize할 수 없는 receipt는 migration을 중단한다. 각 canonical receipt/context coordination payload와 immutable payload digest도 이 단계에서 계산한다. v3 routine replay는 과거 receipt 정규화를 위해 `State.Events`를 요구하지 않는다.
5. 새 generation ID를 만들고 기존 event history를 sequence 순으로 검증하면서 historical WAL frames로 변환한다. 연속된 동일 non-empty `request_id`는 하나의 atomic frame으로 묶고, legacy event에 request ID가 없으면 한 event를 하나의 historical frame으로 기록한다. non-empty request ID frame에는 같은 request ID의 canonical receipt가 있으면 **receipt body가 아니라 coordination payload digest/reference**를 attach한다. context map entry는 receipt result의 context credential hash/run ID와 일치해 요청에 안전하게 귀속할 수 있을 때 해당 context payload digest/reference를 attach한다. event frame에 귀속되지 않은 receipt(대표적으로 zero-event committed request)와 귀속할 request frame이 없는 context map entry는 revision을 증가시키지 않는 metadata-only frame으로 기록한다. 하나의 logical receipt/context entry를 중복 frame에 쓰지 않는다. atomic request를 total-byte 기준으로 임의 분할하지 않고 event/reference subrecord를 streaming encode하며 legacy file 전체 크기 자체를 새 migration 거절 기준으로 추가하지 않는다.
6. **triggering request의 새 frame을 같은 staged generation에 마지막으로 기록한다.** triggering receipt/context coordination payload와 digest를 먼저 계산하고 frame에는 body가 아니라 그 reference를 넣는다. v1 logical state에서 **events가 실제 생성되는 첫 mutation**이면 현재 의미와 동일하게 `profile.upgraded` + mutation events를 한 request frame에 포함하고 logical version을 2로 올린다. `run.claimed claimed=false` 같은 zero-event committed mutation이면 upgrade event를 만들지 않고 version/revision을 그대로 유지한 채 receipt/context coordination reference만 frame에 기록한다.
7. historical + triggering frame 전체 WAL을 fsync하고, 그 전체 metadata를 기준으로 receipt/context coordination sidecar를 **committed 상태로** materialize한다. staged generation은 아직 head가 가리키지 않으므로 pending 단계를 거칠 필요가 없다. 이어서 **triggering mutation 적용 후** compact materialized snapshot을 기록+fsync한다.
8. 새 generation 전체가 durable한 것을 확인한 뒤 같은 global exclusive gate 안에서 recoverable replacement로 task-domain identity sidecar를 검증/게시하고, v2 source `tasks/<key>.json`을 `task-v3` marker로 교체하며 `tasks/<key>.head.json`을 새 generation으로 게시한다. Phase 5 canonical v2 source라면 기존 identity sidecar를 exact match로 재사용하고, source가 아직 legacy hex path라면 새 identity sidecar 생성과 legacy file의 `profile-key-v1` migration marker 교체를 같은 transaction에 포함한다.
9. 이 head publication이 storage migration과 triggering mutation의 단일 logical commit point다. publication 전 crash/failure는 v2 source를 active로 유지해 mutation 미적용으로 재시도할 수 있고, publication 후 응답 유실은 새 generation 안의 triggering receipt로 replay한다.
10. transaction 실패 시 v2 actual source/head 전환은 rollback되고 staged generation은 orphan으로 남아 다음 exclusive maintenance에서 제거된다.

migration은 profile당 한 번뿐이므로 이 구간의 global exclusivity를 허용한다. v3로 전환된 뒤의 routine operations만 shared gate를 사용한다.

### 신규 task state의 최초 v3 publication

기존 v2 task state가 전혀 없는 profile에서 첫 committed mutation이 발생하면 빈 canonical file을 거쳐 v2를 만들지 않는다. global maintenance exclusive gate -> profile exclusive lock 순서로 새 generation을 stage하고, 그 generation에 첫 mutation frame + committed receipt/context coordination sidecar + compact snapshot을 모두 durable하게 만든 뒤 task-domain identity sidecar + `task-v3` marker + head pointer + Phase 5의 legacy `profile-key-v1` blocker(legacy state path가 표현 가능한 경우)를 하나의 recoverable replacement로 게시한다. 첫 요청이 eventful이고 logical state가 version 1이면 frame에 `profile.upgraded + mutation events`를 함께 기록하고 snapshot은 version 2가 된다. 첫 요청이 zero-event committed mutation이면 upgrade event 없이 version 1 snapshot + zero-event frame을 게시하며, 이후 첫 eventful mutation에서 upgrade를 수행한다. staged generation은 아직 active lookup 대상이 아니므로 coordination sidecar는 pending 단계를 거치지 않고 committed로 기록한다. head publication 전 crash는 unreachable orphan만 남기고 mutation은 미적용으로 취급한다. head publication 후에는 same request receipt가 이미 generation 안에 durable하므로 retry가 replay로 수렴한다.

읽기-only upgrade 직후에는 v2를 계속 읽을 수 있고, first mutation에서 migrate하여 단순 `profile list`나 completion이 migration write를 일으키지 않게 한다.

## 7.5 Backup/restore/cleanup integration

현재 backup restore는 values/tasks raw JSON 파일을 `maintenance.Replace`로 함께 교체한다. v3에서는 raw task path를 외부 package가 직접 다루지 않게 한다.

tasks package가 logical export/import 경계를 제공한다. 각 경계는 routine caller용 auto-acquire API와 global gate를 이미 보유한 orchestrator용 caller-held-gate variant를 짝으로 제공한다. backup/restore/cleanup은 후자만 사용한다.

- `ExportSnapshot(limit)`: active v2/v3 storage에서 full event history + receipt map + context map을 검증해 Journal-compatible exact logical snapshot으로 직렬화한다. backup/cleanup처럼 global gate를 이미 exclusive로 보유한 caller는 profile exclusive lock을 추가로 잡고 v3 incomplete-tail/pending-coordination recovery를 먼저 완료한 뒤 export한다. 따라서 응답 유실 직후 남은 pending sidecar 때문에 backup이 불필요하게 막히지 않는다. backup/cleanup caller는 현재 공개 동작과 같은 `128 MiB` logical snapshot limit을 전달한다. limit을 넘으면 기존과 동일한 storage/size failure로 종료하며 routine task query/mutation scalability와 별개로 취급한다. 내부 v2→v3 migration은 이 byte-snapshot API를 사용하지 않고 event/metadata iterator로 streaming하므로 128 MiB backup limit이 migration blocker가 되지 않는다.
- `StageSnapshot(data, mode)`: validated logical snapshot을 full event history와 함께 replay한 뒤 새 v3 generation으로 stage+fsync한다. `exact` mode는 cleanup restore처럼 receipt/context/run state의 의미를 보존하되, v3 sidecar를 만들기 전에 legacy receipt를 migration과 같은 canonicalization 규칙으로 정규화하여 이후 replay가 `State.Events`에 의존하지 않게 한다. `normalized-restore` mode는 현재 `tasks.RestoreSnapshot`과 동일하게 active claim을 release하고 context/receipt를 제거해 backup/import restore semantics를 유지한다. logical Version 1 history는 Version 1로 보존하며 임의 upgrade event를 restore 시점에 추가하지 않는다.
- backup/import restore는 `normalized-restore`, cleanup restore는 `exact`만 사용한다. 이 mode는 내부 typed enum으로 고정하고 사용자 옵션으로 노출하지 않는다.
- cleanup archive-readiness/snapshot API도 caller-held-gate variant를 제공해 `Engine.lock()`이 잡은 global exclusive gate를 다시 acquire하지 않는다. v3 profile이면 profile exclusive lock 아래 incomplete WAL/pending coordination을 먼저 수선한 뒤 compact state의 `last_event_at`, running run, completion assessment와 exact snapshot stamp를 계산한다.
- 실제 profile restore transaction은 backup payload의 **domain presence 자체**도 현재 계약대로 적용한다. selected Values가 있으면 values canonical state + identity + 필요한 legacy blocker를 게시하고, 없으면 replace 대상 profile의 기존 values canonical/legacy state + identity + marker를 같은 transaction에서 제거한다. selected Tasks가 있으면 staged task v3 generation의 marker/head/identity + 필요한 legacy blocker를 게시하고, 없으면 target의 기존 v2/v3 task marker/head/state + identity + legacy marker를 logical deletion set으로 넣는다. 즉 backup에 없던 domain이 target에 남아 있는 상태를 허용하지 않는다.
- 이 domain create/replace/delete set과 backup receipt를 하나의 maintenance exclusive replacement로 게시한다. task domain을 교체/삭제해 old head가 끊긴 경우 이전 generation은 commit 후 orphan collection 대상으로 넘긴다.
- head switch 전에 staged generation이 완전하지 않으면 transaction을 시작하지 않음
- transaction 실패/취소 시 staged generation은 active state가 아니며 orphan cleanup 대상으로 남김

### cleanup item/source backward compatibility

`completed_tasks`의 archive metadata와 old incomplete cleanup receipt는 Phase 5 이전의 `tasks/<legacy-hex>.json` absolute `Item.Source`를 영구적으로 포함할 수 있다. physical-key migration 뒤에도 이를 invalid archive로 만들지 않는다.

- `Engine.validItem`의 completed-task source validator는 profile에 대해 정확히 두 syntactic shape만 허용한다: legacy `tasks/<hex(profile)>.json` 또는 canonical logical marker `tasks/<profilekey>.json`. 둘 다 `e.Data/tasks` 바로 아래여야 하며 symlink/external path는 허용하지 않는다.
- 새 preview는 현재 resolver가 legacy storage를 active source로 보고 있으면 legacy path를, canonical/v3이면 canonical marker path를 `Item.Source`로 사용한다.
- archive restore/purge는 old `Item.Source`가 현재 active physical location을 가리킨다고 가정하지 않는다. `Item.Profile`을 logical identity로 사용해 tasks store를 resolve하고 canonical logical digest/conflict 규칙을 적용한다.
- old incomplete cleanup receipt가 legacy candidate를 durable workset에 가지고 있는 상태에서 그 source가 **다른 operation에 의해** canonical migration된 경우에는 same-request apply가 path를 임의 재타겟하지 않는다. receipt candidate의 original stamp/source가 더 이상 active source가 아니므로 `revision_conflict`로 멈춘다. cleanup 자신이 앞서 수행한 item mutation만 archive entry/progress recovery 규칙으로 이어간다.
- old archive entry의 Source 문자열은 audit metadata로 그대로 유지하고 migration 때문에 rewrite하지 않는다.

completed-task cleanup도 v3 physical files를 개별 candidate로 노출하지 않는다. cleanup package가 task filename을 직접 읽어 `ArchiveReady`를 계산하는 결합을 제거하고, tasks store가 profile의 logical archive readiness와 stable snapshot/stamp를 반환한다. v3 readiness는 repaired compact `MaterializedState` + bounded WAL tail을 기준으로 판단한다. **event revision이 0이거나 `last_event_at`이 zero이면 candidate가 아니다**; current `ArchiveReady`의 `len(events)>0` 조건을 보존한다. 그 다음 active run 부재, included task/workstream 완료성, completion-current 조건을 판정하고 `last_event_at`이 30일 cutoff 이전인지 확인한다. zero-event receipt는 revision/`last_event_at`을 갱신하지 않는다. 따라서 preview readiness 자체는 full history replay를 요구하지 않는다. candidate가 실제 preview item으로 materialize될 때만 canonical full-history `ExportSnapshot(limit)`을 한 번 생성해 `Bytes`와 canonical export digest를 계산한다.

공개 `Item.Source`는 기존 path-like string 성격을 유지해 active task logical marker `tasks/<key>.json`의 절대 경로를 사용한다. generation/head/revision/canonical-export digest는 candidate `Stamp` 계산에 포함해 preview stale 검증을 수행한다. 따라서 WAL/snapshot 내부 파일 경로를 공개 source로 노출하지 않으면서 기존 cleanup JSON shape를 유지한다.

apply 시 logical task profile snapshot을 archive payload로 만들고 v2 actual source 또는 v3 marker+head를 logical deletion 대상으로 다룬다. canonical task-domain identity sidecar와 Phase 5에서 남긴 legacy `profile-key-v1` marker가 있으면 logical task deletion transaction에서 함께 제거한다. v3 generation directory는 marker/head/identity removal이 durable해진 뒤 orphan으로 취급하며 safe orphan collection에서 삭제한다.

cleanup `exact` restore는 archive payload를 새 generation으로 stage하더라도 publication 전에 global exclusive gate 아래 현재 logical task storage를 다시 resolve한다. 이때 byte-for-byte raw archive digest를 restore 동등성 기준으로 쓰지 않는다. `CanonicalizeExactSnapshot(data, profile)`이 full history를 검증하고 legacy receipt normalization, deterministic map/list serialization을 적용한 **canonical logical snapshot digest**를 계산한다. 새 cleanup archive는 이 canonical snapshot을 payload로 저장하고, Phase 7 이전에 만들어진 old raw task archive는 restore 시에만 canonicalize하여 비교한다.

현재 state가 없으면 canonicalized archive snapshot으로 staged generation을 게시한다. 현재 logical export의 canonical digest가 archive snapshot의 canonical digest와 같으면 기존 state를 그대로 두고 archive metadata만 restored로 수렴시킨다. 따라서 old v2 raw archive를 v3로 restore한 뒤 응답이 유실되어도 receipt canonicalization 때문에 `revision_conflict`가 나지 않는다. canonical digest가 다르거나 다른 active task state가 존재하면 기존 계약대로 `revision_conflict`를 반환하고 head/identity를 덮어쓰지 않는다. archive entry/payload의 raw integrity digest는 파일 손상 검증용으로 계속 유지하고 logical equality와 혼용하지 않는다. backup/import restore는 별도의 preview digest + `replace` 계약을 그대로 사용하며, replace가 승인된 경우에만 staged generation으로 head를 전환한다. 두 경로 모두 task identity sidecar + v3 marker+head + 필요한 legacy downgrade blocker를 하나의 publication set으로 게시한다.

profile catalog/backup enumeration도 v3 generation 내부 파일을 profile로 오인하지 않는다. task-domain identity sidecar를 active profile identity source로 사용하고, top-level `task-v3` marker와 head의 exact profile/key가 sidecar와 일치하는지 교차검증한다. generation 내부 파일명은 profile enumeration source가 아니다.

orphan generation 삭제 조건:

- 어떤 active head도 참조하지 않음
- maintenance recovery journal이 해당 head 전환을 복구 중이지 않음
- 해당 profile의 global maintenance/profile exclusive lock을 보유해 staging/mutation/read와 경쟁하지 않음
- private directory/file 검증 통과

위 조건이 만족되면 age grace 없이 즉시 삭제 가능하다. head switch가 commit된 뒤 restore source는 backup/cleanup archive payload이고, staged generation이 head에 게시되지 않았다면 애초에 active state가 아니므로 generation 자체에 별도 30일 retention을 둘 이유가 없다. cleanup 성공 뒤 old generation과 archive payload를 30일 이중 보관하지 않는다.

orphan generation 삭제는 **logical commit 이후 best-effort cleanup**이다. head switch/marker deletion이 이미 committed된 호출에서 orphan remove 실패를 새 operation failure로 바꾸지 않는다. 그러면 caller는 실제 commit 여부가 모호해지기 때문이다. 대신 tasks package는 다음 global/profile exclusive 진입 시작 시 active head와 maintenance pending transaction을 먼저 resolve한 뒤 orphan generation을 다시 청소한다. 그 pre-operation cleanup에서도 private path 검증/삭제가 실패하면 새 mutation을 시작하기 전에 `storage_error`로 막는다. 즉 이미 commit된 사용자 operation은 성공으로 유지하되, persistent disk-cleanup 문제는 다음 mutation을 조용히 통과시키지 않는다.

## 7.6 Query loading / pagination

현재 task query 중에는 materialized current state만 필요한 명령과 `State.Events`를 직접 사용하는 history/graph/revision 명령이 섞여 있다. v3에서 모든 query가 compact snapshot만으로 동작한다고 가정하지 않는다.

query loader를 두 경계로 나눈다.

- current-state loader: list/show/spec show/plan show/check/impact/next/current처럼 event history 자체를 직접 소비하지 않는 경로는 compact snapshot + WAL tail만 replay하고 full historical frames를 materialize하지 않는다.
- history loader: `history`, `context`, `export`, `checkpoint list`, `at-revision` reconstruction처럼 event stream을 직접 소비하는 경로만 WAL event iterator를 사용한다. tree 자체의 계산은 current-state loader로 충분하지만 **truncated 첫 tree 결과가 GraphSnapshot cursor를 생성해야 할 때**는 그 시점에 full event iterator를 추가로 사용해 기존 snapshot payload 의미를 보존한다. 필요 범위가 명확한 history/revision query는 해당 revision/range까지만 stream하고, full export/graph snapshot처럼 전체 history가 필요한 경우에만 전체 WAL을 순회한다.

`MaterializedState` 적용 로직은 current projection을 갱신하면서 `Events` slice를 누적하지 않는 별도 apply 경계를 사용한다. 기존 history-sensitive helper가 `State.Events`를 암묵적으로 요구하는 부분은 WAL iterator/explicit history input으로 분리한다.

### lockless completion snapshot

`tasks.Store.CompletionIDs`와 `values.Store.CompletionNames`는 shell completion용 best-effort read라는 기존 역할을 유지하고 maintenance/profile lock을 새로 만들거나 기다리지 않는다. 다만 multi-file/canonical migration에서 torn view를 정상 state로 해석하지 않도록 bounded stable-read protocol을 사용한다.

- values completion: direct profile identity가 이미 있으므로 canonical pair/legacy source를 lockless resolve한다. migration transaction의 중간 half-pair가 보이면 즉시 corruption으로 확정하지 않고 canonical/legacy locator를 한 번 다시 읽는다. 두 번째 관측도 안정된 valid state가 아니면 completion error를 반환한다. CLI completion caller는 현재처럼 오류를 무시하고 빈 후보로 처리한다.
- task v2 completion: 단일 atomic journal snapshot 경로를 유지한다.
- task v3 completion: head H1을 atomic read -> H1 generation의 compact snapshot + current WAL tail을 read-only replay -> head H2를 다시 읽는다. H1==H2이고 generation reads가 모두 valid할 때만 IDs를 반환한다. generation file이 중간에 사라지거나 H1!=H2이면 최신 head로 **한 번만** retry한다. 두 번째에도 unstable하면 completion error를 반환한다.
- lockless WAL read는 마지막 incomplete frame을 ignore할 수 있지만 checksum-invalid complete frame은 corruption으로 처리한다.
- orphan generation 삭제와 경쟁해 ENOENT가 나면 stale head로 간주해 retry하며, 임의의 다른 generation을 추측하지 않는다.

이 경로는 completion latency를 위해 lockless/best-effort를 유지하는 예외이고, `Catalog.List/Inspect`, task query, backup/cleanup처럼 correctness-bearing read는 반드시 maintenance/profile gate를 사용한다.

pagination은 기존 page snapshot semantics를 유지한다.

cursor 후속 page는 이미 저장된 query snapshot만 읽으므로 **maintenance global gate와 profile lock을 획득하기 전** fast path로 분기한다. 현재처럼 `Query` 진입 직후 global lock → journal load를 한 뒤 cursor를 처리하지 않는다.

- 일반 `token:offset` cursor: command/target/options/profile로 fingerprint와 limit을 먼저 검증하고 cache snapshot을 private-file 규칙으로 읽는다. snapshot projection/profile/fingerprint/expiry/offset을 검증한 뒤 저장된 `Revision`과 `Items`만으로 page를 반환한다. task storage는 열지 않는다.
- graph/tree cursor: cursor token으로 `GraphSnapshot`의 projection/profile/kind/expiry를 먼저 검증하고 저장된 event snapshot만 replay하여 continuation을 계산한다. active task journal revision을 다시 읽지 않는다. 기존 cursor 자체가 생성 당시 revision에 고정된 snapshot 계약을 유지한다.
- cursor cache read/prune는 task data-root maintenance lock과 무관하다. prune failure는 현재처럼 best-effort이며 cursor payload의 private-file validation은 유지한다.
- cursor가 invalid/expired하면 `cursor_invalid`를 반환하고 현재 journal로 자동 fallback하지 않는다.

첫 page/no-cursor 요청만 global shared gate + profile shared lock을 거쳐 current/history loader를 사용한다.

## 7.7 Benchmark gate

구현 전 baseline fixture를 고정한다.

profiles/fixtures:
- 1 profile / 1k, 10k, 100k events. **current item/run/tracking cardinality는 동일하게 유지**하고 같은 소수 item의 reversible edits/checkpoints로 history만 늘려 total-history 영향을 분리한다.
- 별도 scale fixture는 current item cardinality 증가 비용을 측정하되 history benchmark와 섞지 않는다.
- 8 profiles each 10k history events for cross-profile concurrency

operations:
- task list first page
- cursor second page
- task mutation one event
- multi-event request
- same-profile concurrent read/read
- different-profile concurrent mutation
- global exclusive restore/recovery gate waiting behavior

측정:
- `go test -benchmem`의 ns/op, B/op, allocs/op
- test-only `taskStorageIOStats`: snapshot bytes read/written, WAL bytes/frames read, WAL bytes appended, coordination files read/written, full-history replay/scan count
- lock test hook: global/profile shared/exclusive wait duration과 동시에 critical section에 들어간 profile 수. production build에는 timing aggregation을 두지 않는다.

acceptance:
- fixed-current-state 1k/10k/100k fixture에서 current list/mutation의 routine WAL frames read가 checkpoint threshold(`<=256 frames`, `<=4 MiB tail`) 안에 있고 **total historical WAL bytes 전체를 읽지 않을 것**
- 새 request ID mutation에서 `full-history replay/scan count == 0`
- valid cursor second page에서 task storage snapshot/WAL/profile-lock counters가 모두 0이고 query-cache file만 읽을 것
- different-profile task mutations가 global shared gate 아래 서로 다른 profile exclusive lock을 동시에 보유할 수 있음을 barrier test로 증명. 같은 profile mutation은 serialize
- global exclusive maintenance recovery/restore는 기존 global shared holders가 빠질 때까지 기다리고, writer-intent 이후 새 shared holder가 진입하지 않음을 turnstile test로 증명
- `values.Store.Read/Update` 같은 single-file routine operations는 shared global gate 동시성을 얻되 global `maintenance.Replace`를 사용하는 values management/cross-domain mutation은 의도적으로 exclusive 예외 유지
- data/retry/history/backup/cleanup contract regression 없음

---

# Phase 8 — Integrated verification and documentation

## Targeted validation matrix

| 문제 | 주요 unit/integration proof |
| --- | --- |
| 1 | archived marker vs corrupt hole enumeration |
| 2 | partial cleanup failure + same request resume |
| 3 | starting orphan + lease race + late supervisor |
| 4 | restart pre-stop fail preserves running old execution |
| 5 | PTY input, Ctrl+C, Ctrl+Z/continue, tty restoration |
| 6 | maintenance lock contention + SIGINT/HTTP cancel |
| 7 | 128-char profile + legacy migration + duplicate conflict |
| 8 | exact last-1-MiB content + append/compaction benchmark |
| 9 | WAL recovery + snapshot tail replay + shared/exclusive lock concurrency |

## 단계별 검증

Phase 1:

```sh
go test ./internal/services ./internal/cleanup ./internal/profiles
python3 verify/run.py cleanup
python3 verify/run.py processes
python3 verify/run.py profiles
python3 verify/run.py project_lifecycle
```

Phase 2:

```sh
go test ./internal/execution ./internal/services ./internal/lifecycle ./internal/cli ./internal/dashboard
python3 verify/run.py processes
python3 verify/run.py project_lifecycle
```

Phase 3:

```sh
go test ./internal/process ./internal/execution ./internal/cli
python3 verify/run.py tty
```

`verify/scenarios/tty.py`를 새로 추가한다. Python 표준 `pty`/`termios`만 사용해 실제 설치 binary를 PTY foreground에서 실행하고 입력, Ctrl+C, Ctrl+Z→fg, Ctrl+Z→bg, 종료 후 terminal foreground 복원을 검사한다. `verify/run.py`는 scenario 파일을 자동 발견하므로 `all` gate에도 포함된다.

Phase 4:

```sh
go test ./internal/values ./internal/tasks ./internal/cli ./internal/dashboard
python3 verify/run.py cancellation
```

`verify/scenarios/cancellation.py`를 새로 추가해 maintenance lock을 별도 process가 보유한 상태에서 `var list`와 task query에 SIGINT를 보내 lock release 전에 exit 130이 관측되는지 검사한다. Dashboard는 unit/httptest에서 request context cancel을 검증한다.

Phase 5A:

```sh
go test ./internal/maintenance
DEVTOOLS_LARGE_STORAGE_TEST=1 go test -run TestReplaceLargeBeforeImage ./internal/maintenance
```

기본 `internal/maintenance` suite에는 multi-target N번째 publish failure, recovery 중 재중단, legacy inline pending journal recovery, version-2 pointer의 old-decoder fail-closed, streaming-copy helper의 bounded-memory 동작을 작은 fixture로 검증한다. 실제 128 MiB 초과 private before-image는 `TestReplaceLargeBeforeImage`에서만 만들고 `DEVTOOLS_LARGE_STORAGE_TEST=1`이 없으면 skip한다. targeted Phase 5A 검증에서는 위 명령으로 반드시 실행하지만 일반 `go test -race ./...`마다 128 MiB 이상을 복사하지 않는다. large fixture도 거대한 byte slice를 만들지 않고 file streaming으로 생성한다.

Phase 5:

```sh
go test ./internal/maintenance ./internal/values ./internal/tasks ./internal/profiles ./internal/backup ./internal/cleanup ./internal/dashboard
python3 verify/run.py profiles backup tasks storage_migration
```

`verify/scenarios/storage_migration.py`는 legacy hex values/tasks fixture, 128자 canonical profile, 신규 canonical profile의 legacy downgrade marker, legacy/canonical split-brain, legacy value-management receipt replay를 실제 설치 binary로 검증한다.

Phase 6:

```sh
go test ./internal/services ./internal/cleanup
python3 verify/run.py processes
go test -run '^$' -bench 'BenchmarkBoundedLog' -benchmem ./internal/services
```

`internal/services/log_benchmark_test.go`에 structural counters가 포함된 benchmark를 두어 elapsed time뿐 아니라 input bytes 대비 snapshot rewrite/compaction 횟수를 비교한다.

Phase 7:

```sh
go test ./internal/tasks ./internal/maintenance ./internal/backup ./internal/profiles
python3 verify/run.py tasks workflow backup task_storage
go test -run '^$' -bench 'BenchmarkTaskStorage' -benchmem ./internal/tasks
```

`verify/scenarios/task_storage.py`는 legacy v2 fixture → v3 lazy migration, zero-event receipt replay, pending payload/tree 생성 후 `pending.json` 전 crash cleanup, durable `pending.json` 후 WAL 전 crash rollback, WAL commit 후 pending finalize replay, finalize 중 일부 sidecar만 rename된 crash recovery, incomplete WAL tail recovery, checksum corruption rejection, backup/cleanup restore generation switch를 검사한다. 새 request-id receipt miss와 routine pending recovery가 committed receipt shard/full WAL을 history-size로 enumerate하지 않는지는 unit seam/counter로 고정한다. `internal/tasks/store_benchmark_test.go`는 1k/10k/100k event와 8-profile concurrency matrix를 고정한다.

## Documentation convergence

구현과 함께 다음 durable contract를 실제 storage/runtime 의미에 맞춰 갱신한다.

- `docs/processes.md`: lost-start retry, restart pre-stop validation, raw-log physical implementation은 숨기되 공개 동작 갱신
- `docs/cleanup.md`: durable apply workset, process marker에 의한 정상 archived state, v3 task logical archive/restore 의미
- `docs/cli-contract.md`: interactive TTY/job-control과 cancellation behavior
- `docs/profile-retirement-contract.md`: `<hex>.json` 직접 경로를 logical profile storage/profilekey + task head/generation 모델로 교체
- `docs/devtools-hardening-plan.md`처럼 구현 완료 상태를 설명하는 기존 설계 문서의 storage path 서술이 새 모델과 충돌하면 현재 상태 기준으로 정정하거나 historical 기준임을 명시

public 문서에는 hash key, WAL frame 같은 내부 형식을 필요 이상 노출하지 않고 behavior/recovery contract만 기록한다.

## 최종 gate

```sh
go vet ./...
go test -race ./...
python3 verify/run.py all
```

Linux 외 최종 release 전에는 macOS와 실제 WSL에서 TTY/PTTY, flock, rename/fsync 관련 targeted scenario를 별도로 실행한다.

---

# 구현/커밋 단위

각 항목은 review 가능한 독립 work unit으로 커밋한다.

1. process storage state + archived-log discovery
2. cleanup durable workset/resume
3. starting reconciliation/fencing + startup grace
4. restart pre-stop validation
5. interactive TTY execution
6. context-aware read locks
7. maintenance on-disk before-image transaction + legacy pending recovery compatibility
8. profile storage key + downgrade markers + receipt migration
9. bounded log append/compaction
10. maintenance shared/exclusive locking foundation + profile shared/exclusive lock helpers
11. task storage v3 core: generation/WAL/materialized snapshot/durable coordination sidecars + v2 migration + first-state publication
12. task v3 backup/restore/cleanup integration + generation switching + orphan generation collection
13. task current/history loaders + cursor fast path
14. storage/log/task benchmarks + public/internal docs convergence

각 commit은 관련 targeted validation이 통과한 뒤 다음 단계로 진행한다. 구현 순서는 위 번호로 고정한다. 6번의 context-aware acquisition이 10번 shared/exclusive gate의 cancellation contract를 선행하고, 7번 large-file transaction이 8번 profile migration과 11번 v3 head switch의 recovery primitive를 선행하며, 8번 profile key가 11번 이후 모든 task physical path identity를 선행한다. Phase 7은 10~13번으로 나누되, 각 intermediate commit에서 기존 v2 path 또는 완성된 v3 path 중 하나가 항상 정상 동작해 global recovery 보장을 깨는 상태를 main에 남기지 않는다.

---

# Rollout / compatibility

- persistent profile/task format을 변경하는 Phase 5/7은 **새 바이너리가 기존 legacy state를 읽는 backward read compatibility**를 유지한다. Phase 6 raw-log writer도 physical `output.log` representation을 최대 1280 KiB까지 확장하므로 storage representation change로 취급한다.
- format migration 후 구버전 바이너리의 write compatibility는 지원하지 않는다. profile/task는 `profile-key-v1` / `task-v3` strict-decoder marker로 구버전이 별도 legacy state를 새로 만들어 data fork를 일으키지 못하게 fail-closed한다. raw log에는 downgrade marker를 추가하지 않으므로 새 writer가 1 MiB를 초과한 physical log를 만든 뒤 구버전 `process logs`/log writer가 해당 file을 읽지 못할 수 있다.
- lazy migration은 첫 mutation에서 자동 발생하고 Phase 6 log representation도 새 process output부터 즉시 사용될 수 있으므로 **migration 전 safety backup이나 old-log compatibility가 항상 존재한다고 가정하지 않는다**. 이 릴리스가 새 storage/log representation을 쓰기 시작한 뒤 구버전 binary로의 in-place downgrade는 지원하지 않는 것으로 계약한다. 사용자가 migration 전에 별도로 생성해 보관한 구버전 호환 backup이 있는 경우에만 별도 데이터 디렉터리에서 구버전 restore가 가능하다. release notes는 이 forward-migration boundary와 `in-place downgrade unsupported`를 명확히 고지한다.
- destructive bulk migration command는 추가하지 않는다. lazy migration을 사용한다.
- migration 중 오류는 기존 actual source/head가 active 상태로 복구되어 retry 가능해야 한다. staged canonical/generation artifacts는 active pointer가 게시되지 않은 orphan으로만 남을 수 있다.
- backup/restore fixture는 legacy와 current format을 모두 포함한다.
- archive/backup payload의 logical profile identity는 physical key가 아니라 원래 profile string으로 유지한다.

---

# Review-loop ledger

## Cycle 0 — draft

- Scope: 문제 1~9 전체의 구현 전 계획
- Modes: planning
- Axes 예정: scope/acceptance, logic/dependencies, contract/code match, migration/recovery risk, validation, context hygiene
- Status: draft produced
- Next entry: planning review cycle 1

## Cycle 1 — contract / recovery review

- Axes: scope/acceptance, logic/dependencies, code-contract match, migration/recovery, validation
- Findings fixed:
  - restart plan이 현재 `services.Request`에 없는 pre-stop hook을 암묵적으로 가정함 -> explicit `BeforeRestart` boundary와 entry-point ownership 추가
  - pre-stop validation failure를 completed replay처럼 기술해 기존 incomplete process receipt semantics와 충돌 -> non-mutating retry/revalidation 의미로 수정
  - canonical+legacy duplicate를 dedupe하면 split-brain을 숨길 수 있음 -> recovery 후 동시 존재는 storage error로 고정
  - backup profile enumeration이 현재 filename hex decode에 의존함 -> common catalog/store resolver 사용으로 수정
  - log compaction target 768 KiB가 last-1-MiB retention을 깨뜨림 -> target 1 MiB + large-write special case로 수정
  - append fd가 compaction rename 뒤 stale inode를 계속 가리킬 수 있음 -> per-Write open/validate/sync/close로 고정
  - task snapshot에 current `State.Events`/receipt/context map을 넣으면 history-size rewrite가 남음 -> compact MaterializedState + per-generation derived indexes로 분리
  - task v3 raw multi-file layout이 기존 values/tasks atomic backup restore를 깨뜨림 -> staged generation + atomic head-pointer switch로 수정
  - v2 migration에서 shared global lock을 잡은 채 exclusive로 upgrade하는 설계가 lock order를 깨뜨림 -> one-time migration은 global exclusive -> profile exclusive 순서로 고정
  - incomplete WAL tail과 checksum corruption을 같은 auto-truncate로 취급함 -> EOF partial tail만 writer가 truncate, full checksum mismatch는 corruption으로 고정
  - process archive purge 뒤 marker 제거 가능성이 corrupt hole을 만들 수 있음 -> 하위 lifecycle plan에 marker survival rule 추가
  - TTY stop/resume에 waiter ownership이 불명확함 -> interactive branch의 single wait loop + wrapper SIGSTOP 전이 명시
- Validation: current code/contracts와 계획 문서 대조 완료; source code mutation 없음
- Unresolved findings: none carried from cycle 1
- Next entry: affected-axis planning re-review cycle 2

## Cycle 2 — race / compatibility / durability re-review

- Axes: lifecycle race, selective cleanup reachability, downgrade/split-brain compatibility, retry metadata durability, WAL commit/recovery, lock granularity, validation executability
- Findings fixed:
  - process archive marker가 record 부재만 설명하고 residual `output.log` 소유권을 잃음 -> marker에 최소 profile/ended_at/capture metadata를 보존하고 cleanup 전용 archived scan 추가
  - caller crash 직후 late supervisor가 lease를 잡기 전 retry가 terminalize할 race -> 기존 10초 startup deadline과 공유하는 reconciliation grace 추가
  - legacy state filename/lock 길이 경계를 혼동함 -> `.json`은 125자, `.json.lock`은 122자까지라는 실제 component 경계를 분리
  - 신규 canonical profile에 legacy source가 없으면 downgrade blocker도 생기지 않아 old binary가 split-brain state를 만들 수 있음 -> first canonical creation도 marker와 atomic publication
  - values state만 migration하면 legacy management receipt를 못 찾아 same request mutation을 재적용할 수 있음 -> canonical-first + legacy receipt fallback/migration 규칙 추가
  - shared maintenance gate 아래 global `maintenance.Replace`를 호출할 수 없는 구조 충돌 -> values management/cross-domain Replace 경로는 exclusive 예외로 유지
  - physical storage v3와 logical `JournalVersion`을 혼동할 여지 -> task storage format v3와 event semantic version 1/2를 명시적으로 분리
  - zero-event receipt/context mutation이 WAL source-of-truth에서 빠짐 -> zero-revision mutation/metadata frame을 정식 지원
  - nested generation/shard directory가 기존 final-Lstat 방식으로 symlink 방어를 약화할 수 있음 -> component-wise private directory validation 추가
  - staged generation의 WAL fsync를 active commit으로 오인할 수 있음 -> active generation은 WAL fsync, staged generation은 atomic head publication을 logical commit으로 분리
  - 신규 task profile의 첫 v3 publication 경로가 없음 -> staged first mutation + marker/head/legacy blocker atomic publication 추가
  - PTY/cancellation/storage migration/task storage validation이 주석 수준 -> auto-discovered verify scenarios와 benchmark command/file을 구체화
  - completed-task cleanup의 marker 제거 규칙을 future profile retirement에 잘못 재사용할 위험 -> active-history cleanup과 retired-name blocker 정책을 분리
- Validation: code/path/API references rechecked against current repository; source code mutation 없음
- Unresolved findings: none carried from cycle 2
- Next entry: final planning/context-hygiene cycle 3

## Cycle 3 — final planning / context hygiene

- Axes: scope/non-goals/acceptance, dependency order, public contract preservation, storage-version terminology, migration completeness, validation commands, reviewability/context hygiene
- Findings fixed during affected-axis pass:
  - legacy task journal 전체에 실제 128 MiB cap이 있다는 잘못된 전제 -> total frame allocation cap을 제거하고 length-prefixed subrecord streaming으로 변경
  - task storage v3라는 physical format과 logical `JournalVersion` 명칭이 일부 work item에 다시 섞임 -> `task storage v3`로 canonical terminology 통일
  - checkpoint threshold가 구현 시점 open tuning으로 남아 있음 -> 256 frames / 4 MiB tail 기본값 고정, benchmark는 증거가 있을 때만 같은 work item에서 조정
  - v3 cleanup `Item.Source`가 synthetic identity로 바뀔 여지 -> logical marker absolute path를 유지하고 generation/revision은 stamp에 포함
  - Phase 7 구현 단위가 너무 커 review boundary가 불명확함 -> lock foundation, storage core/migration, backup-cleanup integration, query loader를 별도 commit unit으로 분리
  - Phase별 검증이 placeholder 주석에 머문 부분 -> `tty`, `cancellation`, `storage_migration`, `task_storage` verify scenario와 exact benchmark command로 고정
- Re-review result: selected planning axes 전체에서 actionable finding 없음
- Validation: 두 계획 문서 `git diff --check --no-index /dev/null <file>`에서 whitespace/error output 없음; Git status는 이 세션이 만든 계획 문서 2개만 untracked
- User decisions: none remaining
- Status: review-loop clean; implementation may start from work item 1


## Cycle 4 — deep compatibility / crash-consistency re-review

- Trigger: user-requested additional review-loop after Cycle 3
- Axes: upgrade compatibility, crash commit ambiguity, retry exactness, lock-order/fairness, multi-file storage integrity, cleanup/restore idempotency, TTY/cancellation contract match, performance-proof executability
- Findings fixed:
  - marker 도입 전 정상 `completed_process` cleanup이 unmarked UUID hole로 남아 새 버전에서 corruption으로 오판될 수 있음 -> valid un-restored old cleanup archive metadata를 neutral `archiveproof` compatibility proof로 인정하고 passive 조회는 marker를 쓰지 않음
  - legacy proof를 얻기 위해 모든 malformed cleanup archive가 process enumeration을 깨뜨릴 수 있음 -> unmarked hole이 있을 때만 proof lookup, unrelated malformed entries는 후보 제외, 해당 hole에 proof가 없을 때만 corruption
  - legacy archived process는 ended/capture marker metadata가 없어 residual log owner를 잃음 -> immutable archive-id/profile/archived-at 기반 legacy owner stamp + 30일 completed-process proof가 7일 raw-log retention을 충족한다는 규칙 추가
  - startup grace를 explicit stop에도 적용할 여지 -> status/start retry만 grace를 존중하고 stop은 즉시 lease fencing하도록 helper mode 분리
  - project restart multi-command에 all-or-nothing preflight barrier가 암묵적으로 생길 여지 -> 기존 child-level partial-success/retry semantics 유지, 각 child stop 직전 non-destructive validation
  - `maintenance.Replace`의 128 MiB inline before-image가 valid large task journal migration을 막음 -> on-disk streaming before-image transaction 도입
  - Replace target publish 후 pending 삭제 시점만으로 commit을 표현해 commit/rollback이 모호함 -> `applying -> committed` durable phase를 도입하고 committed pointer publication을 logical commit point로 고정
  - committed 이후 scratch cleanup 실패를 mutation failure로 반환하면 already-committed upper operation을 재실행할 수 있음 -> commit 이후 cleanup은 post-commit, Replace는 성공 유지, 다음 Acquire가 cleanup 실패를 차단
  - pointer publication 전 crash가 secret-bearing transaction scratch를 남기고 read-only workload에서 오래 유지할 수 있음 -> shared acquire도 orphan scratch를 감지하면 exclusive cleanup으로 재시작
  - nested `.identity` replacement가 generic path join/write로 symlink defense를 우회할 수 있음 -> typed component-safe target resolver를 Replace read/publish/recovery 전 경로에 적용
  - canonical physical key를 reversible mixed-case encoding으로 두면 case-insensitive filesystem collision이 생기고, pure hash만 쓰면 catalog가 identity를 복원하지 못함 -> fixed lowercase SHA-256 key + per-domain private identity sidecar로 확정
  - canonical state는 존재하지만 required legacy downgrade marker가 사라진 상태를 정상 허용함 -> legacy path가 표현 가능한 profile에서는 canonical pair + exact blocker가 모두 있어야 정상
  - migrated state만 보고 legacy values-management receipt를 놓치면 same request mutation을 재적용할 수 있음 -> canonical-first + representable legacy receipt fallback/migration
  - backup enumeration을 전체 profile catalog로 대체하면 process/port-only profile이 새 backup entry로 들어감 -> values/tasks logical domain identity의 union만 유지
  - Catalog correctness read가 lockless `CompletionIDs`를 재사용할 수 있음 -> caller-held-gate current-state loader로 분리
  - shared global gate 아래 migration 필요성을 발견한 routine mutation이 in-place exclusive upgrade하면 deadlock 위험 -> locks를 모두 놓고 global exclusive -> profile exclusive 순서로 처음부터 re-resolve
  - single flock fairness에 기대면 exclusive restore/recovery가 reader wave에 밀릴 수 있음 -> global/profile RW lock에 writer-intent turnstile 추가
  - raw-log writer/read만 lock하고 cleanup remove/restore가 lock을 우회할 수 있음 -> archive snapshot/remove/restore도 동일 `output.log.lock` exclusive scope 사용
  - log candidate stamp가 physical compaction representation에 묶일 수 있음 -> logical last-1-MiB bytes + owner metadata 기반 stamp/Bytes로 변경
  - 이미 watermark를 초과한 physical log를 writer가 자동 truncate하면 corruption을 숨길 수 있음 -> existing file > high watermark는 storage error
  - task receipt를 WAL append 뒤 derived cache로 쓰면 crash 후 sidecar miss 복구를 위해 new request마다 full-history scan이 필요할 수 있음 -> WAL-before durable generation-local `pending.json` + payload tree, WAL fsync commit, committed sidecar finalize protocol로 변경
  - committed receipt/context history shard를 pending recovery가 enumerate하면 mutation 비용이 history-size에 비례함 -> generation root에 단 하나의 pending pointer/tree만 두고 committed shards와 분리
  - missing/corrupt snapshot을 coordination repair 전에 checkpoint로 다시 쓰면 pending frame을 건너뛸 수 있음 -> full replay는 memory-only, pending recovery 후 snapshot repair
  - v2→v3 migration과 triggering mutation이 서로 다른 commit point가 될 수 있음 -> triggering frame/receipt/snapshot을 staged generation에 포함하고 head publication 하나로 atomic commit
  - legacy receipt replay가 `State.Events` 기반 normalization에 의존함 -> migration/exact-stage 시 receipt를 canonicalize하여 v3 replay가 history slice를 요구하지 않게 함
  - zero-event first mutation이 logical v1을 의도치 않게 v2로 올릴 수 있음 -> eventful first mutation만 `profile.upgraded`, zero-event committed mutation은 v1 유지
  - exact cleanup restore가 canonicalized receipt 때문에 raw archive bytes와 달라져 retry conflict가 날 수 있음 -> raw integrity digest와 canonical logical snapshot digest를 분리해 idempotency 판정
  - backup replace에서 source backup에 없는 domain을 target에 남길 수 있음 -> absent domain도 same transaction의 logical deletion set에 포함
  - old cleanup archive/receipt의 legacy task `Item.Source`가 profile-key migration 후 invalid가 될 수 있음 -> legacy/canonical source shape를 모두 검증하고 restore는 Source path가 아닌 logical profile identity로 resolve
  - task v3 lockless completion이 head switch/orphan generation delete의 torn view를 볼 수 있음 -> head-before/read/head-after stable snapshot + one retry
  - cursor second page가 journal load뿐 아니라 global maintenance gate까지 먼저 획득함 -> cursor cache fast path를 global/profile gate 이전으로 이동
  - task current-state cleanup readiness에 last event time이 없어 full history를 요구함 -> MaterializedState에 `last_event_at` 추가, zero-event receipt는 갱신하지 않음
  - v3 logical Version 1 exact restore 후 first eventful mutation의 upgrade baseline이 full Events를 요구함 -> 그 한 번만 WAL history iterator로 baseline 계산
  - WAL frame 기반 revision loader가 multi-event request 중간 revision을 허용할 수 있음 -> frame boundary에서만 `AtRevision` 허용, 기존 `revision_not_committed` 유지
  - large-file proof를 기본 race suite에 항상 넣으면 검증 비용이 과도함 -> targeted env-gated >128 MiB test + 기본 작은 crash/recovery tests로 분리
  - log/task benchmark acceptance가 환경 의존 prose에 머묾 -> test-only I/O counters, fixed-current-state history fixtures, lock barriers와 structural acceptance로 구체화
- Source-code mutation: none
- User decisions: none
- Unresolved findings: none carried from Cycle 4 fixes
- Next entry: affected-axis final re-review cycle 5


## Cycle 5 — affected-axis final re-review

- Axes: active contract consistency, current-code path verification, upgrade/downgrade blockers, crash/retry state machines, lock ordering, logical-vs-physical identity, validation completeness, context hygiene
- Re-review checks:
  - marker/future-process state vs marker-before-release legacy archive proof and residual-log cleanup
  - status/start grace vs explicit stop fencing and late-supervisor behavior
  - direct/Dashboard/project restart ordering and project child-level partial-success semantics
  - TTY foreground/job-control model against Go `SysProcAttr.Foreground/Ctty` behavior and current top-level signal ownership
  - context propagation through values/tasks/catalog wrappers and cancellation error mapping
  - maintenance v1/v2 pending journal compatibility, large before-image streaming, component-safe nested targets, applying/committed recovery and post-commit cleanup semantics
  - canonical profile hash + identity sidecar, required legacy blocker, old binary values/tasks/backup fail-closed paths, legacy receipt fallback and backup enumeration scope
  - bounded log logical stamp/locking/high-watermark invariants and cleanup/restore interaction
  - global/profile RW lock order, writer-intent turnstile and shared-to-exclusive restart rule
  - task v3 WAL frame boundaries, generation-local pending transaction, authoritative committed coordination, checkpoint ordering, zero-event receipts, v1 upgrade, v2 migration atomicity, exact/normalized restore, absent-domain backup deletion, cursor/completion fast paths, revision boundaries
  - cleanup legacy/canonical task Source compatibility and canonical logical digest idempotency
  - targeted large-storage/log/task benchmark and integration-test gates
- Code-contract spot checks:
  - current old backup `--replace` reads migration marker but strict `values.RestoreSnapshot/tasks.RestoreSnapshot` validation fails before raw overwrite, so downgrade blocker is fail-closed
  - current `AtRevision` rejects mid-request revisions, preserved by WAL frame-boundary rule
  - current zero-event `run.claimed claimed=false` receipt semantics and v1 upgrade behavior are preserved
  - current completion APIs are lockless best-effort only; correctness-bearing Catalog paths no longer reuse them in the plan
- Re-review result: no actionable findings on selected planning axes
- Source-code mutation: none
- User decisions: none remaining
- Validation: both plan files produced no `git diff --check --no-index` whitespace/error output; stale searches for superseded `WAL source of truth`, higher-level migration-marker exception, and unconditional markerless-hole failure returned no matches; Git status contains only the two untracked plan documents from this planning session
- Status: review-loop clean; implementation may start from work item 1
