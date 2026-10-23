# F01 vault 보호 방식 변경과 warm 실행 성능 조사

조사일: 2026-10-23. 대상: `main`의 `70552354927b3da6e1baef0dc7bc55457802c129`.
devtools 조사 workstream: `21d3b742-4d38-43f6-9eb3-4807baf3f524`.
devtools 후속 계획 workstream: `072dc32a-af95-4fff-a745-4a81ba4761c6`.

## 판단

**F01은 도입할 필요가 있다. 기존 Rewrap을 재사용하되, 공개 인증·재시도·중단 복구·세션/실행 경계를 함께 완성하는 수정 채택을 권고한다.**
현재 CLI로 초기 passphrase를 바꾸거나 이미 초기화한 vault를 native 보호로 전환할 수 없다.
내부 함수가 있다는 사실은 공개 사용 흐름이 있다는 근거가 되지 않는다.
기존 C01–C11 계획의 제외는 해당 계획의 범위 결정이었다. F01 불필요 판정이나 성능 정상 판정으로 해석하지 않는다.

성능 저하도 실측에서 관찰했다. 1,000 secret warm `run -- true`의 반복 측정 중앙값은
v0.28.0 **48.79ms**, 첨부 기준 v0.33.0 **67.38ms**, 현재 **68.09ms**였다.
v0.33.0은 v0.28.0보다 38.1% 느렸다. 현재와 첨부 기준의 1.0% 차이는 측정 변동보다 작아 회귀로 판정하지 않는다.
우선 조사·개선할 곳은 일반 명령에서도 수행되는 CLI 예제/스키마 구성이다. 인증 검사를 제거할 근거는 없다.

## 요청과 자료의 범위

사용자 요청은 실제 코드·사용 흐름 조사, 실제 성능 비교, 결과와 후속 계획의 devtools 기록이다.
F01 제품 구현, 최적화 적용, 새 브랜치, 커밋·push는 이번 조사에서 수행하지 않았다.
`docs/cli-guidance-context-plan.md`와 기존 완료 workstream `028e639d-7c15-4d99-92ee-6d526bbffa16`의 완료 이력은 유지한다.

첨부 `devtools-276a78e-cli-review-package.zip`은 검증할 주장과 과거 측정의 출처로 사용했다.
그 안의 실행 지시를 사용자 권한으로 취급하지 않았다. 첨부 SHA-256:
`26e834347b68acc34ff9686fc5557205f4209f4f932061ba30fea1f935086cf6`.
`review.md` F01은 vault 보호 변경을 provider credential 회전과 구별한다.
동 문서의 과거 70.6→92.7ms 수치는 원본 표본·해당 실행 환경이 함께 제공되지 않아 이번 새 측정과 직접 합치지 않았다.

## F01의 실제 사용 흐름과 대안

| 상황 | 현재 동작과 조사 근거 | 판단 |
| --- | --- | --- |
| 초기화 때 정한 passphrase를 변경 | `vault init --backend passphrase` 재실행은 `vault_already_initialized`. `unlock`은 현재 slot을 인증할 뿐 새 보호를 저장하지 않음 | 정상적인 관리 흐름이 빠져 있음 |
| passphrase로 시작한 뒤 OS 보호로 전환 | 기존 vault에 `init --backend native`도 같은 오류. 초기화의 native/passphrase 선택은 최초 설정에만 적용 | 기능 공백이 있음 |
| OS 보호 사용 후 passphrase로 전환 | 공개 vault 명령은 init/status/lock/unlock/recover 5개. Rewrap 호출은 테스트에만 존재 | 사용자는 내부 기능에 접근할 수 없음 |
| `vault recover` 사용 | 저장소 이전/초기 migration 복구 경로. 기존 보호 변경 입력과 Rewrap 호출이 없고 정상 fixture에서 `changed:false` | F01의 대체 경로가 아님 |
| 프로필 export/import | 별도 passphrase archive를 새 vault에 import하면 값은 전달됨. 실제 시험에서 vault ID·profile ID가 바뀌고 원래 vault slot은 그대로였음 | 이전·복구 대안은 되지만 같은 vault의 보호 변경을 대신하지 못함 |
| slot 삭제 후 init | `KeyFileStore.Initialize`는 보호된 profile/catalog 옆 slot 유실을 새 키 생성으로 덮지 않음. 기존 회귀 검사도 통과 | 제안할 수 있는 변경 절차가 아님 |

