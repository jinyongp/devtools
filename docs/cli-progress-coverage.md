# 오래 걸리는 CLI 작업의 진행 표시

Protocol 25, envelope 1. `doctor`와 `project status`에 한정했던 진행 표시를 공통 명령
실행부로 확장한다. 별도 worktree 없이 main에서 작업하고 기존 native workflow로 검증한다.

## 공통 계약

일반 명령은 실행이 길어지면 `Running devtools profile list (3s)`처럼 현재 명령과 전체 경과
시간을 stderr에 표시한다. 실제 단계 이벤트가 있으면 그 메시지가 기본 표시를 대신하고,
끝나면 상위 단계로 돌아간다. 새 일반 명령도 이 실행부를 통과하므로 개별 spinner 등록이
필요하지 않다. 결과의 JSON schema, stdout 내용, exit code와 적용 효과는 바꾸지 않는다.

- ANSI text 터미널은 250ms 뒤 한 줄을 갱신한다. 짧은 작업에는 표시가 없다.
- 명시적인 text 파이프와 단순 터미널은 제어 문자 없이 표시한다. 단계 변경은 최대 초당
  한 번, 같은 대기는 10초 간격이다. 출력 전용 파일을 생성하는 stdout에는 쓰지 않는다.
- JSON과 알려진 agent 실행은 진행 표시를 출력하지 않는다. 자동 형식의 파이프도 기존
  JSON 계약을 따른다.
- 비밀번호, init 질문, hosts 사전 안내와 sudo가 터미널을 사용할 때는 동기적으로 표시를
  지운다. 중첩된 입력이 모두 끝나야 재개하며, 재개 뒤에도 250ms 지연을 적용한다.
- foreground 프로그램이 실행되는 동안에는 stdout과 stderr를 프로그램에 맡긴다.
  보호된 출력의 마지막 flush가 끝난 뒤 표시를 재개한다. 준비와 종료 후 정리가 오래 걸리면
  그 단계는 표시한다. 표준 스트림을 다른 writer로 감싸 terminal 판별을 바꾸지 않는다.
- 최종 성공이나 오류를 출력하기 전에 표시를 닫고 goroutine을 종료한다. 부가적인 진행
  출력 실패가 실행 결과를 실패로 바꾸지 않는다. 취소와 timeout은 원래 작업의 계약을 따른다.
- 분모를 아는 storage apply만 현재/전체를 표시한다. 알 수 없는 진행률을 백분율로 만들거나
  PID, 비밀번호, 환경 값, 인증 정보와 저장소 내부 경로를 표시하지 않는다.

## 명령 전수 대조

현재 catalog의 canonical command는 199개다. 일반 결과 모드 194개, foreground passthrough
2개, artifact 2개, help text 1개이며 alias는 같은 handler를 사용한다. 아래 표의 명령군을
모두 공통 실행부와 대조했다. `schema`와 `config schema`는 기계 전용 계약을 우선한다.

| 명령군 | 개수 | 오래 걸릴 수 있는 경로와 표시 |
| --- | ---: | --- |
| `doctor` | 1 | 저장소 검사, 권한 검사, 전환 단계와 실제 잠금 대기 |
| `project` | 7 | 시작, 재시작, 종료, 준비 완료 대기, 서비스 및 listener 관측 |
| `process` | 9 | controller 시작 확인, 종료와 cleanup 확인, readiness 대기, 기록 조회 |
| `command` | 9 | 값과 포트 준비, 요구 조건 검사, foreground 실행 전후 |
| `proxy` | 24 | 경로와 연결 준비, listener 시작과 종료, 연결 정리, 제한된 OS helper |
| `expose` | 4 | 공개 tunnel 준비와 provider 확인, 종료, 인증 상태 변경 |
| `backup` | 6 | snapshot 수집, 암호화, 기록, 복호화 검사와 복원 |
| `profile` | 6 | 목록과 상세 조회, diff, export와 import, 공유 archive 경로 |
| `cleanup` | 6 | 후보 탐색, 선택 항목 보관, 복원과 삭제, 보존 이력 관리 |
| `vault` | 6 | 보호 설정과 인증, session unlock, 복구와 보호 방법 변경 |
| `values` | 15 | 조회, frozen delivery 준비, runner 응답 대기와 결과 기록 |
| `task` | 56 | 대량 이력 조회와 변경, graph와 export, query cache 구성 |
| `update` | 1 | 실행파일 업데이트와 새 실행파일을 통한 저장소 준비 |
| `dashboard` | 3 | server 시작 확인, 상태 조회와 종료 |
| `config` | 12 | 설정 조회와 변경. `config schema`는 JSON 전용 |
| `variable`, `secret`, `env`, `bundle`, `import` | 16 | 보호된 값 읽기와 변경, 대량 입력, 공통 저장소 잠금 |
| `port`, `instance` | 11 | 할당과 관측, 사용 중인 자원 검사와 변경 |
| `diagnostics` | 1 | 구성과 저장소 진단 |
| `init`, `version` | 2 | 공통 표시. init 질문 중에는 일시 중단 |
| `help`, `schema`, `license`, `completion` | 4 | 도움말과 기계 discovery, 원문 artifact 출력 계약 유지 |

