# 로컬 secret 보호

[문서 목록](README.md) · [값과 명령](cli-contract.md) · [출력 보호](output-protection.md)

이 문서는 **현재 개발 소스**의 vault 기능을 설명한다. 공개 v0.27.0에는 이 기능이 없다.
다음 배포의 변경과 전환 조건은 [릴리스 준비서](releases/v0.28.0.md)를 따른다. 실제 native·설치물 검증은
[A12 수용 기록](var-sec/A12-final-review.md)에 있으며, 릴리스 게시·설치된 CLI의 지원과는
구분한다. 설치한 CLI에서 `devtools schema vault status`와 `devtools schema vault init`이
제공되는지 먼저 확인한다. 이전 빌드의 평문 자료는
업데이트나 이 문서만으로 전환되지 않는다.

profile/env를 한 번 선택하면 그 범위의 값을 실행에 전달한다. 명령마다 키 이름이나
암호를 열거하지 않는다. 공개 var만 쓰는 새 저장소는 vault 없이 사용할 수 있다.
secret을 쓰기 전에 보호를 한 번 설정하고, 이후 해제된 키 세션을 재사용한다.

## 처음 설정하기

기존 OS 보호 서비스를 사용하는 기본 설정이다.

```sh
devtools vault status
devtools vault init
```

macOS는 Keychain, Windows는 현재 사용자 DPAPI, Linux는 이미 실행 중인 Secret
Service를 사용한다. OS 확인 창을 허용하려면 해당 init/unlock에 `--interactive`를
명시한다. 서비스를 설치·자동 시작하거나 실패 시 다른 backend로 전환하지 않는다.
WSL은 별도의 Linux 저장소와 서비스를 사용한다. Windows의 키나 데이터 디렉터리를
직접 공유하지 않는다.

macOS Keychain은 OS의 기존 앱 접근 조건을 유지한다. 업데이트로 서명이 바뀌면 새
버전의 접근 승인이 필요할 수 있다. 비대화형 명령의 `key_access_denied` 또는
`key_interaction_required`는 저장 데이터가 사라졌다는 뜻이 아니다. OS 확인이 가능한
터미널에서 `devtools vault unlock --interactive`로 명시적으로 확인을 허용한다.
해결 전에는 원본·키·접근 가능한 기존 빌드를 보존한다. 설치/업데이트 완료와 Keychain의
새 앱 승인 완료는 별도이며, 일반 명령마다 OS 확인을 자동 반복하지 않는다.

keyring이 없는 서버·컨테이너에서는 처음부터 암호 backend를 명시한다. 터미널에서는
숨김 입력을 사용하고, 자동화에서는 준비한 비공개 일반 파일이나 파이프를 명시한다.
다음 `/private/vault-passphrase`는 직접 준비한 파일 경로로 바꾼다.

```sh
devtools vault init --backend passphrase --passphrase-file /private/vault-passphrase
```

암호를 명령 인자·환경변수·프로젝트 설정에 넣지 않는다. passphrase 파일을 계속
보관할지는 사용자가 결정한다. 재사용 세션 때문에 모든 명령에서 그 파일을 읽을
필요는 없다. `--passphrase-stdin`도 제공하며 file과 동시에 선택하지 않는다.
`DEVTOOLS_ISOLATED_ROOT`의 native 변경은 거절하므로 테스트에는 암호 backend를 쓴다.

init은 실제 보호 키를 다시 열어 일치 여부를 확인하고 기존 모든 profile의 secret
계층을 전환한다. 완료 전에 catalog·keyset·secret 계층을 검증한다. 원래 `.env`와
사용자가 준비한 secret 파일은 지우지 않는다. 기존 파일·백업·과거 로그와 파일시스템의
이전 사본이 안전하게 삭제되었다고 보장하지 않는다. `vault_already_initialized`면
init을 반복하거나 키를 지우지 말고 기존 상태를 확인한다.

## 기존 저장소를 전환하기