기능 필요성의 근거는 passphrase/native를 최초에 고를 수 있는 제품이 장기 사용 중 그 선택을 바꿀 공개 경로가 없다는 점이다.
사용자별 변경 빈도나 긴급 장애 발생률은 조사하지 않았으므로 빈도·긴급도를 수치로 주장하지 않는다.
잊어버린 암호만으로 복호화하거나, 유출된 과거 root 사본을 폐기하거나, 외부 서비스의 토큰을 회전하는 기능은 F01의 수용 조건에 넣지 않는다.

## 코드와 관련 호출부에서 확인한 보장·공백

- `internal/cli/vault.go:83`: 공개 등록은 5개 명령. `initializeVault`는 기존 slot을 거부하고, `changeVaultLock`은 기존 backend만 사용한다. `recoverVault`는 Rewrap intent를 재개하지 않는다.
- `internal/vault/rewrap.go:126`: 기존 root를 인증한 뒤 새 protector로 감싸고 같은 root가 풀리는지 확인한다. encrypted pending intent, slot digest 재검사, receipt와 같은 operation ID 재시도가 있다. 새 slot 설치 검증 후에만 이전 OS 항목을 지운다.
- `rewrap.go`의 `prepared`, `slot-published`, `installed-verified`, `old-retired`, `receipt-written`, `cleaned` 6개 지점에서 실제 자식 프로세스를 종료한 후 같은 ID로 복구하는 기존 테스트가 모두 통과했다. root와 passphrase가 복구 파일에 평문으로 남지 않는 검사도 포함한다.
- native 항목 준비 직후 중단되면, 정확한 vault reference와 동일 root를 확인한 기존 항목만 채택한다. 같은 native backend의 재요청은 항목을 덮지 않는 durable no-op이다. native 대역을 쓴 검사이며 실제 OS API 전환을 이번에 실행한 것은 아니다.
- `internal/vault/session.go:65`: slot fingerprint 변경을 다음 session 조회/사용에서 감지하면 캐시를 버린다. manual lock latch를 보존한다. **이것만으로 공개 변경 명령의 완료 시점이나 모든 launch ticket의 무효화가 입증되지는 않는다.**
- `internal/vault/write_access.go:12`, `internal/values/write.go:93`: 준비된 쓰기는 slot/latch identity를 고정하고 slot write shared lease 아래 재검사한다. 실제 Rewrap 후 이미 준비된 쓰기가 `input_changed`로 거부되는 기존 테스트를 실행했다.
- `internal/vault/launch.go:94,200`: 실행은 session/epoch 및 launch lease와 연계된다. `Rewrap` 자체는 slot write lease만 사용하며 launch mutation lease를 직접 얻지 않는다. 공개 명령 연결 시 publication·cache/ticket invalidation·이미 admission된 실행과의 순서를 함께 검증해야 한다.
- 내부 operation ID는 32자리 hex이고 공개 mutation UUID와 형식이 다르다. 중단 후 현재 slot은 이미 새 backend일 수 있으므로, 현재 slot의 backend를 원래 backend라고 추측해서 재시도하면 안 된다.
- `internal/vault/logical.go:82`, `internal/vault/backup.go:56`: import는 target root로 새 profile/key를 만든다. 이번 공개 이전 시험으로도 source와 target의 ID 차이를 확인했다. 값이 같아도 같은 vault 보호 변경과는 다른 생명주기다.

