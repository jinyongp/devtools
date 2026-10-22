# HTTP proxy 전달 계약

[문서 목록](README.md) · [설정과 실행](local-network.md) · [네트워크 전체 계약](network-design.md)

이 문서는 route 선택과 HTTP 전달을 설명한다.
listener·DNS·HTTPS·공유·복구 설정은 [네트워크 계약](network-design.md)을 따른다.
명령 예시는 [사용 가이드](local-network.md)에 있다.

## Route 선택과 소유 범위

프로젝트 route는 등록된 instance의 `devtools.toml` 선언과 port assignment를 결합한다.
프로젝트의 canonical root와 profile이 등록 정보와 같아야 한다.
다른 위치의 설정이나 다른 profile을 기존 assignment와 결합하지 않는다.
전역 route는 프로젝트 없이 고정 backend를 참조하며 별도 route UUID를 가진다.

호스트는 선택 listener에서 하나의 ready route와 정확히 일치해야 한다.
alias·할당·설정이 없거나 충돌하면 임의의 후보로 전달하지 않는다.
한 instance의 오류가 다른 정상 route의 전달까지 막지는 않는다.
route 설정·소유권과 활성화 원본은 요청 시 확인한다. 장기 연결도 주기적으로 재검사한다.

literal `.localhost` route는 기존 implicit 활성화와 호환된다.
환경변수 기반 route는 activation snapshot을 사용하며 변경 뒤 다시 up해야 한다.
명시 down은 tombstone을 남겨 TOML 제거·재등록이나 daemon 재시작만으로 해제되지 않는다.

## HTTP 응답과 TLS

| 조건 | HTTP 상태 |
| --- | --- |
| 잘못되거나 등록되지 않은 hostname, TLS SNI 불일치 | 404 |
| route 충돌·사용 불가 또는 해석 실패 | 503 |
| 공유하지 않은 route에 대한 외부 요청 | 403 |
| backend 연결 실패 | 502 |
| HTTPS가 설정된 listener의 HTTP 요청 | 307, 같은 경로·query의 HTTPS URL |

HTTPS는 명시 설정한 listener에 적용하며 활성 route의 SNI hostname만 허용한다.
클라이언트의 CA 신뢰와 backend readiness는 listener 실행 여부와 별도로 확인한다.
공유하지 않은 route는 wildcard listener에서도 loopback 접근만 허용한다.
TLS handshake에서 거부된 hostname에는 HTTP 안내 페이지를 반환할 수 없다.
아래 안내는 HTTP 요청 처리까지 도달한 오류에 적용한다.

프록시의 404·403·503·502 오류는 같은 스타일의 안내 페이지를 사용한다.
`Accept`에 `text/html`을 허용한 브라우저에는 HTML을, 나머지 클라이언트에는
같은 복구 안내를 plain text로 반환한다. 두 응답 모두 캐시하지 않는다.

- 404는 주소·route·listener 확인 방법을 안내한다. 잘못된 hostname과 TLS SNI 불일치도 같은 안내를 쓴다.
- 403은 로컬 연결 제한과 공유 주소 불일치를 구분하고, 현재 공유 URL 확인 방법을 안내한다.
- 503은 route 해석 실패와 공유 사용 불가를 구분하고, route·listener·공유 상태 확인 방법을 안내한다.
- 502는 backend 응답 실패와 대상 loopback port를 보여주고, 서버 실행·로그 확인 방법을 안내한다.

확인 명령은 항상 표시하며 상태 코드, 새로고침 링크와 요청 hostname을 함께 제공한다.
backend 주소는 502에만 표시한다. backend가 직접 반환한 오류 응답은 그대로 전달하며,
관리 API와 공유 Basic 인증 challenge는 기존 응답을 유지한다.

## Backend와 전달 헤더

backend는 등록된 loopback port로 연결한다. 임의 URL을 대상으로 하는 forward proxy가 아니다.
transport는 부모의 `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`를 사용하지 않는다.
일반 HTTP body·header, streaming response와 WebSocket upgrade를 전달한다.

`host_policy` 생략 또는 `rewrite`는 upstream `Host`를 loopback target으로 바꾼다.
`preserve`는 route·SNI·접근 검사를 통과한 요청의 원본 authority를 대소문자와 포트까지
유지한다. 두 정책 모두 실제 URL/TCP 대상은 `127.0.0.1`의 할당 포트다.
일반 HTTP와 WebSocket에 같은 정책을 적용하며 `Origin`은 변경하지 않는다.
원래 공개 authority는 `X-Forwarded-Host`로 전달한다.
`X-Forwarded-Proto`는 실제 연결의 HTTP/HTTPS를 반영한다.
외부 provider가 TLS를 종료하는 bridge는 commit한 공개 scheme·authority를 사용한다.
클라이언트가 보낸 forwarding header를 신뢰 근거로 사용하지 않는다.
`Forwarded`는 제거하고 `X-Forwarded-*`는 다시 만든다. 공유는 route 정책을 상속한다.
공유의 `preserve` Host는 commit한 공개 authority이며 클라이언트의 전달 헤더가 아니다.
소비자의 Host·Origin 검사와 allowlist는 그대로 적용된다.
설정과 소비자별 검증 범위는 [호환성 가이드](proxy-compatibility.md)를 따른다.

공유 Basic 인증을 소비했으면 `Authorization`을 backend에 넘기지 않는다.
명시적인 `--no-auth` 공유는 애플리케이션 인증을 위해 해당 헤더를 유지한다.
오류 응답에 프로젝트 경로나 내부 오류 원문을 포함하지 않는다.

## 연결 수명과 정리

listener와 exposure는 각자 backend 연결 풀을 소유한다.
선택한 listener 중지, 공유 해제와 source 변경은 해당 범위의 요청·stream·idle 연결을 정리한다.
다른 listener나 공유의 정상 연결은 유지한다.
동시 provider 변경, 열린 stream의 취소와 CA 정리는 [전체 계약](network-design.md#exposure-replay-and-cleanup)을 따른다.

포트 reservation은 listener 중지 뒤에도 유지한다.
port registry의 현재 저장 형식과 호환성은 [포트 저장 계약](port-design.md#범위와-저장-범위)을 따른다.
실제 HTTP·WebSocket·TLS 검증 방법과 모의 provider의 한계는
[검증 안내](../verify/README.md#검증-경계)를 참고한다.
