# Init profile 기본값과 입력·출력 계획

상태: **protocol 22 소스에 구현**. 2026-11-02 사용자 결정에 따른 계약이다.
플랫폼별 검증 상태와 실제 source 기준은 연결 workstream의 V15·V10 기록을 따른다.
기존 설치 CLI 0.39.0/protocol 21은 `init --profile NAME`을 요구하므로 설치 빌드의 `version`과 `schema init`을 확인한다.
아래 기본값·대화형 옵션·응답 필드는 protocol 22에 적용된다.
연결 workstream은 `76536172-4285-4efa-9ad6-528d3fa1de5f`, 추가 작업은 T13이다.

## 1. Profile 선택

`init`은 현재 디렉터리에 설정을 만드는 명령이다. 일반 실행의 profile 선택
규칙을 바꾸거나 상위 프로젝트의 profile을 가져오는 기능으로 확대하지 않는다.

| 현재 디렉터리의 상태 | 입력 | 계획한 결과 |
| --- | --- | --- |
| 설정 없음, 이름 `demo-web` | `devtools init` | `profile = "demo-web"`을 저장 |
| 유효한 설정의 profile이 `shared-app` | profile 생략 | 기존 profile 사용, 파일 변경 없이 성공 |
| 유효한 설정의 profile이 `shared-app` | `--profile shared-app` | 파일 변경 없이 성공 |
| 유효한 설정의 profile이 `shared-app` | `--profile other-app` | 기존 `profile_conflict`, exit 3, 파일 보존 |
| 설정 없음 | 유효한 `--profile NAME` | 지정한 이름으로 생성 |
| 설정이 손상됐거나 regular file이 아님 | 모든 입력 | 기존 오류 유지, 덮어쓰기와 이름 질문 없음 |
| 상위 디렉터리에만 설정 존재 | profile 생략 | 현재 디렉터리를 대상으로 위 규칙 적용 |

설정이 없을 때만 canonical directory의 basename을 후보로 삼는다. Symlink의
별칭 이름 대신 실제 디렉터리 이름을 사용하며, 현재 `Init`의 canonical path
정책과 맞춘다. 한번 저장한 profile은 디렉터리 이름 변경이나 이동으로 바뀌지 않는다.

이름 규칙은 기존 `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`를 유지한다. 공백·한글·선행
점/하이픈·길이 초과·파일시스템 루트는 자동 후보로 사용할 수 없다. Lowercase 변환,
slug 생성, 잘라내기, 숫자/hash suffix 부여는 하지 않는다. 명시한 `--profile ''`도
생략으로 간주하지 않고 입력 오류다.

## 2. 이름 중복의 의미와 확인 범위

Profile 이름은 기기 내 선택 data root의 공유 namespace다. 같은 이름을 사용하면
변수·secret·env·task/workstream을 공유한다. 같은 이름이라는 이유로 기존 데이터를
덮어쓰거나 이름을 자동으로 바꾸지 않는다.

자동 후보는 파일을 만들기 전에 **저장소에서 알려진 동일 이름의 profile**을 확인한다.
Values/tasks뿐 아니라 instance/process 기록에만 존재하는 profile도 포함한다.
기존 Catalog의 이름 집계와 생성 없는 Query 잠금 경계를 재사용하며, 값 본문·secret·task 실행
인증정보·supervisor RPC는 읽지 않는다. 다른 profile의 내용을 열거나 vault를
unlock하지 않는다. Profile 공유 가능성을 확인하는 동작으로 전체 상세 조회를 호출하지 않는다.

결과는 `existing`, `absent`, `unavailable`로 구분한다. 누락된 정상 빈 저장소는
`absent`지만 읽기 거부·손상·불완전 recovery·전환 필요 상태를 `absent`로 취급하지 않는다.
Catalog의 writable 준비 fallback을 그대로 호출하지 않고 passive lookup 진입점을 둔다.
조회 때문에 data root·잠금 파일·profile 데이터를 새로 만들거나 forward recovery를 실행하지 않는다. 정상 read-only
data에서도 확인할 수 있어야 한다. `--isolated-root`가 선택되면 그 root 안에서만 확인한다.

