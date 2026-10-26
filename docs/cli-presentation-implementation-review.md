# CLI presentation 구현·리뷰 기록

2026-10-26. 계약: [구현 계획](cli-presentation-plan.md),
[계약 결정 D01–D13](cli-presentation-contract-review.md).
workstream: `bd696a33-81fa-4dab-9f61-ca5e76300d49` / profile `devtools`.
검토 범위: baseline `ff8edcca1ba5bae4fdd7981779bc4f0c5e557438`부터
제품·검증 소스 `eed45cacdbe1fa4edcabece27b10c5b2c2686659`까지.

## 구현 결과

- T01 `c278879`: root format, host 관측, 순수 presentation 정책, 독립 `cliui`, 공통 출력 경계.
- T02 `d143ee0`: 조회 목록·상세·값·로그 view, 원문과 unknown field 보존.
- T03 `1d5a86e`: 변경·preview·replay·진단·task view와 원 오류/복구 행동 보존.
- T04 `0909024`: protocol 15 discovery, completion, old/new JSON bootstrap, 내부 소비자·문서·bundled skill 이행.
- T05 `3c08d94`: 기본 auto 전환, 실제 설치 PTY 검사, 기존 Unix/Windows native CI 연결.
- 리뷰 수정 `4029a63`: 연속 필드 출력, 플랫폼별 argv 검사, 설치·업데이트 fixture 격리.
- native CI 수정 `eed45ca`: macOS Intel 부분 readiness fixture의 실행 예산과 실패 근거 보완.

일반 터미널은 text, 알려진 agent 신호·non-TTY stdout은 JSON이다.
root `--format` > `DEVTOOLS_FORMAT` > auto이며 TOML 필드는 추가하지 않았다.
schema는 JSON, help/completion/child/private stdout은 기존 소유권을 유지한다.
표현은 같은 한 번의 공개 결과에서 만들며 저장된 요청·fingerprint·재실행에 관여하지 않는다.
완성된 Next 행동은 설명 아래 독립된 한 줄 전체를 복사해 실행할 수 있다.

## 전 범위 리뷰

| 영역 | 소스 검토·재검토 | 근거와 남은 한계 |
| --- | --- | --- |
| root 입력·선택 | reviewed | flag/env/default, 잘못된 값·중복, alias/EOF offset, isolation, child 경계, 취소 우선순위 검사 |
| 공개 데이터 경계 | reviewed | public envelope 1회, UseNumber, omitempty/json:- 경계, unknown field·status와 shape fallback 검사 |
| 목적별 view | reviewed | catalog metadata 전수, provider required path 대조, 목록/상세/값/log/변경/진단/task fixture와 설치 실행 |
| 오류·경고·후속 행동 | reviewed | 원 message/code/exit, checks/causes/effects/retry/scope, 필수 입력, 한 줄 shell argv 보존; mutation 재실행 없음 |
| CLI UI·스타일 | reviewed | immutable Document, control escape, 폭 40/79/80/120, CJK/complex fallback, NO_COLOR/TERM, 단일 연속 출력; 실제 다중 플랫폼 시각 수용은 pending |
| 출력 실패·native 소유권 | reviewed | JSON/native UI bypass, 최대 두 render, 원 오류 exit·긴급 전체 진단, short write/stderr 재시도 금지, post-handler effectsunknown; Windows 기존 VT는 읽기만 수행 |
| discovery·소비자 | reviewed | strict catalog/global_options, 네 shell 생성, old/new bootstrap, 설치·검증·perf 호출, README/출력 문서/bundled skill; Windows native 실행도 Actions에서 통과 |
| 검증·작업 기록 | reviewed | 작업 단위 커밋, 전체 Linux 검사와 38 installed scenario 및 세 OS native Actions 성공. 실제 화면 미검증은 별도 기록; workstream close하지 않음 |

