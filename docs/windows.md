# Windows 네이티브 사용

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

Windows 네이티브 x64·ARM64 지원의 첫 배포 대상은 v0.25.0이다.
v0.24.1까지는 Windows 배포물이 없으며, v0.25.0이 게시되기 전에는 검증된 후보 자산을 사용한다.
이 가이드는 후보를 설치·실행할 때의 요구 사항과 한계를 설명한다.
WSL 안에서 사용할 때는 [Linux용 설치와 WSL 안내](wsl.md)를 따른다.

## 지원과 검증 범위

| 항목 | 범위 |
| --- | --- |
| 지원 목표 | Windows 11 25H2·26H2, x64·ARM64, 각 버전 최신 누적 업데이트 |
| 실행 | standalone `devtools.exe`, 복사본 `dvt.exe`; PowerShell 5.1/7·cmd |
| 필수 런타임 | Go·Python·Git Bash·WSL 불필요. 실행할 프로젝트 도구는 별도 설치 |
| 자동 검증 | GitHub Actions `windows-2025` x64, `windows-11-arm` ARM64. Server는 테스트 호스트이며 제품 지원 OS가 아님 |
| 제외 | Windows 10·24H2 이하·26H1 특수 배포·LTSC/IoT/Insider·32비트·self-hosted 운영. 향후 OS 버전 자동 지원 없음 |
| 미검증 | 실제 PC의 UAC·브라우저·터미널 닫기·사용자 설치 관찰, 전체 CPU×OS 조합, 실제 WSL2/Windows 브라우저 네트워크, 외부 provider 계정/LAN |

Actions 성공은 각 지원 OS/CPU 조합을 실기기로 모두 검증했다는 뜻이 아니다. 두 표준
runner는 public repository 기준 무료 실행 범위이며 larger/GPU runner를 사용하지 않는다.
artifact 저장량에는 계정의 저장 한도·과금 정책이 별도로 적용될 수 있다.

## 후보 설치와 업데이트

v0.25.0 게시 전에는 공개 stable 설치 URL 대신 검증된 후보 파일을 사용한다. [최종 후보 검증 기록](windows-support/implementation/W9.md)에서
해당 Actions 실행을 확인하고 `release-assets` artifact를 받는다. installer·해당 CPU ZIP·checksum·
`version.txt`를 같은 디렉터리에 준비한다. 다른 실행의 파일을 섞지 않는다.

PowerShell을 installer가 있는 디렉터리에서 열고 아래 버전·경로를 실제 값으로 바꾼다.
`-Version`은 archive 이름/version과 정확히 맞아야 한다.

```powershell
.\install.ps1 -Version CANDIDATE_VERSION -Source C:\Temp\devtools-release
```

v0.25.0이 실제 게시된 뒤에는 해당 GitHub Release의 `install.ps1`과 Windows 자산을 사용한다.
Pages 설치기 게시도 완료되었다면 다음 PowerShell 명령으로 같은 버전을 설치할 수 있다.
이 명령은 릴리스와 Pages 게시 전에 실행하지 않는다.

```powershell
Invoke-WebRequest -Uri 'https://jinyongp.dev/devtools/install.ps1' -OutFile '.\install.ps1'
.\install.ps1 -Version 0.25.0
```

다운로드한 설치기의 출처와 내용을 확인한 뒤 실행한다. 기본 bin은 Known Folder LocalAppData 아래 `Programs\devtools\bin`이다. ARM64 OS에서
x64 PowerShell을 실행해도 ARM64 archive를 고른다. 설치기는 checksum·ZIP·PE CPU·실제
version/commit을 확인한다. ZIP에는 regular `devtools.exe` 하나만 들어간다.
`dvt.exe`는 같은 내용의 별도 파일이며 symlink나 `.cmd`가 필요 없다. 외부 소유 `dvt`와
충돌하면 해당 파일을 보존하고 `skipped_conflict`로 알린다.

설치 결과의 bin 경로를 현재 세션 PATH에 추가하거나 전체 경로로 실행한다. 영구 user
PATH 수정은 명시 `-AddToPath`에서만 수행한다. installer는 shell profile·시스템 PATH·
영구 execution policy를 바꾸지 않는다. 실행 정책은 조직/사용자의 정책에 따라 처리한다.

`myapp`이 기존 profile이면 값을 공유하므로 예제에는 사용하지 않은 이름을 고른다.

```powershell
$env:PATH = "$env:LOCALAPPDATA\Programs\devtools\bin;" + $env:PATH
devtools version
dvt version
devtools init --profile myapp
devtools var set LOG_LEVEL --value info
devtools run -- cmd.exe /d /c set LOG_LEVEL
```

마지막 명령의 `LOG_LEVEL=info` 출력으로 값 주입을 확인한다.

