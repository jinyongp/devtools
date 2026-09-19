# 설치와 업데이트

devtools는 운영체제와 CPU에 맞는 실행 파일을 설치해 사용한다. 설치된 프로그램은 Go 도구 없이 실행할 수 있다. 지원 배포물은 macOS와 Linux의 amd64·arm64이며, WSL은 Linux 배포물을 사용한다.

## 배포물 준비

저장소에서 버전을 명시해 배포물을 만든다. 이 단계에는 Go 1.27.x가 필요하다.

```sh
just release 0.1.0
```

기본 대상은 빌드 환경의 운영체제와 CPU다. 다른 대상을 지정할 때는 환경변수를 사용한다.

```sh
TARGET_OS=linux TARGET_ARCH=amd64 just release 0.1.0
```

`dist/`에 실행 파일 하나를 담은 아카이브와 SHA-256 체크섬이 생성된다.

```text
devtools_0.1.0_linux_amd64.tar.gz
devtools_0.1.0_linux_amd64.tar.gz.sha256
```

`OUTPUT_DIR`로 출력 디렉터리, `COMMIT`으로 빌드의 커밋 식별자를 지정할 수 있다. 배포 버전은 명시적으로 선택한다.

오프라인·수동 설치용 Agent Skill 아카이브도 같은 버전으로 만든다. 일반 설치는
`npx skills add jinyongp/devtools`로 저장소에서 직접 수행한다.
아카이브 생성 자체에는 `uvx`와 `skills-ref`가 필요하지 않지만, 게시 전 `just check`가
공식 Agent Skills 검증을 수행한다.

```sh
just release-skill 0.1.0
```

`dist/`에는 `devtools/` 스킬 디렉터리를 담은 아카이브와 체크섬이 생성된다.

```text
devtools-skill_0.1.0.tar.gz
devtools-skill_0.1.0.tar.gz.sha256
```

Agent Skill의 한 줄 설치, 전역 설치, 업데이트와 수동 설치 방법은
[Agent Skill 설치](agent-skill.md)를 참고한다.

## 처음 설치하기

Homebrew에서는 `brew install jinyongp/tap/devtools`로 설치할 수 있습니다.
이 경로는 태그 커밋의 소스를 내려받아 Go로 빌드하며 필요한 Go 도구는 Homebrew가 준비합니다.
업데이트는 `brew upgrade jinyongp/tap/devtools`로 수행합니다.

공개 설치 진입점은 devtools 프로젝트의 GitHub Pages에 둔다. 안정 릴리스가 성공하면 그 릴리스에 게시된 `install.sh`와 동일한 파일을 Pages에 배포한다.

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh
```

설치기는 동작 인자를 생략하면 `install`로 처리한다. 배포물의 기본 주소는 `https://github.com/jinyongp/devtools/releases`이며, 버전을 생략하면 최신 안정 릴리스의 `version.txt`를 조회한 뒤 해당 버전의 고정 주소에서 배포물과 체크섬을 받는다. `--version 0.1.0`으로 특정 버전을 선택할 수 있다. 버전 인자는 태그의 `v`를 제외한 값이다.

로컬 배포물을 사용할 때는 위치와 버전을 함께 전달한다.

```sh
sh scripts/install.sh install --version 0.1.0 --source ./dist
```

기본 설치 경로는 `~/.local/bin/devtools`다. 설치기는 같은 디렉터리에 `dvt` 심볼릭 링크도 만들어 짧은 이름으로 실행할 수 있게 한다. 설치 경로나 현재 PATH의 기존 명령이 `dvt`를 사용 중이면 건드리지 않고 별칭만 생략한다. 이 충돌은 stderr 경고와 성공 결과의 `skipped_conflict` 상태로 알리며 `devtools` 설치는 정상 완료한다. 설치기는 현재 운영체제와 CPU에 맞는 아카이브를 선택한다. PATH에 설치 디렉터리를 포함하면 두 이름으로 실행할 수 있다.

```sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
dvt version
devtools schema
```

에이전트 실행 환경에서도 프로세스 시작 설정에 같은 PATH를 지정한다. 설치기는 명시한 디렉터리에 실행 파일을 배치한다. 셸 시작 설정은 사용자가 관리한다.

다른 위치에 설치하려면 `--bin-dir`를 지정한다.

```sh
sh scripts/install.sh install --version 0.1.0 --source ./dist --bin-dir /path/to/bin
```

