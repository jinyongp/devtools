# CLI 진단·doctor 계약

[문서 목록](README.md) · [설정 모델](cli-config-redesign.md) · [설정 편집](config-edit-contract.md)

이 문서는 protocol 10 소스의 진단 계약이다. 사용 순서는 [개발환경 진단](doctor.md)을 따른다.
공통 diagnostic과 Check·DoctorReport·ErrorDetails의 구조는
[계약 schema](../internal/configcontract/schema.json)의 같은 이름의 정의를 따른다.
명령의 문장은 설명에 사용하고 자동화는 code·reason·status·retry·effects를 읽는다.

## 공통 오류

실패는 stderr에 `{"schema_version":1,"ok":false,"error":{...}}` 한 개를 반환한다.
error에는 code, message, details가 필수다. 성공의 data를 실패와 함께 반환하지 않는다.
자식 실행 뒤의 stdout/stderr passthrough와 종료 코드는 기존 계약을 유지한다.

| details 필드 | 계약 |
| --- | --- |
| operation | canonical CLI 명령 또는 도메인 작업. alias 호출도 같은 이름 |
| phase | parse, resolve, validate, preflight, lock, prepare, publish, observe, render |
| target | 공개 가능한 resource kind·이름·선택 profile/env/command와 caller가 선택한 config 경로 |
| sources | kind·path와 실제로 확보한 byte 위치. 없는 위치는 생략 |
| reason | 해당 code 안에서 구분하는 안정된 snake_case 판정 이유 |
| facts | schema가 허용한 기대/관찰 버전·revision·상태·입력 규칙·numeric OS code 등 |
| checks | 별도로 수행한 검사. 없으면 [] |
| causes | 하위 오류의 code·reason·target·source·허용 facts. 없으면 [] |
| remedies | argv·required_inputs·message·effect. 없으면 [] |
| retry | never, after_input_change, after_refresh, after_dependency_change, inspect_first, same_request |
| effects | none, committed, partial, unknown |
| report | check/fmt/doctor가 만든 전체 보고서. 해당 명령에서만 제공 |

최상위 code는 caller의 작업 실패를, causes는 보존한 하위 원인을 나타낸다.
다중 명령의 부분 성공은 `facts.outcomes`에 kind·name·state·changed와 알려진 ID·code를 보존한다.
task의 정수 revision과 설정의 문자열 revision은 각 도메인의 타입을 유지한다.
code·reason을 만들기 위해 원문 메시지를 파싱하지 않는다.
도메인 오류는 typed condition을 만들고 CLI wrapper가 scope·작업·source를 붙인다.
I/O 오류는 errors.Is/As와 numeric OS code에서 reason을 결정하며 err.Error() 원문을 덧붙이지 않는다.

remedy.effect는 read, config_write, value_write, system_write 중 하나다.
빈 argv는 자동 호출할 수 없는 수동 조치이며 필요한 값은 required_inputs에 표시한다.
수정 argv를 제공해도 실행·승인 권한이 부여된 것은 아니다.
secret 등록 remedy는 --stdin과 stdin required input을 사용하고 값은 포함하지 않는다.

source.path는 문자열 배열이다. JSON 배열 index는 문자열, literal 이름은 한 원소다.
argv source는 path에 positional 이름 또는 option 이름을 넣고 offset에 원래 argv index를 기록한다.
config/JSON의 offset은 0부터, line·column은 1부터 세는 UTF-8 byte 위치다.
source를 모르는 하위 오류에 임의의 파일·행을 붙이지 않는다.

### 공개 정보의 범위

| 영역 | 허용하는 사실 |
| --- | --- |
| CLI·설정 입력 | 실패한 명령·옵션·필드, 선언 위치, type/enum/길이 규칙, 후보 |
| 도구 | 등록 이름·선언 executable, 요구·추출 버전, 검색 origin 분류, probe 실패 종류 |
| 값·vault | profile/env/key 식별자, 선택 bundle, 보호 상태·expiry 판정과 revision; 값 없음 |
| process·ports·proxy·expose | resource ID·선택 route·phase·상태와 공개 설정의 식별자 |
| task·workstream | task/workstream/run 식별자, revision, 상태와 관련 resource; 실행 context credential 없음 |
| backup·cleanup | 사용자가 선택한 artifact 역할·request/preview ID·보존 상태 |
| storage·installer | domain/resource·작업·safe reason·numeric OS code·복구용 basename. installer의 세부 단계는 facts.stage |

