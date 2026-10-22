# 프로젝트 설정·CLI 진단 계약

[문서 목록](README.md) · [설정 편집 CLI](config-edit-contract.md) · [진단·doctor](diagnostic-contract.md)

이 문서는 protocol 10 소스의 설정 모델과 구현 계약을 설명한다.
설치본에서 `devtools version`과 `devtools schema config edit`로 기능을 확인한다.
실제 사용 순서는 [설정 편집 가이드](configuration.md), 출력은 [CLI 출력](cli-output.md)을 따른다.
각 OS의 게시·권한 수용 기준은 해당 OS에서 직접 검증하며 cross compilation으로 대체하지 않는다.

이 계약은 도구 사양의 중복 작성, 설정 편집 실수와 진단 범위의 불일치를 해결한다.
TOML 작성 모델과 CLI JSON 입력·응답은 [공통 schema](../internal/configcontract/schema.json)의 `$defs`를 따른다.
schema가 표현하지 못하는 참조·UTF-8 byte 상한·OS 상태 검증은 이 문서와 하위 계약의 규칙을 함께 적용한다.
[진단 개선 요구사항](cli-guidance-audit.md)의 D01–D19, C01–C03은 수용 검증의 추적 식별자다.

## Schema 실행 방식

schema의 정본은 internal/configcontract/schema.json 하나다. 같은 package에서 go:embed로 포함하고
config schema·개별 명령 discovery·런타임 검증·계약 테스트가 같은 bytes를 사용한다.
본문 입력을 schema와 별도의 field switch로 다시 정의하지 않는다.

런타임 engine은 github.com/santhosh-tekuri/jsonschema/v6 **v6.0.3**, x/text는 **v0.43.0 이상**으로 정한다.
Go 1.27 빌드와 호환되는 pure-Go API와 Apache-2.0 배포 조건을 확인한 선택이다.
배포 notice는 바이너리에 포함하며 `devtools license`로 조회한다. 의존성 변경 시 module graph와 notice를 함께 검증한다.
engine에는 AddResource로 포함된 schema만 등록하고 UseLoader는 나머지 file/network/URN 조회를 모두 거절한다.
draft는 2020-12로 고정하고 표준 Go regexp를 사용한다. 임의 사용자 schema·format/content plugin은 받지 않는다.
x-maxUtf8Bytes는 별도 vocabulary로 등록하고 AssertVocabs를 활성화해 실제 byte 제약으로 검사한다.
기본 validator가 모르는 annotation을 무시했다는 이유로 이 제약을 통과한 것으로 간주하지 않는다.

