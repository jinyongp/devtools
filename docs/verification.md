# 검증 구성과 유지보수

[개발](development.md) · [설치 시나리오](../verify/README.md) · [성능 측정](../perf/README.md)

검사는 보호하는 실패와 실행 환경에 따라 유지한다. 단위 테스트가 같은 기능을 호출하더라도
실제 설치·사용자 권한·브라우저·provider를 통과하는 검사는 별도 근거다.
필수 상태 이름은 `Windows validation`·`Homebrew validation`이다. focused 진단이나
release의 재사용 실행은 `Windows focused validation`으로 보고하며 필수 검사를 대신하지 않는다.

## Workflow의 역할

| Workflow | 실행 시점과 보호 범위 | 유지 방침 |
| --- | --- | --- |
| Windows platform boundaries | 모든 PR, 관련 push, Release 호출. x64·ARM64 Go/vet, 보안·복구·프로세스, 패키지·설치·browser, x64 race | 전체 필수 gate 유지. `focus`는 빠른 진단용 |
| Unix validation | 관련 PR, 수동 실행, Release 호출. Linux·macOS race/static, 네 아키텍처 handoff·다른 UID | 전체 기능 검사 유지. 과거 성능 비교는 Release·선택 수동 실행 |
| Proxy consumer compatibility | Unix에서 재사용, 독립 수동 실행. 실제 Framework/Library, Chromium/Firefox/WebKit, LAN, Windows·WSL2 browser | Chromium을 HTTP/HTTPS 두 작업으로 분할. 공개 provider는 명시 dispatch만 |
| Homebrew | 모든 PR, 관련 main push, 수동 실행. Formula 생성·spec/audit | 필수 gate 유지 |
| Homebrew native validation | 수동 실행. 실제 네 플랫폼의 Formula 설치·테스트, native receipt | 배포하지 않고 native 경계를 재검증하는 경로로 유지. PR spec 검사와 구별 |
| Release tooling validation | 관련 PR/main push, 수동 실행. release 도구의 prepare/preview/distribute, installer byte 일치 | 게시 없이 release 도구 변경을 검증하므로 유지 |
| Release | 명시 version·commit의 수동 실행. 전체 native·설치·OS vault·profile 교환·패키징, 선택적 게시 | 배포 파이프라인으로 유지. 일반 PR 검사와 목적이 다름 |
| Publish Installer Page | stable release 후 또는 명시 tag의 수동 실행 | 검증 후 실제 Pages 게시를 수행하므로 별도 유지 |

GitHub의 `Dependabot Updates`는 서비스가 생성하는 항목이다. 저장소의 별도 workflow 파일이 아니다.
`Windows helper cancellation investigation`은 소스에서 이미 삭제된 과거 조사 기록이다.
실행 기록을 지워 sidebar를 정리하지 않는다. 기존 run이 남아 보이는 것과 현재 실행 경로는 구분한다.
`Windows support preflight`는 지원 구현 전 build failure를 허용하던 조사용 workflow였다.
현재는 삭제하고 독립 API 관찰만 Windows workflow의 `focus=api`로 합쳤다.

## 로컬 검사의 선택

`go build -o bin/devtools ./cmd/devtools` 후 checkout CLI를 PATH 앞에 둔다.
Windows에서는 실행 파일 이름에 `.exe`를 사용한다. 아래 명령은 저장소 루트 기준이다.

