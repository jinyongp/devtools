# CLI 표시 형식: auto · text · json 구현 계획

상태: 계획 리뷰 수정본. 제품 구현·구현 후 검증·릴리스는 미착수.
기록일: 2026-10-26 KST. 기준 소스: `ff8edcca1ba5bae4fdd7981779bc4f0c5e557438`.
설치 CLI: 0.34.0 / protocol 14. 등록 대상: devtools profile `devtools`의 별도 workstream.
Workstream ID: `bd696a33-81fa-4dab-9f61-ca5e76300d49`.
이 문서 전체를 workstream plan에 보존한다. 후속 구현 시 소스와 등록 문서를 함께 갱신한다.
추가 계약 리뷰의 근거·결정·검증 범위는 [contract review](cli-presentation-contract-review.md)에 기록한다.

## 1. 목적과 범위

사용자가 같은 CLI 명령을 실행해도 개인 터미널에서는 읽기 쉬운 텍스트를,
감지된 agent와 파이프에서는 기존 JSON을 받는다. agent 감지는 편의상 추론이며
권한·샌드박스·사용자 신원 판정이 아니다. 명시한 형식은 감지보다 우선한다.

확정된 사용자 요구는 사람용 별도 alias/TUI 없이 동작, 환경변수에 의한 추론,
형식 이름 `auto | text | json`, 기존 JSON 연동 보존이다. text에는 정보 계층·폭 대응과
터미널의 제한된 ANSI 강조를 포함한다. 새 TUI, 자동 pager, 실행 스크립트 echo,
동작 효과, 언어별 번역, 사용자별 영구 설정 UI는 제외한다.
후속 논의의 임의 agent 환경변수 목록·`[cli]` TOML 설정은 사용자 요청으로 보류했다.
JSON이 반드시 필요한 호출은 명시 옵션을 사용한다. 알려지지 않은 agent의 PTY 실행을
완벽하게 식별한다는 보장은 하지 않는다.

현재 `--output auto|redacted|raw`는 실행 stream 보호 옵션이다
(`internal/cli/output_policy.go`). 앞 대화의 표시용 `--output` 제안은 이 근거로 대체한다.
표시용 신규 옵션은 루트 전용 **`--format auto|text|json`**, 환경변수는
**`DEVTOOLS_FORMAT`**으로 고정한다. `DEVTOOLS_OUTPUT`은 새로 도입하지 않는다.

## 2. 현재 경계와 조사 근거

| 현재 책임 | 확인한 위치 | 변경 원칙 |
| --- | --- | --- |
| public argv 진입·private helper 분기 | `cmd/devtools/main.go`, `internal/cli/platform_windows.go` | private IPC/launcher wire는 표시 계층에 통과시키지 않는다 |
| 명령 선택·파싱·실행·오류·성공 출력 | `internal/cli/cli.go:221`, `internal/cli/parse.go` | 루트 옵션·표시 결정·최종 출력으로 책임을 분리한다 |
| stdout 소유권 | `internal/cli/output.go`, `Command.OutputMode` | json/text/artifact/passthrough와 표시 형식을 별개로 둔다 |
| JSON envelope·public 오류 | `internal/protocol/protocol.go`, `diagnostic.go`, `guidance.go` | 환경·TTY·표·색상 의존성을 넣지 않는다 |
| schema/help | `internal/cli/schema.go`, `discovery.go` | machine schema와 표시 계약을 함께 명시한다 |
| 정적/동적 completion | `internal/cli/completion.go`, `completion_dynamic.go`, `completion_powershell.ps1` | root flag 문법을 지원하고 private 후보 wire를 보존한다 |
| 자식 출력 보호 | `internal/cli/output_policy.go`, `internal/process/output.go`, `internal/redact` | 표시 형식으로 보호 정책을 바꾸지 않는다 |
| 기존 공개 보장 | `docs/cli-output.md`, `internal/cli/output_test.go` | 현재 '항상 JSON' 보장을 변경하므로 protocol 전환으로 취급한다 |

`App.Run`은 discovery 성공을 두 경로에서 직접 JSON으로 쓰고, 일반 성공과 fail closure도
별도로 직렬화한다. 일반 성공 한 곳만 바꾸면 schema·parse 오류·help 오류가 누락된다.
`IO`는 reader/writer 추상화이므로 테스트·임베딩에서 OS stdout을 대신 관측하면 안 된다.
현재 출력 보호 옵션과 표시에 같은 이름을 주거나 `processResult`를 JSON으로 감싸면 안 된다.
설치본 catalog는 총 196개 명령, JSON 191개·text 1개·artifact 2개·passthrough 2개다.
JSON 중 schema/config schema를 제외한 189개가 auto/text 적용 대상이다. 이 수치는 조사
baseline이며 신규 명령을 금지하는 고정 제품 제한으로 사용하지 않는다.

현재 세션의 관측: Codex sandbox 안·밖과 PTY에서 `CODEX_CI=1`, `CODEX_THREAD_ID` 존재.
`CODEX_SANDBOX`, `AI_AGENT`는 미설정. 사용자가 제공한 Claude Desktop 관측은
`CLAUDECODE=1`, `AI_AGENT=claude-code_2-1-308_agent`다. Claude 관측을 직접 검증했다고
기록하지 않으며 세션 ID·계정·토큰 값은 감지·저장하지 않는다.

## 3. 계층별 책임과 금지 의존성

```text
OS argv / IO / 허용한 환경변수
        ↓ CLI host adapter (환경 관측)
root parser → format resolver → command dispatcher
                                 ↓
                  기존 handler → service/domain/store
                                 ↓ public data / protocol.Error / processResult
                  BindGuidance / Public / JSON 직렬화 경계
                                 ↓
                  JSON bytes 또는 command presenter
                                       ↓ cliui.Document
                                CLI UI layout / renderer
                                 ↓
                       stdout 성공 / stderr 실패
```

### 신규 계층의 패키지 경계

기존 `presentation` 안에 command 의미 해석과 terminal rendering을 함께 두던 설계는
후속 요구에 따라 분리한다. 신규 **`internal/cliui`**를 독립 CLI UI 계층으로 둔다.
일반 command와 task/report presenter가 공통 style·width 정책을 반복 구현하지 않게 하는
현재 요구가 분리 근거다. 외부용 UI framework·plugin·범용 TUI engine은 만들지 않는다.

| 패키지 | 소유하는 책임 | 입력 → 결과 | 알지 못해야 하는 것 |
| --- | --- | --- | --- |
| `internal/cli` | root/command parsing, OS·stream capability 관측, handler 호출, public projection, 인코딩·출력·exit | argv/IO → 기존 handler 결과 → public bytes/exit | 개별 table padding·색 코드·wrap 구현 |
| `internal/cli/presentation` | format/text options 정책, command별 필수 의미·읽기 순서·요약·생략·상태 role, unknown field fallback | canonical command + public envelope → `cliui.Document` | OS IO, ANSI codes, terminal cell width 계산, store 재조회 |
| **`internal/cliui`** | 공통 화면 값 타입, spacing·table/stacked·wrap·cell width, 역할별 style, escape·reset | `Document` + `Options` → UTF-8 bytes | command 이름·profile·값/secret 정책·JSON envelope·protocol·환경변수·exit code |
| `internal/protocol` | 기존 public 오류 projection·JSON wire | 기존 public data/error → canonical envelope | CLI UI·TTY·색·Document |

허용 의존 방향은 `cli → presentation → cliui`와 `cli → protocol`이다. host 출력 조합을 위해
`cli → cliui`도 허용한다. `cliui → cli/presentation/protocol/domain/service/store/OS host API`
방향은 금지한다. presentation도 cli 루트·domain/service/store를 import하지 않는다.
`cliui`는 Go 표준 데이터/문자열 처리와 기존 x/text/width처럼 순수한 도구만 사용한다.
JSON 모드는 Document를 만들지 않고 canonical envelope byte를 그대로 출력하므로 CLI UI
변경이 JSON의 필드·escape·정밀도·순서에 개입할 경로를 만들지 않는다.

명령 adapter는 field path별 필수/알려진 생략/unknown 처리를 소유한다. `ready:false`를
`NOT READY`+Failure role로 해석하는 곳은 presentation이다. cliui는 Failure role의 span을
빨강으로 보강할 뿐, ready·ok·replayed 같은 업무 필드를 찾아 상태를 추측하지 않는다.
table header와 row cell의 구조만 알고 이름이 command인지 process인지도 모른다.

화면 경계의 `Document`는 Title/Scope/Status/Table/KeyValue/Section/Paragraph/Details/Next
같은 닫힌 block 값 타입으로 구성한다. 본문은 raw text와 semantic role로 표현한다.
문자열 callback·임의 Go domain object·raw ANSI 삽입 구멍은 제공하지 않는다. primitive
constructor가 필수 cell 수·형태를 검사하고 renderer가 선언한 table을 폭에 따라 stacked로
바꾼다. command별 field 선택·요약을 renderer callback에 넣지 않는다.

field 의미와 style은 서로 다른 검사로 검증한다. presenter 검사에서는 Document의 label·
status role·값·필드 생략·순서를 확인한다. cliui 검사에서는 command와 무관한 synthetic
Document 하나를 여러 width·SGR/color 옵션에 렌더링해 layout·escape·reset·의미 보존을
확인한다. 컴파일과 production import graph를 확인해 금지 역의존이 없는지도 검증한다.

구현 시 고정할 최소 인터페이스는 다음과 같다. 실제 파일 내부의 보조 함수 분할은 가능하지만
인자·결과의 소유권과 아래 닫힌 화면 타입 계약은 유지한다. 무관한 domain interface나 범용
renderer plugin은 만들지 않는다.

