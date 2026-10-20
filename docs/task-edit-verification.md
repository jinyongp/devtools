# 계획 편집 회귀 검증

[문서 목록](README.md) · [계획 편집 API](task-edit-api.md) · [개발 환경](development.md)

계획 편집을 수정할 때 확인할 계약과 검증 진입점이다.
테스트는 임시 저장소를 사용하며 사용자 전역 journal에 개발 바이너리를 적용하지 않는다.

## 변경 영역별 검사

파일은 별도 표기가 없으면 `internal/tasks/` 아래에 있다.

| 변경 영역 | 검증 위치와 판정 기준 |
| --- | --- |
| 정의 편집과 범위 | `edit_test.go`: lifecycle과 독립적인 편집, 삽입·제외·복원, 의존성·참조 검사. |
| 영향과 완료 기준 | `assessment_test.go`: 무관 항목 보존, 후행 전파, 의미 epoch, coverage와 마감 차단. |
| 점유와 실행 | `execution_current_test.go`: stale run sync, 점유 보존, 인계, 과거 컨텍스트 거절과 재마감. |
| 동시 변경 | `edit_concurrency_test.go`: 편집·claim·done·takeover 경쟁, 단일 점유와 원자적 게시. |
| 조회와 재생 | `query_current_test.go`, `replay_test.go`: 과거 계획, 제외 항목, cursor와 이벤트 순서·형식 검증. |
| 백업과 복원 | `backup_current_test.go`: 이력 보존, 실행 권한 폐기와 stale/running 보호. |
| 공개 CLI | `internal/cli/task_edit_test.go`: 입력·schema·preview·retry와 오류 계약. |
| Dashboard | `internal/dashboard/edit_test.go`, `compatibility_test.go`: 편집·미리보기·저장과 인증·protocol 불일치 거절. |

preview는 journal을 변경하지 않아야 한다. 저장 실패는 원자 교체 전·후를 나눠 확인하고,
같은 요청을 다시 보낼 때 중복 적용하지 않아야 한다. 복원·cleanup과 공유하는 maintenance
gate의 대기 취소와 복구 후 일관성도 확인한다.

## 실행

개발 환경을 준비한 뒤 관련 Go 패키지 검사를 실행한다.

```sh
go test ./internal/tasks ./internal/cli ./internal/dashboard
devtools run check:dashboard
devtools run test:installed tasks workflow backup cleanup completion discovery
```

통합 gate는 `devtools run check:all`이다. 지원 플랫폼별 추가 검사는
[개발 안내](development.md#변경에-맞는-검사-선택)를 따른다.
syscall별 모든 커널 장애를 위 명령만으로 검증했다고 해석하지 않는다.

## 수동 브라우저 검사

`internal/dashboard`의 `TestEditBrowserFixture`가 격리 profile과 서버를 만든다.
`DEVTOOLS_EDIT_FIXTURE_OUTPUT`에 접속 링크를 저장할 private 파일 경로를 지정하고
해당 테스트를 실행한다. 실행 중·완료 후 편집, 미리보기와 저장, stale 표시,
제외된 running task와 복원을 확인한다. 테스트는 20분 후 서버와 임시 데이터를 정리한다.
접속 링크와 실행 컨텍스트를 공유 문서나 커밋에 넣지 않는다.
