# Windows native / WSL support implementation plan

## 1. 목적과 상태

Windows 네이티브 CLI를 추가하고 WSL 사용 환경을 개선한다. 구현자가 운영체제별 의미를
추측하지 않도록 지원 범위, 저장소 보장, 실행 계약, 설치·복구 방식, 검증 기준을 이 문서로 고정한다.
이 문서는 **구현 전 설계**이며 Windows 지원이 출시됐다는 안내가 아니다.

- 사용자 결정: 네이티브와 WSL 모두 개선한다. Windows ARM64를 첫 배포에 포함한다.
- 지원 목표: Windows 11 25H2·26H2의 x64·ARM64와 WSL2. 최소 버전은 25H2이며 각 버전의 최신 누적 업데이트를 전제로 한다.
- 검증 기준: GitHub Actions 실제 x64·ARM64 runner. 실제 PC 제품 검증은 수행하지 않고 미검증 한계를 공개한다. Windows 10은 공식 지원에서 제외하며 별도 self-hosted runner를 운영하지 않는다.
- 소스 기준: `28601633c67483e187084ce65527ebac1b8716f3`.
- 조사일: 2026-10-17. 실행 근거와 외부 자료는 [사전 조사 기록](windows-support/research.md)에 분리한다.
- 제품 구현은 하지 않았다. standalone probe와 사전검증 workflow만 추가했다.
- 아래 설계 선택은 이 계획의 기본 계약이다. 실제 검증이 반증하면 구현 중 임의 우회하지 않고
  사전 조사 단계로 돌아와 이 문서와 종속 작업을 함께 갱신한다.