`vault init`은 현재 데이터 root의 **모든 profile**에 적용된다. 프로젝트 하나에만
적용하는 옵션은 없다. 실행 파일 업데이트와 저장 전환은 다음 순서로 구분한다.

1. 기존 버전에서 복구 가능한 [암호화 백업](backup.md)을 만들고 복구 키를 별도로
   보관한다. 원본 `.env`와 입력 파일도 전환 성공을 확인할 때까지 보존한다.
2. 같은 데이터 root에 접근하는 CLI·dashboard 등 writer를 vault 지원 빌드로 맞춘다.
   실행 중인 예전 dashboard는 종료하고 새 빌드로 시작한다. 이미 실행한 앱은
   옛 환경을 유지하므로 전환 후 별도로 재시작할 대상을 정한다.
3. `devtools vault status`를 확인한다. `uninitialized`라면 위의 native 또는 명시
   passphrase 방식으로 한 번 초기화한다. 기존 vault가 있으면 init 대신 unlock을 쓴다.
4. `vault status`의 `data.item.state`와 `devtools values inspect --profile NAME`을
   확인한다. 필요한 명령을 새로 실행해 준비·실행 결과를 확인한다. 저장 전환 성공은
   기존 앱의 갱신이나 외부 서비스 인증 성공을 뜻하지 않는다.

