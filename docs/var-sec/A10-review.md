# A10 저장 전환 검토 — 구현 중간 기록

A10은 아직 완료하지 않았다. 이 기록은 로컬 암호화 복구 자료, 기존 값의 최초
전환과 구버전 쓰기 차단에 관한 구현 단위다. 제품 age 복원·OS 보호 slot 재보호·새 receipt 보존 통합·설치물 및 native 검증은
남아 있다. 실행 admission과 키 회전의 추가 검토는 뒤에 기록한다.

| 영역 | 검토 결과와 근거 |
| --- | --- |
| 복구 자료 | XChaCha20-Poly1305, purpose별 HKDF/AAD; root/operation/purpose를 인증하고 크기를 제한한다. 평문 spool을 만들지 않는다. |
| 최초 전환 | generic before-image journal 대신 암호화한 before/after 자료를 먼저 실제 디스크에서 재개봉·검증한 뒤 intent를 발행한다. |
| 중단 복구 | 실제 별도 프로세스를 9개 durable checkpoint에서 종료한다. intent 이전 원본 보존, 이후 roll-forward, 잘못된 키 거절과 완료 응답 재시도를 검사한다. 전원 차단을 재현한 검증은 아니다. |
| 완료 경계 | committed intent가 남았으면 영수증을 확인하고 정리만 한다. 오래된 after-image를 후속 정상 변경 위에 다시 쓰지 않는다. |
| 구버전 차단 | v3 ready 표식을 root 전체에 유지한다. 보호된 root의 일반 maintenance transaction은 v4 pointer를 사용하며 완료·rollback 모두 v3 ready를 복구한다. 기존 v2 reader는 두 버전을 거절한다. 실제 구버전 설치 바이너리 검증은 남아 있다. |
| legacy API | 전환된 root에서 기존 values.Store의 새 profile 쓰기와 평문 MAC 키 재생성을 거절한다. 새 제품 adapter는 A11에서 연결해야 한다. |
| 전환 보존 | 전체 profile, env, 공통/override, 비어 있는 등록, 공유 MAC 키와 영수증을 조사한다. 실제 재개봉한 profile key와 각 저장 layer의 값을 비교한다. |
| 재시도 | preview fingerprint는 원문 hash가 아닌 root key 기반 HMAC이다. gate 안에서 다시 조사해 stale preview를 거절한다. 같은 operation의 암호화 복구 자료와 완료 영수증을 재사용한다. |
| 공유 구형 MAC 키 | 새 encrypted material로 옮기고 실제 개봉·일치 확인 후 기존 경로를 형식 표식으로 바꾼다. 기존 영수증은 변경하지 않는다. 새 adapter의 과거 영수증 재시도와 보존기간 정리는 후속 작업이다. |
| public-only profile | 암호화 keyset과 sealed catalog entry를 부여하지 않는다. 공개 값의 새 adapter와 보호된 root 안에서의 CRUD는 후속 작업이다. |
| 검증 한계 | Linux Go 회귀·race 및 선택한 Windows/macOS 대상 test compile. 실기 검증은 기존 GitHub Actions의 native runner에서 수행해야 한다. |

검토 중 수정한 문제:

- profile 파일만 새 형식으로 바꾸면 구버전이 새 이름의 평문 profile을 생성할 수
  있었다. 저장소 전체 차단 표식과 이를 보존하는 일반 transaction 형식을 추가했다.
- 일반 transaction 완료 정리가 새 pointer를 v2로 재구성하던 부분을 바꿔
  protected-root 버전과 vault 식별자를 유지한다.
- 빈 값 등록을 직접 추가한 직후 manifest를 갱신하지 않으면 다음 mutation의
  검증이 실패했다. 빈 등록도 서명된 상태로 만든 뒤 다음 항목을 처리한다.
- 큰 자료는 JSON/base64 할당 전에 합산 한도를 확인한다. 손상된 recovery decode의
  부분 byte buffer도 폐기한다. Go 문자열·런타임 복사의 완전한 삭제는 보장하지 않는다.

저장 성공을 실행 반영 성공으로 취급하지 않는다. 이 코드만으로 A08/A10을 완료하거나
기존 앱의 환경 회수·과거 로그 정리·secure erasure·전체 rollback 방지를 주장하지 않는다.

## 실행 admission·키 회전·논리 전송 기반 추가 검토