```go
// internal/cli: OS 관측, argv와 기존 handler 호출을 소유한다.
type rootOptions struct {
    Format string                 // 생략과 explicit auto를 FormatSet으로 구별
    FormatSet bool
    IsolatedRoot string
    CommandArgs []string
    OriginalOffsets []int         // 각 token의 원래 argv index와 마지막 EOF sentinel
}
func parseRootOptions([]string) (rootOptions, *protocol.Error)
func observeOutputEnvironment(IO) presentation.Environment

// internal/cli/presentation: OS·store·domain을 모르는 표시 계층이다.
type Terminal struct {
    IsTerminal bool
    Width int                     // 관측 불가면 0; layout에서 안전한 fallback 사용
    ANSI bool                     // host adapter가 관측한 기존 SGR 지원
}
type Environment struct {
    Stdout, Stderr Terminal
    Signals []string              // 참인 allowlist 변수의 이름만; 값 없음
    FormatEnv FormatInput         // adapter가 읽은 DEVTOOLS_FORMAT, 원문 외부 출력 금지
    NoColor bool                  // NO_COLOR가 비어 있지 않음; 값 내용은 무관
    DumbTerminal bool             // TERM=dumb; agent 식별과 무관
}
type Capability uint8             // adaptable / machine-only / native
type FormatInput struct { Present bool; Raw string }
type Selection struct { Format Format; Source string }
type Detection struct { Signals []string }
const DefaultFormat Format = FormatAuto // 최종 값; T01–T04에서는 같은 정본을 FormatJSON으로 둠
type Decision struct {
    Format Format
    Source string                 // flag / env / default / machine-contract
    Reason string                 // explicit / agent / non-terminal / terminal / machine-contract
}
func DetectAgent(allowlisted map[string]string) Detection
func Select(flag, env FormatInput) (Selection, error)
func Resolve(selection Selection, detected Detection, stdoutTerminal bool, capability Capability) (Decision, error)
func ResolveTextOptions(terminal Terminal, noColor, dumb bool) cliui.Options
func Build(canonicalCommand string, publicEnvelope []byte) (cliui.Document, error)
func BuildGeneric(publicEnvelope []byte) (cliui.Document, error)

// internal/cliui: command/JSON/protocol 없이 공통 terminal UI를 렌더링한다.
type Options struct {
    Width int
    SGR, Color bool               // Color는 SGR일 때만 true
    CommandShells []Shell         // host가 공급한 고정 출력 대상; Unix=POSIX/Fish, Windows=PowerShell
}
type Shell uint8                  // POSIX(sh/bash/zsh) / Fish / PowerShell
type Role uint8                   // primary / muted / success / warning / failure / info
type Flow uint8                   // prose / verbatim
type Span struct { Text string; Role Role }
type Text struct { Spans []Span; Flow Flow }
type Field struct { Label Text; Value Text }
type Column struct { Label Text }
type Row struct { Cells []Text; Details []Block }
type BlockKind uint8              // 1..6=heading / paragraph / fields / table / section / command-line; 0은 invalid
type Block struct { /* private tagged value: heading / paragraph / fields / table / section / command-line */ }
type Document struct { blocks []Block }
func NewDocument(blocks ...Block) Document
func Heading(text Text, level uint8) Block // 1=title, 2=section
func Paragraph(text Text) Block
func Fields(fields ...Field) Block
func Table(columns []Column, rows []Row) (Block, error)
func Section(title Text, children ...Block) Block
func CommandLine(argv []string) (Block, error) // 빈 argv/빈 executable은 invalid; slice를 복사
func (document Document) Blocks() []Block
func (block Block) Kind() BlockKind
// Text/HeadingLevel/Fields/Columns/Rows/Children accessor도 대응하는 값의 복사본만 반환한다.
func Render(document Document, options Options) ([]byte, error)
func Escape(text string) string   // Render와 최후 plain 진단이 공유하는 순수 control escape
```

Scope는 Fields, Status는 semantic role의 Paragraph, Details/Next는 Section으로 구성한다.
primitive 이름마다 별도 상속 계층을 만들지 않는다. Block/Document 내부는 private이며
constructor가 전달받은 slice·Text span·child 목록을 복사한다. 외부 setter·callback·`any`·
raw ANSI·domain pointer를 제공하지 않는다. Document를 구성한 뒤 Render는 이를 변경하지
않으며 다른 호출의 문서를 공유 mutable 상태에 저장하지 않는다. 검사에는 복사본을 주는
read-only accessor를 사용한다. Table constructor는 1–4개 column·1개 이상 row·각 row의
cell 수 일치를 검사해 잘못되면 ErrInvalidDocument를 반환한다. 빈 목록은 presenter가
Paragraph로 구성한다. empty Document·unknown role/flow/heading level·잘못된 나머지 block 형태는 Render가
쓰기 전에 ErrInvalidDocument로 거절한다. label도 Text이므로 scope의 보조 label과 필수
진단 label에 다른 semantic role을 줄 수 있으며 cliui가 label 문자열을 해석할 필요가 없다.
wide table의 Row.Details는 표 뒤에 원 row 순서대로, stacked에서는 해당 row 바로 아래에
배치한다. 대상 식별용 Section 제목은 presenter가 이미 Document에 담아야 한다.
CommandLine은 실제 public action의 argv만 담는 여섯 번째 block이다. presenter는 행동의
원문·토큰·필수 입력을 소유하며, cliui는 주어진 shell별 인용과 복사 가능한 한 줄 배치만
소유한다. shell을 추측하거나 argv를 실행하지 않는다. 세부 계약은 B6.5를 따른다.
폭/SGR 옵션의 안전한
정규화는 renderer가 수행하며 표시 값이나 섹션을 잘라 invalid model을 숨기지 않는다.

`parseRootOptions`는 실패해도 그 지점까지의 rootOptions를 반환한다. CLI가 앞서 유효한
format이 있었는지 판단하는 데 쓰며 raw argv를 진단/renderer에 일괄 전달하지 않는다.
실제 enum·return error는 invalid flag/env를 caller가 구분할 수 있게 하되 원문 값을 출력하지 않는다.
root parser는 flag 문법을, Select는 flag/env 값 선택과 enum 검증을 소유한다. root parser는
format token을 읽는 즉시 `Select(flag, absentEnv)`로 값 검증을 위임하고 유효값만 보관한다.
전체 root 파싱 뒤의 Select도 같은 함수를 사용하므로 flag/env 허용값을 중복 정의하지 않는다. DetectAgent는
host가 읽은 허용 목록의 임시 map만 받아 일치 규칙을 적용하고 Signal 이름만 반환한다.
raw 환경 map은 결과나 Document에 보관하지 않는다. Resolve의 error는 machine-only에 대한
명시 text 요청처럼 capability 충돌만 나타내며 이미 계산한 JSON 오류 출력 결정을 함께 반환한다.
observer는 `App`의 함수 의존성으로 교체 가능하게 하고 `IO`의 기존 In/Out/Err 계약은 유지한다.
`presentation.Build`의 입력 byte는 이미 public projection을 통과한 envelope다. normalized
public node는 presentation 내부에만 둔다. alias command·실행 context·Go domain object를
받지 않는다. `cliui.Render`에는 이 envelope/node도 넘기지 않고 Document만 전달한다.
format resolver는 stdout TTY·agent 신호로 json/text를 고르고, text options resolver는 실제
출력 대상 stream의 capability로 폭·강조를 정한다. CLI host는 runtime 플랫폼에 따라 고정
CommandShells를 options에 공급한다. shell 감지를 위해 환경변수·프로세스 탐색을 추가하지
않는다. 두 resolver는 서로의 결정을 바꾸지 않는다.
공통 출력 함수는 기존 App.Run의 성공/fail과 discovery가 함께 호출하는 CLI 내부 함수로 두며
protocol 인코더·presentation.Build·cliui.Render·writer·exit 정책만 조합한다.

### B1. CLI host adapter: 관측만 수행

`internal/cli`에 `observeOutputEnvironment(IO)`를 둔다. 실제 `streams.Out`에 대해
`golang.org/x/term.IsTerminal`을 사용하고, fd를 확인할 수 없는 writer는 non-TTY로 처리한다.
`os.Stdout`이나 stdin·stderr TTY로 성공 형식을 추측하지 않는다. stderr의 TTY 차이는
형식 선택을 바꾸지 않는다. 스타일·폭은 실제 stdout/stderr 각각을 관측해 별도로 적용한다.
`term.GetSize` 실패·폭 0·음수는 fallback 80 columns이고 렌더링 실패로 처리하지 않는다.
관측 실패·writer가 fd를 제공하지 않음은 각각 non-TTY/ANSI=false/width=0으로 낮춘다.
snapshot은 호출마다 하나고 호출 중 다시 probe하거나 실패/경고를 추가하지 않는다.

환경변수는 agent/format allowlist와 스타일 전용 `TERM`·`NO_COLOR`만 `LookupEnv`로 읽는다.
`os.Environ` 전체 스캔·로그·subprocess·
프로세스 트리·파일 탐색·추가 저장소 조회를 하지 않는다. App의 주입 가능한 observer로
테스트가 synthetic 환경을 공급한다. 다른 요청의 mode를 App 전역 상태에 누적하지 않는다.

### B2. 루트 파서: 문법·원래 argv 위치만 소유

신규 `internal/cli/root_options.go`가 command 선택 전에 연속된 루트 옵션을 파싱한다.
허용 루트 옵션은 `--format`과 기존 prefix형 `--isolated-root`이며 순서는 자유다.
기존 command-local `--isolated-root`도 유지하고 양쪽 지정 시 기존 중복 오류를 유지한다.

```sh
devtools --format text command list
devtools --isolated-root /tmp/example --format=json command list
devtools --format=text run --output redacted test --format json
devtools --format=json run -- node app.js --format text
```

셋째 예의 마지막 `--format json`은 자식 인자다. 넷째 예에서 `--` 뒤도 모두 자식 인자다.
루트 파서는 첫 command token에서 멈춘다. 전체 argv에서 format을 검색·제거하지 않는다.
`devtools command list --format text`는 command-local unknown option이다. help가 올바른
루트 위치를 안내한다. 이 제한은 child 인자 보호 및 기존 `--output` 의미 보존을 위한 것이다.
루트 `--format`만 있고 명령이 없으면 기존 bare 호출처럼 help다. `--version`, `--help`, `-h`
단축 경로는 루트 옵션 제거 후에도 유지한다. 기존 `--isolated-root`의 명령 필수 규칙은 보존한다.

`--format=VALUE`와 `--format VALUE`를 지원한다. 중복·빈 값·알 수 없는 값·누락 값은
`invalid_argument`/2이며 handler와 저장소 접근 전에 거절한다. `--format` 자체의 파싱 오류는
유효 표시 모드가 확정되지 않았으므로 언제나 JSON stderr다. 같은 루트 묶음의 다른 오류는
루트 파싱이 끝나기 전에 오류로 멈췄더라도 이미 유효 format이 확인된 경우 해당 모드를
사용하며 그렇지 않으면 JSON이다. 뒤쪽 flag를 찾기 위해 실패한 token을 건너뛰지 않는다.
루트 파싱·format 확정 뒤 context 취소를 검사하여 정상 입력의 취소 오류도 같은 형식을 쓴다.
모든 argv source
offset·direct executable offset은 원래 argv 기준으로 유지한다. 단순 prefix 길이 덧셈 대신
소비한 원래 인덱스 mapping을 전달하여 root 옵션 순서와 `--isolated-root` 오류를 함께 검증한다.
mapping 길이는 `len(CommandArgs)+1`, 마지막 값은 원래 argv 길이다. missing argument처럼
parser가 EOF 위치를 반환해도 매핑한다. alias는 canonical 이름의 단어 수가 아닌 실제 소비한
token 위치를 사용한다. kind=argv인 source의 offset/end_offset만 정확히 한 번 매핑하고
JSON byte offset·설정 line/column은 변경하지 않는다. 기존 rootArgumentOffset 덧셈 경로를
함께 남기지 않는다. root parser의 오류 source는 처음부터 원 index, unknownCommand의
source는 CommandArgs 기준, parseRequest의 source는 실제 rest 기준이다. 각 상대 index를
그 해당 경계에서 한 번만 매핑한다. handler에 넘기는 Invocation의 OptionOffsets·
ExecutableOffset은 이미 원 index이며, handler-origin 오류나 최종 fail에서 다시 매핑하지 않는다.
unknown root token은 신규 root parser가 추측해 제거하지 않고 기존
command/help 분류로 넘겨 기존 unknown-command 오류를 보존한다.

#### B2.1. 실행 전 결정 순서와 오류 우선순위

