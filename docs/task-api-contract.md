# Task API 상세 계약과 전이 검증

task·workstream의 공개 계약이다. [workstream 설계](task-workstream-design.md)의 기본 동작을 입력·출력·전이·오류 기준으로 구체화한다. 명령별 입력과 JSON 본문 스키마는 `devtools schema`로 확인한다.

## 공통 입력

모든 본문은 UTF-8 JSON 객체이며 알 수 없는 필드·중복 JSON 키는 `invalid_argument`다. 파일은 일반 파일, stdin은 파이프나 파일 리다이렉션을 사용한다. JSON 입력과 본문에 해당하는 CLI 옵션은 택일이며 호출 제어 옵션은 함께 사용할 수 있다. 파일명은 자유다.

| 종류 | 계약 |
| --- | --- |
| ID | 생성 시 발급하는 UUID 문자열. 전체 ID로 참조하고 이후 유지한다. |
| profile | 기존 profile 문자·길이·해석 규칙. 명시적 옵션이 devtools.toml보다 우선. |
| title | 공백만으로 구성되지 않은 문자열, 최대 200자. 원문 보존. |
| text / reason / summary | 공백만으로 구성되지 않은 문자열, 최대 16,384자. |
| Markdown body | 최대 1,048,576바이트. draft에서는 빈 문자열 허용. |
| 문서 key | `^[A-Za-z][A-Za-z0-9_-]{0,63}$`, 문서 종류와 workstream 안에서 고유. |
| revision | 0 이상의 정수. 최초 task 저장소 조회는 0. |
| timestamp | UTC RFC 3339. 적용 시각은 도구가 기록. |
| 배열 | ID·key 집합은 중복 제거 후 정렬. 설명·체크포인트 배열은 입력 순서 유지. |

본문의 누락 필드는 아래 기본값을 적용한다. `null`은 명시적으로 nullable인 필드에서만 허용한다. update는 허용된 필드의 부분 수정이며 배열은 전체 교체한다. spec set·plan set은 전체 문서 교체다. 의미가 같은 update는 `changed: false`로 처리한다.

### 호출 제어

| 옵션 | 적용 |
| --- | --- |
| `--request-id UUID` | task 도메인의 모든 변경에 필수. dashboard 서버 관리와 조회에는 적용하지 않음. |
| `--if-revision N` | 문서·관계·정의 수정, 연결 변경, hold/unhold, 취소·재개·활성화·마감, 면제·면제 철회에 필수. |
| `--context REF` | resume·checkpoint·release·done·task 검증 결과 기록에 필수. `DEVTOOLS_TASK_CONTEXT`를 기본값으로 사용. |
| `--expected-run UUID` | takeover에 필수. 현재 점유의 run과 일치해야 함. |
| `--dir PATH` | claim·takeover·current. 기본값은 현재 디렉터리. |

add·workstream create·claim·takeover·실행 기록 추가는 별도의 profile 리비전 없이 현재 상태를 원자적으로 검사한다. workstream 통합 검증 기록은 해당 검증의 기준 식별자를 검사한다. CLI나 이벤트 기록에서 임의의 `state`를 받는 수정 API는 제공하지 않는다.

claim의 task ID와 `--workstream`은 택일이다. 둘 다 생략하면 현재 profile의 ready task 전체에서 선택한다. `--dir`은 실행 연결만 선택하며 profile은 호출 시 선택한 값을 유지한다. task list의 `--state`는 open·done·canceled·all, workstream list는 draft·active·done·canceled·all을 받는다. 목록의 페이지 옵션은 `--limit`과 `--cursor`다. export의 대상 ID는 필수다.

## 입력 객체

표의 `?`는 선택 필드다. 별도 기본값이 없는 선택 문자열은 빈 문자열, 선택 배열은 빈 배열이다. 대상 ID는 같은 profile 안에서 존재해야 한다.

| 요청 | 본문 |
| --- | --- |
| workstream create | `title`, `description?` |
| task add | `title`, `description?`, `workstream_id?`, `acceptance?: string[]`, `acceptance_keys?: string[]` |
| task update | `title?`, `description?`, `acceptance?`, `acceptance_keys?`; 하나 이상 필요. 소속은 attach/detach 사용. |
| spec set | `body`, `requirements: {key,text}[]`, `acceptance: {key,requirement_keys,text}[]` |
| plan set | `body`, `task_ids: UUID[]`, `validation_ids: UUID[]` |
| depends set | `depends_on: UUID[]` |
| checkpoint | `summary`, `decisions?: string[]`, `validation_record_ids?: UUID[]`, `remaining?: string[]`, `next_action?`, `blockers?: string[]` |
| release | 선택적인 checkpoint 본문. 본문 생략도 허용. |
| done | `summary`, `validation_record_ids?: UUID[]`, `commits?: {repository,commit}[]` |
| validation add | `title`, `method`, `required?: boolean = true`, `task_id?`, `workstream_id?`, `acceptance_keys?: string[]` |
| validation record | `result`, `summary`, `basis_id`, `evidence: {kind,reference,description}[]` |

