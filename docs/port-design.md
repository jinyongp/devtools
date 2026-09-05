# 포트 관리와 명령별 값 연결

상태: 구현 예정인 port 기능의 사용 계약이다. 아래 설정은 port 기능이
추가된 버전에 적용한다. 현재 값 관리와 실행 방식은 [CLI 계약](cli-contract.md)을 따른다.

개발 서버의 포트를 전역으로 관리하고, 명령 실행 시 필요한 포트와 주소를
환경변수로 전달한다. 같은 프로젝트의 여러 worktree를 동시에 실행하거나
다른 로컬 프로젝트의 서버를 참조할 때 사용한다.

## 포트를 식별하는 기준

할당은 profile, 실행 위치, 서비스 이름의 조합으로 구분한다.

| 항목 | 의미 | 예시 |
| --- | --- | --- |
| profile | 설정과 데이터를 공유하는 프로젝트 식별자 | `backend` |
| 실행 위치 | 선택된 `devtools.toml` 디렉터리의 실제 절대 경로 | `/projects/backend` |
| 서비스 이름 | 프로젝트에서 선언한 서버 이름 | `api` |

현재 디렉터리에서 기존 프로젝트 탐색 규칙으로 설정을 찾고 실행 위치를
선택한다. 하위 디렉터리에서 실행해도 같은 설정을 찾으면 같은 할당을 사용한다.
일반 디렉터리, Git 저장소, worktree, 별도 clone에 같은 기준을 적용한다.
Git 정보는 worktree 상태를 확인하는 데 활용한다.

같은 profile의 worktree들은 var/sec와 task 데이터를 공유하고, 포트는
실행 위치별로 할당받는다. 같은 위치에서 branch를 변경해도 할당은 유지된다.
다른 프로젝트에서 특정 실행 위치를 참조할 때는 별칭을 사용한다.

## 서비스와 bind 선언

서비스의 포트와 선택 정책을 `[ports.NAME]`에 선언한다.

```toml
profile = "frontend"

[ports.web]
port = 3000
strict = false
range = [3000, 3099]

[commands.dev]
exec = ["pnpm", "dev"]
inject = true
serve = ["web"]

[commands.dev.bind]
APP_PORT = { port = "web" }
APP_URL = { template = "http://${var.APP_HOST}:${bind.APP_PORT}/api/v1" }
```

`bind`의 키는 자식 프로세스에 전달할 환경변수 이름이다. `APP_PORT`의
`port = "web"`은 현재 실행 위치의 web 서비스 할당을 참조한다.
중괄호 표기는 TOML 인라인 테이블이다.

선택한 profile에 호스트를 var로 등록한다.

```sh
devtools var set APP_HOST --value 127.0.0.1
```

web에 3000이 할당되면 `devtools run dev`는 다음 값을 넣어 `pnpm dev`를 실행한다.

```text
APP_PORT=3000
APP_URL=http://127.0.0.1:3000/api/v1
```

웹서버는 자신이 사용하는 환경변수를 읽어 수신 포트를 설정한다.
예시의 APP_PORT는 프로젝트에 맞는 이름으로 바꿀 수 있다.
실행 인자로 포트를 받는 서버는 아래 실행 인자 규칙을 사용한다.

## 단순 템플릿

템플릿은 문자열에 var 값과 포트 bind 결과를 치환한다.

| 참조 | 값 |
| --- | --- |
| `${var.KEY}` | 선택한 profile·env의 공통 값과 override를 적용한 var |
| `${bind.NAME}` | 같은 명령에 선언된 포트 bind 결과 |

포트 bind를 먼저 해석하고 템플릿을 평가한 뒤 자식 프로세스에 주입한다.
필요한 참조가 없으면 실행 전에 오류를 반환한다. 템플릿 참조 대상은 위 두
종류로 한정한다. secret은 기존 sec 주입 경로로 전달한다.

프로토콜, 경로, 쿼리는 템플릿의 문자열로 표현한다. 결과는 문자열 치환이며,
URL 인코딩이 필요한 값은 사용자가 해당 형태로 준비한다.

