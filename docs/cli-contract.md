# 사용법과 CLI 계약

이 문서는 확정된 사용 방식과 공개 CLI 계약을 정리한다.

현재 제공하는 명령은 `init`, `version`, `schema`, `help`, `project inspect`, `variable`/`var`, `secret`/`sec`, `env`, `run`이다. 이 문서의 예시는 빌드한 `devtools`가 PATH에 있는 환경을 기준으로 한다.

## 사용 목적

devtools는 개인용 개발 도구를 하나의 CLI로 제공한다. macOS, Linux, WSL에서 GUI와 대화형 입력 없이 사용하며, 주 사용자는 에이전트다.

프로젝트는 일반 환경변수를 읽고 기존 실행 명령을 유지한다. devtools는 프로젝트 명령을 호출하는 앞단의 실행 진입점이다. `package.json`이나 기존 task runner의 명령은 독립적으로 실행할 수 있고, 에이전트는 `devtools.toml`에 정의한 이름 또는 직접 지정한 명령으로 실행한다.

값은 사용자 전역 저장소에서 관리한다. 프로젝트에 `.env.*` 파일을 생성하거나 포함하는 방식이 아니다.

## profile과 프로젝트 연결

profile은 프로젝트를 식별하는 사용자 지정 이름이다. `myapp`, `work-api`처럼 지정하며, `local`, `dev`, `staging`은 profile 아래의 env로 구분한다. 이후 task 도구도 같은 profile을 프로젝트 식별자로 사용한다.

`var`, `sec`, `env`, `run`에는 profile이 필요하다. profile은 다음 순서로 선택한다.

1. 명령에 명시한 `--profile`.
2. 현재 위치에서 찾은 `devtools.toml`의 `profile`.
3. 둘 다 없으면 오류.

명시적인 profile은 설정 파일의 profile보다 우선하며, Git 저장소 밖에서도 사용할 수 있다. 값 관리와 직접 명령 실행은 `--profile`만으로 사용할 수 있다. 이름으로 명령을 실행할 때는 명령 정의를 읽을 `devtools.toml`이 함께 필요하다.

```sh
devtools var list --profile myapp
devtools run --profile myapp --env local -- pnpm dev
```

프로젝트에서 매번 profile을 지정하지 않으려면 다음 설정을 Git으로 추적한다.

```toml
profile = "myapp"
```

파일명은 `devtools.toml`이다. 파일에는 연결 정보와 프로젝트 설정을 두고, secret 값은 넣지 않는다. 설정 형식의 호환성은 도구가 관리하며 사용자는 설정 버전을 작성하지 않는다.

설정은 현재 디렉터리부터 상위로 탐색한다. 가장 가까운 파일을 선택하고, `.git`이 있는 디렉터리까지 확인한 뒤 탐색을 멈춘다. Git 저장소 밖에서는 파일시스템 루트까지 탐색한다.

worktree는 체크아웃한 자신의 설정 파일을 사용한다. 같은 profile을 지정한 worktree나 별도 clone은 전역 저장소의 같은 데이터를 사용한다. profile 식별자는 저장소 경로나 remote URL에서 자동 생성하지 않는다.

## 프로젝트 준비와 명령 탐색

```sh
devtools init --profile myapp
devtools project inspect
devtools project inspect --profile myapp
devtools project inspect --dir /path/to/project
devtools schema
devtools version
```

| 명령 | 동작 |
| --- | --- |
| `init --profile NAME` | 현재 디렉터리에 `devtools.toml`을 생성한다. |
| `project inspect` | 선택된 profile, 설정 출처와 경로, 사용자 전역 경로를 반환한다. |
| `schema` | 현재 바이너리가 지원하는 명령, 옵션, 입력 제약, 출력 스키마를 반환한다. |
| `version` | 빌드 버전 정보를 반환한다. |
| `help` | 명령 목록과 사용법을 JSON으로 반환한다. |

