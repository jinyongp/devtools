# A08 launcher/admission 구현 검토

대상 기준은 A07 `0a0d3ad` 이후의 launcher binding, key-session ticket,
영속 launch journal과 OS gate, foreground/managed 소비자, deferred port claim,
재시작 결과와 요청 재시도다. 모든 실행 입력은 RAM과 특정 자식에 상속한 anonymous
pipe로만 전달한다. secret payload를 launcher의 argv/env·디스크·공개 receipt에 싣지 않는다.
부모 환경의 inherit 기본 정책과 동일 사용자 악성 코드에 대한 보장 한계는 유지한다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| key delivery와 admission 연결 | reviewed | 같은 delivery의 session/vault/epoch/만료 ticket, 현재 unlocked cache와 OS peer 확인; 새 session으로 묵시적 갱신 없음 |
| 실제 launcher 소유권 | reviewed | 기존 inherited private lease 검증 뒤 payload 수신; input ID와 profile/command/env/directory 일치; 한 pipe로 bound → admit → commit |
| 잠금 순서·crash | reviewed Linux | durable manual intent 뒤 실제 launcher의 shared OS gate drain; daemon 교체에도 gate/journal 유지; 불확실한 결과는 lock_pending |
| immutable 실행 | reviewed | snapshot의 실제 argv/env/executable/readiness 사용; old stop 뒤 값·설정 재조회 없음; 실행 파일 자체의 교체까지 방지하는 sandbox는 아님 |
| 재시작과 실패 | reviewed Linux | 승인 전 취소는 old 유지, 승인 뒤 lock은 spawn/확정 실패까지 대기; post-stop 실패의 outage와 readiness 분리 |
| foreground 소비자 | reviewed Linux | 동일 snapshot ticket을 실행 프로세스가 소유, Start 경계에서 admission 종료; 같은 payload의 재실행 거절 |
| port 소비자 | reviewed Linux | 값·binding·할당은 미리 고정, old stop 뒤 해당 할당의 유효성/가용성 확인과 claim만 수행; latest 값 대체 없음 |
| 요청 재시도 | reviewed Linux | execution ID/opaque input ID/목표를 고정; frozen 입력이 있는 terminal 실패의 batch 재시도는 새 UUID로 바꾸지 않음 |
| 공개 출력·저장 | reviewed | 원문 fmt/JSON 금지 유지, input ID는 random; plaintext digest로 추측 oracle을 만들지 않음; control task context 미상속 |
| 복구·키 회전과의 결합 | pending A10/A12 | restore/rotation은 admission gate 및 generation 변경과 연결하고 실제 fault matrix를 재검증해야 함 |
| native 실기 | pending A12 | Windows x64/ARM64, macOS, 실제 WSL 재시작은 기존 Actions/통합 gate에서 확인; cross build를 실기로 세지 않음 |

Linux 실제 key-session daemon, launcher, 앱 프로세스와 실제 encrypted profile을
통과한 검사를 추가했다. lock-before는 old 유지, admit-before는 old stop의 파일 barrier에서
lock 응답이 보류되고 새 OS spawn 뒤 완료됨을 확인했다. snapshot 뒤 config/저장 값을 바꿔도
고정된 값으로 실행한다. disposable executable을 old stop 뒤 제거하면 outage로 실패하며,
같은 UUID를 다시 보내도 새 실행을 만들지 않는다. foreground도 같은 잠금·single-use
계약을 확인했다. 테스트 secret과 저장소·실행 파일은 모두 합성·격리 자원이다.

launcher를 실제 Process handle로 강제 종료한 경우에는 durable unknown이 남고,
coordinator를 잠금 drain 중 강제 종료한 경우에는 successor가 기존 launcher의 실제
OS gate를 기다린다. PID 파일을 근거로 종료하거나 unknown을 취소로 추정하지 않는다.
unknown journal의 수동 복구 UX와 restore 결합은 후속 통합 범위이며, 여기서 성공이나
안전한 자동 취소로 표시하지 않는다.

검토 중 수정한 결함:

- supervisor가 PATH를 다시 검색하던 지점을 frozen absolute executable로 바꿨다.
- 실행 admission의 종료를 앱 exit가 아닌 실제 Start로 옮겼다. Unix publication 실패도
  버리지 않고 전달하며, foreground abort 시 소유 process group을 정리한다.
