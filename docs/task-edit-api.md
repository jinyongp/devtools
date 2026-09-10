# 계획 편집 API

계획 정의와 실행·완료 이력을 분리한다. draft/active/done/canceled 및 실행 중에도 삽입·변경·범위 제외·복원이 가능하다. 불완전한 coverage는 저장하고 issues로 반환하며 claim/sync/close에서 필요한 조건을 검사한다.

## 요청

```sh
devtools task workstream edit WORKSTREAM_ID --file edit.json --if-revision N --dry-run
devtools task workstream edit WORKSTREAM_ID --file edit.json --if-revision N --request-id UUID
```

본문은 `{reason,operations}`이며 1–200개 operation을 받는다. preview의 요청 ID는 선택이며 영수증·업무 상태·저장 버전을 바꾸지 않는다. 실제 저장은 같은 평가기를 사용한다. 그사이 리비전이 바뀌면 최신 계획으로 재평가한다. 전체 결과가 2 MiB를 넘으면 저장 전에 거절한다.

| operation | 필드 |
| --- | --- |
| workstream.update | `value`에 metadata 부분 수정. 초기 지원 필드는 `title`, `description` |
| spec.update / plan.update | `value: {body}` |
| requirement.add | `value: {key,text}` |
| acceptance.add | `value: {key,text,requirement_keys}` |
| requirement.update / acceptance.update | `key`, `value`에 정의 필드 부분 수정 |
| requirement.remove/restore / acceptance.remove/restore | `key` |
| task.add | 선택 `ref`, `value: {title,description?,acceptance?,acceptance_keys?}` |
| task.update | `id`, `value`에 task 정의 필드 부분 수정 |
| task.remove/restore / validation.remove/restore | `id` |
| validation.add | 선택 `ref`, `value: {title,method,required?,task_id 또는 workstream_id,acceptance_keys?}` |
| validation.update | `id`, `value`에 title/method/required/acceptance_keys 부분 수정 |
| task.depends.set | `id`, `depends_on: ID[]` |
| workstream.depends.set | `depends_on: UUID[]` |
| task.move | `id`, `before_id` 또는 `after_id` 중 하나 |

새 ID는 요청 내부 `@ref`로 앞·뒤 참조한다. task 소속은 edit 대상 workstream이며 validation은 정확히 한 owner를 가진다. 소유권·참조 존재·DAG는 최종 후보에서 검사한다. 동일 항목의 add/remove, remove/restore 조합은 오류다. 여러 update는 입력 순서대로 적용하고 빈 update는 거절한다. 알 수 없는 필드와 중복 JSON 키도 거절한다.

예를 들어 기존 작업 뒤에 검증 작업을 삽입한다.

```json
{
  "reason": "추가 경로 검증",
  "operations": [
    {"op":"task.add","ref":"check","value":{"title":"추가 경로 검증","acceptance_keys":["A1"]}},
    {"op":"validation.add","value":{"title":"실행 결과 확인","method":"통합 테스트","task_id":"@check"}},
    {"op":"task.depends.set","id":"@check","depends_on":["EXISTING_TASK_UUID"]},
    {"op":"task.move","id":"@check","after_id":"EXISTING_TASK_UUID"}
  ]
}
```

## 범위와 이력

task.remove는 논리적 제외다. 이력·점유를 유지하며 활성 의존성의 양방향 연결과 task의 완료 조건 연결을 정리한다. A→B→C에서 B를 제외해도 A→C를 만들지 않는다. task.restore는 같은 ID를 포함하지만 과거 task 연결·근거를 되살리지 않는다. 필요한 관계는 같은 요청에서 설정한다.

owner 때문에만 숨겨진 validation은 task 복원 시 다시 포함된다. 명시 제외한 validation은 계속 제외된다. requirement/acceptance 제거는 참조를 정리하며 다른 정의를 삭제하지 않는다. ID와 문서 key는 재사용 대신 restore한다. validation restore는 현재 포함된 대상을 가리키는 정의 참조를 보존한다. 명시 설정한 관계가 최종 제외 대상을 참조하면 오류다. 반복 remove/restore는 최종 scope가 같으면 no-op이다.

metadata는 workstream 자체를 대상으로 하므로 operation에 ID를 받지 않는다. `title`과 `description`은 초기 지원 필드이며 하나 이상 필요하다. title은 표시 metadata라 의미 기준을 바꾸지 않는다. description은 workstream의 목표 정의에 포함되어 해당 workstream과 외부 후행의 현재 완료 기준에 영향을 주지만, 소속 task 정의에는 전파하지 않는다. 빈 description은 설명을 제거한다.

