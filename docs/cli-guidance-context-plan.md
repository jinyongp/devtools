# CLI 안내 계약 복구와 InvocationContext 구현 계획

상태: 구현 전 조사·설계 완료, 구현 및 구현 후 검증 미착수. 기록일: 2026-10-23 KST.
기준 소스: `276a78e91479321ba751c5555769a39e6ad05e07`, CLI protocol 12.
등록 대상: devtools profile `devtools`. 이 문서는 workstream spec에 본문으로 보존한다.

## 범위와 완료 결과

첨부 CLI audit의 C01–C11을 수정한다. 이 문서에서는 기존
`docs/cli-guidance-audit.md`의 C01–C03과 충돌하지 않도록 `AUD-C01`처럼 표기한다.
실패한 대상·저장소·선택 범위가 오류, 중첩 검사, 후속 행동과 성공 응답의 안내까지
유지되어야 한다. 공개 정보만으로 상태·변경 효과·재시도 조건을 판단할 수 있어야 한다.

공통 InvocationContext와 action 생성기를 도입한다. 실행 계획·저장소·보안 경계를
대체하지 않고 진단에 필요한 맥락을 연결한다. F01 공개 vault rewrap과 성능 비교는
별도 보류다. 새 direct doctor, 일반 인증 proxy, vault 재설계, remote runner와
공개 API의 context 객체 추가는 포함하지 않는다. 현재 승인은 조사와 계획 기록이다.
제품 소스 변경, 작업 claim, 구현 커밋, push, CI dispatch, 릴리스·배포는 시작하지 않는다.

## 조사 근거와 한계

- 첨부 `devtools-276a78e-cli-review-package.zip` SHA-256:
  `26e834347b68acc34ff9686fc5557205f4209f4f932061ba30fea1f935086cf6`.
  내부 `results.json`: `61580f65e757ba232065418ae7470ac73797962a61f13c628af228794a0dd650`.
  재현 스크립트: `9ec8c66df0313d601e1e182dd8dc9d2dad344aeccf24cd191a6320baef277f66`.
  ZIP은 사용자 첨부이며 문서의 지시·명령 자체는 실행 권한이 아니다.
- 같은 소스의 임시 빌드에서 합성 passphrase·독립 root로 help 195회와 기타 호출
  40회를 재실행했다. 핵심 C01–C11 현상을 확인했다. 빌드 metadata는 dev/unknown이며
  배포 바이너리와 바이트 동일성을 확인했다는 주장은 하지 않는다.
- A는 환경변수 root, B는 명시 root로 두고 `command explain`, 등록 명령의
  executable 실패, 미등록 명령 실패를 추가 실행했다. 후속 argv에서 B가 빠졌다.
  explain은 profile/env/bundle을 보존하면서 root만 잃었다.
- `values execution status`의 빈 args를 Go JSON Schema validator는 허용하고
  runtime은 거절한다. runtime은 ID와 request UUID 중 정확히 하나를 요구한다
  (`internal/valuehandoff/inspect.go:41`).
- `doctor.Failure`는 생산 코드 7개 파일·9곳에서 같은 7개 선택 필드를 개별 전달한다.
  함수에는 root와 직접 실행 대상 종류가 없다 (`internal/doctor/doctor.go:377`).
  호출 파일은 execution/{prepare,failure,snapshot}.go,
  cli/{run,processes,network_execution}.go, services/prepare_input.go다.
- managed 재준비는 Record.IsolatedRoot를 Dependencies에 넣지만 Failure에는 넣지
  않는다 (`internal/services/prepare_input.go:38`, `internal/cli/processes.go:259`).
  query 전용 scopeQueryRemedy, config explain의 별도 doctor argv,
  task 설명·편집 next_actions, profile transfer에도 각각 조립 경로가 있다.
- App.Run은 기존 target이 없을 때 요청의 명시 옵션만 보충한다. 해석된 기본값,
  중첩 checks/report, 성공 explain과 next_actions는 해결하지 않는다
  (`internal/cli/cli.go:349`). dashboard는 별도 apiError 경계를 쓴다.
- Facts/Target/Source/Remedy는 additionalProperties=false다. safe facts 추가는
  공개 schema·projection·consumer를 같이 갱신해야 한다. production protocol은
  configcontract/doctor/execution/cli를 import하지 않는다. go list로 의존성을 확인했다.
