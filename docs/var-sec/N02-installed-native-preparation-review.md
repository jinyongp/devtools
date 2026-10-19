# N02 설치된 CLI의 기본 DPAPI 연결 검증 준비

기존 installed Windows product journey의 source/target passphrase root와 별도로 세 번째
자체 root에서 기본 `vault init`을 호출한다. backend가 실제 dpapi이고 unlocked인지 확인하고
CLI secret 저장 → 실제 Node 자식 주입·마스킹 → manual lock 후 실행 거절 → 암호 인자 없는
명시 unlock → 두 번째 실행 → backend 유지의 public CLI 경로를 확인한다. 이 검사는 단순
DPAPI API unit test와 실제 packaged CLI/provider/native helper 연결을 구분하기 위한 것이다.

합성 값은 stdin으로만 저장하고 Node argv에는 넣지 않는다. 정상 출력은 [REDACTED]여야
하며 raw 실행 오류를 출력하지 않는다. 기존 암호화 전송·readiness·restart·TLS 경로는
유지한다. 세 번째 root도 기존 authenticated retire/OS lease drain 뒤 자체 temporary
root 정리에 포함한다. DPAPI는 별도 사용자 keyring item을 만들지 않는다.

리뷰: 기본 provider 선택, CLI/backend 상태, manual latch의 native 재해제 차단, 실제 별도
자식의 non-TTY 출력, 기존 source/target fixture와 cleanup을 확인했다. Python syntax를
검사했다. 실제 Windows x64/ARM64 product CI가 실행하기 전에는 pass나 N02 완료로
표시하지 않는다. native 데이터가 있는 상태의 설치 update·다른 SID pipe 등은 별도 조건이다.

## 실제 CI에서 확인한 fixture 경계 수정

5fa27ab의 실제 두 product lane은 `isolated_operation_forbidden`으로 종료됐다. 이는 제품의
의도된 제한이다. native key-store mutation을 isolated root에서 허용하도록 바꾸지 않는다.
대신 명시 disposable flag가 있는 Windows Actions 계정에서 public project inspect로
기본 경로를 읽고 config/data/cache/runtime이 같은 예상 root의 child인지 확인한다. 기존
root나 symlink가 있으면 어떤 native mutation/삭제도 하지 않고 실패한다. 없다면 원자적
mkdir 성공 후에만 그 root의 소유권을 기록하고 native 검사를 진행한다.

마지막에는 소유한 세션의 authenticated retire/실제 lease drain 뒤 이 root만 삭제한다.
본래 source/target은 계속 isolated이며 실제 사용자 root를 썼다는 사실을 결과에 표시한다.
모든 자체 root 정리가 끝난 뒤에만 성공 artifact를 발행한다. 현재 개발 호스트의 기본
root에는 이 검사를 실행하지 않는다. Python syntax와 두 call site·실패 전/후 cleanup을
재검토했으며 수정된 Windows native 결과는 후속 CI에서 확인한다.
