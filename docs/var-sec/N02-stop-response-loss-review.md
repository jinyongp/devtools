# N02 native 회귀에서 확인한 accepted stop 결과 보존

`0f2bb92` Windows x64 suite의 기존 restart 검사에서 stopped old execution을 반환하면서
changed=false가 나왔다. 실제 supervisor가 stop을 저장한 뒤 HTTP reply를 잃으면 stop의
reconciliation은 이미 terminal인 record를 읽고 자체 write가 없다는 이유로 changed=false를
반환했다. 그 뒤 새 시작 preflight가 실패하면 이미 관측한 중지 상태 변화까지 사라졌다.
이는 실행 실패 시 저장/중지/새 실행 결과를 분리하는 기존 계약을 위반한다.

실제 HTTP client deadline을 사용해 supervisor가 durable stop을 저장하고 reply만 잃도록
확장했다. 수정 전 Linux에서도 같은 기존 assertion이 실패했다. stop 시작 때 active였던
record가 시도 후 terminal로 관측됐으면 changed=true를 유지하도록 수정한다. 재시작 성공을
의미하지 않으며 Item은 실제 stopped old execution으로 남는다. 시작 전부터 ended였던
record는 기존대로 변경 없음이고 terminal 전환을 모르는 상태도 성공으로 바꾸지 않는다.

리뷰 영역: stop의 기존 terminal 조기 반환·실패 RPC와 lease reconciliation·restart의
preflight/결과 병합·pending receipt와 같은 UUID 재시도·new execution 미생성을 확인했다.
정상 응답과 응답 유실 두 경우를 모두 검사하고 같은 UUID 재시도는 changed/replayed를
유지하며 stop RPC를 반복하지 않아야 한다. 실제 예외 경로의 상태를 단정하지 않는 기존
unknown/recovery 규칙은 유지한다.

검증: 수정 전 응답 유실 재현 실패; 수정 후 두 case와 재시도 race pass. services/lifecycle
전체 race pass. Windows 실기는 새 source suite로 다시 확인하며 해당 이전 CI 실패를
통과로 덮어쓰지 않는다. 전체 N02/N05/A12 완료 판정은 각 native·통합 조건에 별도로 묶인다.