전체 argv/env/PATH, secret·body 원문, private storage의 전체 경로, 토큰·session/context 권한,
도구 stdout/stderr와 installer 예외 원문을 자동 diagnostic에 복사하지 않는다.
공개 source 파일을 사용자가 명시 조회하는 config inspect와 자동 오류 projection을 구분한다.
새 도메인 facts는 schema와 domain allowlist를 먼저 갱신한다. dashboard가 raw details를 임의로 렌더링하지 않는다.
기존 storage domain/resource/numeric reason도 공통 facts로 옮겨 보존하고 UI와 같이 전환한다.

## 입력 오류·도움말

입력 오류의 code는 invalid_argument/exit 2다. reason은 최소 다음 구분을 제공한다.

| reason | 필수 사실 |
| --- | --- |
| unknown_command | 현재 group과 입력한 command token, scoped help, suggestions |
| unknown_option | option 이름, 현재 command와 scoped help |
| missing_argument / missing_option_value | 해당 이름과 필요한 입력 |
| invalid_type / invalid_value | 필드·type/enum/범위·길이 규칙 |
| unknown_field / duplicate_field | JSON/TOML path·source와 알려진 key 후보 |
| conflicting_inputs / conflicting_operations | 충돌한 입력·operation index |
| limit_exceeded | 상한과 관찰 길이. 원문 값 없음 |

후보는 현재 group의 명령, 해당 명령의 옵션 또는 알려진 필드 안에서만 계산한다.
소문자화한 Unicode code point의 Levenshtein 거리 ≤2인 후보를 거리·원래 이름순으로 정렬해 최대 3개 제시한다.
후보의 원래 이름을 유지하며 입력을 자동 수정·실행하지 않는다.
전체 목록은 해당 group help/schema에서 조회한다. 후보 목록이 전체 계약을 대신하지 않는다.

Command/Argument/Option metadata는 description, semantic type, default, enum/pattern/bounds,
조건부 필수 입력, 최소 호출 예제, body schema, output mode와 결과 읽기 규칙을 공유한다.
help는 같은 metadata를 사람이 읽게 렌더링하고 schema는 기계 계약을 반환한다.
runtime 도메인 상태 규칙은 별도 판단하되 정적인 입력 제약을 handler에만 숨기지 않는다.
모든 canonical 명령과 alias에 최소 예제를 제공하고, body 입력에는 파일 내용 예제를 함께 제공한다.

## doctor 입력과 범위

`doctor [COMMAND...]`의 옵션은 --dir, --profile, --env, --bundle, --project-only,
--check, --mode foreground|managed, --timeout DURATION이다. 기존 root isolation 옵션도 유지한다.

- --dir 기본값은 현재 디렉터리다. --mode 기본값은 foreground다.
- --timeout은 양수 Go duration이며 기본 30s다. caller deadline이 더 짧으면 그것을 따른다.
- 이름이 있으면 scope=selected, 없으면 scope=all로 프로젝트의 모든 선언 명령을 검사한다.
- 중복 명령 이름은 한 번만 검사한다. selection과 commands는 이름순이며 alias 문자열로 resource를 바꾸지 않는다.
- --project-only와 명령 이름을 함께 주면 invalid_argument다. project scope는 공통 defaults만 검사한다.
  이 scope의 주입·readiness·launcher 제약은 명령 대상이 없어 not_applicable이다.
- --profile은 config 선택을 우회하지 않고 실행의 profile override와 같은 의미다.
  파일 없이 명령 이름도 없고 명시 --profile만 있으면 scope=profile의 저장소 진단을 유지한다.
- --env는 명령마다 적용하는 명시 override이며 빈 문자열도 공통 env의 명시 선택이다. 기존처럼 주입을 활성화한다.
- --bundle은 명령별 선택을 override한다. 생략하면 command → project → all 순서다.
- 등록되지 않은 명령은 보고서의 required fail이다. 구문상 잘못된 이름과 옵션은 호출 오류다.
- 이름 없는 프로젝트는 전역/project 검사 결과와 commands=[]를 반환한다. 실행할 명령이 없다는 scope를 숨기지 않는다.

