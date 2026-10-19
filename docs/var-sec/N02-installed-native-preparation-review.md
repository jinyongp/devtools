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
