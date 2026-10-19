# B01 실제 값 snapshot의 bundle 선택 리뷰

기존 public-only·protected·session capture에 선택 인자를 추가한다. 기존 호출자는 생략 시 all이며 공개 CLI/schema를 아직 추가하지 않는다. 같은 profile image의 전체 manifest를 검증한 뒤 해당 ValueID 집합과 env slot만 resolve한다. secret/public을 같은 선택·revision으로 처리한다. OS/session key 전달은 선택된 값 수와 무관하게 snapshot당 한 번이다.

| 범위 | 상태·근거 |
| --- | --- |
| public-only와 protected/session provider | reviewed, 선택을 같은 captureProfile로 전달, key/launch ticket과 기존 root/generation 검증 유지 |
| 실제 membership/value/slot freeze | reviewed/pass, 파일에 저장된 bundle을 capture하고 membership 수정 후 이전 snapshot 불변·새 snapshot에만 새 member 반영 |
| all/none/missing·env | reviewed/pass, none은 인증된 빈 집합, missing은 bundle_not_found로 실패, 실제 env 선택 유지 |
| input identity·공개 메타데이터 | reviewed/pass, 선택 이름과 실제 slot ID/version을 revision에 포함, absent public도 all/none 구분, 원문 없음 |
| 기존 호출자·오류·정리 | reviewed, 기존 variadic 생략 경로 all 유지, 두 bundle 오류 분류 추가, profile key/master와 실패 snapshot Destroy 유지 |

대상 integration race 검사 pass(1.451s). 전체 values race 검사도 pass(57.103s). CLI/config/managed handoff/dashboard wiring은 다음 하위 작업이므로 이 결과로 B01 완료를 선언하지 않는다.
