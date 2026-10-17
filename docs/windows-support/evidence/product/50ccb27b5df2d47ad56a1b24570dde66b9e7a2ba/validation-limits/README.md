# Windows 후보 검증 범위와 미검증 한계

후보 전체 SHA: `50ccb27b5df2d47ad56a1b24570dde66b9e7a2ba`. 검증용 버전은 `0.0.0-windows-candidate`이며,
실제 릴리스 버전을 정하거나 게시한 결과가 아니다.
[Actions 전체 실행](https://github.com/jinyongp/devtools/actions/runs/39024924795)에서 Windows 두 full job,
macOS·Linux 회귀, 패키징이 통과했다. publish=false로 태그·Release·Homebrew·Pages는
게시하지 않았다. [원본 결과와 배포물 대조](../actions-39024924795/summary.json),
[기계 판독 기록](validation-limits.json)에 동일 후보 근거를 보관한다.

## 실제 관찰한 환경

| native CLI CPU | 실제 OS·build·runner image | job token | Node·browser CPU |
| --- | --- | --- | --- |
| amd64 | Microsoft Windows Server 2025 Datacenter; 24H2; build26100; image20261007.264.1 | elevated=true | CLI=amd64, Node=x64, Chromium=amd64 153.0.8010.12 |
| arm64 | Microsoft Windows 11 Enterprise; 25H2; build26200; image20261013.183.4 | elevated=true | CLI=arm64, Node=arm64, Chromium=amd64 153.0.8010.12 |

Server2025는 x64 테스트 호스트이며 제품 지원 OS가 아니다. 이 Server의24H2 표기를
Windows11 24H2 지원으로 해석하지 않는다. Windows11 지원 목표는25H2·26H2의
x64·ARM64 최신 누적 업데이트다. 실제 관찰은 위 두 환경이며 전체 CPU×OS 조합의
실기기 검증이 아니다. 과거 x64/WSL standalone primitive 관찰은 당시 코드에만 적용한다.

두 hosted job은 elevated token으로 실행됐다. 별도 standard 계정의 접근 거부·helper
권한 검사는 통과했지만, 전체 설치/제품 여정을 실제 PC의 비상승 사용자로 확인한
것은 아니다. ARM64 CLI와 Node는 네이티브이며 Chromium은 amd64 에뮬레이션이다.
이를 ARM64-native browser 검사로 표시하지 않는다.

## 동일 ZIP과 검증 범위

- amd64: `devtools_0.0.0-windows-candidate_windows_amd64.zip`
  SHA-256 `341c7db0d007b4eedd120dc44dba6ed0571db74267b5f99c9cb10c6d92163849`
- ARM64: `devtools_0.0.0-windows-candidate_windows_arm64.zip`
  SHA-256 `36940bcd2b0ffed3a37132c6989ac55aa37da81093bf5b0ce3ad31ddff03474e`

각 native job이 생성한 실제 ZIP을 PS5.1 설치기와 제품 여정·Chromium 검사에서 사용했다.
별도로 내려받은 native product artifact와 최종 release-assets의 ZIP bytes/checksum이
일치한다. 여섯 OS/CPU archive의 format·CPU·Go build metadata·version·commit,
Skill 소스·설치기·version.txt도 대조했다. 테스트 fixture 실행 파일은 배포물에 포함되지 않는다.

[release-assets](https://github.com/jinyongp/devtools/actions/runs/39024924795/artifacts/12097914928)
보관 만료는 `2026-11-16T10:42:43Z`다. ZIP 다운로드에는 Actions artifact 보관기간이
적용되며 이 Git 기록 자체는 남는다. 실제 stable Windows 다운로드 자산은 게시하지 않았다.

두 native 전체 Go suite는 각923pass와 기존 선택 실행 항목2개다. 대용량 maintenance는
별도 필수 storage gate에서 실행해 통과했고, development browser-server entry는 자동 검사가
아닌 opt-in fixture다. 실제 packaged Chromium 검사는 별도로 통과했다. 필수 저장소·복구·
argv/shim·Job/process·console·network·provider·installer/update·PS5/7 검사를 skip으로
대체하지 않았다. amd64 race268pass와 ARM64 native contention을 구분한다.

설치된 후보는 값·secret·task, project start/restart/stop, proxy, CA 검증을 켠 TLS,
암호화 profile preview/apply/replay를 통과했다. 브라우저는 로그인·변수 수정·reload·CLI
확인과 session 없는401/외부 Origin403을 검증했다. 실제 CA 신뢰 등록을 사용한 브라우저
HTTPS 검사가 아니다. hosts/trust/console/프로세스 하위 fixture도 runner 범위의 근거다.

## NOT RUN

실제 PC의 UAC 승인·취소, CurrentUser trust와 실제 브라우저 통합, 사용자가 터미널을
닫은 뒤 daemon 유지, 실제 PC의 설치/update/rollback 관찰은 **NOT RUN**이다.
최종 제품의 실제 WSL2 NAT·mirrored/Windows browser 연계도 **NOT RUN**이다.
Linux CI·WSL 진단 fixture·이전 primitive 결과로 해당 제품 연동을 PASS로 표시하지 않는다.
ARM64-native Chromium, ARM64 race detector, 외부 provider 계정·public ingress·실제 LAN
peer도 검증하지 않았다. ARM64 race는 현재 Go의 지원 대상이 아니다.

Windows10·24H2 이하·26H1 특수 배포·LTSC/IoT/Insider·32비트·self-hosted 운영은 지원
범위에서 제외한다. Windows/WSL data·daemon·CA·active ownership은 독립이며 firewall,
portproxy, WSLENV, WSL 설정을 자동 변경하지 않는다. 사용자 PC나 로컬 Windows 실행은
이 최종 검증에서 수행하지 않았다. [공개 지원 안내](../../../../../windows.md)와
[WSL 안내](../../../../../wsl.md)의 한계를 그대로 유지한다.

## 마지막 리뷰루프

| 영역 | 상태 | 대조 결과 |
| --- | --- | --- |
| 전체 SHA·ZIP·배포물 | Reviewed | 동일50ccb27의 두 native ZIP을 final release-assets와 byte/hash/identity 대조. 여섯 archive·Skill·설치기 검사 일치. |
| 실제 runner·token·CPU | Reviewed | 원본 metadata의 Server/client, build/image, elevated token과 Node/browser CPU를 그대로 구분. 전체 Win11 조합·비상승 PC·native ARM browser로 확대하지 않음. |
| 실행 범위·NOT RUN | Reviewed | Actions API/console/browser/설치 fixture의 통과와 실제 PC/최종 WSL2·외부 연결 미실행을 분리. 실제-PC PASS 기록 없음. |
| 공개 문서·이전 근거 | Reviewed | README 양쪽, Windows/WSL guide와 plan의 Actions-only 정책·제외·독립 store/trust와 일치. 과거 primitive는 역사적 범위로 제한. |
| 인계·변경 경계 | Reviewed | 증거/수용 기록만 추가. 제품 코드·패키지 입력·공개 사용 계약 변경 없음. W8R·W9에 같은 source/ZIP을 인계하며 여기서 게시/close하지 않음. |

W8L definition183의 요구와 전체 영역을 검토했다. 수정할 불일치나 미해결 in-scope 결함은
없으며 필수 실행 기준을 실제-PC PASS로 바꾸거나 waiver하지 않았다.
