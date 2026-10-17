# 로컬 네트워크와 공유

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

devtools는 서버 실행, 포트, 로컬 도메인, HTTPS와 공유를 함께 관리한다.
macOS·Linux와 Windows 네이티브에서 실행하며 WSL은 Linux 환경으로 동작한다.
Windows의 지원/검증 범위는 [Windows 안내](windows.md), 별도 client trust는 [WSL 안내](wsl.md)를 따른다.
사용자 계정의 daemon 하나가 여러 named listener를 관리한다.
아래 `UUID`는 새 요청 UUID로 바꾼다. 응답을 잃었으면 같은 입력과 UUID로 재시도한다.

## 프로젝트 설정과 일상 실행

아래 예시는 루트의 frontend와 `api` 하위 프로젝트에 각각 `pnpm dev`가 준비되어 있고,
두 서버가 환경변수 `PORT`를 읽는 프로젝트를 기준으로 한다. 실행 도구와 서버는 devtools가 설치하지 않는다.
`shop.test` custom host는 OS hosts 변경 권한이 필요하다. OS 변경 없이 먼저 확인하려면
api의 host를 `api.${instance.alias}.${profile}.localhost`로 바꾸고 고포트 listener를 사용한다.
WSL에서 Windows 브라우저로 접속할 때는 별도의 DNS·신뢰 경계를 확인한다.

```toml
profile = "shop"
env_files = [".env.local", ".env"]

[ports.web]
range = [30000, 30999]
[ports.api]
port = "${API_PORT:-31000}"
strict = true

[proxies.web]
host = "${instance.alias}.${profile}.localhost"
port = "web"
[proxies.api]
host = "api.${instance.alias}.${DEV_DOMAIN:-shop.test}"
port = "api"

[commands.web]
exec = ["pnpm", "dev"]
inject = false
serve = ["web"]
[commands.web.bind]
PORT = { port = "web" }
API_URL = { port = "api", format = "url" }
PUBLIC_API_URL = { proxy = "api" }

[commands.api]
exec = ["pnpm", "--dir", "api", "dev"]
inject = false
serve = ["api"]
[commands.api.bind]
PORT = { port = "api" }
```

```sh
devtools instance name main
devtools project up web api --proxy --request-id UUID
devtools command env web
devtools proxy env api
devtools project status
devtools project restart web api --request-id UUID
devtools project down --request-id UUID
```

`--proxy`는 선택한 서버의 포트를 함께 할당하고 binding·DNS·listener를 준비한 뒤
모든 서버를 시작하고 readiness를 기다린다. web이 아직 시작하지 않은 api를 참조해도
같은 batch의 포트를 사용한다. 선택하지 않은 서버는 자동으로 실행하지 않는다.
재시작은 실행 기록의 proxy 모드를 유지한다. 준비 실패 시 기존 서버를 보존하며,
일부 시작 실패 시 성공한 다른 서버는 유지한다.
foreground는 `devtools run --proxy web`, 단일 managed 서버는
`devtools process start web --proxy --request-id UUID`를 사용한다.
`run`의 devtools 옵션은 command 이름 앞에 둔다. 이름 뒤 옵션은 child 인자다.
다른 worktree는 `instance name feature`처럼 별도 alias를 지정한다.
프로젝트 lifecycle은 현재 실제 프로젝트 디렉터리의 서버만 선택한다.
`project down`은 backend를 종료한다. route 비활성화는 `proxy down NAME --request-id UUID`다.

## listener와 HTTPS

listener는 HTTP/HTTPS 주소 묶음의 이름이다. 프로젝트 `listener` 생략은 이름이
`default`인 묶음을 선택한다. `listener = "default"`를 쓸 필요는 없다.
다른 이름은 먼저 `proxy configure --listener preview`처럼 등록한다.
초기 HTTP 주소는 loopback `:20200`이다. HTTPS는 명시적으로 설정한다.

