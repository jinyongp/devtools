# Profile 관리와 프로젝트 lifecycle 개선 계획

## 상태

기준 릴리스는 `v0.14.0`이다. 이 문서는 profile 탐색·비교·이동, 진단 결과의 구조화,
여러 managed process를 하나의 프로젝트 작업으로 다루는 lifecycle 명령을 다음 개발
단계의 기준으로 정의한다. 구현 전 검토와 구현 후 회귀 검증은 이 문서의 범위와 완료
기준을 기준으로 한다.

계획 기준선인 `v0.14.0`에는 다음 기반이 이미 있다.

- CLI machine contract `protocol_version = 2`와 JSON/text/artifact/passthrough 출력 구분
- var/sec/env, task/workstream/validation, port/instance, proxy, managed process 관리
- age 기반 backup/restore와 `profile export/import`
- retry-safe mutation을 위한 request ID와 receipt
- Dashboard의 전체 profile 탐색
- `doctor`의 project/command prerequisite 검사

## 목표

다음 사용 흐름을 하나의 일관된 모델로 완성한다.

1. 사용자는 현재 컴퓨터에 존재하는 profile을 CLI와 Dashboard에서 같은 기준으로 찾는다.
2. profile의 env, 값 메타데이터, task 상태, instance, process를 secret 원문 없이 점검한다.
3. 두 profile 또는 import 대상의 차이를 적용 전에 확인한다.
4. 다른 환경에서 가져온 profile은 preview 후 정확히 확인한 상태에만 적용한다.
5. `doctor` 결과는 사람이 읽을 수 있으면서 agent가 별도 문자열 파싱 없이 후속 조치를 결정할 수 있다.
6. 하나의 프로젝트에서 여러 이름 명령을 retry-safe한 한 작업으로 시작·조회·종료한다.
7. 기존 low-level `process` 명령은 유지하고 `project` 명령은 그 위의 orchestration 계층이 된다.

## 설계 제약

### `devtools.toml`은 versionless declarative config로 유지한다

`devtools.toml`에는 `version`, `schema_version` 또는 이와 같은 설정 형식 버전 필드를
추가하지 않는다. 이 파일은 프로젝트 의도를 선언하는 최소 설정으로 유지한다.

새 설정은 가능한 한 optional/additive하게 확장한다. 누락된 필드는 해당 기능의 현재
기본 의미로 해석한다. `DisallowUnknownFields`는 유지해서 오래된 실행 파일이 자신이
이해하지 못하는 새 설정을 조용히 무시하지 않도록 한다. 구조 자체를 바꿔야 하는 경우에는
parser에서 지원하는 입력 형태를 명시적으로 canonical model로 정규화하고, 필요한 migration
경계는 실행 파일의 release/machine contract에서 관리한다. 설정 파일에 버전 메타데이터를
추가해 이 문제를 해결하지 않는다.

### Secret 원문은 관리·비교 응답에 포함하지 않는다

`profile list`, `profile inspect`, `profile diff`, import preview, `doctor`와 Dashboard 응답은
secret의 키·존재·scope 같은 메타데이터만 다룬다. secret 값, task execution context,
credential, retry receipt 원문은 공개 응답에 포함하지 않는다. Secret 값의 동일/상이 여부도
profile diff에서 노출하지 않는다.

### Mutation은 retry-safe해야 한다

Profile 적용과 project lifecycle mutation은 request UUID와 입력 fingerprint를 사용한다.
같은 request ID와 같은 입력은 저장된 결과로 수렴하고, 같은 request ID의 다른 입력은
`request_conflict`다. 여러 하위 동작을 포함하는 project lifecycle은 batch 진행 상태도
receipt에 저장해 응답 유실 뒤 이미 완료된 하위 동작을 반복하지 않는다.

### 자동 rollback을 가장하지 않는다

Managed process 시작은 외부 side effect를 가질 수 있다. 여러 명령 중 일부가 성공한 뒤
다른 명령이 실패해도 이미 성공한 프로세스를 자동 종료하지 않는다. 결과에 성공·실패·미완료
항목을 정확히 기록하고 같은 request ID 재시도로 미완료 작업을 이어간다.

## 1. 공통 profile catalog

