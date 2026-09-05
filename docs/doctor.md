# 개발환경 진단

`devtools doctor`는 프로젝트 디렉터리, 설정과 profile, 값·task 저장소,
선택한 env, 필수 실행 도구와 var/sec 등록 여부를 진단한다.
`doctor COMMAND`는 해당 명령의 실행 파일과 필수 조건까지 확인한다.

```sh
devtools doctor
devtools doctor test
devtools doctor test --env staging
devtools doctor --dir ../another-worktree
devtools doctor --profile myapp
```

진단 결과는 JSON의 `data.ready`와 `data.checks`로 제공한다. 각 check는
`id`, `status`, `message`, 필요한 경우 `remedy`와 `expected_version`을 가진다.
status는 `pass`, `fail`, `skipped`다. 저장소나 env 문제로 이어지는 검사를
수행할 수 없으면 skipped로 표시하고 먼저 해결할 조건을 안내한다.

진단을 정상적으로 수행하면 종료 코드는 0이다. 환경 준비 여부는
`data.ready`로 판단한다. 잘못된 옵션이나 취소는 기존 CLI 오류 계약을 따른다.

## 필수 조건 선언

프로젝트 공통 조건은 `[requirements]`, 명령별 추가 조건은
`[commands.NAME.requirements]`에 선언한다.

```toml
profile = "myapp"

[requirements]
vars = ["LOG_LEVEL"]

[requirements.tools.go]
version = "1.27.1"
version_args = ["version"]

[requirements.tools.git]
# version을 생략하면 실행 파일의 존재와 실행 권한을 확인한다.

[commands.test]
exec = ["go", "test", "./..."]
inject = true
env = "local"

[commands.test.requirements]
secs = ["DATABASE_URL"]
```

도구 이름을 실행 파일 이름으로 사용한다. 다른 실행 파일이나 프로젝트
내 스크립트를 지정할 때는 `executable`을 사용한다. 상대 경로와 상대 PATH
항목은 `devtools.toml`이 있는 디렉터리를 기준으로 해석한다.

version은 정확한 버전을 지정한다. `version_args`의 기본값은 `["--version"]`이다.
버전을 선언하면 해당 인자로 도구를 실행하고 출력에서 첫 번째 숫자형 버전을
읽어 일치 여부를 확인한다. 버전 확인에는 3초 제한을 적용하고, 종료 시
최대 3초의 유예 후 프로세스 그룹을 정리한다. 출력이 16 KiB를 넘거나 명령이
실패하면 해당 검사는 fail이다.

공통·명령별 도구 이름이 같으면 명령별 정의를 사용한다. var/sec 키는
두 범위를 합쳐 검사하며, 한 키는 한 종류로 선언한다.

## Profile과 env 선택

doctor는 프로젝트 설정을 먼저 확인하고 `--profile`이 있으면 그 profile의
데이터를 진단한다. 설정이 없는 디렉터리에서도 `--profile`으로 저장소와
env를 확인할 수 있다.

`doctor COMMAND`는 명령에 설정된 env를 사용하고 `--env`로 변경할 수 있다.
기본 doctor는 공통 레이어를 사용하며, env를 선택하면 공통 값과 해당 env의
override를 합친 등록 여부를 확인한다. 빈 값도 등록된 키로 취급한다.

var/sec는 선언한 종류까지 일치해야 한다. 진단 결과에는 키 이름과
등록 여부를 제공한다. 변수값·secret 값·버전 확인 명령의 원문 출력은
진단 응답에 포함하지 않는다. 버전 확인 프로세스에는 부모 환경과 실행에
사용할 PATH를 전달하고, profile의 나머지 값을 주입하지 않는다.

## 실행 전 검사

필수 조건을 선언한 이름 명령은 `devtools run COMMAND`에서 같은 검사를
거친다. 조건이 충족되면 실제 명령을 실행한다. 미충족 시 종료 코드 3과
`requirements_failed` 오류에 checks를 포함한다.

var/sec를 요구하는 명령은 `inject = true` 또는 명시적인 `--env`로
주입을 활성화한다. 기존 필수 조건 선언이 없는 명령과
`devtools run -- EXECUTABLE ...`은 기존 실행 계약을 따른다.

doctor는 진단 보고서를 제공하고, 도구 설치나 설정 수정은 에이전트가
보고서의 remedy와 프로젝트 요구사항을 확인한 뒤 수행한다.

## 검증

`just verify-docker doctor`는 설치된 Linux 바이너리로 진단·버전 일치·실행
차단·env 선택·손상된 저장소·값 비노출을 확인한다. 공통 Docker 빌드 단계는
Go race 테스트와 macOS/Linux의 amd64/arm64 빌드를 수행한다.
