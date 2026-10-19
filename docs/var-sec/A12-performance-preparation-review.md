# A12 성능 비교 fixture 준비

A12의 native 통합 수용은 아직 pending이다. 이 기록은 독립적으로 진행한 성능 측정
준비의 검토이며 A12 완료나 성능 수용 판정이 아니다.

기존 hyperfine runner를 유지한다. 기준 `89100eb`와 현재 후보의 같은 논리 값·task
fixture를 사용하되, schema가 vault init을 제공하는 빌드만 명시 passphrase 설정과
기존 seed의 암호화 전환을 한다. 설치된 baseline에 없는 API를 강요하거나 새 제품을
암묵적인 평문 저장으로 되돌려 측정하지 않는다.

기존 fixture는 data/config/cache를 복사·초기화하므로 live socket과 key daemon을
남긴 채 실행하면 측정과 정리가 모두 잘못될 수 있다. snapshot 전과 restore 전에는
실제 private control과 OS lease로 자체 daemon을 종료한다. 복원 뒤에는 측정 밖에서
명시 unlock을 수행한다. 초기 설정·Argon2·retire·helper build는 timed region 밖이다.
이는 warm-session의 일반 명령 지연만 비교한다. cold unlock·native key API 횟수·daemon
상주 memory·출력 처리량을 이 수치로 대체하지 않는다.

검토 범위: public schema 기반 기능 선택, 기존 binary fallback, fixture 소유·경로
검사, snapshot/restore·daemon 수명, 부분 setup 실패의 cleanup 정보 보존, timing 경계,
기존 사례별 fresh mutation preflight와 metadata/raw sample 보존을 확인했다. 준비 범위에
확인된 적격 결함은 남지 않았다.

실제 검사: 기존 perf runner 회귀 10 passed. 실제 candidate와 fixture secret-set의
3 sample/1 warmup smoke를 완료하고 summary/raw 결과를
`/tmp/devtools-values-plan/A12-perf-protected-smoke/`에 보존했다. 이 smoke는 baseline과의
비교나 성능 수용 근거가 아니다. baseline/candidate를 같은 설정으로 별도 측정한다.