계획 기준선의 Dashboard `/api/profiles`는 task 저장소, value 저장소, port instance를 직접 합쳐 profile
목록을 만든다. CLI에 동일 로직을 복제하지 않고 공통 profile catalog를 먼저 만든다.

새 도메인 계층은 다음 저장소를 읽어 profile을 수집한다.

- value profile 저장소
- task/workstream 저장소
- port registry의 instance profile
- managed process record

중복을 제거하고 이름순으로 정렬한다. Profile 목록 조회는 빠르고 부작용 없는 작업이어야 하므로
process readiness RPC 같은 적극적인 상태 검사는 하지 않는다. 상세 조회에서만 필요한 상태를 추가로
계산한다.

Dashboard `/api/profiles`도 같은 catalog를 사용한다. Profile 발견 규칙의 단일 원본은 이 공통
계층이다.

## 2. `profile list`와 `profile inspect`

공개 명령을 다음과 같이 확장한다.

```sh
devtools profile list
devtools profile inspect PROFILE
```

`profile list`는 `data.items`로 최소 summary를 반환한다.

- `profile`
- value 저장 존재 여부
- task 저장 존재 여부
- env 개수
- instance 개수
- managed process 개수와 active 개수

`profile inspect`는 `data.item`에 한 profile의 상세 관리 상태를 반환한다.

- env 이름 목록
- variable metadata: key, common/env 등록 scope
- secret metadata: key, common/env 등록 scope
- task/workstream/validation 개수와 상태 집계
- 연결된 instance: ID, alias, directory
- managed process: execution ID, command, env, state, readiness 요약

Variable 값도 inspect 응답에는 포함하지 않는다. 실제 값 조회는 기존 `var get`의 역할로 남긴다.

Profile이 어떤 저장소에도 존재하지 않으면 `profile_not_found`다. 일부 저장소만 존재하는 profile도
정상 profile로 취급하며 존재하는 영역만 summary에 반영한다.

## 3. `profile diff`

```sh
devtools profile diff LEFT RIGHT
```

두 local profile의 관리 상태 차이를 secret 원문 없이 계산한다. Diff는 안정적인 key/ID 기반으로
정렬한다.

비교 범위는 다음과 같다.

- env 추가/삭제
- variable key와 scope 추가/삭제/변경
- secret key와 scope 추가/삭제/변경; 값 동일 여부는 비교 결과에 포함하지 않음
- task/workstream/validation의 ID, title, lifecycle/completion 상태 차이
- instance의 alias/directory 존재 차이

Managed process는 환경에 종속된 runtime 상태이므로 profile diff의 canonical data에는 넣지 않는다.
필요한 runtime 상태는 `profile inspect` 또는 `project status`에서 확인한다.

## 4. Profile import를 preview/apply 모델로 정리

`v0.14.0`의 `profile import` 즉시 적용 계약을 `backup restore`와 같은 preview/apply 모델로 바꾼다.
이 변경은 CLI machine contract의 breaking change다.

Preview:

```sh
devtools profile import \
  --file ./app.age \
  --identity-file /secure/identity.txt
```

여러 profile이 담긴 일반 backup에서는 `--profile SOURCE`를 지정한다. 단일-profile export 파일은
source를 자동 선택한다. `--as TARGET`이 없으면 source 이름을 target으로 사용한다.

Preview 결과에는 다음을 포함한다.

- source/target profile metadata
- target 존재 여부
- 적용 전 diff
- exact preview digest
- `changed: false`
- `replayed: false`

적용:

```sh
devtools profile import \
  --file ./app.age \
  --identity-file /secure/identity.txt \
  --apply DIGEST \
  --request-id UUID
```

기존 target을 교체하려면 `--replace`를 명시한다. Preview 자체는 기존 target이 있어도 가능하다.
Preview 이후 target 상태가 달라지면 `revision_conflict`로 적용을 중단한다. `--replace` 적용 시 기존
safety-backup 규칙을 그대로 사용한다.

Archive의 인증된 snapshot은 한 import 동작에서 한 번 복호화한 뒤 source 선택, preview와 apply에
재사용한다. Preview와 apply 사이에는 digest가 target 상태 변경을 검출한다.

## 5. Backup recipient UX

Public age recipient는 secret이 아니므로 파일 경로만 요구하지 않는다.

다음을 추가한다.

