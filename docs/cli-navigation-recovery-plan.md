# CLI 탐색·복구 안내·이력 보관 개선 계획

작성일: 2026-11-03 KST. 기준 소스: `46dfba1417e00b793fc2a5d2c3c4864d8ba1b28f`.
설치 CLI: 0.40.0 / protocol 22 / commit unknown. 대상 profile: `devtools`.
Workstream ID: `73d68d31-7851-4df0-9461-53364c3cb7a3`.
상태: 사용자의 후속 "끝까지 작업" 요청으로 구현·검증·통합 리뷰를 진행한다.
등록한 task를 순서대로 claim하고 main에서 작업하며 기존 GitHub Actions 검증에 필요한
commit/push와 dispatch를 포함한다. 실제 사용자 데이터 정리, 전역 설치본 교체와
릴리스 발행은 별도다. 별도 worktree는 만들지 않는다.

## 1. 목표와 확인한 사실

사용자는 실패 뒤 복사한 복구 명령이 다시 실패하고, 현재 실행을 찾는 목록에 과거
실행이 반복되며, 조건을 골라 탐색할 옵션이 부족한 문제를 보고했다. 완료 결과는
실제 실행 가능한 다음 행동, 현재 상태와 이력의 분리, 명시적인 조회 조건,
보존 목적에 맞는 데이터 수명과 검증 가능한 정리다.

- `internal/ports/run_observation.go`의 managed stop 안내는 지원하지 않는
  `--profile`과 유효하지 않은 `REQUEST_UUID`를 함께 출력한다.
- `protocol.Action`은 argv와 입력 설명을 전달하지만 command registry와 문법을
  대조하지 않는다. 기존 `TestReadGuidanceMatchesPublishedRegistry`는 일부 read
  행동만 검사한다. write/recovery 행동과 실제 적용 결과의 검증이 빠졌다.
- installed CLI의 `command add --dry-run -- PROGRAM ARG...` 안내를 임시 설정에
  적용하면 원래 실행 인자를 잃는다. placeholder를 원래 인자로 바꿔도
  `--isolated-root`가 child 구분자 뒤에 붙어 exec 배열에 저장된다. 두 경우 모두 exit 0이다.
- 공개 catalog 198개 중 이름이 list/history/archives/current에 해당하는 조회 20개를
  조사했다. limit 8개, state 2개, since/until 등 기간 선택 0개다. 이 수치는
  복합 응답 안의 모든 collection이나 모든 runtime 분기를 조사했다는 뜻이 아니다.
- 사용자 첨부의 313개 execution ID는 전부 다르다. 표시된 profile·위치·명령으로
  묶으면 49묶음이며 한 묶음에 최대 45개 이력이 있다. Running 1, Stopped 275,
  exited 26, Interrupted 9, Failed 2다. 실제 동시 실행 수·종료 시각·보관 용량은
  첨부만으로 검증할 수 없다. 원본 식별자와 타 repository 경로는 계획에 복사하지 않는다.
- process list는 모든 profile의 미보관 실행 기록을 전부 열거한다. expose list도
  stopped 기록을 기본 목록에 포함한다. task 목록은 기본 완료/취소 제외와 페이지가
  이미 있고, 값 전달 실행은 50개 페이지가 있다. 모든 영역이 같은 결함인 것은 아니다.
- 프로세스 정리는 종료 30일 뒤 명시적 cleanup이며 같은 instance에 살아 있는
  실행이 있으면 과거 기록도 제외한다. 로그는 종료 7일 후 읽기 만료지만 물리 정리는
  별도다. 일부 만료 영수증은 request identity 때문에 정리 후보에서 제외한다.
- `exited`는 정상 process 상태인데 presentation의 상태 사전에 없어 Unrecognized로 나온다.

이번 계획의 조사는 위 소스·공개 schema·합성 fixture에 근거한다. 원본 macOS 저장소,
외부 provider 계정과 전체 실제 데이터의 크기를 검사하거나 수정하지 않았다.

## 2. 기존 작업과 소유 범위

devtools workstream 전체 목록 20건을 조회했고, 다음 관련 계획 5건의 metadata와
계획을 선택 조회했다. 20개 전부의 구현·검증을 다시 리뷰했다는 의미는 아니다.

