# D01 — Batch 변경과 전역 잠금 후속 리뷰

검증 대상 제품 소스는 `4ce462e54448e7702835a9614fe0eaa882daa7f3`이다.
비교 기준은 `b1db965767abf953f8a1e62221e9432f03cb3491`이다. 이후 문서·근거
커밋은 이 제품 소스를 바꾸지 않는다. 이 기록은 로컬 구현·검증 근거이며 릴리스
승인이나 이번 소스의 native OS 수용 기록을 대신하지 않는다.

## 변경과 리뷰 범위

| 작업 단위 | 결과와 검증 |
| --- | --- |
| 취소와 deadline | 준비 단계에서 context를 확인한다. `canceled`/130과 `deadline_exceeded`/3을 구분하며, 성공한 handler 결과는 늦은 취소로 덮어쓰지 않는다. |
| 직접 batch 변경 | `Profile.Batch`가 원본 인증·후보 복사·최종 서명·최종 인코딩을 고정 횟수로 수행한다. 단일 변경도 같은 엔진을 쓴다. |
| 슬롯 복호화 범위 | 공개 변수·bundle·env 생성·슬롯 삭제는 기존 secret을 열지 않는다. 비교·교체와 import 계획은 입력 대상 슬롯만 연다. |
| 정상 조회 공유화 | 기존 profile의 실행 snapshot과 managed inspection은 공유 잠금을 사용한다. 생성·legacy 전환이 필요하면 해제 후 배타 잠금을 얻고 다시 읽는다. |
| 준비/커밋 분리 | 공유 잠금으로 원본을 얻은 뒤 잠금 밖에서 후보를 만든다. 커밋 시 원본·보호 상태를 확인하고 최신 catalog의 자기 entry만 변경한다. |
| 실패·동시성 | 같은 profile 충돌, no-op 후보 충돌, 동시 요청 replay, 취소, pending recovery, rotation, rewrap, 수동 잠금과 재-unlock을 검사한다. |

`Editor`는 전체 평문 `State` 대신 명시적인 변경 메서드만 제공한다. 일반 변경과
import preview에서 `ExportLogical` 왕복을 제거했다. CLI의 선택 layer 기준 import와
dashboard의 기존 skip/overwrite 의미는 유지한다. 보호된 profile의 공개 변수 변경도
manifest 인증용 키 접근은 필요하다. 슬롯 복호화 0회는 잠금 우회를 뜻하지 않는다.

변경하지 않은 슬롯의 ID·버전·nonce·암호문을 보존한다. 같은 값 재등록은 버전이나
profile 파일 내용을 바꾸지 않는다. Batch의 일부 항목이 실패하거나 취소되면 원본 후보를
게시하지 않는다. 최종 크기·구조·무결성 검사를 생략하는 bulk 우회 경로는 없다.

## 커밋 경계

원본 profile의 인코딩 전체를 비교하므로 profile ID, storage generation, revision,
key epoch, manifest tag 및 나머지 데이터 변경을 함께 감지한다. Vault ID·보호 backend·
wrapped root key·수동 잠금 상태도 다시 확인한다. 기존 `PublishManagedWithFilesHeld`가
최신 catalog를 읽어 자기 entry를 병합하고 profile·catalog·receipt·hint를 기존 복구
가능한 다중 파일 트랜잭션으로 게시한다.

실제 session을 쓰는 변경은 최초 key delivery의 session ID와 lock epoch를 커밋 전에
다시 확인한다. 잠갔다가 다시 unlock해도 이전 후보가 새 권한을 얻지 않는다. 이 확인은
캐시가 만료된 경우 native 접근을 유발할 수 있어 key-slot/maintenance 잠금보다 먼저
수행한다. 커밋의 잠금 순서는 launch → key-slot → maintenance다. 일반 변경은 짧은
공유 launch lease, 최초 secret 보호 전환은 기존 배타 launch mutation lease를 사용한다.
실행 입력의 기존 30초 유효기간은 연장하지 않았다.

저장 전에 context가 취소되면 후보를 버린다. 다중 파일 게시가 시작된 뒤에는 기존
완료·복구 규칙을 유지한다. 실패 응답만으로 저장되지 않았다고 단정하거나 새로운
요청 ID로 무조건 재시도하지 않는다. Managed 요청은 기존 요청 ID로 결과를 확인한다.

## 회귀 검사

- `TestBatchProcessesWholeProfileOnce`: 100/1,000/5,000개 새 secret에서 batch 경계의
  원본 인증·후보 복사·최종 서명·최종 인코딩 각 1회, 기존 secret 열기 0회.