```sh
devtools backup status
devtools backup create --recipient age1...
devtools profile export --recipient age1...
```

`backup status`는 secret 없이 다음 설정 상태를 반환한다.

- configured 여부
- 기본 backup directory
- public recipient

`--recipient`와 `--recipient-file`은 상호 배타적이다. 둘 다 생략하면 기존 configured recipient를
사용한다. Private identity는 계속 파일 입력만 지원하며 CLI 출력이나 설정 조회에서 원문을 반환하지
않는다.

## 6. Structured doctor remedies

Task 진단에는 이미 다음 형태의 machine-readable remedy가 존재한다.

```json
{
  "argv": ["devtools", "task", "current", "--profile", "app"],
  "required_inputs": [],
  "message": "Inspect current execution state."
}
```

이 shape를 공통 public diagnostic 계약으로 승격한다. Doctor의 기존 자유 텍스트 `remedy` 필드를
`remedies` 배열로 교체한다.

공통 remedy는 다음 필드를 가진다.

- `argv`: 실행 가능한 고정 인자. 직접 실행 명령으로 표현할 수 없으면 빈 배열
- `required_inputs`: 호출자가 추가해야 하는 값의 이름; secret/context 원문은 절대 포함하지 않음
- `message`: 사람이 읽는 설명

예를 들어 등록되지 않은 secret은 값을 argv에 넣지 않고 `stdin`을 required input으로 표시한다.
Tool 설치처럼 devtools가 직접 안전하게 수행할 수 없는 조치는 설명 중심 remedy로 반환한다.

Task diagnostics에서 사용하는 ad-hoc object도 같은 공통 타입을 사용하도록 정리해 shape drift를
막는다. `doctor fix` 같은 자동 실행 명령은 이번 범위에 포함하지 않는다.

## 7. Variadic positional argument 기반

Project lifecycle의 자연스러운 CLI를 위해 마지막 positional argument의 반복을 지원한다.

```text
devtools project up web api worker
```

`Argument`에 마지막 인자 전용 repeatable 의미를 추가하고 다음 규칙을 둔다.

- repeatable argument는 마지막 하나만 허용
- parser는 해당 위치 이후의 positional을 모두 같은 argument로 검증
- JSON Schema는 고정 prefix와 반복 `items`를 정확히 표현하고 `maxItems`를 두지 않음
- help는 `<command>...` 형태로 표시
- completion은 반복 위치에서도 command candidate를 계속 제안

이 기능은 project lifecycle에 먼저 사용하지만 CLI parser의 일반 public contract로 테스트한다.

## 8. `project up / status / down`

`project inspect`를 유지하고 그 위에 managed process orchestration을 추가한다.

```sh
devtools project up COMMAND... --request-id UUID [--env ENV] [--capture-logs] [--timeout 30s]
devtools project status [COMMAND...] [--dir DIR]
devtools project down [COMMAND...] --request-id UUID [--dir DIR]
```

### 대상 선택

`project up`은 실행할 이름 명령을 반드시 명시한다. `devtools.toml`의 모든 command를 자동 실행하지
않는다. 같은 설정에는 build/test 같은 one-shot command도 있을 수 있기 때문이다.

`project status`와 `project down`은 command가 없으면 현재 canonical project instance의 managed
process 전체를 대상으로 한다. Command를 주면 해당 이름의 process만 선택한다.

### `project up`

1. canonical project/profile과 선택한 command를 해석한다.
2. 신규 시작 대상 command의 기본 prerequisite와 port/binding을 batch mutation 전에 모두 preflight한다. 이미 실행 중인 singleton은 cold-start preflight를 생략하고 process reuse/conflict 판정을 따르며, snapshot 이후 해당 singleton이 사라져 실제 start가 필요해지면 process 생성 직전에 같은 preflight를 다시 실행한다.
3. batch receipt를 먼저 만들고 각 command에 사용할 child request/execution ID를 저장한다.
4. 입력 순서대로 existing `services.Store` start 경로를 호출한다.
5. readiness probe가 설정된 command는 지정 timeout까지 `process wait`와 같은 경로로 확인한다.
6. probe가 없는 command는 managed process가 running 상태에 도달하면 준비 완료로 본다.
7. 각 하위 결과를 batch receipt에 즉시 기록한다.

