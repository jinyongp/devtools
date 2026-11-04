# 공통 실행 수명과 제어 계약 작업 계획

기준: v0.41.0 / protocol 23 / `9dbe3cf`. 2026-11-04 Linux/WSL에서
Nuxt, Node watch, Uvicorn 및 부모·자식 종료 경계 31개를 조사했다.
foreground의 조회·제어 누락, Unix 부모 조기 종료 뒤 자식 잔류,
부모 종료에 따른 graceful cleanup 조기 중단을 공통 원인으로 확인했다.

## 완료 조건과 경계

- foreground/managed 실행은 기존 process 저장소·인증 채널·요청 영수증을 공유한다.
  실행 하나는 ID 하나로 조회하고, worker PID 교체는 실행 ID를 바꾸지 않는다.
- 실행 상태, 제어 가능 여부, readiness, 소유 범위와 정리 상태를 구분한다.
  제어기 손실을 정리 완료로 추정하지 않는다. 저장된 PID를 직접 종료하지 않는다.
- Unix 프로세스 그룹 / Windows Job 안의 자식은 부모 조기 종료에도 정리한다.
  종료 유예는 살아 있는 소유 프로세스 전체에 적용한다.
- Unix 새 세션, 외부 daemon/원격 서비스는 소유 범위 밖이다. 이 경계를 명시한다.
- foreground의 stdin/stdout/stderr, TTY job control, 자식 종료 코드를 보존한다.
  추적 저장을 강제해서 기존 읽기 전용 일회성 실행을 막지 않는다.
- schema, CLI, 대시보드, 복구 안내, bundled Skill이 같은 실행 계약을 사용한다.
  실행 가능한 복구 argv는 실제 ID를 사용하며 기존 parser로 검증한다.
- 별도 worktree 없이 main에서 작업한다. 작업 단위로 커밋하고 마지막에
  통합 검증·리뷰루프를 수행한다. 기존 GitHub Actions 검증 경로를 재사용한다.

## 작업 단위

1. **Unix 소유 범위 종료 공통화** — foreground/managed가 같은 OS runner를
   사용하고 부모 조기 종료, 상속 pipe, 자식 cleanup 유예를 검증한다.
2. **공통 실행 등록·제어** — 기존 실행 기록과 인증 RPC를 foreground에 연결하고
   목록·status·stop·readiness를 통합한다. 서비스 등록 실패와 읽기 전용
   일회성 실행 정책, 제어기 손실 및 재시도 경계를 검증한다.
3. **공개 계약·소비자 통합** — 구체적인 공통 응답 schema, capability/cleanup,
   복구 안내, 텍스트·대시보드와 문서를 정렬한다. 기존 protocol 정책을 따른다.
4. **통합 검증·리뷰루프** — 실행/저장소/제어/조회/안내/소비자/호환성/플랫폼을
   모두 검토하고 확인된 결함을 수정한다. 최종 소스에 맞는 검증 결과를 기록한다.

## 검증과 리뷰 기록

각 작업 단위에서 관련 기존 검사와 의미 있는 회귀 사례를 실행한 뒤 결과를 추가한다.
실제 실행 검증은 격리 저장소와 임시 프로젝트를 사용한다. 사용자의 서버는 종료하지 않는다.
Linux 로컬 결과와 macOS/Windows native CI 결과는 구분해서 기록한다.

### 1. Unix 소유 범위 종료 공통화

- foreground와 Unix supervisor가 `process.ExecuteOwned`를 공유한다.
  직접 자식은 그룹 정리가 끝날 때까지 reap하지 않아 그룹 식별자를 보존한다.
- 정상 부모 종료, 취소, 상속 pipe를 같은 그룹 수명 경계로 처리하고,
  살아 있는 자식 전체에 3초의 종료 유예를 적용한다.
- Linux: `go test -race -count=1 ./internal/process ./internal/services` 통과.
  부모 정상 조기 종료와 부모 TERM 종료 모두 자식의 cleanup marker 및 출력 보존을 검증했다.
- macOS arm64: process 테스트 바이너리 교차 컴파일 통과. native 실행은 아직 미검증.

### 2. 공통 실행 등록과 제어

- 이름 명령의 foreground 실행을 기존 process 기록·lease·인증 RPC에 등록한다.
  서비스 claim은 같은 실행 ID를 가리키며 `process list/status/check/stop`으로 관리한다.
  기존 foreground의 native stream과 종료 코드는 유지한다.
- Unix/Windows supervisor와 foreground가 한 control server, 시작 기록,
  readiness 직렬화 및 종료 기록 구현을 공유한다. probe 종료와 서비스 claim 해제는
  terminal 기록을 공개하기 전에 마친다.
- 포트 없는 이름 명령은 등록 단계의 읽기 전용/접근 거부에 한해 추적 없이 실행한다.
  이미 실행한 명령의 오류를 재실행하지 않으며, 서비스 명령은 등록 없이 시작하지 않는다.