`init`이 만든 TOML만 있고 저장소에는 기록이 없는 다른 디렉터리는 이 조회로 발견할 수
없다. 임의 파일시스템 검색이나 새 project 등록 DB를 추가하지 않는다. 따라서 `absent`는
조회 시점의 알려진 저장소에 없다는 뜻이며, 전역에서 유일한 디렉터리 이름이라는 보장이
아니다. 새 설정 결과에는 profile이 이름으로 공유된다는 사실을 표시한다. 구분이 필요한
프로젝트는 `--profile`로 이름을 지정한다.

명시한 `--profile NAME`은 해당 이름을 선택하겠다는 의도로 처리한다. 기존 스크립트와
worktree의 공유 사용을 유지하고, 추가 확인이나 저장소 조회를 강제하지 않는다.
기존 유효한 설정의 재실행도 저장소 조회 없이 성공한다. 이때 presence는 `unchecked`다.
별도의 `--reuse-profile`이나 범용 `--yes`는 추가하지 않는다.

## 3. 대화형과 비대화형 선택

기본 대화형 조건은 stdin/stdout/stderr 모두 실제 terminal이고, 최종 출력 형식이 text이며,
기존 agent 신호와 양수 CI 신호(`CI=1` 또는 `true`)가 없는 경우다. 조건을 만족하더라도
정상 이름이고 중복이 없으면 질문 없이 생성한다. `TERM=dumb`/`NO_COLOR`는 장식만 바꾸며
입력 가능 여부의 근거로 사용하지 않는다.

계획한 추가 옵션:

- `--no-interactive`: 모든 자동 질문을 끈다. stdin을 읽거나 별도 TTY를 열지 않는다.
- `--interactive`: 이름 입력·중복 선택을 명시적으로 허용한다. 실제 stdin/stdout/stderr
  terminal과 text 형식이 필요하다. 조건이 안 맞으면 exit 2로 종료한다.
- 두 옵션을 함께 지정하면 입력 오류다. 명시적 JSON 형식과 `--interactive`도 충돌이다.
  Agent/CI가 직접 `--interactive`를 지정한 경우는 위 terminal/text 조건 안에서만 허용한다.

TTY 여부를 조작해도 pipe/JSON/agent에서 자동 질문이 켜지지 않아야 한다.
`--format text`만 지정하는 것은 대화형 선택에 대한 동의가 아니다.
Windows console/ConPTY와 Unix PTY를 실제 환경에서 검증한다.

### 대화형에서 필요한 선택

| 상황 | 질문/처리 | 최종 결과 |
| --- | --- | --- |
| 자동 후보가 유효하고 알려진 중복 없음 | 질문 없음 | 생성, exit 0 |
| 자동 후보가 기존 profile과 겹침 | 기존 profile 공유 / 다른 이름 / 취소 | 아래 선택 처리 |
| 자동 후보가 유효하지 않음 | `Profile name: ` 입력 | 유효한 이름을 다시 검사 |
| 다른 이름도 중복 | 공유 여부를 다시 표시 | 확인 없는 공유 없음 |
| 조회 자체가 불가능 | 조회 실패 표시, 질문 없음 | 원래 원인과 재실행 안내, 파일 생성 없음 |
| Ctrl-C / 취소 선택 / 중복 선택에서 빈 Enter | 취소 | `canceled`, exit 130, 파일 생성 없음 |
| EOF / 입력 terminal 소실 | 질문 중단 | `input_required`, exit 3, 파일 생성 없음 |
| 이름 입력에서 빈 값 또는 잘못된 이름 | 규칙 표시 후 재입력 | 연속 3회 실패하면 exit 2 |

Profile 이름은 비밀 입력이 아니므로 echo를 끄는 passphrase prompt를 재사용하지 않는다.
선택·이름 입력 길이는 제한하고 제어문자는 거부한다. Prompt는 stderr, 최종 성공 결과는
stdout, 최종 오류는 stderr에 출력한다. Cancel/deadline 후 reader가 terminal을 붙잡거나
goroutine/handle을 남기지 않아야 한다. 질문이 끝나기 전에는 임시 설정 파일도 만들지 않는다.

### 비대화형

CI, agent, pipe, 리다이렉션, JSON 형식, `--no-interactive`에서는 stdin을 읽지 않는다.
stdin에 임의 문자열이나 `yes`를 공급해도 동의로 해석하지 않는다.

