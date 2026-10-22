# 로컬 reverse proxy

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

현재 설정·HTTPS·custom domain·실행·공유·정리는 [로컬 네트워크 가이드](local-network.md)를 따른다.

protocol 12의 이름 명령 실행은 `serve`와 연결된 proxy 선언을 자동 준비한다.
`--proxy`는 비활성 route까지 준비하고, `--no-proxy`는 준비만 생략한다.
route를 끄려면 `proxy down`, listener를 끄려면 `proxy stop`을 사용한다.

- [현재 network 계약](network-design.md)
- [HTTP route 선택과 전달 계약](proxy-design.md)
- [서버 실행과 readiness](processes.md)