확인한 결함을 수정한 뒤 영향 caller와 기존 검사를 다시 확인했다.
리뷰한 소스에서 남은 확정 결함은 발견하지 못했다. 아래 미실행 수용 조건 때문에
전체 플랫폼 수용이나 리뷰루프의 최종 완료로 판정하지 않는다.

## 리뷰에서 수정한 내용

1. 필드마다 누적 출력 전체를 복사하던 경로를 제거했다. 많은 레코드에서 누적 복사 비용이
   커지는 원인을 없애고 기존 제목·다중 줄 값·prose continuation을 확인했다.
2. 설치와 self-update fixture가 개발자 PATH의 기존 `dvt`에 의존했다. 실제 전체 검사에서
   alias 생성·실행 case가 실패한 것을 재현하고, 최초 설치의 PATH를 fixture 안에서 분리했다.
   기존 alias 보존·충돌 검사는 그대로 실행한다. 사용자 alias는 변경하지 않았다.
3. Unix shell 인자 전달 검사를 Windows에서 실행하지 않게 하고, 실제 Windows에서만
   PowerShell 5.1/7의 native argv를 확인하는 검사를 추가했다. legacy에서 표현할 수 없는
   빈 인자·내장 double quote 등은 계속 not-copyable fallback이다. Linux에서는 native 검사를 skip한다.
4. T05 상수 변경 뒤 gofmt 검사 실패를 수정했다. 최종 전체 검사는 포맷 수정 후 실행했다.

## 실행 근거

환경: Linux amd64, WSL2 kernel `6.18.33.2-microsoft-standard-WSL2`.
검증용 설치는 runner 소유의 임시 HOME·저장소만 사용했다.
Fish 3.7과 필요한 패키지는 `/tmp/devtools-presentation-fish`에 풀어 검사했으며 전역 설치하지 않았다.

| 검사 | 결과·검증 범위 |
| --- | --- |
| `devtools run check:all` | PASS. 프로젝트 검증·정적 검사·Go race suite 포함. 로그 `/tmp/devtools-presentation-final-check-all.log`; 최종 커밋 재확인 로그 `/tmp/devtools-presentation-4029a63-check-all.log` |
| `devtools run test:installed` | PASS, 38/38 scenario. 임시 Fish를 PATH에 공급. 로그 `/tmp/devtools-presentation-final-installed.log` |
| `devtools run check:skill` | PASS, bundled skill 유효성 |
| 실제 shell argv round-trip | PASS sh/bash/zsh/Fish: 공백·Unicode·quote·backslash·빈 인자·치환/구분 문자 보존 |
| installed completion/discovery | PASS bash/zsh/Fish, Bash 실제 Readline 포함. 후속 Actions에서 Windows native PowerShell completion도 PASS |
| installed `cli_presentation` | PASS 실제 clean/agent PTY, pipe/stderr 분리, 옵션 우선순위, native stdout, 폭·NO_COLOR·dumb, doctor Next 줄 전체 복사 후 실행 |
| 실제 protocol 14/15 bootstrap | PASS 부모 환경 text/invalid 모두에서 자식 JSON 유지. 14에는 새 flag 없음, 15는 discovery 뒤 root JSON 선택, 부모 환경 불변 |
| Windows amd64 cross compile | PASS CLI·CLI UI 테스트 실행 파일 생성. native 실행 pass를 의미하지 않음 |
| production import graph | `cliui`는 protocol/JSON/OS/store를 import하지 않음. presentation은 공개 JSON과 cliui만 소비하고 protocol은 UI를 import하지 않음 |
| Git hygiene | 작업 단위별 status/stat 및 staged/unstaged diff check. baseline 전체 diff whitespace 오류 없음 |

로그는 이 세션의 임시 파일이며 배포된 증거 저장소가 아니다. 제품 소스 이후의 이 문서는 실행 동작을 바꾸지 않는다.

## 기존 Actions의 native 검증

