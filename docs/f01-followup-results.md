# F01 후속 구현과 검증 근거

2026-10-23의 후속 F11·F12·F13·P11 범위다. [최초 조사](f01-vault-rewrap-assessment.md)는
조사 당시 이력으로 보존한다. 후속 workstream은 `072dc32a-af95-4fff-a745-4a81ba4761c6`이다.
최종 같은 SHA의 전체 GitHub Actions 판정과 task validation record는 devtools 작업 기록에 둔다.

## F11: 공개 보호 변경

기존 내부 Rewrap의 존재만으로 실제 사용 흐름이 제공되는 것은 아니었다. 공개 CLI에
`vault rewrap`을 추가하고 기존·대상 backend와 UUID를 명시하도록 했다. root key·profile
bytes·논리 값은 유지한다. 암호 입력, native 재인증, 중단 뒤 동일 요청 재개, 수동잠금,
키 캐시와 이전 실행 준비 무효화를 연결했다. 보호 변경과 외부 provider 자격 회전,
이미 실행된 앱 환경, 과거 키 사본의 회수는 구분한다.

공개 protocol은 14, 내부 키 세션은 4, 저장 형식은 1이다. 이전 키 세션은 보호 변경 전
퇴역시킨다. native 인증은 launch/slot/maintenance lock 밖에서 수행하고, 게시 시점만 기존
launch drain과 slot 쓰기 잠금으로 보호한다. same-native no-op도 실제 보호를 인증한다.

실제 IPC의 passphrase 변경·replay·오류·수동잠금·pending recovery, 여섯 subprocess crash
checkpoint, 이전 launch ticket 거부와 새 준비 성공을 검사했다. full CLI/vault/values 및
관련 race 검사를 통과했다. 구현 커밋은 `fa3c99a`다.

## F12: 실제 native와 사용 흐름

검증 harness 커밋은 `2838268`, native macOS cache 수정은 `c79c411`이다. 사용자 OS 저장소
대신 disposable Actions host에서 public CLI를 실행했다.