현재 구현 착수 판정은 [준비 상태](#12-구현-착수-판정)에서 관리한다. 계획의 상세함과
실제 검증 통과를 구분한다. 제품이 아직 없으므로 제품 통합 테스트는 구현 후 완료조건이다.

## 2. 지원 범위

| 구분 | 첫 배포 계약 | 검증 환경 |
| --- | --- | --- |
| Windows 11 x64 | 25H2·26H2, Windows용 `devtools.exe`, 기존 CLI 기능, PowerShell 5.1/7 및 cmd 호출 | `windows-2025` 자동 검증. 실제 Windows 11 x64 PC 제품 검증은 미실행이며 Server와 client를 구분 |
| Windows 11 ARM64 | 25H2·26H2, 네이티브 ARM64 실행 파일. x64 에뮬레이션으로 대체하지 않음 | `windows-11-arm`, `GOHOSTARCH=arm64` 확인. runner의 실제 버전·build에 한정한 실행 근거 |
| WSL2 | Linux 실행 파일·Linux 저장소 유지. Windows 브라우저 접근과 interop 진단 개선 | 기존 WSL2 primitive 근거 + Linux CI·진단 fixture. 새 제품의 실제 WSL2 네트워크·Windows browser 연계는 미검증 |
| 기존 OS | macOS·Linux 기능 및 저장 계약 보존 | 기존 release matrix |

### 버전과 지원 주장의 경계

최소 지원 버전은 **Windows 11 25H2 (build family 26200)**로 고정한다. 첫 배포의 지원 목표는
25H2·26H2의 일반 배포판이며 최신 누적 업데이트를 전제로 한다. Microsoft의
[버전 정보](https://learn.microsoft.com/en-us/windows/release-health/windows11-release-information)에 따르면
25H2는 build 26200, 26H2는 build 26300이다. 2026-10-17 기준 24H2 Home·Pro는 업데이트가 종료됐다.
24H2와 그 이전 버전, LTSC·IoT 특수 배포판, Insider 및 26H1 전용 신규 장치 계열은 첫 지원 범위에 포함하지 않는다.
이 최소 버전은 제품 지원 정책이며 필요한 Win32 API가 25H2에서 처음 생겼다는 뜻은 아니다.
향후 Windows feature update는 자동으로 지원 목록에 추가하지 않고 제품 검증 근거와 함께 목록을 갱신한다.

지원 목표와 실제 검증 조합을 구분한다. ARM64 Actions는 현재 25H2 build 26200,
과거 x64 로컬 primitive는 26H2 build 26300이며 x64 Actions는 Server 2025다. 실제 PC 제품 검증은 수행하지 않는다.
Windows Server는 자동 회귀 검증 환경으로만 사용하고 별도 제품 지원 대상으로 광고하지 않는다.
CPU × Windows 버전의 모든 조합을 직접 실행했다는 보장은 하지 않는다. Windows 11 x64의
25H2 호환성은 Server x64·Windows 11 ARM64 Actions에 근거한 호환성 목표이며
25H2 x64 직접 검증으로 표시하지 않는다. 이 경계를 release 지원 표에도 그대로 공개한다.
Windows 10은 실행을 일부러 차단하는 기능을 추가하지 않되 공식 지원·검증 범위에서 제외한다.
GitHub-hosted Windows 10 runner가 없으므로 Windows 10 PC·VM 및 self-hosted runner는 마련하지 않는다.

Windows 10·WSL1·32비트 Windows·Windows 서비스 등록·로그인 시 자동 실행·다른 사용자로
실행하는 UAC·SMB/UNC/OneDrive의 활성 private storage는 범위 밖이다. WSL 네트워크 모드나
방화벽·portproxy·시스템 PATH를 자동 변경하지 않는다. Windows와 WSL의 데이터·daemon·CA는
각각 독립 운영하며 기존 `profile export/import`로 이동한다. 양쪽에서 같은 저장소를 직접 열지 않는다.

첫 배포는 변수·시크릿·task/workstream·profile·backup/restore·cleanup·foreground run·
process/project·readiness·port·dashboard·HTTP/HTTPS proxy·hosts/trust·completion·설치/업데이트를
포함한다. 외부 provider는 설치된 호환 도구를 사용하는 기존 optional 기능으로 유지한다.
실제 계정 접속과 LAN peer 검증은 기존 프로젝트처럼 별도 근거이며 로컬 fake 테스트와 구별한다.

## 3. 현재 차단 지점과 구현 경계

| 영역 | 현재 코드와 문제 | 고정할 경계 |
| --- | --- | --- |
| 경로·identity | `internal/paths`, `internal/location`: Linux/macOS만 지원, canonical 문자열 비교 | OS별 경로 해석과 실제 파일 identity |
| private I/O | `tasks/store.go`, `profilekey`, `values`, `maintenance`, `profiletransfer`, `backup`, `ports/store.go`, `services/log_store.go`, `tasks/storage_v3.go`, `tasks/wal_v3.go`: mode bits·`O_NOFOLLOW`·directory sync | 작은 `internal/platformfs` package의 private 생성·검사·열기·publish·replace |
| 잠금 | `maintenance/rwlock.go`, `ports/store.go`, `services/log_store.go`: `flock` | 같은 lock 순서와 shared/exclusive 의미, OS별 구현 |
| lease | `services/lease.go`, `services/store.go`: fd 3 상속 | 별도 launch lease API. 일반 RW lock과 분리 |
| 실행 | `process`, `services/supervisor.go`, `expose/cloudflared.go`, `cmd/devtools/main.go` | OS별 process tree, 신호·console 처리 |
| daemon | `dashboard/server.go`, `proxy/lifecycle.go`: `Setsid` | 동일 실행 파일의 detached supervisor 실행 |
| 비밀번호 입력 | `cli/passphrase.go`, `terminal_linux.go`, `terminal_darwin.go`: termios·poll | OS별 취소 가능한 무반향 console 입력 |
| OS 네트워크 | `hosts/system.go`, `truststore`, `privilege/helper.go`, `netsetup`, `ports/network*` | Windows hosts·CurrentUser trust·권한·Winsock |
| 배포 | `cli/update.go`, `scripts/install.sh`, `scripts/package.sh`, `scripts/project.sh`, `devtools.toml`, release/Pages workflow | Windows install/update와 공통 package/project runner |
| 검증 | Go 테스트의 shell fixture, `verify`, `perf`, Node release/project/browser tests | 공통 fixture와 OS별 실제 경계 검증 |

기존 package가 보유한 데이터·retry·상태전이 로직은 유지한다. OS 차이만 `_windows.go`와
기존 Unix 파일로 분리한다. 범용 OS plugin registry·새 DB·상시 관리 서비스를 추가하지 않는다.
`internal/platformfs`는 실제 중복인 private I/O를 모으며, 프로세스·네트워크 정책까지 소유하지 않는다.
W1에서 CLI 전체의 Windows compile 경계도 먼저 만든다. 아직 구현하지 않은 상위 OS 동작은
`unsupported_platform`(exit 3)로 명시적으로 실패하도록 연결하고 W4/W5에서 실제 backend로 교체한다.
이 중간 상태는 첫 배포 완료가 아니며 W8 이전에 지원 범위의 임시 오류가 모두 제거돼야 한다.

기존 [correctness/storage 계획](correctness-recovery-storage-plan.md)과
[process lifecycle 계획](process-lifecycle-recovery-plan.md)의 no duplicate start, request replay,
private data, 원자적 publish, corruption detection 계약을 보존한다. 이전 계획의 “Windows native는
비목표”는 해당 과거 작업의 범위였으며 이번 지원 추가를 금지하는 전역 제약으로 해석하지 않는다.

## 4. 저장소·경로 계약

### 4.1 경로와 보안

- 기본 Windows root: Known Folder API로 얻은 `LocalAppData` 아래 `devtools`.
  config/data/cache/runtime은 각각 `root/config`, `root/data`, `root/cache`, `root/runtime`.
  roaming `AppData`와 Linux XDG 값을 Windows 기본 경로에 사용하지 않는다.
- 기본 설치 경로: `LocalAppData/Programs/devtools/bin`. 데이터 root와 분리한다.
- `DEVTOOLS_ISOLATED_ROOT`와 `--isolated-root`는 유지한다. isolation에서도 ACL 검사는 생략하지 않는다.
- private storage는 로컬 NTFS만 지원한다. 지원 불가 root는 write 전에 기존 `storage_error`/
  `io_error` 계열로 실패하고 filesystem·위치 수정 안내를 제공한다. 사용자 디렉터리를 자동 이동하지 않는다.
- private 디렉터리는 생성 시점부터 protected DACL: 현재 사용자와 SYSTEM만 full access.
  디렉터리 ACE는 child file/directory에 상속한다. 기존 root를 발견하면 owner/DACL/파일 종류를
  검사하며 permissive root를 자동으로 재권한 설정하지 않는다. 관리자·SYSTEM을 보안 경계 밖 공격자로 간주하지 않는다.
- `0600/0700` 검사에 Windows mode bits를 대입하지 않는다. 모든 private I/O 호출자가 공통
  보안 검사를 사용해야 한다. CA key·backup identity·execution context·인증 파일·temp도 포함한다.
- private root 아래 junction/symlink/reparse point를 거부한다. `FILE_FLAG_OPEN_REPARSE_POINT`는
  최종 요소만 제어하므로 이것만으로 부모 경로 방어가 완료됐다고 간주하지 않는다. 검증된 부모
  디렉터리 handle을 rename 불가로 유지하고 생성·열기 뒤 identity와 실제 경로 containment를 재검증한다.
- project 경로는 private root와 다르다. WSL project는 `/home`과 `/mnt/c`를 허용하고 private data는
  Linux filesystem에 둔다. 네이티브 첫 배포의 managed project 경로는 로컬 NTFS이며 UNC project는
  실행 전에 거부한다. cmd가 UNC cwd를 Windows 디렉터리로 바꾸는 동작을 허용하지 않는다.
- Windows 경로 전체를 소문자로 바꾸지 않는다. 기존 파일은 volume/file identity로 비교하고,
  없는 suffix는 기존 부모 identity와 directory case-sensitivity를 반영한다. 기존 instance ID는
  재발급하지 않는다. drive-relative `C:foo`, device namespace, ADS 경로는 설정·저장 root에서 거부한다.

### 4.2 lock·publish·복구

- 일반 shared/exclusive lock: 별도 lock 파일의 offset 0, length 1에 `LockFileEx`.
  `FAIL_IMMEDIATELY`로 시도하고 기존 10ms 취소 가능한 대기와 global→profile→resource 순서를 유지한다.
  lock 위반만 경합으로 처리하고 접근 거부·잘못된 handle을 busy로 숨기지 않는다.
- process launch lease: `CreateFileW`의 read/write + share mode 0으로 연 파일 handle을 사용한다.
  parent가 연 뒤 supervisor만 allowlist로 상속하며 parent close 후에도 supervisor reference가
  배타성을 유지한다. supervisor는 상속 허용 비트를 즉시 해제한다. 앱 자식에는 lease를 넘기지 않는다.
- lease 확인은 같은 exclusive open으로 수행한다. `ERROR_SHARING_VIOLATION`은 held,
  획득 성공은 free, 그 밖은 storage failure다. metadata-only handle과 file identity로
  inherited lease를 검증한다. **`LockFileEx` 잠금 자체를 Unix `flock`처럼 상속한다고 가정하지 않는다.**
- Windows launch marker는 version 2, `lease_handoff: "inherited-handle"`로 구별한다.
  Unix v1 `inherited-fd`는 그대로 유지한다. Windows runtime record를 profile transfer의 active state로 옮기지 않는다.
  **Windows lease 위치는 `data/process-leases/EXECUTION_ID.lock`으로 고정**한다. 숨긴 staging
  디렉터리 안에 열린 lease를 두면 NTFS에서 execution directory rename이 실패하므로, UUID directory를
  publish하기 전에 외부 고정 lease를 잡고 supervisor까지 유지한다. marker에서 임의 lease 경로를 받지 않는다.
  기존 Unix의 per-execution `lease.lock` 위치는 변경하지 않는다. Windows cleanup/archive는 고정 lease
  파일을 이동·삭제하지 않으며 kernel handle이 닫히면 empty lease 파일이 남아도 held로 판단하지 않는다.
  publish 전 crash로 남은 empty lease도 active execution으로 열거하지 않는다. lease garbage collection은
  첫 배포에 추가하지 않는다. 기존 profile/task 중심 backup/export에는 runtime lease를 포함하지 않는다.
- 파일 쓰기는 같은 디렉터리의 완성된 temp → file flush → close → publish 순서다.
  신규 publish는 `MoveFileExW`에 replace 플래그를 주지 않아 기존 대상을 보존한다.
  교체는 `REPLACE_EXISTING | WRITE_THROUGH`를 사용한다. open reader를 publish 전에 닫는다.
- sharing violation/access denied는 ACL·파일 identity를 재확인한 뒤에만 최대 2초간 10/20/40/80/160/200ms
  간격으로 취소 가능 재시도한다. deadline 뒤 실패 시 기존 파일과 recovery artifact를 보존한다.
- directory `Sync()` 실패를 무시하는 no-op port를 만들지 않는다. Windows는 file flush와 journal의
  before-image/phase 전이를 사용하고 각 durable 전이에서 process kill 복구를 검증한다.
  전원 손실·디스크 cache까지 검증했다는 주장은 하지 않는다. Windows에서 Unix와 같은 directory fsync를
  제공한다는 주장도 하지 않는다. read/write 성공·부분 실패·복구 결과를 기존 error 계약으로 구분한다.
- task-v3/WAL·profile-key-v1·port registry-v4의 logical format은 유지한다. filesystem 이식을
  이유로 새 데이터 format이나 in-place downgrade 지원을 추가하지 않는다.

## 5. 명령·console·프로세스 계약

### 5.1 실행 파일과 인자

- Windows 환경변수 이름은 대소문자 구분 없이 merge한다. injected 값이 parent보다 우선한다.
  같은 injection layer의 `Path`/`PATH`처럼 충돌하는 두 key는 `invalid_argument`로 거부한다.
  Unix 이름·merge 동작은 유지한다. 값 원문을 오류에 넣지 않는다.
- executable lookup은 선택된 child 환경의 PATH/PATHEXT와 child cwd를 사용한다.
  기본 PATHEXT는 `.COM;.EXE;.BAT;.CMD`, 명시한 확장자는 그대로 찾는다. executable mode bit는 검사하지 않는다.
  implicit cwd search는 하지 않고 `./tool.exe`처럼 명시한 상대 경로만 cwd에서 찾는다.
  Windows의 `/`와 `\`를 모두 경로 구분자로 처리한다. 확장자 없는 명시적 상대/절대 경로에도
  PATHEXT를 적용하고, drive-relative 경로는 거부한다. import된 값에 case-insensitive key 충돌이
  있으면 저장 format을 바꾸지 않고 실행 preflight와 doctor에서 key 이름만 진단한다.
- `.exe/.com`은 직접 실행하며 Go/CRT convention의 argv를 보존한다. shell 문자열로 합치지 않는다.
- `.cmd/.bat`은 절대 시스템 `cmd.exe`를 이용하는 전용 adapter로 실행한다.
  `/d /e:on /v:off /s /c`, script와 각 인자의 강제 quoting, trailing backslash doubling을 사용한다.
  **첫 배포의 batch literal domain에서 NUL·CR/LF·double quote·percent가 든 인자나 script 경로는
  실행 전에 `invalid_argument`로 거부한다.** 제한을 조용한 shell expansion으로 대체하지 않는다.
  해당 입력이 필요한 도구는 `.exe` 또는 `node.exe tool.cjs` 직접 실행을 사용한다.
  batch 내부의 `call`/`%*` 재해석까지 임의 script에 대해 보장하지 않는다. 실제 npm/pnpm shim 검증을 추가한다.
- `.ps1`은 자동 실행하지 않는다. 사용자가 `powershell.exe/pwsh -File`을 명시한다.
  shell built-in도 `cmd.exe /c` 또는 PowerShell을 명시한다. config의 `exec` 배열 형식은 유지한다.
- CLI JSON stdin/stdout은 UTF-8, BOM 없이 출력한다. PowerShell 5.1의 native pipeline encoding과
  legacy argv 처리에서 CLI 도달 전에 사라진 인자·문자는 복원할 수 없다. 문서는 PS7을 기본 예제로 쓰고,
  PS5.1 JSON pipeline에는 `$OutputEncoding = [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)`를
  해당 세션에서 설정한다. arbitrary JSON·secret은 가능하면 기존 `--file`/stdin 입력을 사용한다.
  CLI가 받은 argv를 child에 보존하는 검증과 호출 shell 자체의 parsing을 구분한다.

### 5.2 foreground·managed lifetime

- foreground는 기존 console과 stdin/stdout/stderr를 전달한다. ConPTY를 새로 감싸지 않는다.
  Windows에서 Unix `fg/bg`, SIGTSTP job control을 흉내 내지 않는다.
- tree 소유권은 Windows Job Object. kill-on-last-handle-close를 지정하고 breakaway를 허용하지 않는다.
  `PROC_THREAD_ATTRIBUTE_JOB_LIST`로 **프로세스 생성 시점**에 연결한다. start 후 assign하는 race를 두지 않는다.
  Go 1.27 `exec.Cmd`의 `SysProcAttr`에는 이 attribute 주입 지점이 없으므로 기존 `x/sys/windows`의
  `CreateProcessW`/attribute list를 사용하는 Windows launcher를 `internal/process` 안에 둔다.
  추가 dependency나 cgo는 필요하지 않다. stdio pipe 복사·wait·exit code도 이 경계가 담당한다.
- foreground Ctrl+C는 wrapper가 받아 해당 child group에 CTRL_BREAK로 전달한다. 정상 종료를 3초 기다리고
  그 뒤 job 전체를 종료한다. normal exit는 child code를 보존하고 wrapper 취소는 130, 강제 종료는 137.
  SIGTERM/SIGHUP 등록과 POSIX wait status 해석은 Unix 구현에 남긴다.
- managed supervisor는 숨긴 전용 console에서 실행하며 CLI 종료와 분리한다. child는 그 console에서
  별도 process group과 supervisor 소유 job에 속한다. stop은 기존 인증된 control RPC → CTRL_BREAK →
  3초 뒤 job 종료 순서다. supervisor crash는 job handle close로 child tree를 정리한다.
- supervisor launch의 lease 상속과 앱 job 연결을 분리한다. launcher의 임시 parent job에 supervisor를
  넣어 foreground CLI 종료 시 함께 죽게 만들지 않는다. CI host의 상위 job을 탈출하도록 권한을 높이지 않는다.
- RPC token·lease·launch generation이 소유권 근거다. 디스크에 기록된 PID를 믿고 `taskkill /T /PID`하지 않는다.
  현재 `starting/running/stopped/interrupted` 상태와 동일 request UUID의 재실행 금지 계약을 보존한다.
- `dashboard`·proxy·cloudflared도 같은 spawn/tree 경계를 사용한다. readiness probe는 timeout 시 tree까지 정리한다.
- OS 경계를 넘는 managed tree supervision은 제공하지 않는다. 네이티브 managed `wsl.exe`와
  WSL managed Win32 실행은 preflight에서 명확히 거부한다. foreground interop은 허용하되 cross-OS
  descendant 정리 보장은 안내에서 구분한다. WSL 서버는 WSL CLI로 관리한다.
- Windows passphrase prompt는 console mode 저장 → echo 해제 → 취소 가능한 input event read → 모든 종료 경로에서
  mode 복원이다. 백그라운드의 block된 reader를 남기지 않는다. pipe/file 입력은 기존 방식을 유지한다.

## 6. 네트워크·권한·WSL 경계

- Windows port probe는 Winsock의 exclusive bind 의미를 사용한다. wildcard/specific·IPv4/IPv6·동시 bind와
  excluded port의 WSAEACCES를 테스트한다. 권한/예약 실패를 free port로 판단하지 않는다.
  `proxy setup` Windows mode는 native이며 capability를 변경하지 않는다. 실제 bind 결과를 별도로 판단한다.
- hosts 대상은 Windows directory API로 구한 `System32/drivers/etc/hosts`로 고정한다. 기존 소유 block·
  expected digest·외부 mapping 충돌·last-owner 정리를 유지한다. 파일의 원래 ACL·줄바꿈·외부 내용을 보존한다.
- 일반 CLI·daemon은 비상승 token으로 실행한다. hosts 변경만 constrained helper로 분리한다.
  interactive helper는 `ShellExecuteExW(runas)`로 같은 실행 파일을 호출한다. noninteractive에서 권한이
  없으면 UAC를 띄우지 않고 `permission_required`를 반환한다. 사용자가 준비한 상승 token에서는 helper를 직접 실행한다.
- helper IPC는 현재 사용자/SYSTEM 전용 one-shot named pipe, 최대 1MiB JSON, 120초 deadline.
  생성한 helper의 process handle/PID와 연결 client PID, 양쪽 SID·실행 파일 identity를 검증한다.
  pipe 이름만 argv에 넘기고 credential·본문·secret을 argv나 임시 공개 파일에 넣지 않는다.
  helper는 hosts/trust/netsetup의 allowlist 작업만 받고 임의 경로·명령을 받지 않는다.
  `ShellExecuteExW`는 COM 초기화한 전용 OS thread에서 호출하고 `SEE_MASK_NOCLOSEPROCESS`로
  helper process handle을 확보한다. 취소·timeout 뒤 새 mutation 요청은 보내지 않고 기존 operation
  receipt로 결과를 재확인한다. UAC의 다른 계정 선택으로 SID가 달라지면 helper 요청을 거부한다.
- Windows CA trust 대상은 CurrentUser Root. local-machine store는 변경하지 않는다.
  Windows의 root 인증서 확인 창을 유지한다. trust/untrust는 대화형 터미널과 OS 확인이 필요하며, 비대화형 요청은 helper 실행 전에 `permission_required`를 반환한다. Actions는 해당 테스트 PID·고유 fixture CA subject의 확인 창만 처리한다. 실제 PC에서의 화면·사용자 조작은 미검증으로 공개한다.
  `proxy trust/untrust`는 선택한 public CA DER fingerprint로 확인하며 동일한 외부 인증서를
  원래부터 신뢰했다면 소유 receipt 없이 삭제하지 않는다. CA key는 private storage에만 둔다.
- WSL Linux trust와 Windows browser trust는 별개다. 기존 `proxy ca export --output FILE`을 재사용하고,
  Windows `Import-Certificate -CertStoreLocation Cert:\CurrentUser\Root` 절차와 fingerprint 확인·제거를 문서화한다.
  Linux `trusted=true`를 Windows browser trust 성공으로 표시하지 않는다. 인증서 검사를 끄는 우회는 안내하지 않는다.
- WSL NAT와 mirrored를 별도 검증 행으로 둔다. Windows→WSL IPv4 localhost 성공 여부를 실제로 확인한다.
  IPv6 loopback·역방향·서로 다른 distro·LAN 접근은 IPv4 성공에서 추론하지 않는다. `.localhost`와 custom host도 분리한다.
- 자동 interop path 변환·WSLENV 변경은 하지 않는다. Windows `.exe`는 명시적으로 찾고 선택된 argv를 전달한다.
  WSLENV를 사용자가 선언한 변수만 Windows로 전달한다. PATH가 섞여도 Linux `node/pnpm/git`을 우선 조회하며
  Windows 실행 파일을 Linux tool 부재의 근거로 쓰지 않는다. `appendWindowsPath=false`도 정상 지원한다.
- WSL private data·capability 적용 CLI는 Linux filesystem에 둔다. `/mnt/c` project는 허용하지만 DrvFS mode bits를
  private storage 보장의 근거로 쓰지 않는다. mount 옵션이나 `/etc/wsl.conf`를 자동 수정하지 않는다.
- `diagnostics`에 read-only `platform` 객체를 추가한다: `os`, `arch`, `runtime` (`native|wsl`),
  `storage_filesystem`, `storage_supported`, `interop` (`available|unavailable|not_applicable|unknown`),
  `client_trust` (`local_only|separate_windows_store`), `limitations` (고정 code 배열).
  Win32 executable 존재만으로 interop available을 선언하지 않는다. 기본 diagnostics는 Windows child를 실행하지 않아
  `unknown`을 허용한다. unknown만으로 전체 ready를 false로 만들지 않고 실제 storage failure는 false에 반영한다.
  network mode는 확실한 근거가 없으면 출력하지 않는다. 전체 환경변수·WSL 설정 파일 원문을 수집하지 않는다.

## 7. 설치·업데이트·completion·개발 도구

- Windows release 이름은 `devtools_VERSION_windows_amd64.zip`, `..._arm64.zip`과 각각 `.sha256`.
  ZIP은 regular `devtools.exe` 하나만 포함한다. path traversal·중복 entry·symlink·다른 PE architecture는 거부한다.
  stable 다운로드의 checksum·실제 version/commit 확인을 기존 installer와 동일하게 수행한다.
- `scripts/install.ps1`은 PowerShell 5.1/7 양쪽에서 실행한다. ARM64 OS에서 x64 PowerShell을 실행해도
  **OS native architecture**를 조회해 ARM64 배포물을 선택한다. 프로세스 bitness만으로 결정하지 않는다.
  기존 `install.sh`와 Homebrew 경로는 유지한다. Pages는 같은 release tag의 두 installer를 게시한다.
  installer 입력은 `-Version`, `-Source`, `-BinDir`, `-AddToPath`, `-NoPath`를 제공한다. `-Source`는
  explicit version과 함께만 허용하고 로컬 release directory 또는 HTTPS directory를 받는다.
  Windows install lock은 install directory의 `.devtools-install.lock`을 FileShare.None으로 연 handle이며
  installer와 Go updater가 같은 방식을 사용한다. lock file의 존재만으로 busy라 판단하지 않는다.
- 설치 단순화 요청으로 기존 PATH opt-in 결정을 변경했다. installer는 기본으로 user PATH에 bin 경로를 추가한다.
  `-NoPath`로 끌 수 있으며 기존 `-AddToPath` 입력도 유지한다. `-NoPath`가 우선한다.
  중복 경로를 추가하지 않으며 시스템 PATH·현재 세션·영구 execution policy·shell profile은 바꾸지 않는다.
  과거 W7I의 opt-in 검증 기록은 당시 계약으로 보존한다.
- `dvt.exe`는 installer가 만드는 동일 binary copy다. symlink 권한이나 `.cmd` wrapper를 요구하지 않는다.
  설치 manifest에서 소유 hash/identity를 기록하며 외부 `dvt`가 있으면 기존 `skipped_conflict` 동작을 유지한다.
- Windows updater는 Go 내부 구현으로 실행한다. `/bin/sh`를 호출하지 않는다. Unix installer 기반 update는 유지한다.
  Windows에서 실행 중인 image를 직접 덮어쓰지 않고 같은 volume의 고유 `.old.exe`로 rename 후 verified candidate를 publish한다.
- update 순서: install lock → manifest/identity 확인 → 다운로드·checksum/PE/version 검증 → private journal에
  `prepared` 기록 → devtools/dvt의 old backup 보존 → 각 새 이름 publish → 새 binary smoke → `committed` 기록 →
  응답. 일부 실패 시 기록한 old 파일로 복구한다. 복구 실패는 retained artifact와 `update_failed`를 반환한다.
  installer 재실행은 incomplete journal을 먼저 복구한다. active daemon은 자동 중지하지 않는다. 사용 중인
  old image 삭제가 막히면 다음 설치/업데이트에서 정리한다. journal과 backup 없이 두 executable을 순차 덮어쓰지 않는다.
- CLI가 `dvt.exe`로 실행돼도 install manifest의 main 경로로 update한다. 정체가 다른 파일은 교체하지 않는다.
  package manager 소유 설치는 기존처럼 해당 manager로 갱신하도록 거부한다.
- PowerShell completion은 기존 CLI metadata와 `__complete` 후보를 재사용하고 `Register-ArgumentCompleter -Native`로
  `devtools/dvt`에 등록한다. AST에서 literal token을 추출하며 입력 문자열을 evaluate하지 않는다.
  cmd는 실행 지원만 제공하고 자동완성 engine은 추가하지 않는다. 기존 bash/zsh/fish는 유지한다.
  공개 shell 이름은 `powershell`, 생성물은 raw `.ps1`이다. AST의 literal token만 후보 조회에 쓰고
  dynamic expression은 평가하지 않는다. `__complete` stdin은 UTF-8 NUL-delimited tokens로 전달하며
  completion text의 single quote는 PowerShell 규칙으로 두 번 써서 escape한다.
- `scripts/project.sh`가 묶는 build/check/package 동작은 stdlib-only Python project runner로 이동하고 shell 파일은
  기존 호출 호환 shim으로 남긴다. 개발 검증은 `python3` Unix/`python` Windows를 사용한다.
  사용자 CLI 실행에 Python·Git Bash·Go를 새 prerequisite로 추가하지 않는다.
- Python package runner가 기존 네 tar.gz + 두 Windows ZIP을 생성하고 version/commit/checksum을 검증한다.
  `clean:build`와 wildcard Node test 호출도 shell 의존성을 제거한다. release.mjs의 `/dev/null`·shell fixture를 수정한다.

## 8. 외부 도구 계약

cloudflared와 Tailscale을 자동 설치·로그인하거나 사용자의 기존 설정을 덮어쓰지 않는다.
Windows child 생성·취소는 공통 process 경계를 사용한다. 네이티브 architecture의 devtools와
provider executable architecture는 별도 정보이며 provider의 에뮬레이션 사용을 ARM64 devtools 검증으로 세지 않는다.

Tailscale Windows 지원 기준은 공식 stable `1.104.1` 이상으로 고정하고 기존 Unix 최소 버전은 유지한다.
그보다 오래된 Windows provider는 기존 `tailscale_version_unsupported`로 거부한다.
Tailscale Windows는 `debug local-creds`를 호출하지 않는다. 공식 소스에서 이 명령은 지속 실행하는
LocalAPI proxy를 시작하므로 현재의 one-shot/10초 timeout adapter와 맞지 않는다. Windows는 고정된
`\\.\pipe\ProtectedPrefix\Administrators\Tailscale\tailscaled`에 HTTP transport를 직접 연결한다.
`CreateFileW(FILE_FLAG_OVERLAPPED)` → Go 1.27 `os.NewFile` → deadline을 지원하는 작은 `net.Conn`
adapter를 사용한다. connect는 busy일 때 10ms 재시도로 기존 10초 operation deadline을 지키며,
request 취소는 connection close/read deadline으로 전파한다. Windows protected pipe prefix 밖으로
fallback하지 않는다. 사용자별 개발용 tailscaled pipe는 첫 배포 범위 밖이다.
Unix/macOS의 기존 socket/TCP credential 경로와 ETag/If-Match·응답 제한·redirect 거부는 유지한다.
Microsoft/go-winio 추가는 불필요하다. 기존 Go/x/sys로 필요한 I/O와 deadline을 실제 probe로 검증한다.

공식 cloudflared 다운로드 표는 Windows x86/amd64만 제공하며 Windows ARM64 native 배포물은 없다.
따라서 Windows ARM64의 cloudflared provider는 사용자가 설치한 amd64 도구의 OS 에뮬레이션 실행을
조건으로 지원한다. provider가 없거나 실행 불가면 기존 not-installed/start 오류를 반환한다.
이를 devtools ARM64 자체의 에뮬레이션으로 대체하지 않는다. Tailscale은 공식 ARM64 MSI가 있다.
fake provider로 소유권·충돌·종료를 검증하며 실제 외부 접속은 자격증명이 준비된 환경에서만 별도 수행한다.

## 9. 구현 단위와 의존성

| ID | 결과·관련 경로 | 선행 | 완료 근거 |
| --- | --- | --- | --- |
| W0 | 이 문서·probe·실제 runner 근거 확정 | 없음 | 준비 상태의 사전검증 gate 충족 |
| W1 | Windows compile 경계·경로·ACL·secure open·lock·publish 추가 | W0 | CLI build 및 amd64/arm64 private I/O·경합·교체 fixture 통과 |
| W2 | values/tasks/maintenance/backup/profile/ports/log 저장소 이식 | W1 | task-v3/WAL recovery, concurrent mutation, replay, backup/export/import/cleanup 통과 |
| W3 | lookup·env·argv·batch·console·passphrase 및 foreground run | W0 | 실제 cmd/PS5.1/PS7에서 인자·입출력·취소·종료 code 통과 |
| W4 | lease·process tree·detached process/project·dashboard·HTTP proxy | W2,W3 | terminal exit·parent/supervisor crash·start retry·stop/restart·readiness 통과 |
| W5 | Winsock·hosts/trust·helper·HTTPS·provider adapter | W2,W4 | disposable runner OS trust/hosts 원복·오류·TLS/WS/H2 종료 통과 |
| W6 | WSL interop 진단·Windows client trust/DNS 안내·경계 회귀 | W3,W5 | 실제 가능한 WSL 행과 문서 예제 검증. 미검증 행 명시 |
| W7 | installer/updater·ZIP·dvt·project runner·PowerShell completion | W2,W3 | 두 architecture 설치→업데이트→rollback와 셸 completion 통과 |
| W8 | release matrix·사용자 문서·Agent Skill·공개 schema 통합 | W4,W5,W6,W7 | 아래 최종 Actions 통합 gate·실제 PC 미검증 한계 공개, release publish=false artifact 검증 |

W1과 W3는 독립 진행 가능하다. W7의 package runner는 W4와 병행 가능하다.
W3의 독립 검증은 process package/fixture 기준이며 전체 CLI 통합에는 W1/W2의 결과를 함께 사용한다.
W2/W4가 완성되기 전 partial build를 Windows 지원 릴리스로 게시하지 않는다.
공개 JSON envelope와 기존 TOML 형식은 유지한다. 새 diagnostics 필드·completion shell은 CLI schema,
문서·Agent Skill·contract tests에 함께 반영한다. machine-facing diagnostics 구조와 completion 입력이
확장되므로 이 기준 커밋의 protocol 6을 7로 올리고 envelope schema 1은 유지한다.
착수 전 다른 작업이 protocol을 바꿨다면 그 최신 계약과 충돌을 정리해 이 계획의 기준을 갱신한다.

## 10. 검증 설계

### 구현 전 근거

standalone probe는 제품 기능이 아닌 OS primitive의 적합성만 검증한다. x64·ARM64 각각 native 실행을 요구한다.
재현 코드·로컬 JSON·빌드 실패 log·Actions run/artifact는 [조사 기록](windows-support/research.md)에 연결한다.
파일 보안은 현재 probe의 DACL 관찰과 별개로 다른 사용자 access-denied 검증이 필요하다.

### 구현 후 필수 gate

| 영역 | 반드시 관찰할 실패·성공 | 환경 |
| --- | --- | --- |
| storage | insecure DACL·reparse·동일 이름 충돌 거부, shared/exclusive 경쟁·취소, old/new snapshot 보존 | 두 Windows runner |
| recovery | 각 WAL/maintenance/update 전이에서 kill 후 retry, stale digest/UUID conflict, 손상 fail-closed | 두 Windows runner |
| argv | 공백·빈 인자·한글·trailing slash·metachar·선택 PATH, batch 제한 preflight, 실제 npm/pnpm shim | 두 Windows runner, PS5.1/7/cmd |
| lifetime | CLI 종료 후 managed 유지, supervisor crash 후 자손 정리, no duplicate start, 정상/강제 stop, pipe EOF | 두 Windows runner |
| secret input | echo 비활성·취소/EOF 뒤 mode 복원·비대화형 전달·argv/log 비노출 | Windows console fixture + pipe |
| network | IPv4/v6·wildcard/specific 충돌, CA·hosts 소유권/권한 거부/복구, TLS·WebSocket·HTTP/2 | 두 disposable Windows runner |
| installed | ZIP/PE/checksum, 새 사용자 설치·dvt 충돌·PATH 옵션·업데이트 중단/원복 | 두 Windows runner |
| dashboard | 실제 packaged CLI 로그인→값 수정→reload→CLI 확인, session/Origin 보존 | runner에서 사용 가능한 실제 Chromium |
| WSL | Linux data·`/home`/`/mnt/c` project 경로·WSLENV opt-in·DNS/별도 trust 안내 fixture, mode별 진단 | Linux CI·진단 fixture. 실제 WSL2/Win browser 연계는 NOT RUN 공개 |
| regression | 기존 Go·Node·installed suite·skill checks·macOS/Linux matrix | 기존 환경 |

기존 shell fixture를 Go fixture executable로 교체한다. Unix TTY/job-control 테스트는 build tag로 유지하고,
Windows 행을 통째로 skip해 전체 성공으로 만들지 않는다. `go test -race`는 지원되는 Windows amd64에서만
호환 C toolchain을 준비해 실행한다. Windows ARM64에는 일반 테스트·process contention 검증을 실행하며
race detector를 실행했다고 표시하지 않는다. Playwright native browser 부재도 실제 gap으로 보고한다.

WSL NAT↔mirrored 전환은 현재 사용 중인 distro를 강제 shutdown하면서 수행하지 않는다.
전환 가능한 disposable 환경이 없으면 그 행은 미검증이다. GHA의 `wsl.exe` 존재나 WSL1 설치를
WSL2 nested virtualization·실제 networking 검증으로 대신하지 않는다.

### Actions 검증과 실제 PC 미검증 한계

2026-10-17 사용자 결정: **“Actions 검증으로 확정하고 실제 PC 미검증 한계 공개”**.
이 결정은 이전의 필수 로컬 제품 gate를 대체한다. 로컬 Windows 실행은 중단한다.
제품 완료의 실행 근거는 동일 후보 artifact의 x64·ARM64 Actions 및 기존 Unix 검증이다.
W8L은 실제 PC의 성공 기록을 만드는 대신 자동 검증 범위와 아래 미검증 항목을 대조하고
최종 사용자 문서·지원 표·후보 인계에 한계를 공개하는 작업으로 변경한다.

아래 표는 향후 사람이 추가 확인할 때 쓸 절차다. 현재 필수 실행 gate가 아니며
미실행 행을 PASS로 표시하지 않는다. Actions에서 실행한 API·console fixture·browser 검사는
해당 runner와 테스트 범위의 증거이며 실제 PC 관찰을 대신하지 않는다.

| 추가 확인 항목 | 향후 수동 확인 절차 | 확인할 결과 |
| --- | --- | --- |
| 설치·private data | 새 임시 설치 경로에 ZIP/installer 설치, `dvt`와 CLI 실행, 값·시크릿·profile round trip | 관리자 권한 없이 사용자 경로에 설치·저장, owner/SYSTEM private ACL, 기존 사용자 데이터 보존 |
| 실제 셸·console | PS5.1/7/cmd 각각 공백·한글 경로, 빈 인자·metachar 및 batch 허용/거부 사례 실행; 비밀번호 입력 취소 | 문서의 argv 계약과 exit code 일치, 입력 echo·시크릿 log 없음, 취소 후 console mode 복원 |
| process·daemon | managed process/project 시작 후 터미널 닫기, 새 터미널에서 조회·stop; supervisor 강제 종료 후 재시작 | 정상 managed 유지, crash 자손 정리, 중복 시작 없음, port·lease 회수 |
| UAC·hosts | 기존 hosts 백업 후 전용 테스트 block 추가 요청에서 UAC 취소, 이어 같은 사용자 승인·제거 | 취소 시 변경 없음, 승인 시 지정 block만 변경, 종료 시 기존 내용 복구 |
| trust·browser | 임시 CA를 CurrentUser에 추가해 browser에서 로컬 HTTPS와 dashboard 사용 후 제거 | 인증서 경고 없이 접속·값 수정 반영, 다른 인증서 보존, 테스트 fingerprint만 제거 |
| update·rollback | packaged 이전/후보 artifact로 실행 중 업데이트, 기존 failure fixture로 중단·retry·rollback | 새 실행 파일·`dvt` 정상 실행, 실패 후 이전 실행 파일 복구, 데이터 보존·임시 backup/journal 수렴 |
| WSL2 경계 | `/home`·`/mnt/c` 프로젝트, 공개 WSLENV canary opt-in, Windows browser→WSL IPv4·별도 trust 안내 확인 | Linux private data 유지, opt-in 변수만 전달, localhost 응답, Windows/WSL daemon·CA 독립; NAT 결과로 기록 |
| 원복 | 테스트 process·설치·hosts block·CA·생성 파일 정리 후 비교 | 사용자 기존 hosts·trust·데이터 보존, 테스트 process/port 누수 없음 |

실제 PC의 UAC 대화상자 승인·취소, 실제 browser의 CurrentUser trust 통합,
사용자 터미널을 닫은 뒤 daemon 유지, 실제 PC installer/update/rollback,
새 제품과 실제 WSL2 NAT·mirrored/Windows browser 연계는 **NOT RUN**으로 공개한다.
과거 standalone primitive·WSL 보조 검증은 그 당시의 코드·범위에만 적용되며
최종 후보의 제품 동작으로 확대하지 않는다. Windows 11 x64 모든 버전 조합,
ARM64 desktop UAC·browser 및 ARM64 WSL2도 직접 검증했다고 주장하지 않는다.

자동화 가능한 설치·rollback·프로세스 tree·별도 사용자 ACL·API·console·browser 하위 검사는
동일 후보의 disposable Actions에서 실행한다. Windows ARM64 race detector나
실제 provider 계정 접속을 일반 suite의 성공으로 포함하지 않는다.

W8L 근거는 `docs/windows-support/evidence/product/<full-commit>/validation-limits/`에
후보 commit·ZIP SHA-256·각 runner의 실제 OS/CPU·자동 검사 결과와 NOT RUN 목록으로 남긴다.
다른 artifact 결과를 합쳐 단일 후보가 통과했다고 표시하지 않는다.
이 범위 변경은 main 병합·태그·Release 게시를 승인하지 않는다.

기존 macOS/Linux gate를 제거하지 않는다. artifact가 생성돼도 어느 필수 gate가 실패하면 태그·Release를
게시하지 않는다. 기존 release의 `publish=false`로 installer·ZIP·metadata·Pages 파일을 먼저 확인한다.

Windows 최초 설치는 기존 Linux/WSL data를 찾거나 자동 migration하지 않는다. cross-OS profile import는
기존 preview/digest/apply 흐름을 유지한다. task claim·port assignment·process·CA key의 active ownership은
이동하지 않는다. Windows filesystem 변경 때문에 기존 저장 format의 downgrade 경계를 완화하지 않는다.

실패 시 복구 대상은 installer journal, backup file, 선택한 hosts block, 선택한 trust fingerprint로 제한한다.
사용자 환경 전체 rollback·다른 CA 삭제·다른 서버 종료·전역 방화벽 해제는 수행하지 않는다.

## 12. 구현 착수 판정

설계와 OS primitive 검증은 별도 상태로 기록한다. 진행 상황은 다음과 같다.

- [x] Windows 11 25H2·26H2 x64/ARM64·WSL2 지원 목표, 최소 버전 25H2, Windows 10·self-hosted 운영 제외 결정.
- [x] Actions 제품 검증으로 확정. 실제 PC 미실행·Server/client 차이·WSL 연계 한계를 공개하고 필수 자동 검사 실패 시 게시 금지.
- [x] 현재 소스·기존 계약·이식 대상 조사.
- [x] 로컬 Windows x64 비상승 token에서 standalone primitive 검증.
- [x] 실제 WSL2에서 기존 핵심 Go 테스트와 Windows interop/IPv4 loopback 보조 검증.
- [x] 최종 publication probe를 포함한 GitHub Actions x64·ARM64 실행과 artifact 검토.
- [x] Actions 결과와 provider 근거를 반영한 최종 조사 기록 확정.

판정: **위 지원 범위의 구현 계획과 primitive 사전검증 gate 충족**.
GitHub Actions [최종 실행](https://github.com/jinyongp/devtools/actions/runs/38994030329)의
두 architecture에서 16개 probe 기록을 확인했다. 근거는 저장소에도 보관했다.
구현 후 제품 검증 gate는 별도이며 사전검증이 대체하지 않는다. 실제 UAC 대화상자·다른 사용자 ACL·
전원 손실·WSL NAT/mirrored 전체 조합·외부 provider 계정 접속은 현재 primitive probe의 보장 범위가 아니다.
실제 x64 UAC·browser·새 제품의 실제 WSL2 NAT는 필수 로컬 gate에서 제외하고 위 미검증 한계에 공개한다.
다른 미검증 조합은 지원 근거로 확대하지 않는다. 현재 남은 제품 검증은 실행할 완료조건이며
지원 버전·검증 방식에 관한 미결 정책 선택은 없다. 구현이 계약을 반증하면 목적과 상태에 정한 재조사 절차를 따른다.