validation 소유 대상은 task_id 또는 workstream_id 중 정확히 하나다. task의 workstream은 해당 task에서 해석한다. 독립 task의 acceptance_keys는 빈 배열이며 자체 acceptance로 완료 조건을 기술한다. task의 자체 완료 조건은 task 검증과 완료 요약으로 설명한다.

validation 정의 변경은 `validation update VAL_ID --file FILE`로 수행한다. title·method·required·acceptance_keys의 부분 수정을 받고 대상 소유자는 유지한다. 정의 변경에는 if-revision과 영향 검사를 적용하고 새 정의 리비전을 발급한다. 기존 결과와 면제 기록은 이전 정의에 남는다.

`result`는 pass·fail·blocked·skipped, evidence의 `kind`는 command·file·commit·url·note다. reference는 명령문·경로·커밋·URL·노트 참조 문자열이다. 증거를 읽거나 명령을 실행하는 부작용 없이 기록하며, 실제 검증은 호출 에이전트가 수행한다.

요구사항과 완료 조건은 서로 참조가 유효해야 한다. 문서 key는 유지하고 같은 의미의 문구 수정에는 리비전을 추가한다. 다른 의미의 조건은 새 key를 사용한다. 활성화된 기준을 삭제·변경하면 영향 검사를 적용한다.

### 구조 검사와 완료 조건

workstream activate는 목표, 최소 하나의 요구사항·완료 조건·task, 비어 있지 않은 명세·계획 본문을 요구한다. 각 요구사항은 완료 조건에, 각 조건은 task에, 각 구현 task는 최소 하나의 검증 정의에 연결한다. 검증 작업을 별도 task로 만들 경우도 자신이 수행한 결과를 기록할 검증 정의를 연결한다. 계획의 task_ids는 소속 task 전체, validation_ids는 관련 검증 정의 전체와 일치해야 한다.

active에서 새 task·검증을 등록하면 목록과 구조 검사에 즉시 나타난다. 새 항목은 계획과 검증 연결을 완성할 때까지 claim할 수 없다. 기존 조건을 바꾸지 않는 새 참조의 추가는 기존 run의 기준을 바꾸지 않는다. task의 제거·이동 후 남은 계획 참조는 같은 변경에서 정리하며 수정 리비전을 기록한다.

독립 task는 명세·계획 문서 없이 사용할 수 있다. done에는 자체 acceptance와 완료 요약을 확인하고 등록된 필수 검증을 충족한다. 필수 검증이 0개인 간단한 task도 완료 가능하다.

## 응답 객체

기존 `schema_version`, `ok`, `data`/`error` 봉투를 사용한다. 다음 객체는 모두 JSON으로 직렬화하며 실제 구현 시 `schema`의 출력 정의에도 같은 필드를 제공한다.

| 결과 | data 필드 |
| --- | --- |
| 단건 조회 | `profile`, `revision`, `item` |
| 목록 | `profile`, `revision`, `items`, `next_cursor: string|null` |
| 일반 변경 | `profile`, `revision`(적용 당시), `current_revision`, `request_id`, `replayed`, `changed`, `action_ids: UUID[]`, `item` |
| claim / takeover | 일반 변경 필드와 `claimed`, `run: object|null`, `context: string|null`, `context_valid`, `blockers` |
| next | `profile`, `revision`, `item: task|null`, `reason` |
| current | 목록 형식의 `items: run[]`. 디렉터리에 연결된 현재 run만 반환. |
| check | `profile`, `revision`, `valid`, `issues: {code,target_id,message}[]` |
| impact | `profile`, `revision`, `target_ids`, `affected_task_ids`, `affected_workstream_ids`, `running_ids`, `completed_ids`, `allowed`, `blockers` |
| tree | `profile`, `revision`, `nodes`, `edges: {from,to}[]`, `roots: UUID[]`, `truncated`, `continuations: {id,direction,depth}[]` |
| history | 목록 형식의 `items: action[]` |
| export | `profile`, `revision`, `item`, 관련 `documents`, `tasks`, `validations`, `history` |