`init`은 profile을 명시적으로 받는다. 같은 profile의 유효한 설정이 있으면 내용을 바꾸지 않고 성공한다. 다른 profile이거나 잘못된 설정이면 기존 파일을 보존하고 오류를 반환한다. 생성 결과에는 `created`, `config_path`, `profile`이 포함된다. Git 스테이징은 수행하지 않는다.

`schema`와 `project inspect`는 조회 명령이다. 전자는 호출 방법을 확인하고, 후자는 현재 프로젝트 연결을 확인하는 데 사용한다.

## 공통 값과 env

profile에는 모든 env가 공유하는 공통 값과 env별 값이 있다. env별 값은 공통 값에 키를 추가하거나 같은 키의 값을 덮어쓴다.

| 키 | 공통 값 | local 값 | local의 최종 값 |
| --- | --- | --- | --- |
| `API_TIMEOUT` | `30` | 없음 | `30` |
| `LOG_LEVEL` | `info` | `debug` | `debug` |

값 관리와 직접 명령 실행에서 `--env`는 선택 옵션이다. 생략하면 공통 영역을 대상으로 한다. 이름으로 실행하는 명령은 아래의 `commands` 설정에서 주입 여부와 env를 선택한다.

| 작업 | `--env` 생략 | `--env local` 지정 |
| --- | --- | --- |
| 등록 | 공통 값을 쓴다. | local의 값을 쓴다. |
| 조회 | 공통 값만 조회한다. | 공통 값과 local 값을 합친 최종 상태를 조회한다. |
| 제거 | 공통 값을 제거한다. | local의 값만 제거한다. |
| 직접 명령 실행 | 공통 값을 주입한다. | 공통 값과 local 값을 합쳐 주입한다. |

local의 덮어쓰기 값을 제거하면 공통 값이 다시 적용된다. `unset`은 부모 프로세스의 환경변수를 제거하는 명령이 아니다.

### env 생성과 삭제

env는 값을 등록하거나 실행에 선택하기 전에 명시적으로 생성한다.

```sh
devtools env create local
devtools env create test
devtools env create staging
devtools env list
```

같은 env를 다시 생성하면 `changed: false`로 성공한다. `env list`는 이름을 정렬해 반환한다. env 이름에는 profile과 같은 문자 규칙을 적용한다. profile의 첫 값 등록이나 env 생성 시 전역 데이터가 준비된다.

존재하는 env를 지정해야 값 등록·조회·제거와 환경변수 주입을 수행할 수 있다. 없는 env를 지정하면 오류를 반환한다. 이름 명령의 설정에서 선택한 env에도 같은 규칙을 적용한다.

env를 삭제하려면 해당 env에 직접 설정한 var와 sec 값을 먼저 제거한다. 공통 영역에서 상속되는 값은 유지한다.

```sh
devtools var unset LOG_LEVEL --env local
devtools sec unset DATABASE_URL --env local
devtools env remove local
```

해당 env에 직접 설정한 값이 남아 있으면 삭제 요청은 오류를 반환한다. `env create`와 `env remove`도 `--profile NAME`으로 대상 profile을 지정할 수 있다.

## 일반 변수: variable / var

`variable`은 정식 이름이고 `var`는 같은 동작을 하는 별칭이다. 일반 변수는 값을 읽을 수 있다.

```sh
# 공통 값 등록
devtools var set LOG_LEVEL --value info

# local에서 덮어쓰기
devtools var set LOG_LEVEL --env local --value debug

# 공통 값 조회: info
devtools var get LOG_LEVEL

# local의 최종 값 조회: debug
devtools var get LOG_LEVEL --env local

# 공통 키 목록과 local의 최종 키 목록
devtools var list
devtools var list --env local

# local 덮어쓰기 제거: 이후 local에서도 공통 값 적용
devtools var unset LOG_LEVEL --env local

# 공통 값 제거
devtools var unset LOG_LEVEL
```

기본 목록은 값 대신 키와 출처를 보여준다. 값을 읽으려면 `get`을 사용한다. 위 명령들은 모두 `--profile NAME`으로 프로젝트 연결을 대신할 수 있다.

