# 프로세스 준비 확인

`process check`와 `process wait`는 실행 중인 이름 명령에 선언된 준비 검사를 수행한다.
`running`은 프로세스 수명 상태이고, `readiness.ready`는 해당 시점의 검사 결과다.
`status`와 `list`는 실행 메타데이터를 조회한다.

| 목적 | 명령 | 판단 기준 |
| --- | --- | --- |
| 실행에 필요한 설정·도구 확인 | `doctor web` | `data.ready`와 검사 항목 |
| 프로세스 수명 확인 | `process status ID` | `data.state` |
| 포트 점유·연결 확인 | `port check web` | 포트 검사 결과 |
| 서비스 준비 조건 한 번 확인 | `process check ID` | `data.readiness.ready` |
| 서비스 준비 조건 충족까지 대기 | `process wait ID` | 종료 코드 0이면 준비 완료 |

## 설정과 사용

아래 예시는 Python 3와 curl이 설치된 환경에서 실행할 수 있다.
작업 디렉터리를 제공하는 로컬 HTTP 서버를 띄우고 응답을 확인하는 예시다.
먼저 같은 디렉터리에 예제 서버 `server.py`를 만든다.

```python
import os
from http.server import SimpleHTTPRequestHandler
from socketserver import TCPServer

TCPServer.allow_reuse_address = True
with TCPServer(("127.0.0.1", int(os.environ["PORT"])), SimpleHTTPRequestHandler) as server:
    server.serve_forever()
```

`devtools.toml`에는 다음 내용을 저장한다.

```toml
profile = "readiness-demo"

[ports.web]
range = [28000, 28099]

[commands.web]
exec = ["python3", "server.py"]
serve = ["web"]

[commands.web.bind]
PORT = { port = "web" }

[commands.web.ready]
exec = ["sh", "-c", "curl --fail --silent --output /dev/null \"http://127.0.0.1:$PORT/\""]
timeout = "2s"
```

실제 프로젝트에서는 `commands.web.exec`를 서버 실행 명령으로 바꾸고,
서버가 제공하는 준비 확인 경로에 맞춰 URL을 지정한다. 서버의 포트 옵션이나 환경변수 이름도 프로젝트에 맞춘다.
`ready.exec`는 실행 파일과 인자 배열이다.
HTTP는 curl, DB는 해당 클라이언트, 복합 조건은 프로젝트 검사 스크립트로 확인한다.
쉘 문법은 위 예시처럼 비대화형 `sh -c`를 명시한 경우에 해석한다.
검사 명령은 종료 코드 0으로 준비 완료를 나타낸다.

```sh
request_id=$(python3 -c 'import uuid; print(uuid.uuid4())')
devtools process start web --request-id "$request_id"
```

start 성공 응답의 `data.item.id`를 아래 `EXECUTION_ID`에 넣는다.
`request_id`는 start 요청 재시도에 사용하고, 실행 ID는 검사·대기·종료 대상을 가리킨다.

```sh
devtools process check EXECUTION_ID
devtools process wait EXECUTION_ID --timeout 30s
devtools process stop EXECUTION_ID --request-id "$(python3 -c 'import uuid; print(uuid.uuid4())')"
```

`check`는 한 번 검사하고 결과를 반환한다. 검사 완료 시 준비 여부와 관계없이
종료 코드 0이며 `data.readiness.ready`로 판단한다.

`wait`는 준비 완료 시 종료 코드 0, 전체 대기 시간 초과 시 종료 코드 3과
`readiness_timeout`, 호출 취소 시 종료 코드 130과 `canceled`를 반환한다.
완료된 검사 사이에는 250ms 간격을 둔다. 기본 대기 시간은 30초, 최대 10분이다.
준비 실패 후에도 개발 프로세스는 자신의 수명을 유지한다.

`ready.timeout`은 1회 검사 제한으로 기본 2초, 허용 범위는 10ms부터 30초다.
검사 취소 시 프로세스 그룹에 TERM을 보내고 최대 3초 후 강제 정리한다.
호출 대기 시간이 끝나면 응답은 종료하고 supervisor가 남은 검사 정리를 맡는다.
검사 스크립트는 자신이 시작한 작업을 마친 뒤 종료하도록 작성한다.

## 실행 기준과 결과

검사 설정·작업 디렉터리·환경변수는 대상 프로세스를 시작할 때 확정한다.
var/sec/env와 port binding이 적용된 동일 환경을 사용한다.
실행 중 설정이나 값을 바꾸면 기존 실행은 시작 시점 기준을 유지하고,
restart로 생성한 새 실행은 최신 설정과 값을 사용한다.
설정 변경 전후의 실행은 고유 실행 ID로 구분한다.
검사 실행 파일과 스크립트 내용은 검사 시점의 파일을 사용한다.

성공 응답의 `data`에는 기존 실행 메타데이터와 다음 필드가 포함된다.
아래 JSON은 `data`의 준비 확인 필드만 발췌한 것이다.

```json
{
  "ready_configured": true,
  "readiness": {
    "ready": true,
    "checked_at": "2026-09-06T12:00:00Z",
    "reason": "probe_passed",
    "exit_code": 0
  }
}
```

`reason`은 `probe_passed`, `probe_failed`, `probe_timeout`,
`probe_execution_failed`, `process_stopped` 중 하나다.
종료 코드가 확인되지 않은 검사는 `exit_code: null`이다.
결과는 응답 시점의 관측값이며 다음 `check` 또는 `wait` 호출에서 새로 검사한다.
`ready_configured`만 실행 메타데이터에 보관한다.

| 오류 | 의미와 다음 동작 |
| --- | --- |
| `readiness_not_configured` | 준비 검사 선언을 추가하고 restart로 새 실행을 만든다. |
| `process_not_running` | status로 종료 원인을 확인하고 필요한 실행을 새로 시작한다. |
| `readiness_unavailable` | supervisor 통신 또는 검사 기능 확인 실패. status와 실행 파일 업데이트 여부를 확인한다. |
| `readiness_timeout` | 전체 대기 시간 사용. 서버와 준비 조건을 점검한 뒤 다시 대기한다. |

이 오류들의 종료 코드는 3이다. 잘못된 대기 시간은 `invalid_argument`와 종료 코드 2,
호출 취소는 `canceled`와 종료 코드 130으로 처리한다.

이 기능을 지원하는 실행 파일로 시작한 프로세스에서 사용한다.
업데이트 전에 시작한 프로세스에는 restart로 새 supervisor를 적용한다.

## 동시 실행과 데이터 보호

같은 실행의 검사는 한 번에 하나씩 수행한다. status·stop은 검사와 별도로 처리하며,
대상 종료와 호출 취소는 진행 중 검사에도 전달한다.
환경변수는 supervisor 메모리에서 전달한다. 검사 stdout·stderr는 버리고,
조회 응답과 로그에는 준비 여부·시각·원인·종료 코드만 제공한다.
준비 검사는 상태 확인 용도로 작성하고 반복 실행을 안전하게 처리해야 한다.

Dashboard의 Processes 화면에서 `Check readiness`로 동일 검사를 수행한다.
`POST /api/actions` 본문은 다음과 같다.

```json
{"domain":"process","profile":"app","process":{"action":"check","id":"EXECUTION_ID"}}
```

인증·profile 일치 검사를 적용하며 매 요청마다 새 관측을 반환한다.
준비 검사에는 변경 영수증을 사용하지 않는다.

설치 검증 실행법은 [검증 안내](../docker/README.md), 구현 구성과 검증 결과는
[관리 로드맵](management-roadmap.md#프로세스-준비-확인-확장)을 참고한다.
