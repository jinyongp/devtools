# CLI 출력 계약

이 문서는 현재 소스의 CLI protocol v3 출력 규칙을 설명합니다. v0.15.0용 protocol v3 전환과 v0.13.0의 JSON 경로 변경은 아래 마이그레이션 절에 정리했습니다. 저장 파일 형식과 dashboard HTTP API는 CLI protocol 버전과 별개이며, `devtools.toml`에는 버전 필드를 두지 않습니다.

## 출력 종류 확인

명령의 결과를 어떻게 읽을지는 `devtools schema COMMAND`의 `data.output_mode`로 확인합니다. 터미널인지 파이프인지에 따라 형식을 자동으로 바꾸지 않습니다. 명령별 `--json` 옵션도 사용하지 않습니다.

| `output_mode` | 해당 명령 | stdout |
| --- | --- | --- |
| `json` | 조회·변경·진단·schema 명령 | JSON 응답 한 개 |
| `text` | `help`, 인자 없는 호출, `--help`·`-h` | 사람이 읽는 도움말 |
| `artifact` | `completion bash`, `completion zsh`, `completion fish` | 생성한 셸 스크립트 원문 |
| `passthrough` | `command run`, 별칭 `run` | 자식 프로그램의 출력 원문 |

`--help`는 실행 대신 도움말을 요청하므로 원래 명령의 출력 종류와 관계없이 텍스트를 반환합니다. 내부 자동완성 요청인 `__complete`는 공개 명령 카탈로그에 포함하지 않는 줄 단위 프로토콜입니다.

생성물은 그대로 파일에 저장할 수 있습니다. 실행 명령은 자식의 stderr와 종료 코드도 보존합니다.

```sh
# 파일에 저장할 스크립트 원문
mkdir -p ~/.config/fish/completions
devtools completion fish > ~/.config/fish/completions/devtools.fish

# 등록된 명령의 출력은 JSON으로 감싸지 않음
devtools command run test
```

`output_schema`는 JSON 성공 응답의 `data`를 설명합니다. 텍스트·생성물·자식 프로세스 출력에는 적용하지 않으므로 이 세 종류의 명령에는 `output_schema`를 제공하지 않습니다.

## 성공과 실패

JSON 명령은 성공 시 stdout에 응답 한 개를 출력합니다. 성공 응답에는 항상 `data`가 있고 `error`는 없습니다. 결과가 없는 경우에도 `data` 필드 자체를 생략하지 않습니다.

```json
{"schema_version":1,"ok":true,"data":{"items":[]}}
```

Devtools가 처리하는 실패는 stderr의 JSON 응답 한 개로 반환하며, `data` 대신 `error`를 제공합니다. `error.code`는 분기에 사용하고 `message`는 설명으로 사용합니다. 부가 정보가 있는 오류에만 `details`가 있습니다.

```json
{"schema_version":1,"ok":false,"error":{"code":"command_not_found","message":"The selected project command is not defined."}}
```

이 오류 규칙은 도움말 대상 오류와 completion 생성 오류에도 적용됩니다. `command run`은 실행 준비 오류와 Devtools 자체의 스트림 처리 오류에 JSON stderr를 사용합니다. 자식 프로그램의 출력과 종료 상태는 그대로 전달하므로, stderr 전체가 Devtools JSON이라고 가정하면 안 됩니다. 출력 전달 자체가 실패하면 자식의 일부 출력 뒤에 Devtools I/O 오류가 기록될 수 있습니다.

`ok: true`는 요청을 처리했다는 뜻이지 진단 대상이 정상이라는 뜻은 아닙니다. `doctor`의 `data.ready`, `process check`의 `data.readiness.ready`, 작업 검사의 `valid` 등은 별도로 확인해야 합니다. 기존 종료 코드와 진단 판정 규칙은 유지합니다.

