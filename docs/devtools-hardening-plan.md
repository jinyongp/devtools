# devtools 안정성·운영성 개선 계획

상태: 구현 계획
기준: 2026-09-18 `main` / `v0.15.1`

## 목표

현재 기능 집합을 더 안정적으로 확장할 수 있도록 검증 경계와 실행 경계를 정리하고, 프로젝트·profile 운영에서 반복되는 저수준 절차를 줄인다.

핵심 목표는 다음과 같다.

1. 태그가 push된 뒤 실제 GitHub Release와 Homebrew 게시가 시작되기 전에 macOS/Linux/Docker/Dashboard 검증을 독립 CI 단계에서 모두 통과하도록 한다.
2. Dashboard JavaScript 회귀 테스트를 공식 `just check` gate에 포함한다.
3. 이름 명령의 env·requirements·port/binding·실행 파일 준비 규칙을 하나의 application 계층으로 모아 `command run`, `doctor`, `process`, `project`, Dashboard의 의미 drift를 막는다.
4. project lifecycle에서 자주 필요한 로그 조회와 재시작을 execution ID를 직접 찾지 않고 수행할 수 있게 한다.
5. profile 폐기, 통합 diagnostics, retention, 실제 browser smoke의 후속 확장을 저장 모델과 보안 경계를 깨지 않는 방식으로 진행한다.

## 결정 사항

### Release

- 태그 생성과 push 방식은 현재 로컬 흐름을 유지한다.
- 실패한 release tag는 삭제·재사용하지 않고 다음 patch 버전으로 진행할 수 있다.
- 이번 범위에서 별도 main CI나 exact-HEAD release gate를 추가하지 않는다.
- Release workflow는 `validate-tag -> ci-macos + ci-linux -> release -> homebrew` 구조로 정리한다.
- `release` job은 검증을 수행하지 않고 검증 완료 artifact를 패키징·게시하는 역할에 집중한다.
- `contents: write` 권한은 실제 release job에만 둔다.

### Dashboard test gate

- 기존 `scripts/dashboard-*.test.mjs`를 새 `just check-dashboard` recipe에서 직접 `node --test`로 실행한다.
- `just check`가 `check-dashboard`를 포함한다.
- CI에서는 Node 버전을 명시적으로 준비해 runner 기본 설치 버전에 기대지 않는다.
- pnpm 설치는 Dashboard unit test 실행에 필요하지 않으므로 check path에 추가하지 않는다.

### 실행 준비 계층

새 `internal/execution` 패키지가 사용자-facing command 실행의 공통 준비 규칙을 소유한다.

목표 의존성 방향:

```text
project / values / ports / doctor / process
                 ↓
             execution
                 ↓
        cli / services / dashboard
                 ↓
             lifecycle
```

`execution`은 `services`, `cli`, `dashboard`, `lifecycle`을 import하지 않는다.

공통 계층은 다음을 단계별로 제공한다.

- 이름 명령과 env override를 해석해 effective env, inject, merged requirements를 만든다.
- 필요할 때 profile value state를 읽고 env 유효성을 확인한다.
- port/binding을 dry-run 또는 reservation 모드로 준비한다.
- effective executable/PATH를 계산한다.
- `doctor.CheckRequirements` 기반의 동일한 structured checks를 반환한다.
- 실제 foreground/supervisor 실행에서 같은 prepared command와 injected environment를 사용한다.

`services.Store`는 durable process/idempotency storage를 계속 소유하고, command 의미를 복제하지 않는다. CLI와 Dashboard의 public start 경로는 공통 execution preflight를 거친 뒤 Store mutation으로 들어간다.

Supervisor가 CLI `runCommand`를 다시 호출하는 현재 결합은 공통 execution runner가 준비되면 제거한다.

## 조사 결과

### Release workflow

현재 workflow도 게시 전에 검증하지만 Linux `just check`와 `just verify-docker`가 `contents: write`를 가진 `release` job 내부에 있다. macOS 검증은 별도 job이므로 두 OS 검증 구조도 비대칭이다.

따라서 게시 전 CI를 별도 job으로 분리하는 변경은 의미가 있다. 태그가 먼저 존재하는 것은 허용된 운영 모델이다.

### Dashboard

`package.json`에는 `test:dashboard`가 있고 현재 11개 Node test가 통과한다. 그러나 `just check`에는 포함되지 않는다. Release의 macOS/Linux 검사도 `just check`만 호출하므로 Dashboard JS regression은 release gate 밖에 있다.

### Execution/preflight

현재 비슷한 command 준비 규칙이 다음 위치에 중복된다.

- `internal/cli/run.go`
- `internal/cli/doctor.go`
- `internal/cli/project_lifecycle.go`

