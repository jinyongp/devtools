# N04 실제 IPC 요청 경계 리뷰

기존 readSessionFrame의 memory-buffer 검사를 유지하고 별도 실제 daemon과 platform
transport를 통하는 요청을 추가했다. warm passphrase session에 다음 8개 요청을 보낸다:
중복 operation, unknown field, zero/과대 frame, 다른 vault, stale session, protocol 불일치,
이전 connection의 challenge 재사용. malformed 요청은 timeout까지 대기하는 대신
connection을 즉시 거절해야 한다. identity 불일치는 원문 없는 protocol 오류여야 한다.
모든 응답에서 key가 없음을 확인하며 실패 경로의 response key도 지운다.

각 거절 뒤 status가 원래 state와 정확한 idle expiry를 유지해야 한다. 마지막 정상
client는 원래 키를 사용해야 한다. key의 원문을 failure 메시지에 넣지 않는다. 부수적으로
private metadata의 endpoint만 바꿔 localhost/IP/외부 UNC pipe와 다른 local root endpoint가
ReadSessionEndpoint에서 거절되는지 검사한다. 이는 실제 원격 SMB client 공격 실험이
아니라 network 접근 이전 endpoint binding 검증이다. production pipe의 remote rejection
flag와 다른 사용자 SID 검사는 별도 영역으로 유지한다.

리뷰 영역: 별도 process/실제 IPC, strict frame 경계, ID/challenge/protocol, noTouch,
잘못된 lock operation 미실행, key 비전달과 정상 세션 보존, deadline/retirement,
metadata 검증과 transport dial 순서를 확인했다. runtime 코드는 변경하지 않는다.
Linux 해당 두 검사 race pass(1.809s 및 후속 두 검사 pass). Windows x64/ARM64 compile
pass. 기존 vault 전체 race와 실제 다른 OS의 native suite 결과를 별도로 확인한다.
이 추가만으로 N04 전체나 원격 SMB 실기 완료로 처리하지 않는다.
