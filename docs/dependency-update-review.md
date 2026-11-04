# 의존성 업데이트 검토

2026-11-04의 조사, 검증, 배포 기록이다. 시작 소스는
`efdf1771baad032eac06429845ba9f431bc064f5`이고, 최종 소스
`e18ed8efd08e5cf8e622f48c146673b6b6fb62f5`를 v0.42.0으로 배포했다.
의존성 버전과 후속 조치의 당시 판단을 기록하며, 이후 최신 버전을 보장하지 않는다.

## 범위와 선택 기준

Go 직접 의존성 9개와 간접 의존성, 루트 npm 개발 의존성 2개와 proxy fixture 의존성 18개,
GitHub Actions 저장소 13개의 사용처 106개를 조사했다. 함께 사용하는 런타임,
Playwright 이미지와 명시 live 검증의 provider 버전도 대조했다.

공식 레지스트리와 upstream 릴리스에서 안정 버전을 확인했다. 안정된 major도 선택하되,
현재 소비자의 엔진과 peer 요구사항, 플랫폼 지원, 직렬화 방식이 맞는 조합을 유지했다.
잠금파일은 Go와 pnpm의 기본 resolver로 생성했다. 강제 override는 추가하지 않았다.

## 반영한 버전

| 대상 | 이전 | 적용 |
| --- | --- | --- |
| Go 최소 버전 | 1.27.1 | 1.27.3 |
| purego | 0.11.2 | 0.11.3 |
| x/crypto | 0.56.0 | 0.58.0 |
| x/sys | 0.47.0 | 0.49.0 |
| x/term | 0.45.0 | 0.47.0 |
| pnpm | 11.24.0 | 12.16.1 |
| Playwright 및 Docker 이미지 | 1.63.0 | 1.64.1 |
| skills | 1.6.0 | 1.7.6 |
| proxy CI와 WSL의 Node 22 | 22.22.2 | 22.23.4 |
| Cloudflared live 검증 | 2026.10.3 | 2026.10.7 |
| Tailscale host와 peer 검증 | 1.104.2 | 1.104.4 |
| ARM64 위 x64 PowerShell 검증 | 7.6.6 | 7.6.9 |
| dashboard의 vendored D3 | 7.9.0 | 7.9.2 |

Go release workflow의 버전 중복 8곳은 `go-version-file: go.mod`로 통일했다.
프로젝트 개발용 Node 24.x 정책은 유지했다. Nuxt의 새 엔진 요구사항을 충족하도록
fixture의 Node 최소 버전은 22.22.3으로 올렸다.
Playwright 이미지의 OCI digest와 WSL Node archive의 SHA-256도 해당 버전과 함께 갱신했다.

| proxy fixture | 이전 | 적용 |
| --- | --- | --- |
| @nuxt/devtools | 3.4.2 | 3.4.4 |
| @sveltejs/kit | 3.1.0 | 3.1.1 |
| @vitejs/plugin-react | 6.1.2 | 6.2.1 |
| next | 16.4.2 | 16.4.5 |
| nuxt | 4.5.2 | 4.6.4 |
| svelte | 5.57.2 | 5.57.3 |
| webpack | 5.112.0 | 5.112.2 |
| webpack-cli | 7.2.3 | 7.3.1 |
| ws | 8.22.0 | 8.22.1 |

18개 중 위 9개를 갱신했다. 나머지 Svelte Vite plugin 7.3.1, Vue Vite plugin 6.0.10,
React와 React DOM 19.3.1, Socket.IO와 client 4.8.4, Vite 8.3.5, Vue 3.5.44,
webpack-dev-server 6.1.0은 조사한 안정 버전을 이미 충족했다.

| Actions 저장소 | 이전 | 적용 | 변경 사용처 |
| --- | --- | --- | --- |
| astral-sh/setup-uv | 10.3.0 | 10.3.3 | 6 |
| releaseway/actions | 0.7.3 | 0.8.0 | 5 |
| tailscale/github-action | 기존 v4 SHA | 4.4.0 SHA | 1 |
| Homebrew/actions | 기존 main SHA | c262868c096d909fc278daa87ed9772967331ea3 | 1 |