| 기존 workstream | 이번 계획과의 경계 |
| --- | --- |
| `bd696a33-81fa-4dab-9f61-ca5e76300d49` 표시 계층 | auto/text/json·shell 인용·native stream·스타일을 재사용. 이번에는 조회 의미와 실제로 실행할 행동을 변경한다. |
| `8a6329be-e317-4645-8c53-92b5fc248c63` 목적별 출력 | 기존 계층·스타일·진단 우선순위 유지. 필터·기본 조회 의미·ready 행동은 이번 계약이 추가 기준이다. |
| `93a2408d-c8cc-42ab-83a4-06a027b373bd` 네트워크 | provider·pin·현재 실행 소유권 유지. exposure 조회와 종료 후 metadata 수명만 연결한다. |
| `028e639d-7c15-4d99-92ee-6d526bbffa16` InvocationContext | scope/effect/retry 보존을 이어받고, 실제 argv 계약 검사와 missing-input 상태를 보강한다. |
| `76536172-4285-4efa-9ad6-528d3fa1de5f` 저장소 전환·RO 조회 | 기존 writer lock·읽기 전용 조회·doctor 공통 전환 엔진을 유지하고 새 조회 index/보관 형식도 같은 경계에 연결한다. |

기존 task·claim·검증 기록은 이번 등록으로 수정하지 않는다. 기존 문서의
"원 argv 보존"은 잘못된 argv를 계속 출력하라는 뜻으로 사용하지 않는다.
이번 사용자 요구에 따라 ready 행동의 생성 단계에서 유효한 값과 argv를 완성하고,
renderer는 완성된 동일 argv를 text/JSON에 전달한다.

## 3. 확정할 공개 동작

### D01. 현재 상태, 시도 결과, 이력의 단위

프로세스의 기본 행은 `profile + canonical directory + command`인 서비스다.
실행 ID별 행은 `--view history`에서 제공한다. env와 설정·입력 버전은 서비스 행의
현재 실행/최근 시도 정보와 history 조건으로 표시한다. source의 singleton 계약을
유지하며 이름이 비슷한 다른 명령·위치·profile을 임의로 합치지 않는다.

기본 `--view current`는 현재 선언된 서비스와 살아 있거나 결과가 불명인 실행,
해결되지 않은 최신 실패를 대상으로 한다. 현재 실행과 마지막 시도 결과는 별도
필드다. 기존 서버를 유지한 채 restart 준비만 실패하면 Running과 최근 실패를
한 행에서 함께 알린다. 후속 성공으로 해결된 과거 실패·종료 실행은 history에 둔다.
동일 서비스에서 복수 활성 실행을 발견하면 개수·ID와 conflict를 표시하고 숨기지 않는다.
현재 선언은 있으나 실행한 적 없는 명령은 Not started, 정상 종료는 Exited로 표시한다.
명령을 제거했어도 살아 있는 실행/미해결 결과는 남겨 확인할 수 있게 한다.
기본 서비스 후보는 serve 선언 또는 관리 실행 이력이 있는 명령이다. 실행 이력과
serve 선언이 모두 없는 build/check 명령까지 Not started 서비스로 늘리지 않는다.

### D02. 범위 선택

- project collection은 기본 현재 프로젝트의 canonical directory다. `--dir`은
  프로젝트/위치 선택, `--profile`은 해당 profile의 위치 전체, `--instance`는 실제
  instance를 선택한다. 동시에 주면 교집합이며 모순이면 invalid_argument다.
- `--all-profiles`는 명시적 전체 범위이며 --profile과 배타다. 단독 --profile은
  현재 cwd 때문에 다른 위치를 놓치지 않는다. 선택 결과의 profile·위치·view를 출력한다.
- 프로젝트 밖에서 project collection의 범위를 추론하지 못하면 scope_required와
  실제로 선택할 수 있는 profile/instance 조회를 제공한다. 몰래 전역으로 넓히지 않는다.
- profile list와 cleanup archives처럼 본래 전역인 자원은 전역이 기본이다.
  proxy의 기존 --global 의미는 유지한다. task의 profile 선택 규칙도 유지한다.

### D03. 필터·시간·페이지 계약

