# 암호화 백업과 복구

[문서 목록](README.md) · [설치와 첫 실행](../README.ko.md)

`devtools backup`은 변수·secret·env와 task/workstream을 age로 암호화해 보관한다.
별도 설정 없이 복원용 암호를 입력해 백업할 수 있다. 공개키 방식은 ML-KEM-768 + X25519 혼합 키를 사용한다.

<a id="다른-환경으로-profile-이동"></a>

기기 간 이동은 [Profile 전송 가이드](profiles.md#다른-기기로-내보내기)를 참고한다. 결과는 CLI 출력 설정을 따르며 passphrase·개인 identity·백업 값 원문은 출력하지 않는다.

vault가 설정된 보호 root에서는 내보낼 값을 기존 키 세션으로 인증한다.
secret이 포함된 논리 자료를 받을 대상도 먼저 [vault 설정과 해제](secret-protection.md)가
필요하다. archive는 논리 값·버전·종류·계층을 보존하지만 기기 root key, 세션과 실행
handle은 옮기지 않는다. 복원한 profile은 새 로컬 ID·generation·keyset을 사용한다.
공개 전용 root의 공개 값 백업에는 vault 설정이 필요하지 않다. 이전 형식의 archive는
호환 경로로 읽으며, profile 전송 암호와 로컬 vault 암호는 별개다.

## 설정 없이 백업

```sh
devtools backup create
devtools backup create --profile myapp --passphrase-file /secure/backup-passphrase.txt
cat /secure/backup-passphrase.txt | devtools backup create --passphrase-stdin
```

혼합 공개키를 등록하지 않았다면 터미널에서 복원용 암호를 숨김 입력으로 두 번 받는다.
자동화에서는 `--passphrase-file` 또는 `--passphrase-stdin`으로 전달한다. 암호는 age scrypt로
파일 키를 보호하며, 안전성은 암호의 강도에도 달려 있다. 충분히 긴 무작위 암호를 사용하고
복원할 때 꺼낼 수 있는 곳에 보관한다. 암호는 설정에 저장하지 않는다.

`--output`을 생략하면 설정된 백업 폴더를 사용한다. 설정이 없으면 devtools 데이터 폴더의
`backups/`에 저장한다. 다른 기기에서 복원할 계획이라면 결과에 나온 파일을 그곳으로 옮긴다.
명시적인 암호 입력은 기존에 등록한 공개키보다 우선한다.

## 공개키와 기본 위치 준비

키 파일을 둘 디렉터리를 준비한 뒤 다음 명령을 실행한다. 파일이 이미 있으면 그대로 보존하고 오류를 반환한다.

```sh
devtools backup keygen --identity-file /secure/devtools-identity.txt --recipient-file /secure/devtools-recipient.txt
devtools backup configure --directory /backups/devtools --recipient-file /secure/devtools-recipient.txt
```

생성된 두 파일은 사용자 전용 권한을 가진다. 개인키는 비밀번호 관리자나 오프라인 저장소에 별도로 보관하고, 복구할 때 파일로 제공한다. 공개키와 절대 백업 경로는 devtools 설정 경로의 `backup.json`에 저장한다. 이 파일은 기존 `config.toml`의 port 설정과 함께 사용할 수 있다.

`backup keygen`이 만드는 공개키는 `age1pq1…`, 개인키는 `AGE-SECRET-KEY-PQ-1…` 형식이다.
개인키 파일은 한 줄이며, 암호화된 파일은 age로 감싼 JSON 스냅샷이다.
기존 X25519 백업은 기존 개인키로 계속 검사하고 복원할 수 있다.

기존 설정이 X25519 공개키를 가리키면 터미널에서는 암호를 입력해 백업할 수 있다.
무인 실행에서는 암호를 명시하거나 혼합 공개키를 등록해야 한다. X25519 단독 공개키를 직접
지정하거나 등록하면 `legacy_backup_recipient`로 거절된다. 기존 X25519 백업을 복원할 때
필요한 개인키는 계속 보관한다.

기본 설정을 다시 확인하려면 `devtools backup status`를 사용한다. `data.item`에
`configured`, `directory`, 공개 `recipient`를 반환하며 개인 identity는 읽거나 출력하지 않는다.
설정이 없으면 `configured: false`와 빈 문자열을 반환하고, 손상된 설정은 오류로 구별한다.

`backup create`에서 `--recipient` 또는 `--recipient-file`을 지정하면 해당 혼합 공개키를
사용한다. 암호와 공개키 입력을 모두 생략하면 등록한 혼합 공개키를 먼저 사용한다.

## 백업 생성과 확인

```sh
devtools backup create
devtools backup create --profile myapp
devtools backup create --profile myapp --output /backups/myapp.age --recipient-file /secure/devtools-recipient.txt
devtools backup inspect --file /backups/password-backup.age
devtools backup inspect --file /backups/myapp.age --identity-file /secure/devtools-identity.txt
```

profile 생략은 전역 데이터에 존재하는 모든 profile을 선택한다. 출력 경로 생략 시 설정한 디렉터리에 고유 이름을 생성한다. 출력 파일이 이미 존재하면 오류를 반환한다. 명시적인 출력 파일의 부모 디렉터리는 미리 준비한다.

백업은 값과 작업 저장소의 공통 잠금 아래에서 일관된 스냅샷을 획득한다. 생성 응답에는 파일 경로, 생성 시각, profile별 값·작업 데이터 존재 여부가 들어간다. inspect는 암호 또는 개인키로 전체 암호문을 검증한 뒤 같은 메타데이터를 반환한다. 평문 문서 크기는 최대 128 MiB다.

CLI의 keygen·configure·create는 `data.item`에 결과 메타데이터를, `data.changed`에 쓰기 여부를 반환한다. inspect는 `data.item`을 반환한다. restore는 `digest`·`targets`·`applied` 등 계획 보고서를 유지하며 `changed`와 `replayed`를 함께 제공한다. 미리보기에서는 둘 다 false이고, 동일한 적용 요청의 재시도에서는 최초 changed를 유지한 채 replayed가 true다. 전체 규칙은 [CLI 출력 계약](cli-output.md)을 따른다.

복구·가져오기의 적용 영수증은 최대 128 MiB이며, 가져오기 영수증에는 전체 비교 결과도 포함된다. 쓰기와 재시도 읽기에
같은 상한을 적용한다. 직렬화된 결과가 상한을 넘으면 `result_too_large`로 저장소 교체 전에
거절한다. 이전 버전이 기록한 1 MiB 초과 영수증도 새 상한 안이면 재생할 수 있다.

## 복구 미리 보기와 적용

전체 백업에서도 복구는 profile 단위로 적용한다. `--profile`은 백업 안의 원본, `--as`는 복구할 대상이다. 대상을 생략하면 `<원본>-restored`를 사용한다. 이름 길이 제한에 걸리면 `--as`로 유효한 이름을 지정한다.

`backup inspect`와 `backup restore`는 파일 형식을 감지한다. 암호 백업은 터미널에서
암호를 묻고, 무인 실행에서는 `--passphrase-file` 또는 `--passphrase-stdin`을 사용한다.
혼합 공개키 백업과 기존 X25519 백업은 해당 파일에 맞는 `--identity-file`을 지정한다.

```sh
devtools backup restore --file /backups/myapp.age --identity-file /secure/devtools-identity.txt --profile myapp --as recovered
```

기본 동작은 미리 보기다. 결과의 `digest`를 다음 요청에 전달한다. 요청 UUID는 CLI가 생성해 반환한다.

```sh
devtools backup restore --file /backups/myapp.age --identity-file /secure/devtools-identity.txt --profile myapp --as recovered --apply DIGEST
```

적용 요청은 미리 보기와 같은 파일·profile·대상을 사용한다. 대상 데이터가 변경되면 `revision_conflict`로 새 미리 보기를 요청한다. 같은 UUID와 동일한 적용 입력을 재전송하면 저장된 적용 결과를 반환한다. digest나 교체 옵션을 포함해 다른 입력으로 재사용하면 `request_conflict`다.

기존 profile도 `--replace` 없이 미리 볼 수 있다. 검토한 대상을 실제로 교체하려면
`--replace`를 추가한다. 암호 백업을 복원할 때는 같은 암호로 데이터 폴더의 `backups/`에
안전 백업을 만든다. 공개키 백업을 복원할 때는 설정된 혼합 공개키와 백업 폴더를 사용한다.
응답의 `safety_backup`에 경로를 반환한다. 안전 백업 생성에 실패하면 교체를 시작하지 않는다.
기존 대상을 `--replace` 없이 복원하려 하면 `profile_exists`다.

## 복구 후 상태

값과 env 계층은 원본대로 복원한다. 작업 UUID·명세·의존 관계·이력은 보존하고, 진행 중이던 claim에는 반납 이력을 추가한다. 원래 실행의 인증 컨텍스트와 재시도 영수증은 복원하지 않는다. 복구된 작업은 새 claim으로 이어간다.

프로젝트의 `devtools.toml`과 port 선언은 프로젝트 저장소에서 관리한다. 실행 환경에 종속된 port 할당, 디렉터리 연결, 프로세스, dashboard 세션과 조회 캐시는 백업 대상에서 제외한다. 복구 후 프로젝트 연결과 port는 실제 경로에서 다시 준비한다. 새 profile로 복구하면 프로젝트의 profile 선택도 해당 이름으로 연결한다.

교체 중에는 사용자 전용 `.maintenance` 디렉터리에 복원용 원본 데이터를 임시 보관한다. 성공하면 제거한다. 오류나 프로세스 중단으로 교체가 끝나지 않으면 다음 값·작업 저장소 접근이 원본을 복원한 뒤 진행된다. 복구용 임시 데이터는 기존 저장소와 같은 개인 파일 보호 범위를 따른다. 같은 저장소를 사용하는 CLI와 dashboard는 업데이트된 버전을 함께 사용한다.

복구는 task 조회 스냅샷을 무효화하므로 기존 cursor는 새 조회로 갱신한다. 복구 성공 여부가 불확실할 때는 같은 요청 ID와 입력으로 재시도한다.

## 오류와 검증

주요 오류는 `invalid_backup`, `invalid_recipient`, `backup_not_configured`, `profile_exists`, `revision_conflict`, `request_conflict`, `storage_error`, `backup_error`다. CLI 입력 형식 오류는 기존 `invalid_argument` 계약을 따른다. 오류에는 복호화된 값이나 키를 포함하지 않는다.

관련 테스트는 암호화 왕복, 잘못된 키, 손상된 파일, stale preview, 재시도, 실행 컨텍스트 해제와 중단 복원을 검증한다. `devtools run test:installed backup`은 설치된 release 실행 파일로 키 준비부터 복구까지 확인한다.
