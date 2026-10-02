# Profile 조회, 비교와 환경 간 이동

Profile은 변수·secret·env와 task/workstream을 함께 관리하는 이름입니다. 프로젝트는
`devtools.toml`의 `profile`로 이 데이터를 선택합니다. 같은 기기의 여러 worktree는 같은
profile을 공유하지만, 다른 기기로 옮길 때는 암호화한 파일을 내보내고 가져와야 합니다.
아래 profile 사용법은 현재 안정 버전의 CLI protocol v5와 `main`의 protocol v6에서 같습니다.

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

기본 profile 이동은 **원본 기기에서 먼저** 시작합니다. 대상 기기를 미리 준비할 필요가
없고, 이동해야 하는 filesystem artifact는 암호화된 `.age` 파일 하나뿐입니다.

```sh
# 원본 기기: 현재 프로젝트의 profile
devtools profile export
# Profile transfer passphrase:
# Confirm profile transfer passphrase:

# 프로젝트 밖에서는 profile을 직접 지정
devtools profile export --profile myapp
```

기본 export는 age scrypt로 암호화하며 passphrase를 저장하지 않습니다. 터미널에서는
passphrase를 echo 없이 두 번 입력해 확인합니다. 자동화나 pipe 환경에서는 passphrase를
argv나 환경변수에 넣지 말고 다음 중 하나를 명시합니다.

```sh
devtools profile export --passphrase-file /secure/transfer-passphrase.txt
cat /secure/transfer-passphrase.txt | devtools profile export --passphrase-stdin
```

`--profile`을 생략하면 현재 디렉터리 또는 `--dir PATH`에서 프로젝트를 찾습니다.
`--output`을 생략하면 현재 디렉터리에 `./<profile>.age`를 만듭니다. 기존 디렉터리를
지정하면 그 아래 `<profile>.age`를 자동으로 사용합니다. 최종 출력 파일이 이미 있으면
snapshot을 만들기 전에 즉시 `output_exists`로 실패하고 기존 파일은 덮어쓰지 않습니다.

생성된 `.age` 파일 하나만 대상 기기로 복사합니다. archive에는 변수·secret·env와
task/workstream/validation 데이터가 들어갑니다. `devtools.toml`, port 할당, instance 연결,
실행 중 process, dashboard 세션은 이동하지 않습니다. 프로젝트 파일은 Git 등 기존 방식으로
별도 준비하세요.

### 고급 recipient 방식

자동화에서 public-key 방식을 유지해야 하면 기존 X25519 transfer도 계속 사용할 수 있습니다.
이 경우에만 대상 기기에서 recipient를 먼저 준비합니다.

```sh
# 대상 기기
devtools profile transfer prepare

# 원본 기기
devtools profile export --recipient age1...
# 또는 --recipient-file /secure/recipient.txt
```

recipient mode와 passphrase mode는 한 export에서 섞을 수 없습니다. `backup configure`는
profile transfer의 기본값으로 사용하지 않습니다.

## 미리보기 후 가져오기

기본 passphrase archive는 대상 기기에서 파일 하나만 가지고 import합니다.

```sh
devtools profile import --file ./myapp.age
# Profile transfer passphrase:
```

터미널에서는 archive passphrase를 echo 없이 한 번 입력합니다. non-interactive 환경은
export와 마찬가지로 `--passphrase-file` 또는 `--passphrase-stdin`을 명시합니다.

```sh
devtools profile import --file ./myapp.age --passphrase-file /secure/transfer-passphrase.txt
cat /secure/transfer-passphrase.txt | devtools profile import --file ./myapp.age --passphrase-stdin
```

단일-profile 파일은 원본 이름을 자동 선택하며 대상도 같은 이름을 사용합니다. 여러 profile이
있는 일반 backup 파일에서는 `--profile NAME`으로 원본을 선택하고, 다른 이름으로 가져오려면
`--as NAME`을 추가합니다.

recipient로 암호화한 기존/고급 archive는 `profile transfer prepare`의 로컬 identity를
자동 사용할 수 있고, 외부 키를 쓰는 경우 `--identity-file PATH`를 명시합니다.
`--identity-file`과 passphrase 입력 옵션은 함께 사용할 수 없습니다.

import 기본 동작은 미리보기입니다. `data.digest`, `data.target_exists`, `data.diff`를
검토한 뒤 같은 파일·원본·대상에 digest와 새 요청 ID를 전달합니다.

```sh
devtools profile import --file ./myapp.age \
  --apply DIGEST --request-id UUID
```

interactive passphrase archive라면 apply 호출에서도 같은 passphrase를 다시 입력합니다.
non-interactive에서는 preview/apply 모두 같은 `--passphrase-file` 또는
`--passphrase-stdin`을 사용합니다.

`--apply`와 `--request-id`는 반드시 함께 사용합니다. 기존 대상도 미리보기는 가능하지만
실제 교체에는 `--replace`가 필요합니다. 교체 전 safety archive는 현재 import와 같은
암호화 방식으로 생성됩니다. passphrase import라면 같은 passphrase, recipient import라면
같은 identity의 public recipient를 사용합니다. `backup configure`는 필요하지 않습니다.
경로는 `data.safety_backup`에 반환되며 같은 passphrase/identity로 다시 import할 수 있습니다.
안전 백업을 만들지 못하면 대상 교체를 시작하지 않습니다.

주요 transfer 오류는 다음과 같습니다. `passphrase_required`는 non-interactive 실행에서
`--passphrase-file` 또는 `--passphrase-stdin`을 명시해야 한다는 뜻이고,
`passphrase_mismatch`는 interactive export의 두 입력이 다르다는 뜻입니다.
`invalid_backup`은 archive 손상 또는 passphrase/identity 불일치를 포함합니다.
`transfer_identity_missing`·`transfer_identity_invalid`는 advanced recipient archive에
사용할 로컬 X25519 identity가 없거나 안전하지 않은 경우입니다. `output_exists`는 기존
파일을 보존하므로 다른 `--output`을 선택해야 합니다.

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