HTTPS 배포 서버를 운영한다면 `--source`에 아카이브와 체크섬이 있는 디렉터리 URL과 `--version`을 지정한다. HTTPS 다운로드에는 curl이 필요하다. `DEVTOOLS_RELEASE_URL`은 GitHub Releases와 같은 `latest/download/version.txt`, `download/v<버전>/<파일>` 경로를 제공하는 HTTPS 미러 주소로 기본 배포 주소를 바꾼다.

체크섬은 배포물의 전송 무결성을 확인한다. 설치 스크립트와 배포물은 신뢰하는 출처에서 받아 사용한다.

## 업데이트하기

Homebrew 설치본에서 `devtools update`를 호출하면 종료 코드 3의 `package_managed`와
`brew upgrade jinyongp/tap/devtools` 안내를 반환합니다. Homebrew가 설치 파일의 버전을 관리합니다.

설치된 실행 파일에서 다음 명령을 실행하세요. 현재 실행 파일의 실제 설치 위치를
찾아 최신 안정 버전으로 교체합니다. 프로젝트 디렉터리나 profile 선택은 필요하지 않습니다.

```sh
devtools update
devtools update --version 0.2.0
devtools version
```

설치기와 동일하게 체크섬·아카이브·실행 버전을 검증하고 교체하며, 실패하면
`update_failed` 오류를 반환합니다. 설치 경로에 쓰기 권한이 필요하고 다운로드에는
curl을 사용합니다. 로컬 배포물은 `devtools update --version 0.2.0 --source ./dist`로
지정할 수 있습니다. 실행 중인 process, proxy daemon과 dashboard는 업데이트 후
재시작하면 새 버전을 사용합니다.

에이전트용 devtools Agent Skill도 최신 지침을 사용하려면 `skills` CLI로 업데이트합니다.

```sh
npx skills update devtools
```

전역 설치는 `npx skills update devtools --global`을 사용합니다. 수동·오프라인 설치를
사용한 경우에만 릴리스 아카이브를 직접 교체합니다. 자세한 내용은
[Agent Skill 설치](agent-skill.md)를 참고하세요.

`update` 명령이 추가되기 전에 설치한 버전은 아래 설치기로 한 번 업데이트하세요.

