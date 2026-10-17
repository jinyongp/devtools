# WSL에서 devtools 사용하기

WSL에서는 Linux용 devtools와 Linux용 node·pnpm·git을 설치한다. Windows 네이티브
설치와 WSL 설치는 데이터, daemon, CA, 실행 중인 서버를 공유하지 않는다. 프로필을
옮기려면 암호화된 profile export/import를 사용하며 실행 소유권은 이전되지 않는다.

## 프로젝트와 저장소

프로젝트는 `/home/...` 또는 `/mnt/c/...`에 둘 수 있다. private data/config/cache와
권한을 설정한 Linux CLI는 Linux 파일시스템에 둔다. `/mnt/c`의 DrvFS 권한 표시를
private storage 보장으로 취급하지 않는다. WSL은 Linux의 XDG 경로를 사용하며
Windows의 LocalAppData를 자동 검색하거나 가져오지 않는다.

`appendWindowsPath=false`도 지원한다. `node`, `pnpm`, `git` 조회는 Linux PATH에서
해당 이름을 찾으며 Windows의 `node.exe`를 Linux 도구 대신 선택하지 않는다.
Windows 도구를 foreground로 실행하려면 `.exe` 또는 절대 경로를 명시한다.
WSL에서 Win32 프로그램을 managed server로 실행하는 것은 지원하지 않는다.
Windows 서버는 Windows 네이티브 devtools로 관리한다. foreground interop에는
cross-OS 자손 프로세스 종료 보장이 없다.

WSLENV는 사용자가 필요한 변수만 opt-in한다. devtools는 WSLENV를 자동 생성하거나
수정하지 않는다. 예를 들어 공개 값만 전달하려는 경우 사용자가 shell에서
`WSLENV=PUBLIC_CANARY/u`를 설정할 수 있다. 비밀 값을 테스트용 canary로 사용하지 않는다.

## 진단 읽기

```sh
devtools diagnostics
devtools schema diagnostics
```

`data.platform`은 `os`, `arch`, `runtime`, `storage_filesystem`,
`storage_supported`, `interop`, `client_trust`, `limitations`를 반환한다.
WSL의 `client_trust`는 `separate_windows_store`다. 기본 진단은 Windows 프로그램이나
네트워크 probe를 실행하지 않으므로 interop 등록이 enabled여도 `unknown`일 수 있다.
명시적으로 disabled인 등록은 `unavailable`로 표시한다. unknown만으로 전체 ready를
실패시키지 않는다. 확인된 비지원 private 파일시스템은 ready=false와
`unsupported_storage` issue를 반환한다. filesystem=unknown은 판별 불가를 뜻하며
지원된다는 근거가 아니다. 기존 ACL·symlink 검사 결과도 함께 확인한다.

진단은 네트워크 모드를 추측하거나 WSL 설정 파일·환경변수 원문을 출력하지 않는다.
WSL 설정, 방화벽, portproxy, PATH, distro 상태를 자동 변경하지 않는다.

## Windows 브라우저에서 접속하기

먼저 WSL 내부에서 [로컬 네트워크 가이드](local-network.md)의 고포트 예제를 확인한다.
Windows에서의 IPv4 localhost 전달, 이름 해석, 인증서 신뢰는 각각 별도로 확인해야 한다.
`.localhost` 성공이 custom host, IPv6, 역방향, LAN 연결을 보장하지 않는다.
custom host를 Windows에서도 쓰려면 Windows 클라이언트의 DNS/hosts를 별도로 설정한다.
WSL의 hosts 수정은 Windows hosts를 수정하지 않는다.

Linux의 `trusted=true`는 Windows 브라우저의 신뢰 상태가 아니다. WSL에서 public CA만
내보낸 뒤 Windows로 복사한다. 기존 파일은 덮어쓰지 않는다.

```sh
devtools proxy ca export --output ./devtools-wsl-ca.pem
```

응답의 SHA-256 fingerprint를 별도로 기록한다. 개인 키와 Linux private data는 복사하지
않는다. 다음은 Windows PowerShell 5.1/7에서 사용자가 직접 수행하는 예시다.
파일 경로와 EXPECTED_SHA256을 실제 값으로 바꾼다. Windows 신뢰 등록은 명시적인
OS 인증서 확인을 요구할 수 있다. 시스템 전체가 아닌 CurrentUser Root에 등록한다.

```powershell
$path = 'C:\Temp\devtools-wsl-ca.pem'
$expected = 'EXPECTED_SHA256'
$cert = [Security.Cryptography.X509Certificates.X509Certificate2]::new($path)
$sha = [Security.Cryptography.SHA256]::Create()
try { $actual = ([BitConverter]::ToString($sha.ComputeHash($cert.RawData))).Replace('-', '').ToLowerInvariant() }
finally { $sha.Dispose() }
if ($actual -ne $expected.ToLowerInvariant()) { throw 'CA fingerprint mismatch' }
$wasPresent = Test-Path ('Cert:\CurrentUser\Root\' + $cert.Thumbprint)
if (-not $wasPresent) {
  Import-Certificate -FilePath $path -CertStoreLocation Cert:\CurrentUser\Root
}
$cert.Thumbprint
```

브라우저를 다시 시작하고 hostname·포트가 맞는 HTTPS 요청으로 확인한다. 인증서 검사를
끄지 않는다. 제거 시 **이번에 새로 가져온 인증서만** 대상으로 한다. 위 `$wasPresent`가
true였다면 이 절차로 제거하지 않는다. 기록한 정확한 thumbprint를 사용하며 이름으로
일괄 삭제하지 않는다.

```powershell
if (-not $wasPresent) {
  Remove-Item -LiteralPath ('Cert:\CurrentUser\Root\' + $cert.Thumbprint)
}
$cert.Dispose()
```

이 CA는 WSL 소유이므로 Windows 네이티브 devtools의 `proxy untrust`가 대신 제거하지
않는다. 네이티브 CA도 필요하면 Windows 쪽에서 별도로 초기화하고 관리한다.

## 검증 범위

최종 제품은 Linux CI와 WSL 진단 fixture를 기준으로 검증한다. 새 제품의 실제 WSL2
NAT/mirrored 네트워크, Windows 브라우저, IPv6·역방향·다른 distro·LAN·ARM64 WSL2 및
위 PowerShell 수동 인증서 절차는 실제 PC에서 실행하지 않았다. 과거 primitive NAT
probe는 당시의 코드에만 해당한다. Actions의 `wsl.exe` 존재를 WSL2 구동·네트워크 검증으로
간주하지 않는다. 현재 distro를 종료하거나 네트워크 모드를 바꾸는 검증도 수행하지 않는다.

공식 명령·설정 참고: [Import-Certificate](https://learn.microsoft.com/en-us/powershell/module/pki/import-certificate?view=windowsserver2025-ps),
[WSL interop 설정](https://learn.microsoft.com/en-us/windows/wsl/wsl-config),
[WSLENV](https://learn.microsoft.com/en-us/windows/wsl/filesystems#share-environment-variables-between-windows-and-wsl-with-wslenv).
