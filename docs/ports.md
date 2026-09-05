# 로컬 포트 사용하기

프로젝트의 서비스 포트를 선언하고 실행 명령에 연결한다. 서버가 실행 인자로
포트를 받는 경우 다음처럼 구성한다.

```toml
profile = "frontend"

[ports.web]
port = 3000
range = [3000, 3099]

[commands.dev]
exec = ["pnpm", "exec", "vite", "--port", "${bind.APP_PORT}", "--strictPort"]
serve = ["web"]

[commands.dev.bind]
APP_PORT = { port = "web" }
```

```sh
devtools doctor dev
devtools run dev
devtools port show web
```

doctor는 할당 후보와 참조를 진단한다. run은 최초 실행에서 포트를 할당하고,
명령 실행 기간의 중복 실행을 막는다. 다음 실행에서도 저장된 포트를 사용한다.
처음 지정한 포트가 사용 중이면 range에서 선택한다. 최초 할당부터 지정값을
요구하려면 `strict = true`를 선언한다.

서버가 환경변수를 읽으면 exec에 기존 실행 명령을 두고 bind의 키를 서버가
사용하는 변수명으로 지정한다. serve는 시작하는 서비스, bind는 주입할 값이다.

## 다른 프로젝트 주소 연결

backend 프로젝트에서 api 서비스를 선언하고 실행 위치에 별칭을 붙인다.

```sh
devtools instance name main
devtools port allocate api
```

frontend의 명령에서 해당 할당을 참조한다.

```toml
[commands.check]
exec = ["pnpm", "test"]

[commands.check.bind]
BACKEND_PORT = { profile = "backend", instance = "main", port = "api" }
BACKEND_URL = { template = "http://${var.BACKEND_HOST}:${bind.BACKEND_PORT}/api/v1" }
```

```sh
devtools var set BACKEND_HOST --value 127.0.0.1
devtools doctor check
devtools run check
```

bind가 기존 주소를 참조할 때는 서버가 이미 포트를 사용하고 있어도 정상이다.
애플리케이션 준비 확인은 프로젝트의 health 명령으로 수행한다. var는 선택한
profile·env에서 읽고, 템플릿은 문자열을 한 번 치환한다.

## 조회와 정리

```sh
devtools port list
devtools port check api --profile backend --instance main
devtools instance list --profile backend
devtools instance move main --profile backend --dir /new/path/backend
devtools port release api --profile backend --instance main
devtools port prune --profile backend
```

release는 활성 실행과 포트 점유를 확인한 뒤 할당을 해제한다. prune은 사라진
실행 위치의 기록을 정리하고 보류한 항목의 사유를 반환한다. 같은 profile의
다른 worktree와 clone은 실행 위치별로 서로 다른 할당을 사용한다.

전체 설정·범위·응답·오류는 [사용 계약](port-design.md), 명령별 옵션은
`devtools schema`에서 확인한다. 설치된 Linux 바이너리의 동작은
`just verify-docker ports`로 검증한다.