`process start`와 Dashboard process mutation은 `services.Store.Apply`로 직접 들어가며, supervisor가 나중에 CLI `runCommand`를 호출하면서 최종 실행 검사를 다시 수행한다. 이 구조는 실패 시점과 durable process record 생성 여부를 진입점별로 다르게 만들 수 있다.

### Profile retirement

Profile identity는 values/tasks만으로 끝나지 않고 port instance와 managed process history에서도 발견된다. 기존 cleanup archive는 개별 artifact 단위이고, backup의 atomic replacement는 values/tasks 파일에 맞춰져 있다.

따라서 profile archive/remove를 단순 파일 삭제로 추가하면 안 된다. active process 차단, runtime history의 처리, port assignment 복구 충돌, raw log 보안, multi-domain rollback을 먼저 계약으로 정해야 한다.

### Project operations

조사 당시 `project up/status/down`만 존재해 로그와 restart는 execution ID 기반 `process logs/restart`로 내려가야 했다. 같은 project instance에서는 active command가 singleton이므로 command 이름으로 현재 execution을 선택하는 방향을 채택했다.

### Browser/diagnostics/retention

- Dashboard Docker workflow는 HTTP management API를 검증하지만 실제 browser engine은 사용하지 않는다.
- `doctor`는 project prerequisite 중심이며 전체 devtools subsystem 상태를 한 번에 요약하지 않는다.
- log expiry 7일은 `services`와 `cleanup`에 중복되어 있고, 30일 보존 기준도 cleanup 내부에 고정되어 있다. 정책 drift를 막을 중앙화 가치가 있다.

## Work Items

### Phase 1 — release와 기본 gate

- [x] **WI-001 — Dashboard tests를 `just check`에 편입**
  - `check-dashboard` recipe 추가.
  - 기존 11개 test를 직접 Node test runner로 실행.
  - `check` 의존성에 연결.
  - 개발 문서의 prerequisites와 check 설명 갱신.
  - Node 부재가 모호한 실패가 되지 않도록 CI에서 명시적으로 setup.

- [x] **WI-002 — Release workflow CI/release 역할 분리**
  - `validate-tag` 유지.
  - `ci-macos`, `ci-linux`를 validate 이후 병렬 실행.
  - macOS: `just check`, build, `project inspect`, Agent Skill reference package 생성.
  - Linux: `just check`, `just verify-docker`.
  - `release`는 두 CI job 성공을 `needs`로 요구.
  - `release`에서는 재검증을 제거하고 네 플랫폼 package, cross-OS Skill 비교, GitHub Release 게시만 수행.
  - stable release 성공 뒤 Homebrew reusable workflow 실행은 유지.

### Phase 2 — command execution architecture

- [x] **WI-003 — effective command model 추출**
  - `internal/execution`에 command/env/inject/requirements 해석 모델 추가.
  - pure resolution tests로 env override, common+command requirements, bind/serve command를 검증.
  - public JSON contract는 변경하지 않는다.

- [x] **WI-004 — 공통 preflight와 preparation 구현**
  - value state/env, port/binding dry-run, executable/PATH, structured requirement checks를 공통 서비스로 통합.
  - 검사와 실제 reservation 사이의 상태 변화는 실제 prepare 단계에서 다시 검증한다.
  - secret 원문은 check/report에 포함하지 않는다.
  - `doctor`와 foreground `command run`을 공통 모델로 이관한다.

- [x] **WI-005 — managed process/project/Dashboard 실행 경계 통합**
  - `process start`, Dashboard process start, `project up`이 같은 preflight를 사용.
  - 실패한 cold start는 durable execution record 생성 전에 종료.
  - 기존 singleton 재사용은 불필요한 cold-start 검사를 요구하지 않는다.
  - snapshot 이후 기존 process가 사라진 경우 실제 새 start 직전에 preflight를 다시 수행.
  - supervisor는 CLI App 재진입 대신 common execution runner를 사용.
  - process retry/request-id 및 project lifecycle receipt semantics는 유지.

### Phase 3 — project UX

- [x] **WI-006 — `project logs COMMAND`**
  - canonical current project instance에서 active command execution을 선택.
  - 기존 `services.Logs` 보안/expiry 계약 재사용.
  - capture disabled/expired/not-running 조건을 구조화 오류로 유지.
  - stopped historical execution은 execution ID 기반 `process logs`를 사용하도록 역할을 구분.

- [x] **WI-007 — retry-safe `project restart COMMAND...`**
  - lifecycle action에 restart 추가.
  - 최초 요청에서 대상 active execution ID를 receipt에 고정.
  - child restart request ID를 저장해 응답 유실 뒤 중복 restart를 방지.
  - 선택한 command가 active가 아니면 새로 시작하지 않고 명시적 condition으로 보고.
  - env/capture override가 없으면 기존 process restart와 동일하게 이전 선택을 유지하고 최신 project 설정/value를 적용.
  - multi-command partial failure/retry는 `project up/down`과 같은 item 단위 결과 모델을 사용.