- 실제 key session의 admission은 snapshot에 기록한 profile ID, storage generation,
  key epoch를 인증된 catalog/profile과 비교한다. state mutex 밖에서 disk maintenance를
  기다리고, 결과를 얻은 뒤 같은 session/lock epoch가 여전히 유효한지 재검사한다.
- 실행 payload는 ticket의 profile/identity/key epoch가 snapshot metadata와 일치하는지
  확인한다. key delivery ticket 자체는 값 metadata를 포함하지 않으며 snapshot을
  인증한 소비자가 연결한다. IPC 계약 확장에 맞춰 session protocol을 2로 올렸다.
  구버전 daemon은 기존의 안정된 control handshake로만 교체한다.
- `AcquireLaunchMutation`은 실제 launcher가 가진 공유 OS lease의 배타 측을 잡고
  unknown outcome이 없음을 확인한다. 그 뒤에 disk gate를 잡는다. 이 물리 lease는
  session/admission state mutex가 아니며, 상태 조회나 수동 잠금 intent를 막지 않는다.
- profile key rotation은 새 난수 keyset·nonce와 key epoch로 다시 암호화한다.
  profile/storage identity, 논리 revision, slot ID/version, TTL과 bundle을 보존한다.
  전체 manifest와 실제 재개봉한 새 keyset/각 slot을 검증한 뒤 catalog와 함께 저장한다.
- 실제 daemon/launcher lease를 사용하는 테스트에서 회전 전에 승인된 launcher의
  spawn 결과를 기다리는지, unknown이면 회전을 거절하는지, 회전 뒤 이전 ticket을
  거절하는지 확인했다. 실제 encrypted forward replacement로 generation을 변경한
  경우에도 이전 준비 결과를 거절하고 새 generation을 수락한다.
- 논리 전송 payload는 profile 이름·sealed 이력·env·value/slot ID와 version·TTL·bundle을
  포함한다. OS 보호 키, 원본 profile ID/generation, session/launch/process는 제외한다.
  실제 age X25519 암복호화 뒤 대상의 새 profile ID/generation/keyset으로 재생성하고
  원문·논리 version·bundle·만료 override를 검증했다. public-only는 vault 없이 가능하며,
  한 번 sealed였던 빈 profile은 전송 뒤에도 sealed 상태를 유지한다.

이 추가분도 A10 전체 완료는 아니다. 제품 age backup/restore의 원자적 publication과
재시도/preview 연결, OS 보호 slot 재보호·이전 slot 정리, 새 receipt 형식 및 회전 후
verification-key 보존기간, public-only 실행의 generation 확인은 후속 통합 범위다.
기존 sealed snapshot의 native 실기 및 설치된 CLI를 통한 최종 회귀도 남아 있다.

- 종속 호출부 점검에서 legacy backup restore도 보호된 root에 새 평문 profile을
  만들 수 있음을 확인했다. keyed restore adapter가 연결될 때까지 이 경로를
  `unsupported_storage`로 차단하고 실제 age archive를 이용해 회귀 검증한다.
- 논리 자료에서 `value`나 `sealed`가 빠지거나 null인 경우를 명시적으로 거절한다.
  유효한 JSON인 손상 fixture로 확인했으며, 빈 문자열과 공개 profile의 정상 전송은
  계속 허용한다. key rotation 전후 실제 effective input revision이 같고 별도
  key epoch만 증가하는 snapshot 회귀 검사도 추가했다.

## 상위 키 보호 방식 변경 검토

`KeyFileStore.Rewrap`은 같은 난수 root를 새 protector로 감싼다. profile key epoch나
논리 값을 바꾸지 않는다. 새 protector를 통한 실제 개봉 검증, 암호화한 pending 자료의
실제 개봉 검증, 새 key file publication, 설치된 slot의 실제 개봉 검증, 이전 native
항목 제거, 영수증·pending 정리 순서다. Native 호출 중에는 maintenance/session/
key-slot write 잠금을 보유하지 않는다. 동일 native backend 재선택도 no-op 영수증을 남긴다.

- 실제 별도 프로세스를 prepared / slot-published / installed-verified / old-retired /
  receipt-written / cleaned의 6곳에서 종료하고 같은 operation ID로 복구했다.
