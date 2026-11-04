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

### Native 검증 결과와 남은 대기

검증 대상은 `71e3d96be972c65237f385586f807d0b0c00bf08`이다. 구현은 `d88dddf`, 설치본
검증과 계약 문서는 `71e3d96`으로 나눠 커밋했다. 아래 결과 기록 이후에는 문서만 변경한다.

| 기존 workflow | 확인한 결과 |
| --- | --- |
| [Windows platform boundaries](https://github.com/jinyongp/devtools/actions/runs/41849308567) | 최종 gate 포함 23개 job 성공. ARM64와 x64 CLI, amd64 race, 제품과 설치기 검사 통과 |
| [Unix validation](https://github.com/jinyongp/devtools/actions/runs/41849352992) | 22개 job 성공, 선택 검사 3개 제외. 마지막 `macos-15 / rest`는 runner 배정 대기 |

2026-11-04 14:58 UTC 확인 시 마지막 macOS job은 `status: queued`, `runner_name: ""`,
`steps: []`였다. 약 21분 동안 실행을 시작하지 못했으며 제품 테스트 실패는 관측되지 않았다.
Unix 최종 gate도 이 job을 기다리므로 **Unix 전체 검증 완료로 판정하지 않는다**. Workflow는
취소하거나 다시 시작하지 않았고 기존 실행이 계속 대기한다. 코드와 검증 조건을 바꿔 이
대기를 우회하지 않는다.

이미 통과한 Unix 범위는 macOS와 Linux CLI race, 읽기 전용 잠금, ARM64와 Intel Mac 및
Linux 설치본, vault와 values, Windows/WSL2와 실제 브라우저 호환성을 포함한다. 제외한
선택 검사는 명시적 real Cloudflared, 두 기기 Tailscale, 성능 측정이며 각각
`live_provider=none`, `measure_automation=false`로 요청한 기존 workflow 조건이다.