- collection 공통: `--limit` 1..200, 기본 50; `--cursor`; `--sort`; `--order asc|desc`.
  history 기본은 created_at desc, 이름 자원은 name asc, 서비스는 attention/command의
  고정 순서다. 동률에는 resource ID/서비스 key를 사용한다.
- `--state`는 해당 명령에서 실제 존재하는 상태만 허용하고 반복 입력은 OR다.
  다른 필터와는 AND다. state가 없는 collection에 무의미한 옵션을 만들지 않는다.
- 기간 있는 collection에 `--since`, `--until`을 추가한다. timezone을 포함한 RFC3339만
  받고 `since <= timestamp < until`이다. `--time-field created|updated|ended`로 기준을
  고르며 history는 created가 기본이다. ended가 없는 행은 ended 기간 검색에서 제외한다.
  누락된 과거 시각을 파일 mtime으로 추정하지 않는다. 가능한 time-field는 schema로 제한한다.
- 문자열 검색은 이름/명령의 정규화된 문자 부분 일치인 `--query`이며 값·secret·원문
  로그·private 경로는 검색하지 않는다. 날짜 없는 자원에는 기간 필터를 제공하지 않는다.
- 필터→안정 정렬→페이지 순서로 조회한다. cursor는 root·범위·view·필터·정렬·snapshot에
  묶고 다음 페이지는 같은 snapshot을 읽는다. 조건 변경/만료는 cursor_invalid다.
  next 안내에는 모든 조건과 실제 cursor를 포함한다. limit 0/전체 무제한 옵션은 제공하지 않는다.
- snapshot이 필요한 collection은 기존 `--query-cache-dir` 계약을 공유한다. private
  cache는 데이터 root와 분리하고 명시 경로를 모든 continuation에 유지한다. 첫 페이지에
  결과가 끝나면 cache를 만들지 않고, cursor 읽기는 생성·prune을 하지 않는다.
  읽기 전용 데이터와 쓰기 가능한 별도 cache 조합을 지원한다. snapshot에는 secret 값,
  private 입력과 원문 로그를 넣지 않는다.
- text와 JSON은 같은 선택 결과를 사용한다. 표시 개수·현재 조건·다음 페이지 여부를
  제공하며 전체 수를 확인하지 않았으면 exact total로 표현하지 않는다.

### D04. 목록별 적용 표

이 표는 현재 명령 이름을 기준으로 한 적용 계약이다. 옵션은 구현 예정이며 오늘
설치본에서 사용할 명령 예제로 제시하지 않는다. T01은 복합 collection 누락도 확인한다.

| 명령 | 기본 단위/추가 선택 | 정렬·기간 |
| --- | --- | --- |
| process list | current 서비스 / view=current\|history, scope, state, command, env, query | current 우선순위; history created/updated/ended |
| expose list | 현재 공유·해결 필요 상태 / view, profile, dir, provider, state, query | current 우선순위; history created |
| values execution list | 현재 전달·해결 필요 상태 / view, profile, delivery-state, execution-state, query | created/updated, 실제 시각만 |
| task list | 기존 할 일 기본 / state, completion, scope, workstream, query | 기존 순서가 기본; created/updated 선택 |
| task workstream list | 기존 기본 / state, completion, query | 기존 순서가 기본; created/updated 선택 |
| task validation list | 기존 대상 / workstream, scope, recorded-result, query | 기존 순서; created/updated 선택 |
| task current | 현재 유효 run / profile, dir, task/workstream 선택 | started/updated |
| task history | 기존 target / action, workstream, query | 발생 시각 desc |
| task workstream history | 기존 workstream target / action, query | 발생 시각 desc |
| task checkpoint list | 기존 run target / checkpoint 종류 | 발생 시각 desc |
| variable list, secret list | 현재 유효 key metadata / profile, env, source, query | name; 값 내용 검색 없음 |
| env list, bundle list | 현재 정의 / profile, query | name; 없는 시각 필터 없음 |
| command list | 현재 선언 / dir, query | name; exec 원문 검색 없음 |
| port list | 현재 할당 / profile, dir, instance, service, protocol, query | 서비스/포트 |
| instance list | 현재 연결 / profile, dir, instance, location-state, query | 위치/이름 |
| proxy list | 현재 선언/route / 기존 global/profile, dir, listener, state, query | route/host |
| profile list | 전역 profile metadata / query | name |
| cleanup archives | 보관 항목 / profile, kind, archive-state, query | archived 시각 desc |