- Linux race: 실제 foreground의 목록 → status → readiness → stop → 동일 UUID
  replay, native 출력/종료 코드 및 읽기 전용 일회성 실행 검사 통과.
- Windows amd64: services 테스트 바이너리 교차 컴파일 통과. native 실행은 아직 미검증.
- services 전체 race 검사 통과. CLI 전체 검사는 통합 검증에서 최종 소스로 확인한다.

### 3. 공개 계약과 소비자 통합

- Protocol 24: 공통 실행 schema를 process/project/요청 결과에서 재사용한다.
  `mode`, `control`, `cleanup`, `stopping`, mode 필터를 CLI와 dashboard에 연결했다.
  기존 설정 schema ID와 설치본·Windows 검증의 protocol gate도 함께 갱신했다.
- control의 현재 관측과 저장된 요청의 과거 관측을 구분한다. 저장된 응답은 정확히
  replay하고 현재 권한은 인증 RPC를 다시 관측해 판정한다. 제어기 손실과 stop 응답 유실은
  unknown effects/cleanup을 유지한다. 기존 terminal 기록은 소급 검증하지 않는다.
- 이름 명령의 외부 Runner 경계를 보존하고, 비주입 명령은 실제 값 저장소가 사용할 수
  없어도 실행되도록 검증했다. 초기 strict-port 충돌도 서비스·포트·대상을 보존한다.
- Windows는 부모 조기 종료에도 자식 cleanup 유예를 보장하고 강제 종료 뒤 Job accounting이
  비었는지 확인한다. Unix는 그룹 식별자를 유지하면서 조용한 관측과 최종 그룹 신호 뒤
  소유 범위가 비었는지 다시 확인한다. 외부 세션/daemon을 포함한다고 주장하지 않는다.
- Linux 설치본: processes, project_lifecycle, readiness, tty, cli_guidance,
  discovery, sandbox_queries, cli_presentation 8개 통과. 실제 foreground 충돌의 ready argv를
  그대로 실행해 종료·포트 재사용을 검증했다. 저장 결과 동일성 및 TTY job control도 통과했다.
- 기존 `check:static` 통과. sandbox의 기본 캐시 쓰기 제한은 임시 uv 캐시/도구 경로로
  분리하고, installer 검사는 임시 XDG 경로에서 기존 host 계약으로 실행했다.
- 전체 Go race 검사에서 확인한 schema 2건, legacy stop fixture 호환성, 이력 text 표의
  열 수 초과를 수정했다. 관련 전체 package와 각 실패 사례의 재검증을 통과했다.
  native Windows/macOS의 최종 소스 검증은 기존 workflow에서 진행한다.

## 통합 리뷰 범위

| 영역 | 상태 | 확인 내용 |
| --- | --- | --- |
| Unix 실행·TTY·그룹 정리 | 검토 및 로컬 검증 완료 | 부모 조기 종료, cleanup 유예, 상속 pipe, job control, PID 식별자 유지 |
| 실행 등록·값·저장소 | 검토 및 로컬 검증 완료 | 기존 기록/lease 재사용, 값 비주입, 읽기 전용 fallback, 호출자 Runner |
| 제어·요청 재시도 | 검토 및 로컬 검증 완료 | 인증, 실행 ID 고정, 응답 유실, 결과 replay, 제어기 손실 |
| 목록·필터·text·schema | 검토 및 로컬 검증 완료 | mode 선선택, bounded history, 실행 schema 재사용, 이력 표 재검증 |
| dashboard·Skill·문서 | 검토 및 로컬 검증 완료 | capability 기반 작업, cleanup 표시, protocol 24 발견과 경계 |
| Windows Job·macOS backend | Windows runtime 통과 / macOS native 실패 조사 중 | 기존 Unix/Windows workflow로 같은 최종 소스 검증 |

현재: 작업 1–3 완료. 작업 4의 최종 리뷰 및 native CI 결과 확인이 남아 있다.

### 통합 리뷰 수정 1 — foreground 선택과 현재 목록

- foreground 등록에 명시적인 env/bundle override와 proxy snapshot 정보를 보존했다.
  실제 foreground → managed restart에서 공통 env/none bundle 선택을 유지하고,
  이전 실행 stop이 교체 실행에 영향을 주지 않는지 race 검사로 확인했다.
- 정상 종료한 비서비스 foreground 명령은 current 서비스로 남기지 않고 history에 보존한다.
  mode 필터는 선택한 실행에 연결된 요청만 포함해 다른 모드의 실패를 섞지 않는다.
- 관련 foreground·선택·목록·이력 회귀 검사 통과. Native CI는 최종 소스에서 확인한다.

### 통합 리뷰 수정 2 — 시작 관측 실패의 소유 범위 정리

