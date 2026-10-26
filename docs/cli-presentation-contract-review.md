# CLI 표시 계획 추가 계약 리뷰

기준: `ff8edcca1ba5bae4fdd7981779bc4f0c5e557438`, 설치 CLI 0.34.0 / protocol 14.
범위: [표시 계획](cli-presentation-plan.md)의 계약 전체. 이 계약 리뷰 당시 제품 구현은 미착수였다.
방식: 순차 source 대조 → 모순·미결정 확인 → 계획 수정 → 영향 계약 재검토.
별도 agent나 외부 reviewer를 사용하지 않았다. 명시 보류한 임의 agent 설정·TUI·새 TOML·
색상 강제 옵션·릴리스/설치는 추가하지 않는다.

## 확인한 빈틈과 확정 결정

### D01. 화면 모델의 실제 타입·소유권

- 빈틈: Document가 주석만 있는 struct였고 primitive가 mutation 가능한 domain 값이나
  서로 다른 row cell 수를 받을 때의 계약이 없었다.
- 근거: 계획의 `Build→Document→Render` 경계가 public data 의미 검사와 순수 UI 검사를
  분리하려면 화면 값을 renderer 구현과 독립적으로 읽고 고정할 수 있어야 한다.
- 결정: Role/Flow/Span/Text/Field/Column/Row, private Block/Document의 닫힌 block constructor로
  고정한다. constructor는 slice를 복사하고 renderer는 값을 변경하지 않는다. callback/any/
  domain pointer/raw ANSI를 받지 않는다. 잘못된 shape/role/level은 ErrInvalidDocument다.
  후속 D13으로 CommandLine이 여섯 번째 block에 추가되며 원 argv slice도 복사한다.
- 검사: presenter의 값·상태·순서, cliui의 layout/escape/reset, row-column mismatch와 caller
  slice 변경 후 결과 불변을 각각 검사한다. public import 방향도 검사한다.

### D02. format·help·schema·취소의 결정 순서

- 빈틈: 'help 우선'과 'schema 오류는 JSON' 및 bootstrap/cancellation 순서가 충돌할 수 있었다.
- 근거: `internal/cli/parse.go:90`는 name 뒤 토큰을 child로 넘기고, `:83`의 help는 그 앞에서만
  해석한다. unknown option은 help를 보기 전에 실패할 수 있다. `internal/cli/cli.go:281`의
  discovery 분기는 `schema --help`만 특수 처리한다. 현재 실행 결과도 아래 표와 일치한다.
- 결정: private bypass → root 문법/선택 값 검증 → 기존 parser 결과 보관 → effective action
  capability → ContextError → 기존 parse/discovery 오류 → 신규 format/capability 충돌 → 실행.
  --help를 전체 argv에서 검색하지 않는다. normal schema의 env=text는 JSON 고정이고 explicit
  flag=text만 새 입력 오류다. 기존 parse 오류가 있다면 그 오류를 format 충돌보다 먼저 유지한다.
  `protocol/context.go`의 explicit cancel/130과 deadline_exceeded/3도 그대로 구분한다.
- 검사: 계획 B2.1의 matrix와 child boundary·pre-canceled 호출·선택되지 않은 invalid env를 확인한다.

### D03. 원래 argv offset의 단위·EOF·alias

- 빈틈: prefix 길이와 index mapping을 함께 남기거나 EOF/alias를 누락하면 잘못된 token을 가리킨다.
- 근거: `internal/cli/parse.go:189`와 `:206`은 missing/conflicting 입력에 len(tokens) EOF를
  반환한다. cli.go는 현재 command 길이와 rootArgumentOffset을 별도 더한다.
- 결정: 원 token mapping에 EOF sentinel을 추가하고 kind=argv의 offset/end_offset만 한 번
  변환한다. 실제 alias token 소비량을 사용하고 JSON byte offset·line/column은 바꾸지 않는다.
  root 오류와 handler의 Invocation source는 이미 원 index다. parser-relative source만 해당
  경계에서 매핑하고 최종 fail에 공통 재매핑 코드를 남기지 않는다.
