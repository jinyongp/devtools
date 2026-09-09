# 계획 편집 회귀 검증

workstream `97b9d991-f326-49dc-b3c1-3d9b49413bfa`의 E01–E49 검토 항목을 아래 검증과 연결한다. task 저장소는 임시 디렉터리에서 검증하며 개발 중 바이너리를 사용자 전역 journal에 적용하지 않는다.

| 항목 | 검증 위치와 확인 범위 |
| --- | --- |
| E01–E02 | edit_test: 모든 lifecycle에서 편집·순서 변경, metadata의 근거 보존 |
| E03–E06 | assessment_test: 무관 작업·조건 보존, 직접 참조와 task/workstream 후행 전파, 자기 fingerprint 순환 방지 |
| E07–E09 | edit_test: 제외·복원, 의존성 정리, 문서 제거, validation의 명시 제외와 owner 제외 구분 |
| E10–E13 | edit_test: 앞·뒤 ref, 최종 DAG, 명시 제외 참조 거절, lifecycle 모순·표시 순서 |
| E14–E16 | edit_test / task_edit_test: preview의 journal 무변경, commit/replay, operation 200/201 경계; Decode와 저장 전 응답 크기 검사 코드 대조 |
| E17–E20 | execution_current_test: stale run sync, checkpoint/release, done+stale 원자 claim, 점유 보존; assessment_test의 draft/coverage 차단 |
| E21–E22 | execution_current_test / edit_concurrency_test: late record, takeover 후 현재 basis와 명시 accept, 이전 context 거절 |
| E23–E24 | edit_concurrency_test / edit_test: A→B→A의 epoch·waiver 무효화, required 검증 제외 감사, 검증 정의 복원 |
| E25–E28 | assessment_test / execution_current_test / backup_current_test: 완료 직후 proof, workstream 재마감, canceled/removed/current 구분 |
| E29–E32 | query_current_test: 필터·next 일치, 과거 계획, 제외 상세, 간접 영향 history; edit_test의 표시 순서 |
| E33–E35 | replay_test: 진짜 v1 fixture baseline, 유효 근거·현재 run 보존, stale 미승격, version/action/sequence·중간 요청 경계 거절 |
| E36 | task_edit_test / tasks_test / edit_legacy_test: 공개 CLI 입력·schema·preview/retry, legacy 정의 편집과 membership 검증 |
| E37 | dashboard/edit_test + Browser Plugin: 실행 중·완료 후 편집, preview/저장, stale·removed-running·복원; 실행 action 권한 거절 |
| E38 | edit_legacy_test / diagnostics.go: 대상 이름·조건·remedy argv; Dashboard는 사용자 문자열을 textContent로 표시 |
| E39 | query_current_test / dashboard/compatibility_test: 구 cursor 및 인증·UI·task protocol 불일치 거절 |
| E40–E41 | backup_current_test / backup·cleanup 패키지 및 Docker: v2 이력·scope 보존, context/receipt 폐기, stale/running archive 거절, excluded open 허용 |
| E42–E44 | edit_concurrency_test: edit와 edit/claim/done/record/sync/takeover/unclaim/close 경쟁, 점유 단일성·완료 유효성 |
| E45 | edit_concurrency_test: 원자 교체 전 실패의 byte 보존, 교체 후 오류와 영수증 복구. WritePrivate의 temp-write/fsync/rename 순서 코드 대조 |
| E46 | TestEditHonorsMaintenanceGate 및 maintenance_test: 복원·cleanup과 공유하는 gate 우선 잠금, 대기 취소·해제 후 실행, 복구 후 reader 일관성 |
| E47 | assessment_test: 1,000 task DAG의 반복 위상 계산과 결정론적 영향 |
| E48 | CLI 전체 및 Docker tasks/workflow/completion/discovery: 기존 독립 task, 설치된 명령·도움말·schema·스킬, shell completion |
| E49 | execution_current_test: close 후 basis 교체·fail/blocked/skipped·unwaive, pass 회복 후에도 명시 close 필요 |

구버전 호환성은 변경 전 commit `75fa433bc3c6834163c0888e6441bc8a122330b0`을 별도 임시 디렉터리에 추출해 subprocess로 컴파일·실행했다. 새 v2 fixture의 Read와 Execute가 모두 거절되고 journal 바이트가 바뀌지 않는 것을 확인했다.

Browser Plugin 검증은 `TestEditBrowserFixture`가 만드는 격리 profile을 사용한다. 선택적으로 `DEVTOOLS_EDIT_FIXTURE_OUTPUT`에 접속 링크를 저장할 private 파일 경로를 지정하고 해당 테스트를 실행한다. 테스트는 20분 후 서버와 임시 데이터를 정리한다. 링크와 실행 context는 공유 문서에 기록하지 않는다.

필수 통합 명령:

```sh
just check
pnpm test:dashboard
just verify-docker tasks workflow backup cleanup completion discovery
```

실제 검증 결과와 코드 기준은 workstream의 task별 validation 및 통합 validation에 기록한다. syscall별 커널 장애를 모두 재현한 것으로 해석하지 않으며 저장 실패는 원자 교체 전·후 경계로 검증한다.
