# A03 macOS Keychain 어댑터 구현 검토

범위: Security.framework/CoreFoundation 직접 호출과 opt-in native test.
현재 Linux에서 native API를 실행하지 않았으며 N01에서 disposable keychain과
실제 설치·업데이트·잠금/거부 조건을 검증해야 한다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 직접 OS API·배포 | reviewed | 절대 framework 경로, CGO 없이 Darwin amd64/arm64 test binary 컴파일 |
| 항목 격리·중복 | reviewed | generic-password + 고정 service + random vault ID account, add-only, 기존 항목 수정/전체 삭제 API 없음 |
| vault payload·오류 | reviewed | typed OS payload 재검증, locator/ID 불일치 거절, missing/interaction/denied/duplicate/unavailable 구분 |
| FFI 자원·동시성 | reviewed | once 초기화, CF reference 단위 정리, Go byte 인자 KeepAlive, 결과 type/크기 확인, 프로세스 수명 framework handles |
| 비대화형·취소 | reviewed | 기본 인증 UI fail, explicit setup만 UI opt-in; 동기 OS 호출 전후 취소 확인 |
| native test 안전성 | reviewed | opt-in만 OS 항목 생성, random ID·합성 키, 자신의 항목만 cleanup, 중복/다른 ID/삭제 뒤 missing 검사 |
| 실제 OS·업데이트·서명·잠금/취소 | pending N01 | cross-compile이 이 근거를 대신하지 않음 |

리뷰에서 non-retaining dictionary를 참조 값보다 먼저 해제하도록 순서를 명확히 했고,
native test의 명시 삭제 뒤 재생성을 제거했다. 호출자가 도중에 취소되거나 키 파일
게시가 실패해 OS 항목만 남으면 자동 교체하지 않으며 복구 대상으로 처리한다.
OS Keychain 보호는 동일 사용자 악성 프로세스 격리나 iOS 보호 클래스 보장이 아니다.
Darwin vet가 지적한 symbol 주소의 uintptr→unsafe.Pointer 변환도 native memcpy로
대체하여 Go pointer provenance를 유지했다. 두 Darwin 아키텍처 vet를 재실행했다.

추가 의존성: `github.com/ebitengine/purego v0.11.2`, Apache-2.0, Go 1.25 이상,
하위 모듈 의존성 없음. 프로젝트 Go 1.27.1과 macOS amd64/arm64·CGO=0 배포에 맞는다.
표준 Go는 Darwin framework FFI를 제공하지 않고 cgo는 기존 cross-build를 바꾸므로
이 라이브러리를 선택했다. Windows는 기존 x/sys를 사용한다.
공식 [릴리스](https://github.com/ebitengine/purego/releases/tag/v0.11.2)의 thread/signal 수정과
[모듈 정보](https://proxy.golang.org/github.com/ebitengine/purego/@v/v0.11.2.info),
공식 Go vulnerability module index 및 저장소 advisories를 확인했다. 확인 시 module index에
항목이 없었으며 이것은 전체 의존성 보안 감사나 알려지지 않은 결함 부재의 증거가 아니다.

API 근거: Apple [SecItemAdd](https://developer.apple.com/documentation/security/secitemadd(_:_:)),
[SecItemCopyMatching](https://developer.apple.com/documentation/security/secitemcopymatching(_:_:)),
[인증 UI fail](https://developer.apple.com/documentation/security/ksecuseauthenticationuifail).
검증: Linux vault/values/execution race, vault vet, CGO=0 Darwin 두 아키텍처 compile.
Actions 경로는 기존 Release의 macos-latest와 Windows platform의 x64/ARM64다.
원래 native 기준은 workstream N01–N04로 분리했고 A12·Z01의 필수 선행 조건으로 유지했다.
