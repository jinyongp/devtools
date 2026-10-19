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