각 collection은 limit/cursor를 갖는다. 기존 task의 순서·scope·completion 의미는
유지하며, completed-but-stale를 단순히 done으로 숨기지 않는다. `project status`,
`values uses`, runner inventory, cleanup preview 등 복합 응답도 T01에서 포함/해당 없음의
근거를 남긴다. 선택한 항목을 모두 보라는 진단·검증 응답에 임의로 페이지를 끼우지 않는다.

### D05. 표시 열

text 목록에 `--columns` 반복 선택을 제공한다. 허용 열과 기본 열은 각 command
metadata에 정의하고 unknown/중복 열은 parse 단계에서 오류로 반환한다.
서비스 기본 열은 command, current_state, last_attempt, last_changed다. history는
command, state, created_at, ended_at, reason, execution_id다. ID·긴 원인은 상세 조회로
확장할 수 있고 빈 셀은 미확인/해당 없음을 구분한다. --columns는 text 표에만 적용하며
effective JSON과 조합하면 오류다. auto가 agent/pipe에서 JSON을 선택한 경우에도
같고, 표의 열 선택에는 명시 `--format text`를 안내한다. JSON의 필수 필드를 열 옵션 때문에 제거하지 않는다.
일반/agent/CI의 형식 자동 선택에 따라 조회 기본 범위를 바꾸지 않는다.

### D06. 다음 행동의 실행 가능성

행동을 ready / needs_input / manual로 구분한다. ready의 argv는 registry parser와
static rule을 통과하고 모든 자동 생성·관찰된 입력을 실제 값으로 갖는다.
REQUEST_UUID, EXECUTION_ID, VALUE, PROGRAM 등 자리표시자를 Next의 실행 줄에 넣지 않는다.
문서·help의 일반 사용 예시는 template임을 명시할 수 있다.

- 새 요청에는 UUID를 생성해 행동 데이터에 한 번 고정한다. renderer와 shell마다
  다시 만들지 않는다. 응답 유실/부분 완료의 재시도는 원 UUID·입력·scope를 보존한다.
- known execution/profile/revision/digest/cursor는 관찰한 값을 바인딩한다. 새 mutation에
  원 UUID를 복사하거나, stale revision을 새 revision으로 몰래 교체하지 않는다.
- 도메인은 typed intent와 공개 scope를 전달한다. binder는 기존 command registry의
  순수 계약 resolver를 받아 옵션·인자·배타성·필수 입력을 검사한다. handler registry나
  schema를 별도로 복제하지 않는다. dashboard도 같은 binder의 결과를 소비한다.
- root 옵션은 command 앞, command 옵션은 child `--` 앞에 둔다. 외부/manual 행동에는
  devtools 옵션을 상속하지 않는다. 현재 shell literal 인용을 재사용한다.
- 비밀·입력 파일·원 stdin을 알 수 없으면 needs_input이다. 가짜 argv를 실행 줄로
  표시하지 않는다. 입력 항목·출처·입력 방법을 구조화하고 완성된 조회/입력 단계만 제시한다.
  터미널은 필요한 해당 command의 입력 prompt를 제공한다. agent/CI/JSON/pipe는
  명시 file/stdin을 요구하며 자동으로 stdin을 읽거나 숨겨진 prompt를 열지 않는다.

### D07. 미리보기와 실제 적용

preview는 적용할 정규화된 입력·선택·revision과 연결된다. 이미 제공된 공개 입력은
보존하고, argv/JSON body/private file/stdin을 서로 다른 입력 경로로 처리한다.
재공급한 입력이 preview와 다르면 새 preview가 필요하다. private 입력을 보존하려고
일반 조회/preview에서 새 영구 snapshot이나 vault unlock을 만들지 않는다.

