# Task 샌드박스 조회 개선 계획 — GitHub #9

작성: 2026-10-29 (Asia/Seoul).
기준 소스: `f8c22ac3674a14c8d57ed919e866fc104d6d7b5f`, CLI protocol 19.
상태: 2026-10-29 후속 요청으로 구현·작업 claim·단위별 commit·최종 리뷰루프를 진행한다.
push·CI dispatch·릴리스·사용자 CLI 교체는 별도 요청 범위다.
2026-10-29 계획 리뷰 수정: 캐시 경로 경계·정리 판독·준비 안내·native 정책/구버전 증거·검증 소유권을 보완했다.
대상 profile: `devtools`. Workstream: `74903b6d-3154-4d82-8dc3-e43ddc700e3e`.
제안: https://github.com/jinyongp/devtools/issues/9

## 1. 목표, 근거와 선행 결정

정상적으로 초기화된 private 로컬 task 저장소를 macOS/Linux/WSL의 읽기 전용 실행 환경에서 조회한다.
전역 maintenance와 프로필 잠금의 쓰기 모드 open, 생성, 교체 없이 기존 writer와 같은 객체로 일관성을 유지한다.
task 데이터와 잠금은 읽기 전용으로 두며, 페이지/graph 스냅샷을 새로 만들 때에만 선택한 private 캐시에 쓴다.
캐시까지 읽기 전용이면 캐시가 필요 없는 조회와 기존 cursor 조회는 지원하고, 새 스냅샷이 필요한 조회는 명확히 실패한다.
사용자 전역 저장소의 쓰기 허용 없이 전체 페이지를 조회하려면 허용된 임시 경로를 명시적으로 선택할 수 있어야 한다.

선행 workstream `9bb41be8-11bd-45b2-86fb-59092b6aad5e`는 done/current이며 values 조회만 지원했다.
`docs/sandbox-query-plan.md`의 tasks 제외는 그 작업의 의도된 경계였다. 완료 이력·spec·plan은 수정하지 않는다.
이번 요청은 그 경계를 확장하는 별도 후속 계획이다.
전체 workstream 17개와 task 165개를 끝 페이지까지 확인했다. 같은 목표의 미완료 task는 확인되지 않았다.
기존 query cache chunk 최적화 작업 `8eb3cf2e-c4d5-449e-8354-34f61b32064c`의 bounded continuation을 보존한다.

조사에서 직접 확인한 내용:

- 공개 CLI의 task/workstream/validation 조회 22개는 `internal/cli/tasks.go`에서 `Store.Query`로 들어간다.
  `Store.Read/ReadCurrent`만 바꾸면 보고된 공개 명령은 해결되지 않는다.
- Query는 전역 `AcquireShared`와 프로필 `LockShared`를 사용한다.
  Unix 일반 잠금은 shared여도 `O_CREATE|O_RDWR`로 연다.
- Query는 조회 전에 `pruneQueries`를 호출한다. page는 limit 초과 시 indexed snapshot을 쓰고,
  truncated tree는 graph snapshot을 쓴다. page의 별도 정리는 오래된 모든 `.json`을 대상으로 한다.
- cursor continuation은 Query 초반에 캐시만 읽고 반환한다. 현재 task 잠금이나 active WAL이 없어도
  기존 revision을 읽는 계약이 테스트로 고정돼 있다.
- Linux/WSL bubblewrap 실제 read-only mount에서 source-built CLI의 task list/current가
  `storage_error / read_only / acquire_lock / EROFS(30)`로 실패했다.
  기존 잠금 4개는 O_RDONLY+LOCK_SH 성공, O_RDWR 실패했다.
- 기존 잠금 파일만 writable bind하면 작은 list/current는 성공했으나, 2개 task를 limit 1로 조회하면
  `query_cache` 오류가 났다. 캐시도 writable bind하면 성공했고 task 데이터 inventory는 동일했다.
- 기존 native probe는 write/open/create/rename/unlink 차단, reader/writer 양방향 차단,
  concurrent readers, cancellation, close 해제를 확인했다.
  maintenance/tasks/values 선별 Go 검증도 통과했다. 모두 위 소스와 Linux/WSL 조건의 조사 근거다.
- macOS Codex 원래 EPERM 환경과 개선 구현 후 CLI 성공은 미검증이다.
  기존 workstream의 macOS 통과 기록은 이번 새 task 프로필 경로의 통과 근거로 사용하지 않는다.
