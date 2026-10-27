# CLI 신뢰성 변경 리뷰

기준은 e24ac806d7ede22cdab4dfe0d4b63067e3e66a24, 구현은 main의 현재 checkout에서
진행했다. 검증용 branch/worktree를 만들지 않는다. 정본 계획·조사·단위별 증거는
devtools profile의 workstream `f2210fc3-cf00-40bd-af1e-63312a9736db`에 보관한다.
최종 검증 레코드가 실제 source SHA와 Actions 실행 결과를 기록한다.

| 단위 | 변경·리뷰 경계 | 단위 커밋 |
| --- | --- | --- |
| U01 | dotenv 위치·기대/관측 타입·안전한 한도 사실, 빈 action argv | 8155e2a |
| U02 | services/ports/backup/proxy/cleanup의 확인된 실패 원인과 입력 종료 코드 | 5f7dbcf |
| U03 | secret preflight, import dry-run, passive request 조회의 backend 복구 | 1e78398 |
| U04 | 19개 schema/runtime 불일치, boolean 선택·정적 제약·포트·duration·예시 | 85aca52 |
| U05 | 128 MiB raw 입력, 관리 import 500,000 bytes, 취소·버퍼 수명 | f08c6e6 |
| U06 | exposure provider/auth preflight와 인증 읽기 취소 | 63bd99d |
| U07 | 단일 파일의 bounded header/index/chunk, v1 cursor와 archive 복원 | 12be29e |
| U08 | passive 인증 준비, Held 집계, 중복 receipt와 물리 bytes, protocol 16 소비자 | 64629a7 |
| U09 | global route 규모·동시성·source 변경과 분리한 비용 측정 | dfa84db |
| U10 | 전 단위 소비자 재검토, 실제 설치 CLI·PTY·native·WSL 통합 검증 | 이 문서 포함 변경 |

통합 리뷰에서 보강한 사항:

- U04의 강화된 schema가 거절하던 proxy 도움말 예시를 수정했다. task JSON/field의
  별칭과 빈 args 선택을 확인하고, task/vault/port의 정적 충돌을 I/O 전에 거절한다.
- U05/U06의 io.Pipe 성공만으로 OS stdin을 검증했다고 판단하지 않았다. 열린 실제
  pipe에 SIGINT를 보낸 설치 CLI 검사에서 blocking descriptor의 Close 대기를 발견했다.
  Unix 입력을 poll 가능한 소유 descriptor로 준비하고 읽기가 끝난 뒤 원래 flags를 복원한다.
  임의 blocking non-Closer의 종료 책임은 embedding host에 남는다.
- U07의 header/chunk를 cache token에도 묶는다. 같은 필터·revision인 다른 cache의
  chunk를 섞지 않으며 EOF·offset·길이·expiry와 선택 chunk의 순서를 검사한다.
- U08은 services → shared key-slot lease → maintenance 순서로 잠근다. session IPC는
  잠금 전에 끝내며 Held scanner에서 조회 wrapper를 다시 호출하지 않는다. 끝에서도
  identity를 재확인하고 불완전한 집계는 부분 합계를 내보내지 않는다.
- protocol 16 상수를 config schema와 strict 소비자까지 갱신했다. envelope 1,
  versionless TOML, private IPC와 업무 저장 형식은 유지한다.

검증은 기존 package/installed/native fixture를 확장한다. 새 `cli_reliability`는
실제 unlock argv 실행, 별도 인증 입력, UUID retry, passive 조회, 보관 집계,
관리 입력 제한, frozen cursor, JSON/text/Unix PTY의 같은 마스킹 실패,
다른 root로 backup restore와 retry를 검사한다. WSL adapter도 같은 fixture를 실행한다.
Windows product lane은 native ConPTY의 오류 표시·Ctrl+C와 redirected auth EOF를
구분한다. 취소 가능한 Windows pipe 읽기는 기존 context 검사도 함께 사용한다.

일반 task cache benchmark의 200/1,000개 × 4 KiB 항목 후속 limit1 조회는 이 host에서
약 0.34–0.42 ms, 0.82 MB/op였다. 첫 페이지 생성 비용은 이 측정에 포함하지 않는다.
proxy의 global target 직렬 요청은 등록 프로젝트 0/1/50/200개에서 약
0.04/0.06/0.59/2.08 ms였다. Go 1.27.3, Linux amd64/WSL2, Ryzen 9800X3D,
in-process transport, 각 3회 측정이다. raw 결과와 CPU/alloc profile은 단위 증거에
연결했다. CPU profile은 setup도 포함하며 독립 stage 값을 요청 시간처럼 합하지 않는다.
느린 filesystem·실제 wire latency·실제 요청의 lock-wait 비중을 측정한 결과는 아니다.
TTL로 검사를 생략하거나 route engine을 재설계하지 않는다.

보관 집계는 secret payload를 복호화·삭제하지 않는다. 공개/legacy 메타데이터 확인과
보호 메타데이터 인증을 구분하고 payload에는 인증된 record를 근거로 한 파일 크기만
표시한다. expired payload sweep, spent receipt 압축·삭제, 전체 시각 수용 검사와
실제 외부 provider/hosts/trust 변경은 이 변경의 완료 주장에 포함하지 않는다.
Windows의 동기 I/O 취소와 console 신호는 같은 계약으로 취급하지 않는다.
[Windows I/O 취소 API](https://learn.microsoft.com/en-us/windows/win32/api/ioapiset/nf-ioapiset-cancelsynchronousio)도
취소 요청과 실제 완료를 구분하므로 fixture는 종료 결과를 관측한다.

최종 gate: 같은 tree의 `check:all`(전체 Go race 포함), `test:installed` 전체,
skill·shell·schema·소비자 검사와 기존 Release workflow의 `publish=false` 실행.
로컬/cross compile을 native macOS/Windows 실행 성공으로 대신하지 않는다.
