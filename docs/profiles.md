# Profile 조회, 비교와 환경 간 이동

Profile은 변수·secret·env와 task/workstream을 함께 관리하는 이름입니다. 프로젝트는
`devtools.toml`의 `profile`로 이 데이터를 선택합니다. 같은 기기의 여러 worktree는 같은
profile을 공유하지만, 다른 기기로 옮길 때는 암호화한 파일을 내보내고 가져와야 합니다.
이 문서는 CLI protocol v4를 기준으로 설명합니다.

## 저장된 profile 찾기

프로젝트 밖에서도 다음 명령으로 알려진 profile을 조회할 수 있습니다.

```sh
devtools profile list
devtools profile inspect myapp
devtools profile diff myapp myapp-copy
```

`list`는 `data.items`를 이름순으로 반환합니다. 각 항목의 `values`·`tasks`는 해당 저장소의
존재 여부이고, `env_count`·`instance_count`·`process_count`는 연결된 데이터 수입니다.
`active_process_count`는 저장된 실행 기록 기준이므로 실시간 준비 상태로 해석하지 마세요.
목록 조회는 서버에 readiness 검사를 요청하지 않습니다. 값이나 작업 없이 instance 또는
managed process만 남아 있는 profile도 목록에 포함됩니다.

`inspect NAME`은 `data.item`에 env 목록, 변수·secret 키별 공통 값 존재 여부와 env별 scope,
현재 정의에 포함된 task/workstream/validation의 상태별 개수, 연결된 instance, managed process 상태를 반환합니다.
존재하지 않는 이름은 `profile_not_found`입니다. 변수값·secret 값, 작업 실행 인증정보,
원문 로그와 재시도 기록은 포함하지 않습니다.

`diff LEFT RIGHT`는 왼쪽에서 오른쪽으로의 메타데이터 차이를 보고합니다. `data.different`와
`envs`·`variables`·`secrets`·`items`·`instances`의 `added`·`removed`·`changed` 항목을 확인하세요.
실행 중인 process 상태는 비교하지 않습니다. 같은 키와 scope에 저장된 값만 달라졌다면
변경으로 표시하지 않습니다. 특히 secret 값의 동일 여부나 해시를 제공하지 않으므로,
`different: false`는 저장된 값까지 같다는 보증이 아닙니다.

## 다른 기기로 내보내기

먼저 **대상 기기**에서 profile transfer identity를 준비합니다. devtools가 개인 identity를
사용자 전용 data directory 안에 보관하고 공개 age recipient만 반환합니다. 개인키 파일을
직접 만들거나 원본 기기로 복사할 필요가 없습니다.

```sh
# 대상 기기
devtools profile transfer prepare
# data.item.recipient의 age1... 값을 원본 기기에 전달
```

그 공개 recipient 문자열을 **원본 기기**의 export에 전달합니다.

```sh
# 원본 기기: 현재 프로젝트의 profile
devtools profile export --recipient age1...

# 프로젝트 밖에서는 profile을 직접 지정
devtools profile export --profile myapp --recipient age1...
```

`--profile`을 생략하면 현재 디렉터리 또는 `--dir PATH`에서 프로젝트를 찾습니다.
`--output`을 생략하면 현재 디렉터리에 `./<profile>.age`를 만들며 기존 파일은
덮어쓰지 않습니다. `--recipient-file`은 외부 키 관리나 자동화를 위한 고급 입력으로
계속 지원하지만 `--recipient`와 동시에 사용할 수 없습니다. profile export는
`backup configure`의 recipient나 output directory를 사용하지 않습니다.

원본에서 대상 기기로 복사해야 하는 filesystem artifact는 생성된 `.age` 파일 하나입니다.
공개 recipient는 대상에서 원본으로 전달하는 비밀이 아닌 문자열입니다. archive에는
변수·secret·env와 task/workstream/validation 데이터가 들어갑니다. `devtools.toml`, port
할당, instance 연결, 실행 중 process, dashboard 세션은 이동하지 않습니다. 프로젝트 파일은
Git 등 기존 방식으로 별도 준비하세요.

## 미리보기 후 가져오기

대상 기기에서 archive 하나를 가져온 뒤 import합니다. `--identity-file`을 생략하면
`profile transfer prepare`가 준비한 로컬 identity를 자동 사용합니다. import가 identity를
자동 생성하거나 교체하지는 않습니다.

```sh
# 대상 기기
devtools profile import --file ./myapp.age
```

단일-profile 파일은 원본 이름을 자동 선택하며 대상도 같은 이름을 사용합니다.
여러 profile이 있는 일반 backup 파일에서는 `--profile NAME`으로 원본을 선택합니다.
다른 이름으로 가져오려면 `--as NAME`을 추가하세요. 기존 archive나 외부 key manager를
사용하는 경우에는 `--identity-file PATH`로 identity를 명시할 수 있습니다.

