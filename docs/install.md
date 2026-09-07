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

## 처음 설치하기

기본 배포 주소는 `https://github.com/jinyongp/devtools/releases`다. GitHub Releases에 게시된 설치 스크립트를 받아 실행한다.

```sh
curl -fsSL https://github.com/jinyongp/devtools/releases/latest/download/install.sh | sh -s -- install
```

버전을 생략하면 최신 안정 릴리스의 `version.txt`를 조회하고, 해당 버전의 고정 주소에서 배포물과 체크섬을 받는다. `--version 0.1.0`으로 특정 버전을 선택할 수 있다. 버전 인자는 태그의 `v`를 제외한 값이다.

로컬 배포물을 사용할 때는 위치와 버전을 함께 전달한다.

```sh
sh scripts/install.sh install --version 0.1.0 --source ./dist
```

기본 설치 경로는 `~/.local/bin/devtools`다. 설치기는 현재 운영체제와 CPU에 맞는 아카이브를 선택한다. PATH에 설치 디렉터리를 포함하면 `devtools` 이름으로 실행할 수 있다.

```sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
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
지정할 수 있습니다. 실행 중인 서버와 dashboard는 업데이트 후 재시작하면 새 버전을 사용합니다.

`update` 명령이 추가되기 전에 설치한 버전은 아래 설치기로 한 번 업데이트하세요.

같은 설치기에 `update`를 전달하면 최신 안정 버전으로 업데이트한다.

```sh
curl -fsSL https://github.com/jinyongp/devtools/releases/latest/download/install.sh | sh -s -- update
devtools version
```

`--version 0.2.0`으로 특정 버전을 선택하거나, `--version 0.2.0 --source ./dist`로 로컬 배포물을 사용한다. 사용자 지정 경로에 설치했다면 업데이트에도 같은 `--bin-dir`를 전달한다. `install`은 첫 설치에, `update`는 기존 실행 파일 교체에 사용한다. 같은 버전 재설치와 이전 버전 선택도 가능하다.

설치기는 체크섬, 아카이브 구성, 실행 가능 여부와 프로그램의 버전을 확인한 뒤 실행 파일을 원자적으로 교체한다. 확인 중 오류가 발생하면 기존 실행 파일을 유지한다. profile 데이터와 프로젝트의 `devtools.toml`은 업데이트 전후에 보존된다.

설치와 업데이트는 입력 프롬프트 없이 완료하거나 오류를 반환한다. 성공은 stdout, 실패는 stderr의 JSON으로 확인한다.

```json
{"schema_version":1,"ok":true,"data":{"action":"update","version":"0.2.0"}}
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

자동 실행의 진입점은 `v0.1.0` 형태의 태그 푸시다. 하나의 Release 워크플로에서
태그 커밋이 원격 main에 포함됐는지 확인하고 macOS 검사, Linux 검사와 Docker
설치 검증을 거쳐 네 플랫폼 배포물을 생성한다. 모든 검증이 성공하면
아카이브·체크섬·설치기·버전 파일을 GitHub Releases에 함께 게시한다.
빌드에는 태그의 버전과 커밋 식별자를 기록한다.

`v0.2.0-rc.1`처럼 접미사가 붙은 태그는 사전 릴리스로 게시한다. 기본 설치는 GitHub가 최신으로 선택한 안정 릴리스를 사용하고, 사전 릴리스는 버전을 지정해 설치한다.

## Docker에서 설치 환경 검증하기

```sh
just verify-docker install
```

[공통 Linux 샌드박스](../docker/README.md)의 `install` 시나리오로 검증한다. Docker 빌드 단계는 테스트용 두 버전의 배포물을 만든다. 별도의 Debian 실행 이미지에는 그 배포물, 설치 스크립트와 검증 도구를 넣는다. 실행 이미지는 Go 도구와 devtools 소스가 없는 일반 사용자 환경이다.

검증은 설치 명령부터 시작해 다음 동작을 확인한다.

- 기본 경로 설치와 PATH 호출, 설치된 버전 조회.
- 일반 변수·secret·env 등록과 조회 권한 구분.
- 기존 justfile 명령의 독립 실행과 devtools를 통한 주입 실행.
- 이름 명령, env 덮어쓰기, 추가 인자, 별도 Git worktree 사용.
- 전역 데이터의 사용자 전용 파일 권한.
- 손상된 배포물의 업데이트 실패와 기존 실행 파일 보존.
- 정상 업데이트 후 버전 변경, profile 데이터·프로젝트 설정 보존.
- 컨테이너 내부 HTTPS 서버에서 배포물 다운로드와 체크섬 확인.
- 최신 릴리스 조회, 버전 지정, 조회 실패 시 설치된 실행 파일 보존.
- 같은 버전 재업데이트와 자식 프로세스 신호·종료 코드 전달.

컨테이너는 자체 HOME과 테스트 데이터를 사용하며 종료 시 삭제된다. 실행 단계는 외부 네트워크를 차단하고, 호스트 디렉터리 마운트 없이 동작한다. 이미지와 빌드 캐시는 Docker에서 관리한다.

테스트용 두 버전은 같은 소스에 서로 다른 버전 정보를 넣어 만든다. 이 검증은 설치·교체·데이터 보존을 확인한다. 향후 데이터 형식이 바뀌는 릴리스에서는 해당 이전 버전의 배포물을 함께 검증해야 한다.

검증 아키텍처는 Docker 빌드 환경을 따른다. CI는 `just verify-docker`로 등록된 전체 시나리오를 실행한다. macOS·WSL 고유 동작은 각 운영 환경의 검증 범위다.