1. private raw entrypoint는 기존 분기로 종료한다. public root 옵션·환경 format을 적용하지 않는다.
2. OS snapshot과 root prefix 문법을 처리한다. malformed/duplicate format은 JSON/2로 종료한다.
   root의 다른 문법 오류는 앞에서 이미 유효한 format이 있으면 그것을 쓰고 없으면 JSON이다.
3. Select로 flag > env > default를 고른다. flag가 있으면 잘못된 env는 무시한다. 선택된
   format 값이 잘못되면 JSON/2이며 context 취소보다 우선한다. 공통 root/env source만 생성한다.
4. 기존 command/discovery 파서를 수행해 effective action과 parse 오류를 **일단 보관**한다.
   unknown option을 건너뛰거나 모든 --help를 미리 검색하지 않는다. handler·store는 호출하지 않는다.
5. effective help이면 native, normal schema 또는 schema-target parse 오류이면 machine-only,
   일반 결과이면 adaptable, 나머지는 기존 native 소유권으로 분류한다. machine-only 오류는 JSON이다.
6. 기존 ContextError를 확인한다. 명시 취소는 canceled/130, deadline 만료는 deadline_exceeded/3을
   정한 형식으로 반환한다. 둘을 같은 canceled 상태로 합치지 않는다.
   취소가 없으면 보관한 기존 parse/discovery 오류를 먼저 반환한다. 그 다음 유효한 normal
   schema에 대한 explicit text 충돌을 JSON invalid_argument/2로 반환한다.
7. 나머지 플랫폼/선택/저장소 준비·handler 호출은 기존 순서로 수행한다. publication 뒤
   추가 취소 검사를 넣어 성공 결과를 취소로 바꾸지 않는다.

| 입력 상황 | 결정 |
| --- | --- |
| `--format text schema --help` | 기존 effective help, text stdout |
| `--format text schema command list --help` | 기존 invalid schema target, JSON stderr/2 |
| `--format text config schema --help` | 기존 effective help, text stdout |
| `--format text config schema --bogus --help` | 기존 unknown option, JSON stderr/2 |
| `--format text command list --bogus --help` | 기존 unknown option, text stderr/2 |
| `--format text run test --help` | child --help; child stream/exit 유지 |
| `--format text schema version` | 유효 normal schema와 text 충돌, JSON stderr/2 |
| 잘못된 env + 유효 `--format json` | env 무시, JSON |
| 잘못된 env + flag 없음 + help | 신규 env 오류 JSON/2, help 실행 안 함 |

신규 format 오류는 phase=parse, effects=none, retry=after_input_change다. flag source는
kind=argv/path=[format] 및 원 offset, env source는 기존 허용 enum인
kind=system/path=[env,DEVTOOLS_FORMAT]을 사용한다. `env`라는 Source.kind나 임의 schema
필드를 새로 만들지 않는다. invalid raw 값은 message/facts/source 어디에도 출력하지 않는다.
기존 parse/discovery 오류의 본문·code·관계는 그대로 두고 새 format 안내는 공통 신규 오류에만 둔다.
이미 취소된 호출의 ContextError operation은 현재와 같은 devtools를 유지한다. 위 metadata
분류는 출력 결정을 위한 것이며 취소 전에 command handler가 실행됐다는 뜻이 아니다.

### B3. format resolver: 순수한 선택 함수

신규 `internal/cli/presentation` 패키지에 작은 값 타입과 순수 함수를 둔다.
계약은 `DetectAgent`로 신호를 정규화하고 `Select`로 flag/env의 유효 format과 출처를 고른 뒤,
`Resolve(selection, detected, stdoutTerminal, capability)`로 표시 결정을 반환하는 순서다.
CLI는 OS 관측·flag 문법을 소유하고 순수 함수가 값 검증·선택 정책을 소유한다.
여기서 domain/service/store/execution이나 OS API를 import하지 않는다.

우선순위는 다음과 같다.

1. 명시 `--format`이 있으면 그것을 사용한다. 명시 `auto`도 환경변수보다 우선한다.
2. 없으면 비어 있지 않은 `DEVTOOLS_FORMAT`을 사용한다.
3. 둘 다 없으면 단일 `DefaultFormat` 상수다. 최종 릴리스 값은 auto다.
4. 유효한 explicit flag가 있으면 잘못된 `DEVTOOLS_FORMAT`을 검증·적용하지 않는다.
5. flag가 없을 때 잘못된 비어 있지 않은 환경값은 JSON stderr `invalid_argument`/2로 실패한다.
6. `auto`는 알려진 agent 신호 → JSON, stdout non-TTY → JSON, 나머지 → text다.

값은 ASCII 소문자 세 개만 허용한다. 빈 환경값은 미설정과 같고, 공백·대문자·`false` 등은
유효 format이 아니다. 모드가 선택되면 성공·devtools 실패 모두 같은 mode를 유지한다.
stdout은 파이프이고 stderr만 터미널인 경우에도 devtools 실패는 JSON이다.
DefaultFormat은 Select와 global_options metadata가 함께 참조한다. T01–T04의 json 유지와
T05의 auto 전환에서 별도 resolver 예외나 숨은 feature flag를 추가하지 않는다. 이 상수 변경과
최종 metadata·default 검사를 같은 변경에 묶는다. 환경의 빈 문자열은 미설정이며 flag의 빈
문자열은 오류다. Detection.Signals는 고정 allowlist 선언 순서로 반환해 map 순서에 의존하지 않는다.

agent 신호의 최초 목록:

| 환경변수 | 참으로 보는 조건 | 근거/한계 |
| --- | --- | --- |
| `AI_AGENT` | trim 후 비어 있지 않고 소문자 비교로 `0`, `false`가 아님 | 공통 관례, 필수 표준 아님; suffix·버전 파싱 안 함 |
| `CODEX_THREAD_ID` | 같은 nonempty-positive 규칙 | 현재 세션 직접 관측; UUID 형식 검사·값 보관 안 함 |
| `CODEX_CI`, `CLAUDECODE`, `CLAUDE_CODE`, `CURSOR_AGENT`, `GEMINI_CLI`, `OPENCODE` | trim·소문자화 후 `1`, `true` | 알려진 agent별 boolean 관례; synthetic 계약 검사로 고정 |
| `OPENCODE_CLIENT` | 같은 nonempty-positive 규칙 | 알려진 agent별 문자열 관례 |

`CODEX_SANDBOX*`, `CI`, `TERM`, `NO_COLOR`, editor·Git 설정은 agent 식별에 사용하지 않는다.
샌드박스 전용 변수로 agent를 판정할 이유가 없고 현재 관측에서도 없어질 수 있다.
새 신호는 근거·값 규칙·false-positive 검사와 함께 코드 목록에 추가한다. third-party 감지
라이브러리는 도입하지 않는다. 진단용 Decision에는 출처와 신호 이름만 두고 값은 담지 않는다.
`--format text`로 감지를 덮어쓸 수 있다. 신호가 자식 셸에 상속되면 사람의 셸도 감지될 수
있으며, 알려지지 않은 agent PTY가 text를 받는 반대 경우도 문서에 명시한다.

### B4. dispatcher: 실행·출력 소유권 유지

`Command.OutputMode`는 계속 `json | text | artifact | passthrough`다. 이는 stdout을
누가 생성하는지와 handler 반환형을 규정한다. format은 구조화된 결과를 어떻게 표시할지다.
`OutputJSON` 이름은 기존 enum·schema 호환 때문에 유지하지만 protocol 전환 문서에
'구조화 결과의 JSON 표현 계약'임을 명시하고 presentation metadata를 함께 조회하게 한다.

| command 성격 | stdout | devtools 실패 |
| --- | --- | --- |
| 일반 `OutputJSON` | 선택된 json/text presenter | 선택된 형식 |
| `schema`의 모든 discovery 경로, `config schema` | 항상 JSON | 항상 JSON |
| help·bare·`--help` | 기존 help text | 선택된 형식 |
| completion·license | 기존 artifact 원문 | 선택된 형식 |
| command run·run·cmd run·proxy run | 기존 child stream | devtools가 생성한 실패만 선택된 형식 |
| `__complete`, private helper·IPC | 기존 private wire | 기존 private 규칙 |

기계 discovery의 명시 `--format text`는 handler 전 `invalid_argument`/2 JSON stderr로
거절한다. `--format auto/json`과 환경값은 해당 경로의 JSON 고정을 허용한다. help·artifact·
passthrough에서는 유효 format이 stdout을 변환하지 않으며 devtools 오류 표시에만 적용된다.
예: `--format json completion bash`도 completion script이고, `--format json run test`도
child stream이다. `output_mode`와 함께 읽어야 한다. private helper entrypoint는 public root
옵션을 받도록 확장하지 않으며 기존 Windows preflight와 IPC 오류는 별도 wire로 유지한다.

능력 정책은 command 이름만 보기 전에 **실제 수행 동작**을 분류한다. `schema --help`,
`config schema --help`, 명령의 `--help`는 help 동작이므로 정상 text를 내며 JSON 고정이나
text 거절을 적용하지 않는다. `schema`의 잘못된 대상은 discovery 오류이므로 JSON이다.
잘못된 global format/env는 이 분류 전 bootstrap 오류로서 JSON이다. private helper는 기존처럼
public Run 이전 분기로 진입하며 글로벌 env가 잘못되어도 helper wire를 중단시키지 않는다.

성공 반환형 검사(`processResult`/nil/public result), child exit·signal, 취소, prompt 판단,
redaction raw 예외·capture log 정책은 바꾸지 않는다. 표시 mode가 noninteractive 여부나
passphrase 입력 여부를 바꾸지 않는다. 본 범위에서 agent 감지는 표시 형식에만 영향을 준다.

### B5. public projection와 직렬화: 같은 공개 데이터만 제공

일반 성공은 기존 `protocol.BindGuidance(data, invocation)`를 top-level 출력 준비에서 한 번 적용한다.
오류도 기존 invocation scope 보강·BindGuidance·`protocol.Public`를 top-level 출력 준비에서
한 번 적용한다. protocol 내부의 기존 nested cause 정규화는 그대로 유지한다.
그 후 기존 `protocol.Success`/`Failure`로 단 한 번 public envelope를 buffer에 직렬화한다.
JSON 모드는 이 byte를 그대로 출력한다. text 모드는 같은 byte에서 `data` 또는 `error`만
정규화하여 읽으며 decoder의 `UseNumber`로 큰 정수·revision의 정밀도를 보존한다.

Go struct reflection으로 숨겨진 필드를 읽거나 domain object를 presenter에 넘기지 않는다.
`json:"-"`, custom marshaler, omitempty, 기존 public error 축약을 우회하지 않는다.
별도 DB 조회·권한 확대·secret 복호화·현재 상태 재조회 없이 이미 반환한 결과만 표시한다.
같은 UUID replay는 저장된 결과를 표시하며 live 상태와 합쳐 새 결과처럼 보이지 않게 한다.

presenter는 registry/plugin 구조 대신 canonical command 이름에 대한 작은 adapter switch와
공통 Document builder를 사용한다. JSON bytes와 public error만 presentation에
전달한다. 여기서 public error는 **직렬화된 envelope에서 정규화한 error node**를 뜻하며,
원래 `*protocol.Error` 객체를 별도 전달하지 않는다. raw envelope를 문서화한 tree로 보여주는
fallback을 반드시 제공하여 새 command가
추가됐을 때 JSON blob 또는 '성공' 한 줄로 정보를 잃지 않게 한다.

