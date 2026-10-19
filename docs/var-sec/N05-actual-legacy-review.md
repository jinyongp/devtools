# N05 실제 이전 CLI의 보호 저장소 접근 수용 보강

모델 reader나 현재 binary의 version 문자열 변경으로 대신하지 않고 baseline 89100eb98888895a450b2ebd403e4e1c9b5f52d2의 실제 source를 git archive/build하여 실행한다. 이미 그 source의 build identity를 가진 binary를 명시적으로 제공할 수도 있다. archive는 새 private directory에 data filter로 추출하며 archive build의 VCS 탐색은 비활성화하고 정확한 baseline commit을 ldflags로 기록·실행 후 대조한다. CI는 같은 저장소의 그 고정 commit만 fetch하며 branch/tag tip을 따라가지 않는다.

이전 CLI로 legacy secret/public 값을 만들고 현재 CLI의 실제 vault init으로 보호 형식으로 이전한다. 이전 CLI의 기존 public read·기존 write·없는 profile의 secret create를 각각 실제 process로 실행하여 storage_error 거절과 JSON data 전체 hash 불변을 요구한다. 새 data/temp/receipt에서 합성 secret canary를 scan하고 현재 CLI의 실제 child 실행은 masked 값이어야 한다. 사용자 원본 .env는 별도 파일로 남아 있고 여전히 민감함을 명시한다.

| 범위 | 상태·근거 |
| --- | --- |
| 실제 구버전 provenance와 이전 | reviewed/pass Linux, baseline 실행 identity·실제 old disk·new vault init |
| 기존 read/write·새 profile 생성 차단 | reviewed/pass Linux, 세 경로 모두 storage_error·JSON hash 불변 |
| 복구 후 실행·canary·원본 .env | reviewed/pass Linux, masked actual child·data scan·원본 파일 불변 |
| session·실패 정리 | reviewed, init 전에 vault directory를 정해 부분 실패도 authenticated retirement/lease로 drain, 자기 temporary root만 제거 |
| native CI 연결 | reviewed/pending, Linux/macOS 기존 profile producer에 결과 JSON 포함; Windows 두 arch의 packaged product binary도 같은 검사를 필수 수행, 기존 artifacts에 JSON 보존 |

로컬에서 기존 baseline binary와 새 archive build의 두 경로 모두 실제 pass했다. 실제 실행 결과는 /tmp/devtools-values-plan/N05-actual-legacy-{linux,built-linux}.json에 있다. synthetic passphrase는 stdin, old secret도 stdin, child argv는 환경 키 이름만 포함한다. 새 runner에서 실제 통과하기 전 native CI pass로 기록하지 않는다. 기존 action pin·권한·배포 조건은 유지하고 외부 action/dependency를 추가하지 않는다. Python/YAML 검사와 release contract 검사를 수행한다. N05의 나머지 fault matrix와 12방향 이전 근거는 별도로 검토한다.


## Windows live lease와 canary scan 순서

6deb437의 실제 Windows 두 product는 old CLI 세 거절과 JSON 불변 검사 뒤, live daemon이 남아 있는 data 전체 read scan에서 PermissionError를 냈다. scan을 생략하거나 PermissionError를 pass로 바꾸지 않는다. 실제 migrated child 실행을 먼저 확인하고 authenticated retirement/lease drain을 끝낸 뒤 모든 일반 파일을 읽는다. 이 순서로 실행 후 추가된 launch/receipt까지 scan한다. 다른 파일의 read 오류는 계속 실패이며 상대 fixture 경로만 진단한다. 현재 Linux 수정 경로 pass, Windows 실제 결과 전까지 원인 해소를 통과로 기록하지 않는다.
