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

## 실제 native 자료를 유지한 self-update 검증 추가

별도 설치/업데이트와 native DPAPI 검사를 잇는다. 같은 임시 기본 root의 native
secret을 남긴 채 manual lock·authenticated retire/lease drain을 완료하고, 같은
source SHA의 다른 version package를 private fixture 디렉터리에 만든다. 실제 설치
binary의 update를 실행한 뒤 새 version·명시 nopassphrase unlock·원래 secret의
masked 실행·동일 vault/backend를 확인한다. old cache로 성공시키지 않는다.

추가 package는 게시하지 않고 같은 owned temporary root 안에서만 사용한다.
원래 candidate version과 실제 최종 test version을 각각 evidence에 기록한다.
기존 isolated source/target 및 관리 실행·암호화 전송 검사를 유지하며 새 image로
동작한다. finally는 모든 root의 old/new worker를 실제 retirement/lease로 정리한 뒤
root를 제거한다. native mutation guard·기존 root 거절은 그대로다.

리뷰: source/version/digest provenance, old key session 종료, actual self-update 호출,
새 worker native unlock, 값/identity 보존, managed/transfer caller와 최종 image/version
표시·cleanup을 확인했다. Python syntax와 release contract를 검사한다. 실제 x64/ARM64
결과 전까지 이 연결 검사를 pass 또는 N02 완료로 표시하지 않는다.
