# 개발환경 진단

[문서 목록](README.md) · [설정 편집](configuration.md) · [진단 계약](diagnostic-contract.md)

protocol 10의 `devtools doctor`는 기본적으로 프로젝트의 **모든 명령**을 검사한다.
실행 파일이 없거나 필수 조건을 만족하지 못하면 해당 이름과 이유, 선언 위치와 다음 조치를 제공한다.
도구 설치나 설정 변경은 수행하지 않는다. 설치본의 지원 여부는 `version`과 `schema doctor`로 확인한다.

## 검사 범위 선택

```sh
devtools doctor
devtools doctor dev:web test
devtools doctor dev:web --env local --bundle all
devtools doctor --project-only
devtools doctor dev:web --mode managed
devtools doctor --dir ../another-worktree --check
```

명령 이름을 지정하면 중복을 제거하고 이름순으로 검사한다.
`--project-only`는 defaults와 프로젝트 상태만 검사하며 명령 인자와 함께 사용할 수 없다.
`--profile NAME`은 프로젝트 파일을 계속 검사하면서 저장소 profile을 바꾼다.
설정이 없는 디렉터리에서는 명령 인자 없이 `--profile NAME`으로 profile 범위를 확인할 수 있다.

`--env`는 선택한 모든 명령의 env를 바꾸고 주입을 활성화한다.
생략하면 각 명령의 env·주입 설정을 따른다. `--bundle`도 선택한 명령 전체에 적용한다.
`--mode foreground`가 기본이며, managed는 launcher와 readiness 실행 파일까지 검사한다.
`--timeout 30s`는 모든 probe를 포함한 진단 전체의 제한이다.

## 결과 읽기

보고서는 공통 `data.checks`, 명령별 `data.commands[].checks`, 집계 `data.summary`를 제공한다.
`scope`는 all/selected/project/profile이며 `selection`에 선택한 명령 이름을 기록한다.
`state`는 ready/not_ready/unknown이고, `ready`는 state가 ready일 때만 true다.

| check.status | 의미 |
| --- | --- |
| pass | 필요한 사실을 확인했고 조건을 만족함 |
| fail | 확인한 사실이 필수 조건을 만족하지 않음 |
| unknown | 선행 조건·인증·제한 시간 때문에 확인하지 못함 |
| not_applicable | 선택한 범위나 실행 방식에 적용되지 않음 |

필수 fail이 하나라도 있으면 not_ready다. fail이 없어도 필수 unknown이 있으면 unknown이다.
각 check는 `id`, `required`, `reason`, `message`, `target`, `sources`, `facts`, `remedies`를 가진다.
예를 들어 tool:node의 not_found는 선택한 검색 환경에서 실행 파일을 찾지 못했다는 뜻이다.
설치 자체가 없다는 결론이나 자동 설치 권한을 뜻하지 않는다.

기본 doctor는 보고서를 작성하면 종료 0이다. CI에서는 `--check`를 사용한다.
미준비·미확인이 있으면 종료 3이며 전체 보고서를 `error.details.report`에 보존한다.
취소는 종료 130이다.

## 도구와 값 검사

[설정 가이드](configuration.md#도구와-preset-선언하기)의 registry·preset·defaults 선택을 사용한다.
기존 legacy requirements도 같은 실행 의미로 검사한다.
명령의 argv[0]과 독립적인 필수 도구를 모두 확인하므로 실행 파일이 없어도 나머지 결과를 얻는다.

정확한 version을 선언하면 `version_args`(기본 `["--version"]`)로 probe한다.
첫 숫자형 버전을 비교하고, timeout·비정상 종료·출력 상한·버전 불일치를 구분한다.
도구당 제한은 3초, 합산 출력 상한은 16 KiB다. probe 원문 출력은 반환하지 않는다.
같은 invocation의 동일 실행 파일·인자·환경 검사는 공유하며 기대 버전은 각각 비교한다.

진단은 vault를 열거나 세션 idle을 연장하지 않는다. public 검색 값과 인증된 metadata만 사용하고
secret 본문을 읽거나 probe에 전달하지 않는다. 인증할 수 없는 입력은 unknown,
명시적으로 잠긴 상태나 손상된 저장소 등 확인된 차단 조건은 fail로 표시한다.
secret PATH/PATHEXT가 필요한 경우 실제 검색 결과를 추정해 pass로 표시하지 않는다.
이 때문에 명시적 unlock 후의 실행 전 검사에서 추가 사실을 확인할 수 있다.

var/sec는 이름과 종류가 일치해야 하며 빈 값도 등록된 키로 취급한다.
값·전체 PATH·부모 환경·task context credential은 보고서에 포함하지 않는다.
binding과 port 선택은 예약 없이 preview하며 필요한 입력을 확인할 수 없으면 사유를 남긴다.

## 복구 안내와 실행 전 검사

remedy는 `argv`, `required_inputs`, `message`, `effect`를 제공한다.
effect는 read/config_write/value_write/system_write 중 하나다.
빈 argv는 자동 호출할 수 없는 수동 조치이며 필요한 입력을 임의로 채우지 않는다.
secret 등록은 `--stdin`을 사용하고 값을 argv에 넣지 않는다.

`run`과 managed 명령은 같은 preflight 검사를 사용한다.
실행 파일 누락은 `executable_not_found`와 종료 127, 다른 필수 조건 실패는
`requirements_failed`와 종료 3이다. `error.details.checks`와 `causes`에 결과와 하위 원인을 보존한다.
안내의 doctor 명령은 실패한 명령의 dir/profile/env/bundle과 실행 방식을 유지한다.
오타에는 입력 위치와 가까운 후보·범위가 맞는 help를 제공하며 자동 교정하지 않는다.

## 전체 devtools 상태 확인

```sh
devtools diagnostics
```

diagnostics는 profile metadata, port assignment, managed process, proxy/dashboard,
공개 backup 설정과 vault 보호 상태를 조회한다. 세션을 열거나 idle을 연장하지 않는다.
proxy/dashboard 중지나 미설정 backup은 조회할 수 있는 정상 상태다.
`data.ready:false`는 하나 이상의 영역을 안전하게 검사하지 못했다는 뜻이다.

각 영역의 실패는 `issues`의 code·공통 details로 설명한다.
profile 변수·secret 값, raw log, 인증 token, task context, backup recipient는 포함하지 않는다.
프로젝트 명령의 실행 준비 상태는 doctor로 검사한다.

## 검증

`devtools run test:installed doctor config_edit`는 설치한 테스트 release로 진단과 설정 편집을 검증한다.
`devtools run check:all`은 정적 검사와 Go race 검사를 실행한다.
Windows·macOS의 파일 게시·ACL 동작은 해당 OS의 native 검사로 확인해야 한다.
