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

## 실제 구버전 dashboard 보강

05bf2f3의 run 39254960017에서 위 canary drain 수정은 Windows x64/ARM64
product와 Linux/macOS 이전 fixture 모두 실제 통과했다. 이후 B01 b9ddd11의
전체 run 39257449471도 통과했다. 이 결과가 아래 새 검사의 native 통과를
뜻하지는 않는다.

기준 89100eb의 실제 dashboard를 먼저 실행하고 일회성 링크로 인증해 두 값의
메타데이터를 조회하는 positive control을 확인한다. 서버와 세션을 유지한 채
현재 CLI로 보호 저장소에 이행한 뒤 같은 구버전 서버의 조회와 관리 쓰기를
실제 HTTP로 호출한다. 둘 다 storage_error여야 하며 기존 CLI의 세 거절과
함께 protected JSON hash가 변하지 않아야 한다.

현재 CLI의 dashboard 시작은 incompatible server ID를 교체해야 한다. 새 서버에
옛 세션을 보내면 HTTP 401, 새 일회성 링크로 만든 세션은 이행된 메타데이터를
정상 조회해야 한다. 원문/암호 canary는 모든 HTTP 응답에서 검사하고, 인증정보는
RAM에만 두며 evidence에 기록하지 않는다. loopback 주소를 검증하고 proxy를
사용하지 않는다. HTTP body 2 MiB와 10초 timeout을 제한한다.

Linux의 실제 old-source build/server/current CLI 검사 pass:
`/tmp/devtools-values-plan/N05-actual-old-dashboard-linux.json`. 첫 시도는 정상
plain HTTP 401을 JSON으로 읽어 fixture가 실패했다. 401의 HTTP denial을 그대로
확인하도록 고친 후 전체 여정이 통과했다. 정리는 자기 root의 dashboard stop과
vault retirement이며, dashboard stop 오류에도 vault retirement를 시도한다.

전체 보강 범위(positive control·old read/write·원자성·server 교체·session 격리·
출력 및 정리)를 리뷰했다. runtime/product 코드는 바꾸지 않았다. 기존 release
workflow의 네 native 경로에서 같은 script를 실행하므로 별도 action이나 권한은
추가하지 않는다. 이 새 검사의 macOS/Windows native 결과와 N05 전체 fault
matrix 최종 근거는 아직 pending이다.

## 실제 옛 MAC 영수증과 이후 secret의 비회귀

추가로 실제 구버전 dashboard에서 secret.set을 완료하여 옛 공유 MAC 키와
영수증을 생성한다. 이행 후 현재 CLI로 secret을 다시 변경한 다음 새 dashboard에서
옛 revision·원문·UUID를 그대로 재시도한다. 원래 changed 결과와 replayed=true를
반환해야 한다. 실제 자식은 stdin으로 받은 최신 기대값과 전달 환경을 비교하고,
일치할 때만 값을 출력한다. 출력은 여전히 [REDACTED]여야 한다. 최신 secret이나
암호를 argv에 넣지 않는다.

새 데이터 전체 scan은 옛 실제 32-byte MAC 키도 검사하며, 그 키는 evidence에
저장하지 않는다. Linux 실제 old-source/old-server/current CLI 여정 전체 pass:
`/tmp/devtools-values-plan/N05-actual-old-receipt-linux.json`.
변경 범위는 기존 fixture의 native 통합 수용 보강이다. 실제 옛 영수증과 이후
secret 비회귀, 원문 없는 응답/증거, 추가 데이터 전체 scan과 자기 자원 정리를
리뷰했다. 이 새 부분의 네 native 결과는 아직 pending이다.