| 실제 환경 | 같은 c79c411의 성공 근거 |
| --- | --- |
| Linux Secret Service | [Release native job](https://github.com/jinyongp/devtools/actions/runs/39933142467/job/120346162927) |
| macOS Intel Keychain | [Release native job](https://github.com/jinyongp/devtools/actions/runs/39933142467/job/120346163033) |
| macOS ARM64 Keychain | [Release native job](https://github.com/jinyongp/devtools/actions/runs/39933142467/job/120346163136) |
| Windows x64 DPAPI | [main product job](https://github.com/jinyongp/devtools/actions/runs/39933141646/job/120347120028) |
| Windows ARM64 DPAPI | [main product job](https://github.com/jinyongp/devtools/actions/runs/39933141646/job/120347120231) |
| 실제 WSL2 종료·재시작 | [Release WSL job](https://github.com/jinyongp/devtools/actions/runs/39933142467/job/120346162937) |

native↔passphrase, 이전 native item 제거 후 replay, 잘못된 기존 암호의 slot 보존,
same-native no-op/replay, manual lock, 같은 root/profile bytes와 자식 secret 소비를
실행했다. macOS 두 아키텍처의 JSON은 warm cache에서도 잠긴 Keychain을
`key_access_denied`로 거부함을 확인했다. Linux unavailable 검사도 암묵적 fallback 없이
원래 slot을 보존했다. WSL2는 종료 전후 같은 UUID receipt와 수동잠금을 유지했다.

최초 run39931025771의 Intel job은 native 실행 전에 setup-go cache egress 제한으로
취소됐다. 선택적 cache restore만 껐으며 검사나 수용 조건은 줄이지 않았다. 위 native
job 성공을 전체 workflow 성공으로 해석하지 않는다. 같은 c79c411의 Windows CLI suite는
추가 테스트가 pending 파일을 일반 쓰기로 만든 탓에 실패했다. F13에서 제품과 같은
비공개 게시 함수로 fixture의 Windows ACL 조건을 맞췄다. 최종 전체 판정은 별도 기록한다.

최종 수용 조건 대조에서는 native 중단 경계의 기존 대역 검사도 보완했다. 실제 native
item 생성 직후와 slot 게시 직후 subprocess를 종료하고 동일 UUID로 같은 root를
복구하는 검사를 기존 세 OS의 native Go gate에 연결했다. Keychain·Secret Service는
실제 item을 보존한 채 제거 경계의 거절을 주입하고 재개·실제 제거를 확인한다. 이 주입은
실제 OS ACL 제거 거절을 재현했다는 주장이 아니다. DPAPI에는 외부 item이나 제거
연산이 없어 그 경우를 적용하지 않는다. 추가 검사의 실제 결과도 최종 Actions 기록에 둔다.

## P11: 측정에 따른 최소 변경

App 생성 중 입력 예시를 만들 때 참조 정의가 필요 없는 schema에도 전체 설정 schema를
매번 복사했다. 공개 body schema는 자체 정의를 포함하므로 이 불필요한 복사만 제거했다.
새 전역 cache, registry 분리, 인증·마스킹 생략은 도입하지 않았다. 구현은 `5577da9`다.

동일 Go 1.27.3·CGO=0·trimpath·buildvcs=false·버전과 같은 host에서 직전 기능 구현
`2838268`과 후보 `5577da9`를 비교했다. public catalog와 도움말 196개의 정규화 결과가
동일했다. 각 실행은 별도 CLI process이며 fixture 준비·unlock은 측정 밖이다. 입력은
32-byte synthetic secret 10/100/1000개와 var 0개다. 기존 perf fixture의 1000 var와
혼동하지 않는다. 자식이 전달된 모든 값을 검증했다.

hyperfine 1.20.0의 shell 없는 반복 측정으로 총 780개 유효 timing을 보존했다. 아래는
1000-secret 집중 재현의 네 교차 순서 block, 조건당 60회 중앙값이다.

| 작업 | 기준 ms | 후보 ms | 변화 |
| --- | ---: | ---: | ---: |
| warm secret 주입·자식 실행 | 66.439 | 59.882 | -9.87% |
| bundle none 실행 | 48.555 | 42.099 | -13.30% |
| version 새 process | 20.859 | 13.251 | -36.47% |

주입 실행 p10–p90은 기준 65.013–68.537ms, 후보 58.586–61.789ms였다. App 생성 Go
benchmark 5회 중앙값은 7.894→2.838ms, 16,212,141→4,429,637 B/op,
116,043→35,640 allocs/op였다. task 등록은 0.665→0.672ms로 원인이 아니었다.
후보의 남은 warm 실행 CPU profile에서는 snapshot/authentication, 엄격한 decoding,
redaction과 실제 실행이 관찰됐다. 중첩 CPU 비율을 합산하거나 wall latency 비율로
해석하지 않는다. 추가 최적화를 정당화하는 SLO나 KDF 병목 근거는 없다.

원본·도구·manifest·동등성 결과·교차 block·경고·CPU profile은 로컬 ignored
`perf-results/p11-20261023/`에 보존했다. 최초 scale 측정의 outlier를 버리지 않았다.
이 수치는 위 두 SHA의 비교이며 임의 host나 이후 문서 변경의 새 측정으로 주장하지 않는다.

## F13: 안내·계약과 호출부 리뷰

help/schema workflow, 보호 안내, protocol v14 migration과 repository Agent Skill을
실제 입력·재개 흐름에 맞췄다. 설치한 사용자 CLI/Skill은 교체하지 않았다. `vault status`의
pending metadata는 미인증 routing 정보이며 오류는 기존 request_id/retry/remedy 계약을 쓴다.

리뷰에서 `config schema`의 실제 protocol 14와 공개 output schema의 옛 const 10 불일치를
재현했다. embedded schema ID·const를 14로 맞추고 실제 응답을 공개 output schema로
검증하는 독립 consumer 검사를 추가했다. 기존 v13 migration 이력은 보존했다.

direct execution과 managed services는 같은 BeginLaunch와 epoch 확인을 사용한다.
values 쓰기와 valuehandoff prepare/deliver는 WriteIdentity를 저장·재검사한다. backup은
새 대상 profile/keyset을 만들며 rewrap과 구분한다. dashboard는 기존 세션 오류를 유지한다.
관련 execution/services/valuehandoff/backup/dashboard package 검사도 통과했다. 공개
schema/help 예시와 오류 projection을 재검증하며 최종 main SHA의 기존 Release
publish=false 전체 gate를 통합 완료 조건으로 유지한다.
