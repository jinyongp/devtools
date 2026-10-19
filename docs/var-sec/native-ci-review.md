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