foreground는 run, managed는 process/project의 준비 제약을 검사한다.
managed에서만 readiness executable·launcher 제약을 필수 검사한다.
ready.exec 자체를 실행하거나 앱의 실제 readiness를 증명하지 않는다.
proxy 활성화·DNS/trust/provider 호출, port 배정·reserve, process 시작과 task storage 변경을 수행하지 않는다.
doctor의 ready는 선택한 mode와 scope의 **선언된 실행 전 조건**이다.
명령 내부의 미선언 의존성, 실제 작업 성공과 미요청 network 활성화를 증명하지 않는다.

config decode 실패는 configuration required fail로 보고하고 종속 검사는 unknown으로 표시한다.
이때 config_path/profile은 알 수 없으면 null이다.
selected의 명령 row는 유지하며 아직 해석하지 못한 row의 profile/env/selection도 null이다.
row가 ready이면 이 세 값은 모두 확정된 문자열이어야 한다.
경로·설정 오류를 무조건 일반 io_error로 합치거나, 보고서를 만들 수 있는 실패를 조용히 성공 check로 바꾸지 않는다.
읽기 API의 기존 maintenance lock·미완료 transaction recovery는 수행될 수 있다.
'앱을 시작하지 않는 진단'을 저장소에 어떤 파일 쓰기도 없는 명령이라고 설명하지 않는다.

## 공통 preflight

run, process, project와 doctor가 같은 검사 evaluator와 설정 resolver를 사용한다.
차이는 실제 실행 요청이 값을 인증·포트/실행 입력을 고정하는 권한을 갖는지 여부다.
독립적인 검사를 먼저 수집하고, 한 실패 때문에 다른 검사의 결과를 버리지 않는다.

1. 설정·명령·명시 override와 argv 구조를 해석한다.
2. profile/env/bundle의 존재·등록·expiry·주입 여부를 확인하고 검색 환경의 출처를 결정한다.
3. 확인 가능한 도구와 argv[0]를 검색하고 버전 probe를 수행한다.
4. public bind와 선택한 mode의 준비 제약을 확인한다. 미해결 동적 입력은 unknown이다.
5. run은 기존 권한 경계 안에서 필요한 실행 snapshot을 인증하고 달라진 입력에 의존하는 검사를 재평가한다.
6. 필수 조건이 통과한 뒤 실제 executable·값·network 입력을 고정한다.
   포트나 파일이 바뀌면 재검사하거나 실패하며 이전 결과로 그대로 실행하지 않는다.

metadata가 필요 없는 독립적인 tool lookup은 vault 실패 전후에도 수집한다.
실행 준비가 끝날 때 필수 fail 또는 unknown이 하나라도 있으면 앱을 시작하지 않는다.
기본 probe 완료와 OS의 실행 허용 여부는 같은 사실이 아니며 실제 spawn 오류도 typed 원인으로 반환한다.

### 값 인증과 검색 환경

[InspectInput](../internal/values/input_inspect.go)과 InspectCached의 passive 인증 경로를 재사용한다.
doctor는 daemon 시작, native unlock, passphrase 파생, secret 복호화와 idle 만료 연장을 하지 않는다.
warm session에서는 인증된 선택 metadata와 필요한 public 검색 값을 읽는다.
cold/locked 상태에서 인증할 수 없으면 registration을 확인된 pass로 표시하지 않는다.
수동·passphrase 잠금은 알려진 준비 실패, native 키 가용성을 검사하지 못한 상태는 unknown이다.

검색은 같은 parent policy, profile/env/bundle과 확정된 public binding을 반영한다.
parent의 PATH/PATHEXT가 확정돼 있고 선택된 값이 이를 덮어쓰지 않으면 그것으로 독립 검색할 수 있다.
영향 여부를 인증하지 못했으면 임의의 parent fallback으로 pass/fail을 단정하지 않고 environment_unresolved다.
상대 executable은 project root 기준이다. Windows는 기존 PATH/PATHEXT·launcher 규칙을 사용한다.
env_files를 일반 실행의 PATH 계층으로 추가하지 않는다.