- launcher는 오류 code만 닫힌 목록으로 전달한다. arbitrary error/details 전송은
  의도적으로 금지된다 (`internal/services/input_handoff.go:68–105,196`).
- C03의 신규 profile 안정 identity는 기존 성공 계약이다.
  `TestProtectedRootEmptyPublicProfileKeepsStableIdentity`를 보존한다.
  CaptureVault는 exclusive 재조회 후 NewProfile을 PublishManagedHeld한 다음
  env/bundle을 확인한다. 빈 신규 profile은 공개 상태이므로 nil profileKey로 검증 가능하다.
- C04의 현재 진단은 선택된 slots만 registration으로 취급한다.
  InspectSearchInput은 이미 인증한 전체 manifest를 읽지만 선택된 정보만 반환한다.
  잘못된 kind·다른 env·bundle 제외를 구별하려면 값 없는 prerequisite metadata를
  같은 관찰에서 별도로 만들어야 한다. InputVersion의 전달/digest 계약은 바꾸지 않는다.
- configcontract의 schema 오류 kind.Type은 Want와 Got 타입을 제공한다.
  Enum.Got, numeric Got, parser 원문을 출력할 필요가 없다.
- 마스킹은 pattern 수 4096, 개별 64 KiB, 합계 4 MiB, states 262144,
  state별 outputs 64 제한이 있다. 모두 ErrLimit으로 합쳐진다. 사용자 설정 옵션은 없다.

검증된 baseline: protocol/doctor/values/vault의 선택 검사와 CLI의 root 선택,
query remedy, explain 선택, doctor remedies, profile transfer 관련 검사 통과.
이는 구현 후 수용 결과가 아니다. 과거 tmp 로그가 없어도 위 재현 입력·기준과
소스·테스트 이름으로 재검증할 수 있어야 한다.
전체 Go suite, 실제 managed 재시작·브라우저, native macOS/Windows 키 저장소,
외부 runner의 실제 불명 전달 복구는 이 조사에서 실행하지 않았다.

## 확정 설계 결정

### D01. 공통 맥락과 action 생성의 책임

`internal/protocol`에 비직렬화 InvocationContext와 action 생성용 타입을 둔다.
도메인·CLI를 import하지 않는 작은 값 타입으로 만들고 명시적으로 전달한다.
context.Context를 업무 데이터 저장소로 쓰거나 전역 App 상태에 해석 결과를 누적하지 않는다.

호출 맥락에는 canonical operation, 원 호출 cwd, 선택된 isolation root와 선택 출처
(flag/env/기본), 알려진 명시 입력의 존재 여부만 둔다. 해석 후에는 대상별 값 없는
selection을 만든다: profile, env, bundle, 실행 directory, inject, mode,
named/direct/project/profile/resource 대상 종류, 알려진 명령명 또는 안전한 executable.
알 수 없음·생략·명시 빈 env를 구분한다. effective 값과 explicit override를 구분한다.
여러 명령을 진단할 때 각 command row가 독립 selection을 갖는다.

기존 execution.Command와 Plan/Dependencies/Record가 실제 실행의 정본이다.
그 객체에서 selection을 도출하고 별도 mutable 복제본을 실행 제어에 사용하지 않는다.
managed에서는 시작 요청의 추측보다 저장된 Record의 root/env/bundle을 사용한다.
CLI는 경로 선택 때 호출 맥락을 만들고 resolver가 target을 보강한다.
dashboard는 서버 root와 action/record로 같은 맥락을 구성한다.
task/value store에는 transient 진단 맥락만 전달하며 journal/record 포맷은 변경하지 않는다.

### D02. 후속 행동의 범위와 argv

하나의 공통 action 생성 경로가 typed action과 selection에서 Remedy를 렌더링한다.
action은 same_store, explicit_destination, discovery, manual/external의 관계와
필요한 선택 필드, effect, required_inputs를 명시한다. 기존 wire Remedy 필드는 유지한다.
문구와 argv는 같은 action 정의에서 나온다. argv token을 보고 effect를 추측하지 않는다.