키는 영문자 또는 밑줄로 시작하고, 이후에는 영문자·숫자·밑줄을 사용한다. 값은 UTF-8 문자열이며 빈 문자열도 명시적인 값으로 취급한다. 환경변수로 전달할 수 있도록 NUL 바이트는 오류로 처리한다. 값의 `$NAME` 같은 문자열은 입력한 그대로 저장한다.

같은 값을 다시 등록하거나 이미 비어 있는 영역의 값을 제거하면 `changed: false`로 성공한다. 키 종류는 값을 모두 제거한 뒤에도 유지된다.

## 민감한 변수: secret / sec

`secret`은 정식 이름이고 `sec`는 같은 동작을 하는 별칭이다. secret은 프로세스에 주입할 수 있지만, 값을 반환하는 조회 명령은 제공하지 않는다.

값 등록은 파일이나 stdin을 사용한다. 다음 파일 경로는 실제 값을 담은 파일을 준비한 뒤 사용한다.

`--file`은 일반 파일을 읽고, `--stdin`은 파이프나 파일 리다이렉션으로 전달된 데이터를 읽는다. 한 번의 호출에서 입력 방식 하나를 선택한다. 공백과 마지막 개행을 포함해 입력 내용을 그대로 저장하므로 입력 파일은 사용할 값과 정확히 일치하게 준비한다.

```sh
# 공통 secret 등록
devtools sec set API_TOKEN --file /path/to/token

# local secret 등록: 셸은 파일 내용을 명령 인자로 펼치지 않음
devtools sec set DATABASE_URL --env local --stdin < /path/to/database-url

# 값 없이 키와 출처 확인
devtools sec list
devtools sec list --env local

# 지정한 영역의 값 제거
devtools sec unset DATABASE_URL --env local
devtools sec unset API_TOKEN
```

secret에는 값을 명령 인자로 넘기는 `--value`를 제공하지 않는다. GUI, 숨김 입력 프롬프트, 대화형 셸, 수동 잠금 해제를 요구하지 않는다. 파일·stdin 입력은 secret 값을 에이전트가 직접 읽거나 명령 문자열에 포함할 필요 없이 등록하는 경로다.

키의 종류는 profile 단위로 고정한다. 같은 키를 어떤 env에서는 var, 다른 env에서는 sec로 취급하지 않는다. env는 값만 덮어쓴다.

## 기존 dotenv 파일 가져오기: import

var와 sec가 섞인 `.env` 파일을 한 번에 가져온다. 에이전트는 파일 경로와 공개할 키 이름을 전달한다.

```sh
# 값 없이 키별 분류와 변경·충돌 여부 확인
devtools import --file .env.local --env local --dry-run

# 새 키 중 공개할 키를 반복 지정
devtools import --file .env.local --env local --var NODE_ENV --var LOG_LEVEL --var PORT

# 기존 값 교체를 포함해 가져오기
devtools import --file .env.local --env local --overwrite

# 프로젝트 연결 대신 profile 지정, 공통 영역에 등록
devtools import --file .env --profile myapp
```

기존 키는 profile에 등록된 var/sec 종류를 유지한다. 새 키 중 `--var KEY`로 지정한 키는 var, 나머지는 sec가 된다. `--var`로 지정한 키는 입력 파일에 있어야 한다. 기존 sec를 `--var`로 지정하면 종류 충돌로 처리한다. 종류는 값을 모두 제거한 키에도 유지된다.

`--env`를 생략하면 공통 영역, 지정하면 이미 존재하는 해당 env에 등록한다. 비교 대상은 선택한 영역에 직접 저장된 값이다. 공통 값을 상속하던 env에 값을 등록하면 새 덮어쓰기 항목이 된다.

같은 값은 `unchanged`로 성공한다. 기존 값이 다르면 `conflict`가 되고, `--overwrite`를 지정하면 `update`로 처리한다. 입력 전체를 검증하고 저장 잠금 안에서 최신 상태와 비교한 뒤 한 번에 저장한다. 문법 오류나 충돌이 있으면 기존 값 전체를 유지한다. 원본 파일은 가져오기 후에도 유지된다.