| 상황 | 처리 |
| --- | --- |
| 정상 자동 후보, 중복 없음 | 질문 없이 생성 |
| 자동 후보 중복 | `profile_name_in_use`, exit 3; 명시적 `--profile` 해결책 제공 |
| 자동 후보가 유효하지 않음 | `invalid_argument`, exit 2; 유효한 `--profile` 요구 |
| 중복 확인 불가 | 원래 storage/권한/recovery 원인, exit 1 또는 해당 domain의 기존 exit |
| 명시적 profile | 추가 질문·조회 없이 기존 Init 규칙 적용 |
| 유효한 기존 설정 | 변경 없이 기존 profile 반환 |

## 4. 상황별 표시 예시

아래 문구와 JSON은 **설계 예시**다. 실제 출력 정책·escape·width·shell별 remedy 렌더러를
재사용한다. 이름과 현재 호출 경로는 안전하게 escape하며 기존 profile의 값·secret·개수·다른
프로젝트 경로는 출력하지 않는다.

새 설정 생성:

```text
Created project configuration.
Profile  demo-web (directory name)
Config   /work/demo-web/devtools.toml
Projects using this profile name share values, secrets, and tasks.
```

기존 설정 재실행:

```text
Using existing project configuration.
Profile  shared-app (configuration)
Config   /work/renamed-app/devtools.toml
```

대화형 자동 이름 중복:

```text
Profile "web" already exists.
Projects using this name share values, secrets, and tasks.
1) Use existing profile "web"
2) Choose another profile name
3) Cancel
Choice [3]:
```

선택 1 뒤에는 `Created project configuration using existing profile "web".`을 출력한다.
선택 2는 `Profile name: `을 표시하고 새 입력을 같은 유효성·중복 규칙으로 검사한다.
새 이름의 기본값으로 충돌한 이름을 재제공하지 않는다.

자동 이름이 유효하지 않은 경우:

```text
The directory name cannot be used as a profile name.
Use 1–128 ASCII letters, digits, dots, underscores, or hyphens; start with a letter or digit.
Profile name:
```

비대화형 중복의 text 오류:

```text
Profile "web" already exists; no configuration was created.
To share it: devtools init --profile web
To use another name: devtools init --profile NAME
```

두 번째 명령의 `NAME`은 입력이 필요한 placeholder이며 자동 실행할 완성 argv로 광고하지 않는다.
POSIX/fish/PowerShell 표시와 실행 가능한 argv는 기존 protocol guidance로 따로 처리한다.

## 5. JSON·오류 계약

기존 성공 envelope와 `data.changed`, `data.item.config_path`, `data.item.profile`을 유지한다.
계획한 추가 item 필드는 다음과 같다. Schema와 strict 소비자를 함께 갱신하며 protocol
변경 여부는 구현 시점 최신 계약으로 판단한다.

- `profile_source`: `flag | config | directory | prompt`.
- `profile_presence`: `existing | absent | unchecked`. 성공에 `unavailable`을 넣어 정상으로
  위장하지 않는다. Flag/config 경로는 `unchecked`, 자동/직접 입력은 확인한 상태다.

```json
{"schema_version":1,"ok":true,"data":{"changed":true,"item":{"config_path":"/work/demo-web/devtools.toml","profile":"demo-web","profile_source":"directory","profile_presence":"absent"}}}
```

Known collision 오류의 핵심 필드는 다음과 같다. 최종 응답은 공통 ErrorDetails가 채우는
전체 필드와 strict schema를 따르므로 이 예시는 완전한 응답 fixture가 아니다.

```json
{"code":"profile_name_in_use","details":{"operation":"init","phase":"validate","reason":"profile_name_in_use","effects":"none","retry":"after_input_change","target":{"kind":"profile","name":"web"}}}
```

Remedies는 기존 profile 공유용 `devtools init --profile web`과 새 이름 지정용 입력을
구분한다. 새 이름은 `required_inputs`에 `profile`을 표시한다. 모든 same-store 안내는
선택 isolation root를 유지한다. 존재 여부 조회 실패의 원인·stage/os_code·효과를 보존하고
`profile_name_in_use`나 빈 결과로 덮어쓰지 않는다. 일반 Init는 vault unlock을 요구하지 않는다.