전환된 저장소를 구버전 binary로 다시 쓰는 downgrade는 지원하지 않는다. 구버전이
필요하면 전환 전 보관한 호환 백업을 별도 새 데이터 경로에서 복구한다. 중단·손상은
아래 [복구 절차](#오류와-복구)를 따르며 보호 파일 삭제로 초기화를 강제하지 않는다.

## 평소 사용과 잠금

설정 뒤 secret 등록과 실행은 기존 file/stdin·profile/env 흐름을 쓴다.

```sh
devtools sec set API_TOKEN --profile myapp --file /private/api-token
devtools run --profile myapp --env local -- my-installed-command
```

`myapp`, `local`, 실행 파일과 입력 파일을 준비한 예시다. 새로운 env는 먼저
`devtools env create local --profile myapp`으로 만든다. 등록 응답에는 값이 없다.
`sec get`과 secret `--value`는 제공하지 않는다. `VITE_`·`NEXT_PUBLIC_` 등 흔한
frontend 공개 prefix에는 값 없는 경고를 반환한다. 이름만으로 노출 여부가 확정되지는
않으며 secret 저장이 frontend 빌드의 공개 규칙을 바꾸지도 않는다.

키 세션은 profile들 사이에서 공유한다. 성공한 실제 키 사용 뒤 idle 8시간까지
재사용하며, 절대 최대 수명은 아니다. 상태 조회·completion·메타데이터 조회는 시간을
늘리지 않는다. 수동 잠금은 daemon 재시작 뒤에도 유지한다.

```sh
devtools vault lock
devtools vault status
devtools vault unlock --passphrase-file /private/vault-passphrase
```

native backend의 해제에는 passphrase 옵션을 쓰지 않는다. OS가 허용하면 일반 보호
작업에서 silent 해제할 수 있지만 수동 잠금 뒤에는 명시 `vault unlock`이 필요하다.
passphrase backend는 만료 뒤에도 명시 해제한다. 비대화형 명령은 암호를 기다리지
않고 필요한 action이 있는 오류로 끝난다. `vault status`는 daemon을 새로 열거나
잠금을 풀지 않는다.

공개 var와 secret 이름은 잠긴 상태에서도 조회할 수 있으며 `integrity_verified:false`
메타데이터는 인증 완료를 의미하지 않는다. 보호 root가 만들어진 뒤에는 별도 공개
profile도 변경과 실행에 같은 키 세션을 사용한다. 이때 root catalog의 인증과
profile 본문의 인증은 다르다. public-only profile 본문은 서명하지 않으며
`integrity_verified:false`를 유지한다. secret을 전부 unset해도 한번 sealed가 된
profile을 자동으로 평문으로 되돌리지 않는다.

잠금 성공 뒤에는 이전 준비가 새 secret 전달을 시작하지 않는다. 이미 실행된 앱의
환경과 자식 프로세스까지 회수되지는 않는다. 실행은 실제 전달 값을 미리 고정하며,
재시작도 old stop 전에 같은 입력을 준비한다. 잠금·저장 세대 변경으로 준비가 거절되면
기존 실행을 유지한다. 저장 성공이 기존 앱에 새 값이 반영되었다는 뜻은 아니다.

## Dashboard와 진단

Variables & secrets에서 전역 보호 상태와 lock/unlock을 제공한다. 초기 설정과
backend 선택은 CLI에서 한다. 암호 입력은 요청 전송 뒤 비우고, 결과 불명 상태에는
재입력을 요구한다. 접속 URL과 bearer 세션은 모든 profile을 관리할 수 있는 자격이다.
공개 로그에 남기지 않는다.

`diagnostics`는 `vault.state`, `backend`, `idle_expires_at`,
`requires_explicit_unlock`을 필요한 경우 포함한다. 잠긴 상태 자체는 검사 실패가
아니지만 recovery 상태는 `ready:false`의 원인이 된다. `doctor COMMAND`의 실제 실행
준비는 잠금 때문에 거절될 수 있다. 자동 상태 조회는 native 인증 창을 열지 않는다.

## 오류와 복구

`vault_locked`는 명시 unlock, `passphrase_required`는 안전한 입력 제공이 필요하다.
`key_missing`·`integrity_failed`·`vault_recovery_required`를 빈 저장소로 취급하지 않는다.
`recovery_required`는 `vault status`의 상태 이름이며 오류 코드와 구분한다.
보호 파일을 지우거나 새 키로 덮어쓰지 말고 상태와 기존 복구 자료를 보존한다.

설정이나 암호화 저장 전환이 중단되었으면 기존 키로 해제한 뒤 실행한다.
다음 unlock은 passphrase backend용이다. native backend에서는 passphrase 옵션을
생략하고, OS 확인이 필요할 때만 `--interactive`를 사용한다.

```sh
devtools vault unlock --passphrase-file /private/vault-passphrase
devtools vault recover
```

recover는 암호화 전환 자료와 실제 저장 내용을 검증한다. 손상 자료를 추측해
재생성하지 않는다. launch 결과가 unknown이면 PID나 lease만 보고 완료로 바꾸거나
자동 재실행하지 않는다. 결과를 신뢰할 수 없으면 복구가 미완료로 남을 수 있다.

암호화 [backup과 profile 전송](backup.md)은 로컬 키와 별개다. 논리 값은 전송하지만
기기 키·세션·실행 admission은 옮기지 않는다. sealed 자료를 받을 대상에서는 먼저
vault를 설정하고 해제한다. 복원은 새 로컬 profile ID·storage generation·keyset을
만든다. transfer passphrase와 vault passphrase도 독립적이다.

## 보호 범위

sealed profile의 secret 값은 로컬 암호화하고, 그 profile의 공개 값과 메타데이터도
전체 manifest 인증에 포함한다. public-only profile 본문에는 이 인증이 없다.
공개 var나 이름 자체를 숨기지는 않는다. private 파일 권한도 유지한다. 동일 사용자 권한을 가진
악성 프로그램·agent로부터 앱 사용을 격리하는 sandbox는 아니다. 유효한 이전 자료
전체를 되돌리는 rollback 방지도 외부 신뢰 anchor 없이 보장하지 않는다.

새 non-TTY stdout·stderr와 capture의 기본 마스킹, TTY·바이너리 raw 예외와 한도는
[실행 출력 보호](output-protection.md)를 따른다. 과거 로그, 앱이 직접 만든 파일·네트워크,
알 수 없는 token이나 임의 encoding까지 보호하지 않는다. Go/JS 문자열과 OS 환경을
즉시 물리적으로 지우는 secure heap도 제공하지 않는다.