`--dry-run`은 저장소를 수정하지 않고 현재 상태를 미리 확인한다. 성공 응답의 `items`는 키 이름순이며 각 항목은 `key`, `kind`, `action`을 가진다. `action`은 `add`, `update`, `unchanged`, `conflict`, `kind_conflict` 중 하나다. 결과에는 값 대신 메타데이터만 포함한다.

```json
{"schema_version":1,"ok":true,"data":{"profile":"myapp","env":"local","dry_run":true,"changed":false,"applicable":true,"items":[{"key":"PORT","kind":"variable","action":"add"},{"key":"TOKEN","kind":"secret","action":"add"}]}}
```

`changed`는 실제 저장 변경 여부이고, 미리보기에서는 항상 false다. `applicable`은 현재 옵션으로 전체를 적용할 수 있는지 나타낸다. 미리보기는 충돌이 있어도 종료 코드 0과 `applicable: false`를 반환한다. 실제 적용에서 충돌하면 `import_conflict`와 종료 코드 3을 반환하고 `error.details.items`에 메타데이터를 제공한다. 미리보기와 실제 적용 사이의 변경은 실제 적용 시 다시 검사한다.

입력은 UTF-8 일반 파일이다. 지원 문법은 다음과 같다.

- `KEY=value`, 선택적인 `export` 접두사, 빈 값, 빈 줄, `#` 주석.
- CRLF 개행과 파일 처음의 UTF-8 BOM.
- 따옴표 없는 값은 양끝 공백을 정리한다. `#`가 값의 처음이거나 공백·탭 뒤에 있으면 주석으로 해석한다. `a#b`는 그대로 저장한다.
- 작은따옴표 안의 값은 리터럴로 저장한다. 큰따옴표 안에서는 `\n`, `\r`, `\t`, `\"`, `\\`, `\$`를 해석하고 나머지 백슬래시 조합은 그대로 보존한다.
- 따옴표로 감싼 여러 줄 값은 개행을 포함해 저장한다. 닫는 따옴표 뒤에는 공백과 주석을 허용한다.
- `$NAME`, `${NAME}`, `$(...)`는 문자열로 저장한다. 파일은 셸 명령으로 실행하지 않는다.

중복 키·잘못된 키·닫히지 않은 따옴표·NUL·잘못된 UTF-8은 `invalid_dotenv`와 종료 코드 2로 처리한다. 문법 오류에는 `error.details.line`과 고정된 원인 설명을 제공한다. 파일 내용과 값은 진단에 포함하지 않는다.

## 명령 실행: run

`run`은 이름으로 등록한 명령과 직접 지정한 명령을 실행한다. 환경변수 주입을 선택하면 var와 sec를 함께 적용한다.

### 직접 명령 실행

`--` 뒤에 실행 파일과 인자를 지정한다. 실행 위치는 devtools를 호출한 현재 디렉터리다.

```sh
# 공통 값만 주입
devtools run -- pnpm dev

# 공통 값에 local 값을 덮어써 주입
devtools run --env local -- pnpm dev

# devtools.toml 없이 실행
devtools run --profile myapp --env local -- pnpm dev
```

### 이름으로 명령 실행

`devtools.toml`의 `commands`에 명령별 실행 내용과 환경변수 주입 설정을 둔다.

```toml
profile = "myapp"

[commands.dev]
exec = ["pnpm", "dev"]
inject = true
env = "local"

[commands.check]
exec = ["pnpm", "check"]
inject = true
env = "test"

[commands.lint]
exec = ["pnpm", "lint"]
```

```sh
devtools run dev
devtools run check
devtools run lint
```

`exec` 배열의 첫 항목은 실행 파일이고, 나머지는 해당 프로그램에 전달할 인자다. 각 이름은 단일 명령을 실행한다.