원 stdin을 다시 얻지 못하는 비대화형 상황은 needs_input으로 남기며, 적용 성공을
약속하는 명령을 출력하지 않는다. 터미널 입력을 받는 경우에도 원 preview와 대조하고
일치하지 않으면 변경 내용을 다시 보여 준다. `PROGRAM ARGUMENTS...`를 저장하는 식의
성공은 금지한다. 원 입력이 제공된 installed fixture에서는 출력한 ready 명령 그대로
실행해 미리보기와 같은 결과·같은 대상이 되어야 한다.

### D08. 보관·정리 정책

기록을 현재 자원, 시도 이력, 로그/전달 payload, 요청 재생 증거, 사용자 작성 task/정의,
캐시로 분류한다. 같은 fixed retention 상수와 소유권 검사로 inventory·read·cleanup을
맞춘다. 보관 기간이 끝났다는 이유로 pending/unknown 외부 효과를 완료로 간주하지 않는다.

| 데이터 | 수명·정리 기준 |
| --- | --- |
| 현재 자원·살아 있는 실행·미해결 시도 | 현재 상태로 유지. age만으로 삭제/종료하지 않음 |
| 완료 process/exposure/값 전달의 상세 이력 | 해결된 종료 시각 이후 30일. 종료 시각 없는 과거 자료는 확인 전 보존 사유 표시 |
| process 캡처 로그 | 종료 7일 뒤 읽기 만료와 실제 파일 정리를 일치시킴 |
| 값 전달 payload | 기존 ExpiresAt·확정 종료에 따라 인증된 정리. 전달 불명·진행 중 payload는 보존 |
| 완료 요청의 identity와 결과 재생 | 기존 replay 계약 유지. 30일 expiry가 있으면 request_expired, 무기한 계약은 임의로 축소하지 않음 |
| task/workstream·값/명령 정의 | 사용자 기록으로 유지. 기존 명시적 archive/remove 정책을 사용 |
| query snapshot | 30분. 소유한 만료 cache만 정리; caller가 지정한 cache 위치를 별도로 추적하지 않음 |
| 사용자가 보관함으로 옮긴 항목 | 기존 30일 purge 제한과 명시적 restore/purge 유지 |

자동 정리는 관련 쓰기 완료 후 제한된 수의 owned artifact를 처리하고 재개 위치를
보존하는 유지보수 단계다. 일반 read/doctor/status는 파일을 삭제하지 않는다.
독립 daemon/cron은 추가하지 않고 기존 cleanup에 명시적인 dry-run/실행 경로를 연결한다.
오랫동안 쓰기가 없으면 자동 정리는 진행하지 않으며 inventory에 만료 잔여량과 이유를 표시한다.

같은 instance에 다른 서버가 살아 있어도 종료된 해당 실행의 lease·참조·복구 의존성만
검사해 정리할 수 있게 한다. lifecycle batch, previous_id/자동 교체 chain, 요청 결과,
archive restore가 참조한 본문은 단순 삭제하지 않는다. 현재 서비스 요약과 마지막 결과를
유지하고, 재생에 필요한 결과는 작은 영수증 또는 공유된 보관 본문 참조로 보존한다.
동일 본문을 record/receipt/archive에 중복 저장하지 않도록 참조를 검사한 후 압축한다.
요청 ID만 지워 과거 작업이 다시 실행되는 일이 없어야 한다.

최소 identity 및 무기한 재생에 필요한 논리 결과는 요청 수에 따라 증가할 수 있다.
모든 기록의 용량을 일정하게 만들겠다고 약속하지 않는다. 영수증 재생 기한을 새로
줄이는 breaking policy, 사용자 task의 TTL 삭제, 외부 provider 자원 삭제는 제외한다.

### D09. 호환성과 전환

기본 단위·페이지·Action 준비 상태가 바뀌므로 CLI protocol 23 전환으로 계획한다.
envelope 1, versionless TOML, native child/stdout, request fingerprint와 기존 retry 의미는
유지한다. 새 JSON schema와 bundled consumer/skill을 함께 바꾼다. text만 필터링하거나
JSON에서 과거 무제한 동작을 남기는 이중 기본값은 사용하지 않는다.

