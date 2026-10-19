# N04 native IPC·WSL·자동 재해제 최종 리뷰

실제 source **d06fa41a31bc77ff2d1e2ae9d31d08220fc0d9f7**, [Actions](https://github.com/jinyongp/devtools/actions/runs/39251358258)의 native Unix peers/Keychain/Linux logout/WSL와 Windows x64·ARM64 suite/product가 모두 성공했다. [원시 peer events·session terminal events·실제 설치 결과](evidence/N04-native-d06fa41.json)에 해당 job ID와 source를 보존한다. 전체 run/A12/Z01 완료와 구분한다.

| 조건 | 최종 상태·실제 근거 |
| --- | --- |
| I01 양방향 다른 Unix UID | reviewed/pass, Linux·macOS Intel/ARM의 실제 root/65534 process 간 client/server 거절 |
| I02 다른 SID·local-only pipe | reviewed/pass, 실제 별도 SID process 신원 대조, 양방향 peer 거절·private ACL open 거절; 같은 ACL의 실제 SMB loopback positive control은 허용하고 production pipe는 거절 |
| I03 fake/stale endpoint | reviewed/pass, 실제 daemon의 vault/session/challenge 불일치·endpoint의 foreign local/UNC transport 거절, 키 미전달 |
| I04 같은 사용자 악성 앱 한계 | reviewed/pass 한계 시험, typed client 없이 직접 wire handshake/use로 warm key 수신 가능. user identity 인증이며 app sandbox가 아님을 문서와 일치시킴 |
| I05 frame·연결·KDF 폭주 | reviewed/pass, oversized/zero/duplicate frame 실제 IPC 거절, 8개 admitted connection 뒤 초과 연결 prompt 거절·회복, barrier 8개 실제 Argon2 wrong-password 작업의 동시 최대 1 |
| I06 protocol 불일치 | reviewed/pass, 실제 daemon mismatch·stable control-only retirement·키 전달 없는 거절, downgrade/raw fallback 없음 |
| L01 poll·idle | reviewed/pass, 실제 IPC의 status/거절 요청 뒤 IdleExpiresAt 불변; 8h 만료 경계 자체는 fake-clock 단위 검사로 구분 |
| L02 manual lock 재시작 지속 | reviewed/pass, 실제 별도 daemon crash와 두 실제 PTY 설치물 검사, 별도 소유 WSL2 distro의 두 실제 종료·재시작·암호 재입력·세션 재사용 |
| L06 silent native reopen | reviewed/pass, Mac 양 arch/Win 양 arch의 worker retirement/lease 뒤 unlock 입력 없이 새 session ID와 같은 값 실행; Linux 새 PAM session의 OS collection 해제 뒤 vault unlock보다 먼저 실행 성공 |
| 원문·신원·종료·정리 | reviewed/pass, 합성 stdin/anonymous pipe·masked output, 원래 process handle/직접 child·authenticated retire만 사용, own account/distro/HOME/keychain cleanup 뒤 성공 기록 |

추가 native 자동 재해제는 실제 OS API를 사용하는 cold-worker 경계를 통과했다. 이것이 wall-clock 8시간을 기다린 실험은 아니며, 만료 시간 의미는 fake clock, 실제 OS 연결은 native fixture로 각각 확인했다. 일반 status polling은 해제·수명을 연장하지 않는다. manual latch는 자동 OS 접근보다 먼저 적용된다.

리뷰 범위는 IPC transport·양쪽 peer·framing/작업 한도·session/lock/cancellation·protocol recovery·설치된 public CLI·WSL 재시작과 cleanup을 포함했다. Windows fixture의 실제 계정/console/readiness 문제와 root-command --profile 오류를 별도 커밋으로 수정하고 caller와 실제 양 architecture를 재검사했다. 남은 적격 결함은 없다. 기존 준비 문서의 pending은 이 실제 결과로 해소한다.

SMB loopback은 실제 SMB transport 검증이며 외부 물리 LAN host를 사용했다고 주장하지 않는다. 동일 사용자 DoS·root/OS 공격·dump/이미 전달된 환경 회수는 보장하지 않는다. native Keychain 앱 ACL은 유지했다. N05의 launch/restore fault matrix, A12의 전체 통합/성능, B/C와 Z01은 별도 필수 작업이다.
