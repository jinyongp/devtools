# CLI 출력 계약

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

이 문서는 현재 소스의 CLI protocol v15 입력·출력 규칙을 설명합니다. v6의 run 인자 규칙은 유지합니다.
이전 v0.22.3은 protocol v5입니다. 이전 protocol 전환과 JSON 경로 변경은
아래 마이그레이션 절에 정리했습니다. 저장 파일 형식과 dashboard HTTP API는 CLI protocol
버전과 별개이며, `devtools.toml`에는 버전 필드를 두지 않습니다.

## 출력 종류 확인

`devtools schema COMMAND`의 `data.output_mode`와 `data.presentation`을 함께 확인합니다.
`output_mode`는 stdout의 소유권, `presentation.formats`는 구조화 결과의 표시 형식입니다.

`devtools --format auto|text|json COMMAND`의 루트 옵션이 `DEVTOOLS_FORMAT`보다 우선합니다.
둘 다 없으면 auto이며, 알려진 agent 신호나 non-TTY stdout에서는 JSON, 일반 터미널에서는
text입니다. 명시한 auto도 환경변수보다 우선합니다. 빈 환경값은 미설정이고 나머지 값은
소문자 세 값만 허용합니다. 잘못된 format·중복 root 옵션은 JSON stderr invalid_argument/2입니다.
선택된 유효 flag가 있으면 잘못된 환경값은 무시합니다. 임의 agent 변수 설정·새 TOML 필드는 없습니다.

```sh
devtools --format json command list
devtools --format text command list > commands.txt
```

자동 감지는 권한·신원·샌드박스 판정이 아닙니다. agent 변수가 사람의 자식 셸에 상속되거나
알려지지 않은 agent가 PTY를 쓰면 추론이 틀릴 수 있으므로 안정적인 소비자는 JSON을 명시합니다.
`CI`, `TERM`, 샌드박스 변수는 agent 신호로 사용하지 않습니다.

| `output_mode` | 해당 명령 | stdout |
| --- | --- | --- |
| `json` | 조회·변경·진단 | 선택된 JSON envelope 또는 text |
| `json` + machine-only presentation | `schema`, `config schema` | 항상 JSON |
| `text` | `help`, 인자 없는 호출, `--help`·`-h` | 사람이 읽는 도움말 |
| `artifact` | `completion bash`, `completion zsh`, `completion fish`, `completion powershell` | 생성한 셸 스크립트 원문 |
| `passthrough` | `command run`, 별칭 `run` | 자식 출력 stream; 실행별 출력 보호 적용 |

devtools 옵션 위치의 `--help`는 텍스트 도움말을 반환합니다. `devtools run --help`는
devtools 도움말입니다. 자식 프로그램의 도움말은 protocol v5에서
`devtools run NAME -- --help`로 요청하며, protocol v6에서는 `devtools run NAME --help`도
같이 동작합니다. 자식 출력에는 실행별 출력 보호 정책을 적용합니다.
내부 자동완성 요청인 `__complete`는 공개 명령 카탈로그에 포함하지 않는 줄 단위 프로토콜입니다.

생성물은 그대로 파일에 저장할 수 있습니다. 실행 명령은 stdout/stderr를 JSON으로 감싸지 않고 종료 코드를 보존합니다. 새 실행의 non-TTY 출력 보호와 raw 예외는 [실행 출력 보호](output-protection.md)를 따릅니다.

```sh
# 파일에 저장할 스크립트 원문
mkdir -p ~/.config/fish/completions
devtools completion fish > ~/.config/fish/completions/devtools.fish

# 등록된 명령의 출력은 JSON으로 감싸지 않음
devtools command run test
```

`output_schema`는 JSON 성공 응답의 `data`를 설명합니다. 텍스트·생성물·자식 프로세스 출력에는 적용하지 않으므로 이 세 종류의 명령에는 `output_schema`를 제공하지 않습니다.

## 성공과 실패

구조화 결과는 성공 시 stdout, devtools 실패는 stderr에 선택한 형식으로 출력합니다.
아래 envelope 계약은 JSON 표현에 적용합니다. 성공에는 항상 `data`가 있고 `error`는 없습니다.
결과가 없는 경우에도 `data` 필드 자체를 생략하지 않습니다.

```json
{"schema_version":1,"ok":true,"data":{"items":[]}}
```

JSON 실패는 `data` 대신 `error`를 제공합니다. `error.code`는 분기에 사용하고 `message`는 설명으로 사용합니다. protocol 10부터 공통 `details`를 항상 제공합니다.

```json
{"schema_version":1,"ok":false,"error":{"code":"command_not_found","message":"The selected project command is not defined.","details":{"operation":"command run","phase":"validate","target":{"kind":"command","name":"dev:web"},"sources":[],"reason":"command_not_found","facts":{},"checks":[],"causes":[],"remedies":[],"retry":"after_input_change","effects":"none"}}}
```