- 설치 CLI는 0.36.3 / protocol 18이다. 계획 등록은 그 CLI의 실제 schema를 확인해 사용한다.
  조사용 빌드는 source protocol 19이며 사용자 설치를 교체하지 않았다.

## 2. 이번 계획에서 선택한 동작

### D01. 공개 조회 범위

`Store.Query`를 통하는 다음 22개 명령을 대상으로 한다.

- task: list, show, next, current, context, history, impact, tree, export, checkpoint list.
- task workstream: list, show, context, history, impact, tree, export, check, spec show, plan show.
- task validation: list, show.

같은 명령으로 정규화되는 기존 alias도 같은 계약을 적용한다.
filter, limit(기본 50/상한 200), graph depth, directory identity, historical revision과 출력 내용을 보존한다.
impact의 메모리 내 preview와 공개 task export도 포함한다.
workstream edit --dry-run은 mutation dispatcher에 있으므로 이번 read-only 지원 대상이 아니다.

### D02. 잠금은 조회 경로에만 추가

전역은 기존 `maintenance.AcquireQuery`를 재사용한다.
프로필에는 `maintenance.LockQueryShared(ctx, gatePath)`와 같은 조회 전용 진입점을 추가한다.
Linux/Darwin에서는 기존 directory/file 검사와 O_NOFOLLOW/O_NONBLOCK을 유지하고,
생성 없는 O_RDONLY shared turnstile → shared gate 순서를 사용한다.
두 번째 open/lock 실패, 취소와 deadline에는 먼저 얻은 handle까지 해제한다.
성공한 gate는 해당 task 데이터를 모두 읽을 때까지 유지한다.

프로필 lock/directory ENOENT에만, 잡은 조회 handle을 모두 해제한 뒤 기존 LockShared 초기화 경로를 사용한다.
실패는 QueryPreparationError(initialization_required, original cause)로 표시한다.
다른 권한·형식·symlink 오류를 초기화나 무잠금 fallback으로 바꾸지 않는다.
호출자는 전역 → 프로필 순서를 지키고, 프로필 잠금을 보유한 채 전역 잠금 승격을 시도하지 않는다.

Windows/기타 OS의 새 진입점은 기존 LockShared로 위임한다.
AcquireShared/LockShared의 기존 동작, exclusive writer, TryLockExclusive는 일괄 변경하지 않는다.
공통 polling helper 재사용 외에 다른 저장 영역의 잠금 정책을 바꾸지 않는다.

### D03. 전역 복구와 task read 계약은 구분

전역 초기화·복구는 AcquireQuery의 기존 상태 기계와 writable fallback을 그대로 사용한다.
다른 profile의 global pending transaction도 현재 query를 막는다.
v3 ready 보호 marker는 기존처럼 정상으로 해석한다.
legacy/prepared/partial/committed/orphan 상태는 기존 recoveryNeeded 판정을 따른다.

task v3의 corrupt/stale materialized snapshot은 현재처럼 유효한 WAL에서 메모리 재구성할 수 있다.
불완전한 WAL tail의 기존 committed-prefix 읽기 의미도 보존한다.
이 경우 query가 checkpoint, WAL trim, pending receipt finalize, generation GC를 수행하지 않는다.
task pending.json의 존재를 전역 복구 필요와 동일시하거나 기존 유효한 read를 새로 막지 않는다.
손상된 authoritative WAL/identity/head의 기존 거절과 검증 수준을 유지한다.

### D04. 명시적 task query 캐시 위치