```toml
[commands.dev.bind]
APP_PORT = { port = "web" }
APP_URL = { template = "http://${var.APP_HOST}:${bind.APP_PORT}/api/v1" }
HEALTH_URL = { template = "http://${var.APP_HOST}:${bind.APP_PORT}/health" }
```

호스트는 `127.0.0.1`, 로컬 도메인, 현재 IP 등 프로젝트가 사용할 값을 var로
관리한다. 동적 IP는 외부에서 확인해 var를 갱신하고 다음 실행에 반영한다.
실행 중인 프로세스의 환경변수는 실행 시점의 값을 유지한다.

다른 프로젝트의 포트도 profile, 실행 위치의 별칭, 서비스 이름으로 선택해
포트 bind로 연결한다. 이렇게 얻은 결과는 같은 `${bind.NAME}` 문법으로
URL에 사용할 수 있다. 호스트 var는 해당 명령이 선택한 profile·env에서 읽는다.

## 할당과 실행 상태

포트 값, 충돌 정책, 자동 선택 범위를 각각 설정한다.

| 설정 | 의미 | 생략 시 |
| --- | --- | --- |
| `port` | 우선 사용할 포트 | 범위에서 자동 선택 |
| `strict` | 최초 할당에서 true이면 지정한 port만 선택하고 충돌 시 오류 | `false` |
| `range` | 자동 선택 범위 `[시작, 끝]`, 양 끝 포함 | 전역 기본 범위 |

`port = 3000`, `strict = false`, `range = [3000, 3099]`이면
새 할당에서 3000을 우선 시도하고, 충돌하면 범위 안의 빈 포트를 선택한다.
port는 우선값이므로 range 밖의 값도 허용한다.

범위에서 자동 선택하려면 다음과 같이 선언한다.

```toml
[ports.web]
range = [3000, 3099]
```

특정 포트를 반드시 사용하려면 strict를 활성화한다.

```toml
[ports.web]
port = 3000
strict = true
```

strict가 true이면 port가 필수이며 최초 할당에서 해당 포트만 선택한다. 함께 선언한
range는 자동 선택용 설정으로 유지한다. strict를 false로 변경한 뒤 새로 할당할 때 적용된다.
자동 선택 범위의 후보를 모두 사용 중이면 할당 오류를 반환한다.

할당 결과는 사용자 전역 데이터 저장소에 지속적으로 보관한다. 기록에는
profile, 실행 위치, 서비스 이름, 할당 포트가 포함된다. 재실행과 다른
프로젝트의 참조는 저장된 포트를 사용한다. release나 prune으로 정리할 때까지
할당을 유지한다.

port, strict, range는 최초 할당의 선택 정책이다. 기존 할당을 바꾸려면
명시적으로 해제한 뒤 다시 할당한다. 설정 변경만으로 기존 포트를 이동시키지 않는다.
변경한 주소를 사용할 서버와 클라이언트는 다시 실행한다.

새 포트는 전역 할당 기록과 OS 점유 상태를 함께 확인해 선택한다. strict 최초
할당에서 지정한 포트를 사용할 수 없으면 port_in_use를 반환한다. 전역 할당은
동시 요청을 조정한다. 이 기록은 devtools 내부의 할당 약속이며, OS 포트의
실제 점유는 서버 프로세스가 담당한다.

서버 실행 시 저장된 포트를 사용할 수 없으면 충돌 오류를 반환한다.
strict가 false인 경우에도 기존 할당을 유지한다. 확인 후 서버가 바인딩하기까지
발생하는 외부 프로세스와의 경쟁은 최종 실행 결과로 처리한다.
서버가 사용하는 포트는 주입한 할당값과 일치해야 한다.

조회와 주소 참조는 저장된 할당을 읽는다. 포트가 사용 중이라는 이유로
참조 주소를 변경하지 않는다. 실행 중인 프로세스는 주입 시점의 주소를 사용하며,
주소 변경은 재할당 결과를 확인하고 관련 프로세스를 재실행하는 흐름으로 반영한다.