- 새 slot 검증 실패 시 이전 native 항목을 제거하지 않는다. 이전 항목 제거 실패는
  새 보호 상태를 유지한 채 pending으로 남겨 같은 요청으로 재시도한다.
- 영수증 저장 후 pending 정리 전에 중단된 경우도 복구한다. 이 경우를 누락하면
  완료된 작업이 영구 recovery 상태가 되는 결함이 있어 수정했다.
- native item 생성 직후 로컬 intent 전 종료된 경우, 선택한 backend/vault의 기존
  항목을 개봉해 같은 root임을 확인한 경우에만 재사용한다. 다른 root를 덮어쓰거나
  backend를 자동 변경하지 않는다. 실제 OS 호출은 N01/N03 실기 gate가 남아 있다.
- 합성 root/암호가 새 디스크 자료에 원문으로 남지 않는지 검사했다. 수동 잠금
  표식과 session 재시작 뒤 `locked_manual` 상태가 유지되는지 확인했다.

기존 vault 전체 회귀, 관련 race와 vet, Windows/macOS test compile을 수행했다.
실제 native 보호 변경, CLI 연결, 영수증 retention은 아직 완료했다고 기록하지 않는다.

## 최초 생성·portable 경계 재검토

- IPC protocol을 2로 올릴 때 기존 HKDF가 같은 상수를 참조해 저장 format 1의
  키 도출까지 바뀌는 문제를 확인했다. 키 도출은 StorageVersion에 고정했다.
  Python 표준 라이브러리로 독립 계산한 storage v1/HKDF 고정 벡터로 검증했다.
- 새 vault 초기화도 encrypted forward publication과 영속 root format barrier를
  사용한다. 기존 profile이 있으면 migration을 요구하고, 보호된 root의 catalog가
  사라진 경우에는 새 빈 vault를 만들지 않는다.
- 일반 profile publication은 identity/legacy marker도 함께 관리한다. discovery는
  보호된 root의 catalog를 profile로 오인하지 않는다. Create에서 legacy resolution도
  확인해 구형 값을 일반 경로로 덮어쓰지 않는다.
- portable logical payload는 암호화 전에 ID·slot·env·kind·bundle 구조를 검증한다.
  metadata/diff용 호환 view만 private 메모리 안에서 legacy 형식으로 투영하고,
  실제 복원 자료는 logical 형식의 TTL/version/bundle을 유지한다. legacy writer는
  보호된 root뿐 아니라 새 logical archive를 빈 root에 쓰는 경우도 거절한다.

관련 package 회귀·race·vet 및 선택한 Windows/macOS test compile을 확인했다.
실제 keyed backup engine adapter와 CLI 연결은 계속 미완료 상태다.

## 암호화 저장소용 backup engine 연결

- Engine의 root-key provider는 작업마다 한 번, 모든 disk/launch gate 전에 호출한다.
  전체 profile snapshot은 gate 안에서 인증하고, archive에는 논리 payload만 넣는다.
  복원은 대상의 새 profile ID/generation/keyset으로 만든 after-image를 task 상태와
  영수증에 묶어 기존 encrypted-only maintenance transaction으로 커밋한다.
- 실제 age 파일로 export → metadata inspect → preview → apply → replay를 검증했다.
  원본 OS 키는 옮기지 않으며 slot version·bundle·만료 override를 보존한다.
  공개 값만 있는 archive로 기존 sealed profile을 덮어써도 sealed 이력은 유지한다.
- 새 preview는 root에서 용도 분리한 키로 HMAC을 계산하며 raw preview-key를 만들지
  않는다. 논리 payload에 포함되지 않는 대상 profile ID/storage generation도 검증에
  넣는다. 동일한 논리 값을 복원한 뒤 옛 preview가 다시 통과하는 반례를 수정했다.
- 새 backup receipt는 root로 인증·암호화한다. 이전 UUID backup receipt도 migration
  시 같은 형식으로 감싸 과거 완료 결과를 보존한다. values용 옛 shared-MAC receipt는
  기존 방식대로 보존한다. 필요 없어진 managed legacy preview-key는 암호화 복구
  archive에 포함한 뒤 raw 파일을 제거한다. 사용자 원본 dotenv/age 파일은 지우지 않는다.
