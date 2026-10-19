# N04 실제 WSL 다중 터미널·daemon 중단 검증

N04는 진행 중이다. 아래 범위는 현재 설치한 candidate를 실제 WSL2 커널
`6.18.33.2-microsoft-standard-WSL2`에서 실행한 결과다. 다른 OS나 distro 재시작을
실행했다고 표시하지 않는다.

## 실행한 제품 흐름

`verify/scenarios/vault_session.py`는 별도 HOME에 패키징한 CLI를 실제 설치하고,
서로 다른 두 PTY에서 interactive Bash를 실행한다. private file로 최초 passphrase와
합성 secret을 준비한다. 같은 키 세션을 공유하며 명령마다 암호를 다시 전달하지 않는다.

두 번째 터미널의 잠금이 첫 번째 터미널의 protected write를 거절하는지 검사한다.
기존 session은 peer/vault/session/challenge를 확인한 control로 retire하고 실제 OS
lease를 drain한다. 이후 테스트가 직접 만든 Popen daemon을 유지해 신원을 잃지 않고
crash시킬 수 있도록 한다. 다른 터미널에서 해제·마스킹 실행·수동 잠금을 거친 뒤 이
직접 자식만 kill/wait한다. 다음 daemon에서도 manual latch가 유지되고 명시 해제 뒤
다른 터미널의 실행이 같은 세션을 재사용해야 한다.

TTY에서는 `--output redacted`를 명시한다. 일반 TTY의 raw 예외를 감춰 성공시키지 않는다.
원문 secret은 terminal evidence에 없어야 한다. 시작·출력·종료 대기와 버퍼를 제한하고,
실패 시 테스트가 소유한 PTY와 직접 child만 정리한다. stored PID나 프로세스 이름으로
다른 작업을 종료하지 않는다.

## 검토와 실제 근거

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 두 실제 terminal의 session 공유·lock 반영 | reviewed/pass WSL | 실제 설치 CLI와 두 PTY, protected write 거절·재사용 실행 |
| manual latch의 daemon crash 지속 | reviewed/pass WSL | 직접 Popen child kill/wait, 다음 daemon에서 locked_manual, 명시 해제 |
| private IPC와 종료 대상 | reviewed | 기존 사용자/세션 검증 control, actual lease, 직접 child 신원 유지 |
| fixture 실패 정리 | reviewed | 초기 prompt/daemon 시작 실패도 자체 child·FD 정리, bounded waits |
| 배포판 재시작 | pending | 현재 실행 환경에 Windows interop endpoint가 없고 wsl.exe 읽기 전용 호출도 실행되지 않음 |
| Windows pipe·다른 SID·macOS IPC | pending | native Actions 결과가 아직 없으며 Linux/WSL 결과로 대체하지 않음 |

실행: `GOCACHE=/tmp/devtools-values-go-cache python3 -B verify/run.py vault_session`
pass. 원시 결과 `/tmp/devtools-values-plan/N04-installed-two-terminals.log`는 kernel과
`distro_restart: NOT RUN`을 함께 기록한다. fixture cleanup 보완 뒤 전체 installed
suite도 통과했다. 현재 candidate의 `./scripts/project check`도 통과했다(새 vault race
27.515s; 관련 코드/조건이 변하지 않은 기존 검사 결과는 Go cache를 재사용). 이 source 범위에 확인된 적격 결함은 남지 않았다.

현재 N04 전체 완료는 아니다. 실제 distro restart·다른 사용자·Windows named pipe 등
원래 요구한 native 경계를 각각 확인한 뒤 판정한다. N05/A12/Z01의 필수 조건도 유지한다.