모든 대상 CLI query에 `--query-cache-dir PATH`를 추가한다.
PATH는 절대 경로인 task snapshot 전용 private 디렉터리다. 상대 경로와 빈 값은 invalid_argument로 거절한다.
빈 값·상대 경로 같은 정적 입력 오류는 profile 해석이나 저장소 잠금 전에 거절한다.
명시 경로와 선택 data root의 동일·상위·하위 관계는 경로 구성요소 단위로 거절한다. tasks/profiles/.maintenance도 포함된다.
이 정적 비교는 정규화된 경로와 Windows 대소문자·volume 규칙으로 처리하며 filesystem을 열지 않는다.
새 snapshot을 쓰거나 정리하기 전에는 data root의 기존 ancestor alias도 고려한 실제 경로/디렉터리 identity를 확인한다.
cache의 symlink/reparse를 해석해 허용하는 완화는 하지 않는다.
이 물리 중첩 검사를 cursor read에 추가해 현재 data root의 존재·읽기 권한을 새 선행 조건으로 만들지 않는다.
존재하지 않는 leaf를 검사하려고 디렉터리를 먼저 만들지 않는다. 기본 cache 선택과 다른 소비자의 Store.Cache 주입은 바꾸지 않는다.
생성 가능한 미존재 leaf는 실제 snapshot 생성 시에만 기존 private I/O로 준비한다.
기존 디렉터리의 권한을 자동 교정하지 않는다. symlink·공개 권한·일반 파일 경로는 기존 검사로 거절한다.
옵션 미지정 시 기존 CLI cache/task-queries 경로를 유지한다.
데이터 root, profile, config/runtime, values cache는 바꾸지 않는다. 자동 /tmp fallback도 만들지 않는다.

Store.Cache의 기존 주입점을 이용하고 새 전역 환경변수나 daemon은 추가하지 않는다.
조회 전용 option만 등록한다. mutation에는 옵션을 받지 않는다.
cursor를 잇는 각 CLI 실행은 같은 선택 디렉터리를 사용한다.
캐시 위치는 논리 query fingerprint에서 제외한다. 동일 디렉터리를 default/명시 선택해도
같은 논리 query의 cursor를 사용할 수 있어야 한다.
다른 디렉터리에서 cursor를 찾지 못하면 cursor_invalid이며 기본 캐시로 몰래 재탐색하지 않는다.

캐시가 필요 없는 query와 기존 cursor continuation은 디렉터리를 생성하거나 정리하지 않는다.
경로 문법과 정적 data 중첩 검사는 항상 적용하지만, 캐시 filesystem 접근은 필요한 query까지 지연한다.
명시 디렉터리가 없어도 작은 query는 성공할 수 있다. unsafe한 명시 경로는 실제 사용 전 검증한다.
cache 접근 오류가 데이터 조회에 필요하지 않은 단계에서 정상 query를 막지 않도록 준비를 지연한다.
명시 cache의 list/graph cursor read와 list/graph snapshot write 모두 동일한 private root/parent guard를 사용한다.
Unix OpenPrivate는 최종 파일만 no-follow로 열고 WritePrivate는 parent guard를 제공하지 않으므로,
이들 호출만으로 새 옵션의 부모 symlink·권한 검사를 충족했다고 보지 않는다.
cursor read는 create=false로 root를 열어 검사하며, 실제 write도 기존 platformfs.Root 경계로 수행한다.
정상 cursor는 현재 data root가 이동·부재·접근 거부 상태여도 선택 cache만으로 읽는다.
명시 cache의 parent guard는 검사하되 data root의 stat, 전역/프로필 잠금, WAL 읽기는 추가하지 않는다.
새 page/graph snapshot은 그 디렉터리가 writable일 때만 생성한다.
불가능하면 storage_error/resource=query_cache로 실패하고 cursor를 누락한 불완전 성공으로 바꾸지 않는다.

Linux의 XDG_CACHE_HOME은 기존 대안이지만 전체 cache 선택에 영향을 주고 Darwin 경로는 이를 사용하지 않는다.
조회 전용 옵션이 이 use case의 더 좁은 cross-platform 선택이다.
Windows에서도 명시 cache 선택과 기존 ACL 검증은 동작하되 read-only task lock 지원은 약속하지 않는다.

### D05. 캐시 수명과 정리

Query 진입의 unconditional pruneQueries와 page의 포괄적 *.json 삭제를 제거한다.
단일 조회·cursor read에서는 만료된 파일이 있어도 삭제를 시도하지 않는다.
새 스냅샷 publication에 성공한 뒤에만 같은 선택 캐시 디렉터리를 best-effort 정리한다.
정리 실패가 이미 게시된 cursor의 성공을 뒤집지 않는다. graph/page는 같은 좁은 정리 함수를 사용한다.