- 검사: run/cmd run/command run·두 root 옵션 순서·EOF·direct executable·JSON 입력 오류를 대조한다.

### D04. 표시 모드와 요청 identity

- 빈틈: format을 Request.Options에 추가하면 같은 업무 retry가 다른 요청이 될 수 있다.
- 근거: `internal/cli/tasks.go:298`은 r.Options를 store에 전달한다. `internal/tasks/store.go:412`
  fingerprint는 options를 포함하고 `:660`은 request-id/context/file/stdin만 제외한다.
- 결정: format/style은 invocation-local 출력 상태다. handler Options/body, InvocationContext의
  실행 선택, digest/revision/replay 저장 결과·실행 snapshot에 새 필드로 넣지 않는다.
  기존 자식 환경 상속은 바꾸지 않는다. 새 format 값 때문에 UUID를 새로 만들지 않는다.
- 검사: 업무가 허용하는 동일 UUID/동일 입력 retry를 json/text로 바꿔도 원 identity·결과를 유지한다.
  새 형식을 확인하기 위한 mutation 재실행은 하지 않는다.

### D05. 렌더 실패와 원래 exit code의 우선순위

- 빈틈: 'fallback도 실패하면 internal_error/1'이 원래 오류 code/exit 보존과 충돌했다.
- 근거: `internal/protocol/protocol.go:44`의 실패 envelope에는 ExitCode가 없다. cli.go:239는
  stderr 쓰기 실패일 때 1, 정상 오류 전달이면 원 Error.ExitCode를 반환한다.
- 결정: CLI가 canonical byte와 원 exit/failure/stage를 함께 보관한다. 기존 오류는 UI가
  실패해도 원 code/exit를 유지하며, 실제 stderr transport 실패만 1로 우선한다. 성공 결과의
  UI 실패는 새 internal_error/1이고 encoding/write 실패는 기존 io_error/1이다.
- 기존 passphrase prompt(`cli/passphrase_windows.go:67`)·child stderr는 public response의
  단일 writer와 별개다. prefix를 없애거나 새 framing을 만들지 않고 단일 JSON 보장은 기존
  noninteractive 경로에 한정한다. top-level Public/BindGuidance 준비만 한 번 하고 nested
  cause 정규화는 기존 protocol에 맡긴다.
- 검사: 기존 오류 2/3/130 × 전용/Generic 실패 × stderr 실패 및 success publication 뒤 취소를 대조한다.

### D06. 긴급 출력에서도 진단 전체·실패 효과 보존

- 빈틈: 마지막 plain fallback이 네 개 필드만 남기면 nested causes·경고·복구 행동이 사라진다.
  새 오류가 scope/request UUID를 잃거나 모든 query를 무변경으로 추측할 여지도 있었다.
- 근거: `internal/protocol/diagnostic.go:197`은 기본 retry/effects를 만들고 기존 명시 값을
  보존한다. `:216`의 공통 진단은 checks/causes/remedies/target 등을 포함한다. cli.go:419는
  publication 뒤 취소로 성공을 뒤집지 않도록 이미 보장한다.
- 결정: 기존 오류의 최후 text는 고정 제목 아래 동일 canonical 오류 전체를 terminal-safe
  escape한다. Render는 최대 전용+Generic 두 번이며 emergency는 그 구조를 재호출하지 않는다.
  기존 효과는 그대로다. 새 출력 오류만 handler 전 none/after_dependency_change, 성공 반환 뒤
  unknown/inspect_first로 보수적으로 기록한다. 이미 아는 scope/원 UUID를 보존하며 별도 조회하지 않는다.
- 검사: partial/unknown/required inputs/복수 causes가 긴급 출력에도 남는지, short write 뒤
  재출력·새 handler 실행이 없는지 확인한다. 인코딩/쓰기 불능은 정상 표시 동등성과 구분한다.

### D07. Windows ANSI 관측과 host 상태 변경