같은 profile + instance + command의 existing singleton 규칙은 그대로 사용한다. 이미 같은 설정으로
실행 중인 command는 성공적인 no-op item으로 반환한다. Env/capture 설정이 다르면 기존
`process_conflict` 규칙을 유지한다.

### Partial failure와 retry

Batch 결과는 command별 상태를 반환한다.

- `ready`
- `running`
- `failed`
- `pending`
- `unchanged`

일부 command가 실패하면 전체 요청은 실패 exit code를 반환하되 details에 각 command의 item과
condition을 보존한다. 이미 성공한 process는 자동 종료하지 않는다.

같은 request ID로 재시도하면 저장된 batch fingerprint를 확인하고 완료된 command는 다시 시작하지
않는다. 실패 또는 미완료 command만 현재 상태를 다시 판정해 진행한다. 같은 request ID에 command,
env, capture, timeout 등의 입력이 바뀌면 `request_conflict`다.

### `project status`

현재 project instance와 연결된 managed execution을 command별로 반환한다. Process lifetime state와
readiness를 함께 보여주되 원문 logs는 포함하지 않는다.

### `project down`

선택한 active execution을 existing process stop 경로로 종료한다. Batch receipt와 child request ID를
사용해 응답 유실 후 재시도해도 이미 종료한 process를 다시 조작하지 않는다. 종료된 process는
`unchanged`로 수렴한다.

## 9. Machine contract

Doctor `remedy -> remedies` 변경과 `profile import` preview/apply 전환은 기존 machine contract를
깨므로 이 개선 묶음에서 CLI `protocol_version`을 `3`으로 올린다.

공통 JSON envelope 구조는 바뀌지 않으므로 `EnvelopeVersion`과 top-level `schema_version`은 계속
`1`이다. `devtools.toml`에는 protocol/config version 필드를 추가하지 않는다.

모든 새 데이터 명령은 현재 출력 규칙을 따른다.

- collection: `data.items`
- single resource: `data.item`
- mutation: `changed`
- retry-safe mutation: `replayed`

## 작업 순서

| ID | 작업 | 선행 | 완료 기준 |
| --- | --- | --- | --- |
| PP-01 | 공통 profile catalog 추출 | 없음 | CLI/Dashboard가 같은 profile 발견 규칙을 사용하고 기존 Dashboard 동작이 유지됨 |
| PP-02 | `profile list / inspect` | PP-01 | 저장 영역별 profile과 안전한 상세 metadata를 조회 가능 |
| PP-03 | profile canonical summary/diff 엔진 | PP-01, PP-02 | local profile diff가 정렬된 stable shape로 secret 원문 없이 동작 |
| PP-04 | import preview/apply 전환 | PP-03 | preview digest, stale target 방지, replace safety backup, replay/conflict 검증 |
| PP-05 | backup recipient UX | 없음 | `backup status`, direct public recipient 입력, 상호배타 옵션 검증 |
| PP-06 | 공통 Remedy 타입과 doctor 구조화 | 없음 | doctor/task diagnostics가 같은 remedy shape 사용 |
| PP-07 | variadic positional parser | 없음 | parse/schema/help/completion에서 마지막 반복 인자 일치 |
| PP-08 | project batch lifecycle core | PP-07 | batch receipt와 partial/retry-safe start/stop orchestration 구현 |
| PP-09 | `project up/status/down` CLI | PP-08 | public schema와 command UX가 lifecycle core에 연결됨 |
| PP-10 | protocol v3, 문서와 Agent Skill 정리 | PP-04, PP-06, PP-09 | public contract와 migration 설명이 실제 schema와 일치 |
| PP-11 | 설치 바이너리 통합 검증 | PP-01~PP-10 | Docker와 release 수준 검증이 모든 새 흐름을 통과 |

구현은 가능한 한 위 단위로 검증하고 독립 commit한다. PP-01~PP-03처럼 강하게 연결된 내부 기반은
한 commit으로 묶어도 되지만, profile transfer, doctor contract, project lifecycle은 서로 독립된
동작 단위로 분리한다.

## 검증 계획

### Profile catalog / inspect / diff

- value만 존재하는 profile
- task만 존재하는 profile
- instance만 존재하는 profile
- 여러 저장소에 중복 존재하는 profile의 dedupe와 정렬
- invalid/corrupt 저장소 오류 처리
- env와 var/sec metadata
- secret 값과 equality 정보 비노출
- task/workstream/validation 상태 집계
- Dashboard profile 목록과 CLI 목록 일치