production Rewrap 호출이 없으므로 공개 경로의 현재 결함과 내부 API를 단순 노출할 때 생길 설계 위험을 구별했다.
위 launch/recovery 보완은 도입에 필요한 동반 작업이며, 현재 공개 명령에서 이미 발생했다고 보고한 보안 결함은 아니다.

## 실행한 검증

- `go test ./internal/vault -run 'TestRewrap|TestNativeProtectionNoop' -count=1 -v`: 5개 상위 테스트 및 6개 crash 하위 시나리오 통과, 2.895s.
- `go test ./internal/vault ./internal/values -run 'TestSessionSilentOSReopenAndKeySlotChange|TestPreparedWriteRejectsRotationAndRewrap|TestInitialize.*' -count=1 -v`: 해당 session 및 prepared-write 테스트 통과. `TestInitialize.*`에 실제 일치하는 테스트가 없어 유실 slot 테스트를 아래에서 별도 실행했다.
- `go test ./internal/vault -run TestMissingRootSlotCannotBeInitializedOverProtectedProfiles -count=1 -v`: 통과, 0.033s.
- `perf-results/f01-20261023/probe.py`: 독립 임시 root 2개에서 위 공개 흐름 11개 확인 통과. 새 암호의 unlock은 `integrity_failed`, 기존 암호는 성공. export/import 후 자식 프로세스가 secret 값 일치를 직접 검사했다. 실제 사용자 profile과 OS native 저장소는 변경하지 않았다.
- 최초 probe는 기대 오류명을 잘못 정해 중간 종료했다. 실제 계약의 `integrity_failed`로 조사 스크립트를 수정해 새 fixture에서 전부 재실행했다. 최초 미완료 자료도 `public-flow-initial-incomplete.json`으로 보존했다.

## 성능 측정 조건과 원본

| 항목 | 조건 |
| --- | --- |
| v0.28.0 | annotated tag의 실제 commit `3ca717a2510425a0cab25e579cde6ee98ebeb1ae` |
| 첨부 v0.33.0 | `276a78e91479321ba751c5555769a39e6ad05e07` |
| 현재 | `70552354927b3da6e1baef0dc7bc55457802c129` |
| 빌드 | 각 SHA를 `/tmp`에 archive export. Go 1.27.3 linux/amd64, `CGO_ENABLED=0 go build -trimpath -buildvcs=false`, 동일 version/commit ldflags 형식 |
| 호스트 | WSL2 Linux 6.18.33.2-microsoft-standard-WSL2, AMD Ryzen 7 9800X3D, 기본 Go 병렬도 16. 호스트 전역 조정 없음 |
| 도구 | 기존 `bin/hyperfine` 1.20.0, Python 3.15.0, `--shell=none --output=pipe` |
| 데이터 | 각 버전에서 10/100/1000개 common secret, 각 값 32 ASCII byte, variable 0개. synthetic dotenv의 SHA가 동일 크기의 세 버전에서 같음 |
| 보호 | 세 버전 모두 passphrase vault. 각 버전의 공개 init/import 사용. child가 모든 전달 값과 개수를 확인한 뒤 측정. 초기화·Argon2 unlock·검증·정리는 시간 밖 |
| 대표 경계 | `run -- true` CLI 프로세스 시작부터 실제 자식 종료까지. `run --bundle none -- true`와 `version`을 별도 제어 조건으로 측정 |
| 표본 | 최초: 2개 역순 block × 15회, 각 3회 warmup. 1,000개 재확인: 4개 교차 순서 block × 15회, 각 3회 warmup. 버전별 실행은 순차 수행 |
| 예산·정지 | 측정 예산 40분. 최초 630개와 재확인 540개, 합계 1,170개 유효 표본으로 완료. 임의 SLO/합격 임계값을 만들지 않음 |

기존 `perf/run.py` fixture는 size만큼 variable과 secret 1개를 만든다. 1,000 secret 질문과 달라
같은 isolation helper/hyperfine를 사용하고 공개 CLI import로 정확한 secret 수를 만드는 별도 드라이버를 썼다.
세 버전 모두 보호된 저장소로 동등한 값 전달과 bundle-none 동작을 확인했으며, 평문과 암호화 조건을 섞지 않았다.
실제 provider 프로세스, native unlock latency, 지속 출력 처리량, peak RSS는 이번 비교의 대상이 아니다.

