# 실행 충돌 복구와 공통 진행 UI

기준: main `709bed03952ae216efdc1f04dfd04d12888eadae` 이후 작업.
대상: CLI protocol 25, envelope 1. 별도 worktree 없이 main에서 검증한다.
사용자 데이터 정리와 릴리스 발행은 이 구현의 완료 조건에 포함하지 않는다.

## 완료 조건과 공통 구조

1. 서비스 충돌에서는 실제로 실행할 수 있는 다음 명령 하나를 안내한다. 인증된 live
   controller가 확인되면 실제 ID를 넣은 `process stop`, 그 외에는 원래 범위의
   `project status NAME`이다. 긴 history/state 필터나 원래 터미널 소유를 요구하지 않는다.
2. 프로젝트 상태는 기존 기록뿐 아니라 서비스 claim, OS의 TCP 리스너와 devtools 제어
   가능 여부를 함께 관측한다. 저장소 전환이 막혀도 읽을 수 있는 포트 정보는 유지한다.
3. `doctor --storage --fix`처럼 오래 걸리는 작업은 현재 단계와 대기 이유, 경과 시간을
   공통 UI로 표시한다. JSON과 알려진 agent 실행의 출력 계약은 유지한다.
4. 전체 제한 시간과 취소가 내부 잠금에도 전달되어야 한다. 결과 전에 진행 표시를 지우고,
   실패 뒤 부분 적용 보고서와 효과 범위를 보존한다.

`ports`는 claim과 TCP 상태를 관측하고, registry 잠금을 해제한 다음 상위 계층의
inspection callback을 호출한다. `services`는 같은 resolver로 실제 controller를
인증하고 profile, canonical directory, instance와 command를 대조한다. CLI foreground,
managed launcher, dashboard 준비와 doctor의 실행 준비가 같은 dependency factory를 쓴다.
제어 가능한 실행의 stop 안내도 한 함수에서 생성한다.

진행 신호는 출력과 독립적인 `internal/progress`의 context observer다. 저장소 엔진과
잠금 계층은 활동 시작과 끝만 알리고, `cliui.Progress`가 중첩 활동, 한 줄 갱신과 경과 시간을
처리한다. 출력 controller는 text stderr에만 연결한다. 이후
[명령 전체 확장](cli-progress-coverage.md)에서 일반 명령의 공통 실행부, 입력과 foreground
일시 중단까지 연결했다. 작업별 spinner나 전역 stdout 쓰기는 추가하지 않는다.

`update`의 installer는 별도 프로세스에서 JSON 결과를 전달한다. 그 프로세스의 단계 이벤트가
부모 CLI에 전달되지는 않는다. 명령 전체 확장 후에는 부모가 installer 전체 대기와 경과 시간을
표시하며, Windows의 같은 프로세스 단계는 더 자세히 표시한다. Unix shell 내부 단계를
추측하거나 JSON diagnostic을 진행 메시지로 해석하지 않는다.

## 관측과 제어의 경계

- Linux는 `/proc/net/tcp{,6}`의 LISTEN socket inode와 현재 PID의 fd를 대조한다.
  macOS는 OS의 `/usr/sbin/lsof` field output, Windows는 `GetExtendedTcpTable`의
  IPv4와 IPv6 owner PID table을 사용한다. 실행 인자와 환경변수는 수집하지 않는다.
- OS 조회는 최대 1초, 공개 프로세스는 최대 16개다. 권한 부족, 제한 시간과 부분 관측은
  원인과 함께 반환한다. PID나 프로그램 이름을 devtools stop 권한으로 사용하지 않는다.
- 저장된 ID가 있어도 인증된 controller 응답과 서비스 claim의 실행 범위가 맞아야 한다.
  원격 제어가 없던 과거 실행에 새 controller를 소급해서 만들지 않는다.
- 다른 프로젝트의 strict port와 충돌하면 같은 포트의 유일한 claim을 확인하고, 중지를
  안내할 경우 소유 프로젝트를 함께 표시한다. 모호한 후보를 임의로 선택하지 않는다.
- 정리 완료는 기존 Unix process group 또는 Windows Job 범위다. detached session,
  외부 daemon과 remote 자원은 포함하지 않는다. 리스너 부재나 상위 PID 종료만으로
  정리 완료를 추정하지 않는다.