할당 기록, OS 점유 상태, TCP 접속 가능 여부, 애플리케이션 준비 상태는
각각 구분한다. 다른 프로젝트의 주소를 참조할 때 대상이 없으면 참조 오류를
반환한다. 서버 준비 조건은 doctor 진단과 연결한다.

## 포트 명령의 역할

조회·변경 결과는 기존 CLI의 JSON 응답 방식을 따른다. 다음은 명령별 역할이다.

| 명령 | 역할 |
| --- | --- |
| `devtools port list` | 현재 profile의 실행 위치별 할당 조회 |
| `devtools port show web` | 선택한 서비스의 할당과 참조 정보 조회 |
| `devtools port allocate web` | 최초 할당 또는 저장된 기존 할당 반환 |
| `devtools port check web` | 현재 포트 점유·접속 상태 확인 |
| `devtools port release web` | 할당 해제 |
| `devtools port prune` | 삭제된 실행 위치의 할당·별칭 정리 |

일상적인 명령 실행은 run이 serve의 포트 할당과 bind의 주입을 담당한다. list와 show는
조회용이고, allocate는 실행 전에 포트를 확보할 때 사용한다.

## 실행 위치의 이동과 정리

실행 위치가 사라지면 조회에서 상태를 표시하고 prune으로 기록을 정리한다.
일시적인 디스크 연결 해제도 고려해 실제 정리는 명시적인 요청으로 수행한다.
포트가 사용 중이면 release와 prune은 해제를 보류하고 상태를 안내한다.
프로세스 종료는 별도의 작업으로 다룬다.

디렉터리를 이동했을 때는 기존 등록의 경로를 갱신할 수 있어야 한다.
포트 기록 정리 후에도 profile의 var/sec와 task 데이터는 유지된다.

## 서버 실행과 주소 참조

명령의 `serve = ["web"]`는 그 명령이 현재 실행 위치의 web 서버를 시작한다는
선언이다. 여러 서비스 이름을 지정할 수 있다. run은 serve의 미할당 포트를
할당하고 기존 할당은 유지한다. 실행 전 포트가 비어 있는지 확인하며,
사용 중이면 `port_in_use`로 종료한다.

같은 서비스에 대한 devtools 실행은 실행 기간 동안 하나만 허용한다.
동시 실행 시 후속 요청은 `port_run_active`를 반환한다. 실행 종료 후에도
포트 할당은 유지된다. 중단 후 남은 서버 프로세스는 OS 점유 검사로 확인한다.
PID만으로 서버의 정체나 정상 실행을 판단하지 않는다.

bind는 등록된 포트를 참조한다. serve에서 확보한 포트도 bind로 환경변수에
연결한다. serve에 없는 서비스는 기존 할당이 필요하며, 참조는 실행 중인
포트에도 성공한다. 다른 프로젝트의 서비스 시작은 해당 프로젝트의 run으로 수행한다.

여러 serve 포트는 한 요청으로 모두 확보한다. 입력과 참조를 먼저 검증하고,
할당 단계 실패 시 해당 요청의 신규 할당을 저장하지 않는다. 할당을 확정한 뒤
프로세스 시작이나 실행이 실패하면 확정한 할당은 다음 실행을 위해 유지한다.

## 실행 위치와 별칭 API

실행 위치의 API 이름은 `instance`다. 내부 식별자는 등록 시 발급한 불변 ID이며,
현재 위치의 실제 경로로 등록을 찾는다. 별칭은 profile 안에서 유일하고,
profile 이름과 같은 문자·길이 규칙을 사용한다. 한 instance에 별칭 하나를 둔다.

```sh
devtools instance list
devtools instance show
devtools instance name main
devtools instance move main --dir /new/path/backend
devtools instance remove main
```

name은 현재 위치를 등록하고 별칭을 지정한다. 같은 별칭이면 변경 없이 성공하고,
다른 instance가 사용 중이면 충돌 오류다. 현재 instance의 기존 별칭을 바꾸면
이전 별칭을 참조하는 설정은 참조 오류를 받는다. ID 참조는 유지된다.
move는 기존 ID·할당·별칭을 유지하면서 경로를 변경한다. 대상의 설정 profile은
선택한 profile과 같아야 하며, 기존 경로의 활성 실행과 대상 경로 중복을 검사한다.
remove는 활성 실행과 할당이 모두 정리된 instance를 삭제한다.

