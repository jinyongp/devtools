# 로컬 reverse proxy 계약

이 문서는 `devtools proxy` MVP의 프로젝트 설정, route 해석, CLI, daemon 수명과
HTTP 전달 계약을 정의한다. 구현 순서와 검증 범위는 [proxy 구현 계획](proxy-plan.md)을
따른다.

## 사용 목적

같은 profile의 여러 worktree는 실행 위치마다 다른 개발 서버 port를 할당받는다.
proxy는 이 port를 instance별 `.localhost` hostname에 연결한다.

```text
http://main.app.localhost:20200
http://feature.app.localhost:20200
```

proxy는 사용자 단위 daemon 하나다. 프로젝트 process를 시작하지 않으며 현재 port
assignment가 가리키는 loopback server로 요청만 전달한다.

## 프로젝트 설정

route는 프로젝트의 `devtools.toml`에 선언한다.

```toml
profile = "app"

[ports.web]
range = [3000, 3099]

[proxies.app]
host = "${instance.alias}.app.localhost"
port = "web"
```

`[proxies.NAME]`의 이름, `host`, `port`는 필수다. `port`는 같은 파일의
`[ports.NAME]`을 참조한다. 고정 hostname도 사용할 수 있다.

```toml
[proxies.app]
host = "app.localhost"
port = "web"
```

host template은 다음 참조만 지원한다.

| 참조 | 값 |
| --- | --- |
| `${profile}` | instance가 등록된 profile |
| `${instance.alias}` | `devtools instance name`으로 지정한 alias |
| `${proxy}` | `[proxies.NAME]`의 NAME |

host는 소문자 ASCII hostname이며 `.localhost`로 끝난다. 각 label은 영문 소문자,
숫자와 내부 `-`만 사용한다. proxy 이름, template 문법 또는 port 선언 참조가
잘못되면 프로젝트 설정 전체가 `invalid_config`다.

template을 사용해 계산한 최종 hostname도 같은 규칙을 만족해야 한다. alias가 없거나
계산 결과가 유효하지 않으면 해당 route는 전달되지 않는다.

## instance와 route 해석

route 탐색 대상은 port store에 등록된 instance다. resolver는 각 instance의 현재
디렉터리에서 `devtools.toml`을 다시 읽고 다음 identity를 확인한다.

- 설정의 canonical root가 등록된 directory와 같다.
- 설정의 profile이 등록된 profile과 같다.
- route가 참조한 service에 저장된 port assignment가 있다.

root 또는 profile이 다르면 기존 assignment와 새 설정을 결합하지 않는다. 프로젝트
설정, instance alias 또는 port assignment 변경은 다음 요청의 route 계산에 반영되며
daemon 재시작을 요구하지 않는다.

`proxy list`는 정상 route와 진단 항목을 결정적인 순서로 반환한다. 각 항목은 `kind`,
`host`, `proxy`, `profile`, `instance_id`, `alias`, `directory`, `service`,
`target_port`, `status`를 가진다. alias 또는 assignment 누락은 route 진단이다.
디렉터리 부재, 설정 읽기 실패와 identity 불일치는 hostname, proxy 이름, service와
target port가 null인 instance 진단이다.

| kind | status | 의미 |
| --- | --- | --- |
| route | `ready` | 전달 가능한 route |
| route | `alias_missing` | template에 필요한 instance alias 없음 |
| route | `assignment_missing` | 대상 service의 저장된 port assignment 없음 |
| route | `host_invalid` | template 확장 결과가 hostname 규칙을 만족하지 않음 |
| route | `host_conflict` | 둘 이상의 route가 같은 hostname을 사용함 |
| instance | `location_missing` | 등록된 directory가 사라짐 |
| instance | `config_invalid` | 설정 없음, TOML 오류 또는 strict config 검증 실패 |
| instance | `config_unavailable` | directory 또는 설정을 읽을 수 없음 |
| instance | `instance_conflict` | 저장된 profile·directory와 현재 project identity 불일치 |

동일 hostname이 여러 route로 계산되면 관련 route 전체가 `host_conflict`다. 충돌한
후보 중 하나를 선택하지 않는다. 한 instance의 진단은 다른 instance의 정상 route를
막지 않는다.

## CLI

