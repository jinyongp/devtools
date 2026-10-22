# 프로젝트 설정 편집

[문서 목록](README.md) · [개발환경 진단](doctor.md) · [편집 계약](config-edit-contract.md)

protocol 10 빌드는 `devtools.toml`을 검증하면서 편집하는 명령을 제공한다.
`devtools version`과 `devtools schema config edit`로 설치본의 지원 여부를 확인한다.
기존 파일이 없다면 `devtools init --profile sample`로 만든다. 기존 파일은 덮어쓰지 않는다.
아래 이름·프로그램은 가상의 프로젝트 예시다.

## 읽고 검증하기

```sh
devtools config inspect
devtools config check
devtools command list
devtools command explain dev:web
```

`inspect`는 파일 경로, 작성 형식, 선언과 revision을 반환한다. 값 저장소나 도구를 실행하지 않는다.
`check`는 TOML 문법·필드·참조 관계를 검사한다. 실패하면 위치와 수정할 조건을 제공한다.
실제 실행 파일·버전·값 등록 상태는 `doctor`로 확인한다.
`explain`은 명령의 최종 실행 설정과 각 설정의 출처를 보여 주며 프로그램을 실행하지 않는다.

`--dir PATH`는 해당 디렉터리에서 설정을 찾는다. Git 경계 밖의 파일까지 탐색하지 않는다.
`--file PATH`로 읽는 JSON은 현재 process 디렉터리 기준이며 프로젝트 기준이 아니다.

## 도구와 preset 선언하기

```toml
profile = "sample"

[tools]
node = {}
pnpm = { version = "11.24.0" }
go = {}

[presets.web]
tools = ["node", "pnpm"]

[commands."dev:web"]
exec = ["pnpm", "run", "dev"]
requires = ["web"]

[commands."dev:api"]
exec = ["go", "run", "./cmd/api"]
tools = ["go"]
```

`node = {}`는 이름이 `node`인 실행 파일이 있어야 한다는 사양이다. 버전 제한이나 설치 지시는 없다.
등록만으로 모든 명령의 필수 조건이 되지는 않는다. 명령의 `tools`, preset의 `tools`로 선택한다.
`requires`는 preset 이름을 선택한다. 모든 명령의 공통 선택은 `[defaults]`에만 선언한다.
도구·preset 등록을 defaults로 자동 승격하지 않는다.

`version`은 범위가 아닌 정확한 버전이다. `executable`을 생략하면 도구 이름,
`version_args`를 생략하면 `["--version"]`을 사용한다. 명령의 `tool_overrides`는
선택한 도구 사양 전체를 교체한다. 자세한 합성 규칙은 [설정 모델](cli-config-redesign.md#설정-모델)을 따른다.

## 명령 추가와 부분 수정

argv만 필요한 명령은 다음처럼 추가한다. 이미 있는 이름은 거절한다.

```sh
devtools command add test -- go test ./...
```

추가 필드가 있으면 JSON 파일 또는 명시적인 `--stdin`을 사용한다. 예를 들어
`command.json`에 다음 본문을 작성한다.

```json
{"exec":["pnpm","run","dev"],"requires":["web"]}
```

```sh
devtools command add dev:web --file command.json --dry-run
devtools command add dev:web --file command.json
```

`--dry-run`은 저장하지 않고 실제 적용과 같은 후보를 검증한다. 필요한 tool/preset이
없으면 먼저 등록하거나 아래 batch로 함께 추가한다. `add`는 upsert가 아니다.

수정·삭제·이름 변경은 `config inspect`가 반환한 최신 revision이 필요하다.
아래 `REVISION`은 응답의 `data.revision` 값으로 바꾼다. 파일 내용·identity·권한이 바뀌면
충돌을 반환하므로 다시 읽고 변경 의도를 검토한다. 강제 덮어쓰기 옵션은 없다.

```sh
devtools command update dev:web --file patch.json --revision REVISION --dry-run
devtools command update dev:web --file patch.json --revision REVISION
```

`patch.json`에 `{"inject":true}`만 넣으면 나머지 필드는 유지한다. 배열은 전체 교체하고,
선택 필드는 `null`로 제거한다. 필수 필드 제거와 빈 patch는 거절한다.
이름 변경은 `command rename OLD NEW --revision REVISION`, 삭제는
`command remove NAME --revision REVISION`을 사용한다. 다른 선언의 참조를 자동 삭제하지 않는다.

## 여러 변경을 한 번에 저장하기

다음 `edit.json`은 서로 참조하는 도구·preset·명령을 함께 추가한다. 중간 상태를 저장하지 않고
전체 후보의 문법·참조·실행 설정을 검증한 뒤 파일 하나를 게시한다.

```json
{
  "operations": [
    {"op":"tool.add","name":"node","value":{}},
    {"op":"preset.add","name":"web","value":{"tools":["node"]}},
    {"op":"command.add","name":"dev:web","value":{"exec":["node","scripts/dev.mjs"],"requires":["web"]}}
  ]
}
```

```sh
devtools config edit --file edit.json --revision REVISION --dry-run
devtools config edit --file edit.json --revision REVISION
```

같은 resource를 batch에서 두 번 수정할 수 없다. 한 항목이라도 실패하면 저장하지 않는다.
JSON의 모르는 필드·중복 key·잘못된 Unicode·정수가 아닌 숫자 token을 거절한다.
입력은 2 MiB, JSON 중첩은 128단계로 제한한다. `--stdin`은 redirected stdin만 읽으며
`--file`과 동시에 사용할 수 없다. `devtools help config edit`와 `schema config edit`에서
같은 본문 예제와 입력 schema를 확인할 수 있다.

## 기존 requirements 변환과 정렬

기존 `[requirements]`와 `[commands.NAME.requirements]`는 계속 읽는다.
새 registry 선택 필드와 legacy requirements를 혼합하지 않는다.

```sh
devtools config migrate
devtools config migrate --apply DIGEST --revision REVISION
devtools config fmt --check
devtools config fmt --dry-run
devtools config fmt
```

`migrate`의 기본 동작은 미리보기다. 응답의 revision과 `apply_digest`를 각각
`REVISION`, `DIGEST`로 전달해야 저장한다. 모든 명령의 해석 결과가 같을 때만 변환한다.
도구 사양을 등록하고 기존 공통 선택만 defaults로 옮기며, 반복 선택은 preset으로 묶는다.
프로그램 내부의 의존성이나 필요한 설치 명령을 추정하지 않는다.

`fmt`는 공백·작성 형식만 정리한다. preset 추출·버전 변경·migration을 수행하지 않는다.
두 번째 실행은 byte 변경이 없어야 한다. 주석 연결을 보존할 수 없는 편집은
`config_preservation_failed`로 거절한다.

게시 결과를 확정할 수 없으면 `config_commit_uncertain`과 `effects`, `retry:inspect_first`를
반환한다. 무조건 재시도하지 말고 파일을 다시 조회해 후보 반영 여부와 복구 안내를 확인한다.
파일 게시·권한 보존의 자세한 규칙은 [저장 계약](config-edit-contract.md#저장동시성권한)을 따른다.
