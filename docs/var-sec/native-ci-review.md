# Native CI 수정 검토

현재 검증 후보는 main `1035006`이며 release 검증은
[39230873634](https://github.com/jinyongp/devtools/actions/runs/39230873634)에서
`publish=false`로 실행했다. 사용자가 main 진행을 승인했으므로 이후 수정도 main에
반영한다. 이 기록은 실패별 수정과 재검증을 누적하며 native 전체 통과를 뜻하지 않는다.

## Go JSON 결과와 진단 스트림 분리

Linux 서비스 부재 검사는 실제 pass했으나, 처음 다운로드한 Go module의 stderr를
stdout JSONL에 합쳐 Python JSON decoder가 실패했다. stdout만 JSON event로 읽고
stderr는 별도 파일에 보존한다. 같은 결함이 가능한 macOS runner에도 적용한다.
Go 실패 exit, 필수 test 이름의 실제 pass 확인과 skip 거절은 유지한다.

리뷰: 두 runner의 subprocess exit 검사, strict JSON parsing, required-test 집합,
finally cleanup을 재검토했다. Python AST syntax pass. 실제 Linux 정상·잠금·종료 및
macOS locked collection 검사는 수정 revision의 CI에서 다시 확인해야 한다.

## macOS file keychain의 비대화형 접근

ARM64 실제 runner에서 synthetic roundtrip은 pass했지만 locked collection의
SecItemCopyMatching이 UI를 기다려 3분 test timeout이 났다. query의 authentication UI
fail 값만으로는 file-based keychain의 UI를 차단하지 못한다는
[Chromium의 실제 구현](https://chromium.googlesource.com/chromium/src/crypto/+/refs/heads/main/apple/scoped_keychain_user_interaction_allowed.cc)과
[Apple API](https://developer.apple.com/documentation/security/seckeychainsetuserinteractionallowed(_:))를
확인했다. 기존 file keychain 선택을 유지하며 native call 동안 process-global interaction
설정을 명시적으로 적용하고 원래 상태를 복구한다. 이 설정을 쓰는 모든 호출을 같은 mutex로
직렬화해 interactive/noninteractive 호출의 경합을 막는다. 이는 session/storage mutex가
아니며 production의 bounded native helper는 유지한다. 두 interaction-required OS status를
같은 typed error로 처리한다.

리뷰: Wrap/Unwrap/Remove의 query와 모든 조기 반환에서 설정 복구와 CF release를 확인했다.
Darwin ARM64 test binary compile pass. locked collection 실제 거절과 unlock 후 같은 키
유지는 두 native architecture의 수정 CI가 확인할 필수 조건이다.

## Native fixture의 파일 경계

Windows full suite의 다섯 vault 검사에서 공통 session fixture의 `FileStore.Initialize`가
recovery 오류로 끝났다. session fixture는 `os.MkdirTemp` root 자체를 data root로 썼지만,
이 API는 Windows의 protected ACL을 만들지 않는다. 테스트 data root를 그 아래의
production `platformfs.OpenRoot(..., true)`로 생성한 private child로 옮긴다. 공개 ACL을
허용하도록 제품 검사를 바꾸지 않는다. macOS fixture 경로의 기존 OS symlink는 실제
경로로 선택한 뒤 사용한다.

Windows completion의 무변경 검사는 live daemon의 byte-range locked file을 ReadFile해
실패했다. lock 파일 이름과 수정 시간은 계속 비교하되 OS가 잠근 bytes를 읽지 않는다.
다른 일반 파일의 내용 비교는 유지한다. macOS system-network fixture도 `/var` alias를
실제 temporary 경로로 바꿔 no-follow 경계에 전달한다. Windows installed product 실패의
하위 condition 및 session helper의 typed 오류를 출력에 포함해 원인 추적을 가능하게 한다.
값·키·credential·raw child output은 추가하지 않는다.

리뷰 범위: fixture 생성/수명/정리, 두 snapshot 호출의 비교 대상, 설치 fixture의 안전한
오류 필드, network HOME/cleanup 경로를 확인했다. 영향 Linux 검사와 Python AST를 실행하고
Windows 실제 결과는 새 CI에서 판정한다. 기존 실행 실패는 아직 해결됐다고 표시하지 않는다.

macOS full suite의 추가 로그에서도 `/var` 경로를 그대로 쓰는 새 vault/values fixture들의
no-follow 거절을 확인했다. vault의 일반 TempDir 호출은 공통 private helper로 바꿔 OS
alias 해소와 native ACL 생성을 적용하고, 기존 values Unix helper는 반환 경로를 해소한다.
제품의 symlink/ACL 거절 규칙은 유지한다. 기존 테스트가 다루는 손상·누락·승격·복구 조건은
그대로 둔다. 이 추가 수정도 영향 package 전체 검사와 실제 macOS suite에서 확인한다.

추가 수정 후 Linux 전체 vault/values pass(7.874s/9.968s), 두 package race pass
(22.426s/61.446s), Windows ARM64·Darwin amd64 test binary compile pass.
release contract 9개 pass. 첫 release 검사에는 writable GOCACHE 지정이 빠져 local
read-only cache 오류가 났으며, 같은 소스에서 cache 경로를 지정한 재실행으로 확인했다.

## 실제 GNOME의 MIME 응답과 unlock control

`b56e5ce`의 두 macOS Keychain job과 macOS system-network는 실제 pass했다.
Linux는 파싱 문제를 통과한 뒤 정상 roundtrip의 integrity 실패와 collection 재해제 실패가
새로 확인됐다. [GNOME 원본 encoder](https://github.com/GNOME/gnome-keyring/blob/main/daemon/dbus/gkd-secret-secret.c)는
binary Secret.Value에도 고정 `text/plain` MIME을 반환한다. 이를 octet-stream과 함께
허용하되 bytes의 backend/vault/형식/길이/전체 digest 검증은 계속 적용한다. 텍스트 변환이나
손상 자료 허용은 없다. 기존 private-bus contract에 GNOME label의 동일 키 roundtrip,
다른 MIME 거절, text label의 손상 payload 거절을 포함한다.

GNOME fixture는 고정된 private control directory를 만들고 daemon과 unlock helper 모두
그 디렉터리를 명시한다. 별도 helper가 다른 daemon을 찾거나 시작할 여지를 줄이며,
실제 Locked property와 unwrap 결과로 해제 여부를 계속 검사한다. 합성 daemon의 직접
process handle에 대한 종료/회수와 private bus 수명은 유지한다. 이 수정의 실제 GNOME
전체 pass는 이후 CI에서 확인해야 한다.

## Windows frozen launcher와 cleanup 경로

`b56e5ce`의 installed product는 `launcher_unavailable`로 실패했다. 호출을 따라가니
새 `__process-launch`가 Windows private entry allowlist에 없어 inherited lease나 frozen
payload를 받기 전에 unsupported_platform으로 종료됐다. 실제 product가 사용하는 이
entry를 허용하고 기존 platform contract 검사에 포함한다. 동일 binary/상속 lease 검증과
payload/admission 검증을 건너뛰지 않는다. legacy __process-serve도 계속 유지한다.

cleanup helper의 recovery 오류는 endpoint가 root의 경로 문자열에 바인딩되는데 Python
resolve가 Windows temporary 8.3 경로를 긴 이름으로 바꾸는 문제였다. helper는 caller가
선택한 절대 경로 표기를 보존한다. metadata의 endpoint 검증을 느슨하게 하지 않는다.
리뷰: 실제 start entry·platform preflight·inherited lease·handoff caller와 helper의 경로
전달을 확인했다. Python syntax와 양 architecture의 CLI test binary compile을 수행하고,
실제 installed product의 readiness/restart/stop/transfer는 수정 CI에서 다시 확인한다.

## macOS backup·diagnostics fixture 후속 검토

`b56e5ce`의 macOS vault/values/CLI suite는 pass했고 backup·diagnostics에서 같은 `/var`
alias가 남아 있었다. backup의 기존 Unix privateTestRoot와 diagnostics의 vault 상태를
읽는 test root를 실제 경로로 선택한다. 저장소와 read-only 진단의 제품 검사는 유지한다.
기존 fixture caller와 cleanup, missing/corruption/restore/canary 검사의 조건을 재검토했다.
Linux backup/diagnostics 전체 race pass(34.533s/1.126s). 실제 macOS 결과는 후속 CI에 남는다.