#### B5.0. view 선택·필드 coverage·값 타입

`presentation.DefinitionFor(canonicalCommand)`는 package-local 정적 정의에서 view family와
그 command의 required/optional/known-omitted field path를 돌려준다. 동적 registry·plugin이나
command 문자열 부분 일치로 response shape를 추측하는 경로는 만들지 않는다. alias는 CLI가
이미 canonical 이름으로 바꿔 전달한다. 명시 정의가 없는 신규 명령은 Generic이다.
T02/T03은 구현한 command마다 source output schema와 독립 fixture에 이 정의를 연결한다.
모든 189개에 전용 view가 필요하다는 뜻은 아니지만, 전용 view가 있는 명령은 필드별 표시/
명시 생략/unknown Details 중 하나로 다뤄져야 한다. 비어 있지 않은 warnings·진단·status·effects·scope·
revision·ID는 omitted로 선언할 수 없다. 알 수 없는 status enum은 중립 role과 원문으로 표시한다.
알려진 빈 warning array의 섹션만 생략할 수 있으며, warning field가 없다는 이유로
`No warnings` 같은 새 판정을 만들지 않는다. 이 규칙은 실제 경고 항목을 생략하는 권한이 아니다.

public envelope는 하나의 JSON 값·envelope version 1·ok/data 또는 error 규칙을 확인하고
UseNumber로 한 번 decode한다. known required path가 없거나 타입이 다르면 partial 요약을
성공시키지 않고 **전체 node의 Generic Document**로 돌아간다. projection/encode는 반복하지 않는다.
generic object key는 사전순, array는 원 순서다. json.Number의 원 lexical text를 그대로
사용하며 float64로 변환하지 않는다. null·빈 object/array·빈 문자열·false·0은 서로 다른
literal로 표시하고, 없는 path를 'null'이나 빈 값으로 만들어 넣지 않는다.

Flow=prose는 원문 의미를 유지한 wrap만 허용한다. Flow=verbatim은 ID·revision·URL·cursor·
argv token·값 literal의 개행/생략을 금지한다. JSON 인용은 presenter가 public literal/token을
문자열로 표시할 때만 사용하며 renderer는 backslash를 다시 escape하지 않는다. Next의
실행 가능한 public action은 JSON token 문자열 대신 원 argv를 CommandLine에 담아 B6.5의
shell 인용을 적용한다. raw C0/C1·
ESC·bidi만 화면 안전 escape로 바꾼다. generic response 크기·목록 개수·문서 길이에 새
임의 cutoff를 넣지 않고, 기존 source의 bounded/pagination 계약을 따른다.

format 선택은 CLI 호출의 출력 설정이다. `Request.Options`, InvocationContext의 실행 선택,
task/values 요청 body·fingerprint·revision·저장 replay 결과·실행 argv/environment snapshot에
format/style을 추가하지 않는다. CLI는 설정을 별도 invocation-local 출력 상태로만 보관한다.
실행 환경의 기존 환경변수 상속 규칙도 변경하지 않는다. 같은 UUID·같은 업무 입력의 재조회/
허용된 retry는 json/text 선택만 바꿔도 같은 요청 identity를 유지해야 한다.

#### B5.1. 오류·경고·안내의 공통 정본과 drift 방지

JSON과 text는 **같은 public envelope의 진단**을 소비한다. 기존 producer와 public
projection을 오류·경고·안내 내용의 정본으로 유지하고 presenter용 문구 사본·번역·별도
message catalog를 만들지 않는다. 공통 producer에는 기존 handler/service의 공개 메시지,
protocol.Public가 정규화한 오류와 기본 remedy, BindGuidance로 scope가 고정된 행동이 포함된다.

| 항목 | 정본·text 계약 |
| --- | --- |
| `error.code`·exit code | 기존 공개 code와 종료 상태를 그대로 보존; label/색에서 새 code 생성 금지 |
| error/check/cause의 `message` | public 원문을 그대로 Document에 넣고 줄바꿈·terminal control escape만 renderer에서 적용 |
| `warnings` 등 명시 경고 | 같은 공개 경고 항목·문자열·대상 연결을 모두 보존; 요약·생략·의미 강화/완화 금지 |
| `remedies`·`next_actions` | 원문 message·argv token 순서·required_inputs·effect·scope를 유지; 새 행동이나 옵션을 presenter가 합성하지 않음 |
| `retry`·`effects`·source·target·상태 | 같은 공개 값 보존; unknown을 none/실패/성공으로 추측하지 않음 |
| `ERROR`, `WARN`, `Next`, 섹션 label | UI의 고정 구조 label만 허용; 진단 원문을 대체하거나 새 진단 사실을 추가하지 않음 |

severity role은 error 응답·명시 warning 필드·알려진 check/state enum에서만 도출한다.
영어 메시지의 단어·부분 문자열을 검색해서 severity나 retry/effect를 추론하지 않는다.
현재 응답에 없는 severity 필드를 style 때문에 JSON에 추가하지 않는다. 알 수 없는 진단
필드는 기존 unknown Details로 유지하며 임의 성공·경고 요약을 만들지 않는다.
warning/check/cause/action 항목은 원래 대상별 연결과 순서를 보존한다. 배치를 그룹으로
바꾸더라도 진단의 내용과 대상·원인·권고 관계를 끊지 않으며 중요 경고는 dim하지 않는다.

진단에 원래 해결 행동이 없으면 presenter가 명령을 추측해 Next를 만들어 내지 않는다.
기존 public 안전 조회 행동이 있을 때만 다음 행동에 표시한다. 새로운 설명이나 remedy가
필요하면 기존 producer/공통 action 생성기를 고쳐 JSON과 text에 함께 반영하는 별도 명시
변경으로 취급하고, presentation에만 보완 문구를 넣어 drift를 만들지 않는다.

표시 차이는 SGR·줄바꿈·들여쓰기·고정 label과 동일 argv의 shell 인용이다. diagnostic 메시지 node와 action token을
Document에서 원문과 대조하고, render 후 escape/wrap을 감안해 의미·누락을 다시 검사한다.
검사 대상은 top-level error, nested check/cause, 성공 응답 안의 warnings/unresolved,
partial/unknown·same-request retry·권한/required-input 안내와 새 진단 field다. mutation을
두 번 호출해 JSON/text를 비교하지 않고 **한 번 얻은 public envelope**를 양 표현에 사용한다.

기존 진단의 내용 보존과 다음의 명시 변경을 구분한다. `--format`/DEVTOOLS_FORMAT 입력
오류는 신규 기능의 새 진단이고, B7의 post-handler render/I/O 실패 effects:unknown·
retry:inspect_first는 초기 계획에서 정한 공통 진단 보강이다. 이 두 변경도 JSON과 text의
같은 정본을 사용하며 'text만 친절하게 수정'하는 예외로 삼지 않는다.

### B6. CLI UI: 공통 표현·레이아웃·스타일 소유

text는 영어 label·UTF-8의 사람용 표현이다. 기존 CLI 언어와 일치시키며 JSON key는
후속 JSON 조회에서 찾을 수 있게 한다. text의 공백·열 정렬·SGR byte는 machine 파싱 계약이
아니다. 호환 터미널에는 bold·dim·표준 전경색을 제한적으로 사용하고, non-TTY에는 SGR을
내보내지 않는다. 실제 색상·배경은 사용자의 terminal palette를 따른다. spinner·화면 지우기·
커서 이동·역상·깜박임·OSC hyperlink·배경색 블록은 도입하지 않는다.

#### B6.1. 형식과 터미널 장식을 별도로 결정

| 실제 출력 대상과 선택 결과 | 장식 계약 |
| --- | --- |
| json / schema 고정 / artifact / child / private wire | 새 presenter의 SGR·줄바꿈·표를 절대로 적용하지 않음 |
| text, 실제 출력 stream이 non-TTY | plain UTF-8, escape sequence 0개 |
| text, TTY지만 TERM=dumb 또는 기존 VT 지원 관측 실패 | plain UTF-8로 fallback; 명령 실패로 바꾸지 않음 |
| text, TTY·VT 지원, NO_COLOR 비어 있지 않음 | 전경색 없음; 의미 계층의 bold·dim은 유지 |
| text, TTY·VT 지원, 위 제한 없음 | B6.2의 최소 SGR 강조 |

`--format text`는 redirect에 색을 강제하는 옵션이 아니다. stdout이 file, stderr가 TTY인
명시 text 호출은 성공 output은 plain이고 devtools 오류는 stderr capability에 따라 강조한다.
반대로 auto가 stdout pipe 때문에 json이면 stderr가 TTY여도 JSON 오류다. `NO_COLOR=0`도
비어 있지 않으므로 색을 끄고 `NO_COLOR=`는 끄지 않는다. NO_COLOR는 색만 제어하며
agent 감지·format·bold/dim을 제어하지 않는다. TERM=dumb은 모든 SGR을 끈다.
이번에는 `--color`, `--style`, FORCE_COLOR/CLICOLOR 설정이나 TOML 필드를 추가하지 않는다.

Unix에서는 실제 TTY와 비어 있지 않은 TERM≠dumb을 기본 capability로 본다. Windows에서는
실제 출력 handle의 기존 console mode만 GetConsoleMode로 읽고 PROCESSED_OUTPUT과
VIRTUAL_TERMINAL_PROCESSING이 모두 켜져 있을 때만 SGR을 쓴다. 실패/미지원은 plain이다.
이번 표시 기능은 SetConsoleMode로 출력 mode를 바꾸지 않는다. 색을 위해 host state의
저장·복원 실패 경로를 새로 만들지 않으며, 기존 passphrase 입력 mode 처리는 그대로 둔다.
앞 단계의 'VT enable 후 복원' 제안은 이 read-only 관측 결정으로 대체한다.

#### B6.2. 역할별 스타일 토큰

| 역할 | 표시 | 제한 |
| --- | --- | --- |
| 결과 제목·섹션 제목·table header | 기본 전경색 + bold | title은 한 줄, 반복 브랜드 배너 없음 |
| 식별 대상·중요 값 | 기본 전경색, 필요한 대상만 bold | 모든 값을 강조해 계층을 평탄화하지 않음 |
| scope label·보조 설명·출처 | dim, 기본 전경색 | 필수 ID·상태·원인·복구 행동은 dim 처리하지 않음 |
| 성공 | `OK` / `READY` / `CHANGED` + green | label만 색칠; 성공하지 않은 대상까지 green으로 감싸지 않음 |
| 주의·부분 실패·불확실 | `WARN` / `PARTIAL` / `UNKNOWN` + yellow | unknown은 완료로 보이지 않게 원문 상태·효과를 병기 |
| 오류·미준비 | `ERROR` / `FAIL` / `NOT READY` + red | error code·메시지·대상은 기본 전경색으로 읽기 쉽게 유지 |
| 미리보기·정보·다음 행동 | `PREVIEW` / `INFO` / `Next` + cyan | 실제 적용/자동 실행을 뜻하지 않음 |
| 변경 없음·재전송 | `NO CHANGE` / `REPLAYED`, 기본 전경색 | dim으로 숨기지 않고 의미를 명시 |