task item은 `id`, `title`, `description`, `workstream_id: UUID|null`, `acceptance`, `acceptance_keys`, `state`, `running`, `ready`, `blockers`, `current_run: run|null`, `created_at`, `updated_at`, `definition_revision`을 가진다. blocker는 `code`, `target_id: UUID|null`, `message`다.

workstream item은 `id`, `title`, `description`, `state`, `depends_on`, `blockers`, `spec_revision`, `plan_revision`, `counts`, 생성·변경 시각을 가진다. counts는 task 상태별 수이며 진행률로 해석하지 않는다. 상세 문서 본문은 context 조회의 documents 항목으로 제공한다.

run은 `id`, `task_id`, `state`(running·released·taken_over·completed), `previous_run_id: UUID|null`, `directory`, 시작·마지막 활동 시각, `ended_at: timestamp|null`, 기준 spec·plan·task 정의 리비전을 가진다. 일반 run 객체에는 컨텍스트나 점유 증명을 넣지 않는다.

각 변경이 여러 업무 기록을 원자적으로 적용할 수 있어 action_ids는 배열이다. no-op은 빈 배열이며 업무 리비전을 증가시키지 않는다. 단, 요청 처리 영수증은 보존한다. 소속·graph·문서 관계 변경과 부수 효과는 같은 원자적 단위에 포함한다.

context 조회는 목표·문서 본문·최근 결정·현재 실행·다음 행동과 각 기록의 참조를 제공한다. 최대 응답은 2MiB이며 넘으면 `truncated: true`와 생략 문서 ID를 반환한다. `workstream spec show`, `workstream plan show`, `checkpoint list RUN_ID`, `validation show VAL_ID`로 필요한 내용을 추가 조회한다.

## 전이 표

모든 변경에서 입력·대상 범위·현재 리비전이나 실행 증명·의존 조건을 검사한다. 아래 표는 현재 상태에 따른 추가 조건이다. 동일 요청 ID의 재시도는 전이를 다시 적용하지 않는다.

| action | 이전 | 이후 | 추가 조건 |
| --- | --- | --- | --- |
| task create | 없음 | open | 소속을 지정하면 draft 또는 active workstream. |
| claim | open, ready | open + running | 같은 task의 점유 없음. workstream task는 계획 연결과 활성화 확인. |
| resume / checkpoint | open + running | 유지 | 현재 run 컨텍스트. |
| takeover | open + running | open + 새 running | 관찰한 run 일치, 이전 컨텍스트 폐기, 현재 실행 조건 충족. |
| release | open + running | open, 점유 없음 | 현재 run 컨텍스트. 반납 자체는 선행 조건과 무관하게 허용. |
| done | open + running | done, 점유 없음 | 현재 컨텍스트, 현재 기준 검증·완료 근거. |
| hold / unhold | open, 점유 없음 | open | hold 사유 변경. 같은 사유 재설정·이미 해제는 no-op. |
| task cancel | open, 점유 없음 | canceled | 후행 영향 검사. done은 cancel 대신 reopen 검사 필요. |
| task reopen | done 또는 canceled | open | 후행 done·running 보호. 소속 workstream은 draft 또는 active. |
| workstream activate | draft | active | 구조 검사 통과. |
| workstream close | active | done | 선행 workstream done, 모든 소속 task 종료, 전체 조건·통합 검증 충족. |
| workstream cancel | draft 또는 active | canceled | 보호할 후행 완료·현재 점유 없음. 소속 open task 취소를 함께 적용. |
| workstream reopen | done 또는 canceled | draft | 외부 후행 done·running 보호. 내부 task 결과 유지. |

새 요청 ID로 종료된 run에 done·release를 보내면 transition_conflict 또는 context_invalid다. 정확한 기존 요청의 재시도는 영수증을 복구한다. 반복 activate·close·cancel처럼 이미 같은 수명주기 상태인 새 요청은 transition_conflict다. 내부 순서에 의존해 조용히 재실행하지 않는다.

### 영향 범위의 구분

`affected`는 변경 대상과 영향받는 모든 항목, `protected_successors`는 의존 관계상 후행 항목이다. reopen 대상 자신의 완료는 허용 전이이며 그 자체가 충돌 원인이 아니다. workstream에 속한다는 소속 관계만으로 내부 done task를 외부 후행 완료로 취급하지 않는다.

