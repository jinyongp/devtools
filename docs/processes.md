# 개발 프로세스 관리

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

`devtools process`는 `devtools.toml`의 이름 명령을 백그라운드에서 실행하고 관리한다. 프로젝트 명령의 요구 사항 검사, var/sec/env 주입과 port binding은 `devtools command run`과 같다. agent나 dashboard 세션과 별도로 supervisor가 실행을 관리한다.
Windows에서는 상위 host Job이 종료되면 함께 종료될 수 있다. 실제 PC의 터미널 닫기 동작은
미검증이며 [Windows 안내](windows.md#storage-and-processes)의 수명 경계를 따른다.

먼저 `web` 이름 명령과 필요한 `local` env를 준비한다.
서버 설정은 [준비 확인 예제](process-readiness.md#설정과-사용)를 참고한다.
아래 명령은 작업별 예시이며 매 변경 요청의 `UUID`는 새로 생성한다.

```sh
devtools process start web --request-id UUID
devtools process start web --dir /projects/app --env local --request-id UUID
devtools process list --profile app
devtools process status EXECUTION_ID
devtools process stop EXECUTION_ID --request-id UUID
devtools process restart EXECUTION_ID --request-id UUID
```

start는 `item`에 실행 ID, profile, instance ID, 디렉터리, 명령, env, 시간과 상태를 반환한다. `changed`는 실행 변경 여부, `replayed`는 같은 요청의 재전송 여부다. `running`은 OS 프로세스의 시작을 뜻한다. 선언한 서비스 준비 검사는 `process check EXECUTION_ID`로 확인하고 `process wait EXECUTION_ID --timeout 30s`로 대기한다. 설정·결과·종료 조건은 [프로세스 준비 확인](process-readiness.md)을 따른다.

list는 `data.items`, status는 `data.item`에 실행 정보를 반환한다. start·stop·restart는 `data.item`과 최상위 `data.changed`·`data.replayed`를 반환하며 false도 생략하지 않는다. check·wait의 readiness 보고서와 logs의 `data.content`는 별도 보고서로 유지한다. 공통 형식과 이전 버전의 경로 변경은 [CLI 출력 계약](cli-output.md)을 따른다.

같은 `profile + instance + 명령`이 실행 중이면 같은 실행을 반환한다. env, 값 주입 여부, 로그 설정이 다르면 `process_conflict`다. 실행 정보의 `inject`는 해당 실행을 시작할 때 적용한 값 주입 여부이며, 이전 버전에서 시작한 실행에는 없을 수 있다. 주입 여부를 확인할 수 없는 이전 실행은 env의 명시 선택 여부가 바뀌면 restart가 필요하다. restart는 이전 실행을 종료하고 최신 프로젝트 설정과 값을 적용한 새 실행 ID를 반환하며 `previous_id`로 이전 실행을 연결한다. env를 지정했던 실행은 그 선택을 유지하고, 명령의 기본 env를 따랐던 실행은 현재 기본값을 적용한다. 이전 실행 ID에 대한 stop은 새 실행에 영향을 주지 않는다.

모든 변경은 UUID `--request-id`를 받는다. 같은 UUID와 입력은 기존 결과를 반환하며 다른 입력은 `request_conflict`다. 응답이 불확실하면 같은 요청을 재전송하고 실행 ID로 현재 상태를 확인한다. 작업 준비가 10초를 넘으면 `process_pending`과 함께 같은 요청으로 확인하도록 한다. 시작 기록은 남았지만 supervisor가 실제로 사라진 실행은 같은 start 요청의 재시도에서 `interrupted` / `supervisor_lost`로 종결된다. 이 경우 같은 요청 ID로 새 프로세스를 자동 재실행하지 않으며, 새 실행이 필요하면 새 request ID로 start 또는 restart를 실행한다.

일반 종료는 `exited`, 명시적 종료는 `stopped`, 시작 준비 실패는 `failed`다. 종료 코드와 원인 코드를 메타데이터에 기록한다. 재부팅이나 supervisor 손실은 `interrupted`, 살아 있는 supervisor에 접근할 수 없는 상황은 `unknown`으로 조회한다. 다시 시작하려면 명시적으로 start 또는 restart를 실행한다.

종료는 살아 있는 supervisor의 인증 채널을 통해 요청한다.
Unix에서는 소유한 프로세스 그룹에 TERM을 보내고 3초 후 남은 자식을 정리한다.
같은 그룹의 자식은 직접 자식이 먼저 종료돼도 정리하지만 새 OS 세션으로 빠져나간 명령에는
별도 수명 관리가 필요하다. Windows에서는 생성 시점에 Job으로 묶은 소유 프로세스에
CTRL_BREAK를 요청하고 3초 후 남은 트리를 종료한다. Job에서의 breakaway는 허용하지 않는다.
종료가 불확실하면 상태·실제 포트 점유·진단을 확인한다. 저장된 PID만으로 외부 프로세스를 종료하지 않는다.

## 대상과 요청 ID 선택

`UUID`에는 `uuidgen` 등으로 생성한 새 요청 ID를 넣고, `EXECUTION_ID`에는
start 응답의 `data.item.id`를 넣는다. 응답이 불확실한 재시도에는 원래 요청 ID를 유지한다.

start는 `--dir` 또는 현재 디렉터리의 프로젝트 설정에서 profile을 선택한다.
`process list`는 모든 profile의 실행을 조회하고 `--profile NAME`으로 범위를 좁힌다.
status·check·wait·logs·stop·restart는 실행 ID로 대상을 선택하므로 프로젝트 밖에서도 사용할 수 있다.

## 프로젝트 서버를 함께 관리하기

protocol 12부터 `project up web api --request-id UUID`는 `serve`와 matching proxy 선언이
있는 서버의 도메인·설정된 HTTPS를 자동 준비한다. `--proxy`는 비활성 route까지 다시
준비하며 `--no-proxy` 또는 `--proxy=false`는 준비를 생략한다. 기존 listener를 끄지 않는다.
선택 서버의 포트와 binding을 함께 확정하고 DNS·listener를 준비한 뒤 모든 child를 launch하고
readiness를 기다린다. proxy 모드 restart는 전체 후보를 준비한 다음 기존 실행을 교체한다.
최초 준비와 설정은 [로컬 네트워크 가이드](local-network.md)를 참고한다.

서버 여러 개를 하나의 작업으로 시작하려면 `project up`에 명령 이름을 명시한다.
아래 예시의 `web`과 `api`는 `devtools.toml`에 등록된 이름이다. 모든 명령을 자동으로
시작하지 않으므로 build/test 같은 일회성 명령이 의도치 않게 실행되지 않는다.

```sh
devtools project up web api --request-id UUID
devtools project up web api --dir /projects/app --env local --timeout 30s --request-id UUID
devtools project status
devtools project status api
devtools project logs web
devtools project restart web api --request-id UUID
devtools project restart web --env local --capture-logs --request-id UUID
devtools project down api --request-id UUID
devtools project down --request-id UUID
```

이 project 명령들은 현재 디렉터리 또는 `--dir`에서 찾은 프로젝트의 profile과 실제 루트 경로로
범위를 정한다. 같은 profile을 사용하는 다른 worktree의 실행은 건드리지 않는다.
`status`와 `down`에서 이름을 생략하면 현재 instance의 활성 managed process 전체를
대상으로 하며, 별도 `process start`로 실행한 서버도 포함한다. `project logs COMMAND`는
현재 project instance에서 해당 이름으로 실행 중인 하나의 process를 찾아 보관된 로그를 읽는다. 새 실행은 저장 전에 마스킹한다.
같은 profile의 다른 worktree 실행은 선택하지 않는다. 종료 이력과 종료된 실행의 로그는
`process list`, `process status EXECUTION_ID`, `process logs EXECUTION_ID`로 확인한다.

`up`은 신규로 시작할 명령의 필수 조건과 port/binding을 batch 변경 전에 모두 검사한 뒤 입력 순서대로 시작한다.
같은 프로젝트에서 이미 실행 중인 singleton은 cold-start 검사를 요구하지 않고 기존 process 계층이 재사용 여부와
env·로그 설정 충돌을 판정한다. 실제 새 process를 만드는 경우에는 process 생성 직전 현재 project 설정과 값·port 상태로
같은 preflight를 다시 수행한다. 네트워크 batch에서 재사용 대상으로 고정한 singleton이
준비 전에 종료되면 새 UUID로 다시 요청해야 한다.
`ready` probe가 선언된 명령은 준비 완료까지 기다리고, probe가 없으면 프로세스
시작으로 완료한다. `--timeout`은 명령별 readiness 대기 시간이며 기본 30초, 최대 10분이다. `--env`와
`--capture-logs`는 선택한 모든 명령에 적용된다. 같은 설정으로 이미 실행 중이면 기존 실행을 재사용하고, env나 로그
설정이 충돌하면 `process_conflict`로 보고한다. 명령 이름을 중복해서 입력하면 오류다.

성공 응답은 `data.items`에 명령별 `status`, `changed`, 실행 정보 `item`을 반환하며,
전체 `data.changed`·`data.replayed`도 제공한다. 명령별 status는 `ready`, `running`,
`stopped`, `unchanged`이고, 미완료 항목은 `pending` 또는 `failed`와 `condition`을 갖는다.
일부 명령이 실패하면 stderr의 `project_operation_failed` 오류에
protocol 10에서는 `error.details.facts.outcomes`로 성공·실패·미완료 항목의 이름·상태·ID를,
`causes`로 하위 원인을 반환한다. 이전 빌드는 `error.details.items`를 사용한다. 이미 시작한 서버는
자동으로 종료하지 않는다.

부분 실패나 응답 유실 뒤에는 같은 요청 ID와 같은 입력으로 재시도한다. 완료한
항목은 반복하지 않고 미완료 항목만 이어간다. 시작된 서버의 readiness가 아직
충족되지 않았으면 해당 실행의 준비 검사를 이어간다. 실제 전달할 입력을 고정한 새 실행이
확정 실패하면 같은 UUID에서 새 입력으로 다시 시작하지 않는다. 새 시도에는 새 요청 ID를 쓴다. 변경된 입력에 같은 요청 ID를 사용하면 `request_conflict`다.
재시도 결과의 `replayed: true`는 기존 작업을 재개하거나 재현했다는 뜻이다.
현재 서버 상태가 필요하면 저장된 작업 결과 대신 `project status`를 조회한다.

`restart`도 최초 호출 시 현재 project instance에서 선택한 command별 active execution ID를
고정한다. 선택한 command가 실행 중이 아니면 새 process를 시작하지 않고 해당 항목을
`process_not_found` 조건으로 보고한다. 같은 요청을 재전송해도 그 뒤 새로 실행된 process를
대상으로 바꾸지 않는다. 각 restart는 기존 `process restart`와 같이 최신 project 설정과 값을
사용한다. 기존 실행을 종료하기 전에 설정·값·실행 파일을 확인하고 실제 입력을 고정해 특정
launcher에 전달한다. 출력 보호 초기화와 실행 승인도 기존 실행을 멈추기 전에 완료한다. 이 단계에서 실패하면 기존 실행은 계속 유지된다. 실제 port
availability처럼 stop 이후에만 확정할 수 있는 조건은 종료 뒤 새 실행 직전에 다시 검사하며, 이
두 번째 검사에서 실패하면 이미 완료된 stop을 되돌리지 않는다. `--env`와 `--capture-logs`를
생략하면 기존 execution의 선택을 유지하고, 명시하면 새 execution에 적용한다. 일부 command만
실패하면 성공한 restart는 유지하며 같은 요청 ID의 재시도는 미완료 command만 이어간다.
readiness가 선언된 command는 `--timeout` 범위에서 다시 준비 완료까지 확인한다.

`down`은 최초 호출 시 정한 실행 ID만 종료한다. 같은 요청의 재전송은 그 뒤 새로
시작된 서버까지 종료하지 않는다. 새로운 종료 작업에는 새 요청 ID를 사용한다.
Profile 데이터를 다른 환경으로 가져오는 절차는 [Profile 관리](profiles.md)를 참고한다.

## 캡처 로그

기본 출력은 버리고 상태와 종료 원인을 보관한다. 출력 보관이 필요한 실행에는 `--capture-logs`를 명시한다.

```sh
devtools process start web --capture-logs --request-id UUID
devtools process logs EXECUTION_ID
```

stdout과 stderr는 합쳐서 마지막 1 MiB까지 보관한다. 새 실행은 실제 전달 secret을 stdout/stderr별로 마스킹한 뒤 저장하며, logs는 JSON의 `content`로 저장된 결과를 반환한다. 설정의 raw 예외도 캡처 저장을 우회하지 않는다. [출력 보호의 범위와 한도](output-protection.md)를 확인한다. 과거 로그는 소급해 보호하지 않으며 알 수 없는 token이나 앱이 만든 파일은 필터 범위 밖이다. 종료 후 7일이 지나면 `logs_expired`를 반환하며 정리 대상이 된다. restart는 로그 설정을 이어받는다.

실행 메타데이터와 인증 정보, 선택적으로 수집한 로그는 전역 데이터의 `processes` 아래에 개인 권한으로 저장한다. dashboard의 Processes 화면은 명령·상태·env·프로젝트를 실행별 한 행으로 보여준다. 표 머리글에서 검색과 상태 필터를 적용하고 이름 명령을 시작·종료·재시작할 수 있다. Details를 펼치면 전체 경로·실행 ID·종료 원인을 확인한다. 캡처 로그는 별도의 확인 동작으로 읽는다. Projects & worktrees를 펼치면 프로젝트별 명령과 포트를 조회할 수 있다.

## 값 변경 알림과 자동 반영

관리 서버는 선택한 var/sec의 실제 전달 버전이 바뀌면 `value_watch`에 반영
대기 상태를 표시한다. 변경 알림이 유실돼도 주기적으로 확인한다. 잠기면
`verification_pending`으로 표시하고 기존 앱은 계속 실행한다. 상태 확인은
키를 자동으로 해제하거나 세션 만료 시간을 연장하지 않는다.

자동으로 교체할 서버에만 다음 설정을 추가한다. 기본값은 `notify`다.

```toml
[commands.api]
exec = ["pnpm", "run", "dev:api"]
inject = true
env = "local"
values_update = "restart"
```

`process start`나 `project up`으로 실행한 관리 서버에 적용한다. `command run`과
일회성으로 종료된 실행은 반복하지 않는다. 선택하지 않은 값, env override에
가려진 common 값, 같은 값 재등록, 저장 키 재암호화만으로 교체하지 않는다.
변경은 500ms 동안 합치고, 변경이 계속되면 최대 5초 후 현재 목표를 확인한다.

자동 교체는 기존 실행 ID와 입력 목표를 고정한다. 목표당 한 번 시도하며,
기존 서버를 중지하기 전에 실제 새 입력과 launcher를 준비한다. 준비 중 값이
바뀌거나 잠겼으면 기존 서버를 유지한다. 시작 뒤 readiness에 실패하면 해당
상태를 보고하고 저장값을 되돌리지 않는다. 다른 서버의 성공한 실행도 유지한다.

`value_apply`는 요청·성공·실패·일시 정지 상태와 교체 실행 ID를 제공한다.
실패하면 새 값이 등록돼도 자동 시도를 멈춘다. 성공한 교체도 30초 동안 세 번을
넘으면 멈춘다. 원인을 해결한 뒤 현재 실행에 수동 `process restart` 또는
`project restart`를 실행하면 새 시도를 시작한다. 응답이나 담당 프로세스가
사라져 결과를 확인할 수 없을 때도 자동 반복하지 않는다. 저장 성공, 실제 전달,
readiness와 외부 서비스 인증 성공은 서로 다르다.

진단할 때는 `values uses`로 선언과 실제 전달을 구분하고, `process status EXECUTION_ID`의
최상위 `values`, `item.value_watch`, `item.value_apply`를 함께 확인한다. `notify`에서
`pending`은 변경 알림이며 실패가 아니다. 잠금 때문에 `verification_pending`이면
기존 앱의 환경이 회수되거나 값이 반영됐다는 뜻도 아니다. 필요한 명시 해제 뒤 상태를
다시 조회한다. 이미 실패·일시 정지한 자동 시도가 저절로 재개된다고 가정하지 않는다.

준비 오류는 실행 파일·선택·권한·입력 문제를 해결한 뒤 수동으로 재시도한다. 이전 앱을
중지한 뒤 실패했다면 현재 활성 실행과 `next_id`를 먼저 확인한다. 결과가 `unknown`이면
PID만 보고 종료하거나 새 UUID를 반복 발급해 중복 실행하지 않는다. 저장값을 자동으로
되돌리는 대신, 운영자가 현재 상태를 확인하고 다음 start/restart를 명시적으로 선택한다.