정리는 UUID.json 이름, private regular-file/no-follow 검사, 인식 가능한 query/graph snapshot 형식과
기록된 Expires 만료를 모두 만족하는 캐시만 대상으로 한다.
정리용 식별·만료 판독은 기존 queryCacheHeader(64 KiB) 이내의 metadata와 고정 크기 index 접근으로 제한한다.
legacy/graph JSON도 앞부분의 형식·projection·profile/kind 또는 fingerprint·expiry를 판별하고,
판별 정보가 그 한도 안에 없거나 모호하면 삭제하지 않는다. items/nodes/history 전체를 decode해 정리하지 않는다.
indexed cache의 header token도 파일명의 UUID와 일치해야 한다. Expires만 있는 UUID JSON은 소유 cache로 판정하지 않는다.
현재 QueryCacheExpiry의 JSON 분기는 Expires만 확인하며 최대 128 MiB를 decode한다.
이를 소유권 판정이나 조회 후 정리 함수로 그대로 사용하지 않는다. 기존 cleanup API의 보관 동작은 별도로 보존한다.
디렉터리 순회 중에는 context 취소를 확인해 best-effort 정리를 멈추고, 이미 게시된 cursor 결과는 유지한다.
mtime만 오래된 live snapshot, non-UUID JSON, 알 수 없는 JSON, symlink, 다른 파일은 보존한다.
새 보안 정책·임의 권한 교정·임의 경로 재귀 삭제를 추가하지 않는다.

default cache는 기존 cleanup 기능의 대상이다. 명시 custom cache를 global registry에 등록하지 않는다.
custom cache는 publication 후 만료 정리와 caller의 임시 디렉터리 수명으로 관리한다.
read-only로만 재사용하면 만료 파일이 남을 수 있지만 TTL 검사는 계속 적용한다.
30분 TTL, list/graph의 고정 revision, graph node/edge/run/completion view와 bounded chunk read는 유지한다.
추가 파일 포맷이나 cache migration은 만들지 않는다.

### D06. 오류와 복구 안내

task query 전용 변환에서 QueryPreparationError를 해석한다.
기존 storage_error/exit 1, canceled/130, deadline_exceeded/3와 원래 OS reason/stage/resource를 유지한다.
fallback 실패에 facts.state=initialization_required/recovery_required를 추가하고
일부 준비/복구 쓰기 가능성이 있으므로 effects=unknown, retry=after_dependency_change를 사용한다.
순수 fast-path 실패에는 일괄적으로 unknown을 붙이지 않는다.

task 준비/복구 안내는 같은 profile/isolated-root의 비페이지 query인 task next를 writable 실행에서 조회하도록 한다.
task list는 51개 이상에서 새 cache 쓰기를 요구하므로 준비 안내로 사용하지 않는다.
task next는 잠금 준비/복구를 수행하는 범위에서만 사용하고, 완료 후 원래 명령·target/filter/dir/cache 선택으로 재시도한다.
기존 values의 variable list remedy를 복사하지 않는다. 초기 준비/복구 가능 행동은 value_write로 표시한다.
cache 오류는 query_cache를 식별하고 쓰기 가능한 query-cache-dir 선택을 안내한다.
명시 cache 위치, query의 실제 target/filter/dir 및 isolation 선택은 재시도 계약에 맞게 유지한다.
기본 private 경로·OS 원문·task context·비밀을 facts나 임의 메시지로 노출하지 않는다.
remedy는 안내이며 자동 권한 확대, 다른 저장소 선택, mutation 재시도 실행을 하지 않는다.

### D07. 호환성과 공개 버전

새 CLI 입력을 식별하도록 현재 기준 protocol 19 → 20으로 올리고 envelope 1은 유지한다.
구현 시 기준이 바뀌었으면 당시 최신 public contract와 충돌하지 않는 다음 버전을 택한다.
schema/help/completion/aliases/직접 소비자의 current-version assertions를 함께 맞춘다.
과거 protocol 기록과 바뀌지 않은 config schema ID를 일괄 치환하지 않는다.
journal/WAL/snapshot 형식, lock 이름/inode/권한, request UUID/fingerprint/receipt는 변경하지 않는다.
기존 binary writer와 새 query reader의 실제 interop을 검증한다.
기존 binary는 위 기준 commit f8c22ac의 수정하지 않은 소스를 별도 임시 경로에서 native build한 CLI로 고정한다.
후보와 baseline을 다른 경로에 두고 commit·binary hash·protocol을 각각 기록한다. 사용자 설치를 덮어쓰지 않는다.
같은 synthetic v3 저장소에서 baseline CLI의 실제 task 변경과 candidate 조회를 교차 실행하고,
결과가 완전한 이전/이후 revision이며 이후 baseline 조회·변경도 유지되는지 확인한다.
O_RDWR flock probe는 잠금 기법의 증거다. 실제 구버전 CLI의 저장 형식·mutation 호환 증거를 대체하지 않는다.
rollback은 이전 CLI로 돌아가 새 옵션을 생략하는 방식이며 데이터 변환·lock 삭제는 필요 없다.