task 정의·의존성 변경은 해당 task와 task DAG의 후행 경로를 검사한다. workstream의 현재 완료 보장을 바꾸는 cancel·reopen은 workstream DAG의 후행 경로와 거기에 속한 task를 검사한다. 현재 점유는 두 범위 모두에서 보호한다. workstream reopen은 내부 완료 기록을 그대로 유지한다.

명세·계획의 기존 본문과 조건 변경은 소속 task의 실행 기준에 영향을 주므로 관련 done task의 명시적 reopen이 필요하다. task를 후행부터 역순으로 재개하면 다음 선행 재개가 가능하다. 이미 완료된 후행 결과를 유지하려면 후속 수정 workstream으로 진행한다.

attach/detach에는 양방향 task 의존 관계와 acceptance_keys가 없어야 한다. validation은 task 소유이면 같이 유지하되 workstream 조건 참조를 먼저 정리한다. 기존·새 workstream 계획 참조의 제거·추가를 같은 action 묶음으로 처리한다. 새 workstream의 구조 검사는 다시 계산한다.

## 검증 기준과 리비전

profile revision은 동시 수정과 캐시 무효화 기준이다. 검증 근거의 유효성은 별도의 불변 `basis_id`로 판정한다. 검증 대상의 정의·완료 조건·연관 명세·계획·코드 증거를 기준으로 구성하며 무관한 checkpoint나 다른 task의 변경은 기준을 바꾸지 않는다.

`validation basis VAL_ID --file FILE`로 확인할 코드 상태와 범위를 제시하고 basis_id를 받는다. 파일 본문은 `code: {repository,commit,dirty,evidence}[]`이며 Git 밖에서는 commit을 null로 두고 파일 근거를 사용한다. 기준 생성은 요청 ID를 받는 기록 action이다. 대상 정의가 바뀌면 새 basis를 생성한다.

검증 결과 기록은 제시한 basis가 대상 정의에 여전히 적용 가능한지 검사한다. 코드 상태는 에이전트가 검증 직전에 확인한 값으로 기록하며 CLI가 커밋 문자열만 보고 파일 일치를 보증하지 않는다. pass 기록은 자신이 추가된 profile revision 때문에 무효화되지 않는다.

done·close는 적용 가능한 최신 검증 결과와 basis를 참조한다. 다른 run으로 인계해도 대상 기준과 코드가 같으면 기존 근거를 명시적으로 재사용할 수 있다. `validation accept VAL_ID --basis BASIS_ID --record RECORD_ID --reason TEXT`로 재사용 판단을 기록하며 task 소유 검증에는 현재 컨텍스트가 필요하다.

waive는 해당 검증 정의 리비전·basis에만 적용한다. `validation unwaive VAL_ID --reason TEXT`로 철회한다. 면제도 현재 결과에 적용되는 명시적 근거이며 변경된 정의에 자동 상속하지 않는다. 면제의 이유는 누가 요청했는지와 함께 기록하지만 CLI 입력만으로 별도의 사용자 승인을 입증했다고 주장하지 않는다.

## 재시도와 실패 우선순위

1. 입력을 파싱하고 profile을 선택한다.
2. 같은 요청 ID의 영수증이 있으면 정규화된 입력과 원래 실행 증명 연결을 비교한다.
3. 일치하면 원래 적용 결과와 현재 리비전을 반환한다. 원래 점유가 끝났다면 context는 null, context_valid는 false다.
4. 새로운 요청은 대상 존재, 리비전, 현재 컨텍스트 또는 expected-run, 전이·의존·검증을 검사한다.
5. action·결과 영수증·점유 정보를 함께 커밋한 뒤 응답한다.

입력 동일성에는 명령·대상·정규화된 본문·제어 조건을 포함하며 request-id 자체와 파일 경로는 제외한다. 파일은 내용으로 비교하고 실행 컨텍스트는 원래 실행의 증명 연결로 비교한다. done 재시도에서는 같은 원래 증명을 검증할 수 있도록 검증용 정보를 보존하며, 그 증명으로 새 변경은 허용하지 않는다.

실패한 새 요청은 적용 영수증을 만들지 않는다. 성공했는지 응답을 받지 못한 경우 같은 요청 ID로 재시도한다. 충돌을 읽고 입력이나 리비전을 바꾼 새 판단에는 새 요청 ID를 사용한다. 자동 claim의 claimed:false도 성공 영수증으로 저장하므로 나중에 새 작업을 선택할 때 새 요청 ID를 사용한다.

