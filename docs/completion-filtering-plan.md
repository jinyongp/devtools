# 상황별 셸 자동완성

기준: main `b5321ae`. 현재 worktree에서 구현·검증하고 작업 단위로 커밋한다.
새 worktree, 배포, 사용자 셸 설정 변경은 범위에 포함하지 않는다.

## 확인한 문제와 계약

- process의 status/stop/restart/logs/check/wait가 같은 저장 ID 목록을 사용한다.
  종료됐거나 제어할 수 없는 실행도 stop 후보에 나온다.
- 셸 adapter가 동적 후보 뒤에 명령·옵션을 별도로 추가한다. 공통 엔진에서
  후보를 제거해도 adapter가 잘못된 옵션을 다시 제안할 수 있다.
- 엔진은 명령 위치와 선택한 profile/env는 구분하지만 반복 옵션의 선택값,
  옵션 간 제약, 작업 상태, 현재 프로젝트의 실행 상태를 일관되게 반영하지 않는다.

필터는 현재 입력에서 확실히 부적합한 후보를 제거한다. 아직 입력하지 않은 필수
옵션이나 인증 정보가 있다는 이유로 나중에 완성할 수 있는 입력을 막지 않는다.
후보는 실행 권한이나 성공 보장이 아니며 실제 작업이 최종 계약을 다시 검사한다.

## 작업 단위

1. **공통 입력과 셸 adapter**
   - 명령/별칭, root 옵션, 인자 수, child 경계, 이미 지정한 단일 옵션과 반복 선택값을
     한 엔진에서 처리한다. 상호 배타 조건은 기존 정적 입력 계약을 재사용한다.
   - bash/zsh/fish/PowerShell은 같은 필터 결과를 표시한다. quoting, 설명,
     호출한 바이너리 선택, PowerShell literal AST 검사를 유지한다.
   - 기존 이름 전용 비공개 프로토콜은 유지하고 새 adapter용 설명 출력을 추가한다.
     adapter가 제거된 후보를 독자적으로 되살리지 않게 한다.
2. **대상별 공급자**
   - process: stop/restart/check/wait는 공통 control capability, logs는 캡처와 보존 기간,
     status는 조회 가능한 이력을 기준으로 선택한다. foreground/managed에 같은 규칙을 쓴다.
   - project: up은 선언된 명령, status/down/restart/logs는 같은 프로젝트 위치의
     현재 실행과 해당 작업 가능 여부를 기준으로 선택한다.
   - task/workstream/run: claim 가능 여부, 현재 run, lifecycle 전이, 의존 관계와
     선택된 workstream을 반영한다. 읽기 명령의 이력 조회 범위는 유지한다.
   - 값/env/bundle: 선택한 profile/env/kind, 삭제할 실제 env 슬롯, 반복 member 선택을 반영한다.
   - proxy/exposure/port/instance: 선언·저장 상태와 선택한 위치/범위를 대조한다.
     새 이름 입력과 기존 대상 선택을 구분한다.
3. **문서·통합 검증·리뷰**
   - 문서와 번들 Skill의 범위·예시·시간 제한을 실제 계약에 맞춘다.
   - 기존 completion·입력 계약·도메인 검사와 설치형 시나리오를 확장한다.
     모든 지원 셸에서 동일한 제외 조건과 quoting/별칭/child 경계를 검사한다.
   - 변경 범위 전체를 리뷰하고 수정 영향이 있는 호출자를 다시 검증한다.

## 보존할 경계

- Tab은 설정·값·task·데이터·캐시를 쓰거나 vault를 열고 세션을 갱신하지 않는다.
- readiness 실행, 프로그램 시작/종료, 외부 provider 호출, 마이그레이션을 하지 않는다.
- 실행 제어 가능 여부가 필요한 후보에만 기존 인증된 loopback status 관측을
  읽기 전용으로 사용한다. 저장된 capability를 현재 제어 권한으로 간주하지 않는다.
- 전체 200ms 예산, 접두사 필터, 정렬·중복 제거와 최대 100개 제한을 유지한다.
  제어기 지연·관측 실패는 해당 후보를 생략하며 다른 후보를 막지 않는다.
- 비밀 값·본문·인증 토큰은 출력하지 않는다. 설명은 공개 이름·상태만 사용한다.
- 기존 셸 파일을 자동 수정하지 않는다. 새 adapter를 쓰려면 재생성/재로딩이 필요하다.

## 리뷰 범위 추적

| 영역 | 상태 | 확인할 증거 |
| --- | --- | --- |
| 공통 입력·명령·옵션·부분 입력 계약 | 검토·로컬 재검증 완료 | 중복/상호 배타/명시적 false/필수 입력 보완/child 경계/JSON columns |
| process·project | 검토·로컬 재검증 완료 | 실제 foreground·managed와 종료 실행, 상태 RPC 전용, 준비 검사 미실행, 위치 격리 |
| task·workstream·run | 검토·로컬 재검증 완료 | claim/전이/의존/선택 중복/조회 이력/종료 workstream 정의 추가 |
| 값·env·bundle | 검토·로컬 재검증 완료 | kind/env 소속·상속·삭제·반복 선택 |
| proxy·exposure·port·instance | 검토·로컬 재검증 완료 | 실제 listener/exposure 제어 상태, 선언/할당 구분, instance 이동의 원본 범위 |
| bash·zsh·fish | 검토·로컬 재검증 완료 | 실제 셸의 동일 필터·설명·특수문자·호출 바이너리·별칭·child 경계 |
| PowerShell 5.1/7 | 검토·native 재검증 완료 | Windows x64/ARM64 full workflow의 실제 AST/UTF-8/cursor/필터 검사 |
| 문서·번들 Skill·통합 검증 | 검토·통합 재검증 완료 | 정적 검사, 설치형 시나리오, 기존 Unix/Windows workflows 통과 |

