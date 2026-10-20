# D02 — Batch 변경의 비게시 릴리스 검증

검증 대상은 `e16c431f6870a18be4b462217e7d4d0f7801543f`, 버전은 `0.28.1`이다.
[Release 실행 39374333363](https://github.com/jinyongp/devtools/actions/runs/39374333363)은
`publish=false`, `system_network_smoke=true`로 요청했다. 검증과 패키징을 수행하며
태그·GitHub Release·Homebrew·installer Pages를 게시하지 않는다.
이후 main 병합과 릴리스 도구 변경은 [Releaseway 업데이트 기록](../releases/releaseway-upgrade.md)에 구분한다.

## 현재 상태

최종 실행은 성공했다. 필수 26개 job이 통과했고 게시용 `release`·`homebrew`·
`installer-page` 3개만 의도대로 skip했다. Windows x64/ARM64 전체 제품·설치 검사,
x64 race, macOS/Linux CI, DPAPI·Keychain·Secret Service, 실제 WSL 재시작,
시스템 네트워크, 네 플랫폼 간 profile 교환과 패키징을 포함한다.

[검증 기록](evidence/D02-release-e16c431.json)에 job URL·상태와 배포물 checksum을 보존했다.
[release-assets](https://github.com/jinyongp/devtools/actions/runs/39374333363/artifacts/12242638206)의
여섯 CLI archive와 Skill archive를 내려받아 7개 SHA-256을 모두 확인했다.
`install.sh`·`install.ps1`은 후보 소스와 같고 shell 구문 검사를 통과했다.
실제 Linux amd64 archive의 실행 파일은 버전 `0.28.1`, 위 소스 SHA,
protocol `7`을 반환했다. 이 artifact의 보존 기간은 30일이다.

2026-10-20 최종 조회에서 공개 최신 안정 버전은 여전히 `v0.28.0`이었다.
`release:plan`은 기존 `ENOBUFS`로 실패했으므로 공개 릴리스·도달 가능한 태그·이후
fix/perf 커밋을 기존 계산 규칙으로 비교해 `0.28.1`을 선택했다.
이번 소스의 비게시 검증은 완료했으며 실제 게시는 수행하지 않았다.

## 검증 중 발견한 문제와 수정

- [첫 실행 39370235363](https://github.com/jinyongp/devtools/actions/runs/39370235363)의
  소스 `c9e53d8`은 Linux/macOS의 설치된 CLI 저장소 전환 시나리오에서 실패했다.
  구형 profile의 메모리 변환마다 새 UUID가 생겨 준비 시점과 커밋 시점의 변환 결과가 달랐다.
  `79fa0c0`은 유지보수 잠금 안에서 저장 원본을 읽고 비교하도록 바꿨다.
  구형 profile의 변경·동일 값 저장과 실제 동시 변경 거부를 함께 검사한다.
- [두 번째 실행 39371304306](https://github.com/jinyongp/devtools/actions/runs/39371304306)의
  소스 `79fa0c0`에서는 새 테스트 fixture의 디렉터리가 Windows private ACL을 갖지 않아 실패했다.
  `dcde412`는 기존 `platformfs.MkdirPrivate`와 플랫폼별 파일 helper를 사용한다.
  별도 ARM64 위 x64 PowerShell 설치 검사에서는 프로세스가 `0xC0000005`로 종료됐다.
  이 진단만으로 충돌의 내부 원인은 확정하지 않았다. 이후 실행에서는 해당 설치 검사가 통과했다.
- [세 번째 실행 39371957649](https://github.com/jinyongp/devtools/actions/runs/39371957649)의
  소스 `dcde412`은 ARM64의 `TestNormalReadersShareMaintenanceGate`에서 실패했다.
  조회 확인용 2초 context를 durable fixture 쓰기에도 사용하고 있었다.
  `d255d65`는 fixture를 먼저 준비하고 잠금을 잡은 채 조회 완료를 검사한다.
  10초 제한은 잠금 회귀의 대기를 제한하며 성능 SLA가 아니다.
  이전 후보의 남은 실행과 중복 push 실행은 최종 Release 검증의 대기열을 위해 취소했다.
  취소된 검사는 성공으로 계산하지 않는다.
- [네 번째 실행 39372881874](https://github.com/jinyongp/devtools/actions/runs/39372881874)의
  소스 `d255d65`는 Windows 전체와 Linux 검사를 통과했지만 macOS의
  `TestWriteSessionValidationPrecedesDiskLocks`가 3초 context를 초과했다.
  이 제한도 vault fixture 생성 전에 시작하고 있었다.
  `e16c431`은 생성 뒤에 검증 context를 시작하고 같은 유형의 잠금·pause 검사를 함께 점검했다.
  실제 유지 중인 잠금, 후보 pause, 충돌·취소·deadline 구분 조건을 유지하며
  대기 상한을 조회 10초·작업 30초로 정했다. 성능 측정의 통과 기준을 바꾼 것이 아니다.

제품 Go 변경은 `79fa0c0`까지이며 `dcde412`·`d255d65`·`e16c431`은 테스트만 변경한다.
수정마다 diff·호출 경계·실패 보존을 재검토하고 커밋했다.
로컬 전체 `go test -race ./... -timeout=240s`와 `go vet ./...`,
설치된 CLI의 `storage_migration` 시나리오는 제품 수정 후 통과했다.
공유 조회·deadline 구분·구형 profile 회귀 검사는 race 모드에서 5회 반복해 통과했다.
마지막 테스트 변경 뒤 준비/커밋·실제 batch 준비·공유 조회·동시 생성·session 잠금 순서·
충돌·취소·deadline 구분·구형 profile 회귀 검사를 race 모드에서 3회 반복해 통과했다.
전체 native 판정은 위 최종 실행의 결과를 따른다.

## 근거의 적용 범위와 다음 단계

[D01 성능 측정](D01-batch-mutation-review.md)은 소스 `4ce462e`의 결과다.
저장 원본 비교를 바꾼 이번 소스의 성능 측정으로 재표기하지 않는다.
기존 [플랫폼·복구 한계](../releases/v0.28.0.md)는 유지한다.

실제 게시에는 버전을 다시 확인하고 검증한 SHA가 원격 main의 ancestor인지 확인해야 한다.
같은 소스·버전으로 Release를 `publish=true`, `system_network_smoke=true`로 실행하면
필수 검사 뒤 게시가 진행된다. main 병합·게시·게시 후 설치 확인은 이 비게시 검증에서 수행하지 않았다.
태그를 다른 커밋으로 교체하거나 immutable 자산을 덮어쓰지 않는다.
