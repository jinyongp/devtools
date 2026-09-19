# 로컬 reverse proxy MVP 계획

## 상태

MVP 구현은 완료됐으며 CLI, route resolver, HTTP·WebSocket 전달, daemon 수명주기,
port registry 호환성과 설치된 Linux 배포물 시나리오를 자동 검증한다. 이 문서의
범위와 완료 기준은 구현 및 회귀 검증의 기준으로 유지한다.

## 목표

`devtools proxy`는 실행 위치마다 다른 개발 서버 포트를 고정된 `.localhost`
호스트 이름으로 연결한다. 같은 profile의 여러 worktree를 동시에 실행할 때 각
instance가 자신의 주소를 가지며, proxy 재시작 뒤에도 저장된 listener port와
기존 port assignment를 재사용한다.

예시는 다음과 같다.

```text
http://main.app.localhost:20200    -> main instance의 web port
http://feature.app.localhost:20200 -> feature instance의 web port
```

## 범위

- 사용자 단위 HTTP reverse proxy daemon 하나를 관리한다.
- proxy는 IPv4와 IPv6 loopback에서만 요청을 받는다.
- 프로젝트의 `devtools.toml`에 hostname과 대상 port service를 선언한다.
- 현재 port store의 profile, instance, alias, assignment를 읽어 route를 계산한다.
- 일반 HTTP, streaming response, WebSocket upgrade를 전달한다.
- CLI에서 시작, 상태 조회, route 조회, 종료를 제공한다.
- 최초 시작에서 listener port를 선택하고 proxy 전용 상태에 저장한다.

## 비범위

- HTTPS와 로컬 인증서 발급
- 외부 interface bind와 LAN 공개
- hosts 파일 또는 시스템 DNS 변경
- backend process 자동 시작·재시작
- dashboard 화면과 관리 API
- 전역 `config.toml` 생성·조회·수정 기능
- 임의 URL을 대상으로 하는 forward proxy

## 설정 계약

route 선언은 프로젝트와 함께 추적한다.

```toml
profile = "app"

[ports.web]
range = [3000, 3099]

[proxies.app]
host = "${instance.alias}.app.localhost"
port = "web"
```

고정 hostname도 허용한다.

```toml
[proxies.app]
host = "app.localhost"
port = "web"
```

proxy 이름, `host`, `port`는 필수다. `port`는 같은 `devtools.toml`의
`[ports.NAME]`을 참조한다. host는 소문자 ASCII label과 `.localhost` suffix만
허용한다. template은 `${profile}`, `${instance.alias}`, `${proxy}`만 지원한다.
proxy 이름·template 문법·port 선언 참조가 잘못되면 현재 프로젝트 parser 계약에
따라 파일 전체가 `invalid_config`다. proxy daemon은 해당 instance를
`config_invalid` 진단 항목으로 표시하고 다른 instance의 정상 route는 유지한다.
`${instance.alias}`를 사용한 instance에 alias가 없으면 해당 route는
`alias_missing` 상태가 된다.

동일 hostname이 여러 route로 계산되면 모두 `host_conflict` 상태가 된다. proxy는
충돌한 후보 중 하나를 선택하지 않는다. alias 또는 assignment 누락은 route별
상태로 반환한다. 사라진 instance, 읽을 수 없는 설정, profile 또는 canonical root
불일치는 instance 진단 항목으로 반환한다. instance 진단의 hostname, proxy 이름,
service와 target port는 null이다. 진단 하나가 다른 instance의 동작을 막지 않는다.

resolver는 등록된 instance마다 다시 읽은 프로젝트의 canonical root와 profile이
저장된 `directory`, `profile`과 정확히 일치하는지 확인한다. 불일치는
`instance_conflict`로 진단하고 기존 assignment와 새 설정을 결합하지 않는다.

route는 요청마다 현재 instance와 assignment 상태 및 각 instance의 프로젝트
설정을 읽어 계산한다. 프로젝트 설정, alias 또는 port assignment 변경에 daemon
재시작이 필요하지 않다. 등록되지 않은 실행 위치는 route 탐색 대상이 아니다.

## CLI 계약

```sh
devtools proxy start --request-id UUID
devtools proxy start --port 20200 --request-id UUID
devtools proxy status
devtools proxy list
devtools proxy list --profile app
devtools proxy stop --request-id UUID
```

첫 start에서 `--port`를 생략하면 20200을 사용한다. listener port의 단일 원본은 port
store의 proxy 전용 전역 reservation이다. daemon record에 port를 중복 저장하지 않는다.
reservation은 daemon이 중지돼도 유지되고 프로젝트 port allocator의 모든 후보에서
제외된다. 이후 start에서 `--port`를 생략하면 reservation을 재사용한다. proxy가 중지된
상태에서 명시한 새 port로 start하면 port-store lock 안에서 새 port의 assignment,
다른 reservation과 OS 점유를 확인하고 reservation을 한 번의 원자 저장으로 교체한다.
실행 중인 proxy에 다른 port를 요청하면 `proxy_conflict`를 반환한다.

