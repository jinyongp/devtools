# 로컬 var/sec 저장·실행 계약

기준: `89100eb98888895a450b2ebd403e4e1c9b5f52d2`.
Workstream: `1888bdf7-3f14-4eeb-8782-329795103204`.
이 문서는 구현 계약이다. 아래 저장 형식과 신규 명령의 제품 통합·OS 검증은
후속 작업에서 수행한다. 기존 사용자 저장소를 이 문서 추가만으로 전환하지 않는다.

## 호환성과 공개 경계

- 기존 profile/env 전체 선택, 명시 `--env`의 주입 활성화, `inject=false`,
  파일/stdin 입력과 신규 dotenv 기본 secret 분류를 유지한다.
- var는 명시 조회 가능한 값이며 인터넷 공개 안전값이라는 뜻이 아니다.
  sec 조회/`--value`/자동 kind 강등은 제공하지 않는다.
- public-only profile은 vault 없이 기존 var 기능을 제공한다. 한번 sealed가 된
  profile은 secret을 unset해도 강등하지 않는다. 잠긴 var/메타데이터 조회는
  `integrity_verified:false`, 수정·검증된 주입은 unlock을 요구한다.
- normal query/schema/diagnostics/task/process receipt에는 원문·암호·원문 기반
  fingerprint·해제 키·민감 payload handle을 넣지 않는다.
- 실제 민감 payload는 공개 실행 계획과 다른 비공개 타입으로 보유한다.
  JSON·fmt 출력은 거부하거나 고정 redacted 표현이어야 한다.

## 저장·키 계약

저장 형식과 세션 protocol은 각각 독립된 version 1로 시작한다. 슬롯은
32-byte key와 매번 새 RNG 24-byte nonce의 XChaCha20-Poly1305를 사용한다.
Vault 상위키와 profile keyset은 독립 난수이며 HKDF-SHA-256 info는
protocol·vault/profile ID·용도를 결합한다. keyset의 암호화·manifest·receipt
키는 목적을 분리한다. AAD는 형식·vault/profile/value/slot ID·value version·
key epoch를 인증한다. 표시 이름·파일 경로는 안정적 ID가 아니다.

manifest는 profile 전체의 ID·세대·revision·wrapped key 참조, 공개 var,
kind·env·bundle·common/override 존재와 모든 slot/version/expiry/nonce/암호문을
인증한다. catalog는 sealed 상태와 기대 generation을 결합한다. 누락 파일을
새 빈 profile로 초기화하지 않는다. 유효한 과거 자료 전체의 rollback 방지는
별도 신뢰 anchor 없이 보장하지 않는다.

암호 backend는 Argon2id m=64MiB, t=3, p=4, salt=16 random bytes,
out=32 bytes의 명시 preset만 허용한다. KDF 전에 header·salt·nonce·숫자·길이를
검사하며 해제 실패 때 비용 하향·평문 키·새 vault로 전환하지 않는다.
`Protector.Wrap/Unwrap`은 random vault key를 보호하고 오류를 복구 초기화로
해석하지 않는다. Backend는 macOS Keychain, Windows user DPAPI,
기존 Linux Secret Service, keyring 없는 환경의 명시 암호 세션이다.

저장 문서의 최대 표현은 기존 backup plaintext 한도와 같은 128MiB다.
RPC frame은 16MiB를 상한으로 하고 allocation 전에 길이를 검사한다.
이는 메모리 방어 상한이며 OS 환경 블록으로 그만큼 전달 가능하다는 뜻이 아니다.
환경·argv·패턴 총량은 실제 플랫폼 한도를 먼저 검사하고 초과 시 원문 없는
오류를 반환한다. 임의 파일/argv/raw 경로로 조용히 우회하지 않는다.

## 인증용 canonical binary

`internal/vault.Encoder`는 JSON 저장 표현과 독립된 인증 바이트를 만든다.

1. ASCII `DTV1` magic.
2. domain을 uint32 big-endian byte length + UTF-8 bytes로 기록.
3. 고정 schema 순서로 필드를 기록한다. string/blob은 uint32 byte length와
   실제 bytes, uint는 unsigned uint64 big-endian, bool은 `00` 또는 `01`이다.
4. collection은 uint64 count 뒤 항목을 순서대로 기록한다. map처럼 순서가
   의미 없는 집합은 안정적 ID의 byte 사전순으로 정렬하며 중복 ID를 거부한다.
5. optional slot은 presence bool 뒤 존재할 때만 내용 필드를 기록한다.
   빈 문자열은 존재하는 값이며 부재와 다르다. expiry는 presence 뒤 Unix 초다.
6. domain은 용도마다 다르고 비어 있을 수 없다. invalid UTF-8/초과 크기는
   전체 실패다. JSON 입력도 duplicate/unknown/trailing data를 거부한다.