- public payload ID를 원문 hash 대신 random ID로 고정하고 복제 payload에도 유지한다.
- deferred port 준비와 actual claim을 분리해 old listener 때문에 준비 자체가 실패하지
  않도록 했다. late claim은 저장 값이나 config를 다시 물질화하지 않는다.
- 승인 전 후보 취소를 post-stop outage로 오인하지 않도록 public phase/이전 execution의
  완료 상태로 판단한다. 해당 상태 변경은 supervisor mutex 안에서 수행한다.
- 기존 run의 `--isolated-root` 후처리도 snapshot 이후에는 반영되지 않으므로 내부
  isolation overlay를 물질화 전에 적용했다. 명시 flag가 부모 환경보다 우선함을 검사했다.
- 값 복사/transport로 재시도 때 새 입력을 자동 선택하거나 terminal batch 실패를
  새 child UUID로 재실행하지 않도록 했다.

실행한 검사: 전체 Go suite, 영향 8 package race/vet, Windows amd64/arm64·Darwin
amd64/arm64·Linux arm64 CLI build/vet/CLI test compile. 실제 설치물의 processes,
project_lifecycle, readiness, cancellation 및 foreground 전환 뒤 ports, doctor,
cancellation 시나리오가 통과했다. 마지막 scoped source 수정 뒤 영향 검사를 재실행한다.
Raw log는 `/tmp/devtools-values-plan/A08-*.log`에 둔다.

이 단위 커밋은 A08의 구현 checkpoint다. restore와의 결합 및 native 통합 근거가 남아 있어
A08 전체를 완료로 기록하지 않는다. 기존 legacy 저장소의 암호화 전환과 모든 읽기·쓰기
경로의 vault 연결은 A10/A11에서 완료해야 한다. 출력 redactor는 A09에 남는다.

## 공개 전용 실행과 저장소 전환 결합

공개 전용 root는 키/세션을 만들지 않고 PublicLaunchTicket으로 profile ID와 storage
generation(없는 profile은 명시적 absence)을 고정한다. 실제 launcher가 shared OS lease를
얻은 뒤 disk gate 안에서 root 보호 상태와 incarnation을 확인한다. 일반 값 수정은 준비한
원문 snapshot을 유지하며, 복원·최초 보호 설정·first-secret 승격은 오래된 준비를 거절한다.
public journal도 실제 spawn 완료까지 unknown이며, lease만 사라졌다는 이유로 성공/실패를
추정하지 않는다. 새로운 root key slot publication 역시 같은 배타 launch barrier와 disk
gate를 거치며 native wrap/실제 unwrap은 두 gate 전에 끝낸다.

이미 보호된 root의 공개 profile은 같은 root session으로 catalog 분류를 인증하고 key epoch
0을 명시한다. first-secret 승격 뒤에는 이전 epoch 0 ticket이 통과하지 않는다. public-only
profile 자체의 integrity_verified는 false다. 준비 metadata 복사로 내부 ticket을 바꿀 수 없다.
없는 공개 profile의 effective revision은 매 실행 바뀌지 않는다. 보호 root에 없는 profile의
최초 실행은 안정된 공개 identity를 저장한다. 인증하지 못한 legacy secret은 실행하지 않는다.

실제 public child spawn/고정 값, spawn outcome drain, 공유 lease에 의한 세대 변경 보류,
동등 값 복원 뒤 옛 generation 거절, absent→created 거절, 보호 설정 뒤 unsigned 실행 거절,
공개 unknown의 보호 설정 차단, root session의 public→sealed ticket 거절을 검증했다.
기존 sealed restore/key rotation admission 검사는 A10 범위에서 이미 추가했고 이번 회귀에
포함한다. 최종 제품 provider 연결과 native/설치물 통합 검증은 A11/A12와 별도 필수 검증으로
유지하며 이 로컬 단위만으로 최종 수용 완료를 선언하지 않는다.

검증: 전체 Go 회귀 통과. vault/values/execution/cli/services/lifecycle/process 전체 race
검사 통과(33.487s/75.036s/4.454s/46.962s/12.428s/2.211s/2.164s), 관련 vet 통과.
마지막 추가한 보호 root의 빈 공개 profile identity 검사도 별도 race 통과. Windows ARM64
vault 및 macOS ARM64 execution test binary 교차 빌드 통과. 이 단위의 source 리뷰에
남은 확인된 결함은 없다. 원래 설치물/native 최종 요구는 N05에 그대로 보존하며 A11 연결
뒤 수행한다. A08/A10 source 완료가 N05/A12/Z01의 통과를 의미하지 않는다.