`schema_version`은 공통 성공/실패 envelope의 형식 버전이며 1입니다. 명령별 `data` 구조와 입력·출력 규칙을 포함한 CLI machine contract는 현재 `protocol_version: 3`입니다. `devtools version`과 모든 `devtools schema` 응답의 `data.protocol_version`에서 확인할 수 있습니다. 이 값은 `devtools.toml` 설정 버전이 아닙니다. 명령별 계약은 설치한 실행 파일의 `schema`에서 확인하세요.

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
| `command`, `env`, `variable`, `secret` 목록 | `items`; 해당하는 경우 `profile` |
| `command inspect`, `project inspect` | `item`; `profile` 또는 `paths` 메타데이터 |
| `init` | `item`, `changed` |
| `port`, `instance` 목록·단일 조회 | `items` 또는 `item` |
| 포트 할당, instance 이름·위치 변경 | `item`, `changed` |
| 포트 해제, instance 제거 | `changed` |
| `port prune` | `removed`, `retained`, `changed` 보고서 |
| `process` 목록·상태·변경 | `items`, `item`, 또는 `item`·`changed`·`replayed` |
| `process check/wait/logs` | readiness 또는 로그 보고서 |
| `proxy` 목록·상태·변경 | `items`, `item`, 또는 `item`·`changed`·`replayed` |
| `dashboard`·`dashboard start`, status, stop | 각각 `item`·`changed`, `item`, `changed` |
| `backup keygen/configure/create`, inspect | 각각 `item`·`changed`, `item` |
| `backup restore` | 복구 계획·적용 보고서와 `changed`·`replayed`; 미리보기는 둘 다 false |
| `profile list`, `profile inspect`, `profile diff` | 각각 `items`, `item`, `left`·`right`·`different`와 메타데이터 차이 보고서 |
| `profile export` | `item`, `changed` |
| `profile import` | `item`, `digest`, `target_exists`, `diff`, `changed`, `replayed`, `safety_backup`; 미리보기는 변경하지 않음 |
| `backup status` | 공개 설정 상태를 담은 `item` |
| `project status` | `profile`, `directory`, 활성 실행 `items` |
| `project up/down` | `action`, `profile`, `directory`, 명령별 `items`, `changed`, `replayed` |
| `cleanup archives`, apply, restore/purge | 각각 `items`, `items`·`changed`·`replayed`, `item`·`changed` |
| `cleanup preview` | 계획 ID·만료 시각·`items` |
| `task`·workstream·validation | 기존 `item`·`items`와 리비전·변경·재시도 메타데이터; context·tree·export·검사는 보고서 |
| `version`, `update`, `import`, `doctor`, `schema` | 각 기능의 보고서; `update`와 `import`는 변경 여부 포함 |

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

`profile list/inspect/diff`, `backup status`, 직접 공개 recipient 입력과
`project up/status/down`은 새 명령·옵션입니다. 세부 사용법은 [Profile 관리](profiles.md),
[백업](backup.md), [프로세스 관리](processes.md), [doctor](doctor.md)를 참고하세요.

Project lifecycle의 부분 실패는 stderr 오류의 `details.items`에서 확인합니다.
이미 성공한 서버는 유지합니다. 같은 입력과 요청 ID로 재시도하면 완료 항목을 반복하지
않고 미완료 작업을 이어가므로, `replayed: true`인 재개 응답이 항상 최초 부분 결과와
동일한 것은 아닙니다. 완료된 작업의 재전송은 저장된 완료 결과를 반환합니다.

## v0.13.0에서의 마이그레이션

이전 JSON 키를 새 키와 중복해서 반환하지 않습니다. CLI를 파싱하는 스크립트는 해당 경로를 함께 변경해야 합니다.

| 대상 | 이전 | 현재 소스 |
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

Dashboard URL에는 접속 토큰이 포함됩니다. 공개 로그에 남기지 말고 접속할 사용자에게만 전달하세요. JSON 포맷 자체가 비밀 값을 마스킹해 주는 것은 아닙니다. 자식 출력과 명시적으로 요청한 로그 역시 기존 보호 범위를 따릅니다.
