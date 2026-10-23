# 샌드박스 조회: 잠금·복구 보존 구현 계획

작성: 2026-10-23. 조사 기준: `b33d12d98bcb09999d5166e18bfd527c0c674fe8`.
상태: 조사·계획 등록. 제품 구현, 릴리스, 사용자 CLI 교체는 이 작업의 범위가 아니다.
devtools profile: `devtools`. Workstream: `9bb41be8-11bd-45b2-86fb-59092b6aad5e` (draft).
이 문서의 제품·호환성 결정은 확정한다. 후속 작업자는 아래 검증 실패를 이유로
잠금 생략, 권한 확대, 다른 저장소 선택, 지원 플랫폼 축소를 임의 적용하지 않는다.

## 1. 목표와 완료 경계

macOS/Linux/WSL의 기존 private 로컬 저장소에 대해 데이터 경로의 읽기는 허용되고
쓰기는 차단된 실행 환경에서도 다음 명령이 정상 데이터를 조회할 수 있게 한다.

- `variable list`, `variable get`, 별칭 `var list/get`.
- `secret list`, 별칭 `sec list`. 기존처럼 메타데이터만 반환한다.
- `env list`.

전제는 기존 maintenance 디렉터리와 두 잠금 파일이 존재하고 복구할 transaction이
없다는 것이다. 데이터 읽기 자체까지 금지된 환경은 지원 목표가 아니다.
root 전역 복구가 필요하면 다른 profile의 transaction이어도 조회를 진행하지 않는다.
쓰기 가능한 실행에서는 최초 잠금 준비와 자동 복구를 계속 수행한다.
쓰기 불가능한 실행에서는 준비/복구 필요를 명시한 실패를 반환한다.

안전 보장: 기존 writer와 같은 잠금 객체·획득 순서 사용, 데이터 읽기 완료까지
공유 gate 유지, 미복구 상태 미노출, 기존 secret 비노출·무결성 표시 유지.
가용성 보장: 정상 기존 저장소의 조회 성공. 복구까지 읽기 권한만으로 자동 완료하는
가용성은 약속하지 않는다. 쓰기 가능한 별도 서비스는 도입하지 않는다.

## 2. 조사 근거와 현재 동작

| 경로 | 확인한 사실 | 계획에 주는 제약 |
| --- | --- | --- |
| `internal/cli/tools.go:28,61,123,155,187` | store는 `CurrentStore`인 ModernStore. 조회와 set 입력 전 검사 모두 Read 사용 | Read 일괄 변경 금지. 조회 두 분기만 교체 |
| `internal/cli/import.go:38` | import의 env 사전 검사도 Read 사용 | import·dry-run 경로 변경 제외 |
| `internal/values/modern.go:173` | Read는 AcquireShared 후 ReadHeld, 후자는 snapshot을 읽어 메타데이터/공개값 해석 | ReadHeld 재사용, 키 세션 호출 추가 금지 |
| `internal/maintenance/rwlock.go:32,93,113` | MkdirAll 검사, turnstile→gate, shared 해제 후 exclusive 복구와 재검사 | 생성 없는 조회 경로와 기존 복구 경로 분리 |
| `internal/maintenance/rwlock_unix.go:16,29,51` | 0700 디렉터리, private 일반 파일, O_NOFOLLOW/O_NONBLOCK, O_CREATE/O_RDWR, flock polling | 접근 모드만 분리. 기존 검사·취소 유지 |
| `internal/maintenance/rwlock_windows.go:20` | parent guard와 byte-range lock 사용 | Unix 방식의 일괄 적용 금지 |
| `internal/maintenance/rwlock.go:63` | pending pointer와 transactions 디렉터리 검사 | 판정 재사용. orphan 정리도 우회하지 않음 |
| `internal/maintenance/protected_root.go:19` | version 3 / ready pointer는 보호된 정상 root 표시 | marker 존재 자체를 복구 필요로 간주하지 않음 |
| `internal/maintenance/maintenance.go:59` | legacy/v2~v4 복구, committed 정리, orphan 정리 수행 | 복구 알고리즘/commit 경계 변경 금지 |
| `internal/profilekey/snapshot.go:19` | caller가 gate를 보유한 상태에서 legacy/canonical snapshot 선택 | lockless completion 경로로 대체 금지 |
| `internal/values/snapshot.go:251` | VaultError는 context 우선, 일반 저장 오류는 storage_error/exit 3 | 기존 top-level 오류 코드·exit 유지 |
| `internal/storageerr/error.go:50`, `internal/protocol/diagnostic.go:57` | 원래 OS 오류 및 stage 보존, public facts.state 허용 | 새 public field나 schema 개정 불필요 |
| `docs/storage-design.md:20`, `docs/diagnostic-contract.md:121`, `docs/windows.md:141` | 일반 조회에도 잠금·복구 수행 가능 | 정상 경로 개선과 복구 fallback을 명시해 문서 갱신 |