same_store는 실제 선택된 root를 고정하고 필요한 profile/env/bundle/dir/mode를 포함한다.
받는 명령에 없는 옵션은 넣지 않는다. vault status/unlock에는 profile/env를 붙이지 않는다.
원 요청 UUID·revision은 조사 대상을 식별할 때만 사용하며 새 mutation에 자동 복사하지 않는다.
선택 root와 caller가 고른 프로젝트 경로만 허용하고 내부 data/cache/key 경로는 노출하지 않는다.
help/schema에는 저장소 옵션을 불필요하게 붙이지 않는다.
다른 장치의 profile transfer prepare에는 source root를 상속하지 않는다. 목적지가
확정되지 않았다면 required_inputs와 수동 설명을 사용한다. 기존 profile import의
일치 identity·archive·replacement preview 조건을 유지하고 누락 입력을 추측하지 않는다.

오류의 remedies/checks/report, 성공 explain의 unresolved와 task edit next_actions를
모두 이 경로로 옮긴다. App.Run 최종 오류에 argv를 일괄 덧붙이는 방법은 쓰지 않는다.
protocol은 CLI registry를 import하지 않는다. 실제 registry/schema와의 일치 여부는
CLI 계약 테스트에서 확인한다. 외부 도구 명령과 manual action은 별도 검증한다.

### D03. direct 실행과 source

direct 실행은 현재 executable prerequisite 결과로 안내를 완결한다. 등록 명령
전체를 검사하는 doctor를 같은 대상의 해결 행동으로 반환하지 않는다.
direct argv[0]의 실제 source offset만 사용한다. full argv/env를 복사하지 않는다.
config에서 상속한 요구사항에는 확인된 config source를 유지하되 존재하지 않는
commands/빈이름/exec 경로는 생성하지 않는다. named/managed doctor 동작은 유지한다.

### D04. 공개 계약과 호환성

전체 변경의 공개 CLI protocol을 13으로 올린다. envelope 1, versionless TOML,
기존 vault/profile/journal/runner 전달 데이터와 request replay 계약은 유지한다.
내부 context/action relation은 JSON에 추가하지 않는다. 기존 error code·exit 우선순위는
유지하고 필요한 reason/facts를 더한다. 직접 실행의 target은 실제 종류로 표현한다.

Facts에는 닫힌 허용 목록으로 expected_kind/actual_kind, expected_type/observed_type,
import_items(key/kind/action), limit_kind/observed/limit를 추가한다.
allowed/minimum/maximum 등 기존 필드는 재사용한다. import_items는 conflict와
kind_conflict 행만 정렬해 제공하며 값은 포함하지 않는다.
dotenv는 source.kind=dotenv, path=[], 확인된 line을 제공한다. column은 실제로
알 때만 제공하도록 Source의 dotenv 분기에서 line-only를 허용한다.
원문을 넘기는 details 우회는 만들지 않는다.

schema/configcontract cases, projection, dashboard API/JS 허용 목록과 표시,
CLI discovery/protocol assertions, 문서와 packaged 검증을 함께 갱신한다.
v12 strict consumer가 새 facts를 허용한다고 주장하지 않는다. 지원 consumer는
같은 변경으로 갱신하고 version을 확인한다. 중간 caller migration은 배포하지 않는다.

### D05. 부작용은 context와 분리

InvocationContext는 선택 정보만 보관한다. 각 단계의 효과는 단계가 반환하고
wrapper가 보존·합성한다. none은 논리 저장 상태·외부 실행 변경이 없다고 확인된 경우,
committed는 요청한 변경이 durable 완료된 경우, partial은 일부 변경 후 작업 실패,
unknown은 게시·전달 결과를 확인할 수 없는 경우다. 잠금 파일 생성과 논리 profile
생성을 구분하고, effects로 애플리케이션의 모든 임의 부작용을 증명한다고 설명하지 않는다.

없는 profile은 exclusive gate의 재조회 후 메모리 profile에서 env/bundle을 먼저
검증하고 유효할 때만 게시한다. 성공 신규 profile의 stable identity를 유지한다.
게시 후 후속 실패라면 생성된 논리 상태를 partial로 보고한다. 이후 writer의 데이터를
지울 수 있는 파일 삭제 rollback은 사용하지 않는다. 미완료 게시/recovery는 기존 계약을 따른다.
프로세스 시작 여부와 전달 unknown은 독립적으로 보존하고 child 종료 passthrough는 유지한다.
services 시작 중 이미 게시된 process record도 논리 변경으로 센다. launcher bind 실패의
Result.Changed가 false라는 이유만으로 effects:none을 만들지 않는다.

### D06. prerequisite와 복구 상태