같은 설치기에 `update`를 전달하면 최신 안정 버전으로 업데이트한다.

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh -s -- update
devtools version
```

`--version 0.2.0`으로 특정 버전을 선택하거나, `--version 0.2.0 --source ./dist`로 로컬 배포물을 사용한다. 사용자 지정 경로에 설치했다면 업데이트에도 같은 `--bin-dir`를 전달한다. `install`은 첫 설치에, `update`는 기존 실행 파일 교체에 사용한다. 같은 버전 재설치와 이전 버전 선택도 가능하다.

설치기는 체크섬, 아카이브 구성, 실행 가능 여부와 프로그램의 버전을 확인한 뒤 실행 파일을 원자적으로 교체한다. 확인 중 오류가 발생하면 기존 실행 파일을 유지한다. profile 데이터와 프로젝트의 `devtools.toml`은 업데이트 전후에 보존된다.

설치와 업데이트는 입력 프롬프트 없이 완료하거나 오류를 반환한다. 성공은 stdout, 실패는 stderr의 JSON으로 확인한다.

```json
{"schema_version":1,"ok":true,"data":{"action":"update","version":"0.2.0","alias":{"name":"dvt","status":"unchanged"}}}
```

동일한 설치 경로의 갱신은 설치 잠금으로 직렬화한다. 실행 중인 설치기가 있으면 새 호출은 오류를 반환한다. 강제 종료 등으로 잠금이 남았다면 실행 중인 설치기가 있는지 확인한 뒤 설치 디렉터리의 빈 `.devtools-install.lock` 디렉터리를 제거하고 다시 실행한다.

## GitHub Releases 게시

Node.js와 pnpm이 있는 저장소에서 다음 명령으로 버전을 추천받는다.

```sh
pnpm release
pnpm release --publish
```

origin이 있으면 태그를 가져온 뒤 HEAD에 포함된 가장 높은 안정 버전 태그 이후의
커밋을 확인한다. 버전은 `0.x`를 유지하며 Conventional Commits의 `!`,
`BREAKING CHANGE:`, `feat`는 minor, 나머지는 patch를 추천한다. 첫 릴리스는 `0.1.0`이며,
추가 커밋이 없으면 완료 메시지를 반환한다. 커밋 메시지에 기록된 변경을 기준으로
추천하므로 출력된 커밋 목록과 버전을 함께 확인한다.

`--publish`는 깨끗한 main에서 추천 버전의 태그를 생성하고 main과 태그를
atomic push로 함께 올린다. 원격 main과 충돌하면 두 참조 모두 보존하며,
로컬 태그와 재시도 명령을 안내한다. 배포 결과는 GitHub Actions의 Release 실행에서 확인한다.

자동 실행의 진입점은 `v0.1.0` 형태의 태그 푸시다. Release 워크플로는
태그 커밋이 원격 main에 포함됐는지 확인한 뒤 macOS와 Linux 검사를 병렬로 실행한다.
Linux 검사는 실제 Chromium Dashboard smoke와 설치된 release 시나리오를 포함하고,
macOS도 같은 release 시나리오를 실행한다. 두 운영체제의 검증이 모두 성공하면 네 플랫폼 배포물을 생성한다. 이후
플랫폼별 CLI 아카이브·체크섬, 플랫폼 독립 Agent Skill 아카이브·체크섬,
설치기와 버전 파일을 GitHub Releases에 함께 게시한다.
빌드에는 태그의 버전과 커밋 식별자를 기록한다.

안정 Release workflow가 성공적으로 끝나면 `.github/workflows/pages.yml`의 `workflow_run`이
기본 브랜치에서 실행된다. 이 workflow는 해당 릴리스의 `install.sh`를 내려받아 태그의
`scripts/install.sh`와 일치하는지 확인한 뒤 GitHub Pages에 게시한다.
devtools 프로젝트 Pages에는 별도 custom domain을 설정하지 않고 사용자 사이트의
`jinyongp.dev`를 상속해 `/devtools/install.sh` 경로를 사용한다.
사전 릴리스는 Pages installer를 갱신하지 않는다.

Pages source는 저장소의 **Settings → Pages → Build and deployment → Source**에서
**GitHub Actions**를 사용한다. `github-pages` 환경은 기본 브랜치만 배포하도록 유지하며,
`workflow_run`도 기본 브랜치 ref에서 실행된다. **Publish Installer Page** workflow를
수동 실행하면 최신 안정 릴리스를 다시 게시할 수 있다.

`v0.2.0-rc.1`처럼 접미사가 붙은 태그는 사전 릴리스로 게시한다. 기본 설치는 GitHub가 최신으로 선택한 안정 릴리스를 사용하고, 사전 릴리스는 버전을 지정해 설치한다.

## 설치 환경 검증하기

```sh
just verify install
```

[설치된 release 검증](../verify/README.md)의 `install` 시나리오로 검증한다.
runner는 현재 OS와 architecture용 테스트 release 두 버전을 임시 디렉터리에 만들고,
각 시나리오의 격리된 HOME에 실제 설치 스크립트로 실행 파일을 설치한다.

검증은 설치 명령부터 시작해 다음 동작을 확인한다.

- 동작 인자를 생략한 기본 설치, 기본 경로 설치와 PATH 호출, 설치된 버전 조회.
- `dvt` 별칭 생성과 기존 동명 파일 충돌 시 보존.
- 일반 변수·secret·env 등록과 조회 권한 구분.
- 기존 justfile 명령의 독립 실행과 devtools를 통한 주입 실행.
- 이름 명령, env 덮어쓰기, 추가 인자, 별도 Git worktree 사용.
- 전역 데이터의 사용자 전용 파일 권한.
- 손상된 배포물의 업데이트 실패와 기존 실행 파일 보존.
- 정상 업데이트 후 버전 변경, profile 데이터·프로젝트 설정 보존.
- 로컬 HTTPS 서버에서 배포물 다운로드와 체크섬 확인.
- 최신 릴리스 조회, 버전 지정, 조회 실패 시 설치된 실행 파일 보존.
- 같은 버전 재업데이트와 자식 프로세스 신호·종료 코드 전달.

각 시나리오는 자체 HOME, XDG 경로, 작업 디렉터리와 Git 전역 설정을 사용하며 종료 시
자동으로 삭제된다. GitHub Actions에서는 job마다 새 hosted runner를 사용해 clean OS
환경까지 보장한다. 로컬 실행도 사용자 devtools 데이터와 설정을 건드리지 않는다.

테스트용 두 버전은 같은 소스에 서로 다른 버전 정보를 넣어 만든다. 이 검증은 설치·교체·데이터 보존을 확인한다. 향후 데이터 형식이 바뀌는 릴리스에서는 해당 이전 버전의 배포물을 함께 검증해야 한다.

Release CI의 macOS와 Linux job은 모두 `just verify`로 등록된 전체 시나리오를 실행한다.
이때 `proxies` 시나리오는 설치된 실행 파일로 route 전달, WebSocket, 동적 port 변경,
daemon 재시작과 listener reservation 유지를 확인한다. WSL 고유 동작은 실제 WSL
환경에서 별도로 검증할 수 있다.