Known Folder가 리디렉션되어 있으면 설치 결과의 정확한 경로를 사용한다. `$env:LOCALAPPDATA`
예시는 일반적인 기본 배치다. 이후 `devtools update`는 Go 내부 구현으로 설치를 갱신한다.
Unix shell을 실행하지 않는다. 명시 버전/소스는 `update --version VERSION --source DIRECTORY`로
선택한다. 실행 중인 image는 고유 old 이름으로 보존하고 journal로 main/alias를 복구한다.
응답을 잃었거나 중단되면 같은 installer/업데이트를 다시 실행해 복구한다. 외부 파일을
수동 삭제하지 않는다. 사용 중인 old image는 보존되며 다음 업데이트에서 정리할 수 있다.
active daemon은 자동 재시작하지 않으므로 갱신된 CLI와 daemon의 버전이 일시적으로 다를 수 있다.

## 저장소와 프로세스

config/data/cache/runtime은 Known Folder LocalAppData 아래 `devtools`에 서로 분리된다.
private storage는 현재 사용자와 SYSTEM만 접근하는 local NTFS에 둔다. 기존의 비지원 ACL을
자동으로 고치지 않는다. UNC/SMB·장치 경로·ADS·reparse/symlink·cloud 동기화 저장소는
활성 private storage로 지원하지 않는다. private 데이터는 평문이고 backup/profile export는
암호화된다. 다른 컴퓨터/OS로 옮길 때는 [프로필 전송](profiles.md)을 사용한다.

managed 프로세스는 생성 시점에 Job에 연결한다. 정상 종료를 요청하고 3초 유예 후 남은
트리를 종료한다. 상위 host Job에서 빠져나오지 않으므로 host가 종료되면 함께 종료될 수 있다.
저장된 PID만 보고 프로세스를 종료하지 않는다. supervisor 상태를 확인할 수 없으면
unknown/recovery 상태를 반환하며 외부 프로세스를 추측해서 정리하지 않는다.

프로젝트 명령은 argv 배열이다. `.cmd`/`.bat`는 OS의 `cmd.exe /d /e:on /v:off /s /c`로
실행하며 NUL·개행·double quote·percent가 포함된 batch 인수는 거부한다. 복잡한 값은
직접 실행 가능한 프로그램으로 전달한다. `.ps1`은 `pwsh.exe`/`powershell.exe`를 argv에
명시한다. PowerShell 5.1의 native argv 전달 과정에서 이미 소실된 empty/quote 인수는
CLI가 복원할 수 없다. 필요하면 PowerShell 7의 native argument 모드를 사용한다.

## 로컬 HTTPS와 권한

[로컬 네트워크 가이드](local-network.md)의 loopback 고포트부터 시작한다. Windows setup은
소켓/설치 image 진단과 의도만 관리하며 firewall·HTTP.sys·portproxy를 자동 변경하지 않는다.
custom host 변경은 같은 SID의 제한된 UAC helper로 수행한다. 다른 사용자 계정으로
승격하는 흐름은 지원하지 않는다. 비대화형 hosts 변경은 이미 승격된 token이 필요하다.

CA는 Windows CurrentUser Root에 명시 `devtools proxy trust --request-id UUID`로 등록한다.
trust/untrust는 대화형 터미널과 OS 인증서 확인이 필요하며 비대화형이면
`permission_required`다. 제품은 확인 창을 자동 승인하지 않는다. 기존 외부 인증서를
자기 소유로 채택하거나 제거하지 않는다. CLI가 생성한 소유 receipt와 fingerprint가
일치하는 경우에만 제거한다. 신뢰 등록 없이도 public CA를 export해 개별 client에 지정할 수 있다.
OS 신뢰가 필요한 브라우저 동작은 실제 PC에서 검증하지 않았다.

WSL Linux trust, Windows 브라우저 trust, Windows 네이티브 CA는 별개다.
[WSL 안내](wsl.md)의 public 인증서 export/fingerprint/클라이언트 import 절차를 따른다.

## 외부 공개와 자동완성

Tailscale은 사용자가 설치·로그인한 1.104.1 이상과 고정 protected named pipe를 사용한다.
자동 설치·로그인·debug credential proxy는 실행하지 않는다. ETag/If-Match로 선택한
Serve handler의 변경만 반영하며 외부 설정을 덮어쓰지 않는다. cloudflared도 사용자가
설치한다. ARM64에서는 amd64 실행 파일의 Windows 에뮬레이션을 전제로 한다. 두 provider는
실제 pipe/테스트용 child로 검증했으며 실제 계정의 공개 연결은 검증하지 않았다.

PowerShell 5.1/7의 [자동완성](completion.md#powershell-51--7)은 `devtools`/`dvt` 모두
지원한다. cmd는 실행만 지원한다. 자동완성은 literal AST만 읽고 동적 표현식을 평가하지 않는다.