```sh
devtools proxy configure --http-address 127.0.0.1:18080 --https-address 127.0.0.1:18443 --request-id UUID
devtools proxy configure --listener preview --http-address 127.0.0.1:18081 --https-address 127.0.0.1:18444 --request-id UUID
devtools proxy status --all
devtools proxy ca export --output ./devtools-ca.pem
curl --noproxy '*' --cacert ./devtools-ca.pem --resolve main.shop.localhost:18443:127.0.0.1 https://main.shop.localhost:18443/
devtools proxy stop --listener default --request-id UUID
devtools proxy start --listener preview --request-id UUID
```

configure는 선택 listener가 중지된 상태에서 실행한다. 프로젝트 route에서
`listener = "preview"`로 묶음을 선택한다. HTTPS 시작 시 사용자 전용 CA를 생성한다.
OS 신뢰 등록은 명시 `devtools proxy trust --request-id UUID`로 수행한다.
신뢰 등록 전에도 public CA를 지정한 client는 HTTPS를 사용할 수 있다.
HTTP는 선택 HTTPS 주소로 경로와 query를 유지해 redirect한다.
HTTPS는 활성 route의 SNI hostname만 허용한다.

Linux/WSL에서 80/443이 필요하면 최초에 `devtools proxy setup --request-id UUID`를 실행하고
listener 주소를 `127.0.0.1:80`, `127.0.0.1:443`으로 설정한다.
Linux setup은 설치 실행 파일에 최소 bind capability를 적용한다.
macOS에서 권한 없이 80/443을 사용하려면 `--http-address 0.0.0.0:80
--https-address 0.0.0.0:443`을 명시한다. wildcard socket에서도 공유하지 않은 route는
loopback 요청만 허용한다. 낮은 포트의 `127.0.0.1` 등 특정 주소 bind는 권한이 필요하며
자동으로 wildcard로 바꾸지 않는다. loopback socket이 필요하면 고포트를 사용한다.
`proxy setup --check`는 상태만 읽으며 macOS setup은 bind 권한을 추가하지 않는다.
setup 의도와 실제 bind 권한은 별도로 검사한다. standalone 업데이트는 설정 의도를
보존하며 외부 package 교체로 capability가 사라지면 diagnostics가 복구를 안내한다.

## 이름 해석과 환경 변수

`dns` 생략은 `auto`다. `.localhost`는 기존처럼 hosts 파일을 수정하지 않는다.
custom host는 `proxy up` 또는 `--proxy` 시작 안에서 devtools 소유 hosts mapping을
준비한다. 별도 hosts 명령은 일상 실행에 필요하지 않다.
wildcard listener의 로컬 hosts mapping은 IPv4의 `127.0.0.1`, IPv6의 `::1`을 사용하며
`0.0.0.0`이나 `::`를 hosts에 기록하지 않는다.
변경이 필요하면 initiating CLI가 내용을 보여 주고 필요한 OS 인증을 요청한다.
Unix 비대화형 실행은 사전 승인된 sudo 권한으로 `sudo -n`을 사용한다.
Windows hosts는 같은 SID의 UAC helper를 사용하며 비대화형이면 이미 승격된 token이 필요하다.
Windows trust/untrust는 대화형 터미널과 OS 인증서 확인이 필요하다. 권한 부족은
암호 입력 없이 `permission_required`다. daemon과 조회 명령은 OS를 수정하지 않는다.
`dns = "external"`은 사용자가 준비한 DNS, `dns = "hosts"`는 명시 hosts 준비다.
외부 mapping과 충돌하면 덮어쓰지 않는다. 공유 mapping은 마지막 소유 참조 해제 시 제거한다.
복구용 `proxy hosts preview/apply/remove`도 제공한다.

