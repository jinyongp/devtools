# 관리 API 계약

상태: dashboard의 task/workstream 및 var/sec/env 조회·변경 API 계약.

## 접속과 요청

`devtools dashboard`가 발급한 링크를 교환한 세션은 현재 사용자의 모든 profile을 관리한다. profile은 요청 대상을 선택한다. loopback 바인딩, 정확한 Host/Origin 검사, bearer 인증과 세션 만료를 모든 관리 API에 적용한다.

변경은 도구별 허용 action과 구조화된 입력으로 전달한다. 요청에는 대상 profile, action, 입력, 요청 ID와 편집 기준 리비전을 포함한다. HTTP 계층은 도구의 공통 검증·변경 로직을 호출한다. task 변경은 기존 request-id와 실행 컨텍스트 계약을 따른다.

동일한 요청 ID와 동일한 입력은 기존 결과를 반환한다. 다른 입력으로 같은 ID를 사용하면 `request_conflict`로 처리한다. 현재 리비전이 편집 기준과 다르면 변경 내용을 다시 조회하도록 안내한다. 응답은 기존 JSON 성공·오류 envelope를 사용한다.

## 영향 확인과 민감한 입력

삭제와 복구는 영향 미리 보기 결과를 기준으로 적용한다. 적용 시 대상과 현재 상태를 다시 확인한다. 미리 보기의 해시는 변경 허가를 대신하지 않으며, 인증된 요청과 대상 검증을 함께 사용한다.

secret은 입력·교체를 지원하고 조회 응답과 이력에는 메타데이터만 기록한다. HTTP 요청 본문과 오류 원문을 로그에 남기지 않는다. 프로세스 실행은 등록된 이름 명령을 대상으로 삼는다.

## 기능 연결 순서

백업·복구는 CLI에서 제공한다. dashboard는 task/workstream 편집과 var/sec/env 변경을 제공한다. 프로세스와 정리 기능은 같은 접근 인증과 작업별 검증을 사용하는 후속 확장이다.

## HTTP 인터페이스

`GET /api/profiles`는 값이나 작업이 저장된 profile 목록을 반환한다. `GET /api/query`는 기존 task 조회 계약을 사용한다. `GET /api/values?profile=app&env=local`은 `{profile, env, envs, revision, items}`를 반환한다. `env`를 생략하면 공통 값을 조회한다. 각 item은 `key`, `kind`, `source`, `overrides`를 포함하며 variable에만 `value`를 포함한다.

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

대상 작업이 있는 action은 `target`에 UUID를 전달한다. action과 body는 task schema를 따르며, 옵션은 `request-id`, `if-revision`, `context`, `expected-run`, `workstream`, `dir`을 받는다. 모든 변경에 요청 UUID와 조회한 profile 리비전이 필요하다. 실행 변경에는 CLI와 같은 실행 컨텍스트가 필요하다.

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

값 action은 `variable.set`, `secret.set`, `variable.unset`, `secret.unset`, `env.create`, `env.remove`다. set은 빈 문자열을 포함한 `value`를 받는다. unset은 `key`와 `env`, env action은 `env`를 받는다. `env`의 빈 문자열은 공통 영역이다. env 삭제는 override를 먼저 정리한 상태에서 가능하다.

변경 성공은 `{ok: true, data: ...}`, 실패는 기존 오류 envelope를 반환한다. 리비전·요청 ID·점유 충돌의 HTTP 상태는 409다. 값 변경 결과에는 `profile`, `changed`, `revision`, `replayed`가 포함된다. 값 재시도 기록에는 입력 원문 대신 서명된 식별값과 결과 메타데이터를 보관한다.

## 화면에서 관리하기

프로세스 목록은 `GET /api/processes?profile=app`으로 조회한다. 변경은 `POST /api/actions`에 `{domain:"process", profile:"app", process:{action:"start", directory:"/projects/app", command:"web", request_id:"UUID"}}`를 전달한다. stop·restart는 directory·command 대신 `id`에 실행 UUID를 전달한다. start·restart는 선택적으로 `env`와 `capture_logs`를 받는다. 실행 UUID가 변경 대상을 고정하고 요청 UUID가 재시도를 구분한다. 값·작업 편집 리비전은 프로세스 실행에 적용하지 않는다. `GET /api/process-logs?profile=app&id=UUID`는 명시적으로 요청한 원문을 `{content}`로 반환한다.

`Choose profile`로 대상을 선택하고 `Variables & secrets`에서 공통 값과 env override를 편집한다. secret 필드는 새 값을 입력하는 방식이며, 저장 후 입력을 비운다. 작업 목록에서는 생성과 상세 편집, 의존성, workstream 명세·계획과 상태 전이, task 점유·인계·체크포인트·완료를 실행한다.

화면은 변경 대상을 표시하고 제거·취소·인계·완료 전에 확인 단계를 제공한다. 응답 수신이 불확실하면 같은 요청을 재전송하며, 충돌 시 최신 내용을 조회한 후 다시 편집한다. 브라우저가 받은 실행 컨텍스트는 현재 페이지 메모리에 보관한다. 새로고침 후에는 최신 기록을 확인하고 takeover로 실행을 이어간다. 인증 세션은 origin별 sessionStorage에 유지한다.
