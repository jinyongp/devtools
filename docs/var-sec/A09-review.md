# A09 출력 보호 검토

범위는 literal-byte matcher와 stream 수명, foreground/managed 실행 소비자,
TTY/raw 정책·공개 실행 메타데이터, 저장 전 capture 보호와 오류·취소다.
과거 빌드가 이미 저장한 로그의 처리와 migration 후 조회 UX는 A10/A11에서 별도 확인한다.
Native 배포·처리량 비교는 A12의 gate이며 Linux 검사를 native OS 검증으로 대체하지 않는다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 일치 의미·chunk | reviewed | byte trie/failure/output links + bounded lookahead, leftmost-longest; Python 연구의 5개 사례·독립 Go 기준값·임의 분할·fuzz |
| 짧은 값·UTF-8·EOF | reviewed | 빈 값만 제외, 1~3 byte 포함·공개 short flag; 잘못된 UTF-8도 byte 일치, EOF 잔여는 literal-match 규칙 |
| 파생 표현·한도 | reviewed | 실제 전달 값과 명시된 Go JSON body 두 형태; count/byte/state/suffix work budget 초과 시 전체 초기화 거절 |
| stdout/stderr 분리 | reviewed Linux | 실제 pipe에서 양쪽 첫 token부터 마스킹, stdout binary raw 예외는 stderr를 우회하지 않음 |
| capture·old pattern | reviewed Linux | 저장 전 각 stream 마스킹; 실제 encrypted profile 값을 교체하고 vault를 잠근 뒤 기존 앱의 옛 출력과 runtime 디스크 canary scan |
| 초기화와 restart | reviewed Linux | 실제 launcher binding 전에 matcher 초기화; 66,000 byte 합성 값은 유효 OS env이지만 보호 한도로 거절되고 old app 유지 |
| 수명·backpressure·취소 | reviewed Linux | stream reference로 late callback 보호; 마스킹한 byte만 bounded file queue; 실제 느린 pipe 소비자 취소 및 blocked sink 단위 검사 |
| TTY/PTY·원문 선택 | reviewed Linux | auto는 실제 stream TTY별 판단, explicit redacted는 PTY도 필터링; 실제 PTY와 기존 TTY input/signal/job-control fixture |
| 오류·공개 계약 | reviewed | pattern/stream fmt/JSON 거절, 닫힌 launcher 오류 코드만 제어 pipe 전송; raw 요청/실제 모드·이유는 공개 plan/status에 기록 |
| 기존 소비자·문서 | reviewed local | legacy supervisor의 새 실행도 같은 필터 사용, output flags는 configured command 앞에 위치; 관련 schema/가이드·설치물 검증 |
| native 및 benchmark | pending A12 | Windows x64/ARM64·macOS 실기와 matched throughput/memory 결과 필요; cross build는 해당 근거가 아님 |

주요 검토·수정:

- 단순 chunk replace 대신 별도 상태의 bounded byte matcher를 사용했다. substring
  reference는 매 offset에서 가장 긴 literal을 고르는 독립 구현이다. 1,005 fixtures ×
  7 chunk 크기(7,035)와 작은 입력의 모든 분할, raw invalid UTF-8·중첩 suffix·최대 패턴을
  확인했다. 추가 1,000 fixtures는 Go PRNG 생성이며 Python PRNG의 같은 입력이라고 쓰지 않는다.
- 초기 3초 fuzz에서 69,105 inputs, 실제 prefix만 보류하도록 개선한 뒤 5초 fuzz에서
  25,098 inputs를 검사했다. 제한된 예산의 샘플 검증이며 전체 증명은 아니다.
- 전체 설치 회귀에서 unrelated startup marker가 전역 최장 패턴만큼 보류되는 문제를
  확인했다. 가장 이른 실제 trie prefix만 보류하고 관계없는 출력은 즉시 전달한다.
  새 알고리즘도 독립 reference·모든 작은 분할·fuzz와 최대 패턴에서 재검증했다.
- 잠금 ACK 뒤 즉시 unlock하는 실제 호출에서 간헐적인 lock_pending을 재현했다.
  완료한 lock의 pending count를 응답 전 정리했다. 응답을 보낸 server Write를 고의로
  막은 동안 다른 connection에서 unlock하는 결정적인 경합 검사도 통과했다.
- Go exec가 inherited stdout의 blocked Write를 기다려 취소가 멈추는 실제 설치물 실패를
  재현했다. 이미 마스킹한 byte에만 bounded queue를 적용해 producer 취소를 분리했다.
  TTY stdin과 공유할 수 있는 file description의 nonblocking flag는 바꾸지 않는다.
  capture disk 쓰기는 동기로 유지해 끝난 execution 뒤의 orphan disk write를 만들지 않는다.
- 실패/취소 때 prefix를 raw flush하지 않는다. 늦은 callback이 원문 matcher를 잃지
  않도록 stream reference를 유지하고, filtered file callback에는 원문 입력을 넘기지 않는다.
- 출력 wrapper가 직접 file이 아니라는 이유로 Unix controlling-terminal 처리를 꺼 버리지
  않도록 stdin의 실제 foreground terminal을 기준으로 job control을 유지했다.
- 준비한 TTY/non-TTY 대상과 실제 foreground stream이 달라지면 실행 전에 거절해
  TTY 예외가 뒤에 연결된 pipe로 따라가지 않도록 했다.
- 자식 시작 전 masker 오류를 부모가 `launcher_unavailable`로만 오인하지 않도록 닫힌
  public error code frame을 추가했다. 실패 후보가 종료되기 전 다음 restart가 불필요한
  singleton conflict를 보지 않도록 실패한 handoff의 pipe close/종료를 bounded 대기한다.
- Output 실패에 따른 process group 정리가 수동 stop으로만 기록되지 않도록
  `output_protection_failed` 원인을 우선 보존했다.
- 기존 secret 주입/transfer 검사에서 원문을 비교하는 경우만 explicit raw를 사용하게
  바꿨다. 기본 보호·binary raw stdout + protected stderr·nonzero exit는 별도 실제 프로세스로 확인한다.

운영 한계: 동일 사용자 악성 앱, 임의 재인코딩·추가 FD·파일·네트워크·stream 분산 유출은
격리하지 않는다. 앱 환경/Go 문자열의 즉시 물리적 제거, 과거 로그의 소급 보호도 주장하지 않는다.
중단 불가능한 OS file callback은 CLI 종료/해제까지 filtered block을 유지할 수 있으나
원문 matcher나 secret input을 소유하지 않는다.

근거: `/tmp/devtools-values-plan/A09-*.log`, `verify/scenarios/output_redaction.py`,
`internal/redact/*_test.go`, `internal/cli/{launcher,redaction}_test.go`.
현재 소스와 실제 검사 결과가 달라진 경우 영향 근거만 다시 확인하고 native 미실행을 pass로 기록하지 않는다.
