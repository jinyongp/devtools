# Profile 조회, 비교와 환경 간 이동

Profile은 변수·secret·env와 task/workstream을 함께 관리하는 이름입니다. 프로젝트는
`devtools.toml`의 `profile`로 이 데이터를 선택합니다. 같은 기기의 여러 worktree는 같은
profile을 공유하지만, 다른 기기로 옮길 때는 암호화한 파일을 내보내고 가져와야 합니다.
이 문서는 CLI protocol v3를 기준으로 설명합니다.

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

## 내보내기 준비

대상 환경에서 [백업 키](backup.md#키와-기본-위치-준비)를 준비한 뒤 공개 recipient만
원본 환경에 전달하세요. 개인 identity는 대상 환경에 남겨 둡니다. 아래 경로의 부모
디렉터리는 미리 준비하고, `UUID`에는 새 요청 ID, `DIGEST`에는 미리보기 결과를 넣습니다.

```sh
# 원본 환경: 현재 프로젝트의 profile을 내보냅니다.
devtools profile export --output ./myapp.age --recipient-file /secure/recipient.txt

# 프로젝트 밖에서는 profile을 직접 지정합니다.
devtools profile export --profile myapp --output ./myapp.age --recipient-file /secure/recipient.txt
```

`--profile`을 생략하면 현재 디렉터리 또는 `--dir PATH`에서 프로젝트를 찾습니다.
`--recipient`에 공개 age X25519 recipient를 직접 전달할 수도 있습니다. `--recipient`와
`--recipient-file`은 동시에 사용할 수 없습니다. recipient나 출력 경로를 생략하면
`backup configure`로 저장한 기본값을 사용합니다. 출력 파일과 recipient를 모두 지정하면
기본 백업 설정 없이 내보낼 수 있습니다. 기존 출력 파일은 덮어쓰지 않습니다.

파일에는 변수·secret·env와 task/workstream/validation 데이터가 들어갑니다.
`devtools.toml`, port 할당, instance 연결, 실행 중 process, dashboard 세션은 이동하지 않습니다.
프로젝트 파일은 Git 등 기존 배포 방식으로 별도로 준비하세요.

## 미리보기 후 가져오기

가져오기의 기본 동작은 미리보기입니다. 이 단계는 대상 profile을 생성하거나 교체하지 않습니다.

```sh
# 대상 환경
devtools profile import --file ./myapp.age --identity-file /secure/identity.txt
```

단일-profile 파일은 원본 이름을 자동 선택하며 대상도 같은 이름을 사용합니다.
여러 profile이 있는 일반 backup 파일에서는 `--profile NAME`으로 원본을 선택합니다.
다른 이름으로 가져오려면 `--as NAME`을 추가하세요.

결과의 `data.digest`는 파일과 대상 상태에 묶인 적용 토큰입니다. `data.target_exists`와
`data.diff`를 검토한 뒤 같은 파일·원본·대상에 digest와 요청 ID를 전달합니다.

```sh
devtools profile import --file ./myapp.age --identity-file /secure/identity.txt \
  --apply DIGEST --request-id UUID
```

`--apply`와 `--request-id`는 반드시 함께 사용합니다. 기존 대상도 미리보기는 가능하지만
실제 교체에는 `--replace`가 필요합니다. 교체할 때는 대상 환경의 `backup configure`가
준비되어 있어야 하며, 기존 데이터를 암호화한 안전 백업을 만든 다음 적용합니다.
안전 백업 경로는 `data.safety_backup`에 있습니다. 백업을 만들지 못하면 교체하지 않습니다.
`--replace`는 미리보기를 확인한 뒤 적용 요청에 추가할 수 있습니다.

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

## 이동 후 프로젝트 실행

프로젝트의 profile 이름을 맞춘 뒤 `doctor`로 도구·env·필수 키를 확인하세요.
서버 명령이 `web`, `api`로 등록된 프로젝트에서는 다음 흐름을 사용할 수 있습니다.

```sh
devtools doctor
devtools project up web api --request-id UUID
devtools project status
```

여러 서버의 시작·준비 확인·종료와 부분 실패 재시도는 [프로세스 관리](processes.md#프로젝트-서버를-함께-관리하기)를 참고하세요.