- native spawn 뒤 시작 관측 콜백이 실패해도 Unix 그룹 / Windows Job이 비었는지
  확인한 뒤 원래 오류를 반환한다. 정리를 확인하지 못하면 cleanup 불명과 원래 오류 코드를
  보존한다. 시작을 관측하지 못한 실행에 가상의 앱 종료 코드 0을 기록하지 않는다.
- 실제 자식 계층을 시작한 뒤 관측 실패를 주입하는 Unix race 검사 통과.
  같은 native Windows Job 회귀 사례를 기존 suite-runtime 검증에 추가했다.

### 통합 리뷰 수정 3 — 명시적 표시 열과 실제 프레임워크

- 공통 목록 renderer가 많은 표시 열을 요청한 경우에도 선택한 행·열을 그대로
  표시하도록 보완했다. 표의 열 수 한계를 넘으면 행별 필드로 표시하며, 빈 선택도
  `No items.`로 처리한다. 전체 JSON 형태의 fallback으로 되돌리지 않는다.
- 실제 CLI에서 9개 열 선택, 선택하지 않은 필드 비노출, 빈 결과를 검증했다.
- 수정한 Linux 바이너리로 실제 프레임워크 7개 사례를 재실행했다: Node watch
  foreground/managed reload, Uvicorn foreground reload/managed workers 2개,
  Nuxt 4.6.4 기본 foreground/managed 및 no-fork foreground.
  foreground도 process API로 종료하고, reload 후 같은 실행 ID와 active count 1을 확인했다.
  7개 모두 소유 프로세스·포트가 정리됐고 별도 연구용 강제 정리는 필요 없었다.

### 통합 리뷰 수정 4 — stop 응답 유실과 정리 판정

- stop 응답이 유실되고 terminal 기록에 cleanup 불명이 공개된 경로가 성공을 반환하는
  결함을 회귀 검사로 재현했다. 일반 완료·응답 유실·재시도에 같은 종료 판정을 적용했다.
  소유 범위의 정리가 불명인 실행에는 restart capability도 제공하지 않는다.
- complete/unknown 기록을 공개한 뒤 응답을 비워 실제 RPC decode 실패를 일으키고,
  같은 요청을 두 번 실행해 검증한 성공과 효과 불명 실패가 각각 유지되는지 확인했다.
  제어기 손실·대기 중 stop·과거 기록 호환성을 포함한 관련 race 검사 통과.
- `e4bf03c`의 전체 Go race 50개 package, 기존 정적 검사, 설치본 9개 시나리오
  (기존 8개 + proxy_auto)가 통과했다. 추가 stop 수정 소스의 실제 Chromium
  dashboard 4개 시나리오도 통과했다. 최종 플랫폼 검증은 기존 native CI에서 확인한다.

### Native 검증 — macOS 관측 경계 재확인

- `d20fc5f`의 Linux 전체 Go race 50개 package, 정적 검사, 설치본 9개 시나리오 통과.
  Windows amd64/arm64 suite-runtime도 기존 native workflow에서 통과했다.
- [Unix native run](https://github.com/jinyongp/devtools/actions/runs/41779003648)에서
  macOS의 process 관측 오류가 doctor/execution/설치본 검사에도 전파됨을 확인했다.
  실패를 무시하거나 정리 완료로 바꾸지 않고 native API 관측을 재확인한다.
- 살아 있는 그룹, 정지한 자식, reap 전 종료 자식과 그룹을 직접 검사하는 macOS
  회귀 사례를 추가했다. 교차 컴파일 통과; native 실패 원인 확인과 수정은 진행 중이다.

### 통합 리뷰 수정 5 — macOS의 좀비 그룹 신호 결과

- native 직접 관측 사례는 통과해 sysctl 조회 자체의 문제를 배제했다. 실제 종료 경로와
  [Apple XNU의 그룹 신호 구현](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_sig.c)을
  대조했다. 좀비만 남은 그룹은 신호 대상 필터에서 제외되며 POSIX 호출이 EPERM을 반환한다.
- 공통 그룹 신호 helper는 EPERM 뒤 살아 있는 구성원이 없는지 다시 관측한 경우에만
  오류를 해소한다. 살아 있거나 관측이 실패한 그룹의 접근 거부는 여전히 정리 불명이다.
  초기 TERM, 최종 KILL 및 강제 종료가 같은 판정을 사용한다.
- macOS 회귀 사례에 reap 전 종료한 자식만 남은 그룹의 TERM/KILL을 추가했다.
  소유 범위를 재관측하는 검증을 유지하며 OS 오류를 일괄 무시하지 않는다.
- Linux process/services/execution/doctor 전체 race 검사 및 macOS arm64 process
  교차 컴파일 통과. 수정 커밋의 native 재검증은 기존 workflow에서 확인한다.