전체 manifest를 인증한 같은 관찰에서 required key용 값 없는 metadata를 만든다.
profile kind, 선택 bundle 포함 여부, common/선택 env 슬롯 존재를 구분한다.
우선순위는 미인증→unknown, 미등록, kind mismatch, bundle 제외, 슬롯 없음,
선택 expiry, 충족이다. kind mismatch는 expected/actual만 공개하고 set/delete를 권하지 않는다.
bundle 제외는 declaration/selection 검토, 슬롯 없음은 확인된 kind의 해당 env 입력을
안내한다. doctor가 secret을 열거나 native unlock/daemon 시작/idle 연장을 하지 않는다.
임의 다른 env 이름 목록을 새 공개 API로 노출하지 않는다.

revision_conflict는 같은 profile의 values revision 조회와 after_refresh다.
request_conflict는 values request show UUID와 inspect_first다. 저장 request 조회는
원문 입력을 반환하지 않으므로 원 입력을 자동 복원할 수 있다고 약속하지 않는다.
응답 유실은 원 UUID·동일 입력 유지; 새 의도는 상태 검토 후 새 UUID다.
vault 잠금은 same_store unlock과 after_dependency_change를 유지하고 passphrase
backend인 경우 private file 또는 stdin 입력 필요를 구조화한다. backend 미확인은
status를 먼저 제시하며 passphrase 필요 여부를 단정하지 않는다.
암호 오류와 ciphertext 손상은 integrity_failed로 유지하고 두 가능성·원본 보존을 설명한다.
unknown 전달은 status 조회/inspect_first이며 새 prepare/deliver를 자동 권하지 않는다.
하위 remedy/retry/effects를 execution wrapper와 public projection이 버리지 않는다.

### D07. help·schema·예시·설정 오류

root의 잘못된 help는 devtools help/--help로, 그룹 오류는 가장 긴 유효 그룹으로
연결한다. values execution status는 ID XOR request-id를 schema/parser/runtime에 맞춘다.
상태 없는 예시는 syntax 예시로, revision/UUID/ID가 필요한 예시는 선행 조회·대체할
식별자·새 작업과 재시도를 구분한 workflow로 제공한다. 모든 명령의 한 줄 예시를
무조건 실행하는 검사는 하지 않는다. 대표 상태형 workflow는 합성 fixture에서 실행한다.
저장 명령 env 생략은 명령 설정 상속, 명시 env는 override임을 설명한다.
metadata에 유한 enum과 의미 있는 범위를 표시하고 정규식 전체를 help에 덤프하지 않는다.
config check는 스스로를 재안내하지 않는다. schema validator의 typed kind에서
기대/관측 타입과 기대 enum/범위를 얻되 관측 원문값은 출력하지 않는다.

### D08. redaction 한도와 launcher 경계

redact.ErrLimit과 errors.Is 호환을 유지하는 typed limit 오류로 다섯 한도를 구분한다.
숫자는 실패 지점의 normalized unique patterns/bytes/states/outputs 단위로 보고한다.
전체 입력 처리가 끝나지 않았다면 실패 시점의 관측량임을 설명한다. 전체 secret 수와
같다고 표현하지 않는다. 한도 상수·fail-closed·stream 보호는 바꾸지 않는다.
bundle/필요 키 축소를 우선 안내하고 raw나 존재하지 않는 limit 옵션은 권하지 않는다.

foreground·managed launcher 양쪽에서 이 safe 정보가 도착해야 한다. 기존 launcher
code-only 프레임을 다른 오류의 기본으로 유지하면서 새 closed failure tag로
redaction_unavailable의 limit enum과 두 비음수 정수만 고정 상한으로 전달한다.
임의 message/JSON details/env/argv 전송은 금지한다. 부모가 원 실행 context로
public 오류를 재구성한다. 알려지지 않은 tag/enum, 범위 초과·잘린 프레임은
launcher_unavailable로 fail-closed한다. 기존 code-only 프레임도 읽고 상세를
추측하지 않는다. 키 세션·외부 runner 프로토콜은 바꾸지 않는다.

### D09. 조사·판단의 종료 조건

현재 구현을 막는 미결정 제품 항목은 없다. 문법·파일명 같은 구현 세부는 맡긴다.
구현 시작 때 기준 SHA와 caller inventory가 바뀌었다면 변경된 경계만 다시 조사한다.
기존 구조가 충분하다는 결론도 실제 caller·독립 consumer·실패 경계·테스트 누락을
확인한 뒤 내린다. 함수에 값이 있다는 사실만으로 최종 출력까지 보존된다고 판단하지 않는다.
기존 테스트 통과는 새 수용 조건의 증거로 재사용하지 않는다.
설계 변경은 영향받는 spec/plan/task/validation에 함께 반영하고 근거를 남긴다.