probe child에는 profile secret과 DEVTOOLS_TASK_CONTEXT를 전달하지 않는다.
알려진 secret 이름의 parent 항목도 제거한다. 전체 parent 환경의 임의 credential을 식별해 제거했다고 보장하지 않는다.
PATH/PATHEXT가 secret이면 해당 검색 환경을 probe에 전달하지 않고 secret_search_environment로 unknown 처리한다.
public/secret 이름 충돌과 Windows의 대소문자 환경 이름 충돌은 별도 fail이다.
호출자의 로컬 환경과 daemon/CI 환경의 결과를 서로 검증 증거로 재사용하지 않는다.

### 도구 검사

각 probe의 제한은 기존 **3s, 합친 stdout/stderr 16 KiB**다.
출력의 첫 version token을 npm SemVer 범위와 비교한다. token은 128 byte 이내의
숫자 major.minor[.patch]와 SemVer prerelease/build metadata여야 한다. 두 구성요소 출력은
비교할 때 patch 0으로 정규화하고, 진단에는 원래 관측 token을 유지한다. 임의 숫자 추출·선행 0·
네 번째 구성요소·비SemVer suffix를 유효한 버전으로 보정하지 않는다.

| 조건 | status / reason |
| --- | --- |
| 확정한 환경에서 executable 없음 | fail / not_found |
| 존재하지만 권한·실행 형식 때문에 사용할 수 없음 | fail / not_executable 또는 permission_denied |
| 버전 제약 없음, executable 있음 | pass / present |
| 관측 버전이 범위를 만족 | pass / version_match |
| 유효한 관측 버전이 범위를 만족하지 않음 | fail / version_mismatch |
| token 없음 또는 잘못된 SemVer token | fail / version_unreadable |
| 동일 파일 identity·probe argv의 전체 조건 교집합이 빔 | fail / version_conflict |
| probe 비정상 종료·spawn 실패 | fail / probe_failed |
| probe의 3s 초과 | fail / timeout |
| 출력 상한 초과 | fail / output_limit |
| 환경·인증·상위 deadline 때문에 검사 불가 | unknown / 구체적 reason |
| 해당 mode에서 요구하지 않는 검사 | not_applicable, required=false |

모든 결과는 도구 이름과 선언 executable을 제공한다. 버전 제약이 있으면 조기 검색 실패에도 expected_version에 원래 범위를 보존한다.
교집합이 있지만 현재 관측 버전이 그 밖인 경우는 선언 충돌과 구분한다. 다른 명령이나 probe argv의 조건은 섞지 않는다.
조회한 private PATH와 실제 절대 executable 경로를 자동 오류에 덤프하지 않는다.

검사는 이름순으로 실행한다. 동일 실행 내에서만 같은 executable identity·size/mtime·probe argv·cwd·
실제 probe 환경 fingerprint의 결과를 공유한다. fingerprint나 raw 환경은 응답·디스크에 남기지 않는다.
expected_version은 probe cache와 분리해 각 요구사항을 별도로 판정한다.
다른 env나 다른 probe argv의 결과를 합치지 않는다. 파일 변경을 관찰하면 cache를 폐기한다.
cache는 실행 파일의 보안 attestation이나 외부 변경에 대한 완전한 고정을 의미하지 않는다.

## 상태·보고서·오류 우선순위

Check의 status는 pass/fail/unknown/not_applicable이다.
not_applicable은 required=false에만 허용한다.
required fail이 하나라도 있으면 not_ready, fail 없이 required unknown이 있으면 unknown, 나머지는 ready다.
선택 command의 상태에 전역 필수 check도 적용하며 informational check는 준비 상태를 바꾸지 않는다.
ready bool은 state=ready일 때만 true다.

DoctorReport에는 scope·mode·선택 profile·config 경로·전체 상태, 전역 checks,
명령별 env·bundle 선택·checks·상태와 summary를 제공한다.
summary의 commands는 명령 row 수, ready/not_ready/unknown은 row별 상태 수다.
required_checks/passed/failed/unknown_checks는 전역 및 모든 명령 row의 필수 check 합계다.
동일 probe를 재사용해도 각 명령의 check는 별도로 센다. 결과 목록을 조용히 생략하지 않는다.

