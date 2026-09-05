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

설치 스크립트에 배포물 위치와 버전을 전달한다. 아래 예시는 저장소 루트에서 로컬 배포물을 설치한다.

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

HTTPS 배포 서버를 운영한다면 `--source`에 아카이브와 체크섬이 있는 디렉터리 URL을 지정한다. HTTPS 다운로드에는 curl이 필요하다. 이 저장소에는 실제 배포 서버의 기본 주소가 설정되어 있지 않으므로 사용할 위치를 직접 전달한다.

체크섬은 배포물의 전송 무결성을 확인한다. 설치 스크립트와 배포물은 신뢰하는 출처에서 받아 사용한다.

## 업데이트하기

새 버전의 배포물을 준비한 뒤 같은 설치기에 `update`를 전달한다.

```sh
sh scripts/install.sh update --version 0.2.0 --source ./dist
devtools version
```

사용자 지정 경로에 설치했다면 업데이트에도 같은 `--bin-dir`를 전달한다. `install`은 첫 설치에, `update`는 기존 실행 파일 교체에 사용한다. 버전 선택은 호출자가 담당하며, 같은 버전으로 다시 업데이트할 수도 있다.

설치기는 체크섬, 아카이브 구성, 실행 가능 여부와 프로그램의 버전을 확인한 뒤 실행 파일을 원자적으로 교체한다. 확인 중 오류가 발생하면 기존 실행 파일을 유지한다. profile 데이터와 프로젝트의 `devtools.toml`은 업데이트 전후에 보존된다.

설치와 업데이트는 입력 프롬프트 없이 완료하거나 오류를 반환한다. 성공은 stdout, 실패는 stderr의 JSON으로 확인한다.

```json
{"schema_version":1,"ok":true,"data":{"action":"update","version":"0.2.0"}}
```

동일한 설치 경로의 갱신은 설치 잠금으로 직렬화한다. 실행 중인 설치기가 있으면 새 호출은 오류를 반환한다. 강제 종료 등으로 잠금이 남았다면 실행 중인 설치기가 있는지 확인한 뒤 설치 디렉터리의 빈 `.devtools-install.lock` 디렉터리를 제거하고 다시 실행한다.
