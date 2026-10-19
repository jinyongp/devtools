# A10 저장 전환 검토 — 구현 중간 기록

A10은 아직 완료하지 않았다. 이 기록은 로컬 암호화 복구 자료, 기존 값의 최초
전환과 구버전 쓰기 차단에 관한 구현 단위다. 키 회전·논리 age 복원·실행 admission의
storage generation/key epoch 연결·설치물 및 native 검증은 남아 있다.

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
