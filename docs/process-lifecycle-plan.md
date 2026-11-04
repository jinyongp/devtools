# 공통 실행 수명과 제어 계약 작업 계획

기준: v0.41.0 / protocol 23 / `9dbe3cf`. 2026-11-04 Linux/WSL에서
Nuxt, Node watch, Uvicorn 및 부모·자식 종료 경계 31개를 조사했다.
foreground의 조회·제어 누락, Unix 부모 조기 종료 뒤 자식 잔류,
부모 종료에 따른 graceful cleanup 조기 중단을 공통 원인으로 확인했다.

## 완료 조건과 경계

- foreground/managed 실행은 기존 process 저장소·인증 채널·요청 영수증을 공유한다.
  실행 하나는 ID 하나로 조회하고, worker PID 교체는 실행 ID를 바꾸지 않는다.
- 실행 상태, 제어 가능 여부, readiness, 소유 범위와 정리 상태를 구분한다.
  제어기 손실을 정리 완료로 추정하지 않는다. 저장된 PID를 직접 종료하지 않는다.
- Unix 프로세스 그룹 / Windows Job 안의 자식은 부모 조기 종료에도 정리한다.
  종료 유예는 살아 있는 소유 프로세스 전체에 적용한다.
- Unix 새 세션, 외부 daemon/원격 서비스는 소유 범위 밖이다. 이 경계를 명시한다.
- foreground의 stdin/stdout/stderr, TTY job control, 자식 종료 코드를 보존한다.
  추적 저장을 강제해서 기존 읽기 전용 일회성 실행을 막지 않는다.
- schema, CLI, 대시보드, 복구 안내, bundled Skill이 같은 실행 계약을 사용한다.
  실행 가능한 복구 argv는 실제 ID를 사용하며 기존 parser로 검증한다.
- 별도 worktree 없이 main에서 작업한다. 작업 단위로 커밋하고 마지막에
  통합 검증·리뷰루프를 수행한다. 기존 GitHub Actions 검증 경로를 재사용한다.

## 작업 단위

1. **Unix 소유 범위 종료 공통화** — foreground/managed가 같은 OS runner를
   사용하고 부모 조기 종료, 상속 pipe, 자식 cleanup 유예를 검증한다.
2. **공통 실행 등록·제어** — 기존 실행 기록과 인증 RPC를 foreground에 연결하고
   목록·status·stop·readiness를 통합한다. 서비스 등록 실패와 읽기 전용
   일회성 실행 정책, 제어기 손실 및 재시도 경계를 검증한다.
3. **공개 계약·소비자 통합** — 구체적인 공통 응답 schema, capability/cleanup,
   복구 안내, 텍스트·대시보드와 문서를 정렬한다. 기존 protocol 정책을 따른다.
4. **통합 검증·리뷰루프** — 실행/저장소/제어/조회/안내/소비자/호환성/플랫폼을
   모두 검토하고 확인된 결함을 수정한다. 최종 소스에 맞는 검증 결과를 기록한다.

## 검증과 리뷰 기록

각 작업 단위에서 관련 기존 검사와 의미 있는 회귀 사례를 실행한 뒤 결과를 추가한다.
실제 실행 검증은 격리 저장소와 임시 프로젝트를 사용한다. 사용자의 서버는 종료하지 않는다.
Linux 로컬 결과와 macOS/Windows native CI 결과는 구분해서 기록한다.

### 1. Unix 소유 범위 종료 공통화

- foreground와 Unix supervisor가 `process.ExecuteOwned`를 공유한다.
  직접 자식은 그룹 정리가 끝날 때까지 reap하지 않아 그룹 식별자를 보존한다.
- 정상 부모 종료, 취소, 상속 pipe를 같은 그룹 수명 경계로 처리하고,
  살아 있는 자식 전체에 3초의 종료 유예를 적용한다.
- Linux: `go test -race -count=1 ./internal/process ./internal/services` 통과.
  부모 정상 조기 종료와 부모 TERM 종료 모두 자식의 cleanup marker 및 출력 보존을 검증했다.
- macOS arm64: process 테스트 바이너리 교차 컴파일 통과. native 실행은 아직 미검증.

현재: 작업 1 검증 완료. 작업 2–4 및 통합 리뷰는 진행 전이다.
