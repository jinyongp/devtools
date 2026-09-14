# 로컬 reverse proxy

`devtools proxy`는 여러 프로젝트와 worktree의 개발 서버를 기억하기 쉬운
`.localhost` 주소로 연결한다. 사용자 계정에서 daemon 하나만 실행되며 프로젝트
프로세스는 직접 시작하지 않는다.

## 프로젝트 설정

`devtools.toml`에서 proxy hostname과 연결할 port 이름을 선언한다.

```toml
profile = "shop"

[ports.web]
range = [30000, 30999]

[proxies.app]
host = "${instance.alias}.${profile}.localhost"
port = "web"
```

`instance.alias`는 같은 profile을 쓰는 실행 위치를 구분하는 사용자 지정 이름이다.
예를 들어 기본 checkout에는 `main`, worktree에는 `feature-cart`를 지정할 수 있다.

```sh
devtools instance name main
devtools port allocate web
```

위 설정은 `main.shop.localhost`를 현재 instance의 `web` port에 연결한다. 사용할 수
있는 hostname 치환값은 `${profile}`, `${instance.alias}`, `${proxy}`뿐이다. 결과는
소문자 ASCII `.localhost` hostname이어야 한다.

## 시작과 확인

변경 명령에는 재시도 안전성을 위한 UUID가 필요하다.

```sh
devtools proxy start --request-id "$(uuidgen)"
devtools proxy status
devtools proxy list
devtools proxy list --profile shop
```

첫 시작의 기본 listener port는 `20200`이다. 브라우저에서는 다음 주소를 연다.

```text
http://main.shop.localhost:20200
```

listener port를 바꾸려면 daemon을 중지한 뒤 새 port로 시작한다.

```sh
devtools proxy stop --request-id "$(uuidgen)"
devtools proxy start --port 20201 --request-id "$(uuidgen)"
```

선택한 listener port는 proxy가 중지돼도 유지되며 일반 프로젝트 port로 할당되지
않는다. 프로젝트 설정, instance alias, port assignment 변경은 daemon 재시작 없이
다음 요청부터 반영된다.

`proxy list`는 정상 route뿐 아니라 `alias_missing`, `assignment_missing`,
`host_conflict`, `config_invalid` 같은 instance별 진단도 반환한다. 미등록 hostname은
404, 사용할 수 없는 route는 503, 연결할 수 없는 backend는 502다.

에이전트가 devtools를 사용한다면 현재 바이너리의 `devtools skill`을 설치한다.
번들 스킬은 proxy가 사용자 전역 daemon이라는 점, alias·port 준비 순서와
request UUID 재시도 규칙을 함께 안내한다. 설치와 업데이트 방법은
[에이전트 스킬 설치](agent-skill.md)를 참고한다.

## 전역 config.toml

기존 전역 `config.toml`은 프로젝트 port의 기본 할당 범위만 읽는다. proxy route와
listener port의 원본은 각각 프로젝트 `devtools.toml`과 내부 port registry이며,
MVP에는 전역 설정 관리 명령이 없다.

- macOS: `~/Library/Application Support/devtools/config.toml`
- Linux/WSL: `$XDG_CONFIG_HOME/devtools/config.toml` 또는
  `~/.config/devtools/config.toml`

```toml
[port]
range = [10000, 19999]
```

daemon record, control token과 receipt는 사용자 data 디렉터리의 `proxy/` 아래에
사용자 전용 권한으로 저장된다. HTTP listener와 backend 연결은 loopback으로만
제한된다. HTTPS, 인증서 발급, dashboard, 임의 URL forward proxy는 현재 범위에
포함되지 않는다.

상태 필드, 저장 호환성, route 진단과 오류의 전체 공개 계약은
[reverse proxy 계약](proxy-design.md)을 참고한다.
