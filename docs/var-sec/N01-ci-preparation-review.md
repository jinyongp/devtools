# N01 native 수용 실행 준비 검토

N01은 진행 중이다. 이 기록은 CI 실행 경로와 fixture 준비만 검토하며 macOS native 수용
완료 근거가 아니다. A11은 `38f07c9`에서 제품 연결·Linux 설치물·review 검증을 완료했다.
N01–N05와 A12, B/C, 최종 Z01의 원래 완료 조건은 유지한다.

## 검토 범위

| 영역 | 상태 | 근거와 한계 |
| --- | --- | --- |
| 기존 native profile 전송 producer/consumer | source reviewed | secret 전에 명시 passphrase root를 설정하고 대상도 별도 설정; 논리 자료와 새 로컬 키 세대를 사용 |
| Windows 설치물 fixture | source reviewed; native pending | source/target 각각 키 세션을 명시 준비; 기존 실제 설치/update·실행·전송 검사는 유지 |
| fixture 세션 종료 | reviewed Linux; other OS pending | 실제 공유 SessionClient의 OS peer·vault/session/challenge 검증과 value-free retire; OS lease 반환 확인 뒤 삭제 |
| macOS Keychain 전용 job | source reviewed; native pending | Intel/ARM runner 실제 arch 확인, disposable keychain만 선택, 원래 default/search 복원 후 합성 keychain 삭제 |
| 실제 잠긴 collection | compile checked; native pending | 기존 실제 roundtrip/duplicate/다른 vault/missing 검사에 silent 거절과 해제 뒤 원래 키 일치를 추가 |
| package gate | source reviewed | 새 Keychain job이 package dependency여서 실패를 배포 성공으로 간주하지 않음 |

현재 기존 전송 fixture는 init 없이 secret 저장을 시도하므로 새 제품 계약에서 실패한다.
이를 명시 setup으로 수정했다. 전역 암묵 초기화나 native keyring fallback은 넣지 않았다.
새 helper는 키를 얻거나 session을 시작하지 않는다. caller가 지정한 절대 test 경로의
기존 endpoint에 retire만 요청하고, 실제 daemon lease를 확인한다. stored PID 기반 종료나
임의 프로세스 탐색은 없다. helper 자신은 삭제할 root 밖에 빌드한다.

macOS runner는 [GitHub 공식 runner 표](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)의
`macos-15-intel`/`macos-15`를 사용하고 `GOHOSTARCH`로 실제 실행을 확인한다. 별도 임시
keychain은 CI가 명시 disposable host라고 지정할 때만 만든다. 빈 password는 이 합성
keychain에만 사용한다. 실제 사용자 keychain을 잠그는 test는 제공하지 않는다.
제품 init은 여전히 OS 서비스를 자동 설치·시작하지 않는다.

## 실제 로컬 검사

- 현재 Linux에서 native 전송 producer를 실제 실행하여 암호화 archive와 값 없는 manifest를
  생성하고 session retire/lease drain 뒤 임시 root를 정리했다. 다른 OS producer로 부르지 않는다.
- release contract/Windows validation reuse/verify runner 관련 기존 검사 통과.
  기존 lockfile의 YAML parser로 workflow 문법과 package 의존성을 확인했다.
  전송 consumer job에는 helper 빌드에 필요한 같은 Go 버전 setup도 추가했다.
  shared vault 회귀 8.616s와 helper vet도 통과했다.
- 새 Python fixture를 compile로 확인했고 Go helper는 Linux 빌드, Windows ARM64 교차
  빌드를 확인했다. 새 Darwin test는 amd64/ARM64 교차 compile로 확인했다.
- macOS/Windows 실기, 12방향 producer/consumer와 실제 OS 로그인·설치/update·거부의
  결과는 아직 없다. 실제 runner 실행이 필요하며 compile이나 준비 코드로 대체하지 않는다.

source 범위 재검토에서 확인된 적격 결함은 남지 않았다. CI native 결과를 받은 뒤 해당
검사를 재검토하고 실패를 수정해야 하며, 현재 N01 완료로 표시하지 않는다. 게시/push나
Actions dispatch는 수행하지 않았다.
