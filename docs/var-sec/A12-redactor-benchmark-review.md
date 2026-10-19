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
