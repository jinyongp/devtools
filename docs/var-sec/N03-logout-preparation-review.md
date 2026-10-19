# N03 실제 PAM/logind logout 검증 준비

실제 disposable Ubuntu CI에서 새 standard 계정·private HOME·고유 PAM service만 만든다.
기존 runner 사용자나 현재 개발 WSL의 세션은 대상으로 삼지 않는다. 계정 이름 충돌은
거절하고 PAM 파일은 exclusive create한다. service는 인증을 항상 거절하고 account를
새 사용자로 제한한다. 시스템 공통 PAM 설정이나 기존 사용자의 정책은 바꾸지 않는다.

[systemd PAMName](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)으로
실제 session을 열고 [pam_systemd](https://www.freedesktop.org/software/systemd/man/latest/pam_systemd.html)의
XDG_SESSION_ID·runtime을 확인한다. 첫 세션에서 실제 GNOME native protection으로
CLI vault/secret을 만들고 실행 출력 마스킹과 reusable key session을 확인한다.
부모는 logind의 User/Name/Service가 모두 만든 fixture와 일치할 때만
[terminate-session/user](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html)를
호출하고 해당 사용자 runtime 소멸을 확인한다. 저장된 PID를 사용하지 않는다.

둘째 실제 login session은 같은 encrypted 값·keyring을 유지한다. collection 재해제
전 secret mutation이 key_backend_locked로 거절되어야 하며 warm key cache가 logout을
넘어 살아남으면 실패한다. 동일 UID native GNOME control peer를 확인한 뒤 합성 암호로
명시 unlock하고 같은 값의 injected/masked 실행을 확인한다. OS와 vault session ID가
모두 바뀌어야 한다. ID와 challenge 원문은 artifact에 저장하지 않는다.

모든 경로에서 새 계정 신원을 재확인하고 자기 user/unit/PAM/HOME만 정리한다. unit은
생성 성공 뒤 소유 목록에 추가한다. cleanup 실패는 pass가 아니며 원인을 남긴다.
자식의 stdout/stderr·환경·secret은 공유 로그로 전달하지 않고 합성 암호는 stdin과
GNOME control frame에서만 사용한다. root fixture stderr는 별도 CI artifact로 남긴다.

리뷰 영역: ownership/partial setup·종료 대상 선택·PAM authentication 차단·실제 session
신원·native collection/cache 재사용 거절·명시 unlock/data 보존·timeout·cleanup·증거
schema를 검토했다. 비 disposable/non-root guard를 mock으로 검사하여 실제 OS 변경이
일어나지 않음을 확인했다. Python syntax와 workflow YAML을 검사한다. 실제 로그아웃
결과는 새 CI의 필수 단계가 통과하기 전까지 pending이다. GUI 잠금·최대절전 검증은
이 fixture로 주장하지 않는다.

추가 준비 검토: PAM session 등록과 user bus socket 준비는 별도이므로 daemon 시작 전에
해당 runtime의 bus 준비를 최대 20초 기다린다. 준비 실패를 정상 로그아웃 결과로
해석하지 않는다. release-check 전체 pass와 PyYAML을 통한 workflow parse pass.
실제 root/PAM 실행은 disposable Actions 전용이며 로컬 guard 검사로 대체하지 않는다.

`452317f`의 첫 actual PAM fixture는 readiness file을 만들지 못해 실패했다. 해당
artifact에는 parent timeout만 있어 어느 phase 내부 조건이 실패했는지 확정할 수 없다.
bus readiness를 기다리는 후속 수정과 함께, 실패 시 성공적으로 생성한 자기 unit의
마지막 40개 journal entry(최대 12KiB)만 별도 stderr로 보존한다. 알려진 synthetic
암호/secret을 제거하며 시스템 전체 journal이나 다른 사용자의 unit은 읽지 않는다.
이 실패를 logout 통과나 특정 backend 결함으로 해석하지 않는다.

`0f2bb92`에서는 실제 PAM account 조건 통과 후 phase가 exit=1로 종료된 사실을
확인했다. PAM은 프로세스를 login scope로 옮기므로 원래 service의 journal만으로
Python 내부 오류를 얻지 못할 수 있다. scope 전체 journal을 확대 수집하는 대신
소유 HOME의 고정 private error file로 제한된 오류 type/message를 전달한다. 알려진
합성 값은 제거하고 parent는 실패를 즉시 인식한다. 이 자료는 실제 검증의 성공으로
취급하지 않는다. stderr routing도 해당 unit 안에서만 journal로 명시한다.

`aaf5fd0`의 private error file로 첫 session의 vault API 실패까지 좁혔다. 아직
logout 이전이다. init/status 등 subcommand와 원문 없는 제한된 API error code를
함께 남기도록 보완한다. raw stderr·native key·환경 원문을 수집하지 않는다.