기존 실행과 이력은 삭제하지 않고 summary/index를 별도로 재구성한다. 현재 포맷 reader는
현재 형식만 읽고 새 형식 준비/전환은 기존 doctor --storage --fix 엔진과 installer/update
hook을 사용한다. ordinary query는 migration_required/initialization_required와 같은
root의 실제 준비 행동을 반환한다. 전환은 원 writer lock·원자 게시·재개·검증을 사용한다.
설치/doctor 전환 자체로 history TTL 정리나 archive purge를 실행하지 않는다.
이전 바이너리의 알 수 없는 포맷 읽기/쓰기와 downgrade는 fixture로 확인하고 조용한
overwrite를 허용하지 않는다. 배포 승인은 이 계획과 별개다.

## 4. 구현 작업과 의존성

모든 구현 작업은 아직 open/pending이다. 아래 단위마다 task와 필수 validation을
등록하고, 최종 통합 검증을 별도로 연결한다. 중간 커밋만으로 protocol 23을 배포하지 않는다.

| ID | 결과·주요 경로 | 선행 | 완료 근거 |
| --- | --- | --- | --- |
| T01 | 전체 collection/action/보관 inventory와 protocol 23 계약 고정. cli registry/schema/static rules, protocol, retention, docs | 없음 | 기준 198 canonical과 이후 추가 command 전체 분류, 동적 생성 경로·복합 collection 포함. 적용/해당 없음의 이유, 필드·기본값·선택 예제 검토 |
| T02 | ready/needs_input/manual binder와 실제 UUID·scope·static 계약 검사. protocol/guidance, cli/parse·출력 경계, domain producers | T01 | read/write/recovery 모두 유효한 argv. managed stop에는 profile 없음. 원 UUID 재시도·root·비밀 비노출·text/JSON 동일 행동 |
| T03 | preview 입력 재공급·일치 검사와 child/root 경계. cli/preview_output, configedit, backup/profile/proxy preview | T02 | argv/file/stdin/비밀 입력별 분기. 출력된 ready 적용 명령이 preview와 같은 변경. 원 stdin 없는 경우 needs_input이며 조용한 다른 입력 적용 없음 |
| T04 | 공통 collection 선택·정렬·cursor·열 계약. cli/schema/options, 작은 query helper, domain query adapters | T01 | filter AND/state OR, RFC3339 경계, 안정 정렬·snapshot cursor, 0/200 초과 오류, text/JSON 동일 선택 |
| T05 | 서비스 current 요약과 execution history. services/store·request/lease, lifecycle, cli/processes, presentation | T04 | 종료 312건+현재 1건 fixture의 기본 서비스 요약, 최신 실패/회복·복수 활성·다른 env/위치, exited 표시, history로 전체 도달 |
| T06 | exposure와 값 전달 실행의 current/history·필터·페이지. expose/proxy exposure_control, valuehandoff/inspect, cli | T04 | stopped/expired 이력 분리, unresolved delivery 유지, provider/profile/state/time 검색, 마지막 page까지 누락·중복 없음 |
| T07 | 나머지 collection과 복합 응답에 적합한 조회 옵션 연결. tasks/values/ports/proxy/profiles/cleanup, cli | T04 | D04 전 행·T01의 추가 대상 확인. 기존 task 순서·stale completion·권한·비밀 비노출 보존. 부적합 옵션은 지원하지 않음 |
| T08 | artifact별 TTL·참조 검사·제한된 자동 정리·압축을 공통 cleanup에 연결. retention/cleanup/services/expose/valuehandoff/netops/lifecycle | T05,T06 | 같은 instance의 live peer를 유지하며 종료 항목 정리. replay/unknown/chain/archive 보호. 7/30일 경계·crash/retry·잔여량 설명 |
| T09 | 현재 summary/index와 compact 형식의 명시적 전환·복구. storageupgrade adapters, doctor/install/update, maintenance | T08 | clean/legacy/locked/RO/중단·다시 실행·old writer. ordinary read는 전환·삭제·잠금 파일 쓰기 없이 동작 |
| T10 | help/schema/completion·문서·bundled skill·dashboard consumer를 최종 계약에 맞춤. cli/presentation·cliui, docs, skills/devtools, dashboard | T03,T05,T06,T07,T09 | protocol 23, 실제 지원 옵션·상태·열·조건과 예제 일치. 필요 input과 ready 명령 구분. 원 native 출력 보존 |
| T11 | 큰 이력과 동시 변경에서 조회·정리 비용 검증. services/cleanup query integration, 기존 perf/verify harness | T03,T05,T06,T07,T08,T09 | 313/1,000/10,000 history와 live 1/다수. 반환 크기는 page 비례, RPC는 선택된 현재 실행에 한정, 불필요한 payload 복호화 없음, cleanup 반복 전량 List 금지 |
| T12 | 전체 변경·모든 생성/조회 caller의 통합 리뷰와 installed/native 검증. verify/scenarios, 기존 workflows | T10,T11 | 통합 리뷰 ledger 전 영역 reviewed, 유효 미해결 결함 없음, 필수 CI/evidence가 최종 SHA에 적용. 실제 사용자 정리·릴리스 없이 구현 완료 판정 |