### 실행한 조사 검증

- 설치 CLI: `0.32.0`, commit `unknown`, protocol `12`. 소스의 protocol도 `12`, envelope `1`.
- `project inspect`: profile `devtools`, 저장소의 `devtools.toml`로 선택됨.
- 기존 workstream 전체 7개, `next_cursor:null` 확인. 동일 주제 등록 없음.
- 현 샌드박스에서 `task workstream list`도 `storage_error`, reason `read_only`,
  OS 30, stage `acquire_lock`으로 실패. 허용된 외부 실행에서는 성공.
  이는 Linux의 동일 계열 제약 증거이며 보고된 macOS 실패를 재현한 것은 아니다.
- WSL interactive bash에서 Go `go1.27.3 linux/amd64` 확인.
- `go test -race ./internal/maintenance ./internal/values ./internal/profilekey ./internal/cli ./internal/protocol ./internal/storageerr`
  6개 패키지 통과. maintenance 8.068s, values 43.790s, profilekey 1.046s,
  cli 128.765s, protocol 1.089s, storageerr 1.021s.
- 일회용 fixture를 `bwrap --ro-bind / /` 안에서 열어 Linux 실제 read-only mount 검증.
  O_RDONLY+LOCK_SH 성공, O_CREATE/O_RDWR 거부, 바깥 기존 O_RDWR writer의
  LOCK_EX와 양방향 차단, close 후 writer 재획득을 확인했다.
  조사 probe는 `/tmp/devtools-readonly-lock-probe.py`. 제품 구현 검증으로 계산하지 않는다.