- 실제 공유 launch lease를 보유하면 restore가 데이터 변경 전에 중단되는지, 같은
  UUID로 안전하게 재시도되는지, 변경된 목표는 conflict인지 확인했다. 이후 값 변경과
  profile key rotation 뒤에도 과거 receipt replay가 옛 값을 다시 적용하지 않는다.
- key slot이 사라져도 catalog/보호 root가 남아 있으면 새 난수 root로 초기화하지 않는다.
  이 경우는 명시적인 recovery 오류다. 새 profile keyset도 실제 재개봉 후 검증한다.
- JSON/age 경계의 소유 byte buffer와 snapshot view를 작업 종료/오류 때 폐기한다.
  런타임 문자열·OS 사본까지 지워진다는 보장은 하지 않는다.

Linux 전체 Go 회귀와 관련 race/vet, 선택한 Windows/macOS test compile을 수행했다.
CLI/profiletransfer factory가 provider를 선택하는 연결, 값 mutation/replay·retention 통합,
설치물 및 native producer/consumer 실기 검증은 남아 있다. A10 완료로 기록하지 않는다.

## 현재 profile 저장·관리 요청·검증 키 수명

- ModernStore는 공개 전용 root에서 키 설정 없이 논리 ID/slot version을 저장한다.
  첫 secret은 실제 새 profile key 개봉 검증 뒤 sealed로 승격하며, 기존 공개 ID/version과
  generation을 보존한다. 마지막 secret 제거 후에도 sealed 이력은 유지한다. 첫 승격의
  물리 실행 barrier는 disk gate 밖에서 얻고, 재진입 뒤 profile 및 UUID 영수증을 재검사한다.
- metadata/public read는 세션을 열거나 idle 시간을 연장하지 않는다. secret placeholder는
  실행용 State/CaptureLegacy에 전달할 수 없다. 만료 override는 common으로 fallback하지 않는다.
  기존 public 논리 profile을 포함한 migration도 ID/version을 바꾸지 않는다.
- 관리 변경은 revision 확인, 변경, profile/catalog, UUID receipt를 같은 maintenance
  transaction으로 저장한다. 같은 UUID/입력은 원래 결과를 반환하고 현재 값을 되돌리지
  않는다. 다른 입력과 오래된 revision은 거절한다. 공개 전용 128자 profile도 지원한다.
- sealed receipt는 root로 인증·암호화하며 요청 원문을 넣지 않는다. 요청 fingerprint는
  profile key에서 용도 분리한 MAC 키로 계산한다. 회전·복원 때 살아 있는 영수증에 필요한
  파생 검증 키만 root 암호화 상태로 보존하며 이전 데이터 복호화 키를 보관하지 않는다.
  새 값 변경 영수증의 재시도 기간은 30일이다. 만료 UUID는 request_expired로 거절하고,
  다음 관리 변경 시 더 이상 필요한 영수증이 없는 검증 키를 원자적으로 정리한다.
  새 요청이 없는 저장소에서 달력 시간만으로 파일이 삭제된다는 보장은 하지 않는다.
- 기존 shared-MAC 영수증은 migration 뒤에도 검증한다. 공개 전용 root의 MAC 키는 비밀
  값을 보호하는 키가 아니며, root 보호 도입 뒤에는 암호화한 verification 자료를 쓴다.
- 검토 중 scope 획득과 disk gate 사이에 보호 상태가 바뀌는 경합을 확인했다. gate 안에서
  다시 확인해 오래된 unsigned scope는 input_changed로 거절한다.

공개 저장/첫 승격/수동 조회, 같은 값의 version 보존, 긴 profile 이름, UUID 충돌/재시도,
회전과 새 incarnation 복원 후 재시도, 후속 값 비회귀, 만료/검증 키 정리, legacy 영수증
migration을 실제 파일로 검증한다. CLI/dashboard 연결, public-only 실행 admission,
설치물/native matrix 및 최종 통합 리뷰는 여전히 미완료다. 이 단위만으로 A10을 완료하지 않는다.

검증: 이 단위의 전체 `go test ./... -count=1` 통과. values/vault/backup/maintenance
전체 race 검사 통과(각 68.516s/25.188s/44.449s/2.315s), 관련 package vet 통과.
values의 Windows ARM64 및 macOS ARM64 test binary 교차 빌드 통과. 교차 빌드는
실제 native 실행 근거로 사용하지 않는다. 이 단위 리뷰에서 남은 확인된 결함은 없다.
