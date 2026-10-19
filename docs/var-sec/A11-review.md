# A11 제품 연결 검토

A11은 진행 중이다. 이 기록은 먼저 연결한 관리 명령 단위의 검토이며, 기존 var/sec·실행·
backup·dashboard의 provider 연결이나 전체 A11 수용 완료를 뜻하지 않는다. 원래 설치물/
native 검증은 N01–N05/A12, 전체 변경 리뷰는 Z01의 필수 조건으로 유지한다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| vault init/status/unlock/lock | reviewed Linux | 실제 same-binary daemon으로 초기화·기존 secret migration·manual lock·틀린 암호 거절·재해제 검사 |
| 중단 복구 | reviewed Linux | key slot만 생성된 중단에서 migration 완료, 기존 암호화 forward 및 일반 transaction 복구 선택, 최종 catalog/keyset/각 secret layer 검증 |
| passive 상태 조회 | reviewed | daemon 생성·키 해제·idle touch 없이 상태 확인, protected data 옆의 key 누락을 새 빈 root로 표시하지 않음 |
| 명령 입력·schema | reviewed | passphrase argv/env 금지, 명시 파일/파이프 또는 숨김 terminal 입력, 비대화형 입력 없음 즉시 오류, native fallback 없음 |
| native 초기화 | source reviewed; native pending | OS의 기존 보호 서비스만 선택, 명시 interaction 옵션, isolated root에서는 native 변경 거절 |
| 긴 Unix 경로 IPC | reviewed Linux; macOS native pending | 실제 실패를 재현한 뒤 100byte 미만 소켓 경로 선택, root별 사용자 전용 private 디렉터리, 기존 OS peer·vault/session/nonce 검증 유지 |
| 기존 값/실행/backup provider | pending | 다음 구현 단위 |
| dashboard/진단/completion/문서/Skill | pending | 다음 구현 단위 |

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