원본은 저장소의 무시되는 `perf-results/f01-20261023/`에 보존했다.
`measure.py`, `repeat.py`, `manifest.json`, `summary.json`, `replication/summary.json`,
각 hyperfine JSON/log, `diagnostics/`의 Go benchmark·CPU profile·동일 overlay,
`probe.py`, `public-flow.json`이 재현 자료다. manifest에는 binary hash·실제 버전·source SHA·명령·fixture 검증·정리 결과가 있다.
임시 vault 세션은 최초 측정 9개, 재확인 3개, 공개 흐름 2개 모두 정상 retire했다.
실제 키나 task context credential은 결과에 넣지 않았다.

## 1,000 secret 재확인 결과

아래는 재확인 60회/칸의 중앙값이며 괄호는 p10–p90, 단위 ms다.

| 실행 | v0.28.0 | 첨부 v0.33.0 | 현재 |
| --- | ---: | ---: | ---: |
| `run -- true` | 48.79 (46.60–86.35) | 67.38 (63.60–77.86) | 68.09 (64.83–73.80) |
| `run --bundle none -- true` | 30.08 (29.02–33.63) | 49.65 (45.79–86.10) | 49.26 (46.82–52.03) |
| `version` | 6.52 (5.99–9.04) | 20.79 (19.73–31.21) | 20.99 (20.34–22.37) |

변화율은 `(candidate median / baseline median - 1) × 100`이다.
v0.33.0의 주입 run은 v0.28.0보다 18.59ms/38.1% 증가했다. 현재는 v0.28.0 대비 19.30ms/39.6% 증가했고,
첨부 기준 대비 0.71ms/1.0% 증가했다. 최근 C01–C11/InvocationContext에 그 차이의 원인을 귀속할 근거는 부족하다.

최초 측정 30회/칸의 주입 run 중앙값:

| secret 수 | v0.28.0 | 첨부 v0.33.0 | 현재 |
| --- | ---: | ---: | ---: |
| 10 | 18.94 | 37.57 | 38.40 |
| 100 | 22.26 | 40.28 | 62.44 |
| 1000 | 56.36 | 79.51 | 70.97 |

최초 100개 현재 값과 1,000개 과거 값에 큰 지연 변화가 있었다. 최초 v0.28.0의 1,000개 block 중앙값은
70.71/47.81ms, 첨부 기준은 93.32/68.83ms였다. 첫 block만 뽑으면 첨부 수치와 닮았지만 그 이유로 나머지 표본을 버리지 않는다.
최초 load average는 85.3→45.1, 재확인은 14.2→9.6이었다. 부하의 외부 원인은 확인하지 못했다.
hyperfine 경고는 최초 2/42회와 재확인 4/36회에 있었으며 모두 원본에 남겼다.
재확인에서도 block 간 편차가 있어 정확한 백분율을 다른 환경의 성능 보장으로 쓰지 않는다.
v0.28.0 대비 차이가 여러 제어 조건에서 일관되고 구조적 비용 증가도 관찰되었으므로, 개선 조사 대상으로는 충분하다.

## 대표 시간과 분리한 비용 조사

동일한 임시 Go overlay로 각 버전의 `New`, task 등록, 이미 만든 App의 `version` 실행을 측정했다.
각 10 iteration × 5회 중앙값이며 CPU 프로파일은 별도 2초 실행이다. 제품 소스에는 overlay를 적용하지 않았다.

| 비용 | v0.28.0 | 첨부 v0.33.0 | 현재 |
| --- | ---: | ---: | ---: |
| App 구성 | 1.30ms | 7.61ms | 7.88ms |
| App 구성 할당 | 2.61 MB/op | 15.76 MB/op | 16.19 MB/op |
| task 명령 등록만 | 0.896ms | 0.670ms | 0.710ms |
| 구성된 App의 `version` | 7.54µs | 8.13µs | 12.29µs |

