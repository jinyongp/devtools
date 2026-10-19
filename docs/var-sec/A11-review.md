# A11 제품 연결 검토

A11 제품 연결 범위의 구현·문서·Linux 설치물 검토를 완료했다.
원래 native 통합 수용과 첫 배포 판정은 아래 필수 gate에 남으며 이 기록으로 대체하지 않는다. 원래 설치물/
native 검증은 N01–N05/A12, 전체 변경 리뷰는 Z01의 필수 조건으로 유지한다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| vault init/status/unlock/lock | reviewed Linux | 실제 same-binary daemon으로 초기화·기존 secret migration·manual lock·틀린 암호 거절·재해제 검사 |
| 중단 복구 | reviewed Linux | key slot만 생성된 중단에서 migration 완료, 기존 암호화 forward 및 일반 transaction 복구 선택, 최종 catalog/keyset/각 secret layer 검증 |
| passive 상태 조회 | reviewed | daemon 생성·키 해제·idle touch 없이 상태 확인, protected data 옆의 key 누락을 새 빈 root로 표시하지 않음 |
| 명령 입력·schema | reviewed | passphrase argv/env 금지, 명시 파일/파이프 또는 숨김 terminal 입력, 비대화형 입력 없음 즉시 오류, native fallback 없음 |
| native 초기화 | source reviewed; native pending | OS의 기존 보호 서비스만 선택, 명시 interaction 옵션, isolated root에서는 native 변경 거절 |
| 긴 Unix 경로 IPC | reviewed Linux; macOS native pending | 실제 실패를 재현한 뒤 100byte 미만 소켓 경로 선택, root별 사용자 전용 private 디렉터리, 기존 OS peer·vault/session/nonce 검증 유지 |
| 기존 값/실행/backup provider | reviewed Linux | 공개 전용/보호 root 선택, 실제 재사용 세션, frozen foreground/managed 실행, 새 keyset/generation 전송 |
| dashboard 공통 store·managed 입력 / completion / doctor | reviewed Linux | passive metadata, 공통 frozen preparation·admission, 잠금 진단, lock 없는 이름 completion |
| dashboard 보호 상태 UI·vault 진단 요약 | reviewed Linux | 실제 browser keyboard lock/unlock·오입력 정리·passive polling·390px 화면, HTTP 인증 경계·JSON schema 검사 |
| 문서/Skill | reviewed current source | README 두 언어·CLI/backup/dashboard/진단·Skill의 저장/출력/해제 설명을 실제 동작과 대조; 배포 완료로 표시하지 않음 |

확인·수정한 결함:

- 긴 테스트/사용자 데이터 경로에서 Unix socket의 OS 길이 제한으로 daemon이 시작하지
  못했다. 기본 경로는 유지하고 긴 경우에만 root 경로와 사용자로 구분한 짧은 private
  디렉터리를 쓴다. payload나 키를 임시 파일에 쓰지 않으며 socket/빈 디렉터리 정리는
  listener close의 once 수명으로 묶었다. 실제 Linux client/server 통신으로 확인했다.
- 복구가 정상 ready marker만 보고 성공하면 손상 catalog도 복구 성공으로 보일 수 있었다.
  성공 전에 전체 catalog와 sealed profile/keyset/secret layer를 인증한다. 손상 fixture는
  integrity_failed로 거절하며 새 root나 빈 catalog로 대체하지 않는다.
- catalog 밖 공개 profile을 읽는 공통 경로에서도 파일 선택 이름과 profile 본문의 이름이
  일치해야 한다. 불일치는 integrity 오류로 닫는다.

실행 근거:

- cli/vault 전체 Go 회귀 통과(10.249s/7.837s).
- vault control 및 session/긴 경로 관련 race 통과(vault 5.993s, cli 2.797s).
- cli/vault vet 통과, Windows ARM64 CLI와 macOS ARM64 vault test binary 교차 빌드 통과.
- 실제 빌드한 devtools 실행 파일을 별도 isolated root에서 사용해 status → passphrase init/
  migration → manual lock → explicit unlock → verified recovery → schema 흐름을 확인했다.
  이 smoke test는 installer/native OS 검증으로 부르지 않는다. 생성한 daemon은 peer를
  확인한 기존 control handshake로 종료했다. 실제 사용자 저장소·native keyring은 건드리지 않았다.

이 단위의 검토에 남은 확인된 결함은 없다. pending 영역은 계속 구현한다.


## 공통 provider와 실제 제품 경로 연결

