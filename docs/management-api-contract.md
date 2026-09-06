# 관리 API 계약

dashboard의 task/workstream, var/sec/env, 프로젝트·프로세스, 백업·복구와 저장소 정리 API 계약이다.

## 접속과 요청

`devtools dashboard`가 발급한 링크를 교환한 세션은 현재 사용자의 모든 profile을 관리한다. profile은 요청 대상을 선택한다. loopback 바인딩, 정확한 Host/Origin 검사, bearer 인증과 세션 만료를 모든 관리 API에 적용한다.

변경은 도구별 허용 action과 구조화된 입력으로 전달한다. 대상 profile과 입력을 명시하고, 변경 종류에 따라 요청 UUID·편집 리비전·실행 ID·미리 보기 ID로 정확한 대상을 고정한다. HTTP 계층은 도구의 공통 검증·변경 로직을 호출한다. task 변경은 기존 request-id와 실행 컨텍스트 계약을 따른다.

동일한 요청 ID와 동일한 입력은 기존 결과를 반환한다. 다른 입력으로 같은 ID를 사용하면 `request_conflict`로 처리한다. 현재 리비전이 편집 기준과 다르면 변경 내용을 다시 조회하도록 안내한다. 응답은 기존 JSON 성공·오류 envelope를 사용한다.

## 영향 확인과 민감한 입력

삭제와 복구는 영향 미리 보기 결과를 기준으로 적용한다. 적용 시 대상과 현재 상태를 다시 확인한다. 미리 보기의 해시는 변경 허가를 대신하지 않으며, 인증된 요청과 대상 검증을 함께 사용한다.

secret은 입력·교체를 지원하고 조회 응답과 이력에는 메타데이터만 기록한다. HTTP 요청 본문과 오류 원문을 로그에 남기지 않는다. 프로세스 실행은 등록된 이름 명령을 대상으로 삼는다.

## 관리 범위

CLI와 dashboard는 task/workstream, var/sec/env, 프로세스, 백업·복구, 정리를 같은 접근 인증과 작업별 검증으로 연결한다. 백업 키 생성과 기본 저장 위치 설정은 CLI에서 준비한다.

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

준비 확인은 같은 endpoint에 `{domain:"process", profile:"app", process:{action:"check", id:"UUID"}}`를 전달한다. 대상 profile을 검사한 뒤 실행 시점의 설정·환경으로 한 번 확인하고, 실행 메타데이터와 `readiness`를 반환한다. 매 요청은 새 관측이며 변경 영수증을 사용하지 않는다. [준비 확인 계약](process-readiness.md)에 결과와 시간 제한을 정의한다.

`GET /api/project?profile=app&instance=ID`는 선택한 프로젝트의 이름 명령과 port 할당 메타데이터를 조회한다. 명령의 실행 인자와 환경변수 원문은 조회 응답에 포함하지 않는다.

정리 미리 보기는 `GET /api/cleanup-preview?profile=app`, 보관함 목록은 `GET /api/archives?profile=app`이다. profile을 생략하면 전체 범위다. 적용 요청은 `{domain:"cleanup", profile:"app", cleanup:{action:"apply", plan:"PLAN_UUID", ids:["ITEM_UUID"], request_id:"UUID"}}`다. restore·purge는 `{action:"restore", id:"ARCHIVE_UUID"}`처럼 보관함 ID를 전달한다. 전체 범위 요청은 profile에 빈 문자열을 사용한다. 선택된 미리 보기 항목과 archive ID가 변경 대상을 고정한다.

암호화 백업 생성은 `{domain:"backup", profile:"app", backup:{action:"create", request_id:"UUID"}}`로 요청하며 `{path, replayed}`를 반환한다. 백업 파일명은 요청 UUID로 고정한다. 복구 미리 보기는 backup에 `{action:"restore", file:"/backups/app.age", identity_file:"/secure/identity", source_profile:"app", replace:false}`를 전달한다. envelope의 profile이 복원 대상이며 source_profile은 백업 안의 원본이다. 적용은 같은 입력에 미리 보기의 `digest`와 새 `request_id`를 추가한다. 원문 secret과 개인키는 응답에 포함하지 않는다.

`Choose profile`로 대상을 선택하고 `Variables & secrets`에서 공통 값과 env override를 편집한다. secret 필드는 새 값을 입력하는 방식이며, 저장 후 입력을 비운다. 작업 목록에서는 생성과 상세 편집, 의존성, workstream 명세·계획과 상태 전이, task 점유·인계·체크포인트·완료를 실행한다.

화면은 변경 대상을 표시하고 제거·취소·인계·완료 전에 확인 단계를 제공한다. 응답 수신이 불확실하면 같은 요청을 재전송하며, 충돌 시 최신 내용을 조회한 후 다시 편집한다. 브라우저가 받은 실행 컨텍스트는 현재 페이지 메모리에 보관한다. 새로고침 후에는 최신 기록을 확인하고 takeover로 실행을 이어간다. 인증 세션은 origin별 sessionStorage에 유지한다.
