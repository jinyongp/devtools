# 개발 프로세스 관리

`devtools process`는 `devtools.toml`의 이름 명령을 백그라운드에서 실행하고 관리한다. 프로젝트 명령의 요구 사항 검사, var/sec/env 주입과 port binding은 `devtools run`과 같다. agent나 dashboard 세션을 종료해도 실행은 유지된다.

```sh
devtools process start web --request-id UUID
devtools process start web --dir /projects/app --env local --request-id UUID
devtools process list --profile app
devtools process status EXECUTION_ID
devtools process stop EXECUTION_ID --request-id UUID
devtools process restart EXECUTION_ID --request-id UUID
```

start는 `item`에 실행 ID, profile, instance ID, 디렉터리, 명령, env, 시간과 상태를 반환한다. `changed`는 실행 변경 여부, `replayed`는 같은 요청의 재전송 여부다. `running`은 OS 프로세스의 시작을 뜻한다. 선언한 서비스 준비 검사는 `process check ID`로 확인하고 `process wait ID --timeout 30s`로 대기한다. 설정·결과·종료 조건은 [프로세스 준비 확인](process-readiness.md)을 따른다.

같은 `profile + instance + 명령`이 실행 중이면 같은 실행을 반환한다. env나 로그 설정이 다르면 `process_conflict`다. restart는 이전 실행을 종료하고 최신 프로젝트 설정과 값을 적용한 새 실행 ID를 반환하며 `previous_id`로 이전 실행을 연결한다. env를 지정했던 실행은 그 선택을 유지하고, 명령의 기본 env를 따랐던 실행은 현재 기본값을 적용한다. 이전 실행 ID에 대한 stop은 새 실행에 영향을 주지 않는다.

모든 변경은 UUID `--request-id`를 받는다. 같은 UUID와 입력은 기존 결과를 반환하며 다른 입력은 `request_conflict`다. 응답이 불확실하면 같은 요청을 재전송하고 실행 ID로 현재 상태를 확인한다. 작업 준비가 10초를 넘으면 `process_pending`과 함께 같은 요청으로 확인하도록 한다.

일반 종료는 `exited`, 명시적 종료는 `stopped`, 시작 준비 실패는 `failed`다. 종료 코드와 원인 코드를 메타데이터에 기록한다. 재부팅이나 supervisor 손실은 `interrupted`, 살아 있는 supervisor에 접근할 수 없는 상황은 `unknown`으로 조회한다. 다시 시작하려면 명시적으로 start 또는 restart를 실행한다.

종료는 살아 있는 supervisor의 인증 채널을 통해 요청한다. supervisor는 자신이 소유한 프로세스 그룹에 TERM을 보내고 3초 후 남은 자식을 정리한다. 같은 그룹의 자식 프로세스는 직접 자식이 먼저 종료된 경우에도 정리한다. 자체적으로 새 OS 세션을 만드는 명령은 별도 수명 관리가 필요하다. supervisor가 강제 종료된 경우 잔여 프로세스는 실제 port 점유와 진단 결과로 확인한다. 저장된 PID를 사용해 다른 프로세스를 종료하지 않는다.

## 대상과 요청 ID 선택

위 예시의 `web`은 프로젝트에 선언한 이름 명령이고, `local`은 미리 생성한 env다.
완전한 서버 설정 예시는 [준비 확인 가이드](process-readiness.md#설정과-사용)를 참고한다.
`UUID`에는 `uuidgen` 등으로 생성한 새 요청 ID를 넣고, `EXECUTION_ID`에는
start 응답의 `data.item.id`를 넣는다. 응답이 불확실한 재시도에는 원래 요청 ID를 유지한다.

start는 `--dir` 또는 현재 디렉터리의 프로젝트 설정에서 profile을 선택한다.
`process list`는 모든 profile의 실행을 조회하고 `--profile NAME`으로 범위를 좁힌다.
status·check·wait·logs·stop·restart는 실행 ID로 대상을 선택하므로 프로젝트 밖에서도 사용할 수 있다.

## 원문 로그

기본 출력은 버리고 상태와 종료 원인을 보관한다. 원문이 필요한 실행에는 `--capture-logs`를 명시한다.

```sh
devtools process start web --capture-logs --request-id UUID
devtools process logs EXECUTION_ID
```

stdout과 stderr는 합쳐서 마지막 1 MiB까지 보관한다. logs는 JSON의 `content`로 원문을 반환한다. 출력에는 명령이 출력한 secret이 포함될 수 있으므로 일반 상태 조회와 분리해 사용한다. 종료 후 7일이 지나면 `logs_expired`를 반환하며 정리 대상이 된다. restart는 로그 설정을 이어받는다.

실행 메타데이터와 인증 정보, 선택적으로 수집한 로그는 전역 데이터의 `processes` 아래에 개인 권한으로 저장한다. dashboard의 Processes 화면에서 profile별 실행을 조회하고 이름 명령 시작·종료·재시작을 할 수 있다. 원문 로그는 별도의 확인 동작으로 읽는다.