숨겨진 shell completion 조회와 내부 JSON helper도 사용자 진행 출력을 만들지 않는다.
진행 이벤트를 보내지 않는 일반 조회나 변경도 공통 기본 메시지가 계속 보이므로 저장소,
대량 데이터와 외부 응답의 지연을 명령별로 누락하지 않는다.

Unix `update`는 별도 installer의 JSON stdout과 stderr를 제한된 private buffer로 받는다.
부모가 `Updating the executable and preparing storage`와 경과 시간을 표시하므로 installer를
기다리는 동안에도 무응답으로 보이지 않는다. shell 내부의 download나 전환 세부 단계를
추측해 표시하지 않는다. Windows는 같은 부모 표시 안에서 artifact fetch, 검증과 publication,
설치된 실행파일의 저장소 준비 단계를 추가로 보고한다. 새 child JSON 통신 규약은 추가하지 않는다.

## 검증과 리뷰

공통 실행 경로, 중첩과 일시 중단, 기계 출력 경계, native 출력, 실제 저장소 잠금과 취소,
암호화와 자원 제어의 기존 계약을 리뷰한다. 검증은 소스 검사와 대표 경계의 실행 증거를
구분한다. 199개 명령 각각에 인위적인 지연을 넣어 실행했다는 뜻은 아니다.

| 범위 | 검증 |
| --- | --- |
| 이벤트가 없는 handler | 실제 dispatcher에서 지연된 handler도 기본 진행 표시, JSON/agent/artifact는 침묵 |
| 중첩 입력과 출력 | 겹친 suspend의 독립 수명, 동기 clear, 늦게 끝난 활동, 재개 지연 |
| foreground | native 출력과 exit status 보존, 긴 무출력 구간에 spinner 미출력 |
| 설치본 PTY | 실제 collection/storage lock, timeout과 SIGINT, 지연된 비밀번호 prompt에서 redraw와 값 노출 없음 |
| 업데이트 대기 | 실제 storage lock으로 child installer를 지연해 부모 진행 표시와 같은 실행파일 바이트 유지 확인 |
| 전체 회귀 | Go race, 기존 정적 검사, 설치본 시나리오, 기존 Unix와 Windows workflow |

### 통합 리뷰에서 반영한 사항

공통 dispatcher와 전체 명령군, output mode별 조기 반환, 입력과 native 출력의 모든 쓰기
지점, updater의 프로세스 경계, 종료와 취소 및 부가 maintenance 경로를 검토했다.

- post-write maintenance가 끝나기까지 같은 observer를 유지한다. 이미 확정된 작업 결과와
  실패 효과를 진행 UI가 바꾸지 않으며, 최종 출력 전에는 반드시 닫는다.
- 실행 전 값 검증과 launch admission에는 진행 표시를 유지한다. 실제 실행 직전에 멈추고,
  보호된 출력의 마지막 flush가 끝난 뒤 재개하도록 수명을 조정했다.
- 중첩된 입력이 서로의 표시를 재개하지 않도록 독립적인 suspend ID를 쓴다. 입력 종료와
  native 출력 직후 짧은 작업에 불필요하게 표시가 나타나지 않도록 지연을 다시 적용한다.
- stderr를 제공하지 않은 API 호출은 진행 UI를 생략하도록 보호하고 회귀 검사에 포함했다.
- 현재 문서와 bundled skill의 doctor 한정 설명을 갱신했다. 초기 UI 계획의 spinner 제외도
  후속 공통 진행 표시의 좁은 예외와 연결해 현재 계약이 충돌하지 않도록 했다.

### 로컬 검증 결과

- 최종 Go 소스의 `go test -race ./...` 통과. CLI package는 229.601초에 완료했다.
- 기존 `scripts/project check-static` 통과. skill 수정 후 같은 검사를 다시 통과했다.
- 설치본 40개 시나리오 통과. 마지막 공통 보호와 installer 대기 검증을 추가한 뒤
  `cli_presentation`, `update`를 새 설치본으로 다시 실행해 통과했다.
