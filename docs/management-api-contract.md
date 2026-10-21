# 관리 API 계약

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

dashboard의 task/workstream, var/sec/env, 프로젝트·프로세스, 백업·복구와 저장소 정리 API 계약이다.

## 접속과 요청

`devtools dashboard`가 발급한 링크를 교환한 세션은 현재 사용자의 모든 profile을 관리한다. profile은 요청 대상을 선택한다. loopback 바인딩, 정확한 Host/Origin 검사, bearer 인증과 세션 만료를 모든 관리 API에 적용한다.

변경은 도구별 허용 action과 구조화된 입력으로 전달한다. 대상 profile과 입력을 명시하고, 변경 종류에 따라 요청 UUID·편집 리비전·실행 ID·미리 보기 ID로 정확한 대상을 고정한다. HTTP 계층은 도구의 공통 검증·변경 로직을 호출한다. task 관리 변경은 요청 ID와 최신 profile 리비전으로 검사한다.

동일한 요청 ID와 동일한 입력은 기존 결과를 반환한다. 다른 입력으로 같은 ID를 사용하면 `request_conflict`로 처리한다. 현재 리비전이 편집 기준과 다르면 변경 내용을 다시 조회하도록 안내한다. 응답은 기존 JSON 성공·오류 envelope를 사용한다.

## 영향 확인과 민감한 입력

삭제와 복구는 영향 미리 보기 결과를 기준으로 적용한다. 적용 시 대상과 현재 상태를 다시 확인한다. 미리 보기의 해시는 변경 허가를 대신하지 않으며, 인증된 요청과 대상 검증을 함께 사용한다.

secret은 입력·교체를 지원하고 조회 응답과 이력에는 메타데이터만 기록한다. HTTP 요청 본문과 오류 원문을 로그에 남기지 않는다. 프로세스 실행은 등록된 이름 명령을 대상으로 삼는다.

## 관리 범위

CLI와 dashboard는 task/workstream, var/sec/env, 프로세스, 백업·복구, 정리를 같은 접근 인증과 작업별 검증으로 연결한다. 백업 키 생성과 기본 저장 위치 설정은 CLI에서 준비한다.

## HTTP 인터페이스

### 조회 endpoint

`GET /api/profiles`는 `{profiles: string[]}`로 알려진 profile 이름을 정렬해 반환한다.
값·작업 저장소뿐 아니라 instance나 프로세스 기록만 남은 profile도 포함한다.
`GET /api/query`는 task 조회 계약을 사용한다.

`GET /api/values?profile=app&env=local`은 `{profile, env, envs, revision, items, bundles, integrity_verified, public_prefixes}`를 반환한다. `integrity_verified:false`는 저장 인증이 완료됐다는 뜻이 아니다. `env`를 생략하면 공통 값을 조회한다. 각 item은 `key`, `kind`, `source`, `overrides`와 존재하는 경우 `value_id`를 포함하며 variable에만 `value`를 포함한다. `bundles`는 `bundle_id`, `name`, `value_ids`로 공유 membership을 반환한다.

### Task 변경

`POST /api/actions`는 `Content-Type: application/json`, 같은 서버의 정확한 `Origin`, 브라우저 세션의 `Authorization: Bearer …`를 요구한다. 본문은 최대 1 MiB의 JSON 객체다. task 요청 예시는 다음과 같다.

```json
{
  "domain": "task",
  "profile": "app",
  "action": "task.add",
  "body": {"title": "Implement endpoint"},
  "options": {
    "request-id": "00000000-0000-4000-8000-000000000001",
    "if-revision": "0"
  }
}
```

대상 작업이 있는 action은 `target`에 UUID를 전달한다. action과 body는 task schema를 따르며, 옵션은 `request-id`, `if-revision`, `expected-run`을 받는다. 모든 변경에 요청 UUID와 조회한 profile 리비전이 필요하다. 실행 context는 해당 action이 권한이나 선택적 guard로 소비할 때만 검증하고 영수증 identity에 결합한다. context-free action의 ambient context는 무시한다. `workstream.edited`는 추가로 문자열 `dry-run: "true"`를 받아 같은 평가기로 미리보기만 수행한다. 이때 요청 UUID는 선택이며 영수증·업무 상태를 저장하지 않는다.