## 3. 영향 범위와 제외

| 영역 | 작업 | 지켜야 할 경계 |
| --- | --- | --- |
| maintenance/query*.go, rwlock Unix helper | profile query primitive 추가와 시험 | 일반 shared/exclusive·전역 복구·Windows byte-range 경로 유지 |
| tasks/query.go, query_cursor.go, query_cache.go, graph snapshot | Query 잠금·진단·lazy cache·안전한 정리 | cursor fast path, WAL read semantics, bounded cache 포맷 유지 |
| cli/tasks.go, task schema/diagnostic tests | query-cache-dir, profile별 진단·remedy | mutation dispatcher와 작업 context 권한 유지 |
| protocol 현재 버전과 CLI/schema 소비자 | protocol 20 식별·동작 검증 | envelope·저장 형식·과거 문서 유지 |
| dashboard Query 소비자 | writable 회귀 검증 | CacheStamp/registry/session 및 readonly dashboard 지원은 범위 밖 |
| profiles list/inspect/diff | 직접 소비자 회귀 검증 | 외부 AcquireShared와 ReadCurrentHeld의 기존 동작 유지 |
| Store.Read/ReadCurrent/ReadHeld/Profiles/CacheStamp | 회귀 검증 | 공개 Query와 혼동해 일괄 치환하지 않음 |
| backup/cleanup/profile transfer/mutation/dry-run | 회귀 검증 | ExportSnapshotHeld의 WAL 복구·orphan 정리·보호 계약 유지 |
| native probe/fixture/Unix workflow | 새 task 경로 검증 추가 | 실제 write denial·기존 values gate·fixture path helper 재사용 |
| docs/tasks.md, storage/diagnostic/CLI docs, canonical Skill | 새 지원 범위·사용법·제약 기록 | user-global 설치된 Skill은 자동 교체하지 않음 |

Windows read-only lock 지원, immutable export 서비스, daemon/snapshot 서버, stateless cursor 재설계,
task 실행·정의 기능 추가, 일반 저장소 잠금 전환, 성능 튜닝, macOS 권한 정책 변경은 이번 범위에서 제외한다.

## 4. 작업 단위와 의존성

### T01 — 조회 전용 프로필 잠금과 task 준비 진단을 연결한다

대상: internal/maintenance/query*.go 및 query/rwlock tests;
internal/tasks/query.go, store.go의 query 전용 오류 helper와 관련 tests.
전역 AcquireQuery + profile query shared lock으로 실제 Query를 연결하고 eager cache prune을 없앤다.
D02/D03/D06대로 해제·fallback·task storage 오류를 처리하며 기존 일반 Read API는 유지한다.
완료: 정상 작은 list/current가 쓰기 차단 저장소에서 읽히는 제품 경로,
동일 inode reader/writer·intent·취소·부분 획득 해제, missing lock과 global recovery state 행렬의 Go 증거.
준비 안내는 cache가 읽기 전용이고 task가 51개인 fixture에서도 task next로 성공해야 한다.
T01의 완료는 이 Go/CLI 단위 검증 범위에서 판정한다. 후행 T03 native 결과를 T01 완료의 선행 조건으로 두지 않는다.
수정하는 파일에 기존 사용자 변경이 있으면 보존한다.
의존성: 없음. A01/A02/A03/A06.

### T02 — 명시적 query 캐시와 안전한 수명·CLI 계약을 구현한다

