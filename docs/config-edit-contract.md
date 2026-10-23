# 설정 편집 CLI 계약

[문서 목록](README.md) · [설정 모델](cli-config-redesign.md) · [진단 계약](diagnostic-contract.md)

이 문서는 protocol 13 소스의 설정 편집 계약이다. 사용 순서는 [설정 편집 가이드](configuration.md)를 따른다.
본문의 입력·응답 이름은 [schema의 $defs](../internal/configcontract/schema.json)를 가리킨다.
JSON envelope는 기존처럼 성공을 stdout, 실패를 stderr에 한 개씩 반환한다.
범위 지원 소스의 `Version`은 `x-npmRange: true`를 가진다. 런타임은 이 확장 keyword로
128 byte 이내의 비어 있지 않은 npm SemVer 범위를 검증한다. 일반 JSON Schema 검사기는
이 keyword를 무시할 수 있으므로 문자/길이 검사만으로 범위 문법이 검증됐다고 간주하지 않는다.
Tool·ToolPatch·TOML 로드는 같은 범위 검사기를 사용한다. 잘못된 문법이나 전체 조건의 빈 교집합은
기존 편집 원자성에 따라 원본을 보존한다. 저장·fmt·구조 migrate는 범위 문자열을 유지한다.
버전 의미 변경과 정확 고정 전환은 [설정 가이드](configuration.md#기존-버전-설정을-옮길-때)를 따른다.

## 명령과 옵션

root의 기존 `--isolated-root` 선택은 유지한다.
아래 표에 없는 옵션, JSON의 모르는 필드, 중복 옵션은 invalid_argument다.
`--dir`의 기본값은 현재 디렉터리다. 해당 옵션은 현재 디렉터리 기준으로 해석하며 process cwd를 바꾸지 않는다.
`--file`도 process cwd 기준이고 프로젝트 root 기준이 아니다.
아래 편집 명령에는 `--profile`, `--force`, `--yes`, 임의 dotted set 옵션이 없다.

| 명령 | 위치 인자 | 허용 옵션 | 입력 또는 응답 |
| --- | --- | --- | --- |
| config schema | 없음 | 없음 | SchemaResult |
| config inspect | 없음 | --dir | InspectResult |
| config check | 없음 | --dir | ValidationReport |
| config fmt | 없음 | --dir, --check, --dry-run, 선택적 --revision | EditResult |
| config edit | 없음 | --dir, --file 또는 --stdin, --revision 필수, --dry-run | Edit → EditResult |
| config migrate | 없음 | --dir, --dry-run 또는 --apply DIGEST, apply에는 --revision 필수 | MigrationResult |
| config tool add | NAME | --dir, --file 또는 --stdin, 선택적 --revision, --dry-run | Tool → EditResult |
| config tool update | NAME | --dir, --file 또는 --stdin, --revision 필수, --dry-run | ToolPatch → EditResult |
| config tool remove | NAME | --dir, --revision 필수, --dry-run | EditResult |
| config preset add | NAME | --dir, --file 또는 --stdin, 선택적 --revision, --dry-run | Preset → EditResult |
| config preset update | NAME | --dir, --file 또는 --stdin, --revision 필수, --dry-run | PresetPatch → EditResult |
| config preset remove | NAME | --dir, --revision 필수, --dry-run | EditResult |
| command add | NAME | --dir, JSON 입력 또는 -- PROGRAM ARG..., 선택적 --revision, --dry-run | Command → EditResult |
| command update | NAME | --dir, --file 또는 --stdin, --revision 필수, --dry-run | CommandPatch → EditResult |
| command remove | NAME | --dir, --revision 필수, --dry-run | EditResult |
| command rename | NAME NEW_NAME | --dir, --revision 필수, --dry-run | EditResult |
| command explain | NAME | --dir, --profile, --env, --bundle | ExplainResult |

`--file`과 `--stdin`은 정확히 하나를 명시해야 한다. stdin의 암묵적 선택은 없다.
command add에서 JSON 입력과 `-- PROGRAM ARG...`도 상호 배타적이다.
TTY stdin을 기다리지 않으며 JSON은 pipe 또는 file redirect로 받는다.
빈 본문, UTF-8 BOM, 뒤따르는 두 번째 JSON 값과 중복 JSON key를 거절한다.
숫자는 signed 64-bit 십진 정수 token만 허용하며 소수·지수 표기는 받지 않는다.
JSON 중첩은 128단계 이내다. 이 계약의 유효한 입력 구조보다 훨씬 깊은 비정상 입력을 schema 검사 전에 제한한다.
입력 파일은 열린 handle의 regular file 여부를 확인하고 한 번 읽은 bytes를 사용한다.
입력 파일을 열거나 읽지 못하면 io_error/exit 1이며, 입력 내용이 잘못됐으면 invalid_argument/exit 2다.

bool flag는 생략하면 false다. `--dry-run=false`는 apply 모드다.
config fmt의 --check=true와 --dry-run=true는 함께 사용할 수 없다.
config migrate는 --apply가 없으면 preview이며, --apply와 --dry-run=true는 함께 사용할 수 없다.
입력 선택·상호 배타 조건은 bool flag의 true 값에 적용한다. false로 제공한 flag는 선택으로 세지 않는다.
--revision을 제공한 모든 명령은 그 값의 일치를 검사한다. 잘못된 revision 문법은 exit 2다.
add의 선택적 revision은 create-only 외에 호출자가 관찰한 전체 파일 상태까지 고정하려는 경우에 사용한다.

command explain의 --profile은 값 선택의 이름만 override하며 편집 파일 탐색을 우회하지 않는다.
--env는 기존 실행처럼 inject=true를 만들고, --bundle은 명시 선택을 override한다.
explain은 이를 정적으로 해석하며 값 저장소를 읽지 않는다.

## 조회

config schema는 프로젝트 탐색 없이 `{"protocol_version":13,"schema":{...}}`를 반환한다.
schema는 이 저장소의 계약 schema와 동일한 $defs를 포함한다.
`schema config preset add` 등 개별 command schema도 같은 정의를 묶어 독립적으로 검증 가능하게 반환한다.
명령 schema의 input/output과 이 계약이 다른 경우 출시하지 않는다.

config inspect는 config_path, revision, 원문의 format과 registry 모델로 투영한 document를 반환한다.
legacy 투영은 migration과 같은 결정적 registry·override 규칙을 사용하지만 파일을 쓰지 않는다.
neutral의 빈 map은 생략할 수 있으며, 명시된 false·빈 배열과 생략 여부는 보존한다.
이 직접 조회에는 설정에 작성된 exec·문자열이 포함된다. 이를 자동 오류·dashboard 로그에 복사하지 않는다.
저장소의 var/sec 실제 값은 조회하지 않는다.

config check는 문법·필드·참조·정적 충돌만 검사하고 실행 파일 존재·버전·값 등록을 검사하지 않는다.
유효하면 valid=true, diagnostics=[]와 exit 0이다.
유효하지 않으면 invalid_config/exit 3이며 같은 ValidationReport를 error.details.report에 넣는다.
문법 때문에 format을 판별할 수 없으면 unknown이다. 읽기 자체가 실패하면 보고서를 만들었다고 가정하지 않는다.
검사할 파일을 찾지 못하면 project_not_found이며 init 또는 올바른 --dir를 안내한다.

## 추가·수정·삭제 의미

- add는 create-only다. 대상이 있으면 config_resource_exists다.
- update/remove/rename은 대상이 없으면 config_resource_not_found다.
- rename의 목적지가 있으면 config_resource_exists다. 이름이 같으면 no-op이다.
- rename은 commands의 literal key와 source 연결만 바꾼다.
  exec·ready.exec·외부 script의 문자열을 검색하거나 교정하지 않는다.
- 내부 참조 삭제는 최종 후보에서 검사한다. 사용처가 남으면 config_reference_conflict이며 cascade는 없다.
- 부모 object의 삭제는 그 object 전체의 의도된 제거다. unrelated resource는 제거하지 않는다.
- 기본값에 의존하는 필드 제거의 결과도 impact에 포함한다.

update 규칙은 다음과 같다.

| 입력 | 의미 |
| --- | --- |
| 생략 | 기존 선언과 명시 여부 유지 |
| scalar | 제공한 값으로 교체 |
| 일반 object | 제공한 자식만 부분 수정 |
| array | 전체 교체, 이어 붙이기 없음 |
| null | 선택 필드·선택 map entry 제거. 필수 profile·exec·ready.exec·proxy의 host/port에는 오류 |
| tool_overrides entry | Tool 전체 교체. 생략한 다른 entry는 유지 |
| bind entry | 제공한 자식만 수정. 최종 Binding의 서로 배타적인 형태를 다시 검증 |
| 빈 최상위 patch {} | invalid_argument |
| 제공한 빈 nested map | 자식 변경 없음. 유효하면 no-op |
| 유효하지만 실제 선언 변경 없음 | changed=false, 원문 bytes·revision 유지 |

version을 null로 지우면서 version_args를 유지하면 최종 Tool 검증이 실패할 수 있다.
의존 필드까지 함께 지우는 동작은 자동 추론하지 않는다.
전체 Tool 교체와 부분 수정의 차이를 help의 최소 예제에서 설명한다.

### Batch

예시 본문은 같은 요청에서 도구와 preset을 등록하고 명령이 선택하게 한다.

```json
{
  "operations": [
    {"op":"tool.add","name":"node","value":{}},
    {"op":"preset.add","name":"web","value":{"tools":["node"]}},
    {"op":"command.add","name":"dev:web","value":{"exec":["node","scripts/dev.mjs"],"requires":["web"]}}
  ]
}
```

전체 본문은 Edit다. operation별 필수 필드는 schema의 Operation에 정의한다.
project.update는 profile·bundle·env_files·defaults만 변경한다.
tool/preset/command/port/proxy에는 add/update/remove를 제공하고 command에만 rename을 제공한다.

한 요청에서 같은 (resource kind, literal name)을 두 번 대상으로 삼으면 invalid_argument다.
project.update도 한 번만 허용한다. rename은 원래 이름과 목적지 이름을 모두 점유한다.
remove+동일 이름 add, rename chain과 동일 resource의 여러 update를 거절한다.
요청 순서는 diff의 표시에만 사용하며 참조는 최종 후보에서 검사한다.
따라서 새 도구 등록과 이를 사용하는 preset·command 추가를 한 요청에서 할 수 있다.
기존 후보를 전부 검증한 뒤 한 파일로 게시하며, 성공한 operation만 부분 저장하는 모드는 없다.

## Revision과 preview

revision은 64자리 소문자 hex의 불투명 토큰이다. 클라이언트는 계산하지 않고 inspect 결과를 사용한다.
서버는 domain separator, canonical config 경로, 파일 identity, 관찰한 소유권·mode·ACL과 원문 bytes를 SHA-256에 묶는다.
POSIX identity는 device/inode, Windows는 volume/file ID다. 길이 구분된 binary encoding을 사용해 경계 충돌을 막는다.
경로 spelling의 alias는 location.Canonical과 같은 규칙으로 해석한다.
동일 bytes의 파일 교체, 권한과 주석 변경도 revision을 바꾼다.

처리 순서는 입력 검증 → 대상 read → revision 비교 → 기존 설정 검증 → operation 평가 →
후보 재해석·보존 검증 → 게시 직전 재관찰 → 게시 → 결과 관찰이다.
invalid한 기존 설정을 semantic 편집으로 강제로 고치지 않는다. config check의 source 위치를 따라 먼저 수정한다.

dry-run은 apply와 같은 evaluator·writer를 실행해 후보를 만들지만 파일을 쓰지 않는다.
결과의 changed는 false, would_change는 원문과 후보 bytes가 다른지 여부다.
preview의 previous_revision과 revision은 모두 관찰 revision이다.
candidate_digest는 후보 bytes의 SHA-256이다. apply 전에는 새 파일 identity가 없으므로 다음 revision을 예측해 반환하지 않는다.
실제 apply는 이전 revision을 다시 검증하며, 미리보기 결과를 최신 파일에 자동 병합하지 않는다.

config migrate의 apply_digest는 다음 입력에 묶는다.
`SHA256(UTF8("config-migrate-v1") || NUL || UTF8(revision) || NUL || UTF8("10") || NUL || UTF8("registry-v1") || NUL || candidate_bytes)`.
NUL은 byte 0이고, ||는 byte 연결이다.
동일 revision·protocol·변환 규칙의 같은 후보만 같은 digest를 갖는다.
preview와 apply 사이에 parser/writer 규칙이 바뀌면 다시 preview해야 한다.
--apply가 틀리면 config_migration_conflict이며 파일을 쓰지 않는다.

## 성공 응답

각 명령의 data는 첫 표의 $defs를 사용한다. 추가적인 임의 필드를 반환하지 않는다.

| EditResult 필드 | 의미 |
| --- | --- |
| config_path | 선택한 canonical 파일 경로 |
| previous_revision | 읽고 검증한 이전 revision |
| revision | apply 후 관찰 revision. preview/no-op은 previous_revision과 같음 |
| changed | 이 요청이 bytes를 게시했는지 여부 |
| would_change | 원문과 계산한 후보 bytes가 다른지 여부 |
| candidate_digest | 계산한 후보 bytes의 SHA-256 |
| changes | 작업별 변경 source path. 값·전체 argv·원문 diff는 포함하지 않음 |
| impact | 영향 명령의 정렬된 이름과 수, 외부 사용·실행 process는 not_checked |
| format·candidate_format | 원문과 후보의 형식 |

changes의 op는 요청의 typed operation, 또는 format/migrate다.
rename에는 이전 path와 to_path를 제공한다. 배열 path의 index도 문자열로 표현한다.
유효한 단건 no-op은 changes=[]다. 한 batch의 no-op operation은 changes에서 생략한다.
impact.commands는 전후 effective command 또는 실행 context가 바뀐 이름의 합집합이다.
rename은 이전·이후 이름을 모두 포함하며 command_count는 배열 길이다.
사용하지 않는 도구의 변경과 순수 format은 영향 명령이 0개다.
migration의 동등성 검증이 성공한 경우에도 provenance·작성 형식 변경은 changes에 남기고 실행 의미의 impact는 0개다.

MigrationResult는 EditResult에 equivalent와 apply_digest를 추가한다.
성공의 equivalent는 항상 true다. 동등성 실패는 성공 보고서 대신 config_migration_conflict와
불일치 source path를 causes로 반환한다. apply는 유효한 digest를 요구한다.
직접 원문을 확인하려면 사용자가 파일을 읽는다. 자동 preview·오류에는 raw 설정을 첨부하지 않는다.
실제 저장은 실행 중 process의 적용 상태를 증명하지 않는다.

## 저장·동시성·권한

### 대상과 잠금

탐색은 기존 .git/worktree 경계를 따른다. 자동으로 상위 경계 밖 파일이나 다른 새 파일을 만들지 않는다.
생성은 기존 init이 담당하며, 이번 편집 API는 존재하는 파일만 대상으로 한다.

읽기는 기존 symlink 탐색 호환성을 유지한다. 쓰기는 최종 config entry가 regular file이고
hardlink 수가 1일 때만 허용한다. 최종 symlink/reparse point·특수 파일·read-only 속성은
config_unsafe_target 또는 io_error로 거절한다. 부모 alias는 canonical 경로로 해석한 뒤 handle identity를 다시 확인한다.

같은 사용자·runtime namespace의 CLI writer는 runtime/config-edit 아래의 경로 hash별 OS lock을 사용한다.
Windows lock key의 경로 spelling은 case folding해 alias가 별도 lock을 만들지 않게 한다.
다른 사용자, 별도 isolated-root의 runtime, 잠금을 따르지 않는 외부 editor는 이 협력 범위 밖이다.
이 경우에도 게시 직전 identity·bytes·권한을 재검사하지만 마지막 검사와 교체 사이의 완전한 CAS는 보장하지 않는다.
이 제한을 성공 메시지에서 외부 editor까지 보호했다는 주장으로 바꾸지 않는다.

잠금 대기는 최대 2s이며 caller deadline이 더 짧으면 그 기한을 따른다.
경합 종료는 config_busy/exit 3, 명시 취소는 canceled/exit 130이다.
2s는 기존 Windows 파일 게시의 제한 시간과 맞춘다.
읽기·check·explain은 writer lock 파일을 만들지 않는다. no-op은 파일을 게시하지 않는다.

### 게시

project-file 전용 adapter를 사용한다. private storage의 ACL 강제 helper를 그대로 적용하지 않는다.

1. 같은 디렉터리에 예측 불가능한 candidate 이름을 create-exclusive로 만든다.
2. 현재 파일의 소유권·mode·ACL을 보존한다. 보존을 확인할 수 없으면 원본을 쓰기 전에 중단한다.
   Windows candidate의 데이터 handle과 보안 변경 권한을 구분한다. ReOpenFile로 같은 kernel object에
   WRITE_DAC을 요청하며 소유자·group이 다른 경우에만 WRITE_OWNER를 추가한다. 권한을 얻지 못하면 거절한다.
3. candidate를 write·flush·close하고 다시 parse·semantic·comment 검증한다.
4. 원본 identity·bytes·권한을 재확인한다. 차이가 있으면 config_revision_conflict다.
5. POSIX는 같은 filesystem의 rename 후 parent directory sync를 사용한다.
6. Windows는 같은 volume의 ReplaceFileW를 사용하고 ACL merge 오류 무시 flag는 사용하지 않는다.
   교체 candidate의 보안 handle을 share-delete로 고정하고 file ID를 확인한다. ReplaceFileW가
   상속 ACE를 명시 항목으로 복사한 뒤 부모 ACE를 중복 전파할 수 있으므로, 고정한 handle에
   원본 DACL과 보호 상태를 복원한다. 경로를 다시 열어 후속 작성자의 권한을 변경하지 않는다.
   복원 실패는 이미 게시된 상태의 결과 불명 오류로 보고하고 backup을 보존한다.
   기존 파일의 복구용 backup 이름을 지정하고, 성공 후 대상·권한을 검증한 뒤 backup을 제거한다.
   sharing violation의 제한된 재시도마다 원본·candidate identity를 재검사한다.
7. 게시 결과를 확인한 뒤 반환한다. orphan candidate는 원본이나 다른 writer의 파일임이 아닌 것을 확인한 경우만 정리한다.

성공은 OS가 확인한 게시 완료를 의미한다. 임의 filesystem·원격 share·전원 장애까지의 durability를 약속하지 않는다.
필요한 게시·권한 보존 기능이 없으면 config_write_unsupported로 쓰기를 거절한다.
성공 전에 원본을 삭제하거나 직접 truncate하는 fallback은 없다.

| 관찰한 상태 | 응답과 후속 동작 |
| --- | --- |
| 게시 전 실패, 원본 identity·bytes 유지 확인 | 해당 오류, effects=none |
| 게시 성공, candidate bytes와 최종 identity 확인 | 성공, changed=true |
| 게시 API 성공 후 sync/관찰 실패 | config_commit_uncertain, effects=committed, retry=inspect_first |
| 게시 오류 후 원본 유지 확인 | io_error 또는 구체적 오류, effects=none |
| 게시 오류 후 원본·후보·backup의 상태를 확정하지 못함 | config_commit_uncertain, effects=unknown, retry=inspect_first |

API 성공을 확인한 뒤 도착한 취소는 완료를 되돌리지 않는다.
API 호출 중 취소되어 게시 여부를 확정하지 못하면 결과 불명으로 보고한다.
ReplaceFileW의 부분 실패에서는 원본이 backup 이름에 있을 수 있다.
이때 backup과 candidate를 보존하고 공개 가능한 basename·candidate_digest를 facts에 제공한다.
오류 응답만으로 원본 무변경이나 안전한 재시도를 추정하지 않는다.

### 결과 불명 복구

config inspect로 현재 revision·문서를 조회하고 의도한 변경과 비교한다.
candidate_digest가 현재 파일과 같으면 저장된 결과를 확인할 수 있지만 최초 요청의 replayed라고 보고하지 않는다.
대상 파일이 없거나 backup만 있으면 제공된 basename을 같은 디렉터리에서 확인한다.
다른 writer의 변경 여부를 확인한 뒤 사용자가 파일을 복원한다. 자동 rollback으로 나중 변경을 덮어쓰지 않는다.
그 뒤 새 revision을 읽어 남은 변경만 새 요청으로 수행한다.
이 범위에는 durable request receipt와 config recover 명령을 추가하지 않는다.

## 오류와 종료 코드

모든 오류 details는 공통 ErrorDetails다. target·source·reason·remedy와 effects를 제공한다.

| code | exit | 조건·재시도 |
| --- | ---: | --- |
| invalid_argument | 2 | 옵션·JSON·필드·이름·중복 작업 오류; 입력 수정 |
| io_error | 1 | JSON 입력 또는 config I/O·권한 실패; 안전한 OS reason 보존 |
| project_not_found | 3 | 선택 경계 안에 파일 없음; --dir 또는 init 확인 |
| invalid_config | 3 | 기존/후보의 문법·참조·정적 충돌; report 또는 causes 제공 |
| config_resource_exists | 3 | add 또는 rename 목적지 존재; 대상 재조회 |
| config_resource_not_found | 3 | update/remove/rename 대상 없음; 이름 확인 |
| config_reference_conflict | 3 | 삭제 후 참조가 남음; 같은 batch에서 사용처도 명시 수정 |
| config_revision_conflict | 3 | 관찰 revision 또는 게시 직전 상태 불일치; inspect 후 intent 재검토 |
| config_busy | 3 | 협력 writer lock 경합; 원본 무변경을 확인한 뒤 재시도 |
| config_migration_required | 3 | legacy semantic 편집; migrate preview |
| config_migration_conflict | 3 | apply digest 불일치·의미 동등성 실패; 새 preview |
| config_preservation_failed | 3 | 주석/source를 안전하게 편집할 수 없음; source 위치의 수동 정리 |
| config_unsafe_target | 3 | symlink/reparse·hardlink·특수 대상; regular 파일로 명시 정리 |
| config_write_unsupported | 3 | 필요한 파일 게시·권한 보존 기능 없음; 지원되는 파일 위치 사용 |
| config_format_required | 3 | fmt --check에서 변경 필요; EditResult를 details.report로 반환 |
| config_commit_uncertain | 1 | 게시/관찰 결과 불명; 자동 재적용 금지, inspect_first |
| command_not_found | 3 | explain 대상 미등록; command list 안내 |
| canceled | 130 | 명시 취소; 게시 단계에 따른 effects 유지 |
| deadline_exceeded | 3 | caller deadline 만료; 게시 단계에 따른 effects 유지 |

파일 크기 초과는 config 입력이면 invalid_config/reason=limit_exceeded,
JSON 본문이면 invalid_argument/reason=limit_exceeded다.
오류 우선순위는 처리 순서에 따른다. 후보에서 여러 문제가 발견되면 source 위치·code 순으로 전부 반환한다.

## 수용 기준

- create-only, 없는 대상, rename 충돌, literal dot·Unicode 이름과 case 구분.
- 미지·중복 JSON key, null, 빈 patch, 배열 교체, whole Tool override, 잘못된 참조.
- 같은 batch의 forward reference와 참조 삭제, 중복 resource operation, 실패 시 부분 저장 없음.
- 이전 revision, 같은 bytes의 파일 교체, 주석·권한 변경, 두 협력 writer와 외부 editor 경합.
- dry-run/apply의 같은 후보, no-op의 동일 bytes·revision, 같은 formatter의 두 번째 no-op.
- TOML 1.1·CRLF·quoted key·내부/standalone comment의 보존과 안전한 거절.
- 공통 Tool과 명령별 다른 Tool, 중복 key와 기본값을 포함한 합성 legacy migration의 모든 필드 동등성.
- POSIX rename·directory sync 실패, Windows ACL·sharing violation·ReplaceFileW 부분 실패.
- source read, candidate 생성·flush, 게시 직전·직후와 응답 유실 경계의 실제 process 종료.
- 결과 불명 복구에서 나중 writer 보존, basename 외 private 경로·원문 diff 비노출.

## 근거

이름·기본값·merge는 [project](../internal/project/project.go)와
[requirements](../internal/project/requirements.go), 입력 2 MiB 경계는 [기존 JSON reader](../internal/cli/tasks.go)를 따른다.
[현재 init](../internal/project/init.go)의 create-exclusive 게시를 유지한다.
[private Windows 게시](../internal/platformfs/private_windows.go)는 재시도·identity 검사 관례의 근거이며
그 ACL 정책을 프로젝트 파일로 옮기는 근거는 아니다.

[Go Rename](https://go.dev/src/os/file.go)은 모든 플랫폼에서 같은 atomicity를 보장하지 않으므로 adapter를 분리한다.
[ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)의 ACL 보존과
오류별 파일 상태를 기준으로 Windows backup·결과 불명 분기를 정한다.
[SECURITY_INFORMATION](https://learn.microsoft.com/en-us/windows/win32/secauthz/security-information)의
보안 변경 access mask와 [ReOpenFile](https://learn.microsoft.com/en-us/windows/desktop/api/winbase/nf-winbase-reopenfile)을
따라 candidate의 보안 handle을 얻는다. 일반 데이터 쓰기 handle이 ACL 변경 권한도 가진다고 가정하지 않는다.
이는 설계 근거이며 native 실행을 완료했다는 의미가 아니다.