| 설정 | 동작 |
| --- | --- |
| `inject = false` 또는 생략 | 부모 프로세스의 환경을 상속한다. |
| `inject = true`, `env` 생략 | 부모 환경에 profile 공통 값을 주입한다. |
| `inject = true`, `env` 지정 | 부모 환경에 공통 값과 해당 env 값을 합쳐 주입한다. |

이름 명령은 선택된 `devtools.toml`이 있는 디렉터리에서 실행한다. 프로젝트 하위 폴더에서 `devtools run dev`를 호출해도 같은 실행 위치를 사용한다. worktree에서는 해당 worktree의 설정 파일과 실행 위치를 사용한다.

### 이름 명령의 env 선택

실행 시 `--env`를 지정하면 명령 설정의 env보다 우선하며, 환경변수 주입을 활성화한다.

```sh
# commands.dev에 설정된 local 사용
devtools run dev

# staging으로 덮어써 실행
devtools run dev --env staging

# 주입 설정을 생략한 lint에도 local 값 주입
devtools run lint --env local
```

명시적인 `--env`는 `inject = false`로 설정된 명령에서도 주입을 활성화한다. `--env`를 생략하면 명령의 `inject`와 `env` 설정을 적용한다.

### 이름 명령에 추가 인자 전달

이름 명령 뒤의 `--` 이후 인자는 `exec` 배열 끝에 순서대로 추가한다.

```toml
[commands.test]
exec = ["pnpm", "test"]
inject = true
env = "test"
```

```sh
# pnpm test --watch 실행
devtools run test -- --watch

# staging 값을 주입하고 pnpm test --watch 실행
devtools run test --env staging -- --watch
```

추가 인자는 각 인자의 경계를 유지해 실행 프로그램에 전달한다. `exec`에서 셸을 선택했다면 인자의 의미는 해당 셸의 호출 규칙을 따른다.

| 호출 형식 | `--` 이후의 의미 |
| --- | --- |
| `devtools run -- PROGRAM ARG...` | 직접 실행할 프로그램과 인자 |
| `devtools run NAME -- ARG...` | 등록된 `exec` 뒤에 추가할 인자 |

devtools의 `--profile`, `--env` 옵션은 구분자 `--` 앞에 지정한다.

### 여러 단계 구성

여러 단계의 순서와 실패 처리는 프로젝트의 task runner 또는 명시적으로 실행한 셸이 담당한다. devtools는 그 진입 명령을 실행하고, 선택한 환경을 자식 프로세스에 전달한다.

예를 들어 위의 `commands.check`는 다음 프로젝트 스크립트를 호출할 수 있다.

```json
{
  "scripts": {
    "check": "pnpm lint && pnpm test"
  }
}
```

셸에서 단계를 구성할 때는 실행할 셸과 명령 문자열을 명시한다.

```toml
[commands.check]
exec = ["sh", "-c", "go vet ./... && go test ./..."]
inject = true
env = "test"
```

이 예시에서는 셸이 앞 명령의 성공 여부에 따라 다음 명령을 실행한다. 선택한 환경변수는 두 명령에 상속된다.

### 환경변수와 프로세스 동작

주입을 선택한 실행의 환경변수 우선순위는 다음과 같다. 오른쪽 값이 같은 키의 왼쪽 값을 덮어쓴다.

```text
부모 프로세스 환경 → profile 공통 값 → 선택한 env 값
```

주입은 실행한 자식 프로세스와 그 자식에만 적용된다. 에이전트의 부모 셸이나 다른 터미널의 환경변수는 바뀌지 않는다. 같은 profile을 공유해도 각 worktree는 실행할 때 env를 따로 선택할 수 있다.

`run`은 자식 프로세스의 입출력과 신호를 전달하고 종료 코드를 보존한다. 자식 출력은 JSON으로 감싸거나 마스킹하지 않는다. 따라서 `true`, `3000` 같은 변수값 때문에 정상 출력이 변형되지 않는다.