대상: internal/cli/tasks.go, internal/tasks/query*.go 및 graph/cache tests,
internal/protocol/protocol.go와 현재 CLI/schema/version 소비자.
D04/D05/D07을 한 slice로 구현한다. flag·path 검증·lazy 생성·fingerprint·page/graph publication과
범위 제한 cleanup, protocol 식별을 함께 다룬다.
완료: 2-task/limit-1 및 3-node graph continuation이 선택 캐시에만 쓰고 original revision을 유지한다.
default/명시 같은 dir, 다른 dir, TTL, denied/missing/unsafe cache, 무관한 JSON과 live 오래된 mtime,
readonly continuation, mutation flag 거절, 기존 indexed/legacy cache compatibility 검증을 통과한다.
data와 같은/상위/하위 cache 경로 거절과 parent symlink를 통한 list/graph read/write 거절을 검증한다.
명시 cache cursor가 현재 data root 이동·부재·접근 거부에도 성공하며 stat/잠금/WAL에 접근하지 않는지 확인한다.
판독 bytes를 관찰해 대형 graph/legacy 본문을 정리 과정에서 읽지 않는지 확인하고,
Expires만 가진 UUID JSON과 식별 metadata가 한도를 넘는 파일도 보존한다.
T02 완료는 이 cache/CLI 검사 범위에서 판정한다. 후행 native/Windows/실제 Codex gate는 T03–T05가 소유한다.
의존성: 없음. T01과 같은 파일은 조정하되 파일 공유 자체를 선행 조건으로 만들지 않는다.
A04/A05/A06/A07. T01/T02는 통합 검증 전 각각 릴리스 가능한 상태라고 간주하지 않는다.

### T03 — native task sandbox fixture와 Unix CI 통합 gate를 확장한다

대상: verify/probes/sandbox_query_lock.go, verify/sandbox_query_fixture.py,
verify/scenarios/sandbox_queries.py 또는 재사용 가능한 인접 task scenario,
verify/test_sandbox_query_fixture.py, .github/workflows/unix-platform.yml.
T01/T02 실제 CLI를 설치한 fixture에서 22개 query와 alias/옵션을 필요한 synthetic 데이터로 검증한다.
current에는 실제 claim run을 준비하고 context credential은 로그·artifact에 남기지 않는다.
데이터와 캐시를 disjoint 경로로 두고 RO/RO, RO/RW, writable의 세 조합을 검증한다.
기존 sandbox_command/Sandbox를 확장해 data와 선택 cache의 write-denial/writable 목록을 독립적으로 지정한다.
현재 Darwin policy는 data subpath만 막으므로 RO/RO에는 cache deny 규칙도 추가한다.
Linux는 의도한 writable scratch/cache만 bind하며 cache를 scratch의 하위에 두어 RO 검사가 풀리지 않게 한다.
각 OS·조합에서 query 전에 data와 cache 각각의 write-open/create 음성·양성 대조를 실행한다.
RO cache의 delete/rename도 차단됨을 확인한다. 기존 cursor는 writable 준비 단계에서 만든 뒤 RO로 전환한다.
query가 필요하지 않은 cache를 무시해서 성공한 경우만으로 cache 쓰기 차단이 확인됐다고 보지 않는다.
global/profile gate·turnstile 각각의 contention/취소와 outside legacy writer를 확인한다.
잠금 probe 외에 D07의 baseline/candidate 실제 CLI 교차 변경·조회 검증을 별도로 수행한다.
기존 values 시나리오와 protected ready marker 검사를 보존한다.
완료: macOS/Linux의 실제 OS write denial 음성 대조, CLI 결과·inode/hash/mode inventory,
경합/복구 왕복·private path 비노출·fixture cleanup 증거를 commit/binary identity와 함께 보존한다.
쓰기 시도 0은 inventory 동일만으로 입증하지 않는다. Linux는 fixture syscall trace에서
task data/lock 경로의 writable open/create/unlink/rename/mode 변경이 없는지 검사한다.
CI에 필요한 tracing 도구는 테스트 job에만 설치한다.
macOS는 동일 Unix 경로 정적 검토와 native sandbox 결과를 구분하며 syscall trace를 얻지 못하면
그 한계를 기록한다. 실제 Codex 정책 검증은 T05에서 별도 수행한다.
T03 완료는 위 native matrix와 baseline 호환 증거로 판정한다. T05의 실제 Codex 결과를 기다리지 않는다.
의존성: T01, T02. A01–A08.

### T04 — writable·Windows·직접 소비자 회귀와 문서·Skill을 맞춘다