T02/T04는 T01의 계약을 소비하는 서로 독립된 축이다. T05/T06/T07은 T04 뒤에
각각 검증한다. T08은 실제 service/exposure 참조를 소비한다. 파일 공유만을 이유로
불필요한 작업 의존성을 만들지 않는다. 기존 외부 workstream의 close는 의존성으로 걸지 않는다.

## 5. 요구사항과 완료 조건

| 요구사항 | 내용 | 완료 조건 |
| --- | --- | --- |
| R01 | 모든 collection·행동 생성부를 실제 registry와 연결해 조사 | A01: T01 inventory에 전 명령·동적/중첩 경로와 적용 여부 존재 |
| R02 | ready 행동이 실제 command 계약·값을 만족 | A02: T02의 read/write/recovery fixture와 parser/static/schema 대조 통과 |
| R03 | 원 UUID·scope·효과·불명 결과 의미 유지 | A03: 응답 유실/부분 성공/원래 root·profile·target 복구에 새 작업 실행 없음 |
| R04 | preview와 적용의 입력·대상 동등성 | A04: T03에서 ready 명령을 그대로 실행한 결과가 preview와 일치 |
| R05 | current/history와 현재 실행·마지막 시도를 구분 | A05: T05의 복수 위치/ID/회복·실패·unknown·retired command 선택 정확 |
| R06 | 범위·상태·검색·시간을 명시적으로 선택 | A06: D02–D04 옵션 조합·invalid/empty·time-field 경계 검증 |
| R07 | 제한된 페이지와 안정적인 탐색 | A07: 필터/정렬 결합 cursor, snapshot·만료·조건 변경 검증; 모든 행 도달 |
| R08 | text 표시 열과 JSON 결과 선택 일관성 | A08: D05 열 제약·기본 요약·시간/원인·exited와 JSON schema 검증 |
| R09 | 다른 collection에도 맞는 정책 적용 | A09: T06/T07 전체 적용 표와 task 기존 의미·protected metadata 보존 |
| R10 | 데이터 종류별 보관 목적과 실제 정리 일치 | A10: 종료 이력 30일·로그 7일·payload expiry·cache 30분 및 N/A 이유 확인 |
| R11 | 정리가 살아 있는 실행·재시도·참조를 보존 | A11: live peer 유지·unknown 보존·receipt replay·chain·archive restore crash/retry 검증 |
| R12 | 조회는 비파괴이며 새 포맷은 명시적으로 전환 | A12: T09 native RO·locked·prepare/recovery·old writer fixture 통과 |
| R13 | 도움말·schema·consumer의 실제 계약 일치 | A13: T10 protocol 23·completion·bundled skill·dashboard·native stdout 확인 |
| R14 | 큰 이력에서도 탐색·정리 비용이 통제됨 | A14: T11 규모·RPC/IO/payload 계측과 page-sized 결과 확인 |
| R15 | 계획·구현·검증 상태를 구분하고 전체 리뷰 수행 | A15: 계획 readback/check·리뷰 ledger, 후속 구현의 최종 CI 근거와 미실행 구분 |

## 6. 검증 계획

- V01–V12: 각 T01–T12의 완료 근거를 해당 task 소유 필수 validation으로 등록한다.
  구조/문구 snapshot만 통과시켜 실제 command 성공을 대체하지 않는다.