기본 doctor는 보고서를 만들면 exit 0, ok=true/data=DoctorReport다.
--check는 ready일 때만 같은 성공 응답이다. not_ready/unknown이면 requirements_failed/exit 3이고
동일 보고서는 error.details.report에 있다.
30s 전체 기한이 끝나면 미실행 항목을 unknown/deadline_exceeded로 채워 보고한다.
명시 취소는 canceled/exit 130이며 확보한 보고서는 details.report에 보존한다.

실행 요청의 실패 우선순위는 다음과 같다.

1. 잘못된 호출·설정·미등록 명령: 해당 invalid_argument/invalid_config/command_not_found.
2. 확정한 실행 환경에서 argv[0] 미발견: executable_not_found/127.
3. 그 밖의 필수 fail/unknown: requirements_failed/3.
4. 준비 통과 뒤 실제 spawn/스트림/child 종료: 기존 실행 계약과 typed OS 원인.

argv[0] 미발견과 다른 도구 누락이 겹쳐도 모든 checks를 포함한다.
하위 vault·port·storage 원인은 causes로 보존한다.
환경을 결정하지 못한 상태를 executable_not_found로 바꾸지 않는다.

remedy는 실패한 이름·dir·명시 profile/env/bundle·mode를 유지한 doctor 호출을 제공한다.
이를 위해 새 doctor가 받지 않는 옵션이나 다른 기본 범위로 안내하지 않는다.
--env 같은 옵션은 doctor에서는 이름 앞뒤에 허용하지만 기존 run에서는 명령 이름 앞에 두는 경계를 유지한다.

## 도메인별 적용과 수용 기준

도메인의 기존 condition code를 불필요하게 다시 분류하지 않는다.
설정/실행의 새 code와 doctor 범위는 protocol 10에서 전환하고 모든 consumer의 strict schema를 같이 갱신한다.
그 외 기존 code는 공통 context·원인·remedy를 보강한다.
인증 oracle을 만들지 않도록 암호 오류와 손상 ciphertext의 구분 제한은 유지한다.

- CLI parser·TOML decoder·body validator가 source와 expected 규칙을 잃지 않는다.
- services/ports/proxy/expose/CA/cleanup이 target·현재 상태·실패 phase와 request/preview 식별자를 보존한다.
- backup/profile/import가 입력 파일 역할과 safe I/O reason을 구분한다.
- values/vault/task가 원문 값 없이 scope·revision·관련 resource와 다음 조회를 제공한다.
- installer/update가 typed stage와 OS reason을 보존하고 raw stderr나 예외를 덤프하지 않는다.
- diagnostics와 dashboard가 동일 public diagnostic을 소비하고 저장소 전용 allowlist로 다른 도메인의 안내를 지우지 않는다.
- 모든 remedy argv를 실제 command schema와 대조한다. required input은 자동 값으로 채우지 않는다.
- doctor/run/process/project의 같은 입력은 같은 필수 검사 판정을 갖는다.
  foreground/managed와 caller 환경, passive/실제 인증의 차이는 scope와 unknown reason으로 드러낸다.
- 캐시 분리, 두 env의 다른 PATH, secret PATH, cold/warm vault, idle deadline 불변과 probe 출력 canary 비노출을 검사한다.
- executable+여러 도구 동시 누락, probe timeout·출력 초과·다중 version token, 명시 취소·전체 deadline을 검사한다.
- native Windows/macOS/Linux의 실제 검색·launcher·권한과 UI 오류 소비를 각각 검증한다.

근거: [현재 doctor](../internal/doctor/doctor.go)의 probe 상한·버전 비교,
[현재 실행 timeout](../internal/execution/input.go), [실행 모델](../internal/execution/command.go),
[passive metadata 인증](../internal/values/input_inspect.go),
[InspectCached](../internal/vault/session.go), [parent 정책](../internal/process/parent.go),
[기존 실행 입력 고정](../internal/execution/snapshot.go).
기본 30s는 기존 실행 준비 상한을 재사용하며 --timeout은 큰 프로젝트에서 명시적으로 조절한다.
