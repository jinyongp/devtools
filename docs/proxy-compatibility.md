# 개발 서버의 Host·Origin 호환성

[로컬 네트워크](local-network.md) · [전달 계약](proxy-design.md) · [검증 실행](../verify/README.md)

공개 HTTPS 주소로 접속했을 때 개발 도구가 Origin 불일치를 보고한다면, 해당 route에
`host_policy = "preserve"`를 설정한다. protocol 11부터 제공하며 생략은 기존 `rewrite`다.
먼저 `devtools version`으로 실행 파일의 protocol을 확인한다.

## 프로젝트에 적용하기

다음 예시는 포트 `web`의 서버를 `https://app.dev.example`로 사용하는 설정이다.
DNS·HTTPS listener와 클라이언트 CA 신뢰는 [네트워크 가이드](local-network.md)대로 준비한다.

```toml
[proxies.web]
host = "app.dev.example"
port = "web"
host_policy = "preserve"
```

서버의 허용 호스트에도 `app.dev.example`을 지정하고 route를 다시 활성화한다.
서버의 allowlist나 URL binding을 바꿨으면 해당 서버도 재시작한다.

```sh
devtools proxy up web --request-id UUID
devtools proxy list
```

`proxy list`의 route 항목에 유효한 `host_policy`가 표시된다. `preserve`에서 backend는
공개 Host를 받지만 TCP 연결 대상은 계속 `127.0.0.1`의 할당 포트다.
`Origin`은 원문을 유지한다. 빈 문자열·환경변수 표현식·다른 enum은 허용하지 않는다.

전역 route를 새로 만들 때는 다음처럼 지정한다. `4400`의 서버는 이미 실행되어 있어야 한다.

```sh
devtools proxy add app 4400 --global --host app.dev.example --dns external --host-policy preserve --request-id UUID
devtools proxy up app --global --request-id UUID
```

각 `UUID`는 별도의 새 요청 UUID로 바꾼다. 응답 유실 뒤에는 같은 입력과 UUID를 사용한다.
기존 전역 route의 정책을 바꾸는 순서는 아래 복구 절차와 같다.

## 소비자 설정과 확인할 기능

devtools는 소비자의 검사를 끄거나 호스트를 자동 허용하지 않는다. 실제 공개 hostname만
허용한다. `allowedHosts: true`, 전체 Origin 허용, CSRF 검사 해제는 필요하지 않다.
공개 포트가 표준 443이 아니라면 HMR 클라이언트에도 그 포트를 지정해야 한다.

| 소비자 | 제한된 설정과 확인할 동작 |
| --- | --- |
| Nuxt 4.5.2 / DevTools 3.4.2 | `vite.server.allowedHosts`에 공개 hostname, HMR에 공개 `wss` 호스트·클라이언트 포트. DevTools Pages/Components의 실제 조회와 HMR을 확인한다. |
| Vite 8.3.5 / React 19.3.1·Vue 3.5.44 | `server.allowedHosts`에 공개 hostname, HMR에 공개 WS 주소. counter 변경 후 소스 수정 시 문서 reload 없이 DOM·상태가 유지되는지 확인한다. |
| Next.js 16.4.2 | `allowedDevOrigins: ['app.dev.example']`. App Router asset·Fast Refresh를 확인한다. 재현 fixture는 이동 가능한 의존성 링크를 위해 Next의 `--webpack` 개발 모드를 사용한다. |
| SvelteKit 3.1.0 / Svelte 5.57.2 | v3 `sveltekit()` Vite plugin과 정확한 `allowedHosts`. SSR·탐색·form action·HMR을 확인한다. fixture의 counter는 module store를 사용한다. 편집한 컴포넌트의 로컬 상태 재생성은 Svelte 자체 동작이다. |
| webpack 5.112.0 / dev-server 6.1.0 | `allowedHosts: ['app.dev.example']`, `client.webSocketURL`에 공개 scheme·호스트·포트. asset·HMR·상태 유지 확인. |
| ws 8.22.0 | upgrade 전에 요청 Host와 공개 authority, Origin과 공개 origin을 비교한 뒤 echo·close 확인. |
| Socket.IO 4.8.4 | `allowRequest`에서 동일한 검사를 수행하고 필요한 정확한 CORS origin만 지정. polling→upgrade, websocket-first, ack·재연결 확인. CORS만으로 WS 접근을 제한하지 않는다. |