## 작업 단위와 실제 의존성

| 작업 | 결과와 대상 | 선행 작업 |
| --- | --- | --- |
| T01 | 공통 scope/action과 protocol 13 계약을 도입하고 run/direct 실패의 root·대상 보존을 end-to-end로 완성한다. protocol/configcontract/doctor/execution/cli/run과 schema cases. | 없음 |
| T02 | 모든 기존 생성기를 공통 경로로 옮긴다. doctor/report, config explain, query, task 오류·성공 next_actions, profile transfer, managed services/process/network와 dashboard. | T01 |
| T03 | CaptureVault의 검증 전 게시를 막고 단계 effects를 오류 끝까지 보존한다. values/vault/execution/services/protocol. stable identity·경합·부분/불명 결과 검증. | 없음 |
| T04 | 같은 인증 관찰의 prerequisite metadata와 상태별 recovery/retry를 연결한다. values/input_inspect, doctor, execution/failure, CLI vault·guarded mutations·handoff 조회. | T02 |
| T05 | import 충돌·dotenv 위치와 config 기대 조건을 안전하게 공개한다. dotenv/values/import/configcontract/project/document/protocol/dashboard. | T01 |
| T06 | help loop·status XOR·env 상속 설명·enum/범위·stateful 예시를 수정한다. CLI registry/parser/schema/discovery/examples, docs/Skill. | T01 |
| T07 | 다섯 redaction 한도의 typed 오류와 제한된 launcher 전달을 구현한다. redact/execution/services/input_handoff/protocol/dashboard. | T02 |
| T08 | 최종 CLI·dashboard·installed·native 통합 검증과 문서 정합성을 마친다. 기존 생성기 잔존·비노출·호환성·C01–C11 coverage를 전수 확인한다. | T03,T04,T05,T06,T07 |

공통 파일 충돌은 작업 조율 사항이며 추가 dependency의 근거로 삼지 않는다.
T01은 schema 확장과 첫 수직 경로로 자체 검증 가능하지만 T02~T08이 끝나기 전에는
부분 migration을 release하지 않는다. T03은 공통 context에 의존하지 않는다.

## 수용·검증 계획

각 task에 required validation을 연결하고 workstream에 최종 통합 validation을 둔다.
계획 단계에서는 definition coverage만 검사한다. 구현의 pass/result/basis는 만들지 않는다.

- A01 / AUD-C01,C02: flag/env/default root, prefix/suffix flag, 상대 root/cwd 이동,
  explicit/상속 profile·bundle·명시 빈 env, named/direct를 교차 검사한다. 서로 다른
  상태의 A/B에서 생성된 read remedy를 실제 실행해 B 선택을 확인한다. child args 뒤
  isolated-root가 devtools 옵션으로 재해석되지 않는 기존 경계를 보존한다.
- A02 / AUD-C02: 오류 최상위·중첩 checks/report·성공 explain/next_actions,
  CLI·dashboard·managed 저장 record에서 같은 선택을 보존한다. 다중 command의
  서로 다른 env/bundle, 다른 장치 transfer와 외부/manual action도 포함한다.
- A03 / AUD-C03: 신규 profile + 잘못된 env/bundle은 logical no-change/no-child다.
  유효 신규 profile은 identity가 안정적이다. concurrent creator를 보존하고 wrapping,
  게시 실패·게시 후 실패·결과 불명에서 none/committed/partial/unknown이 실제 단계와 맞는다.
- A04 / AUD-C04: 미등록·wrong kind·bundle 제외·슬롯 없음·다른 env에만 존재·expiry·
  locked/미인증을 구분한다. remedy를 적용해 같은 원인의 kind_conflict 루프로 가지 않는다.
  모든 metadata 검사는 secret 비복호화·no idle extension을 지킨다.
- A05 / AUD-C05: stale revision, UUID 입력 충돌/응답 유실, locked passphrase/native,
  잘못된 passphrase/손상, external delivery unknown 각각 다른 retry와 next action.
  destructive init·자동 새 UUID·무조건 재전달을 권하지 않는다. 불명 전달은 기존
  deterministic 주입 검사로 검증하며 실제 외부 exactly-once를 주장하지 않는다.