도움말·artifact·run의 devtools 오류도 선택된 형식을 사용하고 native stdout은 변환하지 않습니다.
schema 오류는 JSON이며 명시 `--format text`로 정상 schema를 요청하면 실행 전 JSON 입력 오류로
거절합니다. 기존 parser가 help로 해석한 `schema --help`와 `config schema --help`는 text 도움말입니다.
모든 argv에서 help를 검색하지 않으므로 기존 unknown option/child 경계를 유지합니다.
stdout 선택이 JSON이면 stderr만 TTY여도 JSON 오류입니다. 기존 prompt나 자식 stderr 뒤에 진단이
올 수 있어 stderr 전체가 하나의 JSON이라는 보장은 없습니다. 자식 보호 정책·exit/signal은 유지합니다.

`ok: true`는 요청을 처리했다는 뜻이지 진단 대상이 정상이라는 뜻은 아닙니다. `doctor`의 `data.ready`, `process check`의 `data.readiness.ready`, 작업 검사의 `valid` 등은 별도로 확인해야 합니다. 기존 종료 코드와 진단 판정 규칙은 유지합니다.

`schema_version`은 envelope 버전 1이며 CLI machine contract는 현재 `protocol_version: 15`입니다.
`version`과 `schema`의 `data.protocol_version`에서 확인합니다. TOML·저장소·dashboard/private wire
버전은 별개입니다. format 지원 여부는 숫자 추측 대신 discovery의 `global_options`로 확인하세요.

## text 표시

text도 같은 한 번의 public 결과를 표시합니다. 오류·경고·안내 원문, 효과·재시도 규칙과 action
토큰을 재작성하지 않습니다. 목록은 비교 표, 좁거나 긴 값·복잡한 Unicode가 있으면 세로 배치로
전환합니다. ID·revision·cursor·URL은 자르지 않고, 새 공개 필드는 Details에 남깁니다.
미리보기·재전송·변경 없음과 task의 lifecycle/completion/execution은 구분합니다.

완성된 Next 행동은 설명 아래의 독립된 한 줄을 전체 복사할 수 있습니다. prompt·번호·SGR·
삽입 개행·말줄임 없이 필요한 shell 인용을 적용합니다. Unix POSIX/Fish 표현이 다르면
shell label을 윗줄에 나눠 표시하고 Windows는 PowerShell 대상입니다. 남은 필수 입력이나
원 token을 보존할 수 없는 control/native 전달 조합은 실행 가능한 것처럼 표시하지 않습니다.

실제 출력 stream이 TTY이고 ANSI를 지원할 때 제목·상태에만 최소 강조를 사용합니다.
non-TTY·TERM=dumb·지원 관측 실패는 plain입니다. 비어 있지 않은 NO_COLOR는 전경색을
끄고 bold/dim은 유지합니다. 중요 경고·ID·복구 행동은 dim으로 숨기지 않습니다.
색·공백·열 정렬은 machine 파싱 계약이 아니므로 자동화는 `--format json`을 사용하세요.
text 렌더 실패 시 기존 오류는 전체 escaped public 진단과 원 종료 코드를 보존합니다.
성공 결과를 표시할 수 없으면 internal_error/1, 인코딩·쓰기 실패는 io_error/1이며
성공 handler 반환 뒤 새 출력 오류는 effects:unknown/retry:inspect_first입니다. 자동 재실행하지 않습니다.

실행 준비 중 사용자 취소는 `canceled`와 종료 코드 130, 준비 기한 초과는
`deadline_exceeded`와 종료 코드 3으로 구분합니다. 저장이 완료된 뒤 도착한 취소는
확인된 성공 결과를 덮어쓰지 않습니다. 이미 만료된 실행 입력은
`input_snapshot_expired`와 종료 코드 3으로 거부합니다. 게시 중 실패는 저장소 복구 규칙을 따르며,
실패 응답만으로 변경이 없었다고 판단하지 않습니다.

## JSON 데이터 구조

목록은 `items` 배열로 반환합니다. 결과가 없으면 `null`이나 필드 생략 대신 `[]`를 사용합니다. 기존 페이지네이션이 있는 명령은 `next_cursor` 등 해당 명령의 메타데이터를 함께 반환합니다. 페이지네이션이 없던 명령에 새 페이지네이션을 추가하지는 않습니다.

단일 리소스는 `item`에 둡니다. `profile`, `revision`, 조회에 부가되는 `paths` 등은 리소스와 구별되는 최상위 메타데이터입니다. `task next`처럼 빈 선택이 정상인 명령은 `item: null`을 반환합니다. 조회할 명령이 없는 `command inspect`처럼 기존에 오류였던 경우는 계속 오류입니다.

```json
{
  "schema_version": 1,
  "ok": true,
  "data": {
    "profile": "app",
    "items": [
      {"name":"test","exec":["go","test","./..."],"inject":false,"env":"","serve":[]}
    ]
  }
}
```