대상: tasks/maintenance/cli/profile/dashboard/backup/cleanup tests, 영향받은 current-version fixture,
docs/tasks.md, docs/storage-design.md, docs/diagnostic-contract.md, docs/cli-output.md,
skills/devtools/references/tasks.md 및 execution.md.
새 옵션 예시, 같은 cache dir의 continuation, read-only 범위와 준비/복구 remedy,
custom cache 수명, Windows/다른 조회의 제외를 명시한다.
완료: 기존 writable 초기화·복구·변경·request replay, profile summary/diff,
dashboard CacheStamp/Query, backup/cleanup/export 및 Windows lock/ACL 동작이 유지된다.
현재 버전 assertion 변경만으로 기존 의미가 달라졌다고 주장하지 않는다.
Go target suite와 relevant installed 시나리오를 실행하고 Windows native 결과를 별도 기록한다.
의존성: T01, T02. A03/A06/A07/A08.
문서는 T05 native 결과가 나오기 전까지 지원 목표와 실제 검증 상태를 분리한다.
T04 완료는 자체 writable/Windows/소비자 검사와 목표·미검증 상태가 정확한 문서로 판정한다.
이 단계에서 문서의 macOS Codex 검증 상태를 pass로 올리지 않는다.

### T05 — 전체 영향 리뷰와 실제 macOS Codex 증거를 확정한다

대상: T01–T04 전체 diff와 실행 증거, 이 계획의 검증 상태.
각 in-scope query·failure path·직접 소비자와 제외 경계를 다시 대조한다.
실제 macOS Codex read-only 정책에서 초기화된 synthetic 저장소를 읽고,
단일/다중 page·graph 및 지정 private scratch cache의 성공을 확인한다.
Go race 및 project-prescribed check:all, 관련 installed/native 결과를 최종 소스 기준으로 모은다.
완료: A01–A08 근거와 검증 한계, rollback·data migration 불필요성을 자체 설명 가능한 기록으로 남긴다.
계획만 저장한 현재 상태나 baseline 재현을 개선 완료로 표시하지 않는다.
실제 macOS Codex 환경을 확보하지 못하면 해당 gate는 pending이며 모의 결과로 pass/close하지 않는다.
의존성: T03, T04. A01–A08.

## 5. 완료 조건과 검증 행렬

| key | 완료 조건 | 주 검증 |
| --- | --- | --- |
| A01 | 초기화된 정상 task data/lock을 쓰기 시도 없이 조회 | 작은 list/current/workstream/validation, stale cache가 있는 단일 조회, native RO fixture+trace |
| A02 | 같은 lock 객체·순서·writer intent·취소·deadline·해제 유지 | global/profile gate/turnstile별 old writer interop, concurrent readers, second-open failure |
| A03 | 초기화·전역 복구 분류와 기존 task WAL read 의미 보존 | missing 4 locks/dirs, cross-profile pending, legacy/prepared/partial/committed/orphan/ready, invalid snapshot/valid WAL/incomplete tail |
| A04 | data와 분리된 명시 private cache에만 page/graph snapshot을 쓰고 고정 revision 유지 | data 중첩/alias 거절, list·graph read/write parent guard, limit 1, writer mutation 후 cursor, default/explicit 동일 dir, 다른 dir/TTL |
| A05 | 단일/cursor 조회 무정리와 bounded metadata에 의한 소유 캐시 한정 정리 | RO cache success, publication 후 expired cache, 대형 payload 무판독, expiry-only/unknown JSON·symlink·live-old-mtime 보존 |
| A06 | task·cache·context 오류와 실제 실행 가능한 준비 안내·비노출 계약 유지 | JSON schema/exit/stdout-stderr, state/effects/retry/resource, 51-task/RO cache의 task next 준비, 원 query 재시도·private sentinel |
| A07 | 현재 CLI protocol와 실제 baseline writer·기존 소비자 호환 | protocol 20/envelope 1, schema/help/alias, flag mutation 거절, baseline/candidate 교차 변경·조회, writable/Windows regressions |
| A08 | data/cache 독립 정책 대조와 새 제품 경로의 플랫폼별 실제 증거·문서 일치 | Linux/macOS RO/RO·RO/RW·writable, WSL2, 실제 macOS Codex, final full checks+coverage review |

검증 소유권과 실행 순서:

- V01은 T01의 잠금·복구·준비 안내 단위 검사만, V02는 T02의 cache/CLI 단위 검사만 판정한다.
- V03은 T03의 Linux/macOS native matrix·실제 baseline CLI 호환을, V04는 T04의 writable/Windows·소비자 회귀를 판정한다.
- V05는 T03/T04 완료 뒤 실제 macOS Codex와 최종 full checks·전체 리뷰를 판정한다.
- V06은 workstream의 A01–A08 전체를 최종 통합 판정한다. task의 acceptance_keys는 각 조건에 대한 기여 연결이며,
  각 선행 task가 모든 플랫폼의 최종 증거를 먼저 갖춰야 한다는 의미가 아니다.