start는 proxy operation lock을 먼저 잡고 port-store mutation 동안 registry lock을
잡는다. reservation commit 뒤 registry lock을 놓고 daemon을 시작한다. port allocator는
같은 registry lock과 reservation을 보므로 commit 뒤 해당 port를 선택할 수 없다. commit
전 실패는 기존 reservation을 유지한다. commit 뒤 daemon spawn, listener bind 또는
readiness가 실패하면 선택한 reservation은 유지하고 daemon record에 실패 상태만
기록한다. 다음 start는 이 reservation을 재사용한다. 따라서 별도 파일 rollback이나
두 저장소 reconciliation이 필요하지 않으며, 중간 crash 뒤에도 reservation이 유일한
선택 기준이다.

start와 stop은 request UUID를 사용한다. 같은 UUID와 같은 입력은 저장된 결과를
반환하고, 같은 UUID의 다른 입력은 `request_conflict`다. 동시 start는 daemon 하나만
만든다. start는 control endpoint 준비와 listener bind가 확인된 뒤 성공한다.

status는 `running`, `state`, `port`, `url`, `started_at`, `reason`을 반환한다. port와
URL은 현재 reservation에서 계산한다. daemon control channel에 접근할 수 없고 lease가
해제됐으면 `interrupted`로 판정한다. list는 daemon 실행 여부와 무관하게 route를
계산하며 hostname, proxy 이름, profile, instance ID, alias, directory, service,
target port, 상태를 반환한다.

stop은 저장된 PID에 직접 signal을 보내지 않는다. 살아 있는 daemon의 인증된
control endpoint로 종료를 요청하고 lease 해제를 확인한다. 이미 중지된 proxy의
stop은 변경 없는 성공이다.

## HTTP 동작과 안전 경계

listener는 `127.0.0.1`과 IPv6 지원 host의 `::1`에 같은 port로 bind한다. IPv6가
지원되면 두 bind가 모두 start 조건이며, 하나라도 실패하면 열린 listener를 닫고
start 전체를 실패시킨다. IPv6 protocol 자체가 지원되지 않는 환경만 IPv4 단독
실행을 허용한다. 요청 Host에서 listener port를 제거한 hostname이 정상 route와
정확히 일치할 때만 전달한다.
미등록 hostname은 404, 충돌하거나 해석할 수 없는 route는 503, 연결할 수 없는
backend는 502를 반환한다.

upstream 주소는 port store에서 얻은 local assignment로만 만든다. proxy 설정은
scheme, hostname, URL을 target으로 받을 수 없다. outbound proxy 환경변수도
사용하지 않는다. 이 제약으로 요청이 loopback 밖의 주소로 전달되는 경로를 막는다.

upstream 요청의 Host는 loopback target으로 바꾸고 원래 host는
`X-Forwarded-Host`에 전달한다. `X-Forwarded-Proto`는 `http`로 설정한다. 표준
hop-by-hop header 처리, WebSocket upgrade, streaming flush는 Go reverse proxy의
검증된 동작을 사용한다. proxy 오류 응답에 프로젝트 경로와 내부 오류 원문을
노출하지 않는다.

## 저장과 프로세스 경계

proxy listener는 프로젝트 port assignment가 아니다. listener port의 단일 원본인
proxy 전용 전역 reservation을 port store에 두고 allocator가 제외한다. daemon record,
control address, token, request receipt와 lease는 사용자 데이터 디렉터리의 `proxy/`
아래에 private permission으로 저장한다. 프로젝트별 route는 별도 복사해 저장하지
않고 현재 port store와 `devtools.toml`에서 계산한다.

현재 port registry version 1에는 reservation이 없다. 새 reader는 v1을 빈 reservation
목록으로 읽고, 첫 변경에서 기존 instance와 assignment를 보존한 version 2를 원자적으로
저장한다. v2는 assignment와 reservation 전체에서 port 번호의 유일성을 검증한다.
read-only 명령은 파일을 upgrade하지 않는다. malformed registry와 assignment에 겹치는
reservation은 `invalid_storage`로 거절한다.

proxy daemon은 전역 singleton이므로 profile·instance·이름 명령을 전제로 하는
`process` record로 표현하지 않는다. `internal/proxy`가 별도 상태 모델을 소유하고,
기존 process supervisor의 authenticated control, lease, detached session, 원자 저장
패턴을 재사용한다.

## 구현 작업

### 1. 공개 계약과 프로젝트 설정

- `docs/proxy-design.md`에 확정된 사용·오류·응답 계약을 작성한다.
- `internal/project`에 proxy 선언과 host template 검증을 추가한다.
- `project inspect`, doctor 등 기존 config 소비자가 proxy 선언을 안전하게 읽게 한다.
- 설정 parser 단위 테스트를 추가한다.