수정 명령은 `changed`를 리소스 바깥에 둡니다. 리소스를 반환하는 수정은 `item`과 함께 제공하고, 값 등록·해제처럼 확인 응답만 필요한 수정은 `changed`와 대상 범위만 반환합니다. 반환 형식을 맞추기 위해 secret 값이나 불필요한 리소스를 추가하지 않습니다.

```json
{"schema_version":1,"ok":true,"data":{"profile":"app","changed":false}}
```

`changed: false`도 항상 명시합니다. `init`, 포트 할당, 값 등록, 이미 복구한 정리 항목 등에서 변경이 필요 없으면 false입니다. 파일을 항상 새로 저장하는 `backup configure`, 백업·키 생성, 실행 파일을 교체하는 `update`는 성공 시 true입니다. `dashboard`는 실행 중인 서버를 재사용해도 새 접속 링크를 발급하므로 true를 반환합니다.

요청 ID로 재시도를 지원하는 명령에는 `replayed`도 명시합니다. 동일 요청의 재전송이면 최초 결과의 `changed`를 유지하면서 `replayed: true`를 반환합니다. 따라서 재시도 결과의 `changed: true`를 새로운 변경이 한 번 더 수행됐다는 뜻으로 해석하면 안 됩니다. 재시도를 지원하지 않는 명령에 임의의 `replayed` 필드를 붙이지 않습니다.

진단·계획·다중 대상 작업은 의미를 가진 보고서 필드를 유지합니다. 예를 들어 `doctor`는 `checks`, 작업 관계는 `nodes`·`edges`, 복구 계획은 `targets`, 포트 정리는 `removed`·`retained`를 사용합니다. 단일 값 조회인 `var get`의 `value`·`metadata`, 로그 조회의 `content`도 그대로입니다. `schema --all`은 명령 카탈로그 문서이므로 그 안의 `commands`를 유지합니다.

## 명령군별 적용 범위

| 명령군 | 데이터 구조 |
| --- | --- |
| `command`, `env`, `variable`, `secret`, `bundle` 목록 | `items`; 해당하는 경우 `profile` |
| `command inspect`, `project inspect` | `item`; `profile` 또는 `paths` 메타데이터 |
| `init` | `item`, `changed` |
| `port`, `instance` 목록·단일 조회 | `items` 또는 `item` |
| 포트 할당, instance 이름·위치 변경 | `item`, `changed` |
| 포트 해제, instance 제거 | `changed` |
| `port prune` | `removed`, `retained`, `changed` 보고서 |
| `process` 목록·상태·변경 | `items`, `item`, 또는 `item`·`changed`·`replayed` |
| `process check/wait/logs` | readiness 또는 로그 보고서 |
| `vault status`, init/recover/lock/unlock | 각각 `item`, `item`·`changed`; 상태 조회는 키를 해제하지 않음 |
| `vault rewrap` | `item`, `request_id`, `changed`, `replayed`; 같은 root key의 보호 변경 |
| `bundle set/rename/remove` | `profile`, `bundle`, `revision`, `changed`, `replayed` |
| `secret generate` | `profile`, `changed`, `revision`, `replayed`; 해당하면 값 없는 `warnings` |
| `values revision` | `profile`, `env`, 불투명 문자열 `revision`, `integrity_verified` |
| `values request show` | `profile`, 저장된 요청 결과 메타데이터 `item` |
| `values execution prepare/approve/deliver/cancel/report` | `profile`, `item`, `changed`, `replayed`; `item.revision`은 실행 record의 정수 |
| `values execution status/list` | 각각 `profile`·`item`, `profile`·`items`·`next_cursor` |
| `values runner address` | `root_id`, `vault_id`, `runner_id`, `instance_id`, `endpoint`, `protocol`, `redaction_protocol` |
| `values runner show/register/unregister` | `profile: ""`, `item`; 변경에는 `changed`, `replayed` 추가 |
| `values inspect`, `values uses` | 각각 `profile`·`env`·`selection`·`values`, `profile`·`items`; 원문 없는 관찰 보고서 |
| `proxy` 목록·상태·변경 | `items`, `item`, 또는 `item`·`changed`·`replayed` |
| `dashboard`·`dashboard start`, status, stop | 각각 `item`·`changed`, `item`, `changed` |
| `backup keygen/configure/create`, inspect | 각각 `item`·`changed`, `item` |
| `backup restore` | 복구 계획·적용 보고서와 `changed`·`replayed`; 미리보기는 둘 다 false |
| `profile list`, `profile inspect`, `profile diff` | 각각 `items`, `item`, `left`·`right`·`different`와 메타데이터 차이 보고서 |
| `profile transfer prepare` | 공개 `recipient`를 담은 `item`, `changed` |
| `profile export` | `item`, `changed` |
| `profile import` | `item`, `digest`, `target_exists`, `diff`, `changed`, `replayed`, `safety_backup`; 미리보기는 변경하지 않음 |
| `backup status` | 공개 설정 상태를 담은 `item` |
| `project status` | `profile`, `directory`, 활성 실행 `items` |
| `project logs` | 활성 실행 `id`, `command`, 보관된 `content` (새 실행은 저장 전 마스킹) |
| `project up/restart/down` | `action`, `profile`, `directory`, 명령별 `items`, `changed`, `replayed` |
| `cleanup archives`, apply, restore/purge | 각각 `items`, `items`·`changed`·`replayed`, `item`·`changed` |
| `cleanup preview` | 계획 ID·만료 시각·`items` |
| `task`·workstream·validation | 기존 `item`·`items`와 리비전·변경·재시도 메타데이터; context·tree·export·검사는 보고서 |
| `diagnostics` | 전체 로컬 subsystem의 안전한 metadata 상태 보고서와 `ready`·`issues` |
| `version`, `update`, `import`, `doctor`, `schema` | 각 기능의 보고서; `update`와 `import`는 변경 여부 포함 |

