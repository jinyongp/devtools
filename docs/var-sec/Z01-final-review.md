# Z01 전체 변경 최종 리뷰

기준 `89100eb98888895a450b2ebd403e4e1c9b5f52d2`부터 구현·단계 검증과 최종 문서
수정까지 검토했다. 다른 24개 작업은 현재 정의에서 완료 상태다. 전체 변경 경로와
현재 파일 digest, 영역별 근거, unchanged caller는 [범위 기록](evidence/Z01-review-scope.json)에
보존한다. 구현·운영 문서 변경 401개 경로와 이 최종 보고서/범위 기록을 합쳐 403개다.

## 전체 범위와 재검토

| 영역 | 최종 상태와 종합 확인 |
| --- | --- |
| public-only/sealed·manifest/catalog·키 | reviewed. 전체 sealed manifest·catalog, 목적별 키/AAD, nonce·KDF 입력 제한, sticky sealed, unsigned public 표기와 실제 root 인증을 구분 |
| native protector·IPC·키 세션·잠금 | reviewed. 양쪽 UID/SID, local-only pipe, frame/연결/KDF 한도, stable protocol retirement, passive no-touch, manual intent와 admission drain; creator-app Keychain ACL 유지 |
| snapshot·handoff·lease·멱등성 | reviewed. CLI/Dashboard 공통 실제 frozen 준비, 값/설정/port 고정 후 admission, old stop 전에 payload 확보, 같은 UUID/불명확 결과/late response, 원래 lease/handle 기반 수명 |
| stream·pattern 수명 | reviewed. independent reference/분할·중첩, stdout/stderr·capture-before-disk, 실제 PTY/raw 예외, backpressure/cancel/EOF fail-closed, 옛 앱 패턴 유지 |
| migration·복구·backup·transfer | reviewed. 최초 전환은 encrypted forward journal, 전환 뒤 값 transaction은 암호화된 profile과 v4→v3 barrier 유지, 실제 durable 중단, 옛 MAC/receipt, fresh target authority·공유 logical IDs, 원본 파일 잔여 한계 |
| bundle·parent·사용처·notify | reviewed. 기존 profile/env 전체 기본, flat 공유 membership/우선순위/none, parent 별도, declared/delivered/prepared 구분, 원문 없는 버전과 cached inspection |
| 자동 restart·실패·storm | reviewed. 명시 managed opt-in, old/goal 고정, 실제 준비→admission→stop→spawn, first terminal result, successor completion 보존, 실패/unknown latch와 3회/30초 제한, one-shot 제외 |
| CLI·Dashboard·진단·Skill | reviewed. schema/help/completion, 동일 origin/bearer 경계, public/secret 분류와 입력·출력, DOM unlock 입력 정리, keyboard/UI, 실제 운영 예시와 복구 안내 |
| native·설치·workflow·의존성 | reviewed. 정확한 source/architecture, 실제 설치/update/서명·다른 사용자·logout·WSL, required pass와 skip 방지, artifact source/hash, publish=false 권한·입력 경계; native FFI/D-Bus/crypto 사용처 |
| 성능·검증 자료 | reviewed. baseline/candidate 동일 fixture와 원본 timing/RSS/native unwrap/redactor, 실제 54개 수렴 관찰·storm, 실패/partial/cleanup, source/환경 적용 범위 |

각 단위의 기존 리뷰를 버리거나 모든 검사를 기계적으로 반복하지 않았다. A00–A11,
N01–N05, B01–B02, C01–C02의 source/수정 기록을 현재 계약과 대조한 뒤 공통 경계를
다시 따라갔다. 특히 CurrentStore→실제 snapshot→CLI/Dashboard 준비→launcher binding/
admission→old stop→spawn, ordinary/forward recovery와 legacy barrier, watcher→goal guard→
단일 coordinator→상태 보고, 출력 stream의 완료/취소 수명을 연결해 확인했다.