실행 파일은 선택한 실행 디렉터리와 최종 환경의 PATH를 기준으로 찾는다. `SIGINT`, `SIGTERM`, `SIGHUP`을 받으면 실행한 프로세스 그룹에 같은 신호를 전달한다. 종료 신호 전달 후 3초 동안 명령이 계속 실행되면 프로세스 그룹을 강제 종료한다. 신호로 종료한 자식의 종료 코드는 `128 + 신호 번호`다.

## 출력과 비대화형 사용

`devtools doctor [COMMAND]`로 프로젝트 환경과 필수 조건을 진단한다.
설정의 requirements와 실행 전 검사는 [개발환경 진단](doctor.md)을 따른다.

조회·변경 명령은 기본적으로 JSON을 반환한다. `--json`은 필요하지 않다. 성공은 stdout, 오류는 stderr로 분리하고, 오류에는 에이전트가 분기할 수 있는 고정된 코드가 포함된다. 응답의 `schema_version`은 도구가 제공하는 정보이며 사용자 설정값이 아니다.

일반 명령은 입력을 요구하는 프롬프트를 띄우지 않는다. 명시적으로 지정한 stdin 입력은 데이터 입력 경로이며 대화형 질의가 아니다.

`run`은 데이터 조회 명령과 출력 계약이 다르다. 실행 준비 중의 실패는 devtools의 오류로 반환하고, 자식 프로세스가 실행된 뒤에는 그 프로세스의 출력과 종료 상태를 전달한다.

정확한 JSON 필드와 오류 코드는 `devtools schema`에서 확인한다. 스키마의 `args`는 위치 인자, `child_args`는 `--` 이후 인자를 뜻한다. `aliases`에는 같은 동작을 하는 별칭이 포함된다.

| 작업 | 성공 응답의 `data` |
| --- | --- |
| var·sec 등록/제거, env 생성/삭제 | `profile`, `changed` |
| var·sec 목록 | `profile`, `items` |
| var 값 조회 | `profile`, `value`, `metadata` |
| env 목록 | `profile`, `envs` |

목록 항목과 `metadata`에는 `key`, `kind`, `source`, `overrides`가 포함된다. `source`는 `common` 또는 `env`이며, `overrides`는 선택한 env 값이 공통 값을 덮어썼는지 나타낸다.

| 오류 코드 | 의미 |
| --- | --- |
| `env_not_found` | 선택한 env 생성 필요 |
| `env_not_empty` | 삭제할 env에 직접 설정한 값이 남아 있음 |
| `key_not_found` | 선택한 범위에 조회할 일반 변수 값이 없음 |
| `kind_conflict` | 해당 키가 다른 종류로 등록되어 있음 |
| `storage_error` | 전역 저장소 접근 또는 권한 확인 실패 |
| `invalid_storage` | 저장 데이터 형식 오류 |
| `command_not_found` | 이름 명령 또는 실행 파일 탐색 실패 |
| `execution_failed` | 자식 프로세스 시작 실패 |
| `requirements_failed` | 선언한 명령 필수 조건이 충족되지 않음; details.checks로 원인 확인 |

## 로컬 저장과 secret 보호 범위

첫 버전은 사용자 전용 로컬 파일 저장소를 사용하며 secret 자체를 암호화하지 않는다. 저장 디렉터리는 `0700`, 파일은 `0600` 권한을 사용한다. OS 로그인 인증이나 별도 복호화 키 준비를 요구하지 않는다.

devtools의 기본 목록, 오류, 로그에는 secret 값을 포함하지 않는다. secret은 실행할 프로세스에 직접 주입한다. 보호 목표는 에이전트가 정상적인 CLI 사용 중 실수로 값을 읽어 대화나 로그에 남기지 않게 하는 것이다.

같은 사용자 권한으로 저장 파일을 직접 읽는 접근까지 차단하지는 않는다. 자식 프로그램이 secret을 출력하면 그 출력은 그대로 전달된다. 이 저장 방식과 CLI 계약은 동일 사용자 권한의 의도적 접근을 막는 격리 경계가 아니다.