결과의 `data.digest`는 파일과 대상 상태에 묶인 적용 토큰입니다. `data.target_exists`와
`data.diff`를 검토한 뒤 같은 파일·원본·대상에 digest와 요청 ID를 전달합니다.

```sh
devtools profile import --file ./myapp.age \
  --apply DIGEST --request-id UUID
```

`--apply`와 `--request-id`는 반드시 함께 사용합니다. 기존 대상도 미리보기는 가능하지만
실제 교체에는 `--replace`가 필요합니다. 교체 전 기존 profile은 현재 import에 사용한
identity의 공개 recipient로 암호화되어 devtools의 private transfer recovery directory에
저장됩니다. `backup configure`는 필요하지 않습니다. 경로는 `data.safety_backup`에
반환되며, 같은 identity로 그 파일을 다시 `profile import --file`하여 복구할 수 있습니다.
안전 백업을 만들지 못하면 대상 교체를 시작하지 않습니다.

Transfer 관련 오류는 다음 복구 방향을 구분합니다. `recipient_required`는 대상 기기에서
`profile transfer prepare`를 실행한 뒤 반환된 recipient로 다시 export해야 한다는 뜻입니다.
`transfer_identity_missing`은 현재 archive의 private identity가 없다는 뜻이므로 새 identity를
준비한 뒤 **원본 기기에서 새 recipient로 다시 export**해야 합니다. 잃어버린 identity로 암호화된
기존 archive는 matching explicit identity가 없으면 복구할 수 없습니다.
`transfer_identity_invalid`는 내부 identity의 형식·권한·symlink 경계가 안전하지 않다는 뜻이며
devtools가 자동 교체하지 않습니다. `output_exists`는 기존 파일을 보존하므로 다른 `--output`
경로를 선택해야 합니다.

미리보기는 `changed: false`, `replayed: false`입니다. 적용은 `changed: true`를 반환합니다.
같은 요청 ID와 동일한 적용 입력을 재전송하면 저장된 결과와 `replayed: true`를 반환합니다.
이때 `changed: true`는 최초 적용 결과이며 중복 적용을 뜻하지 않습니다.

대상이 미리보기 이후 바뀌면 `revision_conflict`입니다. 새 미리보기를 검토하고 새 요청 ID로
적용하세요. 이미 사용한 요청 ID에 다른 digest·대상·교체 옵션을 보내면 `request_conflict`입니다.
메타데이터 diff에 표시되지 않는 값 변경도 stale 검사에는 반영됩니다. 반대로 diff가 비어
있어도 값 자체가 다를 수 있으므로 교체 여부는 원본 파일을 신뢰할 수 있는지까지 판단해야 합니다.

가져오기는 기존 대상 전체를 교체하며 부분 병합하지 않습니다. task UUID와 이력은 유지하지만
진행 중 claim은 반납하고 원본 환경의 인증 컨텍스트와 재시도 기록은 복원하지 않습니다.
복구와 중단 후 재시도 규칙은 [백업과 복구](backup.md#복구-후-상태)를 따릅니다.

## 저장 형식 업그레이드와 구버전 실행

profile values와 task 저장소는 읽기만 할 때 기존 형식을 그대로 사용할 수 있지만, 새 버전에서
처음 변경하면 현재 저장 형식으로 자동 전환될 수 있습니다. 전환은 중간 상태가 노출되지 않도록
복구 가능한 transaction으로 게시되며, 같은 profile의 이전 파일 경로에는 구버전이 별도 상태를
새로 만들지 못하도록 차단 정보가 남습니다.

이 전환이 한 번 발생한 데이터 디렉터리를 **구버전 devtools binary가 다시 쓰는 in-place downgrade는
지원하지 않습니다.** 구버전으로 돌아가야 한다면 전환 전에 별도로 보관한 구버전 호환 backup을
새 데이터 디렉터리에 복원하세요. 현재 버전의 `profile export`와 `backup create`는 이동·복구용
논리 데이터를 계속 제공하지만, 새 저장 형식을 구버전 binary가 직접 수정할 수 있다는 의미는 아닙니다.

## 이동 후 프로젝트 실행

프로젝트의 profile 이름을 맞춘 뒤 `doctor`로 도구·env·필수 키를 확인하세요.
서버 명령이 `web`, `api`로 등록된 프로젝트에서는 다음 흐름을 사용할 수 있습니다.

```sh
devtools doctor
devtools project up web api --request-id UUID
devtools project status
```

여러 서버의 시작·준비 확인·종료와 부분 실패 재시도는 [프로세스 관리](processes.md#프로젝트-서버를-함께-관리하기)를 참고하세요.