- 기존 var/sec/env/import는 ModernStore를 사용한다. 공개 전용 root는 vault 없이 동작한다.
  새 secret은 명시 init이 필요하며, secret 입력을 소비하기 전에 키 사용 가능 여부를
  확인한다. 실제 변경도 세션 접근을 다시 확인하므로 그사이 잠금이 생기면 쓰지 않는다.
  입력 읽는 동안 disk gate를 보유하지 않는다. 명령마다 passphrase를 다시 입력하지 않는다.
- 공개 조회·secret 이름·profile inspect/diff·completion은 메타데이터만 읽고 세션을
  열거나 idle 시간을 늘리지 않는다. 공개 조회와 dashboard 관리 view는
  integrity_verified=false를 명시한다. import 비교에는 placeholder 대신 인증된 실제
  상태를 사용하며 요청 값은 출력하지 않는다. doctor는 잠긴 세션을 조회만 해 fail과
  필요한 action을 보여주고, native 해제를 실행해 검사했다고 가장하지 않는다.
- 실제 실행과 managed 준비는 Root 유무에 맞는 snapshot/ticket을 사용한다. proxy
  binding을 변경하는 준비도 먼저 인증한다. dashboard process 경로가 기존 legacy
  start로 남아 있던 지점을 공통 PrepareFrozenInput으로 바꿨다. CLI/dashboard 모두
  old stop 전에 실제 값을 고정하고 같은 private launcher admission을 사용한다.
- managed handoff가 공개 ticket을 건너뛰던 결함을 확인해 수정했다. 실제 자식 프로세스
  재시작 준비 뒤 storage generation을 바꾸면 input_changed로 거절하고 이전 앱은
  running을 유지한다. dashboard factory도 실제 입력을 고정하는지 검사했다.
- backup/전송 factory가 보호 root에서 한 번 세션 키를 얻고 논리 archive를 사용한다.
  공개 전용 root는 provider를 호출하지 않고 같은 논리 형식으로 내보내며, 복원은 새
  로컬 ID/generation을 사용한다. 공개 전용의 동등 값 복원도 옛 preview를 부활시키지
  않는다. secret/sealed 이력을 공개 root에 새 평문 파일로 복원하지 않는다.
- launcher가 recovery/native 오류를 일반 launcher_unavailable로 뭉개던 code allowlist를
  현재 vault 오류에 맞췄다. 알려진 code만 내보내고 private message/body는 전달하지 않는다.
  일반 오류는 원문 없는 unlock/status remedy를 제공하며 임의 backend 변경을 제안하지 않는다.

검사: 전체 Go 회귀 통과. values/backup/execution/cli/dashboard/profiles 전체 race
통과(69.811s/46.204s/4.198s/54.449s/3.429s/1.775s). 공통 managed 준비 수정 뒤
cli/services/dashboard 전체 race 재검사도 통과(46.776s/9.175s/2.522s). 마지막 secret
입력 접근 선검사와 오류 remedy 수정은 해당 consumer 검사로 추가 검증한다. 관련 vet와
Windows ARM64 CLI/macOS ARM64 dashboard test compile도 통과했다. 교차 빌드는 native
실기를 대신하지 않는다.

실제 빌드한 CLI와 두 isolated root로 공개 변수 저장 → 명시 보호 설정 → secret 저장 →
기본 출력 마스킹 → 잠긴 상태의 metadata read/실행 거절 → 명시 해제 → 실제 age 전송/
새 root import → 마스킹 실행을 확인했다. 새 디스크 자료에는 합성 secret canary가
없음을 검사했고 두 session daemon은 peer 확인 뒤 종료했다. native keyring/실제 사용자
저장소는 사용하지 않았다. 이 단위의 범위에는 남은 확인된 결함이 없으며, pending UI/문서/
설치물·native gate는 계속 유지한다.

마지막 추가 검사: secret stdin 접근 선검사 race(cli 2.783s), invalid profile의 무접근
race(values 1.016s), locked doctor의 passive 상태 race(cli 1.747s), 공개 managed
재시작의 실제 old 유지 race(cli 6.945s), dashboard frozen factory race(1.108s) 통과.
새 secret 선검사 뒤 cli/values 전체 consumer 회귀도 통과(18.152s/13.961s).
마지막 오류 remedy/closed code 변경의 services/cli/values 검사가 통과했다.


## Dashboard 보호 상태와 진단

동일 origin·bearer 인증 경계에 전역 vault lock/unlock action과 passive 상태 조회를
연결했다. 초기 설정과 backend 변경은 CLI에 남긴다. 잠긴 상태에서는 metadata 탐색은
유지하고 보호된 값 변경을 막는다. unlock 입력은 전송 직후 DOM과 pending object에서
비우며, 응답 불명 상태의 retry에는 passphrase를 재사용하지 않는다. 이는 JS 문자열이나
HTTP body의 secure erase를 보장한다는 뜻은 아니다. 기존 실행의 값 회수도 약속하지 않는다.