| 목적 | 명령과 경계 |
| --- | --- |
| 변경 영역의 빠른 확인 | `devtools run check:dashboard`, `check:project`, `check:installer`, `check:release`, `check:skill-release`, `check:perf` 중 선택 |
| 정적·개발 도구 전체 | `devtools run check:static`: 위 회귀 검사와 Go 형식·vet. 실제 browser·installed 전체는 포함하지 않음 |
| 일반 소스 회귀 전체 | `devtools run check:all`: static과 모든 Go 패키지. Unix는 race 포함. disposable native OS 검사를 대신하지 않음 |
| CI Go 분할 재현 | `devtools run test:go-lane cli`·`values`·`vault`·`handoff`·`rest`. 모든 패키지를 한 번씩 포함하고 새 패키지는 rest에 배정 |
| 설치 후 공개 흐름 | `devtools run test:installed --list`, `devtools run test:installed all` 또는 시나리오 이름. 한 번 패키징하고 매 시나리오는 격리 |
| 실제 소비자/browser | `devtools run check:proxy-compat --binary CANDIDATE ...`. frozen fixture·browser/컨테이너 전제는 검증 안내 참고 |
| 공개 Skill 발견 | `check:skill-discovery-public`은 공개 저장소의 상태를 검사. 로컬 checkout 발견·archive 검증과 구별 |

Unix의 일반 source matrix는 `cli`·`values`·`vault`·`rest`를 실행하고, 기존 네 native
handoff 작업이 `valuehandoff`·`runnerengine`을 담당한다. handoff는 `-race -count=1`을
유지한다. 로컬 `check:all`은 여전히 `./...` 전체를 실행한다.
Windows는 여섯 suite의 합집합으로 전체 패키지를 검사한다. handoff 두 패키지와 설정 두
패키지를 전체 suite 전에 다시 실행하지 않는다. `focus=config`의 독립 재현은 유지한다.
실제 다른 SID의 fixture, `-p 1`, native peer의 skip 금지, package shim, ARM64 x64 provider,
handle 누수 mutation, 반복 lifecycle/TLS, 설치 중 실행 파일 교체는 유지한다.
Release의 exact-SHA 재사용 판정도 같은 전체 suite step의 성공을 요구한다.

## 설치 시나리오와 플랫폼 검사

다음 35개 시나리오는 `verify/run.py`가 파일 이름으로 자동 발견한다. 각 행은 실행 범위가
겹치더라도 공개 계약·실패 경계가 달라 유지하는 그룹이다.

| 영역 | 시나리오 |
| --- | --- |
| 설치·업데이트·패키지 | `bootstrap`, `install`, `update`, `discovery`, `completion` |
| 프로젝트·설정·진단 | `config_edit`, `doctor`, `project_lifecycle`, `workflow` |
| 프로세스·포트·출력 | `ports`, `processes`, `readiness`, `cancellation`, `output_redaction`, `tty`, `cleanup` |
| 네트워크·proxy·공유 | `network`, `network_failures`, `proxies`, `proxy_auto`, `proxy_policy`, `exposure`, `http2_exposure`, `cloudflared_faults`, `tailscale_simulated` |
| 보호된 값·저장·전송 | `backup`, `profiles`, `storage_migration`, `secret_mutations`, `value_handoff`, `value_updates`, `vault`, `vault_session` |
| 작업 관리 | `tasks`, `task_storage` |

`check-linux-vault*.py`, `check-macos-vault.py`, `check-macos-installed-vault.py`는 실제
Secret Service/PAM logout/Keychain을 검사한다. `check-unix-vault-peers.py`와 Windows
native peer 검사는 다른 UID/SID를 사용한다. `check-legacy-vault.py`는 실제 구 CLI와의
보호 경계, `check-profile-exchange.py`는 네 OS origin 사이의 암호화 archive 교환을 확인한다.
이들은 일반 passphrase fixture나 mock의 성공으로 대체하지 않는다. `wsl_vault.py`의 실제
distro 재시작과 `wsl_proxy_compat.py`의 Windows browser 경계도 서로 다르다.

독립 `proxy_policy_contract.py`는 공식 구 daemon·strict reader와 downgrade를 비교한다.
새 candidate만 검사하는 `proxy_policy` 시나리오와 함께 유지한다.
`check-windows-handle-cleanup.py`는 handle 정리 코드를 망가뜨린 overlay가 실제 테스트에서
실패하는지 확인하는 negative control이다. 일회성 조사 도구로 제거하지 않는다.