사용자가 기존 Actions 검증과 main-only 진행을 승인했다. `3449f60`을 origin/main에 push하고
[Windows full](https://github.com/jinyongp/devtools/actions/runs/40270980861)과
[Unix native](https://github.com/jinyongp/devtools/actions/runs/40271033252)를 시작했다.
첫 Windows full은 통과했고 Unix에서는 macOS Intel의 기존 `proxy_auto` 부분 readiness 검사가 실패했다.
최초 원격 push 승인 대기와 PowerShell 접근 제한은 해소됐다. 별도 브랜치·워크트리는 만들지 않았다.

실패한 fixture는 Python/TLS 준비 확인에 300ms만 주었다. 이 검사의 목적은 startup 지연 SLA가
아니라 ready/pending의 부분 결과와 동일 UUID 재시도다. 예산을 3초로 변경하고 기대 상태는
유지했으며 실패 시 실제 outcomes도 남긴다. 지연 command는 ready.flag를 쓰기 전에는
ready가 될 수 없다. Linux installed `proxy_auto` 재검증과 후속 macOS Intel 설치 검증이 통과했다.

`eed45cacdbe1fa4edcabece27b10c5b2c2686659`를 대상으로 기존
[Release 검증 run 40271664472](https://github.com/jinyongp/devtools/actions/runs/40271664472)를
`publish=false`, `reuse_validation=false`, `system_network_smoke=false`로 실행했다.
최초 dispatch의 축약 SHA는 입력 검사에서 거절되어 전체 SHA로 수정했다.
첫 native 시도의 Intel job은 제품 실행 전 외부 uv manifest 다운로드 타임아웃으로 실패했고,
run 종료 후 실패 부분을 재실행한 attempt 2에서 다운로드와 실제 검사를 통과했다.
제품 기대값을 바꾸거나 실패한 job을 제외하지 않았다.

최종 상태는 attempt 2 / `success`다. 전체 job을 pagination 포함 조회하여
61 success, 6 intentional skip, failure 0을 확인했다. skip은 두 외부 provider,
system-network smoke와 `publish=false`에 따른 release/homebrew/installer-page다.
최초 실패·성공한 재실행 모두 같은 위 source SHA를 가리킨다.

| native 근거 | 결과 |
| --- | --- |
| `ci-linux`, `ci-macos` | PASS 전체 installed 시나리오와 profile 교환·legacy 보호 경계 |
| Unix handoff amd64/arm64 Linux, Intel/ARM macOS | PASS 실제 `cli_presentation` PTY와 proxy/readiness/replay. Intel 로그에서 두 시나리오의 성공을 직접 확인 |
| Windows amd64/arm64 full lanes | PASS Go 제품 suite·race·설치/업데이트·Chromium·native completion·profile 교환 |
| `TestPowerShellNativeCommandLineRoundTrip` | PASS 실제 `powershell.exe`와 `pwsh` 하위 검사. amd64 suite-rest JSON 이벤트의 `Action:pass` 확인 |
| 외부 provider·system-network smoke | 입력에 따른 의도적 미실행. 이번 표시 변경에 추가하지 않는 기존 범위 유지 |

## D14: 실제 사용 피드백 이후의 가독성 수정

사용자가 제공한 실제 출력은 기존의 전체 필드 보존 규칙이 읽기 화면의 목적을 달성하지 못함을
보여줬다. `command list`의 반복 Item/selection/Details와 argv 개수 요약을 제거하고,
이름과 전체 실행 내용을 한 번씩 표시한다. 오타 오류는 잘못된 입력과 교정 후보를 먼저 안내한다.
공통 오류·doctor·경고·readiness·action도 설명과 복구 행동을 중심으로 줄였다.
부분 변경·결과 불명확·동일 UUID 재시도와 원 action argv는 계속 보존한다.
전체 공개 정보와 확장 필드는 JSON에 유지한다. 상세 결정은 계약 리뷰의 D14가 우선한다.

리뷰 범위는 목록, parser 오류, 일반 오류와 복구 안전성, 진단·경고·readiness, action/template,
좁은 화면, JSON 호환성과 fallback이다. 수정 후 해당 경로와 호출자를 다시 검토했다.
27개 명령은 폭 40/80/120에서도 두 번째 상세 목록 없이 각 이름과 실행 내용을 한 번씩 출력한다.
실제 CLI 오타 검사는 text의 교정 제안과 JSON facts/sources/effects/retry 보존을 함께 검증한다.
복구 검사는 공개 target 안의 request ID를 사용하는 실제 형태를 포함한다.

제품 소스 `8535c3d`에서 `devtools run check:all`과 전체 `devtools run test:installed`가 통과했다.
임시 로그는 `/tmp/devtools-readable-text-final-check-all.log`,
`/tmp/devtools-readable-text-final-installed.log`다. 로컬 `bin/devtools`도 다시 빌드해
사용자가 제공한 두 명령과 doctor의 실제 출력을 확인했다.
위의 Release run은 D14 이전 소스의 증거이며 이번 수정의 native 통과 근거로 재사용하지 않는다.
이번 소스의 Actions 결과는 별도로 기록한다. 실제 terminal 시각 확인 gate는 여전히 열려 있다.

후속 리뷰에서 종합 `diagnostics`의 하위 issue 안에 있는 공개 복구 안내가 빠지는 경로를 확인해
`c1911a1`에서 수정했다. 하위 subsystem 이름, cause 설명, 효과/동일 UUID 복구와 원 action을
보존하는 회귀 검사를 추가했다. 사용자 후속 지시로 가운데점과 장식 구분자를 제거하고
제목과 Profile을 별도 줄로 나눴다. 다른 목록의 장식 대시도 일반 label 구분으로 바꿨다.

설정 오류의 public cause에는 message가 없는 형태도 있어 reason의 문장화와 실제 field 경로,
line/column을 보존했다. 실제 잘못된 TOML로 재현했고 argv offset은 계속 생략한다.
이 두 수정은 `86358b0a92f28d7a8c695d0730a605dde908ddf6`까지 포함한다.
이 최종 제품 소스에서 `devtools run check:all`, 전체 `devtools run test:installed`가 다시 통과했다.
임시 로그는 `/tmp/devtools-readable-text-final-check-all-r3.log`와
`/tmp/devtools-readable-text-final-installed-r3.log`다.

- [Windows full run 40281534138](https://github.com/jinyongp/devtools/actions/runs/40281534138):
  success. 전체 페이지 조회 결과 23 success, failure/skip 0. amd64/arm64 native 검증을 포함한다.
- [Unix run 40281534643](https://github.com/jinyongp/devtools/actions/runs/40281534643):
  success. 전체 페이지 조회 결과 24 success, 3 intentional skip, failure 0.
  Linux/macOS source 검사와 native installed handoff를 포함한다. skip은 선택하지 않은 실제
  Tailscale/Cloudflared provider와 비활성화한 자동화 성능 측정이다.
- 두 run 모두 위 최종 제품 SHA를 가리킨다. 중간 수정으로 대체된 run은 최종 근거로 합산하지 않는다.
- 등록된 validation 8개의 method에도 D14 우선 적용과 장식 구분자 금지를 반영했다.
  실제 시각 확인 gate와 stale prerequisite completion은 자동 검사 통과로 덮어쓰지 않는다.

## D15: 실제 ANSI 화면 피드백 이후의 스타일 수정

사용자 screenshot에서 명령 이름 전체가 cyan이고 exec가 기본 밝기여서 주정보와 보조 정보의
위계가 뒤집힌 것을 확인했다. 목록의 개수와 중복 Profile 행도 제목의 범위를 불분명하게 했다.
내용/ANSI strip 검사 통과를 실제 시각 품질의 통과 근거로 사용했던 한계를 반영한다.

공유 presentation의 제목과 label, 목록, exec, Next 구획을 함께 검토했다. 제목은 중립적인
bold, 이름과 label은 기본 전경색, exec와 개수만 dim으로 바꿨다. 비기본 실행 설정과 복구
명령은 dim으로 낮추지 않는다. 오류·경고·PREVIEW 같은 상태의 semantic color는 유지한다.
목록 제목에 profile을 포함하고 중복 scope 행은 제거했다. 빈 environment는 common으로 안내한다.

실제 `command inspect`는 기본 설정에서도 내부 revision과 빈 identity를 긴 트리로 표시했다.
정적 설정은 명령 이름·exec·값 주입 설정을 먼저 보여주고 내부 관측 정보는 JSON에 남긴다.
검증 실패·보류는 설명으로 보존하고 알 수 없는 관측 형태는 기존 fallback으로 유지한다.
실제 CLI 회귀 검사에서 text 요약과 JSON 전체 관측 보존을 함께 확인한다.

실제 CLI의 command list/inspect, doctor, 오타 오류를 PTY로 실행해 ANSI 출력을 캡처했다.
동일 출력을 밝은/어두운 배경으로 렌더링한 미리보기를 검토했다. 이는 browser에서 ANSI를
표현한 미리보기이며 실제 OS terminal 테마/console 검증과 구분한다. 목록의 긴 exec는 자르거나
argv 개수로 대체하지 않는다. 관련 의미·폭·unknown 필드·복사 가능한 action 검사는 재사용한다.

제품 소스 `640e0b4d31a08d549cf84356c5d41463a09fca16`에서 `devtools run check:all`과
전체 `devtools run test:installed`가 통과했다. installed `cli_presentation` 시나리오는 실제
PTY/pipe, agent 감지, 오류 stream, 형식 우선순위, 폭과 복사 가능한 Next 검사를 포함한다.
임시 로그는 `/tmp/devtools-style-check-all.log`, `/tmp/devtools-style-installed.log`다.

- [Windows full run 40296950151](https://github.com/jinyongp/devtools/actions/runs/40296950151):
  success. 전체 페이지 조회에서 23 success, failure/skip 0. amd64/arm64 native 검사를 포함한다.
- [Unix run 40296950133](https://github.com/jinyongp/devtools/actions/runs/40296950133):
  전체 job은 24 success, 3 intentional skip, failure 0이다. Linux/macOS source·installed 검사와
  실제 PTY를 포함한다. skip은 선택하지 않은 실제 Cloudflared/Tailscale과 자동화 성능 측정이다.
- 두 run의 source SHA는 위 D15 제품 소스와 같다. 계획의 스타일 표와 등록된 workstream
  plan/spec에도 D15를 반영했다(profile revision 1142). 실제 native 시각 gate를 pass로 바꾸거나
  stale prerequisite completion을 강제로 갱신하지 않는다.

## 남은 수용 조건

- A11의 OS native 실행은 위 Actions로 검증했다. Windows console/ConPTY의 실제 화면 상태는
  이 기록의 pipe·PowerShell native argv 검사와 별개이며 시각 확인 pass로 기록하지 않는다.
- A13: Linux/macOS/Windows의 밝은·어두운·mono terminal에서 대표 목록·상세·preview·replay·
  부분 실패·오류의 실제 화면 확인. PTY byte 검사나 ANSI strip 비교를 시각 pass로 대체하지 않는다.
- 사용자 승인 후 로컬 Windows PowerShell 5.1 버전 조회는 성공했지만, 후속 사용자 지시에 따라
  native 제품 검증은 기존 Actions에서 수행했다. 태그·릴리스 발행이나 사용자 설치본 교체는 하지 않았다.
- T05 구현과 OS native 자동 검증, T06의 소스 리뷰·수정은 수행했다. 실제 화면 gate가 남아 있어
  등록된 required validation 전체를 통과 처리하거나 workstream을 close하지 않는다.