raw JSON은 engine 호출 전에 중복 key·유효 Unicode와 숫자 token을 검사한다.
이번 편집 입력의 숫자는 signed 64-bit 십진 정수 token만 받는다. 소수·지수 표기와 범위 초과는 거절한다.
token 길이를 먼저 제한한 뒤 ParseInt하고, 제한되지 않은 json.Number를 engine에 넘기지 않는다.
이는 이 모델의 숫자가 port/range 정수라는 요구와
[보고된 무제한 숫자 변환 문제](https://github.com/santhosh-tekuri/jsonschema/issues/261)에 따른 입력 경계다.
대체안인 직접 구현한 JSON Schema subset은 oneOf·조건부 필수·ref·byte 제약의 중복 구현을 늘리므로 채택하지 않는다.
구조 검증 뒤의 참조·실행 의미·filesystem 검사는 기존 typed 도메인 validator를 공유한다.

근거: [고정 버전 API](https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6@v6.0.3),
[byte vocabulary 예제](https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.3/example_vocab_bytecount_test.go),
[go.mod](https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.3/go.mod),
[라이선스](https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.3/LICENSE).
실제 provider는 같은 합성 사례를 Python과 Go engine으로 검증하고 숫자 token·offline loader 거절도 통과해야 한다.
정규식은 두 engine에서 같은 의미를 갖는 표기만 사용한다. JSON Schema의 끝 anchor만으로
마지막 개행을 거절한다고 가정하지 않으며, control 범위는 양쪽이 지원하는 hex escape로 표현한다.

## 계약의 경계

- 설정 읽기·검증·설명과 편집은 실행 가능한 프로그램을 호출하지 않는다. 도구 probe는 doctor와 실행 전 검사에서만 한다.
- 도구 등록, preset 등록과 모든 명령에 적용하는 defaults를 구분한다.
- 기존 argv·주입·bundle·port·network·출력 보호 의미를 유지한다. 명령의 내부 의존성을 추정하거나 설치하지 않는다.
- 설정 편집은 선택한 파일 하나를 저장한다. 실행 중 process의 재시작이나 OS/network 변경을 함께 수행하지 않는다.
- JSON envelope는 1이다. 새 설정에 version/schema_version 필드를 추가하지 않는다.
- 기존 `command list/inspect/env/run`, alias와 run의 자식 argv 경계는 유지한다.

## 설정 모델

다음은 가상 프로젝트의 작성 예시다. 프로그램과 package script는 해당 프로젝트가 제공한다.

```toml
profile = "sample"

[tools]
node = {}
pnpm = { version = "11.24.0" }
go = {}

[presets.web]
tools = ["node", "pnpm"]

[commands."dev:web"]
exec = ["pnpm", "run", "dev"]
requires = ["web"]

[commands."dev:api"]
exec = ["go", "run", "./cmd/api"]
tools = ["go"]
```

### 이름·문자열·목록

| 대상 | 규칙 |
| --- | --- |
| profile, bundle, 도구, preset, port, proxy, listener 이름 | `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`; ASCII 1–128자 |
| 명령 이름 | 기존 `project.CommandPattern` 유지. Unicode 1–128 code point, 첫 글자 `-` 금지, U+0000–001F·007F–009F 금지 |
| var/sec·bind key | `^[A-Za-z_][A-Za-z0-9_]*$`; 별도 길이 상한 없이 파일 상한 적용 |
| 모든 이름 | 대소문자 구분. trim·case folding·Unicode 정규화·이름 자동 교정 없음 |
| 참조 목록 | 중복 선언은 오류. 서로 다른 출처의 동일 참조는 해석 시 합집합 |
| exec·ready.exec | 비어 있지 않은 argv 배열. 첫 항목은 빈 문자열 금지. 모든 항목에 NUL 금지 |
| TOML·JSON | 유효한 UTF-8. TOML은 현재 고정된 parser와 같은 1.1 문법. JSON 중복 key·뒤따르는 두 번째 문서·잘못된 surrogate는 오류 |

`tools."runtime.node"`와 `commands."build.web"`의 이름은 각각 하나의 literal key다.
resource 이름과 JSON path를 마침표·콜론으로 다시 분할하지 않는다.
파일 상한은 원문 **2 MiB**이며 JSON 편집 본문과 저장 후보에도 같은 상한을 적용한다.
이는 기존 CLI JSON 입력의 2 MiB 경계를 재사용한다. 크기 초과를 검사 누락이나 부분 저장으로 처리하지 않는다.

### 등록과 선택

`tools`의 각 값은 `Tool`이다. 허용 필드는 `executable`, `version`, `version_args`다.

- executable 생략은 도구 이름을 사용한다. 명시할 때는 비어 있지 않은 NUL 없는 문자열이다.
- version은 기존 정확한 버전 문법과 128자 상한을 유지한다. 범위·접두 wildcard는 받지 않는다.
- version이 있고 version_args가 없거나 빈 배열이면 `["--version"]`이다.
- version이 없으면 빈 version_args만 허용하고 probe를 실행하지 않는다.
- version_args는 최대 32개이며 각 인수는 UTF-8 기준 최대 4096 byte, NUL 없음이다.
- `node = {}`는 이름이 node인 무버전 제약 사양이다. 등록 자체는 필수 조건 선택이나 설치가 아니다.

`presets.NAME`의 필드는 `tools`, `vars`, `secs`다. 빈 preset도 유효하다.
실제 값, exec, env, bundle, 다른 preset 참조와 상속은 허용하지 않는다.
값 선택인 기존 bundle과 필수 조건 선택인 preset을 별개로 유지한다.

명령과 `defaults`는 `requires`, `tools`, `vars`, `secs`로 조건을 선택한다.
`requires`의 이름은 preset을 참조하고, `tools`의 이름은 registry의 도구를 참조한다.
명령별 해석 순서는 다음과 같다.

1. defaults의 직접 선택과 defaults가 참조한 preset을 수집한다.
2. 명령의 직접 선택과 명령이 참조한 preset을 합친다.
3. tool_overrides를 적용하고 도구 기본값을 계산한다.
4. 이름순으로 도구·var·sec를 정렬한 최종 요구사항을 만든다.

목록 순서는 우선순위를 만들지 않는다. 모르는 이름과 var/sec의 동일 key 충돌은 오류다.
preset 등록만으로 모든 명령의 필수 조건이 늘어나지 않는다.

### 명령 필드와 기본값

명령의 전체 입력은 schema의 `Command`, 수정은 `CommandPatch`다.

| 필드 | 생략 시 의미 |
| --- | --- |
| exec | 필수, 기본값 없음 |
| inject | false. 실행 시 명시적인 --env 선택은 기존처럼 주입 활성화 |
| env | 공통 layer인 빈 문자열 |
| bundle | 프로젝트 bundle, 그것도 없으면 all |
| output | auto |
| stdout·stderr | output 정책 상속 |
| values_update | notify |
| ready | readiness probe 없음 |
| serve·bind·requires·tools·vars·secs·tool_overrides | 빈 선택 또는 map |

프로젝트의 기존 profile·bundle·env_files·ports·proxies 필드는 유지한다.
env_files는 [네트워크 입력 해석](../internal/project/network.go)의 입력이다.
이를 일반 child 환경에 자동으로 dotenv 주입하는 기능으로 확대하지 않는다.
ready.timeout은 기존처럼 기본 2s, 허용 10ms–30s의 Go duration이다.
port는 1–65535 또는 기존 network expression, range는 양 끝을 포함하는 두 정수이며 시작값 ≤ 끝값이다.
기존 빈 port expression도 유지하며, strict port가 필요한 경우에는 기존 network 검증을 적용한다.
strict=true에는 port 또는 port expression이 필요하다.
proxy와 bind의 표현식·외부 profile/instance 해석은 기존 [network 계약](network-design.md)을 따른다.
본 파일에서 확정할 수 있는 참조는 정적으로 검사하고 외부 실행 상태는 doctor에서 구분한다.

### Tool override와 충돌

tool_overrides는 이미 선택된 도구에만 허용한다. 각 entry는 **Tool 전체 교체**다.
registry Tool의 자식 필드를 상속하지 않는다.

```toml
profile = "sample"

[tools]
node = { executable = "node-custom", version = "1.2.3", version_args = ["-v"] }

[defaults]
tools = ["node"]

[commands."check:legacy"]
exec = ["node", "scripts/check.mjs"]
tool_overrides.node = { version = "2.0.0" }
```

이 명령의 Tool은 executable=node, version=2.0.0, version_args=["--version"]이다.
이는 기존 Requirements.Merge의 전체 Tool 교체와 같은 의미다.
CLI의 `tool.update`는 Tool 필드의 부분 수정이므로 override entry 교체와 구분한다.

버전 충돌은 **동일한 명령 안에서 동일 executable·동일 effective version_args가 서로 다른 정확한 버전을 요구할 때**다.
다른 명령의 다른 버전은 허용한다. 다른 probe argv는 별개의 검사다.
정적 검사는 같은 executable 표기를, doctor는 실제 해석한 파일 identity와 probe argv를 기준으로 검사한다.
경로 alias·PATH 차이를 정적 검사에서 확정했다고 표시하지 않는다.

## 작성 형식의 판별과 호환성

| format | 판별 |
| --- | --- |
| neutral | legacy requirements도 새 전용 필드도 없음 |
| legacy | 프로젝트 또는 명령에 requirements 선언이 있고 새 전용 필드가 없음 |
| registry | tools/presets/defaults 또는 명령의 requires/tools/vars/secs/tool_overrides가 있고 requirements 선언이 없음 |
| 오류 | legacy와 새 전용 필드를 함께 사용하거나 모르는 필드를 사용 |

빈 requirements 테이블도 선언으로 센다. neutral은 두 모델에서 의미가 같아 직접 편집할 수 있다.
legacy 읽기·doctor·explain은 유지하고 semantic 편집은 명시적인 migrate를 요구한다.
legacy의 fmt는 형식만 정리하고 requirements 구조를 바꾸지 않는다.
구버전 CLI는 새 필드를 거절하므로 reader들을 먼저 protocol 10 지원 빌드로 맞춘다.
기능 탐색은 protocol 숫자와 `schema config edit`, `schema config preset add`의 존재를 함께 확인한다.

## 해석과 출처

하나의 source-aware 작성 모델에서 ResolvedProject와 ResolvedCommand를 만든다.
CLI, services, lifecycle, proxy resolver, cross-instance bindings, dashboard와 completion이 이를 공유한다.
decoder용 struct나 CLI별 재구현을 설정의 별도 정답으로 사용하지 않는다.

source path는 문자열 배열이며, line·column은 1부터 세는 **UTF-8 byte 위치**다.
offset·end_offset은 0부터 세는 byte 범위이며 끝은 제외한다.
출처가 없는 기본값은 kind=default, path=[]로 표시하고 가짜 행·열을 만들지 않는다.
해석된 각 필드는 defaults → 이름순 preset → command → override 순서의 contributor를 기록한다.
해당 Tool의 생략 필드에는 Tool 교체 후 적용한 기본값의 출처를 기록한다.

`command explain`은 static 해석 결과·provenance·동적 미해결 조건만 반환한다.
exec의 동적 template은 원문 표현으로 남기고 ready가 있으면 effective timeout을 Go duration 문자열로 반환한다.
값 저장소 인증, tool probe와 readiness 실행을 하지 않는다.
실제 실행 가능성은 같은 resolver를 사용하는 doctor와 run preflight가 판단한다.

## migration

`config migrate --dry-run`과 apply는 같은 결정적 변환기를 사용한다.

1. legacy 규칙으로 프로젝트 공통 요구사항과 모든 명령의 최종 요구사항을 계산한다.
2. 도구별 registry 기준은 공통 Tool이 있으면 그것, 없으면 해당 도구를 쓰는 이름순 첫 명령의 Tool이다.
3. 다른 명령의 Tool 사양 차이는 완전한 tool_overrides로 남긴다. 합법적인 명령별 버전 차이를 충돌로 거절하지 않는다.
4. 기존 공통 조건만 defaults로 이동한다. 명령별 조건을 새 공통 조건으로 승격하지 않는다.
5. 공통 조건을 제외한 도구·var·sec 선택 집합이 정확히 같고 두 명령 이상에서 반복되면 preset으로 묶는다.
   빈 선택은 묶지 않는다. 이름은 정렬한 선택의 canonical JSON SHA-256 앞 12자를 사용한 shared-이름이다.
   충돌하면 두 글자씩 늘리고 최종 64자에서도 충돌하면 오류로 중단한다.
6. 남은 선택은 명령의 직접 선택으로 둔다. 참조 목록은 이름순이다.
7. 전후 모든 명령의 실행 의미와 프로젝트 필드를 비교한다. exec·env_files·version_args·ready.exec의 순서를 보존한다.
8. 주석을 의미상 같은 선언·선택의 출처에 연결한다. 소유 관계를 확정할 수 없으면 보존 실패로 중단한다.

registry/neutral에서 변환이 불필요하면 no-op이다. fmt를 겸하지 않는다.
legacy에 필수 조건이 전혀 없었다면 변환 결과는 불필요한 빈 marker 없이 neutral일 수 있다.
legacy의 빈 문자열 기본값과 중복 요구 key는 effective 의미를 유지하는 새 모델의 생략·집합으로 표현한다.
동등성 비교에는 tool 기본값, vars/secs, argv, env/inject/bundle, 출력 정책, values_update,
serve/bind/ready, profile, env_files, ports/proxies와 미해결 expression을 포함한다.
동적 외부 상태를 대신 실행해서 비교하지 않는다.

apply digest는 source revision, protocol, 변환기 규칙과 후보 bytes에 묶인다.
revision·digest가 다르거나 의미·주석 보존 검증이 실패하면 파일을 쓰지 않는다.
자세한 입력·응답과 결과 불명 처리는 [편집 계약](config-edit-contract.md)을 따른다.

## 원형 보존과 formatter

고정된 go-toml parser의 source range와 KeepComments를 감싼 내부 adapter를 사용한다.
모든 node·range·comment·key는 다음 NextExpression 전에 복사한다.
원문 bytes를 유지하면서 수정 범위를 치환하고 후보를 다시 decode·검증한다.
struct 전체 marshal로 원문을 대체하지 않는다. unstable API는 adapter 밖으로 노출하지 않는다.

- 단건 편집은 수정 대상 밖의 bytes를 보존한다. 기존 resource의 위치를 유지하고 새 resource는 같은 명령군의 끝에 추가한다.
- formatter는 항목·배열 순서, 주석 내용·연결, scalar 문자열 값과 LF/CRLF 관례를 보존한다.
- 새 파일은 LF, 혼합 개행 파일에서 새로 넣는 줄은 첫 개행 형식, 개행이 없으면 LF를 쓴다.
- 새 scalar 문자열·quoted key는 TOML basic string으로 escape한다. 식별자에 마침표가 있으면 반드시 quote한다.
- 새 key/value는 공백 하나를 둔 equals, 들여쓰기는 공백 두 개, 문서 끝은 개행 하나다.
- 새 일반 배열은 100 code point 이내면 한 줄, 넘으면 원소별 줄과 trailing comma로 쓴다.
  기존 내부 주석·멀티라인 scalar는 token을 그대로 유지하며 줄을 재배치하지 않는다.
- 새 명령의 필드 순서는 exec, inject, env, bundle, output, stdout, stderr, values_update,
  ready, serve, bind, requires, tools, vars, secs, tool_overrides다. 기존 필드 순서는 유지한다.
- 새 도구 사양은 100 Unicode code point 이내면 inline table로 쓰고, 넘으면 block으로 쓴다.
  fmt는 항목 순서와 TOML의 table 중복 금지 규칙을 지킬 수 있는 경우에 기존 block을 압축한다.
  다른 table이 끼어 있어 parent table을 다시 선언해야 하는 경우에는 기존 block을 유지한다.
- Tool의 기본값과 같은 executable은 해당 필드에 주석이 없을 때 생략한다.
  명시된 version_args, 빈 사양 등록과 명시적 false·빈 선택을 임의로 지우지 않는다.
- 내부 주석의 대응 관계가 사라지는 배열·inline table 교체는 config_preservation_failed다.
  대상 내부의 leaf 수정처럼 source 범위가 명확한 편집은 주석을 유지한다.
- remove는 대상 선언과 그 줄의 inline comment를 제거한다. 앞뒤 standalone comment는 남긴다.
  표·entry 전체의 명시적 삭제가 허용하는 범위를 넘어 주석을 삭제하지 않는다.
- fmt는 preset 추출·defaults 승격·사양 통합·버전 변경·migration을 하지 않는다.
- 같은 formatter의 두 번째 실행은 byte 변경이 없어야 한다. 정리 전후 해석도 같아야 한다.

## 선택의 근거와 대안

| 결정 | 근거와 배제한 대안 |
| --- | --- |
| 이름의 대소문자 구분과 literal key | 기존 project의 이름 규칙과 TOML 의미를 유지한다. 소문자 강제·점 분할은 기존 명령을 바꾼다. |
| registry + preset + 명시적 defaults | 기존 requirements 승격은 모든 명령의 조건을 늘린다. 참조 재사용과 전역 적용을 분리한다. |
| 단순 preset, 상속·중첩 없음 | 현재 재사용 요구는 선택 집합으로 충족한다. 숨은 우선순위·cycle·제거 규칙을 추가하지 않는다. |
| Tool 전체 override | 기존 Requirements.Merge와 Tool.WithDefaults가 이미 전체 교체한다. 부분 상속으로 바꾸면 실행 파일·probe argv가 달라진다. |
| source range adapter | 현재 의존성이 위치·주석을 제공한다. 일반 marshal은 주석을 보존하지 않는다. 새 parser를 추가하기 전에 기존 의존성으로 경계를 고정한다. |
| TOML 1.1 읽기 | go.mod의 고정 parser가 이미 1.1을 지원한다. reader를 1.0으로 제한하지 않는다. 새 writer는 가능한 한 1.0에서도 읽을 수 있는 표기를 쓴다. |
| typed JSON 작업 | 기존 CLI의 strict JSON·argv·revision 관례를 재사용한다. 임의 dotted set·silent upsert·shell 문자열 입력은 사용하지 않는다. |
| 별도 project-file 저장 adapter | private storage helper는 디렉터리 권한·ACL 전제가 다르다. 프로젝트 파일에 private-store ACL을 강제로 적용하지 않는다. |
| 정적 explain·동적 doctor 분리 | 설정 탐색이 executable 실행 권한을 암묵적으로 행사하지 않게 한다. 도구 진단은 명시적인 진단 요청에 한정한다. |
| JSON 기본 출력 유지 | 기존 automation의 stream 소유권을 유지한다. 이번 범위에서 --format text는 추가하지 않는다. |

근거 코드: [project](../internal/project/project.go), [requirements](../internal/project/requirements.go),
[network 입력](../internal/project/network.go), [실행 계획](../internal/execution/command.go),
[passive inspection](../internal/values/input_inspect.go), [현재 출력 계약](cli-output.md).

외부 근거: [TOML 1.1](https://toml.io/en/v1.1.0),
[go-toml unstable API](https://pkg.go.dev/github.com/pelletier/go-toml/v2/unstable),
[Go Rename 계약](https://go.dev/src/os/file.go),
[Windows ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew).
구체적인 저장·진단 결정은 각 하위 계약의 근거 절을 따른다.

## 구현과 수용 기준

1. schema·source adapter·neutral/legacy/registry resolver를 추가한다.
2. public diagnostic, parser/help/schema metadata와 passive preflight를 통합한다.
3. project-file 저장 adapter, typed editor와 migration을 구현한다.
4. run/process/project/proxy/dashboard/completion의 기존 호출자를 공통 모델로 연결한다.
5. 모든 공개 명령·도메인의 D01–D19, C01–C03 수용 검증과 native 플랫폼 검증을 수행한다.

필수 경계는 [편집 수용 기준](config-edit-contract.md#수용-기준)과
[진단 수용 기준](diagnostic-contract.md#도메인별-적용과-수용-기준)에 명시한다.
[합성 계약 사례](../internal/configcontract/cases.json)는 schema와 strict transport의 필수 기대값이다.
이 사례는 새 계약의 필수 기대값을 정의하며 구현한 Go provider에서도 같은 기대값을 검증한다.
schema 검증, 데이터 동등성, Linux 실행과 native OS 실행은 각각 별도의 증거다.
필수 수용 조건을 충족하기 전에는 지원 완료로 판정하지 않는다.
