# A00 계약·참조 fixture 검토

범위: `docs/var-sec-contract.md`, `internal/vault`의 계약 타입·canonical encoder·
민감 자료 경계. 기존 CLI/values/execution은 변경하지 않았다. 실제 암호 저장소,
OS protector, 세션·launcher·출력 필터는 후속 작업이며 이 기록의 제품 pass가 아니다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 승인된 범위·호환성·제외 | reviewed | 기존 profile/env/inject 유지, 신규 제품 동작은 후속으로 명시 |
| canonical 경계·domain·존재/빈 값 | reviewed | 문서 독립 hex vector, 경계 혼동·UTF-8·크기 거절 검사 |
| 민감 payload/키의 공개 직렬화 경계 | reviewed | Material은 JSON/text 거절, fmt redaction, 명시 byte copy와 폐기 |
| 키/세션/IPC·잠금/launcher 계약 | reviewed | ID/세대·resource 순서·admission·오류·복구·보장 한계 명시 |
| 기존 호출자 영향 | reviewed | runtime 소비자 아직 없음; values/execution/CLI 회귀 검사 |

첫 검토에서 string의 크기 검사 전 byte copy가 일어나는 경로를 확인했다.
길이를 먼저 검사하고 기존 오류를 덮어쓰지 않도록 수정했다. 같은 경계를 재검토했다.
각 field를 무조건 정렬하거나 JSON map으로 인증하지 않고 고정 schema에서 순서를
정하도록 명시했다. Key epoch를 공개 effective input identity에 넣지 않는다.

검증 명령: `go test ./internal/vault ./internal/values ./internal/execution ./internal/cli`.
실제 결과·최종 commit SHA는 workstream task의 code basis와 validation에 기록한다.
수용 조건 D01의 계약/fixture만 대상이며 K/I/L/E/O/B/P 실기 수용은 대기 상태다.