- `TestBatchDecryptsOnlyComparedSlots`: 공개 변수·bundle·env·unset은 0회,
  단일 기존 secret 비교·교체는 1회. 게시 경계의 추가 전체 검사는 고정 횟수로 유지한다.
- `TestModernMutationDoesNotOpenUnrelatedSecrets`: 전체 MAC 인증과 개별 슬롯 AEAD 열기를
  구분하는 fixture로 일반 변경·managed bundle·대상 secret 변경의 복호화 범위를 검사한다.
- `TestPreparedWriteAllowsOtherProfileReadExecutionAndCommit`: A의 후보 준비 완료 후 커밋 직전에 멈춘 동안
  B 조회·실행 입력 생성·커밋을 완료한다. Public/protected 및 Update/managed Apply를
  모두 다루고 A·B 결과와 catalog 인증을 확인한다.
- `TestActiveBatchPreparationAllowsOtherProfileCommit`: Batch callback 안에서 첫 secret을 바꾼 뒤, 나머지 999개 변경 전에 A를 멈춘다. 이 상태에서 B의 실행 입력 생성·변경을 완료하고 양쪽 자료와 catalog를 인증한다.
- `TestSnapshotAndInspectionDeadlinesAreNotUserCancellation`: 실제 배타 gate를 잡아 snapshot·inspection의 기한 초과 코드와 종료 코드를 검사한다.
- `TestModernPublicProfileNameMustMatchSelectedFile`: canonical identity/marker 쌍이 유효해도 내부 이름이 다르면 기존 `profilekey.Snapshot` 경계에서 거부하며, 변경 callback 실행과 덮어쓰기를 막는지 검사한다.
- 같은 profile 변경과 no-op 충돌, 커밋 시 동시 요청 replay, rotation/rewrap,
  수동 잠금·재-unlock, pending recovery, 취소, 최초 생성 경합을 별도 검사한다.
- `TestWriteSessionValidationPrecedesDiskLocks`는 session 재확인 중 두 disk 잠금을
  획득할 수 있는지 확인해 native 접근과 disk 잠금의 경계를 고정한다.

## 성능 측정

Linux/WSL2 amd64, Ryzen 7 9800X3D, Go 1.27.2에서 비교했다. 두 소스는 같은
`-buildvcs=false -trimpath` 옵션과 `-X main.version=analysis -X main.commit=REV`
옵션으로 빌드했다. 기준 소스는 별도 `/tmp` archive checkout에서 빌드했다.
[CLI 원시 결과](evidence/D01-mutations-4ce462e.json)는 바이너리 SHA-256, CLI identity,
실행 환경, harness SHA-256과 모든 표본을 보존한다.

각 arm/크기/반복은 독립된 격리 root와 따뜻한 passphrase session을 쓴다. 반복마다 arm
순서를 교대한다. 초기화·암호 해제·입력 생성·정리는 측정 밖이고 CLI wall time에는 GNU
time 시작 비용이 포함된다. 100/1,000개는 각 3회, 5,000개는 신규 import 1회다.
외부 호스트 부하는 통제하지 않았다. 값은 모두 synthetic이며 정확한 입력 크기는 원시에
기록한다. Import 결과·저장 inventory·no-op/preview 파일 불변·실제 child 실행도 확인한다.

| 작업 | 표본/arm | 기준 중앙값·범위(초) | 개선 중앙값·범위(초) |
| --- | ---: | ---: | ---: |
| 100개 신규, 주측정 | 3 | 0.094 (0.089–0.417) | 0.340 (0.051–0.411) |
| 100개 신규, 추가 측정 | 10 | 0.104 (0.091–0.117) | 0.064 (0.054–0.159) |
| 1,000개 신규 | 3 | 3.242 (3.240–3.245) | 0.063 (0.061–0.065) |
| 5,000개 신규 | 1 | 81.107 (81.107–81.107) | 0.106 (0.106–0.106) |
| 1,000개 overwrite | 3 | 6.536 (6.486–6.553) | 0.092 (0.090–0.094) |
| 같은 값 1,000개 | 3 | 0.029 (0.029–0.030) | 0.036 (0.035–0.036) |
| 1,000개 dry-run | 3 | 0.026 (0.026–0.027) | 0.026 (0.026–0.027) |

100개 주측정에서 큰 지연 변동을 관측해 [별도 10회 추가 측정](evidence/D01-mutations-small-followup-4ce462e.json)을 수행했다. 최초 표본은 제거하지 않았다. 단시간 작업의 변동 원인은 확정하지 못했다. 같은 값 1,000개 재등록은 이번 주측정에서 약 0.029초 → 0.036초로 늘었고, 새 경로는 no-op도 커밋 전에 원본을 재검사한다. 모든 작업이 같은 비율로 빨라진다는 뜻은 아니다.