OS 인터페이스 근거:
[Linux TCP 관측](https://www.kernel.org/doc/html/latest/networking/proc_net_tcp.html),
[lsof field output](https://github.com/lsof-org/lsof/blob/master/docs/manpage.md),
[Windows TCP table](https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getextendedtcptable),
[Windows IPv6 owner row](https://learn.microsoft.com/en-us/windows/win32/api/tcpmib/ns-tcpmib-mib_tcp6row_owner_pid).

## 출력과 실패 처리

| 상황 | 동작 |
| --- | --- |
| 빠른 작업 | 진행 표시 없이 기존 최종 결과만 출력 |
| ANSI text 터미널 | 250ms 뒤 현재 활동과 전체 경과 시간을 한 줄에 갱신 |
| text 파이프와 단순 터미널 | 제어 문자 없는 단계 출력, 같은 대기는 10초 간격 갱신 |
| JSON 또는 알려진 agent | 진행 표시 없음, 기존 envelope와 stream 경계 유지 |
| 다른 작업의 잠금 대기 | 대기하는 자원 종류와 잠금을 다른 작업이 보유했다는 사실 표시 |
| timeout 또는 취소 | 내부 잠금도 종료, 진행 표시 정리 후 최종 보고서 출력 |
| process 기록 전환 필요 | `project status`의 `complete: false`, 포트 관측과 진단, storage fix 안내 |
| 제어 가능한 실행 | 실제 실행 ID의 stop 명령, 선택인 request ID는 예제에서 생략 |
| 제어 불가 또는 미확인 | 관측 가능한 PID와 프로그램, 원인과 scoped project status 안내 |

전환의 실제 항목 수를 아는 apply 단계만 `(현재/전체)`를 표시한다. 알 수 없는 진행률을
백분율로 만들지 않는다. 기록, journal과 보호된 값을 화면에 출력하지 않는다. 출력 transport
오류가 작업의 적용 결과를 바꾸지 않으며 완료 문서는 기존 renderer가 계속 담당한다.

## 검토와 검증 기록

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 내부 잠금 제한 시간 | 검토와 회귀 검증 완료 | 실제 retained-body 잠금을 유지한 채 caller deadline 안에 inspection 종료 |
| 진행 UI와 출력 경계 | 검토와 로컬 검증 완료 | 실제 잠금, text/JSON/agent/단순 터미널, 취소와 최종 출력 전 지우기 |
| 설치본 PTY | Linux 검증 완료 | 실제 터미널 관측, 파이프, SIGINT, JSON envelope 보존 |
| 제어 권한과 범위 | 검토와 로컬 검증 완료 | 인증 실패, controller 손실, ID 없음과 profile/directory/instance/command 불일치에서 stop 차단 |
| 실제 복구 명령 | 로컬 검증 완료 | 안내한 foreground와 managed stop argv를 그대로 실행하고 owned cleanup 완료 확인 |
| 저장소 전환 중 조회 | 로컬 검증 완료 | legacy 바이트 불변, 실제 리스너 관측, 출력한 fix argv 적용 후 재조회 |
| native OS와 통합 회귀 | 검토와 검증 완료 | 최종 소스의 Windows 23개, Unix 24개 job 성공. 선택 검사 3개 제외 |

검토 중 기존 process 저장소 adapter가 내부 retained-body 조회와 전환 쓰기에
`context.Background()`를 사용해 caller deadline을 넘길 수 있음을 재현했다. 기존 구현을
복사하지 않고 context-aware 내부 함수를 추가해 일반 API와 명시적 전환이 공유하도록 고쳤다.
사용자 macOS에서 보고한 장시간 대기가 이 경로 때문인지는 아직 확인하지 못했다.

전체 검증 중 발견한 protocol schema의 남은 버전 상수와 대체 lifecycle backend의 조회
경로도 보완했다.

## 최종 통합 검증

검증 소스: `d2b2618000173632c32b06903738d740e80470ba` / protocol 25.
실행 코드의 마지막 변경은 `9d10744738a33fcf980ad1e288aad8ff3b53db08`이며,
이후 `d2b2618`은 대용량 테스트의 준비 방법만 바꾼다.

- Linux 로컬 `go test -race ./...` 전체 통과.
- 같은 소스의 `python3 -B verify/run.py` 설치 시나리오 40개 전체 통과.
  실제 PTY의 진행 표시, 파이프, JSON과 agent 출력, SIGINT 및 timeout을 포함한다.
- `./scripts/project check-static` 통과. Skill packaging, dashboard, installer,
  release/project/perf 자체 검사, gofmt와 Go vet를 포함한다.
- [Windows 전체 검증](https://github.com/jinyongp/devtools/actions/runs/41841494339)
  23개 job 통과. amd64와 arm64 CLI, 설치, 제품 브라우저, native API와 amd64 race를 포함한다.
- [Unix 전체 검증](https://github.com/jinyongp/devtools/actions/runs/41841575828)
  두 번째 attempt에서 전체 성공. macOS와 Linux의 24개 job이 통과했고, 명시적으로 선택하지
  않은 public provider와 automation 측정 3개 job은 설정대로 제외됐다.
  실제 PTY, 읽기 전용 query lock, native handoff와 브라우저 경로를 포함한다.

### CI 중 발견한 테스트 준비 비용

[첫 Windows race 검사](https://github.com/jinyongp/devtools/actions/runs/41839131438/job/126605732205)는
8분 제한에 걸렸다. 스택은 실제 탐색 전 1만 개 합성 기록을 만들며 `FlushFileBuffers`를
호출하던 위치였다. `navigation_scale_test.go`의 과거 기록 생성에 기존 private fixture
writer를 재사용했다. 제품의 crash-safe writer는 바꾸지 않았으며 기록 313/1000/10000개,
없는 이력 본문, 선택된 live RPC 1/2회, 무관한 RPC 0회와 응답 크기 제한을 모두 유지한다.

수정 후 Windows race에서 1만 건 subtest는 143.66초, services package 전체는 217.01초에
통과했다. 서로 다른 runner의 관측값이므로 성능 향상 배율로 해석하지 않는다.

### 미재현 프록시 검사 관측

Unix 첫 실행에서 기존 `TestManagerClosesHijackedConnectionsBeforeStopReturns`의 stop
호출이 한 번 실패했다. 같은 최종 소스의 Linux 전체 로컬 race 검사는 통과했고, 해당 검사를
로컬에서 100회 반복한 결과도 통과했다. 제품 원인은 확정하지 않았으며 이 관측만으로
프록시 동작이나 검증 조건을 바꾸지 않았다. 같은 커밋의 실패한 원격 job만 재실행해 통과했다.
재현된 제품 결함으로 판정하지 않았으며 단발 실패의 원인은 미확정으로 남긴다.

검토한 전체 영역에서 확인된 수정 대상은 해결했고, 최종 검증 결과는 모두 성공이다.
별도 worktree, 사용자 저장소 정리와 릴리스 발행은 수행하지 않았다.