표시 순서는 의미 기준을 바꾸지 않는다. 본문 변경은 소속 전체, 구조화된 조건은 참조 작업과 후행에 전파한다. 무관한 task 추가·제외는 기존 task 완료를 유지한다. 의미 epoch는 단조 증가해 A→B→A나 제외→복원으로 근거가 부활하지 않는다. task.move는 표시 순서만 바꾸며 claim 우선순위는 생성 순번·ID다.

단건 workstream metadata update와 legacy task/validation 정의 명령, spec set, plan set, depends set도 같은 평가기를 사용한다. plan set의 목록은 전체 included membership 검증 입력이며 누락을 삭제로 해석하지 않는다. 소속 변경은 기존 attach/detach의 관계·점유 제약을 유지한다.

## 상태·실행·검증

| 필드 | 값 |
| --- | --- |
| state | 기존 lifecycle: task open/done/canceled, workstream draft/active/done/canceled |
| scope | included / removed. validation은 owner 제외도 반영 |
| completion_status | none / current / stale |
| execution_status | idle / current / stale / removed |
| needs_work / ready / blockers | 실행 필요성과 실제 점유 가능 여부 |
| definition_signature | 실행·검증에 사용하는 의미 기준 |
| last_completion_revision / change_reasons | 완료 이력과 현재 상태 설명 |

done+stale는 ready일 때 reopen+claim을 원자 기록한다. canceled는 명시 reopen, draft workstream은 activate가 필요하다. stale 실행은 현재 context와 reason, 관찰 리비전으로 `task sync`하여 같은 run을 최신 기준에 연결한다. sync는 검증을 수행하거나 pass를 만들지 않는다. stale/removed 실행도 checkpoint/release가 가능하다.

task basis에는 현재 context, workstream basis에는 관찰 --if-revision이 필요하다. definition_version과 의미 signature/run 연결을 보존한다. 늦은 record는 그 run에서 만든 이전 basis에 저장하고 applicable:false를 반환할 수 있다. 다른 run basis로 새 기록은 거절한다. takeover 후 새 basis를 만든 다음 code·의미 기준이 같은 pass만 validation accept로 명시 재사용한다. waiver는 현재 basis/epoch에만 유효하며 skipped는 면제가 아니다.

workstream 검증은 active 또는 activate 이력이 있는 done에서 허용한다. 현재 필수 근거 손실은 마감과 후행을 stale로 만들며 나중 pass만으로 자동 마감하지 않는다. 현재 coverage·통합 근거를 갖춘 뒤 명시 close가 필요하다. 제외 작업을 포함한 모든 running은 close를 막는다.

## 조회와 진단

edit 결과에는 dry_run, would_change, changes, effects, impact, issues, next_actions, created_refs, created_items가 추가된다. preview의 changed는 false다. required 변경과 필수 validation 제외도 effects에 기록한다.

기본 task 목록은 included open과 done+stale다. --state done은 lifecycle done 전체이며 --completion none/current/stale/all로 좁힌다. --scope included/removed/all로 제외 이력을 조회한다. completion/scope를 명시하고 state를 생략하면 기본 lifecycle 필터를 해제한다. workstream completion 필터도 같다. validation 기본 목록은 effective included다. next와 claim은 동일 판정을 사용한다.

`workstream plan show ID --at-revision N`은 완결된 요청 경계의 과거 문서·task·표시 순서를 반환한다. 중간 순번은 revision_not_committed다. task/validation history에도 해당 edit patch가 나타난다. cursor는 필터·리비전·projection version에 묶이며 오래된 캐시는 cursor_invalid로 재조회한다. 공개 조회에는 실행 context를 포함하지 않는다.

변경 판정 오류 details에는 current_revision, condition_code, 이름·상태, related, remedies를 제공한다. operation 입력 오류는 operation_index를 제공한다. remedies는 argv와 required_inputs로 표현하며 context 값은 포함하지 않는다. check는 valid, closable, execution_ready와 issues를 반환한다.

## 저장 호환성

v1은 읽을 수 있으며 첫 실제 업무 변경에서 profile.upgraded baseline과 요청 이벤트를 단일 저장으로 기록해 v2가 된다. read/no-op/preview는 업그레이드하지 않는다. 기존 유효 근거와 실행을 보존하고 이미 stale인 근거를 승격하지 않는다. 구버전 바이너리는 v2 읽기·쓰기를 거절하므로 공유 writer를 함께 업데이트한다. 알 수 없는 버전·action도 거절한다.

backup restore는 v1/v2 이력을 보존하고 source context/영수증을 폐기하며 running을 release한다. cleanup은 모든 running과 included 미완료·stale를 보호한다. excluded open만으로 마감된 workstream을 보류하지 않으며 canceled owner의 미충족 작업은 실행 의무에서 제외한다. Dashboard 서버 재사용은 인증·UI·task 프로토콜이 모두 일치해야 한다.