- 빈틈: '관측만 하는 host adapter'가 VT mode를 변경·복원하며 복원 실패 책임은 정하지 않았다.
- 근거: GetConsoleMode는 기존 console mode 조회, SetConsoleMode는 buffer mode 변경이다.
  현재 public 출력 경로에는 VT enable이 없고 passphrase_windows.go:56의 input quiet mode는
  별도 보호 절차다. 실제 명령 전달에 필요한 동작과 색상의 부수 효과를 묶을 이유가 없다.
- 결정: 이번 UI는 출력 mode를 바꾸지 않는다. Windows output handle에서 기존
  PROCESSED_OUTPUT+VT_PROCESSING을 확인하고 미지원/관측 실패면 plain이다. 기존 passphrase
  input 처리에는 개입하지 않는다. enable/restore 제안은 현 계획에서 제거했다.
- 검사: mode 조회 성공 on/off·실패·TERM=dumb·NO_COLOR·실제 stdout/stderr 차이를 확인한다.
  이 결정은 임의 Windows terminal의 색상 지원을 보장하지 않는다.
- 공식 근거: [GetConsoleMode](https://learn.microsoft.com/en-us/windows/console/getconsolemode),
  [SetConsoleMode](https://learn.microsoft.com/en-us/windows/console/setconsolemode).

### D08. 구버전/신버전 bootstrap

- 빈틈: 'version을 pipe로 조회'해도 env=text가 강제되면 새 binary의 version은 text다.
  새 flag를 먼저 보내 보는 방식은 구 binary와 mutation retry에서 안전하지 않다.
- 근거: version은 cli.go:123의 일반 JSON-output command여서 새 auto/text의 적용 대상이다.
  schema는 이미 별도 discovery wire고 현재 protocol14에서 `schema version` JSON을 직접 확인했다.
- 결정: 호출 자식 env에 DEVTOOLS_FORMAT=json을 덮어쓰고 `schema version`으로 capability를
  조회한다. global_options에 format/json이 있어야 새 root flag를 쓴다. 없으면 구 계약으로
  호출한다. bootstrap 실패는 실제 작업 전에 보고하고 중단한다. 부모 env·저장소는 변경하지 않는다.
- 검사: protocol14/current candidate × 상속 env text/invalid × metadata 유무를 대조한다.
  mutation을 실패시켜 flag 지원을 알아내는 절차는 없다.

### D09. global metadata와 strict schema의 위치

- 빈틈: global_options를 어디에 몇 번 넣을지, machine-only의 text enum, strict catalog 갱신이 모호했다.
- 근거: schema.go:95의 entry와 :136의 `$defs.catalog`는 다른 구조다. 후자는
  additionalProperties=false다. 입력 schema는 CLI의 parsed input이며 stdin endpoint가 아니다.
- 결정: catalog/root/group/exact 응답마다 global_options 한 번, catalog 각 entry에는
  presentation만 둔다. format 정의의 name/values/default/environment/placement와 기존
  isolated-root 정의를 고정한다. machine-only normal input의 format enum은 auto/json,
  일반/native는 auto/text/json이다. `$defs.catalog` required/properties도 같이 변경한다.
- 검사: 모든 discovery path·alias의 정본 일치, response-schema 실제 검증·크기/중복 및 normal
  schema와 help grammar 예외를 확인한다. version data의 기존 필드는 유지한다.

### D10. 전용 view 선택·unknown·누락·타입 손실

- 빈틈: adapter 선택/필드 생략이 임의 구현이 되거나 required field 누락을 빈 값으로 숨길 수 있었다.
- 근거: 현재 command summary의 env/inject/output은 typed public 값(`cli/commands.go:14`)이고,
  Warning은 code/key/message(`values/warnings.go:7`), Check는 status/required/target/remedies
  (`doctor/doctor.go:23`)다. 메시지 단어만으로 같은 UI 의미를 추론할 수 없다.
- 결정: canonical 이름의 정적 DefinitionFor로 family/required/optional/known-omitted path를
  선언한다. 신규/미정의 명령은 Generic, shape 불일치는 전체 Generic이다. 실제 경고·진단은
  생략하지 않고 unknown field/status는 원문으로 남긴다. generic은 key 정렬·array 순서·number
  lexical text·null/false/0/empty 구분을 유지한다. display를 위해 추가 데이터 조회하지 않는다.
- 검사: 독립 schema/fixture와 path coverage, required type mismatch·큰 정수·빈 warning/
  warning 없음·unknown status·scalar data·원 token의 verbatim을 확인한다.

### D11. format 입력 오류의 source와 원문 값

- 빈틈: env 오류를 Source.kind=env로 만들거나 raw 값을 message에 넣을 수 있었다.
- 근거: configcontract/schema.json `$defs.Source` enum에 env는 없고 system/argv는 있다.
  Public projection의 안전한 facts와 오류 shape를 새 renderer가 우회하면 안 된다.
- 결정: argv format은 원 offset/path=[format], env는 kind=system/path=[env,DEVTOOLS_FORMAT].
  새 Source enum은 만들지 않는다. 신규 format 오류는 parse/none/after_input_change이며
  잘못된 raw 값은 출력하지 않는다. 원문 진단·기존 error source는 그대로 유지한다.
- 검사: 생성한 신규 진단이 기존 public schema에 맞고 민감한 모양의 invalid 값이 출력되지 않는지 확인한다.

### D12. 중간 default와 최종 auto의 단일 정본

- 빈틈: Select의 기본 auto와 T01–T04의 기본 JSON 유지가 서로 다른 조건문이 될 수 있었다.
- 근거: 초기 default 변경은 T05에만 수행하도록 이미 합의된 단계다. metadata와 동작이
  다르면 agent가 새로운 schema를 읽어도 잘못된 전제를 갖는다.
- 결정: Select와 metadata가 동일 DefaultFormat 상수를 쓴다. 초기 JSON, T05에서 auto로
  같은 정본을 바꾼다. 숨은 runtime flag·환경 예외·독립 metadata default를 두지 않는다.
- 검사: flag/env 생략 시 동작과 metadata.default를 같이 확인한다. explicit auto는 어느 단계든
  동일 정책이고 실제 최종 제품의 전체 gate 뒤에만 배포한다.

### D13. Next의 한 줄 복사·붙여넣기

- 사용자 결정: raw argv 배열은 바로 실행하기 불편하다. 명령 줄 전체를 복사해서 붙여넣을
  수 있어야 한다. 기존 token 배열 표시 결정의 사용성 한계를 수정한다.
- 근거: 기존 public action은 실행 의미가 담긴 argv를 이미 제공하므로 message나 action을
  새로 만들 필요가 없다. 다만 POSIX/Fish/PowerShell은 quote·backslash·native 인자 전달
  규칙이 달라 문자열 join/JSON 인용만으로 원 토큰을 보존할 수 없다. plan B6.5의 공식
  Fish/PowerShell 문서와 기존 `internal/process/invocation_windows_test.go:200`의 native
  argv fixture 경계를 참고한다. 기존 completion quote 함수는 용도가 달라 그대로 재사용하지 않는다.
- 결정: presenter는 같은 public action의 원 argv를 immutable CommandLine block에 담고,
  cliui가 host가 준 고정 shell 대상별 literal 인용을 수행한다. Unix=POSIX/Fish의 결과가
  같으면 한 줄, 다르면 shell label을 윗줄에 두고 각 한 줄을 표시한다. Windows는
  PowerShell 대상이다. 활성 shell의 환경변수/프로세스 추론·새 설정·cmd.exe formatter는
  추가하지 않는다. 원문 설명과 명령은 별도 줄이며 명령은 0칸 들여쓰기·SGR/prompt/번호/
  comment/실개행/ellipsis 없이 executable부터 마지막 인자까지 보인다.
- 한계: required_inputs가 남으면 완성 명령이라 부르지 않는다. control/개행 token이나
  지원 shell/native 전달에서 보존할 수 없는 literal은 escaped Arguments (not copyable)로
  남긴다. PowerShell 5.1/7의 빈 인자·내장 quote 차이도 실제 round-trip으로 확인한다.
  JSON/action 토큰/진단·요청 identity·기존 Exec 조회는 그대로이며 자동 실행하지 않는다.
- 재검토: B5.0/B5.1/B6의 raw token 원칙을 Next와 Exec 조회로 분리하고 UI 타입/의존 경계,
  예시·T01/T03/T04/T05/T06·A17·등록 spec/task/validation의 관련 이전 문구를 갱신한다.
  copy line을 최후 terminal escape로 바꿔 다른 argv가 되지 않도록 별도 출력 경로도 고정한다.
- 검사: 구현 뒤 부작용 없는 argv-echo fixture로 bash/zsh/fish 및 Windows PowerShell/7에서
  공백·Unicode·quote·backslash·변수/명령 치환·glob/연산자를 대조한다. 40/80/120 폭과 styled/
  plain에서 같은 한 논리 줄·원 토큰·안전 fallback을 검사한다. 이 리뷰는 제품 통과가 아니다.

## 실제 수행한 baseline 조회

interactive WSL bash에서 설치 binary에 DEVTOOLS_FORMAT=text를 주고 아래 public 조회만 실행했다.
현재 protocol14는 이 환경값을 아직 구현하지 않으므로 이는 기존 parsing/discovery 근거다.
신규 format 동작이나 future binary 통과 결과로 재사용하지 않는다.

| argv | 실제 결과 |
| --- | --- |
| `schema version` | stdout JSON, exit 0, protocol14 |
| `schema --help` | stdout text, exit 0 |
| `schema command list --help` | stderr JSON invalid_argument, exit 2 |
| `config schema --help` | stdout text, exit 0 |
| `command list --bogus --help` | stderr JSON unknown-option 메시지, exit 2 |
| `--isolated-root /tmp/devtools-no-execution-review` | stderr JSON 명령 필수 오류, exit 2; 대상 생성 안 함 |

## coverage와 남은 검증

1차 source 대조로 D01–D12를 확정하고 본문을 수정했다. 2차에서 영향을 받은 계약을 다시
읽어 ContextError의 deadline/취소 구분과 handler source의 이중 remap 가능성을 추가로
확인해 D02/D03에 반영했다. 실제 source line도 원문과 대조해 인용 위치를 바로잡았다.

| 영역 | 최종 계약 리뷰 | 확인한 결정·근거 |
| --- | --- | --- |
| OS·agent·format 정책 | reviewed | D02/D07/D11/D12; 허용 신호·format 검증·기존 VT 관측·default 정본 |
| root/alias/help/schema/취소 | reviewed | D02/D03; 실제 기존 CLI 조회6건·parser source·ContextError |
| 계층·화면 타입·소유권 | reviewed | D01/D04/D13; typed constructor·CommandLine·slice 복사·request fingerprint 제외 |
| public encode·표현·unknown | reviewed | D01/D09/D10; 단일 인코딩·number/없음 구분·field coverage |
| 진단·효과·출력 실패 | reviewed | D05/D06/D11; 원 exit·전체 emergency·I/O 우선순위·scope/UUID |
| layout·native wire·Windows | reviewed | D01/D07/D10/D13; A13 스타일·폭·복사 명령, output mode 변경 없음 |
| metadata·소비자·호환 전환 | reviewed | D08/D09/D12; bootstrap schema·strict defs·중간/최종 default |
| 작업·검증·read-back | reviewed | A16/A17과 D01–D13을 기존6 task/8 validation에 연결 후 원문·의존성 재확인 |

이 범위에서 남은 blocking 계약 선택은 없다. private block 저장 구조·보조 함수 분할 등은
위 공개 내부 경계와 검사 계약을 만족하는 구현 선택으로 남긴다. 모든 189개에 전용 화면을
미리 만드는 것은 요구가 아니며 Generic fallback과 전용 adapter의 field coverage로 완전성을
판단한다. native/화면 evidence 부재는 구현 후 필수 검증 pending이며 계획의 미결정으로 숨기지 않는다.

제품 unit/integration/설치·native·실제 화면 검증은 모두 pending이다. 계획 구조 check,
source 대조, 기존 binary 조회를 제품 구현 pass나 workstream 완료로 기록하지 않는다.
