# N04 실제 loopback SMB pipe 거절 준비

기존 remote endpoint metadata 거절과 실제 다른 SID 검사를 유지하고 Windows UNC
transport를 직접 비교한다. 현재 사용자/SYSTEM DACL의 합성 control pipe는 remote
mode를 허용하여 `\\127.0.0.1\pipe\...`로 실제 open돼야 한다. 같은 user ACL을 쓰는
production newSessionPipe는 remote-client rejection flag 때문에 같은 경로가
AccessDenied로 거절돼야 한다. control open에 실패한 host를 remote 거절 성공으로
간주하지 않는다. 실제 다른 물리 머신이나 WAN/firewall 실험으로 표현하지 않는다.

[Microsoft remote-client modes](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-createnamedpipea)의
accept/reject 차이를 동일 보안 descriptor의 실제 transport에서 확인하려는 검사다.
값·key/session frame을 전송하지 않고 handle만 열고 닫는다. 대상 주소는 고정 loopback과
생성한 random pipe 이름뿐이다. SMB 서비스·방화벽·자격을 자동 변경하지 않는다.
GitHub host에 SMB reachability가 없으면 명시 실패로 남기고 원인을 검토한다.

리뷰: control/production ACL 동등성, remote-mode 유일 차이, loopback 고정과 parent
기본 Windows 신원, control의 정상 접근·production 거절, 실제 handle 정리·전체 60초
native preflight 제한, 다섯 parent/subtest pass 요구를 확인했다. 제품 pipe 구현은
변경하지 않는다. Windows 양 architecture compile/vet 및 workflow YAML을 검사한다.
실제 native 결과가 나오기 전에는 N04 완료나 remote transport pass로 기록하지 않는다.