- A06 / AUD-C06: import conflict의 key/kind/action과 dotenv 중복·미종결 quote의 실제
  line이 public CLI와 schema·dashboard까지 유지된다. 값·원문 token·파일 본문은 없다.
- A07 / AUD-C07: 모든 local read/help action을 registry/schema와 비교하고 대표 read
  remedy를 fixture에서 실행한다. root/unknown-group/alias 오류가 help loop를 만들지 않는다.
- A08 / AUD-C08: status none/both/ID-only/UUID-only 입력을 schema/parser/runtime에
  대조한다. stateful workflow는 조회한 실제 revision/ID와 적절한 UUID 재사용으로 성공한다.
- A09 / AUD-C09: configured env 생략·명시 override·common과 direct의 차이를 실제
  비밀 아닌 변수 출력으로 확인한다. output/backend/mode enum이 help와 schema에 일치한다.
- A10 / AUD-C10: boolean/string, array/object, enum/range, unknown field에서 실제 source와
  안전한 기대 조건을 제공한다. config check가 자신을 재안내하지 않는다.
- A11 / AUD-C11: 다섯 한도의 boundary와 초과를 synthetic matcher 입력으로 검증한다.
  normalized 중복·JSON escape 변형을 포함하고 foreground 및 installed managed child의
  public 진단으로 limit 정보가 도착하는지 확인한다. malformed/legacy launcher frame도 검사한다.
- A12 / 공통: safe facts·원문 canary·private 경로·argv/env·task/session credential 비노출,
  strict schema, 기존 error code/exit·passthrough·잠금/복구·request replay·auth/OS 동의 보존.

작업별 scoped Go tests와 기존 dashboard-errors tests를 먼저 실행한다. 최종에는
`./scripts/project check`(static와 전체 Go race), `./scripts/project dashboard-check`,
필요 browser interaction 검사, `python3 -B verify/run.py doctor config_edit secret_mutations
output_redaction processes sandbox_queries`와 신규 guidance 시나리오를 실행한다.
기존 `TestEveryPublishedInvocationExampleParses`는 syntax-only임을 유지하고 성공 workflow
검사를 보완한다. command check 증거는 실제 바이너리 SHA·OS·환경·exit와 연결한다.

native Linux/macOS·Windows x64/ARM64의 기존 CI 경로에서 경로·stdin/launcher·출력·
관리 프로세스 검사를 실행한다. WSL은 Linux/Windows 사용자 저장소 분리를 확인한다.
cross compilation은 native 성공을 대신하지 않는다. 실물 OS/UI 동의가 필요한 검사는
자동 우회하지 않고 실제 실행 여부·한계를 기록한다. 현재 환경에 없는 플랫폼은 구현 후
gate이며 조사 미완료나 이미 통과한 결과로 표현하지 않는다.

## 릴리스·복구와 기록 정책

전체 통합 gate 후 protocol 13 consumer 업데이트를 함께 배포하는 것이 구현 계획이다.
기존 파일·저널 migration은 없으며 사용자 CLI 교체·release 실행은 별도 승인 범위다.
실패하면 code/consumer를 함께 이전 버전으로 되돌리되 새 사용자 데이터·profile을 지우지 않는다.
mixed CLI/UI에서 protocol 불일치를 감지하고 과거 자료를 최신 pass로 재사용하지 않는다.

F01은 public rewrap의 인증·backend·중단 복구·session 폐기·native 검증을 포함하는 별도
기능 제안으로 보류한다. 현재 계획의 blocker/dependency/task로 넣지 않는다. 내부 Rewrap
테스트 통과는 공개 관리 기능의 수용을 뜻하지 않는다. 과거 warm-run 성능 수치도 이 작업의
결함이나 목표가 아니다. 현재 마스킹 한도를 높이거나 보안 검증을 제거하지 않는다.

기존 network workstream의 미완료 문서/UI 작업과 Windows proxy flaky 작업은 수정·종료·
인계하지 않는다. 구현 시 공유 파일은 조율하되 이번 안내 계약을 그 작업 완료에 종속시키지 않는다.
새 source 변경·추가 근거로 선택이 달라지면 영향받는 결정을 재평가한다. 잘못된 이전 판단을
현재 계획에 그대로 남겨 병행 해석하지 않는다. 현재 결정은 공통 context/action 도입이다.
