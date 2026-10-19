# N04 폐기 가능한 WSL2 재시작 검증 준비

현재 개발 WSL의 두 실제 PTY·daemon crash 검사는 N04-local-wsl-review에 있다.
배포판 종료/재시작은 별도 조건이며 이 준비 기록이나 mock 검사로 pass 처리하지 않는다.

release workflow에 `windows-2025`의 별도 `wsl-vault`를 추가하고 package의 필수
dependency로 연결한다. 새 Action이나 runner 권한은 추가하지 않는다. 후보 Linux
binary의 commit을 넣고 실제 WSL2 실행의 version 응답과 대조한다. WSL2/kernel 지원이
없으면 실패 evidence를 남기며 WSL1 또는 skip으로 통과하지 않는다.

[Canonical 공식 이미지 목록](https://cloud-images.ubuntu.com/wsl/releases/24.04/current/)의
24.04 initial amd64 rootfs와 공개 SHA256을 고정한다. 테스트만을 위한 이미지이며 제품
의존성이 아니다. UUID 이름을 기존 배포판 목록과 대조한 후 `--import --version 2`로
자체 배포판을 만든다. [Microsoft의 명령 계약](https://learn.microsoft.com/windows/wsl/basic-commands)에
따라 그 이름에만 `--terminate`·`--unregister`를 사용한다. 전역 shutdown·기존 배포판
변경은 없다. 부분 import도 목록을 확인해 자체 이름만 정리하며 확인/삭제 실패 때
recovery root를 남긴다. unregister 확인 전 VHD 파일을 삭제하지 않는다.

배포판 안에서는 전용 non-root 사용자와 Linux filesystem을 확인한다. 합성 passphrase
root를 명시 설정하고 secret을 실행에 주입해 마스킹을 검사한다. manual lock 후 배포판을
종료·재시작하여 `locked_manual`, secret write 거절, 명시 unlock 뒤 두 process 재사용을
검사한다. unlocked 상태에서도 다시 종료·재시작해 암호 세션이 자동 재개되지 않는지
검사한다. 원문을 argv/env/증거에 넣지 않으며 값은 자체 배포판의 private input 파일에만
둔다. 정리 성공까지 확인한 뒤에만 evidence result를 pass로 표시한다.

리뷰 범위: revision/바이너리 identity, distro 소유권, 부분 실패/cleanup, subprocess timeout,
두 restart 상태와 raw output canary, native 필수 gate를 확인했다. Python AST와 현재
lockfile의 YAML parser를 통과했다. 기존 verify runner 2개와 새 ownership failure 검사
3개가 pass했다. 후자의 mock은 기존 distro 무변경, 부분 import cleanup, unregister 실패 시
복구 파일 보존을 검사하며 실제 WSL2 재시작의 근거가 아니다. 실제 결과는 Actions에서
별도로 확인하고 실패를 수정한다.

## 실제 WSL2 결과

[Actions 39233624557의 wsl-vault job](https://github.com/jinyongp/devtools/actions/runs/39233624557/job/117997371744)은
후보 `de6fd4b6f7459fce2f446bd7e367e23378409296`에서 pass했다. artifact를 내려받아
세 phase의 실제 pass와 같은 binary commit, `checks_passed`, `owned_distribution_removed`를
확인했다. kernel은 `6.18.40.1-microsoft-standard-WSL2`, Linux filesystem `ext2/ext3`,
실행 UID는 1001이었다. manual lock 유지와 unlocked session 소실, 명시 unlock 후 별도
프로세스의 재사용·마스킹이 두 실제 distro 종료/재시작을 넘어 확인됐다.

[원본 JSON 증거](evidence/N04-wsl-restart-de6fd4b.json)를 보존한다. 이 검사는 실제 kernel과
등록된 독립 distro를 사용했으며 현재 개발 WSL이나 다른 기존 배포판을 종료하지 않았다.
다른 OS 사용자·Windows pipe 양쪽 peer 확인 등 나머지 N04 범위는 이 pass로 대체하지 않는다.