색은 항상 같은 문자 label을 보강한다. 아이콘·emoji·red/green 차이만으로 상태를 구분하지
않는다. 임의의 user state string을 해석해 색칠하지 않고 command adapter가 알고 있는
상태 enum/필드에서 semantic role을 정한다. bold/dim/색은 해당 span 안에서 닫고 마지막에
reset한다. 스타일링 전 모든 user string을 escape하여 raw SGR이 합쳐지지 않게 한다.
어두운·밝은 배경에서 필수 정보는 기본 전경색을 사용한다. dim은 보조 정보에만 한정하고
지원하지 않는 터미널에서도 문구·순서·여백으로 같은 계층을 읽을 수 있어야 한다.

#### B6.3. 레이아웃 primitive와 폭

구현은 `internal/cliui`의 Title, Scope, Status, Table, KeyValue, Section, Paragraph, Details,
Next라는 작은 primitive를 사용한다. 이 primitive가 간격·강조·폭을 소유하고 presentation의
command adapter는 섹션 순서·필드 선택·semantic status만 정한다. Document/Block/Span은
cliui 경계의 닫힌 값 타입이고 domain DTO·사용자 확장 plugin API로 노출하지 않는다.

- title 다음에 scope를 같은 줄에 둘 수 있으면 배치하고, 넘치면 scope를 다음 줄로 내린다.
  scope는 항상 유지한다. 최상위 섹션 사이는 빈 줄 하나, label/value는 2칸 간격,
  하위 들여쓰기는 2칸으로 고정한다. 불필요한 선·박스 테두리는 넣지 않는다.
- table은 비교 가능한 필드 최대 4개, 열 간격 2칸이다. 같은 표의 모든 row에 대해
  header/cell의 실제 표시 폭 최댓값과 열 간격이 맞아야 한다. 80 columns 이상이고 필수 열이
  들어갈 때만 표를 사용한다. 최대 layout 폭은 120 columns이며 넓은 화면도 긴 한 줄로 늘리지 않는다.
- 80 columns 미만이거나 긴 이름·열 값 때문에 맞지 않으면 같은 row 순서의 stacked record로
  전환한다. 필수 열을 삭제하거나 ID/URL/cursor를 잘라 table을 유지하지 않는다.
- detail의 label 열은 가장 긴 label 기준 최대 24 columns, 값을 넣을 공간이 24 columns
  미만이면 label 다음 줄에 value를 들여쓴다. Paragraph/원인 설명은 가능한 whitespace에서
  wrap하고 프로그램이 나눈 후속 줄에 같은 값 들여쓰기를 적용한다. whitespace 없는 긴
  segment는 자르지 않고 자연 wrap을 허용하며 그 자연 줄의 hanging indent는 보장하지 않는다.
- ID·digest·cursor·URL·argv token·value 원문에는 프로그램이 개행/ellipsis를 삽입하지 않는다.
  좁은 터미널에서는 별도 줄에서 terminal의 자연 wrap을 허용하며 복사할 원래 token을 보존한다.
  짧은 **요약**을 별도로 만든 경우에만 생략 표시하고 원문 또는 안전한 조회 경로를 남긴다.
- ANSI를 붙이기 전 cell width로 레이아웃을 계산한다. ASCII 1칸, CJK/Hangul Wide/Fullwidth
  2칸, 결합 mark 0칸, Ambiguous 1칸으로 정한다. 기존 x/text/width를 재사용할 수 있다.
  이 정책이 정확한 emoji/ZWJ/grapheme 폭을 보장하지 않으므로 해당 문자열이 table 정렬에
  들어가면 보수적으로 stacked record로 전환하고 code point를 잘라내지 않는다.
  판별은 combining mark·Unicode So/Sk·ZWJ·variation selector·Hangul Jamo 범위가 table의
  label/cell에 하나라도 있는 경우로 고정한다. Jamo는 U+1100–11FF/A960–A97F/D7B0–D7FF,
  variation selector는 U+FE00–FE0F/E0100–E01EF다. 넓은 fallback으로 정렬을 보수적으로 포기하며
  emoji/grapheme를 완전 식별하는 새 라이브러리를 도입하지 않는다.
- terminal 크기는 호출 시 한 번 관측한다. 실행 중 resize에 맞춰 화면을 다시 그리거나
  내용을 반복 출력하지 않는다. 40/79/80/120 columns와 관측 실패 80 fallback을 fixture로 검증한다.
  Options.Width가 0/음수이면 80, 양수면 min(width,120)이다. 양수가 40 미만이어도
  더 큰 폭을 가정하지 않고 stacked/필요한 최소 들여쓰기로 낮춘다. Options.SGR=false면
  Color=true 입력도 false로 정규화한다. 빈 table은 header-only 대신 해당 adapter의 빈 목록
  문구를 쓰고, Generic의 빈 array/object는 []/{} literal로 남긴다.

#### B6.4. 결과별 읽기 순서와 기본 밀도

목록은 제목·scope·개수 → 핵심 비교 열/record → 필요한 row detail → continuation 순서다.
상세는 대상·상태 → 실행/선택 정보 → requirements/bindings/provenance 등 보조 섹션 순서다.
변경은 상태·대상 → 실제 효과 → revision/request metadata 순서다. preview/replay는 첫 줄에
명시한다. 진단은 대상·판정 → 실패 항목 → 원인 → effects/retry → Next 순서다.
전체 `ok:true`만으로 진단 제목을 OK로 만들지 않는다.

기존 초안의 '알려진 모든 public field를 무조건 출력'은 기본값·빈 배열·중복 metadata로
사람용 출력을 다시 복잡하게 만들 수 있어 수정한다. 전용 view는 **필수 의미를 보존하는 요약**이다.
생략 가능한 알려진 기본값·빈 보조 구조는 adapter가 field path별로 명시한다. 전역적으로
false/0/null/빈 문자열을 지우지 않는다. scope·상태·효과·replay/preview·오류·필수 입력·ID·
revision·cursor는 생략 목록에 넣지 않는다. 인식하지 못한 새 public field는 Details tree로
보여 주며 generic fallback은 전체 공개 node를 유지한다. 상세가 더 필요하면 이미 있는 안전한
read-only inspect/show 행동이 public 결과에 이미 있으면 연결하고, 없으면 presenter가
임의 행동을 만들지 않는다. mutation을 JSON으로 다시 실행하라고 안내하지 않는다.

아래는 synthetic public 응답 데이터의 layout 예시다. 예시 Next 문구·argv도 실제 구현에서는
생성된 public remedy를 그대로 사용하며 이 문서의 예시를 새 안내 정본으로 복사하지 않는다.
code fence는 ANSI를 렌더링하지 않으므로 스타일은
B6.2 토큰을 따른다. 실제 handler를 실행한 결과나 최종 플랫폼 검증으로 취급하지 않는다.

```text
Commands (3)  profile: demo

Command       Environment  Values  Exec
dev           local        On      "pnpm" "dev"
build         common       Off     "pnpm" "build"
test          common       Off     "go" "test" "./..."
```

```text
Commands (3)
Profile  demo

Command      dev
Environment  local
Values       On
Exec         "pnpm" "dev"

Command      build
Environment  common
Values       Off
Exec         "pnpm" "build"

Command      test
Environment  common
Values       Off
Exec         "go" "test" "./..."
```

```text
ERROR  command_not_found
The selected project command is not defined.

Target   serve
Profile  demo
Effects  none
Retry    after_input_change

Next
  List configured commands.

devtools command list --dir /workspace/demo
```

`Commands`/table header와 `Next`는 bold, profile label 등 보조 metadata는 dim,
ERROR label은 red+bold, 오류 메시지·target·effects/retry·argv는 기본 전경색이다.
문자형 상태가 항상 있으므로 mono fallback에서도 읽기 순서·판정을 잃지 않는다.

| 결과 | 필수 표시 계약 |
| --- | --- |
| 목록 | 대상 scope, 의미 있는 열, 빈 목록 문구, 조회된 개수, 있을 때 next_cursor와 같은 범위의 이어읽기 방법 |
| 단일 조회·explain | 식별자·상태 먼저, 설정·값 없는 provenance·bindings·requirements를 항목별 표시 |
| mutation·preview | 대상·changed·replayed·dry_run을 구분; 실제 미리보기는 적용했다고 쓰지 않음; revision·digest·request ID 보존 |
| readiness·doctor·check | `ok`와 ready/valid/closable 분리; 실패 check·원인·필수 입력·retry/effects·후속 action 포함 |
| 다중 대상 부분 실패 | 대상별 성공/실패·이미 발생한 효과·unknown·남은 조치와 기존 요청 재시도 규칙 표시 |
| 작업·validation | lifecycle/completion/execution 상태를 구분; blocker·문서·의존성·validation 및 continuation 보존 |
| 로그·값 조회 | `content`·`value`의 newline·빈 문자열·제어문자를 구별; bare raw stream으로 오해시키지 않음 |
| 기타 공개 결과 | 정렬된 key의 들여쓰기 tree, array 순서 보존, null/없음/false/0/빈 문자열 구분 |

전용 view는 B6.4의 요약·명시 생략 규칙을 따르고, 알 수 없는 공개 필드는 Details tree로 남긴다.
새 오류 reason·중요 metadata가 사라지지 않게 adapter별 필수 의미 목록을 검사한다.
list row의 긴 exec/배열은 짧은 요약 후 해당 row의 Details에 원문을 보존한다. ID·revision·
cursor·digest·URL·필수 입력은 말줄임하지 않는다. 모든 페이지를 자동 fetch하지 않는다.
사람용 링크도 기존 JSON에 있는 URL만 표시하며 새 credential 포함 URL을 만들지 않는다.

`exec` 조회는 token별 JSON string 인용으로 실행 선택의 원문을 보여 준다. Next/remedy는
같은 public argv를 B6.5의 복사 가능한 한 줄로 표시한다. 두 표현 모두 token 배열 의미를
보존하며 JSON 응답의 argv·action 내용·요청 identity는 바꾸지 않는다.
사용자 문자열의 ESC·CR·NUL·C0/C1 및 bidi 제어문자는 보이는 escape로 치환한다.
보통 한글·Unicode는 보존한다. '정상화된 공개 데이터'는 secret 마스킹 추가 보장이 아니며,
`var get`처럼 의도한 원문 조회는 기존 노출 계약을 그대로 따르므로 실제 값을 로그에 남기지 않는다.

