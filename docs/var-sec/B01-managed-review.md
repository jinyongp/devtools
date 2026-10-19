# B01 managed process·project batch 선택 지속 리뷰

실제 전달된 selection과 사용자가 명시한 bundle override를 별도로 기록한다. process start/restart와 project up/restart가 같은 선택 provider·frozen payload·admission/handoff를 사용한다. 명시 override가 있으면 restart에서도 계승하며 명시 all/none/named로 바꿀 수 있다. config만으로 선택한 실행은 restart 때 현재 command/project 설정을 사용한다. --env가 none을 확장하지 않는다. inject=false는 기존 값 주입 비활성화를 유지한다.

| 영역 | 최종 리뷰·근거 |
| --- | --- |
| record·UUID·기존 receipt | reviewed, 새 override는 omitempty라 과거 nil 입력 fingerprint를 보존. 잘못된/stop 선택은 mutation 전에 거절. 새 실제 selection은 prepared payload metadata에서 기록 |
| existing execution 재사용·legacy record | reviewed, 실제 선택과 명시 override 정책이 다르면 process_conflict. 이전 record의 없는 selection은 이전 전체-profile 기본값으로만 취급 |
| 실제 process restart·batch inheritance | reviewed/pass, 설치된 CLI의 frontend start → config none 변경 → process restart → project restart가 PUBLIC만 주입, explicit none batch restart는 둘 다 비움 |
| before-stop·오류·handoff | reviewed/pass, missing bundle restart 실패 뒤 기존 실행 running, CLI/dashboard preflight와 양방향 launcher metadata가 같은 선택을 검증 |
| proxy/혼합 batch caller·멱등성 | reviewed/pass, network prepare/phase preflight에도 선택 전달. 리뷰 중 preparation fingerprint의 선택 누락을 확인·수정, 누락 조건에서 회귀 검사 실패와 수정 후 pass |
| 공통 manager와 기존 호출자 | reviewed/pass, internal lifecycle preflight에 선택을 명시 전달하고 mixed/nonproxy/proxy·retry·readiness caller와 기존 tests를 함께 갱신 |

검사: 설치된 vault bundle/managed/batch 시나리오 pass. services/lifecycle/execution/CLI/dashboard 전체 race pass; 최종 CLI 43.283s·lifecycle 1.575s, 관련 변경 없는 services/execution/dashboard는 앞선 같은 source의 6.961/3.877/3.285s 결과를 재사용했다. 첫 variadic network preflight 연결에서 기존 function-field 타입 불일치를 발견하여 고정 signature adapter와 explicit selected implementation으로 분리했고 관련 caller compile·기능 검사를 다시 통과했다.

네트워크 준비 UUID의 bundle 선택 누락은 실제 netops journal 경로로 재현했다. 선택 fingerprint를 제외하는 조건에서 동일 UUID·다른 묶음이 잘못 다시 준비되는 검사가 실패했으며 /tmp/devtools-values-plan/B01-network-selection-fingerprint-before.log에 보존했다. 수정은 optional selection map을 fingerprint에 포함하고 nil/empty는 이전 형식을 유지한다. 기존 request/durable journal contract를 완화하지 않는다.

이번 범위는 managed 선택 지속·재시도·원문 없는 metadata 연결이며 자동 restart나 event watcher를 추가하지 않는다. input의 원문 환경은 여전히 private payload 경계에만 있다. B01 전체에는 최종 통합 review·검증 기록이 남고 B03/A12/native 최종 수용은 별도로 유지한다.


Project batch와 dashboard의 시작/재시작 preflight도 같은 explicit 선택을 받도록 관련 callback signature와 호출자를 재검토했다. batch의 초기 검사·혼합 proxy/nonproxy·계속 진행·이전 override 상속, network journal 선택과 launcher 비교를 모두 포함했다. 설치된 CLI에서 project restart의 implicit frontend 유지와 explicit none도 재검증했다. 잘못된 batch 선택은 operation 시작 전에 거절한다. 전체 project check는 /tmp로 격리한 기존 uv tool/cache로 실행하며 기본 readonly uv 디렉터리 때문에 실패한 첫 실행과 구분한다.
