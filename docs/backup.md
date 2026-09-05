# 암호화 백업과 복구

`devtools backup`은 전역 var/sec/env와 task/workstream을 age로 암호화해 보관한다. 공개키로 백업하고 개인키 파일로 복구한다. 모든 명령은 JSON을 반환하며 개인키와 백업에 담긴 값 원문은 출력하지 않는다.

## 키와 기본 위치 준비

키 파일을 둘 디렉터리를 준비한 뒤 다음 명령을 실행한다. 파일이 이미 있으면 그대로 보존하고 오류를 반환한다.

```sh
devtools backup keygen --identity-file /secure/devtools-identity.txt --recipient-file /secure/devtools-recipient.txt
devtools backup configure --directory /backups/devtools --recipient-file /secure/devtools-recipient.txt
```

생성된 두 파일은 사용자 전용 권한을 가진다. 개인키는 비밀번호 관리자나 오프라인 저장소에 별도로 보관하고, 복구할 때 파일로 제공한다. 공개키와 절대 백업 경로는 devtools 설정 경로의 `backup.json`에 저장한다. 이 파일은 기존 `config.toml`의 port 설정과 함께 사용할 수 있다.

암호화는 `filippo.io/age` 1.3.2의 X25519 recipient를 사용한다. 개인키 파일은 keygen이 생성한 한 줄짜리 age identity이며, 암호화된 파일은 age 형식의 JSON 스냅샷이다. 백업 문서 버전은 프로그램이 관리한다.

## 백업 생성과 확인

```sh
devtools backup create
devtools backup create --profile myapp
devtools backup create --profile myapp --output /backups/myapp.age --recipient-file /secure/devtools-recipient.txt
devtools backup inspect --file /backups/myapp.age --identity-file /secure/devtools-identity.txt
```

profile 생략은 전역 데이터에 존재하는 모든 profile을 선택한다. 출력 경로 생략 시 설정한 디렉터리에 고유 이름을 생성한다. 출력 파일이 이미 존재하면 오류를 반환한다. 명시적인 출력 파일의 부모 디렉터리는 미리 준비한다.

백업은 값과 작업 저장소의 공통 잠금 아래에서 일관된 스냅샷을 획득한다. 생성 응답에는 파일 경로, 생성 시각, profile별 값·작업 데이터 존재 여부가 들어간다. inspect는 개인키로 전체 암호문을 검증한 뒤 같은 메타데이터를 반환한다. 평문 문서 크기는 최대 128 MiB다.

## 복구 미리 보기와 적용

전체 백업에서도 복구는 profile 단위로 적용한다. `--profile`은 백업 안의 원본, `--as`는 복구할 대상이다. 대상을 생략하면 `<원본>-restored`를 사용한다. 이름 길이 제한에 걸리면 `--as`로 유효한 이름을 지정한다.

```sh
devtools backup restore --file /backups/myapp.age --identity-file /secure/devtools-identity.txt --profile myapp --as recovered
```

기본 동작은 미리 보기다. 결과의 `digest`와 새 UUID를 다음 요청에 전달한다.

```sh
devtools backup restore --file /backups/myapp.age --identity-file /secure/devtools-identity.txt --profile myapp --as recovered --apply DIGEST --request-id UUID
```

적용 요청은 미리 보기와 같은 파일·profile·대상·교체 옵션을 사용한다. 대상 데이터가 변경되면 `revision_conflict`로 새 미리 보기를 요청한다. 같은 UUID와 입력을 재전송하면 저장된 적용 결과를 반환한다. 다른 입력으로 재사용하면 `request_conflict`다.

기존 profile을 교체하려면 미리 보기와 적용 모두에 `--replace`를 추가한다. 교체 직전에 설정된 공개키와 백업 디렉터리로 현재 profile을 암호화하고, 성공 응답의 `safety_backup`에 경로를 반환한다. 직전 백업 생성에 실패하면 교체를 시작하지 않는다. 새 profile로 복구할 때는 기존 대상이 있으면 `profile_exists`다.

## 복구 후 상태

값과 env 계층은 원본대로 복원한다. 작업 UUID·명세·의존 관계·이력은 보존하고, 진행 중이던 claim에는 반납 이력을 추가한다. 원래 실행의 인증 컨텍스트와 재시도 영수증은 복원하지 않는다. 복구된 작업은 새 claim으로 이어간다.

프로젝트의 `devtools.toml`과 port 선언은 프로젝트 저장소에서 관리한다. 실행 환경에 종속된 port 할당, 디렉터리 연결, 프로세스, dashboard 세션과 조회 캐시는 백업 대상에서 제외한다. 복구 후 프로젝트 연결과 port는 실제 경로에서 다시 준비한다. 새 profile로 복구하면 프로젝트의 profile 선택도 해당 이름으로 연결한다.

교체 중에는 사용자 전용 `.maintenance` 디렉터리에 복원용 원본 데이터를 임시 보관한다. 성공하면 제거한다. 오류나 프로세스 중단으로 교체가 끝나지 않으면 다음 값·작업 저장소 접근이 원본을 복원한 뒤 진행된다. 복구용 임시 데이터는 기존 저장소와 같은 개인 파일 보호 범위를 따른다. 같은 저장소를 사용하는 CLI와 dashboard는 업데이트된 버전을 함께 사용한다.

복구는 task 조회 스냅샷을 무효화하므로 기존 cursor는 새 조회로 갱신한다. 복구 성공 여부가 불확실할 때는 같은 요청 ID와 입력으로 재시도한다.

## 오류와 검증

주요 오류는 `invalid_backup`, `invalid_recipient`, `backup_not_configured`, `profile_exists`, `revision_conflict`, `request_conflict`, `storage_error`, `backup_error`다. CLI 입력 형식 오류는 기존 `invalid_argument` 계약을 따른다. 오류에는 복호화된 값이나 키를 포함하지 않는다.

관련 테스트는 암호화 왕복, 잘못된 키, 손상된 파일, stale preview, 재시도, 실행 컨텍스트 해제와 중단 복원을 검증한다. Docker의 `backup` 시나리오는 설치된 실행 파일로 키 준비부터 복구까지 확인한다.
