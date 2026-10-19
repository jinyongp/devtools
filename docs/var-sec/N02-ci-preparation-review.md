# N02 Windows DPAPI 다른 사용자 검증 준비

현재 진행(2026-10-19): main의 b56e5ce에서 Windows x64/ARM64 전체 suite가 실제 pass했다. 설치물 검사의 launcher 진입점 누락을 수정하여 de6fd4b에서 재검증 중이다. 아래 준비 당시의 교차 compile과 실제 결과를 구분하며 N02 전체 완료 전이다. 최신 근거는 [native-ci-review.md](native-ci-review.md)를 따른다.

N02는 진행 중이다. Windows native 수용 결과는 아직 없으며 이 문서는 추가한 실제
OS 테스트의 source review와 교차 compile만 기록한다.

기존 Windows Actions의 suite lane은 disposable standard account를 만들고 암호를
mask 처리한 뒤 `DEVTOOLS_TEST_USER`/`DEVTOOLS_TEST_PASSWORD`로 실제 Go suite에
전달한다. 새 `TestNativeDPAPIRejectsDifferentWindowsIdentity`는 같은 fixture를
사용한다. CI에 계정이 없으면 skip으로 성공하지 않고 실패한다.

이 검사는 파일 ACL 거부와 별개다. 원래 사용자로 감싼 합성 32-byte 키의 ciphertext를
메모리에 유지한 채 실제 다른 SID token을 LogonUserW/DuplicateTokenEx로 얻고 OS
thread에 적용한다. 실제 thread SID도 확인한 뒤 DPAPI가 그 ciphertext를 해제하지
못하고 plaintext도 반환하지 않아야 한다. 원래 신원으로 복귀한 뒤 동일 자료가
여전히 원래 키로 열려야 한다. 사용자 암호·키·native 원시 응답을 출력하지 않는다.
thread impersonation 동안 해당 goroutine을 OS thread에 고정하며 복귀에 실패하면
그 thread를 Go 실행 pool에 반환하지 않는다. 실제 사용자나 stored PID를 종료하지 않는다.

검토 범위: DPAPI consumer와 오류 분류, 실제 다른 SID 적용 여부, 원래 신원 복귀,
credential 출력과 fixture 수명, 기존 workflow 계정 설정을 확인했다. 기존 roundtrip·
다른 vault·손상·비대화형 UI 금지 검사는 유지한다. 현재 source review에 남은 확인된
적격 결함은 없다. amd64/ARM64 교차 compile을 수행하되 이것은 실기 결과가 아니다.

현재 필수 pending: x64·ARM64 실제 DPAPI/다른 SID/ACL/설치·업데이트, Windows named
pipe 통합과 ARM64 contention, x64 race 및 전체 제품 native 결과. 이 추가 테스트나
앞선 Linux 결과로 N02/N04/N05/A12를 완료하지 않는다.
