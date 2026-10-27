# 변수·secret과 명령 실행

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

프로젝트에 profile을 연결하고 변수·secret·env를 관리해 명령에 전달하는 가이드다.
예시는 `devtools`가 PATH에 있는 환경을 전제로 한다.
설치본의 기능은 [version·schema 확인 방법](README.md#check-feature-availability)을 따른다.

- [프로젝트 연결](#profile과-프로젝트-연결), [env 만들기](#env-생성과-삭제).
- [일반 변수](#일반-변수-variable--var), [secret 등록](#민감한-변수-secret--sec), [dotenv 가져오기](#기존-dotenv-파일-가져오기-import).
- [값 묶음](#재사용할-값-묶음-bundle), [메타데이터와 사용처](#값-메타데이터와-사용처-values).
- [직접·이름 명령 실행](#프로젝트-명령-command), [추가 인자 전달](#이름-명령에-추가-인자-전달).

포트와 URL은 [포트 가이드](ports.md), 서버 수명 관리는 [프로세스 가이드](processes.md),
JSON 형식·오류·protocol 전환은 [CLI 출력 계약](cli-output.md)을 따른다.

## 사용 목적

devtools는 개인용 개발 도구를 하나의 CLI로 제공한다. macOS, Linux, WSL 및 Windows 네이티브에서 에이전트가 CLI로 사용하고, 사용자는 선택적으로 dashboard에서 같은 데이터를 관리한다. Windows OS trust/untrust는 대화형 확인이 필요하며 [Windows 지원 한계](windows.md)를 따른다.

프로젝트는 일반 환경변수를 읽고 기존 실행 명령을 유지한다. devtools는 프로젝트 명령을 호출하는 앞단의 실행 진입점이다. `package.json`이나 기존 task runner의 명령은 독립적으로 실행할 수 있고, 에이전트는 `devtools.toml`에 정의한 이름 또는 직접 지정한 명령으로 실행한다.

값은 사용자 전역 저장소에서 관리하고 실행 시 환경변수로 주입한다.

## profile과 프로젝트 연결

profile은 프로젝트를 식별하는 사용자 지정 이름이다. `myapp`, `work-api`처럼 지정하며, `local`, `dev`, `staging`은 profile 아래의 env로 구분한다. task 도구도 같은 profile을 프로젝트 식별자로 사용한다.

`var`, `sec`, `env`, `command run`에는 profile이 필요하다. profile은 다음 순서로 선택한다.

1. 명령에 명시한 `--profile`.
2. 현재 위치에서 찾은 `devtools.toml`의 `profile`.
3. 둘 다 없으면 오류.

명시적인 profile은 설정 파일의 profile보다 우선하며, Git 저장소 밖에서도 사용할 수 있다. 값 관리와 직접 명령 실행은 `--profile`만으로 사용할 수 있다. 이름으로 명령을 실행할 때는 명령 정의를 읽을 `devtools.toml`이 함께 필요하다.

```sh
devtools var list --profile myapp
devtools command run --profile myapp --env local -- pnpm dev
```

프로젝트에서 매번 profile을 지정하지 않으려면 다음 설정을 Git으로 추적한다.

```toml
profile = "myapp"
```

파일명은 `devtools.toml`이다. 파일에는 연결 정보와 프로젝트 설정을 두고, secret 값은 넣지 않는다. 설정 형식의 호환성은 도구가 관리하며 사용자는 설정 버전을 작성하지 않는다.

설정은 현재 디렉터리부터 상위로 탐색한다. 가장 가까운 파일을 선택하고, `.git`이 있는 디렉터리까지 확인한 뒤 탐색을 멈춘다. Git 저장소 밖에서는 파일시스템 루트까지 탐색한다.

worktree는 체크아웃한 자신의 설정 파일을 사용한다. 같은 profile을 지정한 worktree나 별도 clone은 전역 저장소의 같은 데이터를 사용한다. profile 식별자는 저장소 경로나 remote URL에서 자동 생성하지 않는다.

## 프로젝트 준비와 명령 탐색

`devtools --help`는 명령을 기능별로 묶어 보여 준다. `cmd`, `var`, `sec` 별칭은
원래 명령 옆에 표시한다. `devtools GROUP --help`로 해당 명령군의 작업 흐름과 옵션을
확인하고, 자동화에 필요한 입력·출력 계약은 `devtools schema GROUP COMMAND`로 조회한다.

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
| `schema` | 짧은 명령 그룹 목록을 JSON으로 반환한다. |
| `schema var get` | 지정한 명령의 입력·출력 스키마를 반환한다. |
| `schema --all` | 전체 카탈로그를 반환한다. 전체 계약 수집이 필요할 때 사용한다. |
| `version` | 빌드 버전과 CLI `protocol_version`을 반환한다. 응답 envelope 버전은 최상위 `schema_version`에 있다. |
| `help`, `--help` | 사람이 읽을 수 있는 짧은 텍스트 도움말을 출력한다. |

`init`은 profile을 명시적으로 받는다. 같은 profile의 유효한 설정이 있으면 내용을 바꾸지 않고 성공한다. 다른 profile이거나 잘못된 설정이면 기존 파일을 보존하고 오류를 반환한다. 생성 결과는 `data.item`에 `config_path`와 `profile`, `data.changed`에 생성 여부를 반환한다. Git 스테이징은 수행하지 않는다.

`schema`와 `project inspect`는 조회 명령이다. 전자는 호출 방법을 확인하고, 후자는 현재 프로젝트 연결을 확인하는 데 사용한다.

protocol 12부터 이름 명령의 `run`·`process start`·`project up`은 `serve`에 연결된 proxy
선언을 자동 준비한다. `--proxy`는 비활성 route까지 준비하고 matching route를 요구한다.
`--no-proxy`와 `--proxy=false`는 준비만 생략한다. `run` 옵션은 이름 앞에 둔다.
설정·권한·기존 실행 전환 규칙은 [로컬 네트워크](local-network.md)를 따른다.

인자 없이 실행한 `devtools`도 텍스트 도움말을 제공합니다. `devtools task --help`로
하위 명령을 찾고, `devtools task claim --help`로 사용법을 확인하세요.
에이전트는 `devtools schema task`로 그룹을 탐색한 뒤 `devtools schema task claim`처럼
필요한 명령만 조회할 수 있습니다. 명령별 스키마에는 입력 스키마와 중복되는 옵션
목록을 생략하며, 참조에 필요한 `$defs`는 해당 응답에 함께 제공합니다.

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

Linux/macOS의 `env list`, `variable list/get`(`var`), `secret list`(`sec`)는
잠금 파일이 준비되어 있고 복구가 필요 없는 기존 저장소를 읽기 전용 샌드박스에서
조회할 수 있다. 잠금을 생략하지 않으며 writer와 대기·배제 규칙을 공유한다.
초기화·복구에 쓰기가 필요하면 실패한다. 오류의 `facts.state`와 `value_write` remedy를
확인하고 해당 저장소에 쓰기 가능한 환경에서 조회한 뒤 재시도한다.
다른 조회 명령과 Windows에 같은 지원을 가정하지 않는다.
| 제거 | 공통 값을 제거한다. | local의 값만 제거한다. |
| 직접 명령 실행 | 공통 값을 주입한다. | 공통 값과 local 값을 합쳐 주입한다. |

local의 덮어쓰기 값을 제거하면 공통 값이 다시 적용된다. `unset`은 부모 프로세스의 환경변수를 제거하는 명령이 아니다.

### 조건부 변경과 응답 유실 복구

`variable/secret set/unset`, `env create/remove`, `import`는 선택적으로
`--revision REVISION --request-id UUID`를 함께 받는다. 먼저 `values revision`
으로 profile 전체의 불투명한 편집 revision을 조회한다. 이 토큰은 값 사용 권한이 아니다.
guard 없는 기존 호출은 기존 입력·출력과 변경 동작을 유지하지만, 호출자가 관측한
과거 상태를 기준으로 한 편집 의도까지 검사하지는 않는다. CLI와 dashboard의
writer는 같은 저장소 revision과 commit 경합 검사를 사용한다.

```sh
devtools values revision --profile app
devtools secret set TOKEN --stdin --profile app --revision REVISION --request-id UUID --create-only
devtools values request show UUID --profile app
```

예시의 REVISION과 UUID는 조회한 revision과 호출자가 보존할 새 요청 UUID로 바꾼다.
성공한 변경과 성공한 no-op은 결과와 요청 식별값을 한 transaction으로 저장한다.
응답이 유실되면 같은 UUID·revision·입력 바이트로 재시도한다. file/stdin의 출처는
identity에 포함하지 않지만 읽은 값과 action·env·모든 변경 옵션은 포함한다.
같은 ID의 다른 입력은 `request_conflict`, 새 요청의 오래된 revision은
`revision_conflict`다. 재시도는 원래 결과를 반환하므로 현재 값은 따로 조회한다.
권한·검증·commit 전 실패는 ID를 소비하지 않는다. 입력을 고칠 때에는 새 UUID를 사용한다.
UUID 문자열은 대소문자까지 동일하게 보존한다. 서로 다른 표기는 별개 요청이며
새 receipt의 저장 경로도 파일시스템의 대소문자 비교에 의존하지 않는다.
기존 receipt는 옮기지 않고 함께 조회한다. 새 protocol 9 변경의 재시도에는
그 계약을 지원하는 실행 파일을 계속 사용한다.

`--create-only`는 guard를 지정한 set에서만 지원하며 선택 계층의 빈 값·만료 값도
기존 값으로 판단해 `value_exists`로 거절한다. 공통 값을 상속하던 env에 새 override를
만드는 것은 허용한다. unset은 해당 계층만 제거하고 kind·ValueID·bundle membership을 유지한다.

`values request show`는 `{profile,item:{request_id,state,result?,created_at,expires_at}}`를
반환한다. state는 `completed` 또는 `expired`이며 만료되면 result를 생략한다.
시각은 UTC RFC3339 문자열이며 기존 시각 없는 receipt는 null과 무기한 재시도를 유지한다.
보호된 root에서는 기존 warm session만 빌리고 daemon 시작·해제·idle 연장을 하지 않는다.
누락은 `request_not_found`/3이며 자동 실행하지 않는다. 새 성공의 replay 기한은 저장부터
30일, `now >= expires_at`부터 `request_expired`/3이다. 메타데이터는 root 수명 동안
보존하므로 만료된 ID도 다시 실행하지 않는다. root 삭제·전체 과거 rollback·다른 root로
profile 이동 이후의 중복 방지는 보장하지 않는다.

guarded import는 CLI의 선택 계층 비교를 유지하며 500,000 bytes·5,000 keys로 제한한다.
dashboard의 유효 값 유지/skip 동작은 별개다. `--dry-run`은 receipt를 만들지 않으므로
같은 UUID로 실제 apply를 할 수 있다. 실제 저장 뒤에는 preview를 포함한 입력 변경에 새 ID가 필요하다.

명령 schema는 정적인 옵션 충돌·동반 필수 조건과 TCP 포트 1..65535를 검증한다.
doctor timeout은 Go duration의 소수·복합 단위와 선행 `+`를 허용한다.
schema의 duration 패턴은 문법만 검사하며, 수치 overflow와 양수 여부는 실행 시
`time.ParseDuration`으로 최종 판정한다. 파일 존재·인증·저장 상태도 실행 시 확인한다.

### secret 자동 생성

`secret generate KEY --revision REVISION --request-id UUID`는 명시적으로 초기화·해제한
보호 저장소에 crypto/rand의 32바이트를 padding 없는 base64url 43자로 저장한다.
profile/env 선택은 set과 같고 원문 대신 변경 메타데이터만 반환한다.
기본은 선택 계층의 생성 전용이며, 기존 빈 값·만료 값에도 `value_exists`로 거절한다.
`--replace`는 없는 slot의 생성과 기존 secret 교체를 허용하되 만료 시각을 보존한다.
이미 만료된 slot에는 `value_expired`로 실패한다. variable의 종류를 secret으로 바꾸지 않는다.

응답 유실 뒤 같은 요청을 재시도하면 원래 메타데이터를 반환하고 새 난수를 만들거나
나중 writer의 값을 덮어쓰지 않는다. 실제 commit 전 실패로 사라진 후보는 다음 시도에서
다시 생성할 수 있다. 길이·인코딩·seed 선택이나 값 출력·외부 인증 발급 기능은 제공하지 않는다.

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

# 파일 전체를 값 하나로 등록 (실제 값이 담긴 일반 파일 준비)
devtools var set API_URL --file /path/to/api-url

# POSIX 셸에서 파이프 입력: 끝 개행 없이 전달
printf '%s' 'https://example.com' | devtools var set API_URL --stdin

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

`set`은 `--value`, `--file`, `--stdin` 중 정확히 하나를 받는다. `--file`은 일반 파일
전체를 값 하나로 읽으며 dotenv 구문을 해석하지 않는다. `--stdin`은 파이프나 파일
리다이렉션을 받으며 터미널 입력을 받지 않는다. 파일과 stdin은 공백·CRLF·마지막 개행을
그대로 보존한다. `--value ""`, 빈 파일, 빈 stdin은 모두 빈 값을 등록한다.
입력 경로와 무관하게 var로 저장되며 sec로 분류되지 않는다. 설치본의 지원 여부는
`devtools schema var set`에서 확인한다.

키는 영문자 또는 밑줄로 시작하고, 이후에는 영문자·숫자·밑줄을 사용한다. 값은 UTF-8 문자열이며 빈 문자열도 명시적인 값으로 취급한다. 환경변수로 전달할 수 있도록 NUL 바이트는 오류로 처리한다. 값의 `$NAME` 같은 문자열은 입력한 그대로 저장한다.

같은 값을 다시 등록하거나 이미 비어 있는 영역의 값을 제거하면 `changed: false`로 성공한다. 키 종류는 값을 모두 제거한 뒤에도 유지된다.

`--revision`과 `--request-id`를 쓰는 재시도는 입력 경로 대신 읽은 값으로 동일성을
판단한다. 같은 요청의 `--value`를 파일이나 stdin으로 바꿔도 값이 같으면 replay되며,
끝 개행을 포함해 값이 달라지면 `request_conflict`를 반환한다.

## 민감한 변수: secret / sec

`secret`은 정식 이름이고 `sec`는 같은 동작을 하는 별칭이다. secret은 프로세스에 주입할 수 있지만, 값을 반환하는 조회 명령은 제공하지 않는다.

먼저 [vault 설정](secret-protection.md)을 완료하고 잠긴 키 세션을 해제한다.
해제된 세션은 재사용하므로 명령마다 암호를 입력하지 않는다.

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

secret에는 값을 명령 인자로 넘기는 `--value`를 제공하지 않는다.
수동 잠금이나 암호 세션 만료 뒤에는 명시 해제가 필요하다. 파일·stdin 입력은 secret 값을 에이전트가 직접 읽거나 명령 문자열에 포함할 필요 없이 등록하는 경로다.

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

`changed`는 실제 저장 변경 여부이고, 미리보기에서는 항상 false다. `applicable`은 현재 옵션으로 전체를 적용할 수 있는지 나타낸다. 미리보기는 충돌이 있어도 종료 코드 0과 `applicable: false`를 반환한다. 실제 적용에서 충돌하면 `import_conflict`와 종료 코드 3을 반환하고 `error.details.facts.import_items`에 충돌한 key/kind/action을 제공한다. 미리보기와 실제 적용 사이의 변경은 실제 적용 시 다시 검사한다.

입력은 UTF-8 일반 파일이다. 지원 문법은 다음과 같다.

- `KEY=value`, 선택적인 `export` 접두사, 빈 값, 빈 줄, `#` 주석.
- CRLF 개행과 파일 처음의 UTF-8 BOM.
- 따옴표 없는 값은 양끝 공백을 정리한다. `#`가 값의 처음이거나 공백·탭 뒤에 있으면 주석으로 해석한다. `a#b`는 그대로 저장한다.
- 작은따옴표 안의 값은 리터럴로 저장한다. 큰따옴표 안에서는 `\n`, `\r`, `\t`, `\"`, `\\`, `\$`를 해석하고 나머지 백슬래시 조합은 그대로 보존한다.
- 따옴표로 감싼 여러 줄 값은 개행을 포함해 저장한다. 닫는 따옴표 뒤에는 공백과 주석을 허용한다.
- `$NAME`, `${NAME}`, `$(...)`는 문자열로 저장한다. 파일은 셸 명령으로 실행하지 않는다.

중복 키·잘못된 키·닫히지 않은 따옴표·NUL·잘못된 UTF-8은 `invalid_dotenv`와 종료 코드 2로 처리한다. 문법 오류에는 `error.details.sources`의 dotenv source에 확인된 행과 고정된 원인 설명을 제공한다. column은 아는 경우만 제공한다. 파일 내용과 값은 진단에 포함하지 않는다.

## 재사용할 값 묶음: bundle

profile/env 전체 선택이 기본이다. frontend·backend·deploy처럼 일부 값만 반복해서
사용할 때 bundle에 var/sec 키를 한 번 등록하고 실행마다 그 이름을 선택한다.
같은 ValueID를 공유하므로 값을 복사하거나 명령마다 키 목록을 입력하지 않는다.
먼저 `bundle list`에서 현재 `revision`을 조회한다. 기존 `API_URL`과 `TOKEN`,
이미 만든 `local` env와 설치된 명령을 사용하는 예시다. `REVISION`과 `UUID`는
조회한 revision과 새 요청 ID로, `my-installed-command`는 사용할 프로그램으로 바꾼다.

```sh
devtools bundle list
devtools bundle set backend --key API_URL --key TOKEN --revision REVISION --request-id UUID
devtools values inspect --bundle backend --env local
devtools run --bundle backend --env local -- my-installed-command
```

반복해서 사용하는 선택은 프로젝트의 명령 설정에 저장할 수 있다.
아래 설정을 기존 `devtools.toml`에 추가하면 이후에는 `devtools run api`만 실행한다.
`my-installed-command`는 앞 예시와 같은 설치된 프로그램이다.

```toml
[commands.api]
exec = ["my-installed-command"]
inject = true
env = "local"
bundle = "backend"
```

선택 우선순위는 CLI `--bundle` → command `bundle` → project `bundle` → `all`이다.
env는 선택한 값의 슬롯을 정한다. `none`은 profile 값을 선택하지 않으며 `--env`만으로
해제되지 않는다. `--bundle`만으로 `inject = false`인 이름 명령의 주입을 활성화하지는 않는다.
주입하려면 `inject = true` 또는 명시 `--env`가 필요하다. 부모 환경 상속은 별도이므로
부모에 있던 credential까지 제거하지 않는다.
없는 bundle과 선택 밖 var binding은 오류다. 자동으로 전체 선택으로 바꾸지 않는다.

`bundle set`은 전체 membership을 원자적으로 교체한다. `rename`은 ID를 유지하고
`remove`는 묶음만 제거한다. `import --bundle NAME`은 가져온 키를 같은 transaction에서
묶음에 추가하며 기존 kind/overwrite 규칙을 유지한다. 멤버 변경은 이후 실행에 적용하고
이미 전달한 값을 회수하지 않는다. 수정 충돌은 최신 revision을 조회해 새 요청으로 처리한다.
암호화 profile 전송은 bundle ID와 공유 ValueID membership을 보존하면서 대상 기기의
profile ID·저장 세대·keyset을 새로 만든다. bundle은 동일 사용자 앱의 권한 경계가 아니다.

## 값 메타데이터와 사용처: values

`values inspect --profile NAME [--env ENV] [--bundle all|none|NAME]`은 선택한
입력의 이름·종류·common/env 출처·ID·논리 버전·저장소 세대를 조회한다.
원문과 launch/session 권한은 반환하지 않는다. 키를 새로 해제하거나 세션 수명을
연장하지 않으며, 보호된 저장소가 잠겨 있으면 `verification_pending`을 반환한다.
보호 없는 공개 전용 저장소는 키 없이 조회한다. `unsealed` profile의 값 manifest는
`integrity_verified: false`로 구분한다. 보호된 루트 안에서도 이 값은 false이며,
그 루트의 인증 요구는 별도로 유지한다. 조회 중 legacy 데이터를 이행하거나 OS 인증 창을 띄우지 않는다.

`values uses --dir PATH`는 해당 프로젝트의 설정상 선택(`kind: declared`)과
보존된 실행의 전달 기록(`kind: delivered`)을 구분한다. 실행 전에 준비한 기록은
`prepared`이며 전달이 확인된 기록으로 취급하지 않는다. 명령마다 키를 열거할
필요는 없다. 선언에는 output 정책, 실제 전달 기록에는 적용된 stdout/stderr 모드가
포함된다. 앱이 어떤 값을 읽었는지, 외부 인증에 성공했는지는 관찰하지 않는다.

`command list/inspect`의 `item.selection` 또는 `items[].selection`은 command →
project → all 순서로 정한 선택이다. `command inspect`는 최상위 `values`에 현재
입력을 덧붙인다. `process status`도 최상위 `values`를 반환하고, `process list`와
대시보드의 `/api/processes`는 실행 ID별 `values` 맵을 반환한다. record의
`input_version`은 실제 준비한 payload의 안전한 메타데이터이며 이전 실행에는
없을 수 있다. 과거 실행의 `selection`과 현재 `values.current.spec.selection`은
다를 수 있다. 현재 선택에는 실행 때 명시한 bundle/env override를 계속 적용한다.

비교 상태는 `current`(같은 유효 입력), `pending`(변경 대기), `failed`(만료·누락·
무결성 오류 등), `verification_pending`(잠금 등으로 확인 대기), `unknown`이다.
전달 기록이나 현재 입력을 비교할 수 없으면 변경 없음으로 추측하지 않는다.
`inspect`와 설정 선언은 비교 대상 실행이 없어 `unknown`과
`inspection_only`/`declared_only` 이유를 사용하며, 조회한 입력은 `current` 필드에
별도로 제공한다. 조회 요청이 정상 처리되면 종료 코드 0이며 위 상태는 본문에서
확인한다. 프로젝트 조회 자체의 실패 등은 기존 JSON 오류로 반환한다.

유효 입력은 실제 전달 슬롯의 출처·ID·버전과 profile/storage 세대로 비교한다.
같은 값 재저장, 암호화 키 epoch 변경, 선택 밖 슬롯 변경은 변경 대기를 만들지
않는다. 새 bundle 멤버·유효 override·복원된 저장소 세대는 입력을 바꾼다.
기본 동작은 변경 상태 표시이며 기존 앱을 재시작하지 않는다.

## 프로젝트 명령: command

`command`는 `devtools.toml`에 등록된 명령을 조회·확인·실행한다. `command list`는 이름과 `exec`, `inject`, `env`, `serve`, `selection`, `output`, `values_update` 요약을 이름순으로 반환하고, `command inspect NAME`은 해당 명령의 `bind`, project와 command 요구 사항을 합친 `requirements`, readiness 설정까지 보여준다. 다른 프로젝트를 조회할 때는 두 조회 명령에 `--dir PATH`를 지정할 수 있다.

```sh
devtools command list
devtools command inspect dev
devtools command list --dir ../other-project
```

`devtools run`과 `devtools command run`, `devtools cmd run`은 같은 명령이다.
환경변수 주입을 선택하면 var와 sec를 함께 적용한다.

### 직접 명령 실행

`--` 뒤에 실행 파일과 인자를 지정한다. 실행 위치는 devtools를 호출한 현재 디렉터리다.

```sh
# 공통 값만 주입
devtools command run -- pnpm dev

# 공통 값에 local 값을 덮어써 주입
devtools command run --env local -- pnpm dev

# devtools.toml 없이 실행
devtools command run --profile myapp --env local -- pnpm dev
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
devtools command run dev
devtools command run check
devtools command run lint
```

`exec` 배열의 첫 항목은 실행 파일이고, 나머지는 해당 프로그램에 전달할 인자다. 각 이름은 단일 명령을 실행한다.
새 실행은 기본적으로 non-TTY stdout/stderr를 보호한다. `output`, `stdout`, `stderr` 설정과
명시 raw 예외는 [실행 출력 보호](output-protection.md)를 참고한다.

devtools 0.22.2 이상에서 명령 이름은 1~128자로, `:`·`/`·`@` 같은 특수문자와 공백·한글을 사용할 수 있다.
첫 글자 `-`와 제어문자(NUL, 줄바꿈, 탭 등)는 허용하지 않는다.
이름은 입력한 문자열 그대로 조회하며 정규식이나 namespace로 해석하지 않는다.
TOML에서는 특수문자가 포함된 key를 큰따옴표(`"`)나 작은따옴표(`'`)로 감싼다.
셸에서는 공백이나 셸 문법 문자가 포함된 이름을 따옴표로 감싸거나 escape한다.

```toml
[commands."dev:docs"]
exec = ["pnpm", "run", "docs:dev"]

[commands.'문서 개발']
exec = ["pnpm", "run", "docs:dev"]
```

```sh
devtools run dev:docs
devtools run '문서 개발'
```

`dev:docs`는 devtools가 조회하는 명령 이름이고, `docs:dev`는 `pnpm run`에 전달하는
프로젝트 스크립트 이름이다. 두 이름은 같을 필요가 없다. profile·env·port·proxy 이름은
명령 이름과 별도의 규칙을 사용한다.

에이전트나 CLI가 콜론을 거부하면 `devtools version`과 `devtools schema command run`으로
실제로 실행되는 설치본을 확인한다. 최신 릴리즈를 게시해도 설치된 CLI와 Agent Skill은
각각 업데이트해야 한다. [Agent Skill 업데이트](agent-skill.md#업데이트)를 참고한다.

| 설정 | 동작 |
| --- | --- |
| `inject = false` 또는 생략 | 부모 프로세스의 환경을 상속한다. |
| `inject = true`, `env` 생략 | 부모 환경에 profile 공통 값을 주입한다. |
| `inject = true`, `env` 지정 | 부모 환경에 공통 값과 해당 env 값을 합쳐 주입한다. |

이름 명령은 선택된 `devtools.toml`이 있는 디렉터리에서 실행한다. 프로젝트 하위 폴더에서 `devtools command run dev`를 호출해도 같은 실행 위치를 사용한다. worktree에서는 해당 worktree의 설정 파일과 실행 위치를 사용한다.

### 이름 명령의 env 선택

실행 시 `--env`를 지정하면 명령 설정의 env보다 우선하며, 환경변수 주입을 활성화한다.

```sh
# commands.dev에 설정된 local 사용
devtools command run dev

# staging으로 덮어써 실행
devtools command run --env staging dev

# 주입 설정을 생략한 lint에도 local 값 주입
devtools command run --env local lint
```

명시적인 `--env`는 `inject = false`로 설정된 명령에서도 주입을 활성화한다. `--env`를 생략하면 명령의 `inject`와 `env` 설정을 적용한다.

### 이름 명령에 추가 인자 전달

추가 인자는 `exec` 배열 끝에 순서대로 붙는다. devtools 옵션인 `--profile`, `--env`는
명령 이름 앞에 두면 protocol v5와 v6에서 모두 사용할 수 있다.

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
devtools run --env staging test -- --watch

# 실행 프로그램의 도움말 보기
devtools run test -- --help

# devtools run의 도움말 보기
devtools run --help
```

이 `NAME -- ARG...` 형식은 v0.22.3과 v0.23.0 이상에서 모두 지원한다.
공백을 포함한 인자도 입력한 경계를 유지해 전달한다. `exec`에 셸을 지정했다면
추가 인자의 의미는 해당 셸의 호출 규칙을 따른다.

v0.23.0부터 적용되는 CLI protocol v6에서는 `--`를 생략할 수 있다.
명령 이름 앞에서만 devtools 옵션을 해석하고, 이름 뒤의 인자는 모두 실행 프로그램에
전달한다. 이 규칙은 `run`, `command run`, `cmd run`에만 적용한다.

```sh
# v0.23.0 이상에서 구분자 없이 실행
devtools run --env staging test --watch

# 실행 프로그램에 --help 전달
devtools run test --help
```

| 호출 형식 | 실행 프로그램에 전달하는 인자 |
| --- | --- |
| `devtools command run -- PROGRAM ARG...` | 직접 실행할 프로그램과 인자 |
| `devtools command run NAME -- ARG...` | 등록된 `exec` 뒤에 추가할 인자; protocol v5와 v6에서 지원 |
| `devtools command run NAME ARG...` | 등록된 `exec` 뒤에 추가할 인자; v0.23.0의 protocol v6부터 지원 |

protocol v6에서도 명령 이름 바로 뒤의 `--` 하나는 구분자로 처리한다. 이후의 `--`는
실행 프로그램에 그대로 전달한다. 첫 인자로 `--`를 넘기려면
`devtools run NAME -- -- ARG...`로 쓴다. 직접 프로그램을 실행하는 형식에는
계속 `--`가 필요하다.

protocol v6의 `devtools run test --env staging`은 실행 프로그램에 `--env staging`을
전달한다. devtools의 환경을 선택하려면 `devtools run --env staging test`로 쓴다.
설치한 빌드의 버전과 입력 계약은 `devtools version`, `devtools schema command run`으로
확인한다. 다른 명령의 옵션 위치는 바뀌지 않는다.

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

이 POSIX 셸 예시에서는 앞 명령이 성공해야 다음 명령을 실행한다. 선택한 환경변수는 두 명령에 상속된다.
Windows에서는 설치한 task runner나 명시 PowerShell 검사 스크립트로 같은 순서를 구성한다.

### 환경변수와 프로세스 동작

주입을 선택한 실행의 환경변수 우선순위는 다음과 같다. 오른쪽 값이 같은 키의 왼쪽 값을 덮어쓴다.

```text
부모 프로세스 환경 → profile 공통 값 → 선택한 env 값
```

주입은 실행한 자식 프로세스와 그 자식에만 적용된다. 에이전트의 부모 셸이나 다른 터미널의 환경변수는 바뀌지 않는다. 같은 profile을 공유해도 각 worktree는 실행할 때 env를 따로 선택할 수 있다.

`command run`은 자식 프로세스의 신호와 종료 코드를 보존하고 출력을 JSON으로 감싸지 않는다.
새 실행은 non-TTY stdout·stderr에서 실제 전달 secret을 기본 마스킹한다.
공개 var는 마스킹 패턴이 아니다. 짧은 secret도 마스킹하므로 정상 출력 일부가 가려질 수 있다.
TTY·바이너리 예외와 한도는 [출력 보호](output-protection.md)를 따른다.

실행 파일은 선택한 실행 디렉터리와 최종 환경의 PATH를 기준으로 찾는다.
Unix에서는 `SIGINT`, `SIGTERM`, `SIGHUP`을 자식 프로세스 그룹에 전달하고 3초 후 남은
프로세스를 정리한다. 신호 종료 코드는 `128 + 신호 번호`다. Windows에서는 소유 Job의
CTRL_BREAK·종료 경계를 사용한다. batch 인수 제한과 PowerShell 5.1의 인수 전달 한계는
[Windows 실행 안내](windows.md#storage-and-processes), WSL의 명시 `.exe` 실행은
[WSL 안내](wsl.md#projects-and-storage)를 따른다.

## 출력과 비대화형 사용

`devtools doctor [COMMAND]`로 프로젝트 환경과 필수 조건을 진단한다.
설정의 requirements와 실행 전 검사는 [개발환경 진단](doctor.md)을 따른다.

조회·변경 명령은 항상 JSON을 반환한다. 성공은 stdout, Devtools 오류는 stderr로 분리하고, 오류에는 에이전트가 분기할 수 있는 고정된 코드가 포함된다. 도움말은 text, completion은 artifact, 명령 실행은 passthrough로 구분한다. `devtools schema COMMAND`의 `output_mode`에서 출력 종류를 확인한다. 공통 필드와 이전 버전의 JSON 경로 변경은 [CLI 출력 계약](cli-output.md)을 따른다. 응답의 `schema_version`은 도구가 제공하는 envelope 버전이며 사용자 설정값이 아니다.

값 조회·등록 등 일반 데이터 명령은 대화형 질의를 요구하지 않는다. 명시 stdin은 데이터 입력 경로다.
vault 초기 설정·명시 해제와 대화형 profile 전송은 암호 입력이나 허용한 OS 확인이
필요할 수 있다. 자동화에서는 파일·stdin 입력을 명시하고 기존 해제 세션을 재사용한다.
일반 조회·실행이 암호나 OS 확인을 자동으로 기다린다고 가정하지 않는다.
[로컬 secret 보호](secret-protection.md)와 아래 전송 안내를 따른다.
Windows의 hosts 승격·OS 인증서 신뢰는 별도의 사용자 확인이 필요하다.
[Profile 이동](profiles.md)과 [Windows 권한 안내](windows.md#local-https-and-permissions)를 따른다.

`command run`은 데이터 조회 명령과 출력 계약이 다르다. 실행 준비 중의 실패는 devtools의 오류로 반환하고, 자식 프로세스가 실행된 뒤에는 그 프로세스의 출력과 종료 상태를 전달한다.

명령별 입력·출력 JSON 필드는 `devtools schema COMMAND`에서 확인하고, 기능별 오류 코드는 각 사용 계약을 참고한다. 스키마의 `args`는 위치 인자, `child_args`는 `--` 이후 인자를 뜻한다. `aliases`에는 같은 동작을 하는 별칭이 포함된다.

| 작업 | 성공 응답의 `data` |
| --- | --- |
| var·sec 등록/제거, env 생성/삭제 | `profile`, `changed` |
| var·sec 목록 | `profile`, `items` |
| var 값 조회 | `profile`, `value`, `metadata` |
| env 목록 | `profile`, `items` |
| command 목록 | `profile`, `items` |
| command 상세 조회 | `profile`, `item` |
| project 상세 조회 | `item`, `paths` |
| init | `item`, `changed` |

var·sec 목록의 `items`와 var 조회의 `metadata`에는 `key`, `kind`, `source`, `overrides`가 포함된다. `source`는 `common` 또는 `env`이며, `overrides`는 선택한 env 값이 공통 값을 덮어썼는지 나타낸다.

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

vault 지원 빌드는 secret을 로컬 암호화하고 OS 보호 또는 명시 암호 기반 세션으로
키를 보호한다. 공개 var와 이름·종류 등 메타데이터는 숨기지 않는다. setup·잠금·복구와
기존 평문 자료의 전환은 [로컬 secret 보호](secret-protection.md)를 따른다. 이 기능이
없는 설치 빌드는 이전 평문 저장 동작을 유지하므로 설치 CLI의 schema를 확인한다.

Unix의 `0700` 디렉터리/`0600` 파일과 Windows의 private DACL을 유지한다.
Windows는 현재 사용자·SYSTEM, AppContainer에서는 해당 컨테이너 SID도 허용한다. [Windows 경로 제한](windows.md#storage-and-processes)과 별도
[암호화 백업](backup.md)은 계속 적용된다.

실행은 실제 전달할 값을 old stop 전에 고정한다. vault 잠금은 새 보호 실행을 막지만
이미 실행 중인 앱 환경을 회수하지 않는다. non-TTY/capture의 기본 마스킹은
[정의한 출력 경로](output-protection.md)에 적용된다. 앱 파일·네트워크·임의 변환이나
같은 사용자 권한의 의도적인 접근을 막는 sandbox는 아니다.

실행에 전달된 입력과 변경 대기·자동 교체 상태는 [프로세스 관리](processes.md#값-변경-알림과-자동-반영)를,
응답 필드의 의미는 [관리 API 계약](management-api-contract.md#값-변경-관찰과-자동-교체)을 따른다.