`env_files`는 프로젝트 루트 기준이다. process 환경 > 앞쪽 파일 > 뒤쪽 파일 순서다.
없는 파일은 무시하고 읽기 실패나 잘못된 dotenv는 파일·행 오류로 보고한다.
host와 string port에서 `${NAME}`은 필수, `${NAME:-fallback}`은 unset/empty 기본값이다.
한 번만 치환하며 shell을 실행하지 않는다. `${profile}`, `${instance.alias}`, `${proxy}`도 유지한다.
환경 기반 route는 activation 때 값을 고정한다. 파일 변경 시 다시 up하고 shell 환경 변경은
다음 activation에 반영한다. literal route는 DNS가 준비됐으면 다음 요청부터 변경을 반영한다.
명시 down은 TOML에서 삭제했다가 다시 추가해도 유지되며 up으로 해제한다.

port binding 기본은 숫자이고 `format = "url"`은 loopback HTTP URL이다.
proxy binding 기본은 route URL이며 HTTPS를 우선하고 표준 80/443은 생략한다.
`profile`, `instance`로 다른 등록 위치를 참조한다. `template`는 typed 필드와 함께 쓰지 않는다.
`command env`와 `proxy env`는 public network binding만 읽고 missing 입력은 `pending`으로 보고한다.
known URL, listener 실행 여부, upstream readiness, client trust는 별도 상태다.
일반 profile 변수·secret·parent 환경은 조회 결과에 포함하지 않는다.

## 실행 중인 backend와 프로젝트 없는 실행

```sh
devtools proxy add docs 41000 --global --host docs.localhost --request-id UUID
devtools proxy up docs --global --request-id UUID
devtools proxy env docs --global
devtools proxy list --global
devtools proxy run docs --global -- python3 server.py
devtools proxy down docs --global --request-id UUID
devtools proxy rm docs --global --request-id UUID
```

add는 고정 backend 포트를 참조하는 비활성 전역 route를 만든다. 이미 실행 중인 backend에도
up으로 연결하며 process를 인수하거나 종료하지 않는다. foreground run은 포트가 비었을 때
정확한 포트를 claim하고 `PORT`, `URL`, `ROUTE_URL`과 모든 global peer의
`DEVTOOLS_<NAME>_PORT/URL/ROUTE_URL`을 주입한다. 정규화된 이름 충돌은 거절한다.
child 종료 코드·stdio·signal을 전달하고 종료 시 claim을 해제한다.
project/global 동명이름은 별도 scope다. global 선택에는 `--global`을 쓴다.

## 다른 장치에 공유하기

```sh
devtools expose start web --provider lan --alias shop.local --auth-file /private/path/basic-auth --request-id UUID
devtools expose start web --provider cloudflared --auth-file /private/path/basic-auth --request-id UUID
devtools expose start web --provider tailscale --serve-port 8443 --no-auth --request-id UUID
devtools expose list
devtools expose status EXPOSURE_ID
devtools expose stop EXPOSURE_ID --request-id UUID
```

auth 파일은 사용자 전용 regular file에 `user:password`를 담는다. `--auth-stdin`도 지원한다.
공유에는 인증 입력 또는 명시 `--no-auth`가 필요하다. 비밀은 argv·설정·공개 상태에 저장하지
않고 daemon 메모리에서만 보유한다. daemon 종료 뒤 인증 복원은 `auth_required`로 닫힌 상태가 된다.
stop 후 새 UUID로 다시 시작한다. 공유 중 route/listener/port/alias 변경은 pin으로 보호한다.
source 변경이나 provider 실패는 대상 ingress와 열린 stream만 종료하고 복구 상태로 남긴다.