포트 명령은 `--profile`, `--instance`, `--dir`로 대상을 선택한다.
instance에는 별칭 또는 `id:ID`를 지정한다. 기본은 현재 설정 루트이며,
명시적 instance와 dir은 함께 지정하면 입력 오류다. list는 기본적으로 선택한
profile의 모든 instance를 반환하고 instance 또는 dir로 좁힐 수 있다.
다른 profile을 현재 경로에서 추측하지 않고 명시적인 instance로 선택한다.

조회는 등록을 생성하지 않는다. instance name과 최초 포트 할당이 등록을 만든다.
allocate는 대상 실행 위치의 설정을 읽고, show·check·release는 저장된 할당을 사용한다.
prune은 선택한 profile에서 경로가 사라진 instance를 확인한다. 모든 포트가
해제 가능할 때 해당 instance의 할당과 별칭을 함께 정리한다. 접근 권한 오류와
삭제는 구분하며, 확인이 불가능한 instance는 보류한다.

```toml
[commands.dev.bind]
BACKEND_PORT = { profile = "backend", instance = "main", port = "api" }
BACKEND_URL = { template = "http://${var.BACKEND_HOST}:${bind.BACKEND_PORT}/api/v1" }
```

profile을 생략하면 현재 profile을 사용한다. instance를 생략하면 현재 실행
위치를 사용하며, 다른 profile 참조에는 instance가 필수다. 포트 참조는 선택한
instance의 저장된 할당을 사용한다. 파일이 삭제된 instance의 참조는 오류다.

## 주입과 실행 인자

환경변수 적용 순서는 부모 환경, 선택한 profile·env 값, bind 결과다.
bind와 같은 이름의 var는 bind 결과로 덮어쓴다. 선택한 레이어의 sec와 이름이
겹치면 `binding_conflict`로 실행을 중단한다. 템플릿의 var 참조는 덮어쓰기 전
profile·env의 원본 var를 읽는다.

bind는 명령별 명시적 주입이다. `inject = false`여도 bind는 적용한다.
var 템플릿 참조는 선택한 profile·env에서 필요한 var를 읽고,
inject는 기존 계약대로 profile 값 전체의 주입 여부를 결정한다.

exec 배열에는 `${bind.NAME}`를 사용할 수 있다. bind 평가를 완료한 뒤
원래 배열 요소 하나를 인자 하나로 전달한다. 추가 인자 분할과 셸 평가는
수행하지 않는다. run 호출자가 덧붙인 인자는 입력 그대로 전달한다.

```toml
[ports.web]
port = 3000

[commands.dev]
exec = ["pnpm", "exec", "vite", "--port", "${bind.APP_PORT}", "--strictPort"]
serve = ["web"]

[commands.dev.bind]
APP_PORT = { port = "web" }
```

템플릿과 exec에서 `$${`는 리터럴 `${`를 뜻한다. 치환은 한 번 수행하며,
참조값에 들어 있는 `${...}`는 그대로 유지한다. 알 수 없는 참조, 닫히지 않은
구분자, 누락된 키, 템플릿 bind를 다른 템플릿에서 참조하는 경우는 오류다.
bind 하나는 포트 참조 또는 template 중 한 형태로 선언한다.

## 범위와 저장 범위

port는 정수 1–65535다. range는 같은 범위의 정수 두 개이며 시작값이 끝값
이하여야 한다. strict는 boolean이다. 자동 선택은 범위의 작은 값부터 확인한다.
기본 범위는 10000–19999이며, 서비스 range가 전역 기본값보다 우선한다.

사용자 설정 디렉터리의 `config.toml`에서 기본값을 지정한다.

```toml
[port]
range = [10000, 19999]
```