### Phase 4 — lifecycle/운영 확장

- [x] **WI-008 — Profile retirement 계약 확정**
  - values/tasks/instances/process history/logs 전체 ownership을 문서화.
  - active process, attached project instance, recent history 처리 정책 결정.
  - archive payload와 restore 충돌 규칙, raw log 보안, multi-domain rollback 전략 검증.
  - 단순 파일 삭제나 불완전한 profile disappearance는 구현하지 않는다.
  - 계약이 안전하게 닫힌 뒤 별도 구현 work item으로 분리한다.

- [x] **WI-009 — Retention policy 중앙화**
  - 최소한 log expiry와 cleanup 후보 기준의 shared constants/model을 한곳으로 이동.
  - 기존 7일/30일/backup 최소 보존 수의 public behavior는 유지.
  - 사용자 설정 노출은 실제 요구가 생길 때 별도 변경으로 둔다.

- [x] **WI-010 — 통합 diagnostics**
  - read-only aggregate report로 storage/profile/instance/process/proxy/dashboard/backup 상태를 요약.
  - 기존 subsystem API를 재사용하고 비밀 값, raw logs, 인증 token은 제외.
  - `doctor` project prerequisite와 역할이 겹치지 않도록 명령명/출력 계약을 확정한 뒤 구현.

- [x] **WI-011 — 실제 browser smoke**
  - Chromium 1종의 짧은 smoke journey만 추가.
  - dashboard login, profile 선택, navigation reload, 대표 mutation 1개를 실제 DOM/event/sessionStorage 환경에서 검증.
  - Playwright 도입 시 exact dependency pin과 browser 설치 비용을 CI와 분리해 관리.
  - unit test와 Docker HTTP workflow를 대체하지 않고 보완한다.

### Phase 5 — 문서와 Agent Skill 정합성

- [x] **WI-012 — public docs/Skill 정리**
  - release 개발 문서, process/project 사용법, 새 오류·재시도 규칙을 실제 구현과 맞춤.
  - Agent Skill에는 agent가 project logs/restart와 공통 preflight 결과를 사용하는 기준만 반영.
  - 내부 구조 설명을 사용자 문서에 불필요하게 노출하지 않는다.

## Validation

### VAL-001 — baseline/check gate

```sh
node --test scripts/dashboard-navigation.test.mjs scripts/dashboard-refresh.test.mjs scripts/dashboard-task-management.test.mjs
go test ./internal/cli ./internal/lifecycle ./internal/services ./internal/profiles ./internal/cleanup ./internal/dashboard
```

2026-09-18 기준 두 검사 모두 통과했다.

### VAL-002 — Phase 1

- `just check`가 Dashboard JS tests까지 실행하는지 확인.
- `go test -race ./...`, `go vet ./...`.
- release workflow에서 `release.needs`가 macOS/Linux CI 둘 다 요구하고 `contents: write`가 release job에만 있는지 diff review.
- 실제 tag release 시 두 CI job 실패가 Publish release/Homebrew 실행을 막는지 GitHub Actions 결과로 최종 확인.

### VAL-003 — execution architecture

- `go test -race` 대상: `internal/execution`, `internal/doctor`, `internal/ports`, `internal/services`, `internal/lifecycle`, `internal/cli`, `internal/dashboard`.
- missing tool/var/sec/env, bad PATH, port conflict, bind reference, env override를 모든 public start 경로에서 동일하게 검증.
- failed cold start가 process record를 남기지 않는 회귀 테스트.
- existing singleton 재사용과 process/project retry receipt 회귀 테스트.
- Docker `doctor`, `processes`, `project_lifecycle`, `workflow`.

### VAL-004 — project UX

- command-name -> execution 선택의 worktree isolation.
- logs disabled/expired/raw UTF-8 처리 회귀.
- restart response loss/retry, partial failure, missing active command, env/capture inheritance.
- schema/help/completion과 docs contract 확인.

### VAL-005 — Phase 4

각 항목은 별도 package tests와 필요한 Docker/browser 검증을 정의한 뒤 구현한다. Profile retirement는 계약 검토가 끝나기 전 mutation 구현을 시작하지 않는다.

## 비범위

- `devtools.toml` version/schema metadata 추가.
- 실패한 release tag 자동 삭제/재사용.
- main CI 성공을 release script가 조회하는 exact-HEAD gate.
- cloud sync 또는 remote daemon.
- project command group/preset.
- doctor remedy 자동 실행.
- retention 값을 이번 작업에서 사용자 config로 공개하는 것.

