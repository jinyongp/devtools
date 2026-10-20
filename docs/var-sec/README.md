# var/sec 검증과 구현 이력

[문서 목록](../README.md) · [사용자 설정](../secret-protection.md) · [릴리스 준비](../releases/v0.28.0.md)

로컬 var/sec 저장·키 세션·실행 보호, bundle·사용처, 명시 자동 반영의 구현과
단위·통합 리뷰를 완료했다. 사용자 작업은 위 가이드에서 시작한다.
이 디렉터리는 소스별 검증 근거이며 릴리스 게시나 사용자 저장소 전환의 증거가 아니다.

## 최종 판정부터 읽기

| 영역 | 현재 수용 기록 |
| --- | --- |
| batch 변경 이후 소스의 native OS·패키징 | [D02 비게시 릴리스 검증](D02-release-validation.md) |
| 이후 batch 변경·공유 조회·준비/커밋 분리 | [D01 후속 리뷰와 측정](D01-batch-mutation-review.md) |
| 전체 변경과 보장 범위 | [Z01 최종 리뷰](Z01-final-review.md) |
| 저장·키·세션·출력·복구·OS별 실기 | [A12 최종 리뷰](A12-final-review.md), [수용 기준 연결표](A12-acceptance-ledger.md) |
| bundle·사용처·실행별 입력 비교 | [B03 최종 리뷰](B03-final-review.md) |
| 기본 알림·명시 자동 restart·실패와 반복 제한 | [C03 최종 리뷰](C03-final-review.md) |
| 성능과 관찰 한계 | [A12 실행 비용](A12-final-performance-results.md), [C03 반영 관찰](C03-convergence-results.md) |