변경 없는 task 복원은 active claim을 release하고 context/receipt를 제거한다. 기존 network/
proxy/cleanup/installer/package와 private filesystem 호출자는 새 선택·snapshot·권한/수명
계약에 맞는지 재확인했다. 범위 기록은 모든 changed path를 한 영역에 매핑하며,
최종 미검토·needs-recheck·blocked 영역은 없다. 제3자 라이브러리 내부 전체 감사나
모든 결함 부재의 수학적 증명을 수행했다는 뜻은 아니다.

## 확인한 문제와 수정

현재 안내 문서가 실제 통합·native 수용 완료 뒤에도 진행 중이라고 설명했고, IPC
한도를 묶어 표현해 키 세션과 실행 전달의 보장 범위가 불분명했다. `e3e075f`에서
실제 A/B/C 수용 근거와 설치·게시의 차이를 연결하고, 키 frame 128KiB/실행 frame
16MiB의 사전 길이 검사와 supervisor JSON의 16MiB 제한 reader를 구분했다.
소스 상수·호출자·문서 링크를 다시 대조했다. 과거 단위별 pending은 당시 기록으로
보존하며 최신 A12/B03/C03 및 이 최종 ledger가 현재 수용 상태를 설명한다.
추가로 확인된 적격 런타임 결함은 없으며 미해결 적격 finding도 없다.

## 최종 상태에 적용되는 검증

- [A12 실제 native 근거](evidence/A12-native-1e4bebd.json): production `1e4bebd`,
  [Actions 39286793430](https://github.com/jinyongp/devtools/actions/runs/39286793430),
  필수 26개 성공·게시 3개 skip. 실제 Keychain Intel/ARM, DPAPI x64/ARM64,
  Secret Service/UID/SID/pipe/logout, 실제 별도 WSL 종료·재시작, 12방향 nonempty bundle 전송.
  양 Windows full suite는 필수 fault/security 36개씩 실제 pass다.
- [B03](evidence/B03-integration.json)·[C03](evidence/C03-integration.json): current
  `check:all` 전체 race/vet·공식 Skill/package/static 검사, 실제 설치물의 vault·선택/
  사용처·기본 notify·transfer·TLS automatic restart/readiness·출력·두 PTY 검증 통과.
  30개 전체 설치 시나리오와 네 browser 시나리오의 유효한 결과도 A12에 연결돼 있다.
- 최종 production Go/UI/JS는 native source `1e4bebd`와 같다. 뒤의 변경은 추가 local
  installed assertion, 측정 도구·원본 결과와 문서/Skill이다. 새로운 local assertion이나
  수렴 측정을 이전 native job에서 실행했다고 기록하지 않는다. 최신 Skill 검증은 C03에 있다.
- [A12 성능](A12-final-performance-results.md)은 7,020개 raw timing과 RSS/alloc/native
  request·redactor를 보존한다. [C03 수렴](C03-convergence-results.md)은 clean `008f91c`
  source의 18개 소유 root case/54개 관찰과 전부 확인된 cleanup, 실제 storm 억제를 보존한다.
  서로 다른 notify/applied 결과를 동일 작업의 speedup처럼 비교하지 않는다.

초기 emulated x64 pwsh.exe의 0xC0000005 실패와 같은 소스의 성공 재검증, macOS runner
대기는 기록에 남아 있다. 실패를 pass로 바꾸거나 검사/ACL을 약하게 하지 않았다.
일반 Codex quick validator의 기존 compatibility-field 비호환은 실제 공식 Skill/package
검증과 구분하며, 실패를 통과로 기록하지 않는다. 원문·해제 키·task context를 근거에 싣지 않는다.

동일 사용자 악성 앱/agent, 이미 전달한 환경, 앱 자체 파일·네트워크, 외부 anchor 없는
전체 과거 상태 rollback, secure heap/모든 사본 삭제, production signing/notarization,
physical PC/LAN·전원 차단·강제 kernel PID 재사용은 합의한 보장 밖이다. 이 구현은
릴리스 게시나 실제 사용자 vault 이전을 실행하지 않는다. 최종 workstream 수용 기록과
close는 Z01 완료 뒤 별도로 남긴다.
