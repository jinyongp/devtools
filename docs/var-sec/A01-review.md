# A01 인증된 profile 저장 모델 검토

범위: `internal/vault`의 Profile/Slot/Bundle 모델, XChaCha20-Poly1305·HKDF·
manifest/catalog, strict JSON, FileStore와 논리 변경. 기존 `values.Store` runtime의
전환은 A10/A11에서 수행하며 현재 사용자 데이터는 변경하지 않았다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 저장 기밀성·키 용도/ID/epoch 경계 | reviewed | 실제 AEAD roundtrip, 잘못된 키·profile 치환 거절, 평문 scan |
| 전체 선택·var·kind·slot·bundle 무결성 | reviewed | 필드 변조/삭제·membership 변경 후 실제 Environment 거절 |
| sealed/public·존재/빈 값·만료 override | reviewed | vault 없는 public-only, 잠긴 var 미검증 읽기, unset 뒤 sealed 유지 |
| nonce 실패·same value·실패 원자성 | reviewed | nonce 생성 실패 뒤 원래 encode 유지, same value version/cipher 유지 |
| catalog·원자적 저장·기존 파일 보호 | reviewed | 두 batch writer, profile 단독 rollback·등록 파일 삭제·downgrade 거절 |
| parser/자원·호환성·기존 호출자 | reviewed | 중복/unknown/trailing/invalid UTF-8 거절, 기존 values/maintenance 회귀 |

Catalog는 profile ID/generation뿐 아니라 revision/tag를 인증한다. profile 파일만
과거 유효본으로 교체하는 것은 거절하지만 catalog와 모든 자료를 함께 되돌리는
전체 rollback은 여전히 보장 밖이다. 일반 FileStore.Create는 기존 canonical 파일을
덮어쓰지 않는다. 현재의 before-image 경로는 이미 암호화된 상태에만 쓰며 legacy
원문 이전은 A10의 독립 복구 경로가 필요하다.

리뷰에서 값 크기를 암호화 전 검사하도록 보완했다. 평문 scan fixture를 짧은
`p1`에서 긴 합성 값으로 바꿔 base64 암호문과의 우연한 문자열 일치로 인한
불안정성을 제거했다. 관련 테스트·검증 경로를 재확인했다.

검증: `go test -race ./internal/vault ./internal/values ./internal/maintenance
./internal/profilekey ./internal/platformfs` 및 `go vet ./internal/vault`.
현재 범위 K01/K02/K03/K04/K07/U01의 모델·저장 기반만 검증한다. 실제 OS protector,
KDF/session, 사용자 CLI 전환, 설치물/OS 수용 검증은 후속이며 pass로 대체하지 않는다.
