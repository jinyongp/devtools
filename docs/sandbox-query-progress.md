# 샌드박스 조회 구현 진행

기준 계획: [sandbox-query-plan.md](sandbox-query-plan.md).
Workstream: `9bb41be8-11bd-45b2-86fb-59092b6aad5e`, profile `devtools`.

2026-10-23 사용자가 계획에 따른 전체 구현·작업별 검증/리뷰·최종 영향 리뷰와
macOS 필수 검증을 승인했다. 계획 문서 및 최초 workstream 설명의
“조사·계획 기록만, 구현 미착수”는 최초 등록 당시 범위이며 이 실행 요청으로 대체됐다.
사용자가 새 branch 대신 main에서 계속 진행하고 기존 GitHub Actions를 사용하도록 승인했다.
필수 native 검증에 필요한 main commit/push 및 기존 workflow 실행을 진행한다.
merge·release는 승인 범위가 아니다.

## T01

상태: 실행 중. 로컬 구현·리뷰 완료, native macOS 결과 대기.
실행 run: `1d86dd07-37a3-4139-b4b2-03133a07b170`.

변경:

- `verify/probes/sandbox_query_lock.go`: Go O_RDONLY flock과 기존 O_RDWR writer,
  실제 쓰기 차단 5종, 취소·close 동작의 native probe.
- `verify/sandbox_query_fixture.py`: Linux bwrap/macOS sandbox-exec launcher,
  비공개 환경 격리, pipe readiness, 기존 writer와의 양방향 차단·동시 reader·
  취소·잠금 해제·fixture inventory 검증.
- `verify/test_sandbox_query_fixture.py`: sandbox 경계, SBPL 경로 quoting,
  scratch containment, credential 환경 배제, 실패 시 holder 종료 검사.
- `.github/workflows/unix-platform.yml`: macos-15/ubuntu-24.04 native 필수 job과
  source/kernel/filesystem/binary/검증 결과 artifact 수집.

검증 결과:

- Python harness unit 5개 통과.
- 기존 `scripts/test_project.py` 8개 통과. 새 Go probe의 기존 lane 포함 확인.
- Go probe build, `go test ./verify/probes`, `go vet ./verify/probes` 통과.
- WSL2 Linux 실제 read-only mount에서 O_RDWR open/생성/write/rename/unlink 차단,
  old writer→reader 및 reader→old writer 배제, concurrent reader, timeout,
  close 후 재획득, inventory 불변 모두 통과.
- 로컬 native 증거: `/tmp/devtools-sandbox-t01-evidence/native-lock.json`.
  이 증거는 macOS나 제품 AcquireQuery 구현 결과를 대신하지 않는다.
- `git diff --check` 통과.

리뷰:

- 실패·timeout 시 bwrap의 자식이 남지 않도록 `--die-with-parent` 추가하고 재검증했다.
- scratch가 data의 상위/하위/동일 경로이면 거절한다. 실제 쓰기 음성 대조 없이
  sandbox 실행 성공만으로 검증 통과를 판단하지 않는다.
- CI 도구/권한 미준비는 실패하며 skip을 성공으로 취급하지 않는다.
- 제품 코드와 기존 maintenance/values/CLI 동작은 아직 변경하지 않았다.

남은 필수 증거: native macOS T01. 현재 호스트는 Linux이며 macOS 실행 환경은
GitHub Actions macos-15로 준비했다. 사용자가 승인한 main commit/push로 변경을 전달한다.
T01의 V01을 pass 또는 task done으로 기록하지 않았다. T02~T05는 계획의 의존성을 지켜 미착수.

다음 동작: main에 검토된 변경을 commit/push하고
Unix validation을 dispatch한다. T01 native macOS/Linux 결과를 확인하고 task 검증/완료를
기록한 뒤 T02를 claim한다. macOS 정책이 O_RDONLY flock을 막으면 계획대로 명시적
blocker로 보고하며 잠금 우회·권한 확대·별도 broker로 대체하지 않는다.