LAN은 외부에서 도달 가능한 명시 listener 주소가 필요하다. 중지 상태에서
`--http-address 0.0.0.0:18080 --https-address 0.0.0.0:18443`처럼 설정한다.
`.local` alias를 자동으로 mDNS 광고하지 않는다. client DNS·방화벽·public CA 신뢰를 준비한다.
Cloudflared와 Tailscale은 설치된 executable만 사용하며 자동 설치·로그인은 하지 않는다.
Tailscale은 로그인과 tailnet HTTPS가 필요하고 선택 Serve 포트의 소유 handler만 정리한다.
설정은 LocalAPI ETag를 조건으로 갱신한다. 다른 사용자가 동시에 변경하면 덮어쓰거나 삭제하지
않고 `tailscale_configuration_changed`로 복구를 요구한다. LocalAPI 접근 권한과 조건부 갱신
지원이 필요하며 해당 자격증명은 메모리에서만 사용한다.
다른 포트·경로는 유지한다. Cloudflare Quick Tunnel은 provider 제약으로 SSE를 지원하지 않는다.
Cloudflared·Tailscale 공유는 고정 route의 loopback bridge에서 인증과 확정 public authority를 검사한다.
외부 HTTPS 요청을 내부 HTTP bridge에서 다시 리디렉션하지 않으며 backend에는 확정 public scheme/host를 전달한다.
공유 Basic 인증에 사용한 `Authorization`은 backend에 전달하지 않는다. `--no-auth`에서는
애플리케이션 인증을 위해 해당 헤더를 그대로 전달한다. backend 연결 풀은 공유별로 재사용하며,
공유 중지나 source 변경 시 해당 공유의 요청·stream과 idle 연결을 정리한다.
HTTPS로 설정한 LAN 공유의 HTTP 요청은 인증을 요구하기 전에 확정 HTTPS URL로 리디렉션한다.
경로와 query는 보존한다. HTTP만 설정한 LAN 공유는 HTTP를 사용한다. Cloudflared·Tailscale의
loopback bridge는 provider가 앞단에서 TLS를 종료하므로 이 LAN 리디렉션 대상이 아니다.

## 격리, WSL과 정리

`devtools --isolated-root /tmp/demo proxy ...` 또는 `DEVTOOLS_ISOLATED_ROOT`는 config/data/cache/runtime을
분리하고 daemon과 child에도 전달한다. `.localhost`와 고포트 HTTPS는 사용할 수 있다.
공유 hosts·OS trust·설치 실행 파일 capability 수정은 차단한다. kernel TCP 포트와 provider는
계속 공유된다. WSL Linux hosts·CA 신뢰는 Windows browser DNS·신뢰와 별개다.

```sh
devtools diagnostics
devtools proxy teardown --listener preview
devtools proxy teardown --listener preview --apply DIGEST --request-id UUID
devtools proxy teardown --all --remove-ca
devtools proxy teardown --all --remove-ca --apply DIGEST --request-id UUID
```

teardown은 preview와 같은 선택·digest로 apply한다. 선택 exposure·origin·listener·hosts 참조를
정리하고 route를 비활성화하며 프로젝트 TOML과 global 선언은 보존한다.
기본 정리는 공유 CA와 신뢰를 유지한다. 명시 `--all --remove-ca`만 남는 TLS 참조를 다시 확인하고
OS untrust 확인 뒤 private key를 제거한다. 실패·불확실한 신뢰 상태는 키와 복구 기록을 보존한다.
macOS untrust는 해당 CA의 불신 기록을 남기고 키체인의 인증서를 삭제한다. 공개 인증서만
다시 등록해도 신뢰되지 않으며, 명시적인 `proxy trust`로 다시 신뢰할 수 있다.
inactive 선언만으로 CA를 영구 유지하지 않는다. isolated root의 공유 OS untrust는 차단한다.
backup/profile transfer는 CA·인증 key·exposure session·host mapping·실행 상태를 옮기지 않는다.

dashboard의 Processes 화면은 listener·route URL·TLS·공유 상태와 선택 listener start/stop,
exposure stop을 제공한다. hosts 준비와 proxy 모드 restart는 initiating CLI를 안내한다.
전체 계약은 [network design](network-design.md)을 참고한다.
provider 문서: [Cloudflare Quick Tunnels](https://developers.cloudflare.com/tunnel/get-started/quick-tunnels/),
[Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve).
