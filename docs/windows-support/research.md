# Windows / WSL 사전 조사와 검증 근거

조사일: 2026-10-17. 적용 계획: [Windows 지원 계획](../windows-support-plan.md).
이 기록은 소스 조사, OS primitive 실행, 구현 후 필요한 제품 검증을 구분한다.

## 1. 조사 기준

제품 소스 기준은 `28601633c67483e187084ce65527ebac1b8716f3`다. 최초 로컬 빌드·Go 테스트는
바로 전 `eb57d73c6d32d61b2058d52c4d8ca2922c8c2995`에서 실행했다. 그 사이 변경은
`verify/scenarios/proxies.py`의 packaged registry migration 검증뿐이며 이번 로컬 Go 검사 대상과
이식 대상 코드는 동일하다. GitHub Actions는 아래 별도 probe commit을 사용한다.

- Go `1.27.1`, `golang.org/x/sys v0.47.0`, `x/term v0.45.0` 확인.
- README·install·network/storage/process 설계·CLI schema·release/Pages workflow·Go/Node/Python
  테스트 기반을 조사했다. runtime 소스의 syscall, Unix import, OS 경로, 실행·publish 호출을 검색했다.
- 전체 dependency tree를 Windows로 빌드할 때 첫 두 package의 오류가 downstream 컴파일을 차단한다.
  아래 빌드 log를 모든 downstream 오류의 완전한 목록으로 해석하지 않는다. 계획의 영역별 소스 조사가 이를 보완한다.
- 현재 native Windows 제품 빌드는 amd64/arm64 모두 실패한다. probe 통과는 제품 지원 통과가 아니다.

## 2. 실제 실행한 로컬 검증

| 검사 | 결과 | 근거 |
| --- | --- | --- |
| Windows amd64 product cross-build | FAIL, 예상된 미지원 상태 | `maintenance`의 O_NOFOLLOW/flock, `process`의 Setpgid/Kill/TTY 함수 |
| Windows arm64 product cross-build, compiler `-e` | FAIL, 동일 선행 차단 | [log](evidence/windows-arm64-build.log) |
| WSL 핵심 Go package 테스트 | PASS, 6개 package | [log](evidence/wsl-baseline.log) |
| Windows amd64 native API probe | PASS, 16개 기록 | [JSON](evidence/local-windows-amd64.json), [코드](probes/native_windows.go) |
| WSL→Windows 변수 전달 | WSLENV 없으면 absent, 명시한 canary만 present | [JSON](evidence/local-wsl.json), [코드](probes/wsl.py) |
| Windows→WSL IPv4 localhost | PASS | 같은 WSL JSON. 임시 HTTP server의 고정 body를 Windows PowerShell에서 확인 |
| 현재 WSL networking mode | NAT | [원문](evidence/wsl-networking-mode.txt). mode 전환 없음 |

로컬 Windows: build `26300.9711`, display version `26H2`, amd64, PowerShell
`5.1.26100.9711`, native probe token은 비상승. `pwsh`는 설치되지 않았다.
WSL kernel은 `6.18.33.2-microsoft-standard-WSL2`. 이 호스트를 Windows 10 검증으로 세지 않는다.
WSL 설정의 `appendWindowsPath=false`를 유지한 상태에서 absolute Win32 executable로 interop을 확인했다.
일반 sandbox의 interop은 소켓 권한 때문에 실패했으며 승인된 host 실행에서 확인했다.
이 sandbox 실패를 일반 WSL 기능 결함으로 분류하지 않는다.

Go baseline 명령:

```sh
go test ./internal/paths ./internal/location ./internal/process \
  ./internal/maintenance ./internal/ports ./internal/services
```

native probe 재현은 repository root에서 해당 파일을 명시적으로 빌드한다. `//go:build ignore`라서
일반 `go test ./...`나 제품 build graph에는 편입되지 않는다. Windows 임시 NTFS 디렉터리만 만들고
프로세스·handle·파일을 정리한다. 실제 hosts·trust·firewall·WSL 설정은 바꾸지 않는다.

```powershell
$env:CGO_ENABLED = '0'
go build -o "$env:TEMP\devtools-native-probe.exe" docs/windows-support/probes/native_windows.go
& "$env:TEMP\devtools-native-probe.exe"
```