JSON/비대화형 성공은 stdout에 단일 envelope, 실패는 stderr에 단일 envelope이며 prompt,
설명·ANSI·progress를 섞지 않는다. 취소/입력 문제는 설정 게시 전이면 `effects=none`이다.
쓰기·게시를 시도한 뒤의 I/O/출력 실패에는 실제 또는 불명 효과와 inspect-first 안내를 남긴다.
생성 성공 후 결과 전송에 실패했으면 다음 실행이 기존 설정을 읽어 `changed=false`로 수렴한다.

## 6. 게시·동시성·경계

이름 결정과 필요한 조회가 끝난 뒤 기존 temp+sync+link의 create-only 게시를 재사용한다.
기존 파일의 주석·형식·권한·profile은 재실행으로 바꾸지 않는다. 다른 initializer가 먼저
파일을 만들면, profile 생략 호출은 그 유효한 설정을 사용하고 `profile_source=config`,
`changed=false`로 반환한다. 명시적 profile과 다르면 기존 충돌 오류다.

확인과 게시 사이에 자동 후보가 알려진 기존 profile로 바뀌면 무확인 공유하지 않고 다시
선택하거나 비대화형 충돌로 종료한다. 가능한 기존 maintenance 조회 잠금 안에서 짧게
재확인·게시하며 prompt 동안 잠금을 잡지 않는다. 이 검사는 이름을 예약하거나 디렉터리
전체에서 유일성을 보장하는 새 registry가 아니다. 향후 같은 이름을 선택한 다른 프로젝트는
기존 profile 공유 계약을 따른다.

이름 변경·symlink·상위 설정·루트·비ASCII·128/129자·ASCII 대소문자·Read-only data와
writable project·project write denial·손상/누락 lock·pending migration/recovery를 검사한다.
명시적 flag와 기존 config 경로는 data root 접근이 실패해도 설정 처리에 불필요한 의존을
추가하지 않는다. Profile 목록이 바뀌거나 다른 profile이 손상된 경우 추론 결과를 숨기지 않는다.

## 7. 구현 단위와 검증

T13 하나의 사용 가능한 변경으로 다음을 묶는다.

1. `internal/project/init.go`의 현재 파일 우선 처리와 basename 기본값, create-only 재실행.
2. CLI option/schema·입력/중복 조회·대화형 상태 전이·ErrorDetails 및 renderer.
3. Catalog 이름 조회의 RO 경계 재사용. 전체 상세 조회·새 등록 DB·vault 의존성은 추가하지 않음.
4. 기존 Init 단위/CLI/presentation/schema 테스트에 위 분기와 native PTY/console·pipe 검증 추가.
5. README·CLI/config/profile 가이드·reviewed examples·repository 배포 Skill 갱신.

V15는 T13의 필수 검증이다. T08/V08은 다른 변경과 같은 최종 source에서 통합 확인한다.
정상 생성/재실행, 명시적 공유, inferred collision의 공유/새 이름/취소, invalid/EOF/Ctrl-C,
agent+TTY/CI+TTY/text redirect/JSON/고정 stdin, storage unknown/RO, race와 게시 후 출력 실패를
포함한다. Native Windows·macOS·Linux 미실행은 pass로 기록하지 않는다.

계획 리뷰에서는 빈 프로젝트를 전부 발견한다는 과한 주장, explicit profile의 호환성 변경,
format과 interaction 혼동, prompt 동안의 잠금, 오류를 absent로 처리하는 분기, JSON에
prompt가 섞이는 문제, 같은 파일의 동시 Init와 외부 profile 생성 race를 재확인한다.

## 8. 문서·기록의 일반화

예시는 synthetic profile·경로만 사용한다. 사용자 프로젝트의 repo 이름·조직·절대 경로·고유
source commit을 제품 문서·명세·현재 인수인계에 복사하지 않는다. 재현에 필요한 argv·권한·잠금·
인증 상태와 관찰 결과는 유지한다. Dependency·공식 기술 문헌·devtools 자신의 식별자는
구체적 근거로 유지한다.

고유 외부 맥락이 남은 로컬 과거 산출물은 일반화하거나 중복 snapshot을 삭제한다.
일반화한 과거 파일의 기존 원문 hash·signature·실행 검증은 수정본 검증 근거가 아니다.
현재 제품 통과 증거와 구분하며, 실제 사용자 값 저장소나 Git 이력을 재작성하지 않는다.