진단은 backend·잠금 상태·명시 해제 필요 여부를 값 없이 반환한다. 상태 polling은
daemon 생성, native unlock, idle touch를 하지 않는다. deadline 변화만으로 editor를
다시 그리지 않는다. frontend 공개 prefix 이름에는 CLI/import/dashboard에서 경고를
제공하되 키 종류를 바꾸거나 입력을 차단하지 않는다. 이름은 노출 가능성의 단서다.

범위의 source/호출자/오류 경로 검토에서 남은 확인된 결함은 없다. 실제 browser에서
키보드 잠금·해제, 틀린 암호 뒤 빈 입력, focus 복귀, URL/storage canary, 5.5초 passive
polling의 동일 만료 시각, prefix 경고 표시/해제와 390×844 화면의 body overflow 없음을
확인했다. 마지막 즉시 입력 정리 변경 뒤 browser 재검사는 1 passed(7.2s).
전체 dashboard static 검사 33 passed, 영향 Go package race와 전체 Go 회귀 통과.
마지막 CLI/HTTP warning 및 diagnostics consumer 검사도 통과했다. 이는 native OS
보호 저장소·설치물 검증이나 전체 접근성 인증을 대신하지 않는다. 문서와 후속 gate는 남는다.


## 문서와 설치물 최종 연결 검토

CLI/dashboard/diagnostics/profile consumer, 공개 schema·help·completion, README 두 언어,
CLI/backup/windows/task/관리 API 안내와 배포되는 Skill을 검토했다. 별도 secret-protection
가이드는 최초 설정부터 재사용·잠금·기존 전환·복구·실제 보호 범위를 연결한다. 이전 설치
빌드와 현재 소스를 구분하고 native 검증을 완료했다고 쓰지 않는다. 문서의 session protocol
설명도 실제 version 2와 저장 HKDF version 1의 독립 계약으로 바로잡았다. 로컬 파일 링크가
존재함을 검사했다. 가이드의 OS별 실제 첫 설정은 native gate가 확인해야 한다.

설치 검증은 테스트 release를 실제 패키징·설치하고 각 소유 HOME에서 실행했다. secret을
쓰는 기존 시나리오만 명시 passphrase init을 하며 전역 자동 초기화로 오류를 숨기지 않는다.
runner는 종료 시 Linux peer UID/macOS getpeereid와 endpoint/session/challenge를 확인하는
value-free control로 자체 session을 retire한다. native keyring은 변경하지 않는다.

새 vault 시나리오는 public-only → 초기화 전 거절 → 비대화형 암호 없음 즉시 오류 →
명시 init/secret write → 재사용 → manual lock → 값 없는 secret 목록/공개 조회/진단/
completion → locked write 거절 → 오입력 거절 → 해제/recover → 실제 마스킹 실행을
확인한다. no sec get과 아직 미구현 bundle option 거절도 확인한다. 최초 테스트에서
completion에 argv를 전달했던 fixture 오류는 기존 NUL stdin 계약으로 수정했고 재실행했다.

최종 실행 근거:

- `./scripts/project check` 통과: Skill 공식 검증·재현 가능한 package/negative fixture,
  dashboard 33개, installer/release/project/perf 검사, 전체 gofmt/vet/Go race 포함.
  주요 race 결과 backup 58.962s, CLI 70.471s, values 87.651s, vault 37.286s.
- `python3 -B verify/run.py vault completion` 통과. Bash/zsh 실제 completion,
  interactive Bash Readline까지 확인했다. 미설치 fish는 이 호스트에서 확인하지 않았다.
- `python3 -B verify/run.py install tasks doctor ports backup profiles readiness output_redaction`
  전체 통과. 실제 설치/update·task/dashboard·진단·포트·전송/replay·readiness와 실제
  pipe/PTY/capture/느린 소비자 취소를 새 protected provider에 연결해 확인했다.
- 현재 repo의 Playwright file을 명시한 browser 검사 2 passed(10.1s). 별도 중첩 worktree의
  test를 실행한 결과와 혼합하지 않았다. Linux 결과를 Windows evidence로 기록하지 않는다.
- verify runner의 Git 격리와 timeout/cleanup 회귀 2 passed.

현재 A11 범위에서 남은 확인된 적격 결함은 없다. 전체 변경 일괄 리뷰 Z01, 실제 OS별
N01–N05와 A12, bundle/usage/변경 반영 B/C는 아직 완료가 아니다. 이 검토는 그 검증을
면제하거나 배포·push 권한을 추가하지 않는다.