## 완료 기준

- Release 게시 job은 별도 macOS/Linux CI가 모두 성공한 뒤에만 실행된다.
- `just check`가 Go/Skill/Dashboard 회귀를 함께 막는다.
- 사용자-facing command 실행 경로가 하나의 effective command/preflight 구현을 공유한다.
- supervisor가 CLI App에 재진입하지 않는다.
- project logs/restart가 canonical worktree identity와 retry-safe lifecycle 계약을 따른다.
- retention 중복 정책이 중앙화된다.
- diagnostics/browser/profile retirement 후속은 각자의 안전 계약과 검증 기준이 문서화되고, 구현된 항목은 public docs/Skill과 일치한다.

## Review Loop

### Review Contract

- 범위: 이 workstream이 현재 working tree에 만든 코드·CI·문서·테스트 변경과 이를 판정하는 직접 계약.
- 목표: Release publish gate 분리, Dashboard regression/browser gate, 공통 execution/preflight, project logs/restart, retention 중앙화, read-only diagnostics, profile retirement 설계 계약, public docs/Agent Skill 정합성.
- 안전 경계: secret·raw log·token·task context 비노출, cold-start record 생성 전 preflight, canonical worktree 격리, retry-safe mutation, publish 전 CI.
- 비범위: 본 계획의 비범위 항목, profile retirement mutation 구현, 추가 제품 기능·호환성 계층·새 운영 기능.
- 리뷰 방식: review-and-fix.
- 선택 모드: server, node, web, planning.
- 선택 축: API/상태·idempotency·sensitive data, runtime/process/env/fs, browser interaction/API drift, plan/contract/validation, correctness에 직접 영향을 주는 helper boundary.
- 종료 조건: 선택 축에 actionable finding이 없고, 수정이 있으면 affected-axis 재검토와 scoped validation을 다시 통과하거나 환경 제약이 정확히 기록됨.

### Cycle Ledger

- Cycle 1: 완료. Server/API, Node/CI, Web/browser, Planning/contracts, correctness helper 축을 검토했다.
  - 수정: restart stop mutation 결과 보존, restart inherited env preflight, 모든 actual cold start의 즉시 preflight, callback 시점 current project config/identity 재확인, browser CLI timeout, bootstrap credential 오류 로그 차단, public docs drift 정리.
  - unresolved finding: 없음.
  - cycle validation: execution/doctor/process/services/lifecycle/cli/dashboard/diagnostics/retention/cleanup/tasks race PASS; 전체 go vet PASS; Dashboard JS 11/11 PASS; browser spec syntax/discovery PASS; actionlint, frozen pnpm lockfile, diff-check PASS.
  - 환경 제약: 실제 Chromium launch, Docker, uvx Skill validator는 기존 로컬 제약으로 Release CI 확인이 필요.
- Cycle 2: 완료. Cycle 1 수정 파일과 직접 계약을 affected-axis re-review했다.
  - 추가 수정: browser smoke 실패 시 cleanup 보장 및 primary error 보존, diagnostics profile/process active count를 동일 live snapshot으로 정합화.
  - 재검토 결과: 선택한 모든 축에서 actionable finding 없음. 동일 root cause의 oscillation 없음.
  - scoped validation: Cycle 1과 동일한 race/vet/JS/Playwright discovery/actionlint/frozen-lock/diff-check가 최신 변경 기준으로 다시 PASS.
  - 남은 검증 제약: 실제 Chromium launch는 Playwright CDN 403, Docker는 daemon/socket 부재, 공식 Agent Skill validator는 uvx 부재. 모두 Release CI에서 이미 필수 gate로 구성됨.
- Cycle 3: 완료. Preflight/readiness affected-axis를 다시 검토했다.
  - 추가 수정: lifecycle completion이 최초 project snapshot 대신 실제 started process의 `ready_configured`를 사용하도록 변경해 operation 중 readiness 설정 변경을 반영. 모든 actual cold start에 immediate preflight를 유지하면서 의미 없는 receipt boolean을 제거.
  - 재검토 결과: stale project config, inherited env, restart stop-result, cold-start preflight, actual readiness 경계에서 추가 actionable finding 없음. oscillation 없음.
  - scoped validation: execution/doctor/process/services/lifecycle/cli/dashboard/diagnostics/retention/cleanup/tasks race PASS; 전체 go vet PASS; Dashboard JS 11/11 PASS; browser syntax/discovery PASS; actionlint, frozen pnpm lockfile, diff-check, Agent Skill package 생성 PASS.
  - 남은 검증 제약: 실제 Chromium launch는 Playwright CDN 403, Docker는 daemon/socket 부재, 공식 Agent Skill validator는 uvx 부재. Release CI에서 모두 검증하도록 구성됨.