- Linux [flock(2)](https://man7.org/linux/man-pages/man2/flock.2.html)는 open 모드와 무관한
  flock을 명시한다. NFS/SMB는 별도 의미가 있으므로 이 계획은 기존 private 로컬 저장소에 한정한다.
- Apple [flock 매뉴얼](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/flock.2.html)과
  [XNU sys_flock](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_descrip.c)을 확인했다.
  sys_flock은 shared 요청을 F_RDLCK로 바꾸지만 `mac_file_check_lock`도 호출한다.
  O_RDONLY 성공만으로 Seatbelt의 flock 허용을 증명할 수 없다. native 검증을 필수 gate로 둔다.
- [Codex 권한 문서](https://learn.chatgpt.com/docs/permissions#filesystem-permissions)의 읽기/쓰기 분리와 부합한다.
  실제 Codex macOS 세션·Windows native 신규 경로·수정 후 제품 검증은 아직 실행하지 않았다.

## 3. 확정 결정

### D01. 명령 범위와 API 경계

`ModernStore.Read`와 `ReadHeld`의 기존 동작은 유지한다.
`ModernStore.ReadForQuery(ctx)`를 추가하고 `tools.go`의 env list 및 value list/get
두 분기만 연결한다. secret get은 현재 없는 명령이므로 만들지 않는다.
새 플래그, 환경변수, 설정 키, daemon, 외부 의존 패키지는 추가하지 않는다.

`ReadForQuery`는 profile 검증 → `maintenance.AcquireQuery` → defer release →
기존 `ReadHeld` 순서다. 성공 payload, env/key 오류, legacy 해석, secret 마스킹,
`integrity_verified:false`를 그대로 유지한다. `Master`, vault unlock, TTL 갱신을 호출하지 않는다.

### D02. 플랫폼 선택

`maintenance.AcquireQuery(ctx, root)`를 package API로 추가한다.
`query_unix.go`는 `linux || darwin`에서 아래 fast path를 사용한다.
`query_other.go`는 `!linux && !darwin`에서 기존 `AcquireShared`에 위임한다.
Windows의 ACL/parent handle/byte-range locking은 그대로 유지한다.
Windows 샌드박스 조회 개선은 이번 지원 범위가 아니며 문서에도 그렇게 표시한다.

### D03. 생성 없는 잠금 획득

Unix fast path는 `.maintenance`를 `platformfs.CheckDirectory`로 검사한다.
`MkdirAll`, chmod/chown, 생성·교체·truncate·write·sync·transaction 정리를 하지 않는다.
두 잠금 파일은 기존 `platformfs.OpenNoFollow`에 `O_RDONLY`, mode 0을 넘겨 연다.
기존 `openPrivateLock`과 같은 regular-file·private permission 검사를 수행한다.
기존 Unix 검사에 없는 owner UID/hardlink 정책을 이 변경에 섞지 않는다.

`lock.turnstile`에 LOCK_SH를 획득하고 그 handle을 유지한 상태로 `lock`에 LOCK_SH를
획득한 뒤 turnstile을 해제한다. gate handle은 recoveryNeeded 검사와 ReadHeld가
완료될 때까지 유지한다. 경로·inode를 새로 만들거나 /tmp 잠금으로 대체하지 않는다.
writer-intent turnstile의 기존 동작을 보존하며 OS 스케줄러 공정성의 새 보장을 주장하지 않는다.

기존 10ms context-aware polling 로직을 `waitForFileLock(ctx, file, mode)`로 추출해
기존 Unix acquire와 새 acquire가 공유한다. 이 helper는 실패 시 전달받은 handle을 닫고
성공 시 소유권을 caller에 넘긴다. 기존 tryAcquireFileLock 및 Windows polling은 수정하지 않는다.
잠금 획득 직후 context를 재검사하고 취소 시 handle을 닫아 성공을 반환하지 않는다.
새 경로에서 두 번째 open/lock 실패 시 이미 획득한 turnstile도 해제한다.

### D04. fast path와 fallback 상태 기계

| 관찰 상태 | 처리 | 쓰기 불가능할 때 |
| --- | --- | --- |
| 취소/기한 초과 | 즉시 context 오류 | 동일 |
| `.maintenance` 또는 정상 잠금 파일의 ENOENT | 열린 handle 모두 해제 후 기존 AcquireShared 호출 | 초기 준비 필요 상태를 붙인 실패 |
| directory/file 검사·open·flock의 기타 오류 | 즉시 실패. fallback하지 않음 | 원인 보존 |
| 두 공유 잠금 획득, recoveryNeeded=false | 공유 gate를 보유한 release 반환 | 정상 조회 |
| recoveryNeeded 판정 자체 실패 | gate 해제 후 실패 | 손상/권한 오류 보존 |
| recoveryNeeded=true | gate 해제 후 기존 AcquireShared 호출 | 복구 필요 상태를 붙인 실패 |
| fallback 성공 | 기존 AcquireShared가 보유한 shared gate 반환 | 해당 없음 |

ENOENT는 root/lock 준비 필요를 뜻할 뿐 빈 profile을 뜻하지 않는다.
초기화·복구 fallback은 기존 AcquireShared 한 번 호출로 끝낸다. 그 내부의 shared→exclusive→
shared 재검사 loop를 재사용한다. fallback 성공 후 별도 fast path 재진입이나 snapshot 재사용은 없다.
모든 old handle을 닫은 뒤 fallback하므로 같은 프로세스의 자기 잠금 교착을 만들지 않는다.
권한/readonly 오류를 문자열로 추정해 대체 저장소·무잠금 조회로 재시도하지 않는다.
fallback의 복구 필요 판정은 gate를 다시 잡고 재확인하므로 다른 실행자가 먼저 복구해도 안전하다.

### D05. 복구 계약

`recoveryNeeded`, `recoverPending`, `Replace`, 암호화 복구와 before-image 처리는 변경하지 않는다.
v3 ready 보호 marker + 빈 transactions는 정상이다. v1 legacy, v2~v4의 준비/적용/commit
미정리 상태 및 orphan transaction은 기존 판정에 따라 fallback한다.
committed가 이미 완전한 상태여도 정리를 임의 생략하지 않는다.
데이터를 반환한 뒤 복구하거나, 이전 정상본/부분 결과를 성공으로 반환하지 않는다.
쓰기 허용 환경의 기존 자동 초기화·자동 복구는 유지한다.

### D06. 오류와 사용자 안내

`maintenance.QueryPreparationError{State string, Cause error}`를 추가한다.
fallback 실패에만 사용하며 State는 `initialization_required` 또는 `recovery_required`다.
Error 문자열은 고정된 안전한 문자열, Unwrap은 Cause를 반환한다. 경로를 직렬화하지 않는다.
context 오류는 wrapper를 붙이지 않고 그대로 반환한다.

values의 query 전용 오류 변환은 기존 VaultError를 먼저 호출한다.
top-level `storage_error`/exit 3 및 취소·기한 초과 코드를 유지한다.
원래 OS 오류·reason·innermost stage를 유지하고 details의 `state`만 위 상태로 보충한다.
`protocol.Public`는 기존 allowlist로 이를 `facts.state`에 노출한다. protocol 12/envelope 1 유지.
fallback은 파일 생성 또는 복구 일부를 수행했을 수 있으므로 실패의 `effects`는 `unknown`,
`retry`는 `after_dependency_change`로 지정한다. 순수 fast-path 실패는 기존 오류 의미를 유지한다.

remedy는 해당 원 명령을 정상 쓰기 권한이 있는 실행에서 다시 실행한 뒤 조회를 재시도하라는
안내다. argv는 기존 `variable list --profile PROFILE`(선택 env 포함)로 고정하고 effect는
`value_write`로 표시한다. env list 실패에는 env 선택 없이 사용한다. 비밀값이나 사용자 경로를
포함하지 않는다. 존재하지 않는 recover 명령·vault 초기화·샌드박스 전체 해제를 권하지 않는다.
remedy는 안내이며 자동 실행하거나 권한을 올리지 않는다.

### D07. 저장 형식·호환성과 배포

데이터 형식, 잠금 이름·권한·내용·수명, canonical profile identity, request replay,
CLI schema와 성공 응답은 바뀌지 않는다. 데이터 migration과 버전 bump는 필요 없다.
기존 binary의 O_RDWR reader/writer와 신규 O_RDONLY reader를 같은 잠금 파일에 대해 검증한다.
rollback은 이전 CLI 사용으로 충분하며 데이터 변환·잠금 삭제는 수행하지 않는다.
읽기만 가능한 파일에 대한 writer의 기존 O_RDWR 요구는 유지한다.

### D08. 공통 fixture 경로 계약 (사용자 추가 요구, 2026-10-23)

반복되는 macOS `/tmp`·`/var` 별칭 오류는 fixture 경계에서 해결한다.
제품의 `internal/paths.canonicalIsolation`과 no-follow 검사, 기존 세션의 path identity는
변경하지 않는다. 테스트 호출부마다 독립적인 `.resolve()`/EvalSymlinks를 추가하지 않는다.

- Go는 표준 라이브러리만 쓰는 `internal/testfs`의 TempDir/CanonicalDir로 새 fixture의
  실제 경로를 선택한다. 기존 package별 privateTempDir는 이 helper를 사용하고 각자의
  private mode/Windows ACL 설정은 그대로 유지한다. platformfs도 순환 import 없이 사용한다.
- Python은 `scripts/fixture_paths.py`에 새 fixture 경로·임시 root 생성·기존 session 경로
  선택을 모은다. verify runner/sandbox/proxy/handoff/network와 vault cleanup이 재사용한다.
- Windows의 기존 session endpoint는 caller의 선택 경로 표기가 identity에 들어가므로
  새 fixture용 canonicalize와 분리한다. 기존 vault cleanup의 Windows abspath 계약을 보존한다.
- OS별 `/tmp` 리터럴을 기대값으로 쓰지 않는다. 모든 native OS에서 임의 alias를
  만들어 physical root와 미생성 suffix가 같게 선택되는지, session 경로 예외가 유지되는지 검증한다.
- 공통 helper 검증을 프로젝트 check와 Unix sandbox gate에 연결한다. 새 helper 적용으로
  바뀐 test harness만 재검증하고 제품의 symlink 거절 정책을 완화하지 않는다.

## 4. 영향 범위와 유지할 경계

| 영역/호출부 | 직접 변경 | 검증 또는 유지할 이유 |
| --- | --- | --- |
| maintenance rwlock Unix 공통 polling | helper 추출 | 기존 shared/exclusive·취소·writer-intent·handle 수명 회귀 위험 |
| maintenance 새 AcquireQuery | 추가 | 기존 두 파일 사용·복구 전 차단·fallback 해제 순서 |
| values ModernStore query wrapper | 추가 | ReadHeld는 재사용, Read/Inspect/Apply는 유지 |
| CLI tools env/value 조회 | 두 호출부 교체 | var/sec 별칭, env 옵션, stdout/stderr·exit·secret 비노출 |
| tools.go set 사전 검사·import.go | 변경 없음 | Read를 공유하는 mutation 준비 흐름까지 바꾸지 않음 |
| profiles catalog/detail/diff | 변경 없음 | catalog의 외부 gate 아래 ReadHeld 호출. 별도 조회 범위 |
| tasks store/query/cache_stamp | 변경 없음 | data-root와 profile별 잠금, journal 복구 별도 계약. task list는 여전히 쓰기 필요 가능 |
| values Inspect/inspectionProfile·bundle·request/input inspect | 변경 없음 | legacy 변환·인증·revision·키 접근 등 별도 계약 |
| execution CaptureCurrent/Public/Vault, legacy Store.Read | 변경 없음 | launch ticket·migration·세션과 실행 snapshot의 보장 유지 |
| values write/migration/manage | 변경 없음 | shared gate를 잡아도 쓰는 작업 존재. AcquireShared 일괄 치환 금지 |
| vault store/access/launch/delivery/public_launch | 변경 없음 | 키 세션·launch/slot 잠금·authority 유지 |
| valuehandoff prepare/deliver/runner/approval/inspect/summary/store | 변경 없음 | 승인·암호화 불변 입력·전달 receipt/slot 계약 유지 |
| backup/vault·profiletransfer·cleanup | 변경 없음 | 기존 복구/교체 및 snapshot 경계 유지 |
| dashboard server/manage | 변경 없음 | 관리 조회는 ModernStore.Inspect 사용, ReadForQuery로 전환하지 않음 |
| completion_dynamic·CompletionNames | 변경 없음 | 기존 bounded lockless completion은 정상 조회 대안이 아님 |
| Windows rwlock/platformfs | 변경 없음 | 기존 native 저장·ACL·handle 정책 유지 |
| protocol/storageerr/configcontract | 원칙적으로 변경 없음 | 기존 state/effects allowlist 사용. 계약 테스트로 확인 |
| scripts/verify/Unix CI | sandbox scenario 추가 | chmod 대신 OS 차단과 packaged 명령 동작 증거 확보 |
| internal/testfs·package별 privateTempDir·scripts/fixture_paths | fixture 경로 경계 공통화 | physical 경로 선택·Windows session identity 예외·native alias 회귀 |

AcquireShared/LockShared의 production 호출은 위 모든 영역을 검색해 분류했다.
읽기라는 이름만으로 mutation·migration·session 권한을 줄이는 변경은 하지 않는다.
후속 구현 diff에서 허용 파일 외의 호출부 변경이 생기면 범위를 다시 검토한다.

## 5. 구현 단위와 의존성

각 단위는 검증 완료 전 다음 단위의 완료 근거로 계산하지 않는다. 계획 등록 단계의 기존
테스트 통과는 수정 후 결과가 아니다. Task claim/context 및 code basis는 구현 세션에서 생성한다.

### T01 — native 잠금·sandbox 검증 harness

- 경로: `verify/sandbox_query_fixture.py`, `verify/test_sandbox_query_fixture.py`,
  `verify/probes/sandbox_query_lock.go`, `.github/workflows/unix-platform.yml`.
- Go probe는 `//go:build linux || darwin`과 package main으로 제한한다. 기존
  `native_windows.go`의 Windows main과 중복되지 않게 하고, Unix의 `go list ./...`
  신규 package 포함과 scripts/project.py lane 분류를 확인한다.
- 로컬 임시 fixture에서 old O_RDWR writer와 O_RDONLY reader의 상호 차단·해제·취소 검사.
- Linux는 bwrap root ro-bind + 별도 writable scratch로 강제. writer는 밖에서 같은 원본 파일 사용.
- macOS는 sandbox-exec의 allow default + fixture data subtree에 대한 file-write* deny로 시작한다.
  fixture 실제 경로는 resolve하여 /var→/private 별칭을 맞추고 경로 문자열은 SBPL 형식으로 이스케이프한다.
  시작 전 O_RDWR/CREATE·write·rename·unlink 음성 대조가 차단되는지 확인한다.
- parent와 child는 pipe로 준비/획득/해제를 전달하고 monotonic deadline을 쓴다. sleep만으로 순서를 추정하지 않는다.
- CLI/Go test subprocess는 빈 전용 home/isolated root를 쓰고 사용자 vault·keychain을 만지지 않는다.
- macOS에서 read-only flock이 정책상 차단되면 T01 실패. 잠금 우회나 broker로 자동 전환하지 않는다.
  현재 고정 설계로 목표가 충족되지 않았다는 명시적 blocker로 보고한다.
- 완료: Linux와 macOS native 양쪽에서 강제 차단 음성 대조와 old/new 상호 배제 증거.
- 의존성 없음. 실제 OS 기능 검증이므로 제품 정책을 다시 고르는 task가 아니다.

### T02 — maintenance 조회 gate와 회귀 검증

- 경로: `internal/maintenance/rwlock_unix.go`, 새 `query.go`, `query_unix.go`, `query_other.go`,
  `query_unix_test.go`, 기존 maintenance tests.
- 사용자 추가 요구 D08에 따라 `internal/testfs`, 기존 privateTempDir wrappers,
  `scripts/fixture_paths.py`, 관련 Python fixture consumers/tests를 함께 정리한다.
  기존 정규화 조각을 공통 경계로 옮기고 OS와 무관한 alias 회귀를 프로젝트 check에 연결한다.
- D02~D05 알고리즘과 QueryPreparationError 구현. production failpoint/env는 추가하지 않는다.
- fixture 상태: 정상 public/보호 root, 누락 root/디렉터리/각 lock, invalid permissions,
  symlink/FIFO/directory, 복구 pointer 손상, orphan, v1/v2/v4 prepared/applying/committed 및 v3 ready.
- 권한 있는 fallback이 기존 초기화·복구를 완료하고 shared gate로 읽기까지 보호하는지 확인한다.
- writer-intent, concurrent readers, second-lock 실패, context 전/대기/획득 직후 취소,
  recovery 재검사, subprocess 종료 후 잠금 해제를 검사한다.
- 완료: targeted maintenance race + T01 harness가 AcquireQuery를 사용한 상태로 통과.
- 의존: T01. Windows 구현은 기존 AcquireShared 위임 테스트와 native 기존 suite로 확인한다.

### T03 — values·CLI 조회 분기와 오류 계약

- 경로: 새 `internal/values/query.go`, `query_test.go`, `internal/cli/tools.go`,
  새 `internal/cli/sandbox_query_test.go`, 기존 storage/diagnostic/output tests.
- ReadForQuery와 D06 오류 매핑, env/value 조회 두 분기만 교체.
- legacy·canonical public·locked/unlocked protected store에서 동일 조회 결과 확인.
  존재하지 않는 profile은 gate 정상 확보 후 기존 빈 결과/기존 key/env 오류를 유지한다.
- Read/ReadHeld·set stdin/file env 사전 검사·import dry-run·guarded mutation·Inspect·
  실행 snapshot이 기존 gate를 계속 사용하는 회귀 검사. alias는 같은 결과를 내야 한다.
- 초기화/복구 fallback 오류의 state, 원래 OS 코드, reason, stage, exit, effects,
  retry, remedy effect 검증. secret·절대 경로·key/session material이 JSON에 없어야 한다.
- 완료: values/cli/protocol/storageerr/profilekey race 통과, 성공 schema/protocol 불변.
- 의존: T02.

### T04 — 설치 제품 sandbox 시나리오와 Unix CI

- 경로: 새 `verify/scenarios/sandbox_queries.py`, T01 harness,
  `verify/run.py`의 자동 scenario discovery 및 tests, `.github/workflows/unix-platform.yml`.
- `verify/run.py`의 기존 릴리스 설치 구조를 재사용한다. 시나리오 이름은 `sandbox_queries`로 고정.
- 밖에서 CLI로 public/legacy/protected·env fixture 준비 후 data subtree를 ro로 고정한다.
  모든 scoped 명령/별칭 성공, 변수값·메타데이터·빈값·env 오류·비밀 비노출을 검사한다.
- fixture 전체 inventory(bytes/hash, mode, inode, mtime)를 전후 비교한다. atime은 제외한다.
  시작 전 실제 쓰기 open/생성/rename/unlink가 거부되는 음성 대조를 실행한다.
- 미완료 transaction·orphan·누락 잠금 각각에서 stdout 비어 있음, 실패 state·OS cause 확인.
  밖에서 같은 원 명령으로 복구/초기화 후 안에서 정상 조회가 되는 왕복 검증.
- 별도 outside writer/restore와 query 동시 실행. 허용된 이전/이후 완성 snapshot만 관찰하고
  잠금 중인 writer에는 조회가 대기/timeout한다. 실제 crash matrix는 maintenance fixture 재사용.
- Unix CI에 전용 sandbox-query job을 추가한다. macos-15와 ubuntu-24.04 native 필수.
  Linux job은 bwrap 설치, user namespace 제한이 있으면 fixture 한정 sudo wrapper를 사용한다.
  namespace 안 root 역할은 invoking UID/GID로 맞추고 mount 대상은 fixture에 한정한다.
  launcher preflight 실패/도구 부재는 필수 CI에서 skip 성공으로 처리하지 않는다.
- OS/kernel/arch/filesystem, binary version+commit+hash, baseline/candidate 출력, 강제 차단 대조,
  source commit, 실행 명령, 종료 상태를 artifact로 보존한다. credential·개인 데이터는 제외한다.
- 완료: macOS/Linux native packaged gate 통과. 원래 보고한 실제 Codex macOS 세션에서도
  같은 binary로 조회 성공을 확인해야 Codex 환경 검증 완료라고 표시한다. 그 증거 전에는
  Seatbelt 일반 정책 통과와 Codex 실제 세션 결과를 구분한다.
- 의존: T01, T03.

### T05 — 문서·운영 안내·전체 영향 리뷰

- 경로: `docs/storage-design.md`, `docs/diagnostic-contract.md`, `docs/cli-contract.md`,
  `docs/secret-protection.md`, `docs/windows.md`, `verify/README.md`,
  `skills/devtools/references/execution.md`, 이 계획의 진행/증거 부분.
- 정상 기존 저장소 한정 지원, 대상 명령, 초기화/복구와 read denial 구분,
  Windows/다른 조회 명령의 현 계약, side effect 가능 fallback, 안전한 재시도 안내 기록.
- 4절 모든 호출부를 diff 기준으로 재점검. generic AcquireShared 의미, on-disk 잠금 객체,
  protected marker, replay·launch/session·dashboard·task 경계가 바뀌지 않았는지 확인한다.
- 전체 `check:all`, Windows amd64/arm64 native 기존 maintenance/values/cli tests,
  installed 호환 시나리오 및 T04 native gate 결과를 같은 candidate source 기준으로 확인한다.
- 완료: 모든 acceptance에 current validation 존재, 기능 미완성/OS skip을 통과로 계산하지 않음.
  구현 완료 기록과 실제 Codex 세션 미확인 상태를 구분한다. commit/PR/release는 별도 사용자 범위다.
- 의존: T02, T03, T04.

## 6. 필수 검증 행렬

| ID | 검증 | 기대 결과 | 소유 task |
| --- | --- | --- | --- |
| V01 | native O_RDONLY flock와 실제 write deny 음성 대조 | old/new 양방향 차단, 종료 후 해제 | T01 |
| V02 | maintenance 상태 행렬 및 공통 fixture 경로/alias 회귀 | exclusion·복구 유지, handle 누수 없음, OS별 경로 계약 일치 | T02 |
| V03 | values/CLI 출력·오류·alias·기존 mutation callers | protocol12/envelope1 유지, secret 비노출, fallback 정보 보존 | T03 |
| V04 | installed sandbox_queries on macOS/Linux | 정상 조회 성공·쓰기 차단·복구 왕복·동시성 통과 | T04 |
| V05 | 문서·전체 호출부 리뷰·전체/Windows 회귀 | 모든 scope 및 accepted exclusion 일치 | T05 |
| V06 | workstream 최종 integration | V01~V05 current + 실제 Codex 결과의 확인/미확인 명시 | workstream |

수정 후 최소 명령:

```sh
go test -race ./internal/maintenance ./internal/values ./internal/profilekey ./internal/cli ./internal/protocol ./internal/storageerr
python3 -B -m unittest discover -s verify -p 'test_sandbox_query_fixture.py'
python3 -B verify/run.py sandbox_queries storage_migration profiles vault secret_mutations completion
./scripts/project check
```

Windows는 native 환경에서 기존 프로젝트 lane/CI를 사용한다. cross compile은 빌드 증거로만
기록하며 ACL·잠금 동작 검증을 대신하지 않는다. 네트워크 provider live 검증은 변경 경로가
없으므로 새로 요구하지 않는다. 성능 개선 수치나 benchmark 목표는 이번 요구가 아니다.

## 7. 확정하지 않은 사실과 실행 차단 조건

제품/API/지원 범위/오류/복구/fallback/배포 방식의 미결정 항목은 없다.
아직 없는 것은 native macOS·실제 Codex·구현 후 테스트 증거다. 이는 미리 통과를 약속할
결정사항이 아니라 필수 검증 대상이다. T01 native 실패는 T02를 차단하고, T04 실패는 최종
완료를 차단한다. native 기능이 예상과 다르면 이 계획의 해당 근거를 갱신한 뒤 범위를
재검토하며, 구현자가 임의로 보장을 낮추거나 별도 서비스를 도입하지 않는다.

이번 조사에서는 제품 파일을 수정하지 않고 이 계획 문서와 devtools 정의만 기록한다.
기존 데이터·권한·vault 설정은 변경하지 않는다. devtools 기록 자체는 기존 task journal에
정상적인 계획 추가를 수행하며 조회의 잠금 제한 때문에 승인된 외부 실행을 사용한다.
