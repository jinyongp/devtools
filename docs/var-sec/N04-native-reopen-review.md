# N04 실제 native 자동 재해제 연결

fake clock의 idle/재해제 단위 검사와 별도로 실제 OS provider를 사용하는 cold worker 경계를 검사한다. macOS installed Keychain과 Windows installed DPAPI fixture는 현재 session ID를 private 파일에서 읽고 authenticated retire/실제 lease drain을 완료한 뒤 vault unlock이나 암호 입력 없이 같은 값을 실행한다. masked 결과와 새 session ID를 모두 요구하므로 기존 warm key로 성공할 수 없다. 기존 manual lock 이후 자동 사용 거절과 명시 unlock 검사는 유지한다.

Linux 실제 PAM logout fixture는 새 OS session의 잠긴 collection 거절을 확인한 후 OS collection만 명시 해제한다. devtools vault unlock보다 먼저 ordinary run을 실행하여 자동 native 재해제를 검증한다. 이후 명시 unlock과 같은 값 실행도 유지한다. 사용자 keyring activation/권한 정책과 native isolated-root 금지에는 변경이 없다.

리뷰 영역은 새 worker 증거·암호/UI 미사용·manual latch 보존·실제 OS collection 상태·다음 명시 unlock/update 호출자·실패/정리를 포함한다. Python syntax와 diff를 검사했다. N01/N02의 기존 acceptance 근거는 그대로 유효하며 이 추가 native 자동 재해제는 후속 실제 CI 전까지 pending이다. wall-clock 8시간을 기다렸다고 주장하지 않는다. idle timeout/poll 미연장의 시간 의미는 기존 fake-clock 검사로 구분한다.
