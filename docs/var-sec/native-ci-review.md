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