snapshot 읽기 fixture는 기존 `vault.Profile.Batch`로 데이터를 준비해 서명을 한 번 수행한다.
10·100·1,000개 크기와 key 접근 1회·값·무결성·비노출 검사는 유지한다. 단일 변경의 검증과
실패 원자성은 기존 vault batch/values writer 검사가 담당한다. 로그 압축 테스트는 실제
512회 쓰기와 I/O·크기·내용 검사를 유지하고, 기대 tail만 전체 입력에서 한 번 계산한다.
dashboard의 pnpm alias도 `dashboard-*.test.mjs` 패턴을 사용해 수동 파일 목록의 누락을 막는다.

## 수동 진단과 성능 측정

| 도구 | 용도와 실행 정책 |
| --- | --- |
| `verify/probes/native_windows.go` | 독립 Windows API 관찰. `focus=api`에서 수동 실행, full product gate 대체 불가 |
| `verify/probes/wsl.py` | 현재 WSL 환경의 interop·loopback 관찰. 자동 지원 판정이나 distro restart 검사 아님 |
| `verify/probes/storage-identity/` | 고정된 과거 소스의 incident 재현·trace. 일반 release gate에 포함하지 않음. 필요한 조사에서만 실행 |
| `perf/run.py`, `perf/fixture/` | hyperfine 공개 CLI·Go benchmark 측정과 격리 데이터 준비. 전체 측정은 수동 실행 |
| `perf/automation.py` | 고정 baseline과 보호된 mutation receipt·handoff 비교. Release 및 Unix dispatch의 `measure_automation=true`에서 실행 |
| `perf/memory.py` | Linux/WSL CLI·key daemon의 RSS 측정. latency 측정과 구별 |
| `perf/convergence.py` | 값 변경에서 감지·선택적 재시작·ready까지의 수렴 측정 |
| `perf/mutations.py` | 보호된 bulk 변경·contention의 baseline/candidate 비교 |

storage identity builder의 `test_build.py`는 pinned commit의 instrumentation이 원래 판정을
보존하는지 확인한다. 해당 commit object를 준비한 조사 환경에서 실행한다. shallow PR checkout의
일반 검사에 편입하거나 과거 fixture를 현재 구현의 지원 근거로 해석하지 않는다.
shell/PowerShell packaging·installer wrapper는 기존 호출자와 실제 OS 실행 경로를 보존한다.
단순히 같은 Python 구현에 진입한다는 이유로 호환 entry를 삭제하지 않는다.

과거 성능 비교는 latency 허용 기준 없이 수치를 보존하는 설명용 측정이다. PR 기본에서는
반복하지 않고 성능 runner의 unit 검사는 static gate에서 유지한다. Release는 측정을
명시 요청한다. 수동으로 선택한 측정의 실패·취소·skip은 최종 Unix gate도 실패하게 한다.

## 2026-10-23 정리의 근거와 남은 측정

정리 전 tracked inventory는 workflow 9개, Go test 파일 428개·46개 디렉터리,
별도 Node/Python/PowerShell test entry 19개, 설치 시나리오 35개였다.
모든 실행 진입점과 workflow 연결을 조사하고 중복 후보의 package·조건·환경을 대조했다.
428개 Go 파일의 모든 assertion을 개별 심층 검토한 결과는 아니다. 이번 정리에서
개별 Go assertion을 삭제하거나 전체 native 검사의 보호 범위를 축소하지 않았다.

시간은 기존 성공 run의 최신 attempt job 전체를 조회했다. workflow 전체 시간에는 queue가
포함되며 job 시간의 합은 과금 배수를 적용하지 않은 runner 점유 시간이다.