```sh
devtools proxy start --request-id UUID
devtools proxy start --port 20200 --request-id UUID
devtools proxy status
devtools proxy list
devtools proxy list --profile app
devtools proxy stop --request-id UUID
```

첫 start에서 `--port`를 생략하면 20200을 사용한다. listener port의 단일 원본은 port
store의 proxy 전용 전역 reservation이다. reservation은 daemon을 중지해도 유지되고
프로젝트 port allocator의 후보에서 제외된다.

중지 상태에서 `start --port NEW_PORT`를 실행하면 assignment, 다른 reservation과 OS
점유를 검사한 뒤 reservation을 원자적으로 교체한다. 실행 중인 daemon과 다른 port를
요청하면 `proxy_conflict`다.

start와 stop은 request UUID를 사용한다. 같은 UUID와 입력을 재전송하면 기존 결과를
반환한다. 같은 UUID에 다른 입력을 사용하면 `request_conflict`다. 동시 start는
daemon 하나만 만든다.

status는 `data.item`에 `running`, `state`, `started_at`, `reason`과 reservation에서
계산한 `port`, `url`을 반환한다. list는 `data.items`에 route·instance 진단을 반환한다.
start·stop은 `data.item`의 상태와 별도로 `data.changed`·`data.replayed`를 제공하며,
false도 생략하지 않는다. 재시도에서는 최초 changed를 유지한다.
전체 출력 종류와 마이그레이션은 [CLI 출력 계약](cli-output.md)을 따른다.

control endpoint에 접근할 수 없고 lease가 해제됐으면 상태는 `interrupted`다. 이미
중지된 proxy의 stop은 변경 없는 성공이다.

## listener reservation과 저장 호환성

port registry version 2는 project assignment와 별도로 proxy listener reservation을
저장한다. assignment와 reservation 전체에서 port 번호는 유일하다.

version 1 registry는 빈 reservation 목록으로 읽는다. 첫 변경에서 기존 instance와
assignment를 보존한 version 2를 원자적으로 저장한다. read-only 명령은 저장 형식을
바꾸지 않는다. malformed registry와 assignment에 겹치는 reservation은
`invalid_storage`다.

start는 proxy operation lock을 먼저 잡고 reservation 변경 중 port registry lock을
잡는다. reservation commit 뒤 daemon을 시작한다. commit 뒤 spawn, bind 또는 readiness가
실패해도 reservation은 선택한 listener port의 단일 원본으로 유지된다. 다음 start는
같은 port를 재사용한다.

daemon record, control address, token, request receipt와 lease는 사용자 데이터
디렉터리의 `proxy/` 아래에 private permission으로 저장한다. daemon record는 listener
port를 중복 저장하지 않는다. route 선언도 복사해 저장하지 않는다.

## HTTP 전달

listener는 `127.0.0.1`과 IPv6를 지원하는 host의 `::1`에 같은 port로 bind한다. IPv6
지원 host에서는 두 bind가 하나의 start 조건이다. 하나라도 실패하면 열린 listener를
닫고 start를 실패시킨다. IPv6 protocol을 지원하지 않는 환경만 IPv4 단독 실행을
허용한다.

요청 Host에서 listener port를 제거한 hostname이 정상 route와 정확히 일치할 때만
전달한다.

| 조건 | HTTP 상태 |
| --- | --- |
| 등록되지 않은 hostname | 404 |
| 충돌하거나 해석할 수 없는 route | 503 |
| backend 연결 실패 | 502 |

upstream은 port store의 local assignment로만 구성한다. 설정에서 scheme, target
hostname 또는 URL을 지정할 수 없다. transport는 `HTTP_PROXY`, `HTTPS_PROXY`,
`ALL_PROXY`를 사용하지 않는다.

upstream Host는 loopback target으로 바꾸고 원래 host는 `X-Forwarded-Host`로
전달한다. `X-Forwarded-Proto`는 `http`다. 일반 HTTP body와 header, streaming
response, WebSocket upgrade를 지원한다. 오류 응답에는 프로젝트 경로와 내부 오류
원문을 포함하지 않는다.

## MVP 범위 밖

- HTTPS와 인증서 발급
- 외부 interface와 LAN bind
- hosts 파일과 시스템 DNS 변경
- backend process 자동 시작·재시작
- dashboard와 관리 API
- 전역 `config.toml` 관리 명령
- 임의 URL을 대상으로 하는 forward proxy