## protocol v15 마이그레이션

- 항상 JSON이던 일반 구조화 명령의 기본값을 auto로 전환합니다. JSON의 data/envelope는 유지합니다.
- root-prefix `--format`과 `DEVTOOLS_FORMAT`, 응답당 `global_options`와 명령별 `presentation`을 추가합니다.
  기존 `--output auto|redacted|raw`는 자식 stream 보호 옵션으로 그대로입니다.
- old/new CLI를 함께 다루는 consumer는 자식 환경에 DEVTOOLS_FORMAT=json을 지정해
  `schema version`을 먼저 조회합니다. global_options에 root-prefix format/json 지원이 있을 때만
  후속 호출에 `--format json`을 붙입니다. 지원 여부를 알아내려고 mutation을 실패/재시도하지 않습니다.
- 사용자 설정·저장된 요청/replay·fingerprint에 format을 넣지 않습니다. 표시 전환은 같은 업무
  UUID를 다른 요청으로 만들지 않고, 자식 실행·도움말·completion/private wire도 재포장하지 않습니다.

## protocol v14 마이그레이션

현재 소스는 `vault rewrap`을 추가합니다. 명시한 기존·대상 backend와 UUID로 같은
root key의 암호 또는 native 보호를 바꿉니다. 입력·재시도 절차는
[로컬 secret 보호](secret-protection.md#암호와-보호-방식-바꾸기)를 따릅니다.

vault 상태의 `item.rewrap`은 pending 작업이 있을 때만 제공되는 선택 필드입니다.
`request_id`, 선택적 `from_backend`·`backend`, `slot_published`, `completed`,
`integrity_verified:false`를 포함하며 인증 증명이 아닙니다. pending 복구 오류는 기존
`details.target.request_id`, `retry:same_request`, 범위가 유지된 `remedies`로 재개를
안내합니다. 실패의 `effects:unknown`을 무변경으로 해석하지 않습니다.

`config schema`의 응답과 공개 output schema도 protocol 14로 일치시킵니다.
이전 output schema의 잘못 남은 `protocol_version.const:10`에 의존한 consumer는
실제 CLI의 schema를 다시 읽어야 합니다. 설정 파일에는 버전 필드를 추가하지 않습니다.

내부 키 세션 protocol은 4이며 CLI가 이전 세션을 퇴역시키고 새로 연결합니다.
보호 변경 후 이전 세션 키·실행 ticket은 다시 사용할 수 없습니다. 수동 잠금은 유지하고
저장 형식과 envelope `schema_version`은 1을 유지합니다. 실행 중인 앱 환경과 이전
키 사본의 회수는 제공하지 않습니다.

## protocol v13 마이그레이션

진단·후속 argv는 실제 profile/env/bundle과 선택된 isolation root를 유지합니다.
direct 실행 실패는 해당 executable과 원래 argv 위치를 가리킵니다. 다른 장치의
transfer 준비처럼 목적지가 별도인 행동에는 원 저장소 root를 붙이지 않습니다.

strict consumer는 Facts의 `expected_kind/actual_kind`, `expected_type/observed_type`,
`import_items`, `limit_kind/observed/limit`와 `sources.kind: dotenv`를 허용해야 합니다.
dotenv의 확인된 line만 있을 때 column은 생략합니다. redaction 관찰량은 실패 지점의
정규화된 개수·byte·state·output 수이며 전체 secret 수를 뜻하지 않습니다.

`values execution status`는 execution ID와 `--request-id` 중 정확히 하나를 받습니다.
help의 syntax 예시는 placeholder를 포함할 수 있습니다. 상태형 명령은 `workflow`에
설명된 선행 조회와 새 UUID/동일 입력 재시도 구분을 따릅니다.
envelope 1, 버전 없는 TOML, vault/profile 저장 형식과 request replay는 유지합니다.
복구·effects의 의미는 [진단 계약](diagnostic-contract.md)을 따릅니다.

## protocol v12 마이그레이션

`run`, `process start`, `project up`은 matching served proxy를 기본 자동 준비합니다.
기존 포트 실행을 유지하려면 `--no-proxy` 또는 `--proxy=false`를 사용하세요.
`--proxy`는 matching route를 요구하며 disabled route도 재활성화합니다.
옵션은 run의 이름 앞에 둡니다. 두 flag를 동시에 주면 값과 관계없이 오류입니다.
기존 실행과 restart는 기록된 모드를 유지하며 구 UUID는 기존 계약으로 재현합니다.
새 UUID의 준비 의도는 고정되므로 auto/on/off 변경 시 `request_conflict`입니다.
envelope 1, 버전 없는 TOML, daemon control 2, task·registry 저장 형식은 유지합니다.
설정과 예외는 [로컬 네트워크 가이드](local-network.md)를 따릅니다.

## protocol v10 마이그레이션

설정 편집, 설명과 진단은 [설정 가이드](configuration.md), [doctor](doctor.md),
[공통 진단 계약](diagnostic-contract.md)을 따릅니다. envelope는 1을 유지합니다.

| 대상 | protocol v10 |
| --- | --- |
| `config` | inspect/check/schema/fmt/edit/migrate와 tool/preset 편집 제공; 수정·삭제는 revision 필요 |
| `command` | add/update/remove/rename/explain 제공; 기존 조회·실행 유지 |
| 오류 | `details.operation/phase/target/sources/reason/facts/checks/causes/remedies/retry/effects` 공통 구조 |
| 실행 파일 누락 | `executable_not_found`, 종료 127; 다른 독립 검사 결과도 `checks`에 보존 |
| `doctor` 기본 범위 | 전체 명령; 결과는 공통 `checks`, 명령별 `commands[].checks`, `summary` |
| 검사 상태 | pass/fail/unknown/not_applicable; 확인하지 못한 상태를 성공으로 간주하지 않음 |
| `doctor --check` | 미준비·미확인 시 종료 3, 전체 보고서는 `error.details.report` |
| 하위 원인·부분 성공 | `causes`와 `facts.outcomes`; 이전 `details.items`를 읽던 consumer 변경 필요 |
| remedy | `effect` 추가: read/config_write/value_write/system_write |
| help/schema | 명령 예제와 JSON `body_example` 제공 |

기존 legacy requirements 파일은 계속 읽습니다. 새 registry 필드와 혼합하면 거절하며,
변환은 `config migrate`의 preview digest와 source revision으로 명시 승인합니다.
설치본의 protocol과 명령 존재를 확인한 뒤 consumer를 전환하세요.

## protocol v9 마이그레이션

값·env·import 변경에 `--revision`·`--request-id` 쌍을 선택적으로 전달하면
기존 결과에 `revision`, `replayed`가 추가됩니다. 쌍을 생략한 기존 호출의
출력 필드는 유지합니다. `values revision`은 값 없는 편집 기준을,
`values request show UUID`는 저장된 결과 메타데이터를 반환합니다.
set의 `--create-only`는 두 guard가 모두 있어야 사용할 수 있습니다.
dashboard 재사용 인증 프로토콜도 9이며 key-session 프로토콜 3과 저장 형식 1은 유지합니다.

`values execution` 결과의 `delivery_state`와 `execution_state`는 별개입니다.
`delivered`는 수신 확인이며 앱 실행·값 사용·외부 인증 성공을 뜻하지 않습니다.
원문 값·패턴·키 세션 ticket은 공개 결과에 없습니다. 원래 요청의 replay와 현재 status가
다를 수 있으므로 [복구 계약](secret-automation.md#recover-an-uncertain-delivery-or-execution)을 따릅니다.

## protocol v8에서 유지하는 규칙

공통 envelope는 `schema_version: 1`을 유지합니다. `diagnostics.paths.source`와
`diagnostics.platform.isolation`이 추가되고, 실패한 `issues`에는 안전한 저장소
`details`가 추가될 수 있습니다. 엄격한 응답 파서는 설치본의 schema를 다시 조회하세요.
저장 파일 형식, 기존 저장소 오류 코드·메시지·종료 코드는 유지합니다.
task·저장소 잠금의 기한 초과는 `deadline_exceeded`/3으로 통일하고,
명시적 취소는 계속 `canceled`/130으로 반환합니다.

저장소 오류의 `details`는 다음 공통 진단 규칙을 따릅니다. 기능별 `remedies`,
부분 결과 등의 기존 필드는 함께 보존합니다.

| 필드 | 의미 |
| --- | --- |
| `domain` | 실패가 발생한 기능 영역: `tasks`, `values`, `services` 등 |
| `operation` | 최초 확인된 실패 작업: `create_directory`, `open_file`, `validate_wal_checksum` 등 |
| `resource` | 경로 대신 대상 역할: `data_root`, `task_store`, `query_cache` 등 |
| `reason` | 원인 분류: `access_denied`, `invalid_storage`, `unexpected_acl`, `disk_full` 등. 분류 근거가 없으면 `unknown` |
| `os_error`, `os_error_domain` | 원본 OS 오류가 있는 경우에만 숫자와 `win32`/`errno`를 제공 |

예를 들어 유지보수 잠금을 준비하다 data 폴더 생성이 거부되면 다음처럼 반환합니다.

```json
{"schema_version":1,"ok":false,"error":{"code":"storage_error","message":"Cannot access private task storage.","details":{"domain":"tasks","operation":"create_directory","resource":"data_root","reason":"access_denied","os_error_domain":"win32","os_error":5}}}
```

오류에는 실제 경로, 원본 OS 오류 문자열, ACL 원문, 저장된 값·키·credential을
추가하지 않습니다. 실제 경로는 `project inspect` 또는 `diagnostics.paths`에서 확인하세요.
`paths.source`는 `isolated_root`, `windows_known_folder`, `user_directories` 중 하나입니다.
집계 호출자가 기본 경로와 다른 경로를 전달하면 `provided`로 표시합니다.
Windows `platform.isolation`은 현재 프로세스 토큰으로 확인한 `appcontainer`,
`restricted`, `unrestricted` 또는 `unknown`입니다. `unrestricted`는 모든 호스트
정책이나 외부 제한이 없다는 보장이 아닙니다. 다른 OS에서는 `unknown`을 반환합니다.

조회와 diagnostics는 일관성 확보를 위해 저장소·잠금 디렉터리와 잠금 파일을 만들 수
있습니다. 파일 쓰기가 전혀 없는 동작으로 해석하지 마세요. 오류 개선을 위해 세션 해제,
ACL 교정이나 저장소 자동 전환을 수행하지 않습니다.
Windows Known Folder는 격리 환경에서 별도 경로를 반환할 수 있으므로 빈 목록의 저장소
출처도 확인하세요. 오류 5만으로 특정 ACL이나 샌드박스 규칙을 원인으로 단정하지 않습니다.
AppContainer에서 Known Folder 기본 저장소를 선택하면 `platform.limitations`에
`appcontainer_known_folder_store`가 포함됩니다. 별도 저장소 범위를 알리는 정보이며
그 자체로 `ready: false`가 되지는 않습니다. 명시적 격리 저장소에는 이 표시를 붙이지 않습니다.
AppContainer의 저장소 생성·ACL 검증 정책은 [Windows 안내](windows.md#storage-and-processes)를
따릅니다. 해당 identity 정책이 있어도 호스트 샌드박스가 접근을 거부할 수 있습니다.

Windows 패키지 신원이 있는 프로세스에서 기본 저장소를 선택하면
`packaged_windows_default_store`가 포함됩니다. 패키지 신원 확인에 실패하면
`windows_package_identity_unknown`으로 표시합니다. 두 표시는 건강 실패가 아니며,
실제 파일의 재지정 여부를 확정하지 않습니다. `unrestricted`여도 패키지의
AppData 재지정이 적용될 수 있으며, 같은 논리 경로가 같은 물리 저장소를
보장하지 않습니다. 패키지 이름과 사용자 SID는 출력하지 않습니다.

`windows_storage_path_aliases`는 기존 구조 항목의 핸들에서 관측한 최종 경로와
고정한 data 루트 아래의 예상 경로가 다르다는 뜻입니다. 패키지 신원과 독립적으로
확인하며, 경로 문자열을 보안 판정에 사용하지 않습니다.
`windows_storage_mapping_unobserved`는 검사할 항목이 없거나 일부 메타데이터를
확인하지 못했다는 뜻입니다. 고정 항목만 검사하므로 표시가 없다는 이유로 모든
파일의 위치나 기존 데이터 연속성을 보장하지 않습니다. 두 표시 모두 `ready`를
바꾸지 않으며, [Windows 저장소 안내](windows.md#storage-and-processes)를 따릅니다.

## protocol v7 마이그레이션

CLI protocol v7은 Windows 지원과 함께 도입되었습니다. `version`/`schema`의
`data.protocol_version`이 7이고 envelope `schema_version`은 1입니다.
`diagnostics`에 필수 `platform` 객체가 추가되고 `completion`의 공개 shell 목록에
`powershell`이 추가됩니다. 설치된 binary의 schema를 다시 조회하세요.
`run`의 child 인자 경계·출력 모드 이름과 버전 없는 `devtools.toml`은 v6와 같습니다.
현재 소스의 출력 보호는 같은 `passthrough` 모드에서 non-TTY stream을 기본 마스킹하므로
이 모드를 byte 무변경 보장으로 해석하지 마세요. vault·bundle 지원은 protocol 번호만으로
판정하지 말고 설치한 CLI의 해당 명령 schema를 확인하세요.

## protocol v6 마이그레이션

`command run`과 별칭 `run`, `cmd run`은 명령 이름을 기준으로 인자를 나눕니다. devtools 옵션은 이름 앞에서 해석하고, 이름 뒤의 토큰은 모두 실행 프로그램에 전달합니다. 공통 envelope와 JSON 응답 구조, 저장 파일 형식은 그대로입니다.

| 대상 | Protocol v5 | Protocol v6 |
| --- | --- | --- |
| `version`, `schema` | `data.protocol_version: 5` | `data.protocol_version: 6` |
| 이름 명령의 devtools 옵션 | `devtools run test --env local` | `devtools run --env local test` |
| 이름 명령의 추가 인자 | `devtools run test -- --watch` | `devtools run test --watch`; 기존 구분자 형식도 지원 |
| 실행 프로그램의 도움말 | `devtools run test -- --help` | `devtools run test --help` |
| 직접 프로그램 실행 | `devtools run --env local -- PROGRAM ARG...` | 동일 |

기존 스크립트에서 명령 이름 뒤에 둔 `--env`, `--profile`은 이름 앞으로 옮기세요.
이름 뒤에 남겨 두면 실행 프로그램에 그대로 전달됩니다. 다른 명령의 옵션 위치는
바뀌지 않습니다. `devtools version`과 `devtools schema command run`으로 설치한 빌드를
확인하고, 업데이트 뒤에는 해당 계약을 다시 조회하세요.

## protocol v5 마이그레이션

Protocol v4 자동화 중 profile transfer 입력·discovery를 사용하는 코드는 다음 계약을
함께 변경해야 합니다. 공통 envelope `schema_version`은 1로 유지되고 generic
`backup` command 계약은 바뀌지 않습니다.

| 대상 | Protocol v4 | Protocol v5 |
| --- | --- | --- |
| `version`, `schema` | `data.protocol_version: 4` | `data.protocol_version: 5` |
| 기본 transfer 시작점 | 대상에서 `profile transfer prepare` 후 recipient를 원본으로 전달 | 원본에서 바로 `profile export`; 터미널은 passphrase prompt |
| non-interactive export | recipient 기반 | `--passphrase-file` 또는 `--passphrase-stdin`; recipient 방식은 advanced mode |
| `profile import` 기본 | prepared local identity | passphrase archive는 prompt; recipient archive는 prepared identity |
| explicit import key | `--identity-file` | 기존 recipient archive용 `--identity-file`; passphrase는 `--passphrase-file`/stdin |
| safety archive | import identity recipient로 암호화 | 현재 import와 같은 방식(passphrase 또는 recipient)으로 암호화 |
| transfer 파일 이동 | encrypted archive 하나 | encrypted archive 하나; destination preflight 불필요 |
| transfer 오류 | identity/recipient 중심 | `passphrase_required`, `passphrase_mismatch`, identity 오류, `output_exists` 등을 구분 |

passphrase는 argv나 환경변수 입력으로 제공하지 않습니다. interactive export는 두 번,
interactive import는 한 번 echo 없이 입력합니다. 자동화에서는 regular file 또는 piped
stdin을 사용합니다. `profile transfer prepare`, `--recipient`, `--recipient-file`,
`--identity-file`은 기존 X25519 자동화를 위한 advanced compatibility 경로로 유지됩니다.

## protocol v4 마이그레이션

Protocol v3 자동화 중 profile transfer 입력·discovery를 사용하는 코드는 다음 계약을
함께 변경해야 합니다. 공통 envelope `schema_version`은 1로 유지되고 backup command
계약은 바뀌지 않습니다.

| 대상 | Protocol v3 | Protocol v4 |
| --- | --- | --- |
| `version`, `schema` | `data.protocol_version: 3` | `data.protocol_version: 4` |
| 대상 기기 준비 | 사용자가 `backup keygen`으로 identity/recipient 파일 관리 | `profile transfer prepare`가 private identity를 내부 보관하고 공개 recipient만 반환 |
| `profile export` recipient | configured backup recipient로 생략 가능 | `--recipient` 또는 `--recipient-file` 중 정확히 하나 필수 |
| `profile export` output | configured backup directory로 생략 가능 | 생략 시 `./<profile>.age`; backup 설정을 사용하지 않음 |
| `profile import` identity | `--identity-file` 필수 | 생략 시 prepared local transfer identity 사용; explicit file은 override |
| 기존 profile 교체 | configured backup directory/recipient로 safety backup | import identity의 recipient와 private transfer recovery directory로 safety archive 생성 |
| transfer 복구 안내 | backup 설정 중심 오류 | `recipient_required`, `transfer_identity_missing`, `transfer_identity_invalid`, `output_exists` 등 transfer-specific error와 structured remedy |

정상 기기 간 이동은 대상에서 prepare한 공개 recipient 문자열을 원본에 전달하고,
원본에서 생성한 encrypted archive 하나만 대상에 복사합니다. Private identity는 대상
기기를 떠나지 않습니다. 기존 archive 또는 외부 키 관리 자동화는
`--recipient-file`·`--identity-file`을 계속 사용할 수 있습니다.

## v0.15.0용 protocol v3 마이그레이션

Protocol v2의 자동화는 import 입력과 doctor 응답을 함께 변경해야 합니다.
출력 종류와 envelope 버전은 바뀌지 않습니다.

| 대상 | Protocol v2 | Protocol v3 |
| --- | --- | --- |
| `version`, `schema` | `data.protocol_version: 2` | `data.protocol_version: 3` |
| `profile import` 미리보기 | 별도 단계 없음 | 파일·identity만 전달하면 `digest`, `target_exists`, 메타데이터 `diff`를 반환하고 변경하지 않음 |
| `profile import` 적용 | `--request-id UUID`로 즉시 적용 | `--apply DIGEST --request-id UUID`를 함께 전달 |
| 기존 profile 교체 | `--replace`로 명시 | 미리보기 후 적용에 `--replace` 추가; 안전 백업과 stale 검사는 유지 |
| doctor check | 선택적 문자열 `remedy` | 항상 배열 `remedies`; 원소는 `argv`, `required_inputs`, `message` |
| 반복 positional | 선언한 개수로 제한 | 마지막 인자의 `repeatable`; schema의 `args.items`와 help의 `<command...>`로 표현 |

`profile list/inspect/diff`와 `backup status`는 저장된 데이터를 조회합니다.
`project up/status/logs/restart/down`은 프로젝트 범위의 managed process 명령입니다. 세부 사용법은 [Profile 관리](profiles.md),
[백업](backup.md), [프로세스 관리](processes.md), [doctor](doctor.md)를 참고하세요.

protocol v3의 Project lifecycle 부분 실패는 stderr 오류의 `details.items`에서 확인합니다.
protocol v10에서는 `details.facts.outcomes`와 `details.causes`를 사용합니다.
이미 성공한 서버는 유지합니다. 같은 입력과 요청 ID로 재시도하면 완료 항목을 반복하지
않고 미완료 작업을 이어가므로, `replayed: true`인 재개 응답이 항상 최초 부분 결과와
동일한 것은 아닙니다. 완료된 작업의 재전송은 저장된 완료 결과를 반환합니다.

## v0.13.0에서의 마이그레이션

이전 JSON 키를 새 키와 중복해서 반환하지 않습니다. CLI를 파싱하는 스크립트는 해당 경로를 함께 변경해야 합니다.

| 대상 | 이전 | protocol v2 |
| --- | --- | --- |
| CLI machine contract | `protocol_version: 1`; 일부 schema 응답에만 노출 | `protocol_version: 2`; 모든 `schema` 응답에 노출. envelope `schema_version`은 1 유지 |
| `version` | `data.version`, `data.commit` | 기존 필드 + `data.protocol_version` |
| `command list` | `data.commands` | `data.items` |
| `env list` | `data.envs` | `data.items` |
| bare/group `schema` | `data.commands` | `data.items`; `schema --all`은 그대로 |
| `command inspect` | `data.command` | `data.item` |
| `project inspect` | `data.project` | `data.item`; `data.paths`는 그대로 |
| `init` | flat 설정 필드, `created` | `data.item` 안의 설정, `data.changed` |
| port·instance 단일 조회·수정 | flat 리소스 필드 | `data.item` 안의 리소스 |
| port allocate/release, instance remove | `created`·`released`·`removed` boolean | `data.changed` |
| `process status` | flat 실행 정보 | `data.item` |
| proxy 상태·변경 | flat 서버 정보 | `data.item`; 수정의 `changed`·`replayed`는 최상위에 항상 제공 |
| dashboard 시작 | URL 한 줄 또는 `--json` | 항상 JSON; URL은 `data.item.url`; `--json` 제거 |
| dashboard status/stop | flat 상태 또는 `stopped` | `data.item` 또는 `data.changed` |
| backup 단일 리소스 반환 | flat 메타데이터 | `data.item`; 쓰기는 `changed` 추가 |
| `cleanup apply` | `data.archives` | `data.items`, `data.changed` |
| cleanup restore/purge | flat 보관 항목 | `data.item`, `data.changed` |
| schema 명령 메타데이터 | `stream_output` | `output_mode`; 비JSON 출력의 `output_schema` 제거 |
| 알 수 없는 도움말 대상 | stderr 텍스트 | stderr JSON 오류 |

등록된 프로젝트의 명령 이름만 추출하려면 다음처럼 사용합니다. `jq`가 설치되어 있어야 합니다.

```sh
devtools command list | jq -r '.data.items[].name'
devtools env list | jq -r '.data.items[]'
devtools dashboard | jq -r '.data.item.url'
```

Dashboard URL에는 접속 토큰이 포함됩니다. 공개 로그에 남기지 말고 접속할 사용자에게만 전달하세요. JSON 포맷 자체가 비밀 값을 마스킹해 주는 것은 아닙니다. 새 실행의 자식 stream과 캡처 로그에는 [실행 출력 보호](output-protection.md)를 적용합니다. 과거 로그나 앱이 직접 만든 파일까지 소급해 보호하지는 않습니다.