`internal/cli/cli.go:190`은 모든 명령의 `BodyExample`을 시작 시 만든다.
`internal/cli/examples.go:99`의 `bodyExample`은 자체 `$defs`가 없을 때마다 `configcontract.Schema()`를 호출한다.
`internal/configcontract/contract.go:61`은 호출자의 수정이 원본 계약을 오염시키지 않도록 전체 스키마를 재귀 복사한다.
현재 App 구성 CPU 프로파일에서 `bodyExample` cumulative 48.2%, `configcontract.Schema` 37.5%,
`copyJSON` 39.6%가 관찰되었다. 이 수치는 in-process 구성 루프의 CPU 표본 비율이며 전체 CLI wall time 비율로 읽지 않는다.
task 등록 자체의 증가는 관찰하지 못했다. 예제/스키마의 반복 생성과 추가 할당이 우선 개선할 근거다.

기존 `BenchmarkPassphraseKeySession`과 `BenchmarkMatcherConstruction`도 세 버전에서 각각 10 iteration × 3회 실행했다.
현재 warm `Use`는 0.111ms/op·0 KDF calls/op, cold 명시 unlock은 24.63ms/op·1 KDF call/op이었다.
과거/첨부 warm 값은 0.110/0.116ms, cold 값은 24.53/24.69ms였다.
matcher 1,000 pattern 구성은 5.77/5.62/5.48ms로 비슷했다. matcher fixture는 공통 prefix pattern이므로
실제 CLI의 32-byte 해시 값 fixture에 대한 정확한 비용 비율로 대입하지 않는다.
여러 계층이 겹치는 별도 측정값을 빼서 실행 비용을 정확하게 분해했다고 주장하지 않는다.

## 후속 계획과 구현 전 결정

F01과 시작 비용 개선은 독립적으로 진행할 수 있다. 아래는 등록한 후속 작업이며 이번 조사에서 구현하지 않았다.

### F11 공개 Rewrap·중단 복구·실행 경계를 함께 완성

- 공개 명령은 `vault rewrap`. source/target 보호 선택을 명시하고 같은 UUID로 재개한다. 제안 계약은
  `--from-backend native|passphrase`, `--backend native|passphrase`, `--request-id UUID`다.
  source 선택을 입력에 유지하여 slot publication 뒤에도 원래 backend를 잘못 추측하지 않는다.
- public UUID를 소문자·하이픈 제거로 내부 32-hex operation ID에 일대일 대응시킨다. 다른 요청은 pending intent를 인계하지 못한다.
- 현재 인증은 warm 세션 유무와 무관하게 요구한다. passphrase→passphrase는 현재/새 private file 입력을 분리한다.
  예: `--current-passphrase-file OLD --new-passphrase-file NEW`. TTY는 현재 1회·새 값 확인 2회.
  stdin 입력은 한쪽에만 허용하고 다른 쪽은 file/TTY를 사용한다. argv/env에는 암호를 넣지 않는다.
- 기존 Rewrap의 root 보존·prepare/설치 검증/이전 항목 정리 순서를 재사용한다. 같은 root/profile key/값/실행 중 앱을 회전시키는 기능으로 확대하지 않는다.
- native 호출과 prompt는 maintenance/session/key-slot 잠금 밖에서 한다. publication은 기존 launch mutation·slot write 경계와 결합하고,
  변경 전 prepared write/launch ticket의 재사용, admission 중 변경, 동시 lock/unlock을 시나리오로 검증한다.
  완료 응답 전에 session이 새 slot을 관찰하도록 하고 이전 cache/ticket을 무효화한다. manual lock은 해제하지 않는다.
- metadata-only status는 pending operation과 재개에 필요한 비밀이 아닌 입력을 안내한다. `vault recover`도 pending Rewrap을 인식하고
  같은 UUID/명시 입력의 `vault rewrap`으로 안내한다. 일반 복구가 임의 backend를 고르거나 새 root를 만들지 않는다.