5,000개 입력은 210,000 bytes다. 이 크기의 child 검증은 기존 4,096개 고유 redaction pattern 한도를 보존하며 단일 imported value bundle을 선택했다. 전체 profile MAC은 확인한다. [첫 중단 기록](evidence/D01-mutations-incomplete-4ce462e.json)은 full-profile child 검증이 실패한 partial run이다. 별도 candidate 재현에서 `redaction_unavailable`을 확인했고, 해당 5,000개 시도는 유효 표본에 포함하지 않았다. 수정한 harness의 완료 run을 위 표에 사용한다.

| 4,000개 import 중 다른 profile 작업, 각 1회 | 기준 | 개선 |
| --- | ---: | ---: |
| `run -- true` | 30.014초, exit 130 | 0.021초, exit 0 |
| `var get URL` | 52.660초, exit 0 | 0.009초, exit 0 |
| `sec list` | 52.660초, exit 0 | 0.009초, exit 0 |

기준 실행은 사용자 신호 없이 `canceled`/130으로 실패했다. 개선 실행은 성공했다. 시작 시 writer process가 살아 있었지만 출력 중인 경우도 있으므로, 계산 중 비차단의 결정적 근거는 앞의 active-batch 중단 검사다. 이 경합 표본 하나를 SLA나 p99로 해석하지 않는다.

| 신규 import의 CLI/waited-child peak RSS 중앙값(KiB) | 기준 | 개선 |
| --- | ---: | ---: |
| 1,000개 | 28,072 | 24,600 |
| 5,000개 | 66,000 | 36,248 |

[Overwrite benchmark 원시 결과](evidence/D01-overwrite-bench-4ce462e.txt)는 CLI와 별도
측정이다. 초기 보호·seed를 제외한 실제 암호화 overwrite와 파일 게시를 측정한다.
`-benchtime=200ms -count=3`의 각 calibrated batch 추정치 중앙값을 사용한다. 개별 CLI
요청의 p95/p99가 아니며 baseline 할당량 감소를 이 수치만으로 주장하지 않는다.

| 변경 키 | prepare(ms/op) | read held(ms/op) | commit wait(ms/op) | commit held(ms/op) | 할당 MiB/op | allocs/op |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 1.083 | 0.517 | 1.647 | 48.387 | 2.92 | 30,335 |
| 1,000 | 16.604 | 4.949 | 1.844 | 56.427 | 31.64 | 279,693 |
| 5,000 | 64.075 | 20.916 | 2.206 | 86.404 | 161.36 | 1,385,170 |

Read acquire 호출은 중앙값 약 0.04–0.06ms/op다. `wait` 지표는 해당 acquire 호출의 전체 비용으로, 순수 경합 대기와 경로 검사·잠금 준비·복구 확인 비용을 분리하지 않는다. Phase 합은 전체 Update 시간과 같지 않다. 초기 scope, 원본 인코딩, launch/key-slot lease 획득 등 나머지 비용도 있다.

잠금 보유에는 현재 snapshot의 검증·직렬화와 filesystem sync가 남는다. 단일 변경의
전체 snapshot 비용은 데이터 크기에 비례하며 `O(1)`이 아니다. 이번에 제거한 것은
변경 키 수마다 전체 작업을 반복하는 비용과 후보 계산 전체의 전역 배타 잠금이다.
RSS·할당량은 물리적인 secret 잔존이나 zeroization을 입증하지 않는다.

## 검증 상태와 한계

- `go test -race ./... -timeout=240s` 및 `go vet ./...`: 통과. 변경되지 않은 패키지에는 Go의 유효한 test cache를 재사용했다.
- 추가 active-batch·실제 gate deadline 검사와 경합 묶음의 `-race -count=1` 실행: 통과.
- Windows amd64/arm64, macOS arm64의 `values`·`vault`·`cli` 테스트 바이너리 교차 컴파일: 통과. 실행한 native OS 테스트는 아니다.
- [검증 상태·로그 digest·제품 파일 SHA-256](evidence/D01-validation-4ce462e.json)을 보존한다.

기존 A12의 native OS 수용은 그때의 소스에만 적용된다. 이번 변경은 실제 Windows
DPAPI·macOS Keychain·Linux Secret Service의 실행 검증을 새로 수행하지 않았다.
메모리 원문 추적·물리적 소거 검증·성능 SLA도 범위에 없다. Legacy 전환·backup/restore의
명시적인 전체 자료 처리와 복구 트랜잭션은 유지한다. Rewrap 공개 CLI는 별도 후속 기능이다.

재현 명령과 측정 범위는 [성능 안내](../../perf/README.md#measure-encrypted-bulk-writes)를 따른다.