고정된 전체 설정은 [fixture](../verify/fixtures/proxy-compat)에 있다. Vite HMR 옵션은 소비자와
Vite 버전별 API를 따른다. fixture의 구 `server.hmr` 호환 옵션은 Vite 8에서 deprecation 안내를 낼 수 있다.

Nuxt DevTools 3.4.2는 Origin의 `URL.host`와 요청 Host를 비교한다. scheme까지 검사하지 않고
Origin이 없는 연결은 허용한다. 원본 Host의 대소문자나 명시적 `:443`과 브라우저의 URL 정규화가
다르면 `preserve`만으로 exact-string 비교가 성공하지 않을 수 있다. Next 개발 리소스의
allowlist는 hostname 기준이다. SvelteKit 3의 production CSRF 검사를 dev-server 결과로
검증했다고 해석하지 않는다. 독립 ws·Socket.IO fixture는 scheme·authority를 명시적으로 검사한다.

## 공유와 인증

LAN·Cloudflared·Tailscale은 route 정책을 상속한다. 공유의 `preserve` Host는 검증 후 commit한
공개 URL의 authority다. 클라이언트가 보낸 `X-Forwarded-Host`는 선택 근거가 아니다.
전달 헤더는 다시 만들고 `Forwarded`는 제거한다. 공유 Basic 인증을 소비했으면
`Authorization`을 제거한다. 명시적인 `--no-auth`에서는 애플리케이션 Authorization을 보존한다.

공유 주소가 달라지면 소비자의 allowlist·HMR 공개 URL도 그 주소에 맞춰야 한다.
Quick Tunnel은 SSE를 지원하지 않는다. Cloudflared의 WS/RPC 성공을 SSE 성공으로 기록하지 않는다.
다른 Origin 거절과 실제 RPC 조회·HMR·메시지 왕복을 함께 검사한다.

## 복구와 버전 전환

기존 backend가 재작성 Host에 의존하면 그 route는 `rewrite`를 유지한다.
프로젝트는 `host_policy`를 제거하거나 `rewrite`로 바꾼 뒤 `proxy up`한다.
정책 변경은 기존 공유 generation을 무효화하므로 먼저 관련 공유를 명시적으로 중지한다.

전역 route를 바꾸려면 `expose list`에서 해당 route의 공유 ID를 확인한 뒤 각각 중지한다.
다음은 기존 `app`을 `rewrite`로 복원하는 순서다.

```sh
devtools expose stop EXPOSURE_ID --request-id UUID
devtools proxy down app --global --request-id UUID
devtools proxy rm app --global --request-id UUID
devtools proxy add app 4400 --global --host app.dev.example --dns external --host-policy rewrite --request-id UUID
devtools proxy up app --global --request-id UUID
```

공유가 없으면 첫 줄은 생략한다. 같은 이름을 다시 만들어도 새 route UUID이며,
이전 exposure 시작 UUID를 재사용하지 않는다. 현재 주소의 HTTP·WS 기능을 다시 확인한다.

새 CLI가 실행 중인 구 daemon을 만났을 때는 `old_daemon_version`으로 거절한다.
관련 공유를 중지한 뒤 `devtools proxy restart --all --request-id UUID`를 명시적으로 실행한다.
전체 listener에 영향을 주므로 작업 범위를 확인한다. 자동으로 daemon을 교체하지 않는다.

구 CLI로 돌아가려면 새 CLI로 모든 preserve 공유를 중지하고 pin을 해제한 뒤, 프로젝트의
정책을 제거하고 preserve 전역 route를 rewrite로 다시 만든다. 정상 중지된 공유의 정책 metadata는
정리되며 default registry/digest projection은 기존 형식으로 돌아온다. 구 CLI로 명시 restart한다.
새 Host 필드를 남긴 상태의 구 strict reader는 거절한다. registry v4보다 오래된 버전으로
돌아가는 절차는 별도 pre-upgrade backup이 필요하다. private 저장 파일을 수동 편집하지 않는다.

검증 실행·OS·모의 Provider와 실제 ingress의 경계는 [검증 표](../verify/network-coverage.md)에 기록한다.
