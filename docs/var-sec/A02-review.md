# A02 암호 backend·keyset 검토

범위: `internal/vault/keys.go`, `keyfile.go`, Protector identity와 profile key wrapping.
실제 OS backend·세션·사용자 CLI 통합은 후속이다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| KDF 입력·preset 파싱 | reviewed | 4KiB header 한도, 고정 preset·salt/nonce/cipher 길이 검증 뒤만 Argon2 실행 |
| 실제 KDF·AEAD | reviewed | 실제 64MiB/t3/p4 roundtrip·wrong password·cipher 손상·다른 vault 거절 |
| 민감 자료·취소·난수 실패 | reviewed | raw 암호/derived key 미저장, byte copy 정리, 취소/RNG/잘못된 UTF-8·줄바꿈 검사 |
| keyset ID·epoch binding | reviewed | vault/profile/epoch 치환 거절, 기존 Profile 실제 key wrapping 연결 |
| 초기화·key 분실·backend 고정 | reviewed | unwrap 검증 후 create-only 게시, 재초기화·backend mismatch 거절, 없는 키 자동 생성 없음 |
| 기존 호출자·회귀 | reviewed | Profile fixture를 실제 wrapped key로 갱신하고 vault/values/execution race 검사 |

리뷰에서 profile ID 생성 전에 만들어 둔 opaque key wrapper를 받는 생성자 경계를
수정했다. 이제 ID를 생성한 뒤 해당 ID·epoch로 실제 wrapping한다. 또한 protector와
파일의 backend/vault ID가 어긋난 상태로 초기화될 수 없도록 명시 Identity를 확인한다.
기존 A01 저장·manifest 경로를 변경된 key wrapper와 함께 재검사했다.

검증: `go test -race ./internal/vault ./internal/values ./internal/execution`와
`go vet ./internal/vault`. Invalid KDF header 검사는 KDF 호출 횟수 0을 확인한다.
제품 설치·native OS 보호·vault session·비대화형 CLI 수용 조건의 pass가 아니다.