| 관측 | 근거 | 판단 |
| --- | --- | --- |
| Windows 전체 10분 6초, job 합 86.9분 | [39810327230](https://github.com/jinyongp/devtools/actions/runs/39810327230), 23 jobs | 별도 handoff 중복 step이 x64 66초·ARM64 46초. 같은 native suite로 통합 |
| Chromium job 7분 18초~8분 22초, matrix step 4분 43초~5분 44초 | [39798582393](https://github.com/jinyongp/devtools/actions/runs/39798582393), [39805912234](https://github.com/jinyongp/devtools/actions/runs/39805912234) | 48개 조합을 HTTP/HTTPS로 분할. 각 소비자의 baseline·policy·browser 의미 유지 |
| Windows ARM64 PS5 설치 step 5분 54초~5분 57초 | [39806040783](https://github.com/jinyongp/devtools/actions/runs/39806040783), [39810327230](https://github.com/jinyongp/devtools/actions/runs/39810327230) | 실제 PATH·install·실행 중 update·공개 stable→candidate 연속 흐름 유지. 이미 엔진별 분할 |
| Windows x64 race의 늦은 시작, Unix macOS 대기 | 위 Windows run 및 [39798582393](https://github.com/jinyongp/devtools/actions/runs/39798582393) | job 추가만으로 전체 완료 시간 감소를 보장하지 않음 |
| 과거 성능 비교 job 3분 21초~3분 35초 | [39798582393](https://github.com/jinyongp/devtools/actions/runs/39798582393), [39805912234](https://github.com/jinyongp/devtools/actions/runs/39805912234) | PR 기본에서 제외. Release·선택 수동 측정과 샘플·cleanup 검증은 유지 |

불필요한 Windows lane의 Node/uv 설치를 생략한다. Go·Python만 사용하는 lane의 환경 준비를
줄이되 CLI/runtime/rest/product의 Node와 Skill 검증의 uv는 유지한다.
로컬 실제 48개 조합의 정리 포함 시간 합은 HTTP 147.6초·HTTPS 146.9초였다.
각 작업에 모든 소비자를 두므로 특정 Framework의 준비 비용도 양쪽에 분산한다.
Chromium 두 작업은 의존성·컨테이너 준비가 한 번 더 필요하므로 총 runner 비용은 증가할 수 있다.
새 workflow의 실제 완료 시간·queue·runner 합을 재측정한 뒤 추가 분할 여부를 정한다.
설치 runner는 packaging과 시나리오별 정리 포함 시간을 출력하고 proxy 결과는 `elapsed_ms`를
기록한다. 이 정리만으로 얻은 전체 속도 개선율은 아직 측정하지 않았다.

Linux amd64·Go 1.27.3의 같은 호스트에서 다른 검사를 함께 실행하지 않고 `-race -count=3`로
각 테스트를 비교했다. snapshot의 `/1000` case는 49.94·50.12·50.13초에서
0.49·0.46·0.43초로, 로그 압축 case는 3.40·3.43·3.45초에서 1.11·1.12·1.14초로 줄었다.
각 중앙값은 50.12→0.46초, 3.43→1.12초다. 컴파일·queue를 제외한 case 시간이며
다른 플랫폼이나 workflow 전체 시간에 이 개선율을 적용하지 않는다.

로컬에서는 `check:all`의 static·전체 Go race 검사를 통과한 뒤 fixture를 바꾼 두 패키지의
전체 race와 vet를 재검증했다. 실제 `test:go-lane handoff`와 설치된 `proxy_auto`,
dashboard 36개, workflow actionlint도 통과했다. 실제 Chromium 결과 48개는 모두 성공했고,
최종 HTTP/HTTPS 명령의 선택 목록이 이 결과와 중복·누락 없이 각각 24개로 일치함을 확인했다.
Unix 최종 gate의 실제 shell 본문은 dependency·측정 선택의 상태 512개 조합으로 확인했다.
위 로컬 기록에는 새 Windows/macOS Actions 실행과 workflow 전체 완료 시간 측정을 포함하지 않았다.
이후 플랫폼 검증은 해당 commit의 Actions run에서 확인한다.

새 검증은 먼저 기존 gate·scenario·fixture에 연결할 수 있는지 확인한다. 독립 workflow는
권한·native 환경·게시 시점이 별도로 필요한 경우에 둔다. 단기 조사에는 기존 `focus`나
probe를 사용하고 종료 시 유지할 계약과 실행 경로를 기록한다.
