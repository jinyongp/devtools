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

## 남은 수용 조건

- A11의 OS native 실행은 위 Actions로 검증했다. Windows console/ConPTY의 실제 화면 상태는
  이 기록의 pipe·PowerShell native argv 검사와 별개이며 시각 확인 pass로 기록하지 않는다.
- A13: Linux/macOS/Windows의 밝은·어두운·mono terminal에서 대표 목록·상세·preview·replay·
  부분 실패·오류의 실제 화면 확인. PTY byte 검사나 ANSI strip 비교를 시각 pass로 대체하지 않는다.
- 사용자 승인 후 로컬 Windows PowerShell 5.1 버전 조회는 성공했지만, 후속 사용자 지시에 따라
  native 제품 검증은 기존 Actions에서 수행했다. 태그·릴리스 발행이나 사용자 설치본 교체는 하지 않았다.
- T05 구현과 OS native 자동 검증, T06의 소스 리뷰·수정은 수행했다. 실제 화면 gate가 남아 있어
  등록된 required validation 전체를 통과 처리하거나 workstream을 close하지 않는다.