task 허용 action은 `task.add`, `task.update`, `task.depends`, `task.cancel`, `task.reopen`, `workstream.create`, `workstream.update`, `workstream.depends`, `workstream.cancel`, `workstream.reopen`, `workstream.activate`, `workstream.close`, `workstream.edited`, `spec.set`, `plan.set`, `run.revoked`다. `workstream.update`는 metadata 부분 수정을 받으며 지원 필드는 `title`, `description`이다. 에이전트의 점유·sync·인계·체크포인트·검증 기록·완료는 CLI 실행 컨텍스트로 수행한다.

`GET /api/query`는 `workstream plan show`, `workstream check`, `validation list`와 `scope`, `completion`, `at-revision` 옵션을 지원한다. 제외 항목은 상세 조회나 `scope=removed`로 조회한다. 서버 재사용은 인증 프로토콜·UI asset version·task protocol version이 모두 일치할 때만 허용한다.

`run.revoked`는 task UUID를 target으로, `{reason:"사유"}`를 body로 받는다. `expected-run`과 현재 점유가 일치할 때 기존 run을 revoked로 종료한다. task는 open을 유지하며 프로세스 종료는 별도로 관리한다. 새 실행 컨텍스트는 CLI claim에서 발급한다.

### 값과 bundle 변경

값 변경은 조회 응답의 불투명한 `revision`을 그대로 전달한다.

```json
{
  "domain": "values",
  "profile": "app",
  "change": {
    "action": "variable.set",
    "key": "PORT",
    "env": "local",
    "value": "3000",
    "revision": "<revision from api/values>",
    "request_id": "00000000-0000-4000-8000-000000000002"
  }
}
```

값 action은 `variable.set`, `secret.set`, `secret.generate`, `variable.unset`, `secret.unset`, `env.create`, `env.remove`다. set은 빈 문자열을 포함한 `value`를 받는다. unset은 `key`와 `env`, env action은 `env`를 받는다. `env`의 빈 문자열은 공통 영역이다. env 삭제는 override를 먼저 정리한 상태에서 가능하다.

`secret.generate`는 `key`, `env`, `generate:{replace:false}`와 필수 revision/request_id를 받으며
value/import/bundle/create_only는 함께 전달할 수 없다. crypto/rand 32바이트를 padding 없는
base64url 43자로 저장하고 메타데이터만 반환한다. replace는 기존 secret 교체를 허용하되
살아 있는 TTL을 보존하고 만료된 slot을 거절한다. CLI의 `secret generate`와 같은 원자 저장·재시도 계약이다.