### Profile import

- 단일-profile archive source 자동 선택
- multi-profile archive의 명시적 source 선택
- target 미존재 preview/apply
- target 존재 preview와 `--replace` 요구
- diff 결과
- preview 이후 target 변경에 대한 `revision_conflict`
- 동일 request ID replay
- 다른 입력의 같은 request ID `request_conflict`
- 동시 동일 import의 apply/replay 수렴
- 잘못된 identity와 tampered archive 거부
- safety backup 검증

### Structured remedies

- command로 표현 가능한 조치의 정확한 argv
- required input 표시
- secret/context 값 비노출
- command로 표현할 수 없는 조치의 empty argv
- task diagnostics와 doctor schema shape 일치

### Variadic parser

- 0/1/N positional
- repeatable은 마지막 argument만 허용
- pattern validation
- schema minItems/items
- help 표기
- shell completion 반복

### Project lifecycle

- 여러 command 정상 start
- readiness 설정 command의 ready 확인
- readiness 없는 command의 running 완료
- 신규 시작 대상 prerequisite 실패 시 batch mutation 전 preflight, reuse candidate가 실제 start로 바뀌는 race의 fallback preflight
- start 중 일부 실패와 성공 process 보존
- 응답 유실을 가정한 같은 request ID 재시도
- concurrent same request
- same request ID changed input conflict
- existing singleton no-op
- env/capture conflict
- status 전체/선택 조회
- down 전체/선택 종료
- logs/secret 원문 비노출

### 최종 gate

영향 범위의 targeted test 후 마지막 통합 gate는 다음을 사용한다.

```sh
gofmt -w cmd internal scripts skills
go vet ./...
go test -race ./...
just check
just verify-docker
```

Docker 시나리오는 기존 `backup`, `doctor`, `discovery`, `processes`, `workflow` 회귀를 유지하고,
profile 관리와 project lifecycle을 별도 시나리오로 추가한다. 설치된 바이너리의 help/schema/completion도
함께 확인한다.

## 문서 정리

구현 완료 시 사용자 문서는 다음 구조로 정리한다.

- `docs/profiles.md`: list/inspect/diff/export/import의 canonical 사용법
- `docs/backup.md`: backup/restore, key/recipient 관리 중심
- `docs/doctor.md`: structured remedies와 진단 해석
- `docs/processes.md`: low-level process lifecycle과 project orchestration의 역할 구분
- `docs/cli-contract.md`, `docs/cli-output.md`: protocol v3 contract와 migration
- `skills/devtools/SKILL.md`: agent가 새 profile/project 흐름을 사용하는 기준

현재 계획 문서는 구현 계획의 기준이며, 완료된 public behavior만 위 사용자 문서에 반영한다.

## 비범위

- `devtools.toml` version/schema metadata
- `doctor fix` 자동 수정 실행기
- cloud profile sync
- 외부 secret manager provider abstraction
- 원격 daemon 또는 다른 컴퓨터의 process 직접 제어
- project up 실패 시 성공한 process의 자동 rollback
- project command group을 `devtools.toml`에 새로 선언하는 기능

Command group은 실제 `project up COMMAND...` 사용 패턴이 쌓인 뒤 별도 필요성을 검증한다.

## 완료 기준

다음 조건을 모두 만족하면 이 계획을 완료로 본다.

- CLI와 Dashboard의 profile 발견 규칙이 하나의 구현을 사용한다.
- Profile을 list/inspect/diff하고 import 전에 변경 내용을 확인할 수 있다.
- Profile import가 preview digest와 retry-safe apply를 사용한다.
- Public recipient 전달에 임시 recipient 파일이 필수가 아니다.
- Doctor와 task diagnostics가 같은 structured remedy contract를 사용한다.
- 하나의 project lifecycle 요청으로 여러 managed process를 안전하게 시작·조회·종료할 수 있다.
- Partial failure와 응답 유실 뒤 재시도가 중복 process를 만들지 않는다.
- protocol v3 schema, 문서, Agent Skill과 실제 출력이 일치한다.
- 전체 race/vet/check/Docker 통합 검증이 통과한다.