JSON 오류는 invalid_argument, 대상 없음은 not_found, 리비전 불일치는 revision_conflict, 기존 ID의 다른 입력은 request_conflict, 증명 불일치는 context_invalid, 점유 경쟁은 claim_conflict, 관계 조건은 dependency_conflict, 허용되지 않은 action은 transition_conflict, 필수 근거 부족은 validation_required다. 구조 check는 valid:false를 정상 조회로 반환하지만 activate는 구조 실패 시 validation_required로 변경을 거절한다.

## Dashboard·조회 경계

dashboard의 start/status/stop은 작업 action과 분리된 서버 관리다. start는 `{url, server_id, running, initial_profile}`, status는 `{running, server_id|null}`, stop은 `{stopped}`를 data에 반환한다. 실행 중 서버가 없을 때 status·stop은 정상 결과다.

로컬 조회 서버는 Host와 Origin을 검사하고 외부 사이트가 localhost API를 읽는 것을 차단한다. 접속 링크의 bootstrap 증명은 5분 동안 한 번 사용할 수 있으며, 교환한 브라우저 세션은 마지막 요청부터 8시간 또는 서버 종료까지 유지한다. 세션 증명은 포트를 포함한 origin의 sessionStorage에 보관하고 API 요청의 Authorization 헤더로 전달한다. 링크가 만료되면 dashboard 명령으로 새 링크를 받는다. 서버 실행은 브라우저를 자동으로 열지 않고 URL을 반환한다. dashboard 시작은 기존 서버의 인증 프로토콜을 확인하고 이전 방식의 서버를 교체한다.

목록 cursor는 조회 범위·필터·리비전에 연결하며 잘못되거나 만료되면 `cursor_invalid`와 종료 코드 3으로 새 조회를 안내한다. tree의 잘린 가지는 같은 기준 리비전으로 이어 조회할 수 있는 cursor를 제공한다. 캐시를 잃으면 새 리비전의 전체 조회부터 다시 시작한다.

export와 history는 컨텍스트와 점유 증명을 제거한 공개 기록 형태를 사용한다. 생성·인계 영수증도 일반 이력에 노출할 때 권한 데이터를 제거한다. profile 전체 revision은 var/sec 저장소와 별개의 task 도메인 리비전이다.

## 계약 검증 시나리오

| ID | 시나리오 | 기대 결과 |
| --- | --- | --- |
| C01 | 두 claim이 같은 ready task를 요청 | 현재 점유 하나, 충돌 요청의 부수 action 없음. |
| C02 | 같은 run을 두 takeover가 관찰 | 첫 인계만 성공, 두 번째는 claim_conflict. |
| C03 | done과 takeover 경쟁 | 먼저 적용된 action을 기준으로 다른 새 요청 거절. |
| C04 | done 성공 응답 유실 후 같은 요청 재시도 | 원래 완료 결과 반환, 새 action 없음, context_valid:false. |
| C05 | 인계 전 컨텍스트로 새 checkpoint | context_invalid. |
| C06 | auto claim의 작업 없음 재시도 | 같은 빈 결과 유지, 새 요청 ID면 새 후보 선택. |
| C07 | 검증 결과 기록으로 profile revision 증가 | 해당 pass는 같은 basis에 계속 유효. |
| C08 | 선행 task 재개 시 자신만 done | 후행 done·running이 없으면 open으로 재개. |
| C09 | workstream 재개 시 내부 task가 done | 내부 기록 유지, 외부 후행 조건만 별도로 검사. |
| C10 | 의존성 변경과 후행 claim 경쟁 | 원자적 순서에 따라 변경 또는 claim의 조건 검사 실패. |
| C11 | task 이동 시 기존 계획이 참조 중 | 관계 조건 통과 후 계획 참조까지 함께 갱신, 고아 참조 없음. |
| C12 | 아직 문서 참조가 비어 있는 draft | 저장 가능, check valid:false, activate 거절. |
| C13 | canceled 선행 workstream | 후행 계획 작성 가능, task claim은 blocked. |
| C14 | dashboard 종료 후 기존 링크·세션 | 접근 거절, task 점유와 이력 유지. |
| C15 | claim 영수증을 history/export에서 조회 | 컨텍스트와 점유 증명 제외. |

위 시나리오는 전이·응답의 일관성 기준이다. `internal/tasks`의 race 테스트와 `docker/scenarios/tasks.py`의 설치 검증에서 동시 점유, 인계, 이전 컨텍스트 거절, 완료 재시도, worktree 공유, 검증 근거와 마감, 조회 세션 경계를 확인한다. `just verify-docker`로 실행한다.