독립 벡터: domain `x`, string `a`, uint `7`, bool `true`의 결과는
`4454563100000001780000000161000000000000000701`이다.
이 표현은 canonical JSON 또는 map iteration order를 암묵적으로 사용하지 않는다.

## 실행·잠금과 자원 순서

실행 순서:

```
ResolvePlan → AuthenticateSnapshot → Materialize(actual values)
 → BindToLauncher → Admit → Launch 또는 StopOld → LaunchNew → Finalize
```

실행 전 검증 snapshot은 prepare 시점의 최신값이며 spawn 순간까지 최신을
재조회한다는 뜻이 아니다. generation/expiry/대기 한도는 admission 전에
확인한다. old stop 전에 동일 actual payload와 launcher 준비를 확보한다.
preflight용 도구 버전 확인에 전체 secret을 전달하지 않는다.

저장 작업은 기존 maintenance gate → profile lock 순서다. 세션 mutex나
admission barrier를 잡은 상태에서 디스크 gate·IPC·spawn·출력 완료를 기다리지
않는다. 디스크 snapshot은 gate 안에서 확보하고 gate를 놓은 뒤 launcher를
준비한다. admission에서 기대 generation과 lock intent를 다시 확인한다.
generation 변경 관리자는 신규 admission을 닫은 뒤 기존 admitted 작업 상태를
확인한다. 오래 걸리는 종료 확인은 mutex 밖에서 진행한다.

lock은 durable intent 기록과 신규 admission 차단 → 미입장 준비 취소 → 이미
admitted한 작업 drain → key cache 폐기 → 성공 응답 순서다. lock-before는
old 실행을 유지하고, admit-before는 제한된 launch를 완료할 수 있다.
상태 불명/timeout은 `lock_pending`/`recovery_required`이며 성공이 아니다.
수동 잠금은 daemon/WSL 재시작에도 유지된다. idle 8시간은 절대 최대 수명이
아니며 성공한 실제 비밀 사용만 연장한다. OS silent 해제는 재인증 보장이 아니다.

IPC는 private pathname UDS 또는 local named pipe, 양쪽 UID/SID와 vault/
protocol/session identity를 확인한다. 키·암호는 peer 확인 전에 보내지 않는다.
Client가 임의 repository URL로 endpoint를 선택하지 않는다. protocol mismatch,
stale session, 잘못된 frame·action은 원문 없이 거절한다. 세션 서비스는
프로젝트 코드를 실행하지 않는다. 관리 키/IPC handle은 앱에 상속하지 않는다.

## 출력·복구·완료 의미

non-TTY stdout/stderr·capture는 전달된 nonempty secret/알려진 민감 파생값을
저장 전에 마스킹한다. 두 stream은 독립 prefix 상태를 갖고 chunk/EOF/overlap을
처리한다. 짧은 값을 조용히 제외하지 않는다. TTY/PTY·binary raw는 명시 예외이며
`TTY == 사람`으로 해석하지 않는다. 처리 실패 때 raw/spool로 전환하지 않는다.
옛 execution의 패턴은 vault lock이나 값 교체 뒤에도 출력이 끝날 때까지 유지한다.

저장 성공·전달·readiness·외부 인증은 다른 성공이다. request UUID는 원래
execution/input/phase를 고정한다. 불명확한 spawn을 재시도해 중복 실행하지 않는다.
old stop 뒤의 spawn/readiness 실패는 중단 가능 상태를 보고하며 값을 rollback하지
않는다. value/selection/key epoch/storage generation을 각각 관리한다.

Migration은 legacy·MAC receipt inventory → encrypted recovery/key 준비 →
새 해제/동일성 확인 → atomic commit → 구버전 접근 거절 순서다. 기존 plaintext
before-image helper로 새 평문을 만들지 않는다. age restore는 대상 기기의 새
keyset/generation으로 저장하며 live session/handle을 이전하지 않는다.

새 오류 계약은 `vault_locked`, `key_missing`, `integrity_failed`,
`key_backend_unavailable`, `vault_recovery_required`, `lock_pending`과 입력/환경
초과·선택 누락·snapshot 만료를 구분한다. noninteractive 입력 대기 없음,
raw detail 없음, 명시 action 없는 위험한 자동 retry 없음이 기본이다.

Bundle/uses/notify는 단계 B, opt-in managed 자동 restart는 단계 C다.
같은 사용자 악성 agent·이미 실행된 앱·직접 파일/네트워크 출력·root·전체
과거 상태 복원·모든 메모리 사본 삭제는 이 계약의 격리 보장 밖이다.
각 작업은 검증→리뷰/수정/재검증→단위 커밋과 근거 기록 후 완료하고,
전체 구현 후 Z01 종합 리뷰를 수행한다. 모델 통과는 OS/제품 통과가 아니다.