- T01/T02 완료에는 후행 V03–V06 결과가 필요하지 않고, T03/T04 완료에는 V05/V06 결과가 필요하지 않다.
  각 task는 자기 required validation의 실제 pass가 필요하다. 후행 검증을 선행에서 pass로 간주하지 않는다.

우선 기존 검사에 새 meaningful 사례를 보강한다. 구현 내부와 같은 기대값을 복제한 테스트는 만들지 않는다.
read 결과는 synthetic fixture의 task/run/edge와 명시된 contract로 검증한다.
각 native/installed 실행에는 OS/kernel, source commit, binary hash/version, data/cache 접근 정책,
성공/거절 결과, inventory를 기록한다. raw task context/secret/개인 저장소는 artifact에서 제외한다.
기존 baseline 성공·실패 관측은 조사 증거이며 위 A01–A08의 candidate 통과로 재사용하지 않는다.

## 6. 운영·기록 경계

필수 제품 결정은 위 D01–D07로 계획에 정리했다. 캐시까지 쓰기 금지인 환경에서 새 고정 cursor를
항상 생성하는 요구는 채택하지 않았다. 정상 데이터 단일 조회와 기존 cursor 읽기는 계속 지원한다.
다른 cache 정책을 선택하면 D04/D05와 T02–T05/A04–A08을 함께 재검토해야 한다.

새 workstream은 draft로 저장한다. task/required validation을 연결하고 구조 check만 수행한다.
실행 claim/activate/완료·passing evidence 등록은 현재 계획 요청의 결과로 수행하지 않는다.
선행 done workstream의 완료 이력과 다른 진행 작업은 유지한다.
제품 실행은 후속 구현 요청의 범위에서 수행하며, 기존 CI dispatch/commit/push 승인은 새 작업에 상속하지 않는다.

## 7. 계획 리뷰에서 보완한 근거

이번 리뷰의 대상은 계획과 devtools 정의다. 아래는 제품 구현 완료나 candidate test pass를 뜻하지 않는다.

| 항목 | 확인한 문제 | 반영 위치 |
| --- | --- | --- |
| F01 | private cache라는 설명만으로 data 중첩이나 list/graph read/write의 parent symlink 검사가 보장되지 않음 | D04, T02, A04/A05, V02 |
| F02 | 기존 QueryCacheExpiry의 expiry-only/full-JSON decode를 재사용하면 무관한 UUID JSON 삭제와 payload 전체 판독 가능 | D05, T02, A05, V02 |
| F03 | writable data + RO cache에서 51-task task list는 query_cache로 실패하지만 task next는 성공함 | D06, T01, A06, V01 |
| F04 | 기존 Darwin sandbox policy는 data만 deny하므로 cache RO/RW를 독립적으로 설정·대조해야 함 | T03, A08, V03 |
| F05 | O_RDWR flock probe만으로는 실제 구버전 CLI의 mutation·저장 호환을 확인할 수 없음 | D07, T03, A07, V03 |
| F06 | V01이 후행 T03 결과를 기다리는 문구는 T03→T01 실행 의존성과 순환할 수 있음 | T01–T04 완료 경계, 검증 소유권, V01–V06 |

F03은 동일 기준 소스의 CLI와 일회용 51-task fixture를 Linux/WSL bwrap에서 실행해 확인했다.
data를 writable bind하고 cache는 RO로 유지했으며 cache inventory가 동일했다.
F04는 실제 macOS 실행이 아니라 기존 policy 생성 결과와 소스의 경로 포함 여부를 확인한 것이다.
F01/F02/F05/F06은 기존 호출부·계획·tracker 의존성을 대조한 정적 근거다.
F01의 2차 검토에서 물리 중첩 검사 자체가 cached cursor를 현재 data root에 다시 결합할 수 있음을 확인했다.
정적 경로 검사는 공통으로, 물리 data identity 검사는 쓰기/정리 전에만 수행하도록 분리하고
data root가 부재한 cached cursor 검증을 T02/V02에 추가했다. 기존 lockless cursor 계약을 유지하기 위한 보완이다.