설정 디렉터리는 macOS에서 `~/Library/Application Support/devtools`,
Linux·WSL에서 `$XDG_CONFIG_HOME/devtools`이며 기존 경로 기본값 규칙을 따른다.
할당과 instance는 사용자 데이터 디렉터리의 `ports` 아래에서 관리한다.
설정 변경은 다음 신규 할당에 적용한다.

관리 대상은 현재 실행 환경의 로컬 TCP 포트다. 같은 전역 저장소 안에서는
profile·instance와 관계없이 할당 포트 번호를 유일하게 유지한다. 점유 검사는
지원되는 IPv4·IPv6 수신 주소를 고려하고, 확인 불가 시 오류를 반환한다.
다른 사용자, Docker, WSL과 호스트 사이의 별도 저장소는 각각의 할당 영역이다.
호스트 var는 접속 문자열의 재료이며 로컬 할당 영역을 변경하지 않는다.

## 진단과 응답

doctor는 설정, 참조, serve 포트의 사용 가능 여부를 읽기 전용으로 진단한다.
미할당 serve는 신규 할당이 필요함을 표시하고 후보 사용 가능 여부를 확인한다.
같은 명령의 bind는 이 예정 할당을 참조할 수 있다. 실제 포트 확정은 run에서 한다.
진단 성공은 이후의 할당·바인딩 성공을 보장하는 예약이 아니다.

check는 할당 포트의 점유 상태와 로컬 루프백 TCP 접속 결과를 반환한다.
TCP 연결 성공은 애플리케이션 준비와 별개이며, HTTP health 확인이나 대기는
프로젝트의 명시적인 명령으로 수행한다. run의 주소 참조는 서버 준비를 기다리지 않는다.

할당 응답의 공통 필드는 `profile`, `instance_id`, `alias`, `directory`,
`name`, `port`다. alias가 없으면 null이다. show는 이 객체를, list는
`items` 배열을 반환한다. allocate는 할당 필드와 `created`를 반환한다.
고정된 host나 URL 대신 bind 템플릿으로 접속 문자열을 구성한다.

check는 할당 필드에 `occupancy`(`free`, `in_use`, `unknown`)와
`tcp_reachable`(boolean 또는 확인 불가 시 null)을 추가한다. 진단 응답은
정상 수행 시 종료 코드 0이며, 점유만으로 서비스 소유자를 확정하지 않는다.
release는 `released`를 반환하고 이미 해제된 서비스는 false다.
prune은 `removed`와 사유 코드가 포함된 `retained` instance 배열을 반환한다.
활성 run이 있는 할당은 OS 포트가 아직 비어 있어도 release·prune에서 보류한다.

instance 응답은 `profile`, `instance_id`, `alias`, `directory`를 사용한다.
list는 `items`, name·move는 instance 필드와 `changed`, remove는 `removed`를
반환한다. 같은 요청의 반복은 기존 결과를 유지하며, 삭제된 대상을 다시 remove하면
removed는 false다. 모호한 대상 선택은 오류로 처리한다.

입력·TOML 오류는 기존 `invalid_argument`·`invalid_config` 계약을 따른다.
도메인 오류는 종료 코드 3이며 아래 고정 코드로 구분한다.

| 코드 | 의미 |
| --- | --- |
| `instance_not_found` | 선택한 instance 또는 실행 위치 없음 |
| `instance_conflict` | 별칭·경로 중복 또는 instance 삭제 조건 미충족 |
| `port_not_found` | 서비스 선언 또는 필요한 할당 없음 |
| `port_in_use` | 서버 실행용 포트 점유 또는 사용 중 할당 해제 요청 |
| `port_run_active` | 같은 서비스의 활성 실행 존재 |
| `port_exhausted` | 최초 할당 후보 소진 |
| `binding_conflict` | bind와 sec 이름 충돌 |
| `binding_reference_error` | bind·var 참조 누락 또는 잘못된 참조 |

저장소·권한·OS 조회 실패는 기존 I/O 오류 계약을 따른다. 오류 응답은
식별자와 조건을 제공하고 var/sec 값과 완성된 템플릿 문자열은 노출하지 않는다.