set은 선택적으로 `create_only:true`를 받는다. 선택 계층에 빈 값이나 만료된 값이
있어도 `value_exists`로 거절하며 상속된 공통 값만 있는 env override 생성은 허용한다.
CLI의 guard 지정 변경과 같은 Apply·revision·receipt 계약이다. 새 성공 receipt는 30일
재사용할 수 있고 만료 후 `request_expired`다. 메타데이터는 root 수명 동안 보존하며
성공한 no-op도 ID를 소비한다. `values request show UUID`는 저장 결과를 수동 해제나
idle 연장 없이 조회하지만, 보호된 root에서는 이미 해제된 warm session이 필요하다.
잠긴 세션을 우회하지 않는다. 상세 경계는 [조건부 변경](cli-contract.md#조건부-변경과-응답-유실-복구)을 따른다.

bundle 변경도 같은 `domain:"values"`와 `change.revision`·`request_id`를 사용한다.
action은 `bundle.set`, `bundle.rename`, `bundle.remove`다. `change.bundle`은
`{name, members?, new_name?, preview?}`이며 members는 키 이름 배열이다.
set은 전체 membership을 교체하고 rename은 `new_name`을 받으며 ID를 유지한다.
`preview:true`는 저장하지 않는다. bundle 삭제는 값 자체를 삭제하지 않는다.
CLI의 `bundle list`와 HTTP `/api/values`에서 얻은 revision은 같은 값 저장소의 편집 기준이다.

일괄 등록은 `change.action: "import"`와 `change.import: {content:"KEY=value", variables:[], overwrite:false, preview:true}`를 사용한다. `env`, `revision`, `request_id`는 다른 값 변경과 같은 위치에 전달한다. `preview:true`는 원문 값을 포함하지 않는 `items`와 `applicable`을 반환하고, `preview:false`는 같은 리비전을 검사한 뒤 전체 변경을 원자적으로 저장한다. 새 키는 secret이 기본이며 `variables`로 공개 키를 선택한다. 기존 키는 profile에 등록된 종류를 유지한다.

`overwrite:false`는 선택 env의 유효 값과 상속한 공통 값을 유지하고 `skip`으로 표시한다. `overwrite:true`는 선택한 계층에 값을 저장하며 공통 값을 상속하던 키에는 env override를 만든다. 미리 보기와 적용은 기존 CLI의 `.env` 파서를 사용한다. 화면은 최대 500 KB의 파일 또는 붙여넣기를 받고, 변경 API는 전체 JSON 요청을 1 MiB로 제한한다.

변경 성공은 `{ok: true, data: ...}`, 실패는 기존 오류 envelope를 반환한다. task 변경의 `no_change`와 리비전·요청 ID·점유 충돌의 HTTP 상태는 409다. task 변경 성공은 CLI와 같은 before/after 리비전과 affected 항목을 반환한다. 값 변경 결과에는 `profile`, `changed`, `revision`, `replayed`가 포함된다. 값 재시도 기록에는 입력 원문 대신 서명된 식별값과 결과 메타데이터를 보관한다.

외부 실행기의 준비·승인·전달·보고는 이 HTTP API로 제공하지 않는다.
[승인된 로컬 실행기 계약](secret-automation.md)의 `values execution`·`values runner`
CLI와 비공개 채널을 사용한다. 일반 HTTP 응답으로 값이나 로그 보호 패턴을 조회하는 endpoint는 없다.

## 프로세스·백업·정리 API

프로세스 목록은 `GET /api/processes?profile=app`으로 조회한다. 변경은 `POST /api/actions`에 `{domain:"process", profile:"app", process:{action:"start", directory:"/projects/app", command:"web", request_id:"UUID"}}`를 전달한다. stop·restart는 directory·command 대신 `id`에 실행 UUID를 전달한다. start·restart는 선택적으로 `env`, `bundle`, `capture_logs`를 받는다.
실행 UUID가 변경 대상을 고정하고 요청 UUID가 재시도를 구분한다. 값·작업 편집 리비전은 프로세스 실행에 적용하지 않는다.

`GET /api/processes`는 `items`, `instances`와 실행 ID별 `values` 관찰 맵을 반환한다. 실행의 `input_version`, `value_watch`, `value_apply`와 현재 비교 결과는 [값 사용처](cli-contract.md#값-메타데이터와-사용처-values)·[자동 반영](processes.md#값-변경-알림과-자동-반영)을 따른다.

`GET /api/process-logs?profile=app&id=UUID`는 명시적으로 요청한 저장 로그를 `{content}`로 반환한다. 새 실행은 [저장 전 마스킹](output-protection.md)을 적용하며 과거 로그는 소급 처리하지 않는다.

준비 확인은 `POST /api/actions`에 `{domain:"process", profile:"app", process:{action:"check", id:"UUID"}}`를 전달한다. 대상 profile을 검사한 뒤 실행 시점의 설정·환경으로 한 번 확인하고, 실행 메타데이터와 `readiness`를 반환한다. 매 요청은 새 관측이며 변경 영수증을 사용하지 않는다. [준비 확인 계약](process-readiness.md)에 결과와 시간 제한을 정의한다.

`GET /api/project?profile=app&instance=ID`는 선택한 프로젝트의 이름 명령과 port 할당 메타데이터를 조회한다. 명령의 실행 인자와 환경변수 원문은 조회 응답에 포함하지 않는다.

### 정리와 백업

정리 미리 보기는 `GET /api/cleanup-preview?profile=app`, 보관함 목록은 `GET /api/archives?profile=app`이다. profile을 생략하면 전체 범위다. 적용 요청은 `{domain:"cleanup", profile:"app", cleanup:{action:"apply", plan:"PLAN_UUID", ids:["ITEM_UUID"], request_id:"UUID"}}`다. restore·purge는 `{action:"restore", id:"ARCHIVE_UUID"}`처럼 보관함 ID를 전달한다. 전체 범위 요청은 profile에 빈 문자열을 사용한다. 선택된 미리 보기 항목과 archive ID가 변경 대상을 고정한다.

암호화 백업 생성은 `{domain:"backup", profile:"app", backup:{action:"create", request_id:"UUID"}}`로 요청하며 `{path, replayed}`를 반환한다. 백업 파일명은 요청 UUID로 고정한다. 복구 미리 보기는 backup에 `{action:"restore", file:"/backups/app.age", identity_file:"/secure/identity", source_profile:"app", replace:false}`를 전달한다. envelope의 profile이 복원 대상이며 source_profile은 백업 안의 원본이다. 적용은 같은 입력에 미리 보기의 `digest`와 새 `request_id`를 추가한다. 원문 secret과 개인키는 응답에 포함하지 않는다.

<a id="화면에서-관리하기"></a>

## Dashboard 조작과 동시 편집

`Profile` 선택기로 대상을 선택하고 `Variables & secrets`에서 공통 값과 env override를 편집한다. secret 필드는 새 값을 입력하는 방식이며, 저장 후 입력을 비운다. workstream 소속 작업은 실행 중·완료 후에도 정의와 의존성을 편집한다. 계획 본문 편집, 범위 제외·복원은 영향 미리보기 후 같은 리비전으로 저장한다. 제외 항목은 workstream의 복원 메뉴에서 선택한다. 명세는 요구사항과 완료 조건을 행 단위로 편집한다. `stale`, `stale-running`, `removed-running`은 lifecycle과 별도로 표시한다.

화면은 변경 대상을 표시하고 제거·취소·점유 해제 전에 확인 단계를 제공한다. workstream 취소는 함께 취소할 open task의 제목 목록을 보여주며, 미리 보기의 리비전을 적용 요청에 사용한다. 응답 수신이 불확실하면 같은 요청을 재전송하며, 충돌 시 최신 내용을 조회한 후 다시 편집한다. 인증 세션은 origin별 sessionStorage에 유지한다. 실행 컨텍스트는 실제 작업하는 에이전트가 CLI에서 관리한다.


## Vault 상태와 명시 해제

`GET /api/vault`는 query 입력 없이 passive 상태를 반환한다. daemon 생성·native
해제·idle touch를 하지 않는다. 기존 인증과 GET 제한은 그대로 적용한다.
`POST /api/actions`에 `{domain:"vault", vault:{action:"lock"}}` 또는
`{domain:"vault", vault:{action:"unlock", passphrase:"<ephemeral input>"}}`를 보낸다.
이 전역 action은 profile을 요구하지 않으며 실제 passphrase를 URL·로그·영수증에
넣지 않는다. native unlock은 passphrase 대신 `interactive:true`로 OS 인증을 허용한다.
lock에는 unlock 입력을 보내지 않는다. 초기 setup/backend 변경은 제공하지 않는다.

반환은 `{item:{state, ...}}`이며 재시도 영수증을 만들지 않는다. 결과 불명 시 상태를
조회하고 새 명시 입력을 받는다. 값 변경의 동일 UUID 재전송과 다르다. 새 값 변경
영수증의 replay 창은 30일이며 만료 요청은 `request_expired`다. frontend 공개 prefix
경고는 값 없이 `warnings`에 담고 키 종류를 자동 변경하지 않는다.

## 값 변경 관찰과 자동 교체

관리 프로세스의 `input_version`은 실행 전에 준비한 입력의 안전한 메타데이터다.
`value_watch`는 supervisor가 마지막으로 관찰한 값 변경 상태이며
`state`, `reason`, `updated_at`, 선택적인 `goal`, `integrity_verified`를 제공한다.
잠금 등으로 비교할 수 없으면 goal을 비운다. 조회는 키 해제나 세션 수명 연장을 하지 않는다.
종료 이력의 관측 시각은 마지막 관측값이며 현재 상태라는 뜻이 아니다.

명령의 `values_update` 기본값은 `notify`다. 명시적으로 `restart`를 선택한 관리 실행은
`value_apply`에 자동 교체 상태를 기록한다.

| 필드 | 의미 |
| --- | --- |
| `state` | `requested`, `applied`, `failed`, `paused`. |
| `reason`, `updated_at` | 원문 없는 원인과 관측 시각. |
| `execution_id`, `request_id`, `next_id` | 원래 실행, 고정한 변경 요청, 교체 실행의 식별자. 해당할 때 제공한다. |
| `goal` | 입력의 불투명한 식별 정보. 비밀값이나 실행 권한을 포함하지 않는다. |

`auto_origin`과 크기가 제한된 `auto_restarts`는 자동 교체의 원래 실행과 반복 시각을 추적한다.
실패하거나 담당 실행을 잃은 요청은 자동 재전송하지 않는다.
`requested` 이후 90초 동안 완료를 확인하지 못하면 결과 불명으로 일시 정지하며 원래 요청 ID를 유지한다.
교체된 실행은 자신의 완료 결과를 보존하므로 이전 실행 이력 정리가 후속 갱신을 막지 않는다.
원인을 해결한 뒤 새 요청 UUID로 수동 restart한다.