- 실제 PTY에서 collection 잠금과 SIGINT, 두 비밀번호 prompt를 각각 700ms 유지한 입력,
  700ms 동안 조용한 foreground의 원문 출력과 exit 17을 확인했다. 같은 실행파일 update에서
  실제 저장소 잠금을 지연시켜 부모의 진행 표시와 실행파일 바이트 보존도 확인했다.

사용자 저장소 정리와 릴리스 발행은 수행하지 않았다.

### Native 검증 결과와 runner 재요청

검증 대상은 `71e3d96be972c65237f385586f807d0b0c00bf08`이다. 구현은 `d88dddf`, 설치본
검증과 계약 문서는 `71e3d96`으로 나눠 커밋했다. 이 절은 해당 소스에 대한 native 기록이다.

| 기존 workflow | 확인한 결과 |
| --- | --- |
| [Windows platform boundaries](https://github.com/jinyongp/devtools/actions/runs/41849308567) | 최종 gate 포함 23개 job 성공. ARM64와 x64 CLI, amd64 race, 제품과 설치기 검사 통과 |
| [Unix validation](https://github.com/jinyongp/devtools/actions/runs/41849352992) | 첫 실행에서 22개 job 성공, 선택 검사 3개 제외. 마지막 macOS job은 두 차례 모두 runner 배정 실패로 취소됨 |

2026-11-04 14:58 UTC 확인 시 마지막 macOS job은 `status: queued`, `runner_name: ""`,
`steps: []`였다. 약 21분 동안 실행을 시작하지 못했으며, 14:58:35 UTC runner 배정 실패로
취소됐다. GitHub annotation은 여러 차례 시도해도 hosted runner가 작업을 가져가지 못했고
macOS ARM64 수용량 부족으로 대기가 길어질 수 있다고 보고했다. 실제 테스트 step은 없었다.
이 때문에 후속 Unix gate도 실패했다. 제품 테스트 실패로 판정하지 않는다.

같은 소스에서 취소된 job과 후속 gate만 재실행 요청했다. 연결 도구의 Actions 쓰기 권한이
없어 authenticated GitHub CLI를 사용했으며, 기존에 성공한 검사는 다시 실행하지 않는다.
[두 번째 실행의 macOS job](https://github.com/jinyongp/devtools/actions/runs/41849352992/job/126655733672)도
runner 배정 없이 취소됐고 후속 gate는 15:16:50 UTC에 실패했다. 두 번째 실행에도
`runner_name: ""`, `steps: []`였으며 제품 테스트는 시작되지 않았다.
**Unix 전체 검증 완료로 판정하지 않는다**. Workflow와 제품의 검증 조건은 유지했다.

해당 소스에서 통과한 Unix 범위는 macOS와 Linux CLI race, 읽기 전용 잠금, ARM64와
Intel Mac 및 Linux 설치본, vault와 values, Windows/WSL2와 실제 브라우저 호환성을 포함한다.
제외한 선택 검사는 명시적 real Cloudflared, 두 기기 Tailscale, 성능 측정이며 각각
`live_provider=none`, `measure_automation=false`로 요청한 기존 workflow 조건이다.

### 저장소 시간 초과 진단 후속 수정

현재 task 저장소의 profile 잠금을 다른 프로세스가 보유한 fixture에서 기존 출력을 재현했다.
전체 제한에 도달하면 `tasks unknown → current`만 남고, 업그레이드 필요 여부를 확인하지
못했는데도 `Storage upgrade required`와 같은 `--fix` 반복 안내가 출력됐다. Fixture는
잠금 해제만으로 Ready가 되었고 모든 저장소 파일의 바이트는 유지됐다. 이 재현은 실제
사용자의 macOS 저장소에서 어떤 작업이 지연 원인인지 확인했다는 뜻은 아니다.

후속 수정은 다음 범위로 제한한다. 위 native 결과는 수정 전 소스에 대한 기록이며
후속 소스의 검증 결과로 재사용하지 않는다.

- 공통 coordinator가 adapter의 검증 완료 결과와 마지막 blocked 대상을 보존한다.
  오류만 반환하는 adapter에는 기존과 같이 domain 단위 blocked 결과를 추가한다.
- task profile 검사와 잠금 대기 실패에 profile과 알려진 형식을 남긴다. 확인하지 않은
  나머지 profile은 current로 반환하지 않으며, payload 검사 뒤에도 context를 확인한다.
- 현재 상태, 실제 전환 필요, 검증 불가 및 일부 전환 이후 미완료를 구분한다. 시간 초과의
  다음 명령은 같은 저장소를 읽기 전용으로 확인하며, 기본 30초보다 긴 제한을 제공한다.
- JSON 필드 구조, 종료 코드, effects 및 안전한 진단 정보 정책을 유지한다. 저장소 잠금을
  강제로 해제하거나 기존 무결성 검사를 생략하지 않는다. 개별 파일 codec 안의 동기 IO를
  중단하는 변경은 포함하지 않는다.

실제 task profile 잠금, 완료한 profile 보존, 미검사 profile 제외, 파일 바이트 불변,
복구 안내 실행 및 설치본 PTY와 isolation 범위의 회귀 검증을 통과했다. 변경 패키지의
storage/doctor/progress race 검사와 기존 정적 검사도 통과했다.

`96e3c2f3b75704e6c36aa4f1b344d6cddde669a4`의 전체 Go race와 설치본 40개 시나리오를
로컬에서 통과했다. CLI package의 race 검사는 271.749초였다.
[해당 소스의 Windows 검사](https://github.com/jinyongp/devtools/actions/runs/41861189515)에서는
두 아키텍처 모두 새 회귀 검사가 보유 중인 byte-range 잠금 파일을 읽다가 실패했다.
파일 전체 바이트 비교를 fixture 잠금 해제 직후로 옮겨 무변경 검증을 유지한다.
ARM64의 기존 collection 검사도 정상 진행 표시를 stderr 오류로 취급했으므로,
텍스트 결과의 선택과 렌더링 검증에서 stderr가 비어야 한다는 이전 가정을 제거한다.
진행 표시와 오류 스트림 분리는 기존 공통 UI 및 설치본 검사가 계속 담당한다.

[첫 v0.43.0 배포 실행](https://github.com/jinyongp/devtools/actions/runs/41861191236)은
위 검사 수정을 반영할 새 소스로 교체하기 위해 중단을 요청했다. 제품 코드와 JSON 계약은
`96e3c2f` 이후 변경하지 않았으며, 후속 main SHA의 native 검사와 Release workflow는
별도로 완료를 확인한다.

이 시간 초과 수정의 검증 대상은 `811966e40a7d92bbe1df97c708403cf2ef35867c`이다. 위 두 검사
보완 후 관련 CLI race 검사를 15.603초에 통과했다. 전체 Go race와 설치본 40개 결과는
변경하지 않은 제품 코드에 대한 로컬 근거이며, 새 main SHA의 native 완료를 대신하지 않는다.

2026-11-04 16:14 UTC 기준
[최종 Windows 검사](https://github.com/jinyongp/devtools/actions/runs/41863909764)의 22개 job은
모두 queued이고, runner와 실행 step이 없다.
[새 v0.43.0 배포 실행](https://github.com/jinyongp/devtools/actions/runs/41864428644)은
54개 job이 queued이고 기존 조건의 선택 검사 3개가 제외된 상태다. 첫 검증부터 runner
배정을 기다리고 있어 native 검증 또는 배포 완료로 판정하지 않는다. 새 실행은 취소하지
않으며, 필수 검증이 성공해야 태그 생성과 게시로 진행한다.

### 사용자 결과 기준으로 다시 수행한 리뷰

리뷰 기준은 [검증 구성](verification.md#사용자-결과를-기준으로-리뷰하기)에 기록하고,
저장소의 [AGENTS.md](../AGENTS.md)에서 구현·리뷰 시 읽도록 연결했다. 공통 실행부 연결과
spinner 정리 검사는 그대로 유지하며, 실패 설명과 후속 행동의 결과를 별도로 확인한다.

| 영역 | 발견한 누락과 수정 | 실행 근거와 남은 범위 |
| --- | --- | --- |
| 값·task profile | 헤더 검사 실패는 여전히 정상 prefix와 실패 profile을 버렸다. profile별 헤더·payload 검사를 공통 `Layout` 순회로 합쳐 검증을 마친 대상만 보존한다. | 실제 세 profile 중 두 번째를 손상한 검사가 수정 전 두 domain에서 실패했다. 수정 후 check/fix의 JSON/text, 파일 전체 바이트 보존과 입력 복구 뒤 Ready를 확인한다. |
| 프로세스·로그·archive·전달 잠금 | 중간 실패에서 전체 결과를 버리고 마지막 동기 검사 뒤 취소를 놓칠 수 있었다. 기존 `Step.InspectionFailed`로 완료한 대상과 실패 주소를 보존하고 context를 확인한다. | 실제 기록·로그·공유 archive·전달 파일과 잘못된 잠금 파일을 사용한다. 이전 네 adapter만 Go overlay로 연결한 음성 대조가 네 경우 모두 실패했고 수정본은 통과했다. |
| 값 operation receipt | profile 검사 결과 뒤의 receipt 오류가 앞선 결과를 버렸고 receipt 순회에는 context가 없었다. 실패 resource와 완료한 profile을 보존하고 읽기 경계에서 취소한다. | receipt 주소의 잘못된 파일 유형과 검사 단계에서의 취소, 무변경 및 fixture 제거 뒤 재검증을 확인한다. |
| 전환할 항목이 없는 `--fix` | blocked 결과만 있어도 coordinator 잠금·진행 기록을 만들어 불필요한 적용 상태를 남겼다. 전환할 항목이 없으면 검사 결과를 반환한다. | profile와 resource 실패 검사가 check/fix 전후 모든 fixture 파일을 비교한다. 실제 전환·부분 적용·재개 계약은 기존 engine 및 domain 검사가 계속 보호한다. |
| 설치본 후속 안내 | JSON 복구 argv 실행에 더해 실제 PTY에 출력한 text 명령을 그대로 실행하고 같은 isolation root가 Ready인지 확인한다. | 기존 `cli_presentation`을 확장한다. 손상된 값 profile의 JSON/text와 fixture 원본 복구 뒤 재검증도 같은 설치본으로 확인한다. |
| CI 실행 조건 | Windows race 명령에서 문서의 runner 내부 `-p 1` 정책이 빠졌다. 동일 정책을 적용하며 패키지·race·기한을 유지한다. | Workflow 문법과 실제 native 결과를 구분한다. 이 차이가 아래 두 CI 실패의 원인이라고 확정하지 않는다. |
| 텍스트 성공 검사의 전제 | 진행 표시가 허용되는 text 성공에서도 stderr 공백을 요구한 검사가 남아 있었다. 생성 request ID, columns, command alias, rendering과 publication 검사를 대조해 정상 진행과 오류를 구분한다. | 성공 코드·출력·동일 request 재생·오류 표시 금지를 유지한다. JSON·artifact·completion wire의 공백 조건과 공통 progress·foreground 스트림 검사는 유지한다. |

2026-11-04 17:05 UTC 기준, `811966e`의
[main Windows 검증](https://github.com/jinyongp/devtools/actions/runs/41863909764)은 23개 job이
모두 성공했다. 반면 같은 소스의
[Release 실행](https://github.com/jinyongp/devtools/actions/runs/41864428644)은 Windows의
`TestWindowsConcurrentRecordPublication`과
`TestReportResolvesLostACKWithoutRewritingCompletedUnknown`에서 실패했다. 전자는 30초의
publication fixture 기한, 후자는 receiver acceptance 관측에서 실패했고 Go data race
경고는 없었다. Unix의 일부 작업도 abandoned되어 gate가 실패했다. 이전 문단의 queued
기록은 당시 관측이며 현재 상태가 아니다. 이 Release는 게시하지 않았고 공개 stable은
v0.42.0이다. 두 Windows 실패의 간헐성 원인과 후속 소스의 native 완료는 별도 확인이
필요하다. 사용자의 실제 macOS 저장소 지연 원인도 아직 미확인이다.

`d6fda860bc215b469672ab79e6dfbddf14647f80`에서 `check:all`의 전체 정적·Go race
검사와 설치본 40개 시나리오를 통과했다. CLI race는 253.214초, 확장한 실제 PTY
`cli_presentation`은 정리 포함 12.826초였다. Workflow actionlint도 통과했다.
[해당 main Windows 실행](https://github.com/jinyongp/devtools/actions/runs/41874807321)은
21개 job 성공, ARM64 CLI와 집계 gate 실패였다. ARM64의 실제 task 생성·Request ID·
종료 코드는 정상이었고 `TestGeneratedOperationIDAppearsInTextSuccess`가 정상 진행 표시를
오류로 판단했다. 이 실행의 x64 race는 통과했지만 이전 실패의 간헐성 원인을 확정하는
근거로 삼지 않는다.

후속 `7499859195591f1e43d1b73e59337fd50988797b`는 위 text 성공 검사 전제 여섯 곳만
수정한다. 제품 코드·JSON 계약은 `d6fda86`과 같으며, 해당 출력·progress·foreground의
targeted race 검사를 9.219초에 통과했다. 최종 native와 배포 완료는 후속 실행으로 확인한다.