## 공급자 전수 대조

21개 기존 공급자의 모든 선언 위치와 소비 명령을 대조했다.

| 공급자 | 선택 기준과 보존한 범위 |
| --- | --- |
| command | 실행/설정/up은 선언, 나머지 project 동작은 같은 위치의 현재 실행 |
| tool, preset | 기존 선언을 수정·조회·삭제할 때만 제안 |
| profile | 값/task뿐 아니라 port/process에만 남은 profile도 공통 catalog에서 조회 |
| env, variable, secret | 선택한 profile/env/kind; unset은 실제 env 슬롯, get은 상속 포함 |
| bundle, selection, value-key | profile의 bundle·공유 key 이름; selection의 all/none 유지; 반복값 제외 |
| port-declaration, port | allocate의 선언과 show/check/release의 실제 할당 구분 |
| instance | 선택한 profile의 alias/ID; move의 목적지 dir를 원본으로 사용하지 않음 |
| process | 조회 이력, 현재 control, 준비 검사 선언, 캡처 보존 기간을 작업별로 구분 |
| route, listener, exposure | global/project 범위, 명시적 비활성화, 현재 로컬 제어 상태 구분 |
| task, workstream, validation, run | 공개 도메인의 전이·claim·의존·소유 범위; 읽기 명령은 이력 유지 |

새 이름을 만드는 위치에는 기존 항목을 넣지 않는다. 값 전달의 execution/runner,
cleanup plan/archive, private context와 expected-run은 안전한 기존 공급자가 없어
조회 결과를 직접 사용하는 경계를 유지한다. 자동완성 때문에 새 인증·복구 정책을 만들지 않는다.

## 리뷰에서 수정한 사항

- 상속한 common key를 env unset 후보로 제안하던 문제를 실제 슬롯 필터로 수정했다.
- 종료·제어기 손실 실행의 stop/check/wait 후보, 캡처 없는 logs 후보를 제거했다.
  project의 실행 제어는 profile뿐 아니라 정확한 프로젝트 위치를 확인한다.
- shell별 정적 fallback을 제거하고, 기존 입력 schema의 부분 평가로 상호 배타·중복을 처리했다.
  inline 옵션과 root format 중복, JSON의 columns 값 위치도 다시 확인했다.
- profile 수집을 공통 catalog로 합쳐 port/process 전용 profile의 누락을 막았다.
- v3의 완료된 workstream에도 정의를 추가할 수 있다는 도메인 계약을 확인하고
  task add 후보의 과도한 lifecycle 필터를 제거했다.
- 선언 이름의 특수문자 테스트는 유지하고, 비활성 project 동작과 adapter 정적 enum을
  전제로 한 기존 테스트를 새 공통 계약에 맞췄다.

## 로컬 검증

- 전체 Go race 검사에서 CLI 외 50개 패키지 통과. CLI에서 드러난 두 기존 기대값을 수정한 뒤
  CLI 전체 일반 검사와 해당 테스트의 race 재검증 통과.
- 실제 listener/exposure 시작·종료를 재사용한 추가 race 검사 통과.
- `devtools run check:static` 통과: Skill, dashboard, installer, release, project,
  perf, gofmt, go vet. bash/zsh와 셸 준비 script 문법 검사 통과.
- 설치형 `completion_context`, `discovery`, `cli_presentation`, `processes`,
  `project_lifecycle`, `cli_reliability` 전부 통과. 실제 fish 포함.
- 원격 전체 검증 결과는 아래에 기록한다.

공통 엔진과 도메인 연결은 함께 컴파일되어야 하므로 구현·회귀 검사·사용 문서를 첫 커밋으로,
native CI와 설치형 통합 검사를 다음 커밋으로 묶는다. 배포는 수행하지 않는다.

## 통합 검증과 최종 리뷰

검증 대상: `3c214b608797baa52819703e52e7734b74e76b5c`.
구현 커밋은 `506260e`, 설치형·CI 검증 확장은 `3c214b6`이다.

- [Unix validation](https://github.com/jinyongp/devtools/actions/runs/41794506521): 성공.
  필수·기본 작업 24개 통과. macOS/Linux 전체 Go race·정적 검사,
  macOS Intel/ARM64와 Linux x64/ARM64 설치형 시나리오,
  실제 bash/zsh/fish, 기존 proxy·브라우저 호환 검사 포함.
- [Windows platform boundaries](https://github.com/jinyongp/devtools/actions/runs/41794471813): 성공.
  23개 작업 통과. x64/ARM64 전체 제품·설치·업데이트·입력 계약과
  양쪽 플랫폼의 실제 PowerShell 5.1/7 completion 검사 포함.
- 명시적 opt-in인 실 Cloudflared ingress, 두 장치 Tailscale Serve와
  과거 자동화 성능 측정 3개는 설정대로 실행하지 않았다.

전체 변경과 영향받은 호출자·문서·검사를 재검토했다. 위 범위에서 남은 수정 대상은 없다.
이 결과를 기록하는 후속 커밋은 문서만 변경하며 검증한 구현·테스트·workflow는 그대로다.
사용자 셸 설정과 설치된 버전은 변경하지 않았다. 새 버전 적용 뒤 이전 자동완성 파일을
재생성/재로딩해야 셸별 기존 정적 fallback까지 교체된다.