모두 전체 commit SHA로 고정했다. Homebrew는 기존 main 추적 정책을 유지하면서
실제 사용하는 setup-homebrew와 그 의존성 변경을 대조했다. releaseway의 prepare,
publish preview, distribute 입력 계약도 확인했다.
나머지 Actions 저장소 9개는 조사 시점 안정 릴리스를 이미 사용하고 있었다.

## 필요한 호환성 변경

### pnpm 12

[pnpm 12.16.1](https://github.com/pnpm/pnpm/releases/tag/v12.16.1)은 안정 릴리스다.
루트와 fixture의 `packageManager`를 함께 변경하고, 두 잠금파일을 pnpm 12로 재생성했다.
기존 Corepack 0.34.6으로 버전 선택과 설치를 검증했다. 전역 도구는 변경하지 않았다.

잠금파일은 버전 9 형식을 유지하면서 패키지 매니저와 플랫폼 바이너리를 기록하는 YAML
문서가 추가된다. resolver가 생성한 integrity와 플랫폼 항목을 유지했다.

pnpm 12의 엄격한 설치 스크립트 정책에 맞춰 fixture의 `allowBuilds`에서
`esbuild@0.28.2` 스크립트를 명시적으로 거부했다. 기존 CI도 `--ignore-scripts`를 사용한다.
잠긴 플랫폼 바이너리로 실제 빌드와 HMR 검사가 통과했다. 다른 패키지의 새 스크립트는
계속 기본 검사 대상이다.

Next.js 16.4.5는 조사 당시 기본 배포 대기 시간보다 최근에 공개된 보안 패치였다.
[공식 릴리스](https://github.com/vercel/next.js/releases/tag/v16.4.5)를 확인한 뒤
해당 버전의 `next`, `@next/env`, SWC 플랫폼 패키지 10개만 대기 예외로 두었다.
전체 release-age 검사는 유지했다.

### Nuxt DevTools 승인

Nuxt 4.6.4는 DevTools `^3.4.4`를 사용한다. 독립 최신 major 4.1.0을 강제 도입하면
서버와 RPC driver의 의존성 조합이 달라지므로 3.4.4를 선택했다.

새 DevTools는 telemetry와 onboarding 설정에 서버 소유자 승인을 요구한다.
fixture는 격리한 HOME의 XDG 설정 디렉터리에 해당 테스트만의 자격증명을 준비하고,
공개 Origin의 공식 승인 페이지와 `auth-verify` 응답을 통해 검증한다.
기존 Host와 Origin 검사를 끄거나 완화하지 않았다.

preserve에서는 승인 후 실제 Pages와 Components RPC를 확인한다. rewrite는 독립적인
공개 Origin RPC 거절을 음성 대조군으로 유지한다. 서버 로그와 오류에서는 소유 토큰을
제거한다. 승인 페이지 이동 실패와 응답 대기의 예외도 함께 처리한다.

## audit 결과와 후속 조치

루트 npm 의존성 audit는 경고 0건이다. fixture는 17건에서 3건으로 줄었으며
높음과 치명적 경고는 0건이다. 아래 3건은 검증 fixture의 전이 의존성에 남아 있다.
Go CLI 배포물의 npm 의존성으로 포함되는 항목은 아니다.

| 남은 패키지 | 수준과 수정 버전 | 제약과 후속 조치 |
| --- | --- | --- |
| deepmerge 4.3.1 | 낮음, 수정 릴리스 없음 | Svelte Vite plugin과 Nitro의 Rollup 경로. upstream 수정 릴리스와 부모 반영을 추적한다. |
| glob 13.0.6 | 보통, >=14.0.2 | 최신 @vercel/nft 1.11.0의 범위가 ^13.0.0이다. 부모가 glob 14를 지원하면 갱신하고 Next/Nitro를 재검증한다. |
| minimatch 10.2.6 | 보통, >=11.0.1 | 최신 readdir-glob 3.0.0의 범위가 ^10.2.2이며 glob 13 경로도 있다. 부모의 major 호환 반영 후 갱신한다. |

각 근거는 [deepmerge advisory](https://github.com/advisories/GHSA-gcqx-w48m-w2h4),
[glob advisory](https://github.com/advisories/GHSA-3g3m-2fjr-6pw4),
[minimatch advisory](https://github.com/advisories/GHSA-g3hv-75wg-m249)다.
특정 기능의 도달 가능성을 모두 분석한 결과는 아니며, 강제 override로 경고를 숨기지 않았다.

부모가 허용하는 범위에서는 native resolver로 node-forge 1.4.0 → 1.4.2,
body-parser 2.3.0 → 2.4.0, serialize-javascript 7.1.2 → 7.1.4를 추가 갱신했다.
독립 의존성이나 중복 구현은 추가하지 않았다.

fixture 잠금파일의 패키지 항목은 매니저 자체의 15개 항목을 제외하면 909개에서 854개가 됐다.
기존 이름 104개의 버전 구성이 바뀌고 이름 10개가 추가되며 61개가 제거됐다.
Nuxt 4.6.4가 요구하는 `@nuxt/cli ^4.0.1`과 새 Vite server 분리, Nitro 계열 변경이 포함된다.
webpack-cli 7.3.1의 `rechoir ^1.0.0`도 함께 적용된다. 변경된 부모의 선언 범위와 설치 결과를
대조했고, 줄 수 변화만으로 무관한 삭제라고 판단하지 않았다.

## 로컬 검증과 한계

검증 환경은 Linux x64/WSL, Go 1.27.3, Node 22.23.4 및 24.22.2, pnpm 12.16.1이다.
개발 호스트의 DNS나 trust store는 변경하지 않고 일회용 브라우저 컨테이너를 사용했다.

| 검사 | 결과 |
| --- | --- |
| `go test -race ./...` | 통과 |
| `devtools run check:static` | 통과 |
| 루트와 fixture의 frozen 설치 | 통과, 기본 설치와 CI의 스크립트 생략 설치 확인 |
| dashboard Node 검사와 skills 1.7.6 발견 | 통과 |
| dashboard Chromium smoke | 4개 통과 |
| Chromium, 8개 소비자 × HTTP/HTTPS × rewrite/preserve | 32개 통과 |
| Firefox/WebKit, Nuxt/Vite React/ws/Socket.IO × rewrite/preserve | 16개 통과 |
| 로그 토큰 제거와 fixture 정리 단위 검사 | 6개 통과 |
| browser driver 구문 검사 | 통과 |
| `go mod verify` | 통과 |

Go 모듈, pnpm 설치 정책과 잠금파일, browser driver와 로그 기록, Actions 입력과 런타임,
문서의 버전을 순차 리뷰했다. 승인 흐름의 실패 처리 수정 후 Nuxt의 Chromium, Firefox,
WebKit × rewrite/preserve 6개를 다시 실행해 통과했다. 루트와 fixture 모두 기본 frozen 설치와
`--ignore-scripts` 설치를 반복한 뒤 잠금파일 SHA-256이 유지되는 것도 확인했다.

Windows와 macOS의 새 변경사항 native CI, Homebrew native, releaseway 실제 preview 실행,
Windows↔WSL 브라우저 검증은 이 로컬 실행에 포함하지 않았다. 새 provider 버전의 공개 ingress도
실행하지 않았다. 이후 원격 검증과 배포까지 완료한 결과는 아래에 기록한다.

## 배포 검증에서 확인한 추가 사항

첫 원격 후보 `ae7e380`의 Homebrew native 네 플랫폼과 releaseway preview는 통과했다.
아래 실패는 무조건 재시도하지 않고 원격 artifact와 공식 배포 경로를 확인했다.

- [Windows ARM64 installer-x64](https://github.com/jinyongp/devtools/actions/runs/41805263686/job/126490581211):
  고정된 x64 PowerShell 7.6.6이 inline bootstrap 중 `0xC0000005`로 종료됐다.
  Application 이벤트의 실패 프로세스도 `pwsh.exe`였다. 기존에 남아 있던 이 별도 검증
  런타임을 최초 업데이트 목록에서 놓쳤다. 공식 안정 버전 7.6.9와 ZIP SHA-256으로 갱신했다.
  upstream crash의 내부 원인을 확인했다거나 이 변경만으로 해결됐다고 단정하지 않는다.
- [Tailscale live 준비](https://github.com/jinyongp/devtools/actions/runs/41805366084/job/126490925917):
  1.104.5 바이너리는 공개됐지만 Docker peer 이미지는 없어 `manifest unknown`으로 실패했다.
  공식 Docker Hub의 최신 stable 이미지는 1.104.4였다. host와 peer를 이 버전으로 맞추고
  peer의 manifest digest도 고정했다. 버전 문자열만 확인한 최초 조사가 불충분했다.

변경 후 Windows full과 PowerShell 반복 진단, Tailscale 실제 두 장치 검증을 다시 실행해
통과했다. PowerShell 7.6.9의 x64-on-ARM64 fixture는 .NET 10.0.14를 사용했다.

추가 inventory에서 npm manifest 밖의 dashboard D3 7.9.0도 확인했다.
안정 버전 7.9.2의 공식 npm tarball SHA-512를 검증하고 bundle, ISC license,
출처와 SHA-256을 함께 갱신했다. dependency manifest 검사와 실제 정적 파일 응답 검사는
새 버전을 요구하도록 유지했다. dashboard Go race와 Node 검사 46개, 실제 Chromium smoke
4개를 다시 실행해 통과했으며 CDN 원본과 vendored bundle의 SHA-256 일치도 확인했다.

첫 [0.42.0 Release 검증](https://github.com/jinyongp/devtools/actions/runs/41809324623)은
Linux와 macOS의 기존 `completion` 설치 시나리오에서 중단되어 게시하지 않았다.
이 fixture에는 실행이 없는 선언을 project restart/logs 후보로 요구하고, PATH에 바이너리가
없어도 fish의 정적 fallback을 요구하는 이전 기대값이 남아 있었다. 공통 후보 엔진을 사용하는
현재 계약에 맞춰 음성 대조를 수정했다. Zsh `_describe` stub이 출력하는 값의 escaping도
기대값에서 반영했다. 제품의 필터나 후보 공급자는 변경하지 않았다.

변경 전 Linux 실패를 재현했다. 수정 후 실제 설치본의 bash/zsh/fish와 Bash Readline 입력,
`completion_context`를 실행해 통과했다. 실제 실행 중인 명령의 project restart 접두사 조회도
추가해 유효한 후보가 유지되는지 함께 확인했다. 전체 설치 시나리오와 릴리스 검증도 다시 실행했다.

로컬 전체 설치 검증에서는 `tasks` 시나리오에 남은 D3 7.9.0 응답 기대값도 발견했다.
실제 게시할 asset과 독립적으로 확인한 7.9.2를 요구하도록 갱신했다. 전체 tracked 파일을
대조해 현재 실행 검사에 남은 구 D3 버전 기대값이 없는 것도 확인했다.
수정 전 이미 통과한 앞 32개와 수정 후 선택 실행한 나머지 8개를 합쳐 로컬 설치 시나리오
40개를 검증했다. 이후 최종 Release의 전체 실행도 별도로 성공했다.

## 최종 원격 검증과 배포

배포 소스는 `e18ed8efd08e5cf8e622f48c146673b6b6fb62f5`이며 protocol은 24다.

| 경계 | 실제 결과 |
| --- | --- |
| [Windows full](https://github.com/jinyongp/devtools/actions/runs/41811670109) | 최종 SHA에서 23개 작업 성공. x64와 ARM64, 실제 설치와 업데이트, native 경계, x64 race 포함. |
| [Homebrew 필수 검사](https://github.com/jinyongp/devtools/actions/runs/41811873077) | 최종 SHA에서 Formula 생성, spec/audit, 필수 gate 성공. |
| [최종 Release](https://github.com/jinyongp/devtools/actions/runs/41812754434) | 53개 작업 성공. provider opt-in 두 작업은 조건대로 생략했으며 아래 별도 실제 실행으로 확인했다. |
| [실제 Cloudflared](https://github.com/jinyongp/devtools/actions/runs/41807460293) | basic/no-auth, 공개 RPC, HMR, 메시지, 선택 stop와 sibling 보존 성공. |
| [실제 Tailscale](https://github.com/jinyongp/devtools/actions/runs/41807427987) | 두 장치의 실제 Serve, basic/no-auth, RPC/HMR, 메시지, 소유 설정 복원 성공. |
| [PowerShell 반복 진단](https://github.com/jinyongp/devtools/actions/runs/41807415797) | native x64, native ARM64, ARM64 위 x64에서 startup/interop/stdio 총 300회 성공. 설치와 업데이트 흐름은 각각 1회, 1회, 3회 성공. |
| [Installer Pages](https://github.com/jinyongp/devtools/actions/runs/41814611434) | v0.42.0 배포 성공. 공개 sh/ps1 응답이 해당 release asset과 바이트 단위로 일치. |
| [Homebrew tap 후속 CI](https://github.com/jinyongp/homebrew-tap/actions/runs/41814954470) | 게시된 Formula의 maintenance/audit 성공. tap 커밋은 `e7723a1a6eec76732241e7139b3670c5e4aec5ec`. |

최종 Release에는 macOS와 Linux의 전체 Go race와 설치 시나리오 40개, 실제 read-only
sandbox, Secret Service/Keychain/DPAPI와 다른 사용자 경계, 실제 WSL2 재시작,
Windows 브라우저 연결, HTTP/HTTPS와 LAN 브라우저 검증, 과거 자동화 수치 수집을 포함했다.
`system_network_smoke=true`로 macOS와 Linux의 실제 hosts, OS trust, 80/443 검사도 통과했다.
네 native OS producer 사이의 암호화 profile 교환 12개 방향도 모두 통과했다.
Homebrew 게시는 macOS와 Linux의 x64/ARM64 네 native 설치 검증을 통과한 뒤 완료됐다.

provider와 PowerShell 추가 실행의 소스는 `6120daf`다. 이후 최종 배포 SHA까지의 변경은
이 기록과 설치형 Python fixture 세 파일뿐이며, 해당 검증이 소비한 제품 코드, 의존성,
런타임 pin, workflow는 동일하다. 결과를 재사용하면서 최종 SHA의 필수 native와 Release
검증도 별도로 완료했다.

[공개 v0.42.0](https://github.com/jinyongp/devtools/releases/tag/v0.42.0)의 asset 19개를
다시 내려받아 GitHub asset digest와 크기를 전부 대조했다. CLI 여섯 archive와 Skill archive의
공개 checksum도 모두 일치했다. 각 OS/CPU 바이너리의 Go build metadata에서 Go 1.27.3과
최종 소스 SHA를 확인했고, 갱신한 Go 모듈 버전도 포함되어 있었다.

공개 installer의 최신 버전 선택으로 새 설치를 수행하고, 실제 공개 0.41.0 설치본의
`dvt update`로 0.42.0 업데이트도 확인했다. 두 경로 모두 `devtools`와 `dvt`가
version 0.42.0, protocol 24, 최종 소스 SHA를 반환했으며 storage는 ready였다.
실제 사용자 설치 경로는 변경하지 않고 검증 소유 디렉터리를 사용했다.

Go와 npm 의존성, vendored asset과 license, 설치 정책과 잠금파일, Nuxt 승인과 로그,
자동완성 계약과 셸 adapter, native와 배포 workflow, 공개 배포 경로를 최종 재검토했다.
검증 도중 확인한 도구 조합과 fixture 기대값을 수정하고 재검증했다. fixture 전이 의존성의
audit 세 건은 위 후속 조치대로 남아 있으며 강제 override나 검사 제외로 숨기지 않았다.
이 결과를 보존하는 후속 커밋은 문서만 변경한다.

## 주요 버전 근거

- [Go 1.27 수정 릴리스](https://go.dev/doc/devel/release#go1.27.minor)
- [Node 22.23.4](https://nodejs.org/en/blog/release/v22.23.4)
- [pnpm 12.0.0 변경사항](https://github.com/pnpm/pnpm/releases/tag/v12.0.0)
- [Playwright 1.64.1](https://github.com/microsoft/playwright/releases/tag/v1.64.1)
- [Nuxt 4.6.4](https://github.com/nuxt/nuxt/releases/tag/v4.6.4)
- [Nuxt DevTools 3.4.4](https://github.com/nuxt/devtools/releases/tag/v3.4.4)
- [skills 1.7.6](https://github.com/vercel-labs/skills/releases/tag/v1.7.6)
- [releaseway/actions 0.8.0](https://github.com/releaseway/actions/releases/tag/v0.8.0)
- [PowerShell 7.6.9와 공식 SHA-256](https://github.com/PowerShell/PowerShell/releases/tag/v7.6.9)
- [Tailscale 공식 Docker 이미지](https://hub.docker.com/r/tailscale/tailscale/tags?name=v1.104.4)
- [D3 7.9.2](https://github.com/d3/d3/releases/tag/v7.9.2)
