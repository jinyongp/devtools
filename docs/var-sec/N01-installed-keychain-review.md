# N01 실제 서명 설치·업데이트와 Keychain 연결 검증

기존 raw Keychain 실기와 전체 installed suite 사이의 연결을 실제 CLI로 확인한다.
macOS Intel/ARM의 disposable keychain wrapper 안에서 두 실제 production-build test
package에 OS codesign의 ad-hoc signature를 적용하고 strict verify한다. 이들은 합성
검증 package이며 배포 artifact로 게시하지 않는다. Developer ID/Gatekeeper/notarization
동작을 증명한다고 하지 않는다. 실제 CDHash를 읽고 두 버전이 달라야 한다.

wrapper의 synthetic keychain이 원래 UID의 selected default인 경우에만 진행한다.
CLI 파일 자료는 별도 synthetic HOME/XDG root를 사용하며 native mutation을 금지한
DEVTOOLS_ISOLATED_ROOT 모드를 우회 수정하지 않는다. 실제 installer로 v1을 설치하고
default Keychain vault·secret·masked run을 실행한다. manual lock과 실제 OS keychain
lock을 함께 거친 native unlock은 classified denial이어야 한다. keychain 명시 해제 뒤
같은 값을 다시 실행한다. 이후 devtools manual lock과 authenticated retirement/lease
drain을 완료하고 실제 self-update로 v2를 설치한다. 서명 verify와 새 process의 native
unlock·같은 secret 실행·같은 vault/backend 보존을 확인한다.

다른 executable signature의 접근을 old cached key로 대체하지 않는다. 사용자 개입이
필요한 실제 접근 실패가 나오면 fixture를 성공으로 낮추거나 Keychain ACL을 먼저
넓히지 않고 원래 계약과 실제 오류를 검토한다. 실패하더라도 전용 keychain을 unlock하여
worker 정리를 수행하고 wrapper가 원래 default/search를 복구·synthetic keychain을 삭제한다.

리뷰 영역: OS keychain 선택과 filesystem root 분리·합성 package ownership·실제
signing/update/checksum·locked denial·old-session drain·새 native access·같은 값과
key identity·secret 로그/argv 미유입·cleanup/timeout을 확인했다. Python syntax와
workflow YAML을 검사한다. Linux에서 macOS나 native 서명 통과를 주장하지 않는다.
실제 Intel/ARM 결과는 후속 필수 native jobs까지 pending이며 N01 전체는 아직 진행 중이다.

`aaf5fd0`의 두 architecture에서 새 HOME은 Keychain preferences도 비어 있어 default
조회가 실패했다. 아직 native CLI나 update에 도달하지 않았다. parent UID의 기존
default가 wrapper-owned keychain인지 먼저 확인한 뒤, 새 private HOME의 preferences와
search/default도 그 한 keychain으로 명시한다. 다른 keychain을 생성하거나 선택하지
않으며 부모 wrapper의 원래 selection 복원은 유지한다. 제품 Keychain ACL은 변경하지
않는다. 이후 CLI 실패는 원문 대신 제한된 error code와 command 이름만 기록한다.

`3200682`의 typed 오류로 vault/init의 invalid_argument를 확인했다. fixture가
profile 대상 명령의 helper를 root 전체 vault/version/update에도 적용하여 지원하지
않는 --profile을 붙인 원인이었다. 로컬 isolated root에서도 같은 오류를 재현했다.
helper는 실제 profile 명령에만 profile을 붙이며 root 명령은 자신의 vault root를
그대로 선택한다. 제품 schema나 native fallback을 바꾸지 않는다. 올바른 root 명령이
isolated native mutation을 계속 거절하는 것도 확인한다.

## 기존 앱별 보호를 유지한 실제 signature 경계

85e72de는 두 architecture에서 새 서명 이후 OS의 creator-app 규칙으로 unlock이
거절됨을 확인했다. 별도 user-level decrypt ACL 추가안은 자동 승인 검토가 명시 승인을
요구하여 미반영 상태로 보존했다. 기본 source에는 권한 확대를 넣지 않는다.

기존 계약의 명시 --interactive/typed denial 흐름에 맞춰 signed update의 결과와
새 앱 OS 승인을 구분한다. 새 서명은 비대화형 접근이 거절되어야 하고, 그 상태에서
수동 잠금·retirement를 완료한 뒤 사용자가 명시 선택한 이전 버전으로 실제 updater를
실행한다. 원래 서명으로 vault/같은 secret이 다시 열려야 한다. 이는 저장 값/키를
자동 rollback하는 경로가 아니라 이전 binary를 명시 복구하는 합성 fixture다.

이 검사는 OS 앱 승인을 우회하지 않고 update/denial/data 보존/recovery를 실제로
확인한다. headless CI에서 사람이 OS 승인 UI를 누른 것까지 주장하지 않는다.
기존 guide의 --interactive 지침과 승인 필요 상태를 명확히 했다. 최초의 더 강한
‘서명 변경 후에도 무승인 성공’ 기대는 원래 P01 계약의 필수 조건으로 정해진 것이
아니며, 이를 위해 앱별 보호를 자동 제거하지 않는다. 후속 Intel/ARM 실기 전에는
새 recovery 흐름을 pass로 기록하지 않는다.