설계 근거: [NO_COLOR](https://no-color.org/)는 비어 있지 않은 값으로 전경색을 끄는 관례이며
bold 등의 장식까지 끄는 표준은 아니다. Windows VT 지원 관측은
[Microsoft console VT 문서](https://learn.microsoft.com/en-us/windows/console/console-virtual-terminal-sequences)를 따른다.
[x/text/width](https://pkg.go.dev/golang.org/x/text/width)는 East Asian Width 분류 도구이며
모든 terminal grapheme의 실제 폭을 알려 주는 도구로 취급하지 않는다.

#### B6.5. Next 명령의 한 줄 복사 계약

사용자 후속 결정: 바로 실행할 수 있는 public action은 전체 줄을 복사해 붙여넣도록
표시한다. 원 argv 배열은 JSON과 Document 안의 정본이고 사람이 읽는 기본 Next에는
명령 줄을 제공한다. 이 결정은 앞 단계의 'Next도 argv 배열만 표시'를 대체한다.

- action 원문 설명은 앞 문단, 명령은 독립된 **한 개 논리 줄**에 둔다. 명령 줄은 0칸
  들여쓰기·기본 전경색·SGR 없음이며 `$`, `>`, `argv:`, 번호·bullet·comment를 붙이지 않는다.
  executable부터 마지막 인자까지 전부 포함한다. 폭에 따른 개행/ellipsis/continuation 문자나
  명령 끝의 공백을 넣지 않는다. terminal 자연 wrap은 허용하되 실제 newline은 추가하지 않는다.
- 공백·작은따옴표·역슬래시·`$`·backtick·glob·`;`·`&` 등의 shell 해석을 차단해 같은 argv를
  전달한다. 읽기 쉬운 안전 token은 그대로 쓰고 필요한 token만 literal quote한다. 단순
  `strings.Join(argv," ")`·JSON 인용·한 shell용 quote의 재사용은 금지한다.
- host가 제공하는 대상은 Unix의 POSIX(sh/bash/zsh)와 Fish, Windows의 PowerShell이다.
  Unix의 두 formatter 결과가 같으면 한 줄만 표시한다. 다르면 각 `Command (sh/bash/zsh)`/
  `Command (fish)` label을 **윗줄**에 두고 각각 한 줄을 제공한다. Windows는 필요할 때
  `Command (PowerShell)` label을 윗줄에 둔다. OS를 실제 활성 shell의 증거로 부르지 않는다.
  `$SHELL`/ComSpec·부모 process tree·subprocess로 shell을 추정하지 않고 새 설정도 추가하지
  않는다. cmd.exe 명령 생성은 이번 범위에 포함하지 않는다.
  CommandShells는 Render 입력을 복사해 읽으며 중복을 제거한다. nil/빈 목록·미지원 enum은
  host 관측으로 보완하지 않고 해당 block을 `Arguments (not copyable)`로 표시한다.
- POSIX는 single-quote literal과 quote 종료/재개, Fish는 별도 single-quote/backslash 규칙,
  PowerShell은 literal quote와 필요 시 call operator를 사용한다. 이는 **표시** 코드이며
  cliui는 shell/handler를 실행하거나 환경을 설정하지 않는다. 인용 문자는 표현이며 action
  token·원문 message·effect/scope·request ID를 수정하지 않는다.
- `required_inputs`가 남은 행동은 `Required inputs`와 기존 토큰을 template/Arguments로
  표시하며 실행 가능한 완성 명령으로 표시하지 않는다. 값을 추측하거나 `<revision>` 같은
  placeholder를 실제 입력으로 채우지 않는다. raw control/개행·NUL·bidi가 있는 token이나
  지원 shell/native argument 전달에서 원 token 보존을 보장할 수 없는 조합은 escaped
  `Arguments (not copyable)`로 표시한다. 실행 가능한 것처럼 잘못된 명령을 생성하지 않는다.
  특히 PowerShell native 전달은 5.1/7의 차이를 검증하고, 빈 인자·내장 double quote 등
  호환되지 않는 조합은 이 fallback으로 남긴다. 일반 경로·scope·ID 안내는 한 줄을 제공한다.
- renderer는 shell 인용 **뒤에** 일반 display escape로 명령을 재가공하지 않는다. 먼저
  복사 가능 여부를 판정하고 지원 literal만 formatter에 전달한다. 일반 escaped text와
  CommandLine의 검사·출력 경로를 구분한다. 최후 emergency는 기존 전체 escaped JSON이다.

synthetic 예시에서 설명과 명령 줄을 따로 복사할 수 있다. 공백 있는 경로도 한 인자다.

```text
Next
  List configured commands.

devtools command list --dir '/workspace/my project'
```

근거: [Fish quoting](https://fishshell.com/docs/current/language.html#quotes)과
[PowerShell quoting](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_quoting_rules),
[native argument parsing](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_parsing)는
quote/escape와 native 전달 규칙을 따로 정의한다. 복사할 명령의 계약은 보기 좋은 문자열이
아니라 **원 argv round-trip**이다. 실제 shell에서 부작용 없는 argv-echo fixture를 실행해
공백·Unicode·빈 값·양 quote·역슬래시·`$()`·glob·`;`·`&`를 대조한다. terminal 폭 40/80/120과
styled/plain에서 명령 논리 줄이 같고 prompt/ANSI/줄바꿈이 섞이지 않는지도 검사한다.

### B7. output writer: stream과 실패 효과 보존

CLI는 canonical byte와 **원래 종료 코드·성공/실패·handler 반환 전후 stage**를 invocation-local
prepared output으로 보관한다. ExitCode는 JSON에 직렬화되지 않으므로 presenter가 error.code에서
역산하지 않는다. success=0, failure=원래 public Error.ExitCode이고 processResult는 이 경로를 우회한다.
controller는 Build→Render로 최종 text 전체를 만든 후 성공은 stdout, 실패는 stderr에 한 번 쓴다.
이 단일 writer 책임은 devtools의 최종 public response/진단 block에 대한 것이다. 기존
passphrase prompt·child stream은 이미 stderr/stdout을 쓸 수 있으며 이 표시 계층이 다시
감싸거나 지우지 않는다. 따라서 interactive prompt나 child stderr 뒤에는 devtools의 JSON
오류가 올 수 있고 stderr 전체가 하나의 JSON 값이라는 보장은 하지 않는다. 새 framing이나
구분 protocol은 만들지 않으며, noninteractive/정적 조회의 기존 단일 envelope 계약은 유지한다.

전용 view 실패는 출력 전 Generic Document로 한 번만 재시도한다. 재시도 성공이면 내용·원래
종료 코드를 보존한다. 이후 실패 우선순위는 다음과 같이 고정한다.

| 상황 | 결과·종료 코드 |
| --- | --- |
| 기존 오류 응답을 전용/Generic 모두 렌더링 못 함 | controller의 고정 plain `ERROR`/`Diagnostic (escaped JSON)` 구조 아래 **같은 canonical 오류 전체**를 terminal-safe escape하여 stderr에 출력; 원래 오류 code·exit·모든 details 보존 |
| 성공 응답을 전용/Generic 모두 렌더링 못 함 | 공통 producer에서 `internal_error`/1, message `Cannot render output.`, phase=render 생성; 정상 결과로 보고하지 않음 |
| public success/failure 인코딩 자체가 실패함 | 기존 `io_error`/1 정책; 안전한 새 공통 오류만 인코딩·보고하며 실패 원문/내부 Go error를 노출하지 않음 |
| stdout write 오류·short write | stderr에 공통 `io_error`/1 한 번 보고; 이미 나간 stdout은 회수·재시도하지 않음 |
| 원래 오류를 쓰는 stderr write 오류·short write | 추가 render/다른 stream 출력 없이 1로 종료; 원래 exit보다 transport 실패 우선 |
| success publication 뒤 context 취소 | 기존 성공 결과 유지; 출력 실패가 실제 발생한 경우에만 위 I/O 정책 적용 |

긴급 plain 경로는 UI 구조의 Render를 다시 부르지 않고 같은 공통 terminal escape primitive만
사용한다. 원문 경고·nested causes·remedies를 네 개 요약 필드로 줄이지 않으며, 별도 안내 사본도
만들지 않는다. `Diagnostic (escaped JSON)`은 text 오류의 명시적 최후 표현이며 JSON 모드 변경이
아니다. 이 경우도 stdout에 새 JSON을 섞지 않는다. 인코딩/쓰기 불가 자체는 A15의 정상 표시
동등성을 보장할 수 없는 transport 실패로 구분하고 성공/완전 전달을 주장하지 않는다.

새 출력 계층 오류의 code/message/phase/effects/retry는 공통 CLI producer에서 한 번 만든다.
이 오류도 이미 아는 invocation operation·선택 scope·원 request UUID를 보존한 뒤 기존
BindGuidance/Public 경계를 한 번 통과한다. 오류 출력을 위해 profile/store를 다시 조회하거나
request ID를 새로 만들지 않는다. 추가 recovery action은 기존 공통 producer에 실제 정의된
경우에만 제공하며, 모든 command에 request-show가 있다고 가정하지 않는다.
기존 오류의 렌더 실패에는 기존 effects/retry를 유지한다. 새 출력 오류가 handler 반환 전이면
effects=none/retry=after_dependency_change, handler가 성공 반환한 뒤면
**보수적으로 effects=unknown, retry=inspect_first**다.
조회인지 mutation인지, changed=false인지 보고 효과를 추측하지 않는다. 새 phase는 기존 enum
render를 사용한다. 새 post-handler unknown 보강은 JSON과 text 양쪽에 적용하는 protocol 15의
명시 변경이며 정상 handler-origin 오류는 바꾸지 않는다.
handler를 되돌리거나 재실행하지 않는다. UUID가 있는 업무는 저장된 request/status 조회,
응답 유실은 원 UUID·동일 입력 retry 계약을 유지한다. format 전환은 새로운 업무 의도가 아니다.

## 4. 공개 schema·help·completion 계약

다음 CLI protocol은 현재 기준 **15**로 올린다. 구현 전 다른 변경이 번호를 선점했다면 최신
다음 번호로 재조정한다. envelope `schema_version:1`, 저장 형식, TOML, dashboard HTTP,
runner/launcher/key-session wire에는 버전 변경을 전파하지 않는다.

catalog/root/group/exact discovery 응답 data에 같은 정본의 `global_options`를 응답당 한 번
노출한다. catalog의 각 command entry에는 이를 반복하지 않고 presentation만 둔다.
global_options의 format 정의는 name=format, values=[auto,text,json], default=실제 기본값,
environment=DEVTOOLS_FORMAT, placement=root-prefix다. 기존 isolated-root도 name·
value_name=PATH·placement=root-prefix로 명시한다. command-local isolated-root 허용 위치는
기존 명령별 parser 계약대로이며 help/schema discovery에 새 위치를 추가하지 않는다.
command-local Options 목록에 format을 삽입하지 않는다. exact/catalog entry의 parsed-input
input_schema.properties.format은 일반/native 명령에 [auto,text,json], normal machine-only에
[auto,json] enum을 사용한다. help 예외는 기존 help_flags grammar로 설명하며 normal input schema와
혼동하지 않는다. 각 command의 output_schema에는 표시 옵션을 넣지 않는다.
stdout ownership와 실제 표시를 혼동하지 않도록 각 command의 metadata를 다음처럼 고정한다.

```json
{"output_mode":"json","presentation":{"formats":["json","text"],"default":"auto"}}
{"output_mode":"json","presentation":{"formats":["json"],"default":"json"}}
{"output_mode":"artifact","presentation":{"formats":[],"default":"native"}}
```

`formats`는 변환 가능한 stdout 표현이다. 기존 help/passthrough도 native 표기다.
이 예시는 최종 배포 계약이다. 중간 단계에서 default json을 유지하는 동안에는 metadata도
실제 default json을 보고하고, T05의 auto 전환과 metadata 변경을 같은 변경에 묶는다.
`output_schema`는 JSON 표현의 data schema이며 text를 검증하는 schema가 아니다.
`transport.success/failure/output_schema` 설명은 auto 선택·JSON 고정 discovery·오류 stream·
native 예외와 기존 interactive prompt가 stderr 진단보다 먼저 나올 수 있음을 포함한다.
root flags와 child args를 재현하는 grammar 예시도 포함한다.
`$defs.catalog`의 properties/required에 global_options를 같이 추가한다. 현재 strict한
additionalProperties=false에 새 응답만 넣고 schema를 갱신하지 않는 상태를 허용하지 않는다.
version 응답의 기존 data 필드는 유지하며, format capability는 version 숫자가 아니라 위
discovery metadata에서 확인한다. native stdout metadata의 formats=[]는 format 무효가 아니라
stdout 변환이 없다는 뜻이고 devtools 오류의 format 선택은 계속 별도로 적용한다.

정적 bash/zsh/fish/PowerShell completion은 command 앞의 `--format`과 enum을 제안한다.
동적 completion은 root prefix를 건너뛰되 값 입력 중이면 format 후보만 반환한다. `__complete`
자체 호출과 NUL 입력·line 출력은 그대로 두며 후보 조회에서 handler·format 출력기를 부르지 않는다.
`--isolated-root`가 있는 completion과 child 경계도 함께 검사한다.

repo 내부 CLI JSON consumer는 `scripts`, `verify`, `perf`, README 예시 및 bundled skill을
검색하여 명시 JSON이 필요한 호출에 root `--format json`을 적용한다. 특히 PTY fixture에서
상속된 CODEX/CLAUDE 변수가 text 실패를 가리지 않게 synthetic env를 사용한다.
구버전 binary도 실행하는 installer/update/compat verifier는 자식 process env에
DEVTOOLS_FORMAT=json을 덮어쓴 채 **`schema version`**을 먼저 조회한다. 저장소 없는 고정 JSON
경로이며 protocol14는 해당 env를 무시한다. global_options에 format/json 지원이 있을 때만
후속 호출에 root --format json을 붙이고, 없으면 구 계약대로 호출한다. 부모 env는 바꾸지 않는다.
version을 pipe로 조회하는 방법은 명시 env=text에 의해 text가 될 수 있으므로 쓰지 않는다.
새 flag를 붙였다 실패하면 제거하고 mutation을 재시도하는 capability 탐색도 금지한다.
protocol 숫자를 고정한 검사도 조사 대상이다. 현재 `internal/cli/output_test.go`,
`internal/configcontract/schema.json`, `scripts/test-windows-{completion.ps1,product.py}`,
`scripts/dashboard-browser.spec.mjs`, `verify/value_handoff_fixture.py`와 여러 installed scenario에
14가 있다. 최신 기대값을 바꾸되 구버전 호환 fixture의 14는 유지한다. broad 치환으로
역사적 migration 문서·runner wire 숫자까지 바꾸지 않는다.

## 5. 구현 작업과 의존성

중간 commit은 기능을 나눠 검토하기 위한 것이며 각 단계가 독립 릴리스 가능하다는 뜻은
아니다. T01–T04 기간 기본값은 기존 JSON으로 유지하고 explicit `--format auto/text`만
검사한다. 최종 auto 전환은 T05에서 전체 경로와 caller 이행을 확인한 뒤 수행한다.
새 protocol 배포는 전체 수용 완료 후에만 별도 릴리스 작업으로 진행한다.

| 작업 | 결과·주요 경로 | 엄격 의존성 | 완료 증거 |
| --- | --- | --- | --- |
| T01 공통 표시 경계와 explicit text를 연결 | root parser/환경 observer/resolver/public buffer/error/generic Document를 vertical slice로 구현. `internal/cli/{cli,root_options,schema}.go`, 신규 `presentation/`·`internal/cliui/`, `internal/protocol/protocol.go`. discovery 우회·early 오류도 포함 | 없음 | 주입 환경 matrix, prefix/offset/child 경계, JSON 동등성, 전용 view 없는 명령·모든 실패의 text, 기존 output ownership·패키지 의존 방향 검사 |
| T02 조회 결과를 목적별 text로 제공 | command list/inspect/explain, project/profile/port/instance/process/proxy, env/variable/secret/bundle/values/vault 조회·로그 및 나머지 catalog 조회를 분류해 list/detail adapter 추가 | T01 | 빈 값·빈 목록·숫자·Unicode·긴 argv·scope·cursor·새 필드·intentional value의 독립 기대값 fixture; public field만 읽음 |
| T03 mutation·진단·task의 의미를 text에 보존 | changed/replayed/preview, doctor/readiness, project 부분 실패, config/import/backup/cleanup/update, task/workstream/validation/graph/report 전용 adapter와 공통 public error presenter | T01 | 실패 후 효과·unknown·요청 ID·next_actions·required inputs·nested causes·상태별 구분·control 문자 escaping 검사 |
| T04 native 출력·discovery·caller·문서를 이행 | schema 항상 JSON, help/artifact/child/private wire 경계, root completion, transport/schema metadata, scripts/verify/perf 소비자 및 README/출력 문서/bundled skill 갱신 | T01 | schema alias/모든 discovery path, 4 shell completion, 기존 출력 보호·child 인자·exit/signal, protocol14 소비자 호환 fixture와 문서 검사 |
| T05 기본 auto 전환과 실제 제품 검증 | T02/T03 catalog coverage를 확인하고 기본값 auto 전환. 신규 `verify/scenarios/cli_presentation.py`, 기존 runner·native CI lane에 연결 | T02,T03,T04 | 설치 제품의 clean-env PTY/text·agent-env PTY/JSON·pipe/JSON·명시 override·오류 stream·native 경계, Linux/macOS/Windows 실제 근거 |
| T06 최종 전 범위 리뷰와 수용 | 확정 plan/등록 spec와 전체 diff·callers를 재검토. 필요한 수정 및 영향 검증 후 완료 근거 기록 | T05 | 아래 수용 matrix·required validation이 최종 source에서 current, 전 영역 reviewed, 미해결 유효 결함 없음 |

T02와 T03은 서로의 결과를 소비하지 않아 병렬 가능한 작업 단위지만 이 기록이 agent 병렬
실행을 요구하지는 않는다. T04는 T01의 root 계약을 소비한다. T05가 실제 전환 gate이며
T06은 구현 승인 뒤 수행할 제품 리뷰다. 현재 요청의 **계획 리뷰**와 제품 리뷰를 혼동하지 않는다.
후속 스타일·계층 보완은 이 의존성을 바꾸지 않는다. T01은 per-stream terminal/width/NO_COLOR,
presentation의 정책·Document builder와 독립 cliui 계층의 Options·Document·Render를 연결한다.
T02/T03은 그 위에 목적별 presenter와 요약·필수 필드 계약을 구현한다. T04는 새 text 장식이
native/private wire에 적용되지 않음을 확인하고, T05/T06은 A13 시각 확인·A14 의존 경계·
A15 진단 원문/행동 정합성을 최종 검증에 포함한다. A16의 추가 결정도 기존 작업 경계를 따른다.
A17의 복사 명령은 T01의 CommandLine/shell formatter, T03의 실제 public action 구성,
T04의 문서, T05/T06의 실제 shell·terminal 검증에 연결한다. T02의 Exec 조회는 실행 선택
원문 표시로 유지한다.

## 6. 수용·검증 계약

| 키 | 반드시 확인할 실패/경계 | 검사 방법 |
| --- | --- | --- |
| A01 | flag > env > auto, explicit auto, invalid/empty/duplicate, signal false/unknown, 관측 불가 writer | pure resolver·root parser unit, handler 미호출 sentinel |
| A02 | agent PTY, clean PTY, pipe·stderr redirection, 실제 IO를 기준으로 결정 | 주입 observer와 설치 binary PTY/pipe matrix; 상속 agent 변수 제거 후 case별 공급 |
| A03 | JSON public data·envelope·exit·replay 보존, 큰 정수·omitempty·json:- | 기존 protocol/CLI tests와 독립 fixture 대조; 임의 내부 필드·raw error가 text에 나오지 않음 |
| A04 | 모든 catalog command가 dedicated 또는 generic text, schema는 항상 JSON | registry 순회 metadata·대표 실행 검사; 새 Command는 generic으로 동작하고 policy/metadata 누락은 검사 실패 |
| A05 | text 목록/상세/변경/진단/부분 실패/graph에 필수 의미 보존 | 의미 단위 fixture 검사; 전체 spacing snapshot에 의존하지 않음 |
| A06 | child/artifact/help/private 출력·prompt·redaction 불변 | 기존 output_test/run_args/redaction/completion 및 installed output_redaction, run fixture |
| A07 | root prefix+isolation+source offsets+child `--format/--output` 전달 | parse/InvocationContext tests, native Windows argv와 POSIX 실행 검사 |
| A08 | malformed format·unknown command·schema 오류·help 우선순위·I/O·postcommit 실패 | stderr 형식·handler 실행 횟수·exit·effects/retry 및 partial write 주입 검사 |
| A09 | control 문자·긴 Unicode·빈 값·cursor·argv·new public fields | text fixture; secret/key/session 계정 값 없는 synthetic 데이터만 사용 |
| A10 | schema/help/completion/README/skill·내부 consumer 이행 | exact/root/group/--all/config schema 계약, 4 shell 검사, old/new protocol fixture |
| A11 | 실제 설치 제품과 native 플랫폼에서 최종 auto 동작 | 아래 통합 명령 및 Linux/macOS/Windows 실제 실행 로그·source SHA; cross compile로 native pass 대체 금지 |
| A12 | 계획과 구현의 완전성·회귀·최종 증거 | T06 ledger 리뷰→수정→영향 caller/검사 재검토, Git hygiene 및 required validation current |
| A13 | text 계층·SGR·NO_COLOR·폭·요약·명령별 실제 가독성 | per-stream TTY/VT/TERM/NO_COLOR matrix, 40/79/80/120 폭·CJK/combining/emoji fallback, ANSI strip 후 의미 일치·SGR span reset·dark/light 시각 확인; known 생략 규칙과 unknown Details 검사 |
| A14 | CLI/presenter/독립 CLI UI/protocol의 의존·소유권 | presenter Document 의미 검사와 순수 cliui layout/style 검사 분리, production import graph·JSON bypass·bounded fallback·단일 writer/exit 경계 확인 |
| A15 | 기존 오류·경고·안내가 JSON/text 간 drift 없이 유지됨 | 동일 public envelope의 원문 message·code·warning/check/cause·argv/필수 입력/effect/scope·retry/effects와 Document를 대조, render escape/wrap·동일 argv의 shell 인용 및 unknown field·누락/추가 행동 검사; mutation 1회 결과 사용 |
| A16 | 추가 리뷰에서 확정한 경계가 구현·공개 계약에 함께 반영됨 | contract-review D01–D12의 typed/immutable Document, 순서/우선순위·EOF/alias offset, format 제외 fingerprint, 원래 오류 exit·긴급 전체 진단, read-only VT, schema bootstrap/global_options/strict defs·DefaultFormat 정본과 view coverage를 검사 |
| A17 | Next의 완성된 public action을 한 줄 전체 복사해 사용할 수 있음 | B6.5의 CommandLine·필요한 shell literal 인용·Unix 중복 제거/다른 문법 label·PowerShell native 차이·필수 입력/control fallback, 원 argv round-trip, prompt/번호/SGR/실개행/생략 없음과 JSON/identity 보존을 실제 shell fixture로 검사 |

구현 중 검증은 영향 영역의 기존 unit/integration부터 사용한다. 신규 unit은 순수 정책,
serialization 경계와 의미 있는 오류에 한정한다. 실제 PTY 선택·native argv·private helper
호환은 설치 통합 검증으로 확인하며 table renderer 내부 구현을 그대로 복제한 검사는 만들지 않는다.

최종 필수 실행은 `devtools run check:all`, `devtools run test:installed` 전체,
`devtools run check:skill`과 기존 4 shell completion 검사다. 구현 중에는
`devtools run test:installed cli_presentation output_redaction doctor config_edit tasks
project_lifecycle`로 영향을 좁혀 검사한다. `cli_presentation` scenario는 T05에서 등록한 뒤
실행한다. protocol bump는 여러 scenario에 영향을 주므로 최종 subset만으로 완료하지 않는다.
Linux/WSL POSIX PTY와 Windows native terminal/PowerShell 및 macOS terminal의 실제 실행을
기존 CI lane에서 확인한다. native 미실행은 pending으로 남기며 workstream close하지 않는다.
불필요한 네트워크/provider 계정·hosts/trust·키 저장소 실험은 이 표시 변경의 검증에 추가하지 않는다.
text 스타일은 Linux/macOS/Windows의 밝은·어두운 terminal에서 대표 목록/상세/preview/replay/
부분 실패/오류를 실제로 확인한다. 색 없음 상태도 포함한다. 캡처가 없으면 byte 검증만으로
가독성 pass라 기록하지 않는다. 반대로 임의 terminal palette의 대비나 모든 emoji 폭까지
보장한다고 기록하지 않는다. 캡처·preview에는 synthetic 공개 데이터만 사용한다.

구현 뒤 `git status --short`, `git diff --stat`, `git diff --check`와 필요한 staged diff를 확인한다.
각 검증은 실제 source SHA·테스트 환경·실행 결과로 기록하고 계획 등록 성공을 제품 pass로 쓰지 않는다.

## 7. 호환성·복구와 배포 경계

기존 non-TTY JSON consumer는 기본 auto에서도 JSON을 받는다. 감지된 Codex/Claude PTY도
JSON을 받는다. 감지 못 한 agent·사람이 직접 PTY에서 JSON을 읽던 스크립트는 바뀔 수 있으므로
protocol 전환과 `--format json` 또는 `DEVTOOLS_FORMAT=json`을 migration 문서에 명시한다.
env만으로 완벽 호환을 주장하지 않는다. scripts가 명시 JSON을 쓰게 해 테스트가 우연한
agent 환경 상속에 의존하지 않도록 한다.

전환 후 개별 사용자는 `--format json`으로 되돌릴 수 있다. 제품 기본값을 되돌려야 하면
새 public contract로 문서화한 patch에서 default만 json으로 되돌린다. 기존 text/json flag와
schema discovery는 유지하고 저장소·command declaration·redaction 설정을 변경하지 않는다.
이번 작업에는 사용자 CLI 설치·교체, global shell alias/환경 설정 변경, push·merge·release·
CI dispatch를 포함하지 않는다. 계획 등록을 위해 task 저장소 접근·workstream 작성만 수행한다.

## 8. 계획 리뷰 기록

계획 자체를 순차 리뷰한다. 외부 reviewer나 별도 agent가 리뷰했다고 주장하지 않는다.

1차 리뷰는 현재 source와 경계·실패 경로를 대조했다. 아래 결함/모순을 수정했다.

- R01: 표시용 `--output` 제안이 기존 redaction 옵션과 충돌한다. 신규 `--format`과 별도
  `DEVTOOLS_FORMAT`으로 확정하고 root/child grammar·기존 보호 불변 검사를 연결했다.
- R02: 일반 성공 출력만 교체하면 discovery 직접 쓰기·parse/unknown/help 오류·Windows
  private wire가 빠진다. public 출력 공통 경계, machine/native 분류와 bootstrap 오류를 명시했다.
- R03: post-handler I/O 오류의 기존 Public 기본값은 `effects:none`이다. 새 encoding/render/write
  실패에서 `unknown`/`inspect_first`를 명시하고 mutation 재실행 금지·기존 UUID 복구를 추가했다.
- R04: schema JSON 고정을 이름으로 먼저 적용하면 `schema --help`까지 거절한다. 실제 동작
  help 분류를 우선하고 global 입력 오류·schema 대상 오류·private helper의 우선순위를 고정했다.
- R05: final metadata auto와 중간 default json이 충돌한다. 실제 default를 metadata에 반영하고
  최종 auto/metadata 변경을 T05에 묶었다. 구버전 binary에 새 flag를 일괄 전달하지 않게 했다.
- R06: 새 Command는 generic으로 처리한다는 규칙과 '새 Command면 무조건 테스트 실패'가
  충돌했다. generic은 허용하고 policy/metadata 누락을 검출하는 수용 조건으로 바로잡았다.

2차 리뷰는 수정한 계획의 dependency·수용 matrix와 실제 catalog/consumer를 다시 대조했다.
R07: 일부 installed scenario만 검사하면 다른 protocol 14 고정 consumer 실패를 놓친다.
196개 catalog baseline·schema const·Windows/브라우저/installed assertion 위치를 추가하고,
구버전 fixture는 보존하며 최종 installed 전체를 필수 gate로 바꿨다.

| 영역 | 최종 계획 리뷰 상태 | 근거·재검토 결과 |
| --- | --- | --- |
| parser·flag·offset | reviewed | B2와 인터페이스, A01/A07/A08; R01 수정 후 기존 output 보호와 child 전달 재대조 |
| 감지·TTY·override | reviewed | B1/B3, A01/A02; 세션 관측과 알려지지 않은 PTY 한계 구분 |
| 계층·공개 projection | reviewed | 신규 패키지 경계·B1–B5, A03/A14; presenter는 public node, cliui는 Document만 소비 |
| text 의미·실패·보호 | reviewed | B5.1/B6/B7, A05/A08/A09/A13/A15; 원문 진단·행동 보존과 계층·SGR·폭·요약을 재검토 |
| native/private | reviewed | B4, A06/A07; 도움말 예외·helper 분기·child stream 재대조 |
| discovery/completion | reviewed | B4·4절, A04/A10; R02/R04/R05의 경로·default 정합성 재확인 |
| JSON 호환·consumer | reviewed | 4·7절, A03/A10/A11; R07 이후 old binary/new flag와 hardcoded protocol 재대조 |
| 실행 단위·검증·등록 | reviewed | 5·6절, A01–A17; T01→T02/T03/T04→T05→T06, pending 제품 검증 유지 |

후속 스타일 보완 리뷰에서는 R08–R11을 추가로 수정했다.

- R08: plain UTF-8만으로는 요구한 가독성 스타일 계약이 없었다. semantic role·제한된 SGR,
  per-stream capability·width와 3개 synthetic layout 예시를 B6에 추가했다.
- R09: format 결정과 색 결정을 합치면 JSON 오염 또는 stdout redirect/stderr TTY 오류가 생긴다.
  두 resolver를 나누고 NO_COLOR(색만)·TERM=dumb(전체 SGR)·native wire 예외를 명시했다.
- R10: 모든 public field를 무조건 나열하는 기본 출력은 읽기 부담을 반복한다. 필수 의미·
  알려진 생략 규칙·unknown Details를 구분하고 mutation 재실행 없는 조회 경로를 유지했다.
- R11: 단순 rune 길이나 x/text/width로 모든 emoji 폭을 보장할 수 없다. cell-width 정책과
  complex grapheme의 stacked fallback을 명시하고 narrow/mono/dark/light 실제 검증을 A13에 연결했다.

추가 2차 리뷰에서 예시 remedy의 command list가 지원하지 않는 --profile을 발견하여
실제 허용된 --dir로 수정했다. 3.0 인터페이스·B1·B3·B6의 새 필드/함수와 task/수용/검증
연결을 재대조했다. 기존 T01–T06 및 A01–A12를 삭제하지 않고 A13과 R09 스타일 요구를
추가하며 관련 task/validation에 연결한다. 임의 agent 변수 설정·새 color/style 플래그·TOML은 보류다.

CLI 계층 추가 리뷰 R12: 앞 설계는 presentation 하나가 command 해석·순서·field 생략과
cell width·style·ANSI를 함께 소유할 수 있었다. 독립 internal/cliui 계층과 Document 경계로
분리하고 `RenderText(command, JSON, options)`를 `Build(command, public JSON)→Document`
및 `Render(Document, Options)→bytes`로 교체했다. JSON은 Document를 우회한다.
후속 재검토에서는 generic fallback과 UI render 실패의 소유권·횟수도 명시하여 writer/exit가
cliui에 새지 않게 했다. 기존 spec·T01–T06·required validation에 이 변경과 A14를 동기화한다.

진단 drift 리뷰 R13: public data 공통화만으로는 presenter가 message를 다시 쓰거나 warning을
요약하고 Next argv를 별도 조립하는 것을 막지 못했다. B5.1에 원문 재사용·진단 생략/재작성·
메시지 문자열 기반 추론·권고 합성 금지를 추가하고, 기존 조회 안내 규칙도 public action이
존재하는 경우로 좁혔다. A15는 동일한 한 번의 public 결과로 원문과 Document의 문구·관계·
토큰 및 renderer 표시를 대조한다. 신규 format 진단·기존 계획의 post-handler 효과 보강을
기존 안내 보존과 구분했다. 후속 재검토에서 예시를 안내 정본으로 복사하지 않는 규칙도 확인했다.

추가 계약 리뷰 D01–D12는 typed 화면 모델·실행 전 순서·EOF/alias/source 매핑·format과
fingerprint 분리·원 exit/emergency·기존 VT 읽기·안전 bootstrap·strict metadata·view coverage·
환경 source·default 정본을 확정했다. 2차에서 deadline/취소·handler source 재매핑·원문 진단
전체 보존과 기존 prompt/child stream 예외를 다시 확인했다. 근거와 실제 baseline6건은
별도 contract-review 문서에 남기고 A16으로 기존 작업·검증에 연결한다.

후속 사용자 결정 D13: Next를 raw argv 배열 대신 전체 줄 복사 가능한 명령으로 표시한다.
원 argv/진단 정본을 유지하고 독립 CommandLine block과 순수 shell literal formatter로
표현만 바꾼다. prompt·들여쓰기·SGR·실개행/ellipsis를 명령 줄에서 제거한다. 활성 shell을
추측하지 않고 Unix POSIX/Fish는 같은 결과를 합쳐 한 줄만, 다른 결과는 label을 윗줄에
분리하며 Windows는 PowerShell을 명시한다. 필수 입력·지원되지 않는 literal은 실행 가능
표시를 피하고 원 Arguments를 보존한다. B5.0/B5.1/B6.5·A17과 기존 작업/검증을 갱신한다.

현재 계획 범위에서 미해결 유효 결함은 없다. 실제 구현과 플랫폼별 검증은 모두 pending이며
계획 리뷰 통과를 제품 구현 통과로 바꾸어 기록하지 않는다. devtools 등록 후 spec/plan 본문,
6개 task·dependency·required validation·구조 검사 결과를 read-back으로 별도 확인한다.