### 2. route resolver

- `internal/proxy`에 port state와 instance별 `devtools.toml`을 읽는 resolver를 만든다.
- template 확장, hostname 검증, profile filter, 결정적 정렬을 구현한다.
- 저장된 instance와 다시 읽은 canonical root·profile의 일치를 확인한다.
- 정상, alias·assignment 누락, 사라진 instance, invalid config, identity 불일치,
  hostname 충돌 상태를 테스트한다.

### 3. HTTP proxy server

- loopback listener와 Host 기반 dispatch를 구현한다.
- upstream을 local port assignment로 제한한다.
- HTTP body·header, streaming, WebSocket, backend 실패, 미등록·충돌 host를
  통합 테스트한다.
- in-process proxy server를 유지한 채 assignment를 변경하고 다음 요청이 새 target만
  사용하는지 테스트한다.
- `HTTP_PROXY`와 `ALL_PROXY`가 non-loopback sentinel을 가리켜도 sentinel 접속 없이
  assignment의 loopback backend만 사용하는지 테스트한다.

### 4. daemon 수명과 저장

- singleton record, private control token, lease와 request receipt를 구현한다.
- port store의 전역 listener reservation과 allocator 제외 규칙을 구현한다.
- v1 registry를 v2로 upgrade해도 기존 instance와 assignment가 유지되는지 테스트한다.
- reservation 생성·교체의 저장 실패, commit 뒤 crash, daemon spawn·record write·readiness
  실패에도 reservation이 유일한 원본으로 남고 다음 start가 같은 값을 쓰는지 테스트한다.
- detached child entrypoint를 추가한다.
- dual-stack listener의 원자적 bind와 rollback을 구현한다.
- start 준비 확인, listener reservation 유지·변경, 동시 시작, idempotent retry,
  stop, supervisor 손실 판정을 테스트한다.
- 두 번째 address bind 실패를 주입해 첫 listener가 닫히고, 두 주소를 다시 bind할 수
  있으며, reservation과 status가 확정된 실패 상태에 일치하는지 테스트한다.

### 5. CLI와 문서

- `proxy start`, `status`, `list`, `stop`을 등록한다.
- JSON schema, text help, shell completion 테스트를 추가한다.
- README와 사용 가이드에 instance alias 설정부터 접속까지의 흐름을 추가한다.
- 공개 devtools Agent Skill에 alias·port 준비, 전역 daemon과 UUID 재시도 규칙을 추가한다.

### 6. 설치 통합 검증

- 설치된 release 시나리오에 두 실행 위치와 두 backend를 만든다.
- 서로 다른 port가 서로 다른 hostname으로 전달되는지 확인한다.
- backend 중단, hostname 충돌, proxy 재시작과 종료를 확인한다.
- daemon start 시각을 유지한 채 assignment를 바꾸고 새 backend로 전환되는지
  확인한다.
- proxy 중지 중에도 listener reservation을 service allocator가 선택하지 않고
  proxy가 같은 port로 재시작하는지 확인한다.
- macOS에서 IPv4·IPv6 loopback bind와 실제 브라우저 접속을 확인한다.

작업 의존성은 1 → 2 → 3 → 4 → 5 → 6이다. daemon lifecycle의 start 준비
확인은 HTTP listener 구현 뒤에 진행한다.

## 완료 기준

- `main`과 `feature` alias를 가진 두 worktree가 같은 proxy daemon을 통해 각자의
  backend 응답을 반환한다.
- proxy 재시작 후 listener port와 route hostname이 유지된다.
- proxy가 중지돼도 listener reservation이 유지되어 service에 재할당되지 않는다.
- port assignment 변경은 daemon 재시작 없이 다음 요청에 반영된다.
- backend 중단이 다른 instance로의 fallback이나 오연결을 만들지 않는다.
- hostname 충돌과 invalid route가 결정적으로 진단된다.
- 등록되지 않은 Host와 loopback 밖 target은 전달되지 않는다.
- outbound proxy 환경변수가 upstream dial 경로를 바꾸지 않는다.
- WebSocket 기반 개발 서버 HMR 연결이 동작한다.
- CLI process가 끝난 뒤 daemon이 유지되고, stop은 해당 daemon만 종료한다.
- 공개 Agent Skill이 proxy 설정 준비와 전역 daemon 운용 규칙을 안내한다.
- 관련 package race test, `go vet ./...`, macOS/Linux build, 설치된 release proxy 시나리오가
  통과한다.

## 검증 명령

```sh
go test -race ./internal/project ./internal/ports ./internal/proxy ./internal/cli
go vet ./...
GOOS=darwin GOARCH=arm64 go build ./cmd/devtools
GOOS=linux GOARCH=amd64 go build ./cmd/devtools
just verify proxies
```

실제 브라우저 WebSocket과 IPv6 확인은 macOS host 검증 기록으로 남긴다.