각 JSON의 `pass`는 해당 probe의 명시적 assertion에만 해당한다. `batch_direct_observation`은
직접 `.cmd` 호출 결과를 기록하는 관찰 행이며 정상 literal argv 보장의 근거는
`batch_conservative_argv`와 `exe_argv_roundtrip`이다.

## 3. GitHub Actions 검증

워크플로: [windows-preflight.yml](../../.github/workflows/windows-preflight.yml).
사용자가 승인한 `codex/windows-support-preflight` 브랜치에서만 push로 실행한다.
main·release를 변경하거나 외부 계정에 로그인하지 않는다.

| 실행 | commit | 결과·해석 |
| --- | --- | --- |
| [첫 실행](https://github.com/jinyongp/devtools/actions/runs/38993132058) | `8de8975841151103800f70c71c44b68c7b607c98` | 두 runner의 ACL assertion 실패. SDDL 문자열이 현재 사용자 SID를 alias로 표기할 수 있는데 문자열 검색으로 판단한 probe 결함 |
| [수정 실행](https://github.com/jinyongp/devtools/actions/runs/38993505296) | `602eb51a968baff35cfe94d8085af8b3704efc54` | PASS, 두 architecture에서 각각 15개 probe 기록 확인. SID 객체·ACE trustee 비교와 overlapped named pipe 검증 포함 |
| [최종 실행](https://github.com/jinyongp/devtools/actions/runs/38994030329) | `3bd08c78da3410e2ead01b53c72270daa6bc1256` | PASS, 두 architecture에서 각각 16개 probe 기록 확인. 열린 lease와 staging directory publication 조합 추가 |

최종 실행 runner metadata로 실제 네이티브 실행 환경을 확인했다.

| label | architecture | 실제 image | PS7 / PS5.1 | token |
| --- | --- | --- | --- | --- |
| `windows-2025` | amd64 | `win25-vs2026`, `20261007.264.1`, build 26100 | 7.6.6 / 5.1.26100.33438 | elevated |
| `windows-11-arm` | arm64 | `win11-vs2026-arm64`, `20261013.183.4`, build 26200 | 7.6.6 / 5.1.26100.9457 | elevated |

runner가 elevated인 점은 제한이다. 로컬 비상승 token의 primitive 근거를 함께 보관하되,
두 CPU의 별도 표준 사용자·실제 다른 사용자 access-denied·interactive UAC 검증으로 확대 해석하지 않는다.
artifact는 `runner.json`, `native.json`, 예상 실패한 `product-build.log`/status, `wsl-capability.json`이다.
제품 빌드 실패를 명시적으로 수집하는 단계가 있으므로 workflow green을 제품 build green으로 요약하지 않는다.
최종 artifact 본문은 [amd64](evidence/actions-38994030329/windows-preflight-amd64/runner.json)와
[arm64](evidence/actions-38994030329/windows-preflight-arm64/runner.json) 디렉터리에 보관했다.
이전 15개 probe 실행의 artifact도 별도 디렉터리에 유지한다.
Git 텍스트 정책에 따라 파일 줄바꿈은 LF로 정규화될 수 있으며 artifact ZIP의 byte 단위 복제본은 아니다.
source commit·probe/workflow checksum은 [manifest](evidence/manifest.json)에 기록했다.
이후 문서·근거 파일만 추가한 커밋은 같은 probe 코드의 실행 근거를 재사용한다.

WSL capability 수집에서는 x64에 WSL 3.0.1.0이 설치돼 있었고 ARM64의 `wsl.exe --version`은
미설치 안내와 exit 1을 반환했다. 양쪽 모두 `wsl.exe` 파일은 존재했다. 따라서 executable 존재만으로
interop·WSL2 설치나 nested VM 실행 가능을 판정하지 않는다. `versionOutput` 원문은 PowerShell의
native UTF-16 decoding 영향으로 NUL 문자를 포함하며, 표시할 때만 NUL을 제거해 읽었다. 원문은 보존했다.

## 4. 검증이 바꾼 설계 결정

1. **일반 lock과 launch lease를 분리한다.** shared/exclusive 경쟁은 LockFileEx로 검증했지만
   Windows 문서는 inherited handle에 byte-range lock 접근이 그대로 넘어가지 않는다고 명시한다.
   별도 share-mode-0 open handle의 상속·parent close 후 배타성·child 종료 후 해제를 실제 확인했다.
   [LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex),
   [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew).
   추가 경계 재현에서는 staging 내부의 열린 lease가 directory rename을 `Access is denied`로 막았다.
   `FILE_SHARE_DELETE`만 추가해도 해당 재현은 실패했다. lease를 staging 밖 고정 경로로 옮긴 조합은
   열린 lease 배타성을 유지한 채 directory publication에 성공했다. 따라서 Windows v2 launch는
   `data/process-leases/EXECUTION_ID.lock`을 사용한다. Unix의 staging/fd 인계를 그대로 복사하지 않는다.
2. **process 생성 때 Job에 연결한다.** start 후 assign race를 피하는 attribute와
   kill-on-close를 실제 검증했다. 단일 child 검증이며 모든 descendant·console fault matrix를 대신하지 않는다.
   [Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects),
   [UpdateProcThreadAttribute](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute).
3. **Windows directory Sync를 성공으로 위장하지 않는다.** directory Sync가 실패함을 확인했다.
   파일 flush·journal recovery로 구현하며 전원 손실 내구성을 실험으로 검증했다고 주장하지 않는다.
   [FlushFileBuffers](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers).
4. **교체와 업데이트를 분리한다.** live reader가 share-delete를 허용해도 replacement가 접근 거부됐고,
   reader close 뒤 같은-volume replacement는 성공했다. 실행 중 `.exe`의 새 이름 rename은 성공했다.
   따라서 data replacement에는 bounded retry, update에는 backup rename+journal이 필요하다.
   [MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw),
   [ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew).
5. **batch 인자 domain을 명시한다.** executable 인자와 cmd 인자 parsing은 다르다.
   직접 `.cmd` 실행이 성공하는 환경을 관찰했지만 arbitrary literal forwarding의 근거로 삼지 않는다.
   conservative domain의 explicit cmd adapter는 실제 round-trip했다. percent/double quote/CR/LF는
   사전에 거부하고 직접 executable 경로를 제공한다.
   [Go os/exec](https://pkg.go.dev/os/exec),
   [Rust Command의 Windows argument caveat](https://doc.rust-lang.org/std/process/struct.Command.html#method.arg).
6. **Tailscale Windows는 named pipe를 사용한다.** 공식 `runLocalCreds` Windows 분기가
   종료하지 않는 HTTP proxy를 실행한다. 기존 one-shot timeout을 늘리는 것은 해결이 아니다.
   공식 protected pipe endpoint와 기존 conditional HTTP 계약을 유지하는 transport로 고정한다.
   현재 stable `v1.104.1` tag에서도 같은 분기·endpoint를 확인했다. Windows provider 최소 버전은
   이 버전으로 고정하며 기존 Unix 기준은 변경하지 않는다.
   [Tailscale CLI source](https://github.com/tailscale/tailscale/blob/v1.104.1/cmd/tailscale/cli/debug.go),
   [pipe endpoint source](https://github.com/tailscale/tailscale/blob/v1.104.1/paths/paths.go).
7. **추가 named-pipe dependency를 도입하지 않는다.** Go 1.27 `os.NewFile`은 overlapped pipe handle에
   runtime poller를 연결한다. native probe에서 deadline timeout 후 deadline reset·read 성공을 확인했다.
   이를 작은 net.Conn adapter에 사용한다. Microsoft/go-winio v0.6.4도 비교했으나 Go 1.26·x/sys
   v0.49.0·logrus 등 dependency 변경이 생기므로 현재 필요한 I/O만으로는 도입 근거가 부족하다.
   이 비교는 package 설치·업그레이드나 완전한 advisory audit가 아니다.
   [Go file_windows.go](https://go.dev/src/os/file_windows.go),
   [go-winio manifest](https://github.com/microsoft/go-winio/blob/v0.6.4/go.mod).

## 5. 지원·검증 한계의 근거

- GHA 공식 표에 x64 Windows와 `windows-11-arm`이 별개로 제공된다. 오래된 preview 공지보다
  현재 runner reference와 실제 run metadata를 우선한다. `windows-latest`를 고정 OS처럼 쓰지 않는다.
  [GitHub-hosted runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
- Windows ARM64 race detector를 요구하지 않는다. 현재 Go 문서는 Windows amd64를 지원 대상으로
  열거한다. ARM64의 일반 native·contention 테스트가 별도 필요하다.
  [Go race detector](https://go.dev/doc/articles/race_detector).
- CTRL_C는 특정 process group으로 제한할 수 없지만 CTRL_BREAK는 group을 지정한다.
  따라서 child group에 break를 보내고 deadline 뒤 Job을 종료하는 방식을 선택한다.
  [GenerateConsoleCtrlEvent](https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent).
- PowerShell의 native argument parsing과 기본 encoding은 버전에 따라 다르다. child argv 보존과
  shell→CLI 입력 손실을 구별하고 PS5.1의 UTF-8 pipeline 설정을 명시한다.
  로컬 PS 5.1에서 `(Get-Command Register-ArgumentCompleter).Parameters.Keys`를 읽어
  `Native` parameter가 있음을 확인했다. 실제 생성 completion script의 통합 검증은 구현 후 수행한다.
  [PowerShell parsing](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_parsing?view=powershell-7.6),
  [PowerShell 5.1 encoding](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_character_encoding?view=powershell-5.1).
- Windows UAC helper는 문서의 실행·취소 API를 기반으로 설계했다. interactive desktop prompt를
  실제로 띄운 검증은 하지 않았다. named pipe primitive 성공을 UAC UI 검증으로 세지 않는다.
  [ShellExecuteExW](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecuteexw).
- WSL2의 NAT와 mirrored, 방향별 localhost·IPv6 동작이 다르다. 이번 localhost 근거는 NAT의
  Windows→WSL IPv4뿐이다. current-user Windows trust는 Linux CA trust와 별개다.
  [WSL networking](https://learn.microsoft.com/en-us/windows/wsl/networking),
  [CurrentUser certificate import](https://learn.microsoft.com/en-us/powershell/module/pki/import-certificate?view=windowsserver2025-ps).
- DrvFS metadata는 기본 활성화 상태가 아니며 Windows permission도 따로 적용된다.
  `/mnt/c` project 지원에서 private data까지 같은 filesystem으로 옮기지 않는다.
  [WSL file permissions](https://learn.microsoft.com/en-us/windows/wsl/file-permissions),
  [WSLENV](https://learn.microsoft.com/en-us/windows/wsl/filesystems).
- Tailscale의 현재 공식 stable package 목록에는 native ARM64 MSI가 있다. 오래된 MSI 안내의 x86
  사용 설명보다 현재 실제 배포물 목록을 우선한다. cloudflared Windows 표는 x86/amd64만 제공한다.
  ARM64의 provider emulation 조건은 devtools ARM64 실행 근거와 구분한다.
  GitHub API에서도 cloudflared `2026.10.1`의 Windows asset이 386/amd64 exe·msi 네 개임을 확인했다.
  [Tailscale stable packages](https://dl.tailscale.com/stable/),
  [cloudflared downloads](https://developers.cloudflare.com/tunnel/downloads/).

## 6. 남은 검증의 분류

구현 전 필수로 정한 최종 GHA 두 architecture probe의 실제 완료·artifact 검토까지 마쳤다.
interactive UAC, 다른 사용자 접근, secret prompt, 실제 packaged updater의 kill-point recovery,
실제 browser TLS 및 provider service는 제품 구현 후 통합 검증이다. 설계 결정이 아니라
관찰할 완료조건으로 계획에 연결했다.

WSL mirrored·WSL2 ARM64·물리 LAN peer·기업 VPN/보안 정책·Windows 10·전원 손실은
이번 실행으로 검증하지 않았다. GHA의 WSL 설치 여부나 Linux native test 결과로 대신하지 않는다.
Windows 10은 범위 밖이며 다른 미검증 조합을 광고 문구에 추가하려면 별도 근거가 필요하다.