- V_PLAN: 이 문서·spec·task·acceptance·validation·의존성의 계획 리뷰와 등록 readback.
  계획 검증이며 제품 구현 검증과 구분한다.
- V_ACTION: installed CLI에서 원 오류→준비된 실제 argv→실제 의도한 결과를 검사한다.
  단순 문자열·placeholder 치환·mock handler 통과만으로 완료하지 않는다.
- V_QUERY: current/history/모든 필터·시간·정렬·페이지·열·대량 데이터·동시 변경을
  한 합성 fixture에서 연결한다. Linux/macOS 실제 write denial과 Windows handle 검증을 구분한다.
- V_INTEGRATION: 기존 check:all과 installed 전체 scenario, 영향 받는 Go race/native,
  Windows x64/ARM64·Unix 플랫폼·shell round-trip을 기존 GitHub Actions로 실행한다.
  기존 opt-in real provider/system-network 조건을 임의로 required 성공으로 세지 않는다.

회귀 fixture는 profile `app-a/app-b`, 명령 `web/api`, 별도 위치·env와 합성 값만 사용한다.
313행 규모 재현은 수량·관계만 재구성한다. 사용자 원본 데이터·식별자·비밀은 포함하지 않는다.
권한/잠금/보호를 바꿔 검사 실패를 감추지 않는다. 후속 구현 요청에 따라 기존 CI를 실행한다.

## 7. 계획 리뷰 기록

초안 리뷰에서 다음 결정을 반영한다. 등록 뒤 전체 영역을 다시 읽고 구조 검사를 수행한다.

| ID | 확인한 계획 결함/누락 | 반영 및 재확인 기준 |
| --- | --- | --- |
| P01 | Running만 남기면 최신 실패/unknown과 살아 있는 retired command를 숨김 | D01에 현재 실행과 최근 시도를 분리하고 attention 대상을 포함 |
| P02 | --profile에 cwd를 암묵 적용하면 다른 위치가 누락 | D02에서 profile 전체 범위와 --dir 교집합·outside-project 오류를 명시 |
| P03 | raw stdin/secret을 되살릴 수 없는데 runnable apply를 약속 | D06/D07에서 needs_input과 입력 재공급·다시 preview를 명시; secret echo·암묵 cache 없음 |
| P04 | 단순 TTL 삭제가 receipt replay·chain·unknown을 손상 | D08에서 참조/계약 보존과 실제 삭제 가능한 payload·상세 record를 분리 |
| P05 | index/cleanup 준비를 ordinary query나 update의 삭제 부작용으로 처리 | D08/D09에서 read는 비파괴, doctor는 포맷 준비만, 정리는 별도 유지보수로 분리 |
| P06 | CLI 이름이 list인 20개만 보고 전체라고 간주 | T01에 198개 registry·동적 생성부·복합 collection coverage 요구 |
| P07 | 표시만 바꾸고 JSON·cursor·native 호출을 빠뜨림 | D03/D05/D09와 V_ACTION/V_QUERY/V_INTEGRATION에 같은 선택·버전·실제 실행 요구 |
| P08 | 명령·옵션을 한꺼번에 통일해 의미 없는 time/state 옵션까지 추가 | D03/D04의 명령별 상태·시간/검색·해당 없음 근거, 기존 task 의미 보존 |
| P09 | current 서비스 후보를 모든 config command로 잡으면 build/check까지 기본 목록을 채움 | D01에서 serve 선언/관리 이력 후보로 제한하고 live/unknown의 누락을 막음 |
| P10 | 일반 collection 페이지가 RO 데이터 root 안에 cache를 만들 수 있음 | D03에 query-cache-dir·첫 단일 페이지 무쓰기·cursor 무쓰기와 snapshot 비밀 제외를 명시 |
| P11 | auto 형식과 columns의 조합에서 옵션을 조용히 무시할 수 있음 | D05에서 effective JSON 오류와 명시 text의 사용 경로 확정 |

계획 리뷰의 completion은 등록·readback·check에만 해당한다. 제품 validation은
실제로 수행한 뒤 별도로 기록한다. 사용자 후속 요청을 반영해 T01부터 구현을 진행하고,
전체 결과와 통합 근거가 갖춰진 뒤에만 task와 workstream을 완료 처리한다.
