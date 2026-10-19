# N04 실제 다른 Windows SID pipe 검증 준비

기존 native Windows suite가 만드는 standard account의 LogonUser primary token으로 실제
별도 process를 실행한다. DPAPI의 기존 thread impersonation 검사와 같은 계정 생성·SID
대조를 작은 test helper로 공유하며, 기존 DPAPI unwrap 거절·원래 사용자 복귀 검사는 유지한다.
CI에서 계정이 없으면 skip하지 않고 fail한다. credential은 부모 fixture에서만 사용하며
child argv/env/출력에 넣지 않는다.

각 자식은 별도 test binary copy로 실행되어 실제 process token의 SID를 반환한다. 부모는
이를 기대한 다른 SID와 대조한다. 임시 Public 디렉터리에는 공개 test 실행 파일만 놓고
key/session metadata나 사용자 자료를 쓰지 않는다. raw fixture pipe를 의도적으로 열어
parent client의 server PID/SID 검사와 parent server의 client PID/SID 검사를 각각 실행한다.
peer가 SID 확인 전에 종료되지 않도록 양쪽이 닫힐 때까지 자식이 대기한다. 이 자료에는
key frame이나 secret payload가 없다. 별도 case는 production newSessionPipe의 ACL이 같은
다른 사용자 process의 open 자체를 AccessDenied로 막는지 검사한다.

자식은 공개 SID 외 다른 데이터를 보내지 않으며 actual process handle만 종료·wait한다.
context/pipe deadline·자체 binary/root 정리를 적용한다. 제품 listener의 ACL·remote-client
거절·양쪽 사용자 검사·session challenge 검증을 바꾸지 않는다. 이 준비 자체가 원격 SMB
client 실험이나 동일 사용자 sandbox의 근거는 아니다.

리뷰 범위: primary token/실제 process 신원, 공개 fixture의 범위, 각 native peer consumer와
별도 ACL 방어, process/pipe/stdio/token 수명, timeout/cleanup, 기존 DPAPI 검사와 현재
Windows suite 계정 fixture를 확인했다. x64/ARM64 test binary compile과 vet를 수행한다.
실제 다른 SID process의 실행·거절 결과는 새 Windows CI에서 확인하기 전까지 pending이며
N04 전체 완료로 표시하지 않는다.

`9aa1bc5`의 두 architecture에서 CreateProcessAsUser 호출이 실제로 1314(필수 권한
부재)로 실패했다. 제품 pipe 검사에 도달하기 전의 fixture 실패다.
[Microsoft의 CreateProcessWithTokenW 계약](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-createprocesswithtokenw)에
따라 runner의 impersonation privilege를 사용하는 test 전용 launcher로 변경했다.
기존 LogonUser의 다른 SID primary token을 그대로 사용하며 OS 권한 정책을 수정하지 않는다.

절대 application 경로와 ComposeCommandLine, NUL stdin/stderr, 공개 SID 전용 stdout,
SystemRoot/WINDIR만 담은 명시 environment block을 전달한다. 자식의 credential 상속을
막으며 token/process/thread/pipe handle을 닫는다. 원래 소유한 live process handle로
종료하고 context 만료 시 kill한다. 세 native assertion과 SID 대조는 유지한다.
양 architecture compile/vet pass. 실제 생성과 pipe 거절은 후속 CI에서 확인한다.
