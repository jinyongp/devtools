# A12 warm-session matched 측정 결과

A12 준비 과정의 실제 측정이며 native 수용이나 성능 gate 완료 판정은 아니다.
기준 binary는 89100eb, 후보 binary는 33c9c56이다. CRUD harness는 33c9c56,
full-profile 실행 harness는 1035006이며 네 측정 모두 clean checkout에서 시작했다.
후보 runtime은 그 뒤 native 수정 전의 측정이다. 최종 상태의 성능 판정에 자동 재사용하지 않는다.

Linux WSL2 6.18.33.2 / x86_64 / Ryzen 7 9800X3D, Go 1.27.2, hyperfine 1.20.0.
같은 build flags와 synthetic fixture로 기준 후 후보를 순차 실행했다. 각 case 3 warmup·10 samples.
동시에 로컬 무거운 검사·빌드는 실행하지 않았다. 외부 시스템 부하까지 통제한 실험은 아니다.
passphrase 초기 설정·Argon2·fixture 복원/unlock·daemon 종료·helper build는 측정 밖이다.
기존 평문 baseline과 보호된 후보는 보안 작업량이 다르므로 아래 비용 증가는 기능 변화의 비용이다.

36개 CRUD와 3개 full-profile 실행 case를 완료했다. 아래는 39개 중 12개 선택 결과다.
수치는 평균 ± 표준편차(ms)이며 raw JSON에는 모든 case의 개별 시간·median·p95·환경·binary digest가 있다.

| 작업 | 공개 키 수 (+ secret 1개) | 기준 ms | 후보 ms |
| --- | ---: | ---: | ---: |
| values/variable-get | 10 | 6.88 ± 0.46 | 6.88 ± 0.21 |
| values/variable-set | 10 | 10.73 ± 0.29 | 52.03 ± 1.50 |
| values/secret-set | 10 | 10.91 ± 0.41 | 55.43 ± 1.10 |
| values/variable-get | 100 | 7.17 ± 0.56 | 7.59 ± 0.62 |
| values/variable-set | 100 | 32.93 ± 15.69 | 53.98 ± 1.42 |
| values/secret-set | 100 | 10.90 ± 0.29 | 56.27 ± 1.36 |
| values/variable-get | 1000 | 7.67 ± 0.27 | 10.65 ± 0.43 |
| values/variable-set | 1000 | 21.16 ± 14.10 | 71.03 ± 1.08 |
| values/secret-set | 1000 | 13.68 ± 0.56 | 79.22 ± 2.76 |
| project/run-injected-noop | 10 | 8.21 ± 0.54 | 60.50 ± 31.55 |
| project/run-injected-noop | 100 | 7.92 ± 0.27 | 21.11 ± 0.75 |
| project/run-injected-noop | 1000 | 10.02 ± 0.92 | 35.63 ± 2.44 |

쓰기에는 수십 ms의 추가 비용이 관측됐다. transaction·fsync·IPC 각각의 원인 기여는 아직 분리하지 않았다.
100/1,000-key variable-set baseline과 10-key 실행 후보의 변동이 커서 해당 비율·통계적 유의성을 단정하지 않는다.

OS key API 호출 횟수, cold unlock, peak RSS/상주 sensitive memory, redact 처리량·최대 buffer,
concurrent launch, 변경→재시작 수렴 지연은 이 결과에 포함하지 않았다. p99를 10 samples로
평가하지 않는다. 성능 수용 threshold도 정해진 것처럼 만들지 않는다. 이 축들은 후속 A12/C03에 남는다.

[전체 결과와 개별 raw samples](evidence/A12-warm-session-comparison.json). 원래 hyperfine raw output과
경고는 `/tmp/devtools-values-plan/A12-{values,execution}-{baseline,candidate}/`에 보존했다.
리뷰: 두 binary identity, 같은 case key/크기/반복 수, 누락 case 없음, timing 경계와 불안정 사례,
원문 없는 결과 schema를 대조했다. 본 기록을 A12 완료 근거로 제출하지 않는다.