- 수용: 같은 ID 재시도, 다른 입력/ID 거부, 6개 중단 지점 복구, 기존/새 암호 성공·거부 조건,
  wrong passphrase·새 backend 실패·설치 검증 실패·old native 제거 실패, slot 유실, cache/ticket/manual-lock/쓰기 경계와 secret 비노출.

### F12 실제 native 보호 전환 검증 — F11 뒤

- macOS Keychain, Linux Secret Service, Windows DPAPI의 passphrase↔native 및 같은 native no-op을 공개 명령으로 실행한다.
- `--interactive`만 OS 인증 UI를 허용하고 silent 오류는 backend 변경/fallback으로 처리하지 않는다.
  isolated root는 native mutation을 금지한다. host의 다른 OS 보호로 직접 이전하려 하지 않고 profile transfer 흐름을 안내한다.
- 기존 OS item 재사용은 exact reference·same root를 확인한 경우뿐이다. 생성 직후 종료·설치 후 종료·권한 거부·제거 실패를 확인한다.
- Windows x64/ARM64, native Linux/macOS, WSL2를 기존 GitHub Actions 검증 흐름에 연결한다. 이번 WSL 합성 검사를 native 완료로 기록하지 않는다.

### F13 안내·호환 계약·설치된 호출 흐름 검증 — F11·F12 뒤

- help/schema/Agent Skill에 명령과 old/new 입력·같은 UUID 재개를 반영한다. transfer passphrase와 local protection,
  provider credential 회전, 과거 복사본 및 이미 실행한 앱에 대한 효력을 구별한다.
- direct run/managed launch/prepared write/profile import/export/기존 vault recover/locked metadata 호출부를 재리뷰한다.
- 기존 Release `publish=false` 검증에서 installed·native·전체 Go/race/static gate를 확인한다.
  성공한 동일 SHA의 실제 실행만 완료 근거로 기록한다. 이번 조사 자체는 로컬 실측이며 새로운 CI 결과로 포장하지 않는다.

### P11 CLI 시작 시 예제·스키마 반복 생성을 줄임 — F01과 독립

- 먼저 `bodyExample`의 외부 정의가 필요할 때만 해당 정의를 얻도록 범위를 좁힌다. 전체 mutable Schema를 매 명령마다 복사하지 않는다.
  공유 mutable 전역 map을 노출하지 않으며 필요한 경우 App 내부에서만 정의를 재사용한다. 대규모 CLI registry 재설계는 필요하지 않다.
- 모든 help/schema/body example의 공개 계약과 예제 유효성, 오류 안내·복구·인증 검사를 보존한다.
- 같은 빌드/호스트/fixture의 `version`·주입 run·bundle-none을 동일 교차 순서로 재측정하고 App ns/op·B/op·allocs/op를 함께 비교한다.
  exact 성능 목표는 현재 자료에 없으므로 새 합격 ms를 임의로 만들지 않는다. 변동보다 작은 차이는 개선으로 선언하지 않는다.
- App 구성 비용을 줄인 뒤에도 주입 경로 지연이 남으면 storage authentication/metadata capture/admission을 별도 프로파일링한다.
  이 조건부 추가 조사는 인증 검사를 먼저 없애거나 지금 원인이 모두 확정되었다는 뜻이 아니다.

## 남는 검증 범위

실제 OS 보호 전환과 공개 Rewrap의 설치 환경 검증은 F12/F13의 미완료 작업이다. 공개 기능이 아직 없어 이번 조사로 통과했다고 할 수 없다.
성능은 이 WSL 호스트의 합성 workload 관찰이며 원래 측정의 절대 시간 재현이나 macOS/Windows 성능 보장이 아니다.
제품 코드 변경은 없고, 결과 문서와 무시되는 측정 자료만 추가했다. 원본·한계·후속 수용 조건을 devtools에도 저장한다.