[Actions 39286793430](https://github.com/jinyongp/devtools/actions/runs/39286793430)의
native 검증 소스는 `1e4bebd6d955ff58409416fea77e8a4183b81253`이다.
최종 재실행 기준 필수 26개 성공, 게시용 3개 skip이다. 최초 PowerShell 충돌과
동일 소스의 성공 재실행을 함께 보존했다. 구현 마감 `8763df2`까지 제품 Go/UI는
이 native 소스와 같고 후속 문서·Skill·추가 로컬 검사 결과는 각 최종 리뷰에서 구분한다.
이후 문서 정비가 과거 native 실행에 포함됐다는 뜻은 아니다.

batch 변경과 잠금 분리는 위 native 실행 이후 추가된 제품 변경이다.
D01의 로컬 검증과 과거 native 검증을 합쳐 이번 소스의 native 수용으로 판단하지 않는다.
이후 수정 소스 `e16c431`의 native OS·패키징 성공은 D02에서 별도로 확인한다.

## 중간 기록 읽는 법

아래 파일은 각각 기록한 소스와 단계의 판단을 보존한다. `현재`, `pending`,
`후속 검증 필요`라는 표현도 그 기록 시점 기준이다. 현재 완료 여부는 위 최종 판정으로
확인한다. 과거 실패·미완료 결과를 성공으로 고쳐 쓰지 않는다.
`evidence/`의 원시 결과와 digest도 원래 대상 소스에만 적용한다.

## A — 저장·보호·실행 기반

- [A00 계약·참조 fixture 검토](A00-review.md)
- [A01 인증된 profile 저장 모델 검토](A01-review.md)
- [A02 암호 backend·keyset 검토](A02-review.md)
- [A03 macOS Keychain 어댑터 구현 검토](A03-review.md)
- [A04 Windows 사용자 DPAPI 구현 검토](A04-review.md)
- [A05 Linux Secret Service 구현 검토](A05-review.md)
- [A06 키 세션·사용자 IPC 구현 검토](A06-review.md)
- [A07 실행 입력 snapshot 검토](A07-review.md)
- [A08 launcher/admission 구현 검토](A08-review.md)
- [A09 출력 보호 검토](A09-review.md)
- [A10 저장 전환 검토 — 구현 중간 기록](A10-review.md)
- [A11 제품 연결 검토](A11-review.md)
- [A12 acceptance coverage ledger](A12-acceptance-ledger.md)
- [A12 archive KDF input bound](A12-archive-kdf-review.md)
- [A12 final runtime performance measurements](A12-final-performance-results.md)
- [A12 first-stage implementation acceptance](A12-final-review.md)
- [A12 installed metadata contract follow-up](A12-installed-usage-review.md)
- [A12 process memory harness review](A12-memory-harness-review.md)
- [A12 native bundle exchange verification](A12-native-bundle-exchange-review.md)
- [A12 native session call accounting — prerequisite review](A12-native-count-review.md)
- [A12 성능 비교 fixture 준비](A12-performance-preparation-review.md)
- [A12 redactor 측정 준비와 리뷰](A12-redactor-benchmark-review.md)
- [A12 macOS recovery-fixture deadline follow-up](A12-rewrap-deadline-review.md)
- [A12 cold passphrase와 warm key session 측정 준비](A12-session-benchmark-review.md)
- [A12 warm-session matched 측정 결과](A12-warm-session-results.md)

## B — 묶음과 사용처

- [B01 dashboard bundle 관리·import 리뷰](B01-dashboard-review.md)
- [B01 config·foreground 실행 선택 연결 리뷰](B01-execution-review.md)
- [B01 공통 value bundle 전체 범위 최종 리뷰](B01-final-review.md)
- [B01 managed process·project batch 선택 지속 리뷰](B01-managed-review.md)
- [B01 bundle 일괄 관리·import·CLI 하위 작업 리뷰](B01-management-review.md)
- [B01 bundle 모델·선택 하위 작업 리뷰](B01-model-review.md)
- [B01 실제 값 snapshot의 bundle 선택 리뷰](B01-snapshot-review.md)
- [B02 수명 연장 없는 인증용 cached inspection 리뷰](B02-cached-inspection-review.md)
- [B02 delivered input versions](B02-delivery-version-review.md)
- [B02 usage reporting final review](B02-usage-review.md)
- [B03 bundle/usage stage acceptance review](B03-final-review.md)

## C — 변경 알림과 자동 반영

- [C01 managed watcher consumer review](C01-consumer-review.md)
- [C01 commit hints and reconciliation engine](C01-hints-engine-review.md)
- [C02 automatic coordinator integration — final unit review](C02-auto-coordinator-review.md)
- [C02 final implementation acceptance](C02-final-review.md)
- [C02 automatic goal admission guard — prerequisite unit](C02-goal-guard-review.md)
- [C02 installed proxy automatic-update journey](C02-installed-proxy-review.md)
- [C02 Windows coordinator crash-fixture follow-up](C02-windows-crash-fixture-review.md)
- [C03 actual convergence measurement harness review](C03-convergence-harness-review.md)
- [C03 managed value convergence results](C03-convergence-results.md)
- [C03 automatic application final integration review](C03-final-review.md)

## N — OS별 실기와 복구

- [N01 native 수용 실행 준비 검토](N01-ci-preparation-review.md)
- [N01 macOS 설치·업데이트·Keychain 최종 리뷰](N01-final-review.md)
- [N01 실제 서명 설치·업데이트와 Keychain 연결 검증](N01-installed-keychain-review.md)
- [N02 Windows DPAPI 다른 사용자 검증 준비](N02-ci-preparation-review.md)
- [N02/A12 Windows x64 emulation installer 실패 진단](N02-emulated-installer-diagnostics-review.md)
- [N02 Windows x64·ARM64 native 최종 리뷰](N02-final-review.md)
- [N02 설치된 CLI의 기본 DPAPI 연결 검증 준비](N02-installed-native-preparation-review.md)
- [N02 native 회귀에서 확인한 accepted stop 결과 보존](N02-stop-response-loss-review.md)
- [N03 실제 Linux Secret Service 수용 준비](N03-ci-preparation-review.md)
- [N03 실제 Linux 수용과 최종 범위 리뷰](N03-final-review.md)
- [N03 실제 PAM/logind logout 검증 준비](N03-logout-preparation-review.md)
- [N04 IPC 한계·폭주 수용 보강 리뷰](N04-capacity-review.md)
- [N04 native IPC·WSL·자동 재해제 최종 리뷰](N04-final-review.md)
- [N04 실제 IPC 요청 경계 리뷰](N04-ipc-frame-review.md)
- [N04 실제 WSL 다중 터미널·daemon 중단 검증](N04-local-wsl-review.md)
- [N04 실제 native 자동 재해제 연결](N04-native-reopen-review.md)
- [N04 실제 loopback SMB pipe 거절 준비](N04-remote-pipe-review.md)
- [N04 실제 다른 Unix 사용자 peer 검증 준비](N04-unix-peer-preparation-review.md)
- [N04 실제 다른 Windows SID pipe 검증 준비](N04-windows-peer-preparation-review.md)
- [N04 폐기 가능한 WSL2 재시작 검증 준비](N04-wsl-restart-preparation-review.md)
- [N05 실제 이전 CLI의 보호 저장소 접근 수용 보강](N05-actual-legacy-review.md)
- [N05 actual restart-client death review](N05-client-crash-review.md)
- [N05 native execution / migration acceptance](N05-final-review.md)
- [N05 cross-device identity acceptance follow-up](N05-generation-exchange-review.md)

## 전체 리뷰와 CI 이력

- [Z01 전체 변경 최종 리뷰](Z01-final-review.md)
- [Native CI 수정 검토](native-ci-review.md)
