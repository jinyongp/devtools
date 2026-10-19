# A12 redactor 측정 준비와 리뷰

기존 89100eb에는 이 보호 matcher가 없으므로 같은 기능의 속도 비교를 만들지 않는다.
Go benchmark로 실제 immutable matcher 생성 비용과 warm stream을 별도로 측정한다.
10/100/1,000개 32-byte literal 및 64KiB 한계 pattern, 약 1MiB 출력, 1/4,096/32,768-byte
chunk를 선택했다. unprotected control은 동등한 보안 baseline이 아닌 비용 참고점이다.

생성 benchmark는 New/Destroy를 포함하고 fixture 준비는 제외한다. streaming은 matcher
준비를 제외하되 writer 생성·모든 write·EOF·정리를 포함한다. 반복되는 독립 block의
기대 출력 길이와 canary 제거를 timing 전에 확인한다. runtime 기존 property/reference
검사를 유지하며 동일한 fixture 크기와 chunk마다 실제 처리 byte 수를 보고한다.

stream별 allocation을 Go benchmem으로 측정한다. buffer-cap-B는 per-writer backing
array의 용량(5×longest + 32KiB)이며 automaton·sink·전체 RSS가 아니다. withheld-max-B는
알고리즘의 한계이며 각 chunk 뒤 실제 보류 byte가 그 범위를 넘으면 측정을 실패시킨다.
민감 메모리의 OS 수준 잔존 시간이나 process peak memory를 이 지표로 주장하지 않는다.

리뷰: workload 구분·setup/timing 경계·실제 작업 소비·선행 기대값·패턴 한계·EOF/Destroy·
통계적 한계·메모리 지표 명칭을 확인했다. 기존 redact suite pass. 기준 SHA와 새 harness
commit을 고정한 뒤 5회 반복하며 최적화나 임의의 성능 합격선을 추가하지 않는다.

## 실제 측정

clean da65ef7, Go 1.27.2, 같은 WSL2/Ryzen 9800X3D에서 count=5·benchtime=100ms로
18 cases/90 batch estimates를 완료했다. 무거운 로컬 검사를 동시에 실행하지 않았다.
[전체 raw stdout과 반복별 metrics](evidence/A12-redactor-da65ef7.json)를 보존한다. 선택 결과는 다음과
같으며 throughput은 5개 calibrated estimate의 median이다.

| 입력 조건 | MB/s median | stream B/op median | backing capacity |
| --- | ---: | ---: | ---: |
| patterns=10 / 4KiB chunk | 145.17 | 33120 | 32928B |
| patterns=1000 / 4KiB chunk | 150.03 | 33120 | 32928B |
| max-pattern / 4KiB chunk | 182.70 | 360640 | 360448B |

90 batch estimates는 90개의 단일 stream latency samples를 뜻하지 않는다. Go의
calibration이 고른 iteration 수를 raw에 보존했고 이 결과로 p95/p99나 유의성을
주장하지 않는다. 최대 패턴에서 capacity가 크더라도 총 출력량에 따라 증가하지
않는지 각 write 뒤 검사했다. process peak RSS·민감 메모리 보유 시간·cold key API
호출 등 A12의 다른 측정은 별도로 남는다.
