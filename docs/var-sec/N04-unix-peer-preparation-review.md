# N04 실제 다른 Unix 사용자 peer 검증 준비

Linux·macOS native job에 실제 다른 UID의 두 방향 IPC 검사를 추가한다. 실제 root 실행은
disposable host flag가 있는 전용 CI에서만 허용한다. 현재 개발 WSL에서는 실행하지 않았다.
일반 Go suite의 skip/교차 compile을 native pass로 표시하지 않는다.

부모 test process는 CI에서 sudo로 실행하고 자식에 UID/GID 65534를 적용한다. supplementary
groups도 비운다. 자식은 실행 중 실제 UID가 65534인지 확인하며, 현재 test binary를 복사한
전용 public fixture와 socket 외 다른 자료를 읽거나 쓰지 않는다. private key/session 파일을
공개하지 않는다. 다른 사용자 server의 socket은 실제 다른 사용자가 소유한 디렉터리에서
만들며 parent client가 dialSession의 OS peer 검사로 거절해야 한다. 반대 방향도 raw socket에
실제 다른 사용자 client를 연결한 뒤 production verifySessionPeer가 거절해야 한다.

socket 접근 자체는 의도적으로 열어 파일 mode 거절만으로 UID 검사를 대체하지 않는다.
test-only fixture는 공개 constant readiness 외 원문을 보내지 않는다. 부모는 자신이 만든
process handle만 종료·wait하며 stored PID를 쓰지 않는다. deadline과 자체 temporary root
cleanup을 둔다. 제품 IPC listener·UID 규칙·protocol은 변경하지 않는다.

wrapper는 일반 사용자로 test binary를 만든 뒤 그 단일 test만 sudo/env로 실행하고
test2json의 실제 parent/subtest pass와 skip 없음까지 검사한다. stdout JSONL과 stderr를
분리해 artifacts에 보존한다. native Keychain/Secret Service job이 계속 package의 필수
dependency이므로 이 검사의 실패도 남는다.

리뷰 범위: test entry registry/production 비영향, 실제 UID 선택과 group 제거, 양방향 peer
consumer, root/socket/binary 소유권, child/pipe/timeout/cleanup, required-event parsing과
artifact 연결을 확인했다. macOS 두 architecture 및 Windows 기존 test entry compile,
Python syntax·workflow YAML을 검사한다. Linux 일반 suite는 privileged 검사 skip이며
전체 vault race로 기존 경로 회귀를 확인한다. 실제 Linux/macOS 다른 UID 결과는 후속 CI에
남고 이 준비로 N04 전체를 완료하지 않는다.
